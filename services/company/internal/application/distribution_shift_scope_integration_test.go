//go:build integration

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// shiftCore returns a fixed responsible and is used to drive the live queue with
// a controlled clock. Everything else is the night-test fake.
type shiftCore struct {
	*nightCore
	responsible string
}

func (f *shiftCore) ReadLead(_ context.Context, s corebridge.Scope, id string) (corebridge.LeadObservation, error) {
	now := f.now()
	return corebridge.LeadObservation{
		Scope: s, LeadID: id, ObservationRevision: 1, ObservedAt: now,
		Snapshot: &corebridge.LeadSnapshot{LeadID: id, PipelineID: "20", StatusID: "30", ResponsibleUserID: f.responsible, ObservedAt: now},
	}, nil
}

type shiftMember struct {
	id    uuid.UUID
	crm   string
	days  []int
	start string
	end   string
}

type shiftFixture struct {
	ctx     context.Context
	svc     *Service
	core    *shiftCore
	company uuid.UUID
	owner   Actor
	binding uuid.UUID
	group   uuid.UUID
	rule    uuid.UUID
	entry   uuid.UUID
	exec    func(sql string, args ...any)
}

func weekdayIndex(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

func newShiftFixture(t *testing.T, ctx context.Context, clock *time.Time, members []shiftMember, responsibleCRM, lead string) *shiftFixture {
	t.Helper()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID := uuid.New(), uuid.New()
	users := []accessTestUser{{ownerID, "owner", "active"}}
	for _, m := range members {
		users = append(users, accessTestUser{m.id, "employee", "active"})
	}
	seedAccessCompany(t, ctx, pool, company, ownerID, users)
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	nowf := func() time.Time { return *clock }
	fake := &shiftCore{
		nightCore:   &nightCore{fakeDistributionCore: &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}, now: nowf},
		responsible: responsibleCRM,
	}
	userRefs := make([]corebridge.User, 0, len(members))
	for _, m := range members {
		userRefs = append(userRefs, corebridge.User{ID: m.crm, IsActive: true})
	}
	fake.refs = corebridge.References{Timezone: "UTC", TimezoneFetchedAt: *clock,
		State: "fresh", FetchedAt: *clock, FreshUntil: clock.Add(365 * 24 * time.Hour),
		Users: userRefs, Pipelines: []corebridge.Pipeline{{ID: "20", Statuses: []corebridge.Status{{ID: "30"}}}},
	}
	svc := &Service{pool: pool, now: nowf, distributionCore: fake.fakeDistributionCore, deliveryCore: fake}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	conn, e := svc.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	exec("UPDATE distribution_bindings SET mapping_revision=1,mapping_ack_revision=1 WHERE id=$1", conn.BindingID)
	for _, m := range members {
		exec("INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,$5,'verified',now())", uuid.New(), company, conn.BindingID, m.id, m.crm)
	}
	memberIDs := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		memberIDs = append(memberIDs, m.id)
	}
	group, e := svc.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Shift scope", MemberIDs: memberIDs})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.SaveDistributionTimezone(ctx, owner, "UTC"); e != nil {
		t.Fatal(e)
	}
	for _, m := range members {
		days, _ := json.Marshal(m.days)
		exec("INSERT INTO user_schedules(company_id,user_id,template) VALUES($1,$2,$3)", company, m.id, []byte(fmt.Sprintf(`{"type":"week","days":%s,"start":%q,"end":%q}`, days, m.start, m.end)))
	}
	rule, e := svc.CreateDistributionRuntimeRule(ctx, owner, db.CreateDistributionRuleParams{BindingID: conn.BindingID, BindingRevision: 1, AccountID: "123", GroupID: group.ID, PipelineID: "20", StatusID: "30", Active: true, KeepCurrent: true})
	if e != nil {
		t.Fatal(e)
	}
	entry := uuid.New()
	exec("INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)", lead, company, conn.BindingID, entry)
	exec("INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at,event_kind) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking',clock_timestamp(),clock_timestamp(),'lead.created')", entry, company, lead, conn.BindingID, uuid.New())
	return &shiftFixture{ctx: ctx, svc: svc, core: fake, company: company, owner: owner, binding: conn.BindingID, group: group.ID, rule: rule.ID, entry: entry, exec: exec}
}

func (f *shiftFixture) queueRow(t *testing.T) db.DistributionQueue {
	t.Helper()
	rows, e := db.New(f.svc.pool).ListDistributionQueue(f.ctx, db.ListDistributionQueueParams{CompanyID: f.company, Limit: 10})
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one queue row, got %d", len(rows))
	}
	return rows[0]
}

func TestDistributionShiftScopeAssignsOnShiftMember(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 7, 10, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	viktor, anna := uuid.New(), uuid.New()
	// 07:10: Виктор's shift starts 10:00 (off-shift), Анна works 06:00-09:00.
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{
		{viktor, "2", []int{today}, "10:00", "18:00"},
		{anna, "3", []int{today}, "06:00", "09:00"},
	}, "2", "10")
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	if fx.core.calls != 1 {
		t.Fatalf("expected one assignment call, got %d", fx.core.calls)
	}
	if got := fx.core.assignment.Command.TargetResponsibleUserID; got != "3" {
		t.Fatalf("expected in-shift Anna (3), got %q", got)
	}
}

