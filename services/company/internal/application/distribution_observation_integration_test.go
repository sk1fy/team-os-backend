//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestDistributionObservationIsolationDedupPrivacyAndEnableBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employee := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employee, "employee", "active"}})
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	links := &interfaceCore{fakeDistributionCore: &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}}
	links.refs = corebridge.References{State: "fresh", FetchedAt: time.Now(), FreshUntil: time.Now().Add(time.Hour), Users: []corebridge.User{{ID: "2", IsActive: true}}, Pipelines: []corebridge.Pipeline{{ID: "20", Statuses: []corebridge.Status{{ID: "30"}}}}}
	crm := &nightCore{fakeDistributionCore: links.fakeDistributionCore, now: time.Now}
	service := &Service{pool: pool, now: time.Now, distributionCore: links, deliveryCore: crm}
	binding, e := service.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	exec := func(q string, a ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, a...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE distribution_bindings SET mapping_revision=1,mapping_ack_revision=1 WHERE id=$1`, binding.BindingID)
	for _, m := range []struct {
		user uuid.UUID
		crm  string
	}{{employee, "2"}, {ownerID, "42"}} {
		exec(`INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,$5,'verified',clock_timestamp())`, uuid.New(), company, binding.BindingID, m.user, m.crm)
	}
	group, e := service.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Observe", MemberIDs: []uuid.UUID{employee}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.SaveDistributionTimezone(ctx, owner, "UTC"); e != nil {
		t.Fatal(e)
	}
	exec(`INSERT INTO user_schedules(company_id,user_id,template) VALUES($1,$2,'{"type":"week","days":[0,1,2,3,4,5,6],"start":"00:00","end":"23:59"}')`, company, employee)
	rule, e := service.CreateDistributionRuntimeRule(ctx, owner, db.CreateDistributionRuleParams{BindingID: binding.BindingID, BindingRevision: 1, AccountID: "123", GroupID: group.ID, PipelineID: "20", StatusID: "30", Active: true, KeepCurrent: true, ExecutionMode: "observe"})
	if e != nil {
		t.Fatal(e)
	}
	if rule.ExecutionMode != "observe" || rule.LiveStartedAt.Valid {
		t.Fatal("observation received live boundary", rule)
	}
	entry, event := uuid.New(), uuid.New()
	exec(`INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123','10',$1,$2,1,$3)`, company, binding.BindingID, entry)
	exec(`INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123','10',$3,1,1,'20','30',$4,'created_in_stage','checking',clock_timestamp(),clock_timestamp())`, entry, company, binding.BindingID, event)
	q := db.New(pool)
	for i := 0; i < 2; i++ {
		if e = q.ScheduleDistributionObservation(ctx, db.ScheduleDistributionObservationParams{ID: entry, EventID: event}); e != nil {
			t.Fatal(e)
		}
	}
	if found, e := service.ProcessDistributionObservation(ctx); !found || e != nil {
		t.Fatal(found, e)
	}
	if found, e := service.ProcessDistributionObservation(ctx); found || e != nil {
		t.Fatal("duplicate observation ran", found, e)
	}
	if found, e := service.ProcessDistributionQueue(ctx); found || e != nil {
		t.Fatal("observation became live work", found, e)
	}
	var observations, queues, groups, leads int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM distribution_observations),(SELECT count(*) FROM distribution_queue),(SELECT count(*) FROM distribution_group_claims),(SELECT count(*) FROM distribution_lead_claims)`).Scan(&observations, &queues, &groups, &leads); e != nil || observations != 1 || queues != 0 || groups != 0 || leads != 0 || crm.calls != 0 {
		t.Fatal("observation business mutation", observations, queues, groups, leads, crm.calls, e)
	}
	raw, e := service.DistributionObservations(ctx, owner, rule.ID, 25, 0)
	if e != nil {
		t.Fatal(e)
	}
	var page struct{ Items []DistributionObservation }
	if e = json.Unmarshal(raw, &page); e != nil || len(page.Items) != 1 || page.Items[0].DecisionKind != "assign" || page.Items[0].PlannedEmployeeID == nil || *page.Items[0].PlannedEmployeeID != employee {
		t.Fatal(string(raw), e)
	}
	if _, e = pool.Exec(ctx, `UPDATE distribution_observations SET payload='{}'`); e == nil {
		t.Fatal("observation mutable")
	}
	links.deny = true
	raw, e = service.DistributionObservations(ctx, owner, rule.ID, 25, 0)
	if e != nil {
		t.Fatal(e)
	}
	page.Items = nil
	_ = json.Unmarshal(raw, &page)
	if len(page.Items) != 0 {
		t.Fatal("private observation disclosed")
	}
	links.deny = false
	links.unavailable = true
	if _, e = service.DistributionObservations(ctx, owner, rule.ID, 25, 0); e == nil {
		t.Fatal("unknown permission became empty successful data")
	}
	links.unavailable = false
	if _, e = service.DistributionObservations(ctx, owner, rule.ID, 0, 0); e == nil {
		t.Fatal("unbounded observation list")
	}
	foreign := owner
	foreign.CompanyID = uuid.New()
	if _, e = service.DistributionObservations(ctx, foreign, rule.ID, 25, 0); e == nil {
		t.Fatal("foreign company read")
	}
	raw, e = service.DistributionRuntimeWrite(ctx, owner, "rule", rule.ID, []byte(`{"expectedRevision":1,"active":true,"keepCurrentResponsible":true,"executionMode":"live"}`))
	if e != nil {
		t.Fatal(e)
	}
	var live distributionRuleDTO
	_ = json.Unmarshal(raw, &live)
	if live.ExecutionEpoch != 2 || live.LiveStartedAt == nil {
		t.Fatal("missing fresh live boundary", string(raw))
	}
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatal("old observation automatically drained", n, e)
	}
	fresh := uuid.New()
	exec(`INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123','11',$1,$2,1,$3)`, company, binding.BindingID, fresh)
	exec(`INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123','11',$3,1,1,'20','30',$4,'created_in_stage','checking',clock_timestamp(),clock_timestamp())`, fresh, company, binding.BindingID, uuid.New())
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 1 {
		t.Fatal("fresh live entry not admitted", n, e)
	}
	if _, e = service.DistributionRuntimeWrite(ctx, owner, "rule", rule.ID, []byte(`{"expectedRevision":2,"active":true,"keepCurrentResponsible":true,"executionMode":"observe"}`)); e == nil {
		t.Fatal("mode flip ignored unsettled live queue")
	}

	// Lost admission response leaves the frozen identity durable without a
	// registered Core operation. A 404 alone cannot release either claim.
	crm.assignError = errors.New("lost admission response")
	if found, e := service.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatal("live fixture did not freeze a decision", found, e)
	}
	row, e := q.GetDistributionQueueByOperation(ctx, nullID(crm.assignment.Command.OperationID))
	if e != nil {
		t.Fatal(e)
	}
	frozenKey, frozenOperation := row.IdempotencyKey, row.OperationID
	var command corebridge.Assignment
	if e = json.Unmarshal(row.Command, &command); e != nil {
		t.Fatal(e)
	}
	crm.expireError = &corebridge.Error{Status: 404} // rolling deployment with older Core.
	service.now = func() time.Time { return command.Command.ValidUntil.Add(time.Second) }
	exec(`UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1`, row.ID)
	if found, e := service.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatal(found, e)
	}
	current, e := q.LockDistributionQueue(ctx, row.ID)
	if e != nil || current.Reason != "operation_unavailable" || current.State != "uncertain" || current.OperationID != frozenOperation || current.IdempotencyKey != frozenKey || crm.calls != 1 || crm.expireKey != frozenKey.UUID {
		t.Fatal("expired negative lookup changed frozen intent", current, e, crm.calls)
	}
	var held int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM distribution_lead_claims WHERE queue_id=$1`, row.ID).Scan(&held); e != nil || held != 1 {
		t.Fatal("404 released claim", held, e)
	}
	service.now = time.Now
	if _, e = service.DistributionRuntimeWrite(ctx, owner, "rule", rule.ID, []byte(`{"expectedRevision":2,"active":false,"keepCurrentResponsible":true}`)); e != nil {
		t.Fatal(e)
	}
	if found, e := service.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatal(found, e)
	}
	current, e = q.LockDistributionQueue(ctx, row.ID)
	if e != nil || current.Reason != "group_paused" || current.OperationID != frozenOperation || crm.calls != 1 {
		t.Fatal("paused admission sent or retired a frozen command", current, e)
	}
	// The same command can now be fenced at Core after expiry even while the
	// group is paused. The terminal no-attempt evidence retires both claims,
	// leaves the round-robin cursor untouched and allows future recalculation.
	crm.expireError = nil
	crm.expireResponseError = errors.New("lost expiry response after commit")
	service.now = func() time.Time { return command.Command.ValidUntil.Add(time.Second) }
	exec(`UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1`, row.ID)
	if found, e := service.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatal("expiry recovery", found, e)
	}
	current, e = q.LockDistributionQueue(ctx, row.ID)
	if e != nil || current.State != "uncertain" || current.OperationID != frozenOperation || current.IdempotencyKey != frozenKey {
		t.Fatal("lost expiry response retired unproven intent", current, e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM distribution_lead_claims WHERE queue_id=$1`, row.ID).Scan(&held); e != nil || held != 1 {
		t.Fatal("lost expiry response released claim", held, e)
	}
	// A new process discovers the committed terminal result via GET; it does
	// not need to repeat Assign or mint a replacement operation identity.
	service = &Service{pool: pool, now: service.now, distributionCore: links, deliveryCore: crm}
	exec(`UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1`, row.ID)
	if found, e := service.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatal("restart after lost expiry response", found, e)
	}
	current, e = q.LockDistributionQueue(ctx, row.ID)
	if e != nil || current.Reason != "decision_recalculation" || current.State != "waiting" || current.OperationID.Valid || crm.calls != 1 || crm.expireCalls != 2 || crm.expireKey != frozenKey.UUID {
		t.Fatal("durable expiry did not recalculate safely", current, e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM distribution_lead_claims WHERE queue_id=$1`, row.ID).Scan(&held); e != nil || held != 0 {
		t.Fatal("proven no-attempt retained lead claim", held, e)
	}
	claim, e := q.LockDistributionGroupClaim(ctx, db.LockDistributionGroupClaimParams{CompanyID: company, GroupID: group.ID})
	if e != nil || claim.QueueID.Valid || claim.CursorNext != 0 {
		t.Fatal("proven no-attempt retained group claim or advanced cursor", claim, e)
	}
	mirror, e := q.GetDistributionOperationMirror(ctx, frozenOperation.UUID)
	if e != nil || mirror.State != "rejected" || mirror.Unfinished {
		t.Fatal("expiry evidence was not persisted", mirror, e)
	}

}

func TestDistributionModeChangeSerializesAdmissionAndPendingObservationPoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, group, binding, rule, entry := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}})
	exec := func(q string, a ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, a...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO distribution_groups(id,company_id,name,member_ids) VALUES($1,$2,'Race',$3)`, group, company, []uuid.UUID{ownerID})
	exec(`INSERT INTO distribution_bindings(id,company_id,installation_id,integration_id,account_id,state,intent_id,initiated_by,initiator_snapshot,expires_at) VALUES($1,$2,$3,$4,'123','active',$5,$6,$6,clock_timestamp()+interval '15 minutes')`, binding, company, uuid.New(), uuid.New(), uuid.New(), ownerID)
	exec(`INSERT INTO distribution_binding_versions(company_id,binding_id,revision,installation_id,integration_id,account_id,state) SELECT company_id,id,1,installation_id,integration_id,account_id,'active' FROM distribution_bindings WHERE id=$1`, binding)
	exec(`INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id,active,first_activation_at) VALUES($1,$2,$3,1,'123','20','30',$4,true,clock_timestamp()-interval '1 second')`, rule, company, binding, group)
	exec(`INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123','10',$1,$2,1,$3)`, company, binding, entry)
	exec(`INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123','10',$3,1,1,'20','30',$4,'created_in_stage','checking',clock_timestamp(),clock_timestamp())`, entry, company, binding, uuid.New())
	// Configuration holds its rule lock before checking existing work. Admission
	// skips it, including when its statement initially observed the old live mode.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	q := db.New(tx)
	if _, e = q.LockDistributionAvailabilityVersion(ctx, company); e != nil {
		t.Fatal(e)
	}
	if _, e = q.LockDistributionRule(ctx, db.LockDistributionRuleParams{CompanyID: company, ID: rule}); e != nil {
		t.Fatal(e)
	}
	if n, e := db.New(pool).AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatal("admission crossed configuration lock", n, e)
	}
	if e = q.UpdateDistributionExecutionMode(ctx, db.UpdateDistributionExecutionModeParams{CompanyID: company, ID: rule, ExecutionMode: "observe"}); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if n, e := db.New(pool).AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatal("old live snapshot admitted after flip", n, e)
	}
	if e = db.New(pool).ScheduleDistributionObservation(ctx, db.ScheduleDistributionObservationParams{ID: entry, EventID: uuid.New()}); e != nil {
		t.Fatal(e)
	}
	if used, e := db.New(pool).DistributionRuleHasObservation(ctx, db.DistributionRuleHasObservationParams{CompanyID: company, RuleID: rule}); e != nil || !used {
		t.Fatal("pending observation point not protected", used, e)
	}
	// Inverse order: old admission owns the rule until it commits its queue.
	exec(`UPDATE distribution_rules SET execution_mode='live',live_started_at=first_activation_at WHERE id=$1`, rule)
	admit, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if n, e := db.New(admit).AdmitDistributionEntries(ctx); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	locked := make(chan bool, 1)
	go func() {
		gate, e := pool.Begin(ctx)
		if e != nil {
			locked <- false
			return
		}
		defer func() { _ = gate.Rollback(ctx) }()
		queries := db.New(gate)
		if _, e = queries.LockDistributionAvailabilityVersion(ctx, company); e != nil {
			locked <- false
			return
		}
		if _, e = queries.LockDistributionRule(ctx, db.LockDistributionRuleParams{CompanyID: company, ID: rule}); e != nil {
			locked <- false
			return
		}
		busy, e := queries.DistributionRuleUnsettled(ctx, db.DistributionRuleUnsettledParams{CompanyID: company, RuleID: rule})
		locked <- e == nil && busy
	}()
	select {
	case <-locked:
		t.Fatal("mode check overtook uncommitted admission")
	case <-time.After(50 * time.Millisecond):
	}
	if e = admit.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case busy := <-locked:
		if !busy {
			t.Fatal("configuration failed to see admitted live queue")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
