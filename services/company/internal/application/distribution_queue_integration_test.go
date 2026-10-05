//go:build integration

package application

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestDistributionQueueDurableOrderLeaseWakeHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employee := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employee, "employee", "active"}})
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	fake := &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}
	s := &Service{pool: pool, now: time.Now, distributionCore: fake}
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
	group, e := s.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Queue", MemberIDs: []uuid.UUID{employee}})
	if e != nil {
		t.Fatal(e)
	}
	rule := uuid.New()
	exec("INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id,active,first_activation_at) VALUES($1,$2,$3,1,'123','20','30',$4,true,now()-interval '1 second')", rule, company, b.BindingID, group.ID)
	add := func(lead string) uuid.UUID {
		id := uuid.New()
		exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)", lead, company, b.BindingID, id)
		exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking',clock_timestamp(),clock_timestamp())", id, company, lead, b.BindingID, uuid.New())
		return id
	}
	add("10")
	add("11")
	q := db.New(pool)
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 2 {
		t.Fatalf("admit %d %v", n, e)
	}
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatalf("duplicateadmit %d %v", n, e)
	}
	oldToken := nullID(uuid.New())
	first, e := q.ClaimDistributionQueue(ctx, oldToken)
	if e != nil || first.LeadID != "10" {
		t.Fatalf("first %+v %v", first, e)
	}
	if _, e = q.ClaimDistributionQueue(ctx, nullID(uuid.New())); !isNoRows(e) {
		t.Fatalf("later samegroup mustnot overtake leasedread: %v", e)
	}
	exec("UPDATE distribution_queue SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", first.ID)
	current, e := q.ClaimDistributionQueue(ctx, nullID(uuid.New()))
	if e != nil || current.ID != first.ID {
		t.Fatal(e)
	}
	if e = s.queueState(ctx, q, first, "failed", "stale_writer", time.Now(), false); e != nil {
		t.Fatal(e)
	}
	r, e := q.LockDistributionQueue(ctx, first.ID)
	if e != nil || r.State != "waiting" || r.LeaseToken != current.LeaseToken {
		t.Fatalf("stale overwrite %+v %v", r, e)
	}
	if e = s.queueState(ctx, q, current, "waiting", "night_wait", time.Now().Add(time.Hour), false); e != nil {
		t.Fatal(e)
	}
	var reason string
	if e = pool.QueryRow(ctx, "SELECT reason FROM distribution_queue_history WHERE queue_id=$1 ORDER BY id DESC LIMIT 1", first.ID).Scan(&reason); e != nil || reason != "night_wait" {
		t.Fatalf("atomichistory %s %v", reason, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE distribution_queue_history SET reason='rewrite' WHERE queue_id=$1", first.ID); e == nil {
		t.Fatal("history mutable")
	}
	// Schedule edits only increment a durable version, then bounded worker wakes.
	exec("INSERT INTO user_schedules(company_id,user_id,template) VALUES($1,$2,$3)", company, employee, []byte(`{"type":"week","days":[0,1,2,3,4,5,6],"start":"09:00","end":"18:00"}`))
	if _, e = q.ConsumeDistributionAvailabilityWake(ctx); e != nil {
		t.Fatal(e)
	}
	r, e = q.LockDistributionQueue(ctx, first.ID)
	if e != nil || r.NextAttemptAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("durablewake %+v %v", r, e)
	}
	// Pause retains entries and resuming does not reset first activation watermark.
	exec("UPDATE distribution_rules SET active=false,updated_at=now() WHERE id=$1", rule)
	add("12")
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 1 {
		t.Fatalf("pausedentry %d %v", n, e)
	}
	exec("UPDATE distribution_rules SET active=true,updated_at=now() WHERE id=$1", rule)
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatalf("resume duplicates %d %v", n, e)
	}
	// Global scope unique index rejects another company/group's active point.
	if _, e = pool.Exec(ctx, "INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id,active) VALUES($1,$2,$3,1,'123','20','30',$4,true)", uuid.New(), company, b.BindingID, group.ID); !isUniqueViolation(e) {
		t.Fatalf("point conflict %v", e)
	}
	// A newer active rule deterministically owns new entries; old paused queues
	// retain their original immutable rule identity.
	exec("UPDATE distribution_rules SET active=false WHERE id=$1", rule)
	replacementGroup, e := s.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Replacement point owner", MemberIDs: []uuid.UUID{employee}})
	if e != nil {
		t.Fatal(e)
	}
	nextRule := uuid.New()
	exec("INSERT INTO distribution_rules(id,company_id,binding_id,binding_revision,account_id,pipeline_id,status_id,group_id,active,first_activation_at) VALUES($1,$2,$3,1,'123','20','30',$4,true,clock_timestamp())", nextRule, company, b.BindingID, replacementGroup.ID)
	fresh := add("13")
	if _, e = q.AdmitDistributionEntries(ctx); e != nil {
		t.Fatal(e)
	}
	var selectedRule uuid.UUID
	if e = pool.QueryRow(ctx, "SELECT rule_id FROM distribution_queue WHERE entry_id=$1", fresh).Scan(&selectedRule); e != nil || selectedRule != nextRule {
		t.Fatalf("ruleownership%s %v", selectedRule, e)
	}
	// TeamOS owner status does not imply CRM permission. Opaque history is safe
	// even if internal frozen command/result rows acquire additional fields.
	raw, e := s.DistributionRuntimeRead(ctx, owner, "queue", uuid.Nil, 50, 0)
	if e != nil {
		t.Fatal(e)
	}
	var page struct {
		Items []struct {
			LeadID *string `json:"leadId"`
		}
	}
	if e = json.Unmarshal(raw, &page); e != nil {
		t.Fatal(e)
	}
	for _, item := range page.Items {
		if item.LeadID != nil {
			t.Fatal("unmapped owner received CRMlead")
		}
	}
	raw, e = s.DistributionRuntimeRead(ctx, owner, "history", first.ID, 50, 0)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte("stale_writer")) {
		t.Fatal("stale history emitted")
	}
	var h struct {
		Items []struct {
			Payload map[string]any `json:"payload"`
		}
	}
	if e = json.Unmarshal(raw, &h); e != nil {
		t.Fatal(e)
	}
	for _, item := range h.Items {
		if len(item.Payload) != 0 {
			t.Fatal("private history leaked")
		}
	}

	// Known historical ingress/occurrence is excluded even when projection was
	// processed after first activation; timestamps never create entry identity.
	oldEntry := add("14")
	exec("UPDATE distribution_observed_entries SET source_received_at=clock_timestamp(),source_occurred_at=clock_timestamp()-interval '1 day' WHERE id=$1", oldEntry)
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatalf("historiccutoff %d %v", n, e)
	}
	nullEntry := add("15")
	exec("UPDATE distribution_observed_entries SET source_received_at=NULL,source_occurred_at=NULL WHERE id=$1", nullEntry)
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 0 {
		t.Fatalf("rollingunknowncutoff %d %v", n, e)
	}
	// A dirty company with more than one page preserves its wake until all rows
	// have received the current revision, including after a worker restart.
	for n := 0; n < 205; n++ {
		add(strconv.Itoa(1000 + n))
	}
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 200 {
		t.Fatalf("bounded admit%d %v", n, e)
	}
	if n, e := q.AdmitDistributionEntries(ctx); e != nil || n != 5 {
		t.Fatalf("admitrest%d %v", n, e)
	}
	exec("UPDATE user_schedules SET updated_at=clock_timestamp() WHERE user_id=$1", employee)
	if _, e = q.ConsumeDistributionAvailabilityWake(ctx); e != nil {
		t.Fatal(e)
	}
	var pending bool
	if e = pool.QueryRow(ctx, "SELECT revision>processed_revision FROM distribution_availability_versions WHERE company_id=$1", company).Scan(&pending); e != nil || !pending {
		t.Fatalf("lostpartialwake %v %v", pending, e)
	}
	if _, e = db.New(pool).ConsumeDistributionAvailabilityWake(ctx); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT revision>processed_revision FROM distribution_availability_versions WHERE company_id=$1", company).Scan(&pending); e != nil || pending {
		t.Fatalf("unfinishedwake %v %v", pending, e)
	}
	// Schedule mutation can hold its row while waiting for the company revision.
	// Validation reads committed inputs without acquiring that row lock, avoiding
	// the inverse schedule→version / version→schedule cycle.
	transaction, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	locked := db.New(transaction)
	if _, e = locked.LockDistributionAvailabilityVersion(ctx, company); e != nil {
		t.Fatal(e)
	}
	writer := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, "UPDATE user_schedules SET updated_at=clock_timestamp() WHERE user_id=$1", employee)
		writer <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if _, e = locked.DistributionMemberInputs(ctx, db.DistributionMemberInputsParams{CompanyID: company, BindingID: b.BindingID, Column3: []uuid.UUID{employee}}); e != nil {
		t.Fatal(e)
	}
	if e = transaction.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-writer:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("schedule/revision deadlock")
	}

}