func TestDistributionShiftScopeChoosesOnlyTodayShift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	today, tomorrow := weekdayIndex(clock), weekdayIndex(clock.Add(24*time.Hour))
	boris, anna, viktor := uuid.New(), uuid.New(), uuid.New()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{
		{boris, "2", []int{today}, "09:00", "18:00"},
		{anna, "3", []int{tomorrow}, "09:00", "18:00"},
		{viktor, "4", []int{tomorrow}, "09:00", "18:00"},
	}, "3", "11")
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	if fx.core.calls != 1 || fx.core.assignment.Command.TargetResponsibleUserID != "2" {
		t.Fatalf("expected only Boris (2), calls=%d target=%q", fx.core.calls, fx.core.assignment.Command.TargetResponsibleUserID)
	}
}

func TestDistributionShiftScopeWaitsThenAssignsAtShiftStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 7, 10, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	worker := uuid.New()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{
		{worker, "2", []int{today}, "10:00", "18:00"},
	}, "2", "12")
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	row := fx.queueRow(t)
	shiftStart := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	if row.State != "waiting" || row.Reason != "no_available_members" || !row.NextAttemptAt.Equal(shiftStart) || fx.core.calls != 0 {
		t.Fatalf("expected waiting until shift start, got %+v calls=%d", row, fx.core.calls)
	}
	// No new CRM event is delivered: only time passes to the shift start.
	clock = shiftStart
	fx.exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1", row.ID)
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("restart %v %v", found, e)
	}
	if fx.core.calls != 1 || fx.core.assignment.Command.TargetResponsibleUserID != "2" {
		t.Fatalf("expected automatic assignment at shift start, calls=%d target=%q", fx.core.calls, fx.core.assignment.Command.TargetResponsibleUserID)
	}
	fx.queueRow(t) // still exactly one entry, no new CRM event duplicated it
}

func TestDistributionShiftScopeRejectsStaleDecisionBeforeWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 7, 10, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	worker := uuid.New()
	// Initial responsible "9" is not a group member, so the plan is a real assign.
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{
		{worker, "2", []int{today}, "07:00", "08:00"},
	}, "9", "13")
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	a := fx.core.assignment
	if a.Command.DecisionKind != "assign" || a.Command.TargetResponsibleUserID != "2" {
		t.Fatalf("expected assign to on-shift member, got kind=%q target=%q", a.Command.DecisionKind, a.Command.TargetResponsibleUserID)
	}
	// Shift is over before the Core guard runs: the frozen decision must be denied.
	clock = time.Date(2026, 10, 6, 8, 30, 0, 0, time.UTC)
	deny, e := fx.svc.ValidateDistributionDecision(ctx, DistributionDecisionValidationInput{
		Scope: a.Scope, Actor: DistributionDecisionActor{Kind: "system"},
		OperationID: a.Command.OperationID, DecisionID: a.Command.DecisionID, EpisodeID: fx.entry,
		RuleID: fx.rule, GroupID: fx.group, RuleRevision: a.Command.RuleRevision,
		AvailabilityRevision: a.Command.AvailabilityRevision, ClaimRevision: a.Command.ClaimRevision,
		WorkerFence: 1, TargetEmployeeID: worker, TargetResponsibleUserID: a.Command.TargetResponsibleUserID, LeadID: "13",
		DecisionKind: a.Command.DecisionKind, ValidUntil: a.Command.ValidUntil,
	})
	if e != nil {
		t.Fatal(e)
	}
	if deny.Allowed {
		t.Fatalf("Core guard must reject a decision whose shift ended, got %+v", deny)
	}
	// The Core guard refuses this before any CRM write: expired window, gone
	// recipient or a shifted availability graph are all valid refusals.
	switch deny.Reason {
	case "decision_expired", "recipient_unavailable", "availability_stale":
	default:
		t.Fatalf("unexpected guard reason %q", deny.Reason)
	}
	if fx.core.calls != 1 {
		t.Fatalf("guard rejection must not write again, calls=%d", fx.core.calls)
	}
}

func TestDistributionShiftScopeConfirmedLeadStaysSettled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 7, 10, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	worker := uuid.New()
	// Non-member responsible => an actual assign decision to the on-shift member.
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{
		{worker, "2", []int{today}, "07:00", "18:00"},
	}, "9", "14")
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	a := fx.core.assignment
	snapshot := a.Command.ExpectedSnapshot
	snapshot.ResponsibleUserID = "2"
	outcome := "assigned"
	fx.core.operation.State = "succeeded"
	fx.core.operation.ExternalEffectState = "settled"
	fx.core.operation.ResultVersion = 2
	fx.core.operation.Outcome = &outcome
	fx.core.operation.ConfirmedSnapshot = &snapshot
	fx.core.operation.ResolutionEvidence = corebridge.OperationEvidence{Kind: "response_and_observation", ObservedAt: clock, GuardReleasable: true}
	fx.exec("UPDATE distribution_queue SET next_attempt_at=clock_timestamp() WHERE id=$1", fx.queueRow(t).ID)
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row := fx.queueRow(t)
	if row.State != "confirmed" || !row.Settled {
		t.Fatalf("expected confirmed, got %+v", row)
	}
	calls := fx.core.calls
	// Past the shift end: a confirmed assignment must never be re-queued/reassigned.
	clock = time.Date(2026, 10, 6, 19, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
			t.Fatal(e)
		}
	}
	after := fx.queueRow(t)
	if after.State != "confirmed" || !after.Settled || after.ID != row.ID {
		t.Fatalf("confirmed assignment changed after shift boundary: %+v", after)
	}
	if fx.core.calls != calls {
		t.Fatalf("confirmed assignment was reassigned, calls %d->%d", calls, fx.core.calls)
	}
}
