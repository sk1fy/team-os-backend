//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type interfaceCore struct {
	*fakeDistributionCore
	deny            bool
	unavailable     bool
	delay           time.Duration
	permissionCalls atomic.Int32
}

func (f *interfaceCore) Permission(ctx context.Context, s corebridge.Scope, in corebridge.PermissionInput) (corebridge.Permission, error) {
	f.permissionCalls.Add(1)
	if f.delay > 0 {
		timer := time.NewTimer(f.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return corebridge.Permission{}, ctx.Err()
		}
	}
	if f.unavailable {
		return corebridge.Permission{}, errors.New("fixture outage")
	}
	p, e := f.fakeDistributionCore.Permission(ctx, s, in)
	p.CanViewLead = !f.deny
	return p, e
}
func TestDistributionInterfaceActionsFiltersScopeAndConfirmedTimezone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employee := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employee, "employee", "active"}})
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	core := &interfaceCore{fakeDistributionCore: &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}}
	s := &Service{pool: pool, now: time.Now, distributionCore: core}
	b, e := s.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'42','verified',now())", uuid.New(), company, b.BindingID, ownerID)
	exec("UPDATE distribution_bindings SET mapping_revision=1,mapping_ack_revision=1 WHERE id=$1", b.BindingID)
	g, e := s.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Interface", MemberIDs: []uuid.UUID{employee}})
	if e != nil {
		t.Fatal(e)
	}
	exec("INSERT INTO distribution_settings(company_id,timezone) VALUES($1,'Asia/Tokyo')", company)
	rule := uuid.New()
	exec("INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id) VALUES($1,$2,$3,1,'123','20','30',$4)", rule, company, b.BindingID, g.ID)
	// The database protects old and new writers, not just the public editor.
	if _, e = pool.Exec(ctx, "INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id) VALUES($1,$2,$3,1,'123','21','31',$4)", uuid.New(), company, b.BindingID, g.ID); !isUniqueViolation(e) {
		t.Fatalf("onegroup invariant %v", e)
	}
	add := func(lead int, state string, settled bool, updated time.Time) uuid.UUID {
		t.Helper()
		entry, id := uuid.New(), uuid.New()
		exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)", strconv.Itoa(lead), company, b.BindingID, entry)
		exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking',now(),now())", entry, company, strconv.Itoa(lead), b.BindingID, uuid.New())
		exec("INSERT INTO distribution_queue(id,company_id,entry_id,rule_id,group_id,account_id,lead_id,state,settled,updated_at) VALUES($1,$2,$3,$4,$5,'123',$6,$7,$8,$9)", id, company, entry, rule, g.ID, strconv.Itoa(lead), state, settled, updated)
		return id
	}
	now := time.Now()
	queue := add(10, "waiting", false, now)
	raw, e := s.DistributionQueueDetail(ctx, owner, queue)
	if e != nil {
		t.Fatal(e)
	}
	var detail struct {
		UpdatedAt time.Time `json:"updatedAt"`
		LeadID    *string   `json:"leadId"`
		Actions   []string  `json:"actions"`
	}
	if json.Unmarshal(raw, &detail) != nil || detail.LeadID == nil {
		t.Fatal(string(raw))
	}
	key := uuid.New()
	action, _ := json.Marshal(distributionUIAction{Action: "recalculate", RequestID: key, ExpectedUpdatedAt: detail.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, queue, action); e != nil {
		t.Fatal(e)
	}
	// Lost response: replay the exact original body after a new Service instance.
	restarted := &Service{pool: pool, now: time.Now, distributionCore: core}
	if _, e = restarted.DistributionQueueAction(ctx, owner, queue, action); e != nil {
		t.Fatalf("replay %v", e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_queue_history WHERE queue_id=$1 AND reason='user_recalculate'", queue).Scan(&count); e != nil || count != 1 {
		t.Fatalf("duplicateaction %d %v", count, e)
	}
	altered, _ := json.Marshal(distributionUIAction{Action: "cancel", RequestID: key, ExpectedUpdatedAt: detail.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, queue, altered); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("changedbody %v", e)
	}
	stale, _ := json.Marshal(distributionUIAction{Action: "cancel", RequestID: uuid.New(), ExpectedUpdatedAt: detail.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, queue, stale); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("stalerevision %v", e)
	}
	q := db.New(pool)
	row, e := q.LockDistributionQueue(ctx, queue)
	if e != nil {
		t.Fatal(e)
	}
	foreign := owner
	foreign.CompanyID = uuid.New()
	if _, e = s.DistributionQueueDetail(ctx, foreign, queue); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("foreignactor %v", e)
	}
	if _, e = s.DistributionQueueAction(ctx, Actor{CompanyID: company, UserID: employee, Role: "owner"}, queue, stale); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("staleJWTrole %v", e)
	}
	cancelBody, _ := json.Marshal(distributionUIAction{Action: "cancel", RequestID: uuid.New(), ExpectedUpdatedAt: row.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, queue, cancelBody); e != nil {
		t.Fatal(e)
	}
	row, e = q.LockDistributionQueue(ctx, queue)
	if e != nil || row.State != "cancelled" || !row.Settled {
		t.Fatalf("cancel %+v %v", row, e)
	}
	// Complete results belong to the company civil day, not server UTC date.
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	civil := time.Now().In(tokyo)
	day := time.Date(civil.Year(), civil.Month(), civil.Day(), 0, 30, 0, 0, tokyo)
	add(11, "confirmed", true, day)
	add(12, "kept", true, day)
	add(13, "confirmed", true, day.AddDate(0, 0, -1))
	failed := add(14, "failed", true, now)
	add(115, "waiting", false, now)
	add(116, "waiting", false, now)
	// Five independent 800ms CRM checks exceed the shared 3s budget when
	// serialized. The real service must complete them with bounded fan-out.
	core.delay = 800 * time.Millisecond
	core.permissionCalls.Store(0)
	summary, e := s.DistributionSummary(ctx, owner, g.ID)
	core.delay = 0
	if e != nil {
		t.Fatal(e)
	}
	var stats struct {
		Available bool   `json:"metricsAvailable"`
		Confirmed *int64 `json:"confirmedToday"`
		Kept      *int64 `json:"keptToday"`
		Errors    *int64 `json:"errors"`
	}
	_ = json.Unmarshal(summary, &stats)
	if !stats.Available || stats.Confirmed == nil || *stats.Confirmed != 1 || stats.Kept == nil || *stats.Kept != 1 || stats.Errors == nil || *stats.Errors != 1 {
		t.Fatal(string(summary))
	}
	if core.permissionCalls.Load() != 5 {
		t.Fatalf("permission checks=%d", core.permissionCalls.Load())
	}
	core.delay = 4 * time.Second
	timedOut, e := s.DistributionSummary(ctx, owner, g.ID)
	core.delay = 0
	if e != nil {
		t.Fatal(e)
	}
	var timeoutStats struct {
		Available bool   `json:"metricsAvailable"`
		Reason    string `json:"metricsReason"`
		Confirmed *int64 `json:"confirmedToday"`
	}
	if json.Unmarshal(timedOut, &timeoutStats) != nil || timeoutStats.Available || timeoutStats.Confirmed != nil || timeoutStats.Reason != "permission_budget_exceeded" {
		t.Fatal(string(timedOut))
	}
	filtered, e := s.DistributionQueuePage(ctx, owner, DistributionQueueFilter{Tab: "errors", GroupID: g.ID}, 1, 0)
	if e != nil {
		t.Fatal(e)
	}
	var page struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
		HasMore bool `json:"hasMore"`
	}
	_ = json.Unmarshal(filtered, &page)
	if len(page.Items) != 1 || page.Items[0].ID != failed || page.HasMore {
		t.Fatal(string(filtered))
	}
	// Denied resources are never counted. Outage is unknown rather than zero.
	core.deny = true
	summary, e = s.DistributionSummary(ctx, owner, g.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(summary, &stats)
	if !stats.Available || stats.Errors == nil || *stats.Errors != 0 {
		t.Fatal(string(summary))
	}
	if _, e = s.DistributionQueueDetail(ctx, owner, failed); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("denieddetail %v", e)
	}
	core.deny = false
	core.unavailable = true
	summary, e = s.DistributionSummary(ctx, owner, g.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(summary, &stats)
	if stats.Available || stats.Errors != nil {
		t.Fatal(string(summary))
	}
	core.unavailable = false
	config, _ := json.Marshal(map[string]any{"expectedRevision": g.Revision, "name": "Updated", "memberIds": []uuid.UUID{employee}, "disabledMemberIds": []uuid.UUID{}, "active": false, "algorithm": "round_robin"})
	if _, e = s.ConfigureDistributionGroup(ctx, owner, g.ID, config); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfigureDistributionGroup(ctx, owner, g.ID, config); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("groupconflict %v", e)
	}
	// Used rule point may not reinterpret any queued or historic episode.
	change, _ := json.Marshal(map[string]any{"expectedRevision": 1, "active": false, "keepCurrentResponsible": true, "pipelineId": "20", "statusId": "31"})
	core.refs = corebridge.References{State: "fresh", FetchedAt: time.Now(), FreshUntil: time.Now().Add(time.Minute), Pipelines: []corebridge.Pipeline{{ID: "20", Statuses: []corebridge.Status{{ID: "31"}}}}, Users: []corebridge.User{{ID: "42", IsActive: true}}}
	if _, e = s.DistributionRuntimeWrite(ctx, owner, "rule", rule, change); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("usedpoint %v", e)
	}
	// Retry is limited to an explicit no-attempt failure; no new command may be
	// created while an existing operation has an unknown external outcome.
	failedRow, e := q.LockDistributionQueue(ctx, failed)
	if e != nil {
		t.Fatal(e)
	}
	retryBody, _ := json.Marshal(distributionUIAction{Action: "retry", RequestID: uuid.New(), ExpectedUpdatedAt: failedRow.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, failed, retryBody); e != nil {
		t.Fatalf("safe retry %v", e)
	}
	retried, e := q.LockDistributionQueue(ctx, failed)
	if e != nil || retried.State != "waiting" || retried.Settled {
		t.Fatalf("safe retry state %+v %v", retried, e)
	}
	unknown := add(15, "uncertain", false, time.Now())
	operation, decision := uuid.New(), uuid.New()
	exec("UPDATE distribution_queue SET operation_id=$2,decision_id=$3,command='{}',idempotency_key=$4,cancel_key=$5,reconcile_key=$6,availability_hash=decode(repeat('00',32),'hex'),planned_employee_id=$7 WHERE id=$1", unknown, operation, decision, uuid.New(), uuid.New(), uuid.New(), employee)
	exec("INSERT INTO distribution_group_claims(company_id,group_id,queue_id) VALUES($1,$2,$3)", company, g.ID, unknown)
	exec("INSERT INTO distribution_lead_claims(account_id,lead_id,queue_id) VALUES('123','15',$1)", unknown)
	unknownRow, e := q.LockDistributionQueue(ctx, unknown)
	if e != nil {
		t.Fatal(e)
	}
	deniedRetry, _ := json.Marshal(distributionUIAction{Action: "retry", RequestID: uuid.New(), ExpectedUpdatedAt: unknownRow.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, unknown, deniedRetry); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("unknown retry %v", e)
	}
	dispatchedCancel, _ := json.Marshal(distributionUIAction{Action: "cancel", RequestID: uuid.New(), ExpectedUpdatedAt: unknownRow.UpdatedAt})
	if _, e = s.DistributionQueueAction(ctx, owner, unknown, dispatchedCancel); e != nil {
		t.Fatalf("dispatched cancel %v", e)
	}
	held, e := q.LockDistributionQueue(ctx, unknown)
	if e != nil || !held.CancelRequested || held.Settled || held.State != "uncertain" || held.OperationID.UUID != operation {
		t.Fatalf("unsafe cancel %+v %v", held, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_lead_claims WHERE queue_id=$1", unknown).Scan(&count); e != nil || count != 1 {
		t.Fatalf("lead guard released %d %v", count, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_group_claims WHERE queue_id=$1", unknown).Scan(&count); e != nil || count != 1 {
		t.Fatalf("group guard released %d %v", count, e)
	}

	// Permission loss while configuration waits for the company input lock must
	// win over the role observed at request entry.
	latestGroup, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: company, ID: g.ID})
	if e != nil {
		t.Fatal(e)
	}
	waitingConfig, _ := json.Marshal(map[string]any{"expectedRevision": latestGroup.Revision, "name": "Must not save", "memberIds": []uuid.UUID{employee}, "disabledMemberIds": []uuid.UUID{}, "active": false, "algorithm": "round_robin"})
	blocker, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	if _, e = db.New(blocker).LockDistributionAvailabilityVersion(ctx, company); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := s.ConfigureDistributionGroup(ctx, owner, g.ID, waitingConfig); done <- e }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		e = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE state='active' AND wait_event_type='Lock' AND query LIKE '%distribution_availability_versions%' AND pid<>pg_backend_pid())").Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("configuration did not reach input lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e = blocker.Exec(ctx, "UPDATE users SET role='employee' WHERE company_id=$1 AND id=$2", company, ownerID); e != nil {
		t.Fatal(e)
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("revoked while waiting %v", e)
	}
	finalGroup, e := q.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: company, ID: g.ID})
	if e != nil || finalGroup.Name != latestGroup.Name || finalGroup.Revision != latestGroup.Revision {
		t.Fatalf("unauthorized save %+v %v", finalGroup, e)
	}

}
