//go:build integration

package application

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type nightCore struct {
	*fakeDistributionCore
	now                 func() time.Time
	assignment          corebridge.Assignment
	operation           corebridge.Operation
	calls               int
	expireCalls         int
	expireError         error
	expireResponseError error
	expireKey           uuid.UUID
	assignError         error
}

func (f *nightCore) ReadLead(_ context.Context, s corebridge.Scope, id string) (corebridge.LeadObservation, error) {
	now := f.now()
	return corebridge.LeadObservation{Scope: s, LeadID: id, ObservationRevision: 1, ObservedAt: now, Snapshot: &corebridge.LeadSnapshot{LeadID: id, PipelineID: "20", StatusID: "30", ResponsibleUserID: "1", ObservedAt: now}}, nil
}
func (f *nightCore) Operation(_ context.Context, _ corebridge.Scope, id uuid.UUID) (corebridge.Operation, error) {
	if f.operation.OperationID != id {
		return corebridge.Operation{}, &corebridge.Error{Status: 404}
	}
	return f.operation, nil
}
func (f *nightCore) Assign(_ context.Context, a corebridge.Assignment, _ uuid.UUID) (corebridge.AssignmentReceipt, error) {
	f.calls++
	f.assignment = a
	if f.assignError != nil {
		return corebridge.AssignmentReceipt{}, f.assignError
	}
	f.operation = nightOperation(a, f.now())
	return corebridge.AssignmentReceipt{OperationID: a.Command.OperationID, AcceptedAt: f.operation.AcceptedAt, State: "queued", ResultVersion: 1}, nil
}

func nightOperation(a corebridge.Assignment, now time.Time) corebridge.Operation {
	c := a.Command
	return corebridge.Operation{LeadID: c.ExpectedSnapshot.LeadID, OperationID: c.OperationID, Scope: a.Scope, EpisodeID: c.EpisodeID, DecisionID: c.DecisionID, RuleID: c.RuleID, GroupID: c.GroupID, RuleRevision: c.RuleRevision, AvailabilityRevision: c.AvailabilityRevision, ClaimRevision: c.ClaimRevision, EventID: a.EventID, CorrelationID: a.CorrelationID, TargetResponsibleUserID: c.TargetResponsibleUserID, State: "queued", ExternalEffectState: "no_attempt", ResultVersion: 1, ResolutionEvidence: corebridge.OperationEvidence{Kind: "no_request_sent", ObservedAt: now}, AcceptedAt: now, UpdatedAt: now}
}

func (f *nightCore) ExpireAssignment(_ context.Context, a corebridge.Assignment, key uuid.UUID) (corebridge.Operation, error) {
	f.expireCalls++
	f.expireKey = key
	if f.expireError != nil {
		return corebridge.Operation{}, f.expireError
	}
	if f.operation.OperationID == a.Command.OperationID {
		return f.operation, nil
	}
	f.operation = nightOperation(a, f.now())
	f.operation.State = "rejected"
	rejected := "rejected"
	f.operation.Outcome = &rejected
	f.operation.Error = &corebridge.OperationError{Code: "decision_expired", Message: "Срок решения истёк", Terminal: true}
	f.operation.ResolutionEvidence.GuardReleasable = true
	if f.expireResponseError != nil {
		return corebridge.Operation{}, f.expireResponseError
	}
	return f.operation, nil
}
func (f *nightCore) CancelAssignment(_ context.Context, _ corebridge.Scope, id, key uuid.UUID, _ int64) (corebridge.Operation, error) {
	return f.operation, nil
}
func (f *nightCore) ReconcileAssignment(_ context.Context, _ corebridge.Scope, id, key uuid.UUID, _ int64) (corebridge.Operation, error) {
	return f.operation, nil
}
func (f *nightCore) ControlAssignment(_ context.Context, _ corebridge.Scope, id, key uuid.UUID, action string, _ json.RawMessage) (corebridge.Operation, error) {
	return f.operation, nil
}
func TestDistributionNightWaitSurvivesRestartAndConfirms(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employee := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employee, "employee", "active"}})
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	clock := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	fake := &nightCore{fakeDistributionCore: &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}, now: func() time.Time { return clock }}
	fake.refs = corebridge.References{State: "fresh", FetchedAt: clock, FreshUntil: clock.Add(24 * time.Hour), Users: []corebridge.User{{ID: "2", IsActive: true}}, Pipelines: []corebridge.Pipeline{{ID: "20", Statuses: []corebridge.Status{{ID: "30"}}}}}
	svc := &Service{pool: pool, now: fake.now, distributionCore: fake.fakeDistributionCore, deliveryCore: fake}
	binding, e := svc.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("UPDATE distribution_bindings SET mapping_revision=1,mapping_ack_revision=1 WHERE id=$1", binding.BindingID)
	exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'2','verified',now())", uuid.New(), company, binding.BindingID, employee)
	group, e := svc.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Night wait", MemberIDs: []uuid.UUID{employee}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.SaveDistributionTimezone(ctx, owner, "UTC"); e != nil {
		t.Fatal(e)
	}
	exec("INSERT INTO user_schedules(company_id,user_id,template) VALUES($1,$2,$3)", company, employee, []byte(`{"type":"week","days":[0,1,2,3,4,5,6],"start":"09:00","end":"18:00"}`))
	rule, e := svc.CreateDistributionRuntimeRule(ctx, owner, db.CreateDistributionRuleParams{BindingID: binding.BindingID, BindingRevision: 1, AccountID: "123", GroupID: group.ID, PipelineID: "20", StatusID: "30", Active: true, KeepCurrent: true})
	if e != nil {
		t.Fatal(e)
	}
	entry := uuid.New()
	exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123','10',$1,$2,1,$3)", company, binding.BindingID, entry)
	exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123','10',$3,1,1,'20','30',$4,'created_in_stage','checking',clock_timestamp(),clock_timestamp())", entry, company, binding.BindingID, uuid.New())
	if found, e := svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("night %v %v", found, e)
	}
	rows, e := db.New(pool).ListDistributionQueue(ctx, db.ListDistributionQueueParams{CompanyID: company, Limit: 10})
	if e != nil || len(rows) != 1 {
		t.Fatal(e)
	}
	row := rows[0]
	expected := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	if row.Reason != "no_available_members" || !row.NextAttemptAt.Equal(expected) || fake.calls != 0 {
		t.Fatalf("persistnight %+v calls%d", row, fake.calls)
	}
	// Advance only the test clock; PostgreSQL due time is explicitly made ready.
	clock = expected
	exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1", row.ID)
	restart := &Service{pool: pool, now: fake.now, distributionCore: fake.fakeDistributionCore, deliveryCore: fake}
	if found, e := restart.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("restart %v %v", found, e)
	}
	if fake.calls != 1 {
		t.Fatalf("assignment calls%d", fake.calls)
	}
	row, e = db.New(pool).LockDistributionQueue(ctx, row.ID)
	if e != nil {
		t.Fatal(e)
	}
	firstControl, e := restart.frozenRuntimeControl(ctx, row, row.OperationID.UUID, "reconcile", 1)
	if e != nil {
		t.Fatal(e)
	}
	reloaded := &Service{pool: pool, now: fake.now}
	sameControl, e := reloaded.frozenRuntimeControl(ctx, row, row.OperationID.UUID, "reconcile", 1)
	if e != nil || sameControl.Key != firstControl.Key || !bytes.Equal(sameControl.Payload, firstControl.Payload) {
		t.Fatalf("lost control ACK exactreplay %v", e)
	}
	nextControl, e := reloaded.frozenRuntimeControl(ctx, row, row.OperationID.UUID, "reconcile", 2)
	if e != nil || nextControl.Key == firstControl.Key {
		t.Fatalf("versionCASretrykey %v", e)
	}
	if _, e = pool.Exec(ctx, "UPDATE distribution_control_requests SET payload='{}' WHERE key=$1", firstControl.Key); e == nil {
		t.Fatal("frozencontrol mutable")
	}
	a := fake.assignment
	grant, e := restart.ValidateDistributionDecision(ctx, DistributionDecisionValidationInput{Scope: a.Scope, Actor: DistributionDecisionActor{Kind: "system"}, OperationID: a.Command.OperationID, DecisionID: a.Command.DecisionID, EpisodeID: entry, RuleID: rule.ID, GroupID: group.ID, RuleRevision: a.Command.RuleRevision, AvailabilityRevision: a.Command.AvailabilityRevision, ClaimRevision: a.Command.ClaimRevision, WorkerFence: 1, TargetEmployeeID: employee, TargetResponsibleUserID: "2", LeadID: "10", DecisionKind: "assign", ValidUntil: a.Command.ValidUntil})
	if e != nil || !grant.Allowed {
		t.Fatalf("registry %+v %v", grant, e)
	}
	snapshot := a.Command.ExpectedSnapshot
	snapshot.ResponsibleUserID = "2"
	fake.operation.State = "succeeded"
	fake.operation.ExternalEffectState = "settled"
	fake.operation.ResultVersion = 2
	outcome := "assigned"
	fake.operation.Outcome = &outcome
	fake.operation.ConfirmedSnapshot = &snapshot
	fake.operation.ResolutionEvidence = corebridge.OperationEvidence{Kind: "response_and_observation", ObservedAt: clock, GuardReleasable: true}
	exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1", row.ID)
	if _, e = restart.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row, e = db.New(pool).LockDistributionQueue(ctx, row.ID)
	if e != nil || row.State != "confirmed" || !row.Settled || fake.calls != 1 {
		t.Fatalf("confirm %+v %v calls%d", row, e, fake.calls)
	}
	// A permanent pre-send rejection releases claims without advancing the cursor
	// or starving the next ready entry in the same group.
	addNext := func(lead string) {
		id := uuid.New()
		exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)", lead, company, binding.BindingID, id)
		exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking',clock_timestamp(),clock_timestamp())", id, company, lead, binding.BindingID, uuid.New())
	}
	addNext("11")
	if _, e = restart.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	fake.operation.State = "rejected"
	fake.operation.ExternalEffectState = "no_attempt"
	fake.operation.ResultVersion = 2
	rejected := "rejected"
	fake.operation.Outcome = &rejected
	fake.operation.ConfirmedSnapshot = nil
	fake.operation.Error = &corebridge.OperationError{Code: "permission_denied", Message: "Нет прав", Terminal: true}
	fake.operation.ResolutionEvidence = corebridge.OperationEvidence{Kind: "no_request_sent", ObservedAt: clock, GuardReleasable: true}
	exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE lead_id='11'")
	if _, e = restart.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	var failedState string
	if e = pool.QueryRow(ctx, "SELECT state FROM distribution_queue WHERE lead_id='11'").Scan(&failedState); e != nil || failedState != "failed" {
		t.Fatalf("permanent failure%s %v", failedState, e)
	}
	addNext("12")
	if _, e = restart.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	if fake.calls != 3 {
		t.Fatalf("permanent head starves next calls%d", fake.calls)
	}

}
