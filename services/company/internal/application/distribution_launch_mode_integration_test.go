//go:build integration

package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestDistributionDigitalPipelineTriggerCreatesTrustedEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company := uuid.New()
	owner := Actor{CompanyID: company, UserID: uuid.New(), Role: "owner"}
	seedAccessCompany(t, ctx, pool, company, owner.UserID, []accessTestUser{{owner.UserID, "owner", "active"}})
	links := &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}
	fake := &deliveryFake{}
	svc := &Service{pool: pool, now: time.Now, distributionCore: links, deliveryCore: fake}
	connection, e := svc.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "test"})
	if e != nil {
		t.Fatal(e)
	}
	scope := corebridge.Scope{CompanyID: company, BindingID: connection.BindingID, BindingRevision: 1, InstallationID: connection.InstallationID, IntegrationID: connection.IntegrationID, AccountID: "123"}
	now := time.Now().UTC()
	p, st := "20", "30"
	groupID := uuid.New()
	event := DistributionEventEnvelope{SchemaVersion: 1, MessageID: uuid.New(), EventID: uuid.New(), Scope: scope, ReceivedAt: now, EmittedAt: now, SourceOccurredAt: &now, CorrelationID: uuid.New(), Event: DistributionCRMEvent{Kind: "lead.digital_pipeline_trigger", LeadID: "30", ObservationRevision: 1, SourceEvidence: &DistributionSourceEvidence{PipelineID: &p, StatusID: &st}, TriggerEvidence: json.RawMessage(`{"event":{"type":15,"type_code":"lead_appeared_in_status"}}`), TriggerGroupID: &groupID}}
	fake.observation = corebridge.LeadObservation{Snapshot: &corebridge.LeadSnapshot{LeadID: "30", PipelineID: p, StatusID: st, ResponsibleUserID: "1", ObservedAt: now}}
	if _, e = svc.ReceiveDistributionEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if found, e := svc.ProcessDistributionDelivery(ctx); !found || e != nil {
		t.Fatalf("deliver %v %v", found, e)
	}
	var evidence, eventKind string
	var triggerGroup uuid.NullUUID
	var triggerEvidence []byte
	if e = pool.QueryRow(ctx, "SELECT evidence,event_kind,trigger_group_id,trigger_evidence FROM distribution_observed_entries WHERE account_id='123' AND lead_id='30'").Scan(&evidence, &eventKind, &triggerGroup, &triggerEvidence); e != nil {
		t.Fatal(e)
	}
	if evidence != "digital_pipeline_trigger" || eventKind != "lead.digital_pipeline_trigger" || !triggerGroup.Valid || triggerGroup.UUID != groupID || len(triggerEvidence) == 0 {
		t.Fatalf("trusted DP evidence not persisted: %s %s %v %d", evidence, eventKind, triggerGroup, len(triggerEvidence))
	}
}

func TestDistributionLaunchModeCreationAdmitsAnyStage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	worker := uuid.New()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{worker, "2", []int{today}, "09:00", "18:00"}}, "9", "20")
	// creation-mode: the lead is created inside the pipeline, on another stage.
	fx.exec("UPDATE distribution_rules SET source='creation' WHERE id=$1", fx.rule)
	fx.exec("UPDATE distribution_observed_entries SET status_id='99', event_kind='lead.created' WHERE id=$1", fx.entry)
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	if fx.core.calls != 1 || fx.core.assignment.Command.TargetResponsibleUserID != "2" {
		t.Fatalf("creation-mode must admit any stage inside the pipeline, calls=%d", fx.core.calls)
	}
}

func TestDistributionLaunchModeTriggerNeedsGroupScopedEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	worker := uuid.New()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{worker, "2", []int{today}, "09:00", "18:00"}}, "9", "21")
	fx.exec("UPDATE distribution_rules SET source='digital_pipeline' WHERE id=$1", fx.rule)
	queueRows := func() int {
		rows, e := db.New(fx.svc.pool).ListDistributionQueue(ctx, db.ListDistributionQueueParams{CompanyID: fx.company, Limit: 10})
		if e != nil {
			t.Fatal(e)
		}
		return len(rows)
	}
	// 1) Ordinary webhook entry must NOT be admitted in trigger-mode.
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	if queueRows() != 0 || fx.core.calls != 0 {
		t.Fatalf("normal webhook must not admit trigger-mode, rows=%d calls=%d", queueRows(), fx.core.calls)
	}
	// 2) A trigger bound to another group must be rejected.
	fx.exec("UPDATE distribution_observed_entries SET evidence='digital_pipeline_trigger', trigger_evidence='{}'::jsonb, trigger_group_id=$1 WHERE id=$2", uuid.New(), fx.entry)
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	if queueRows() != 0 || fx.core.calls != 0 {
		t.Fatalf("wrong-group trigger must be rejected, rows=%d calls=%d", queueRows(), fx.core.calls)
	}
	// 3) Group-scoped trusted evidence is admitted.
	fx.exec("UPDATE distribution_observed_entries SET trigger_group_id=$1 WHERE id=$2", fx.group, fx.entry)
	if found, e := fx.svc.ProcessDistributionQueue(ctx); !found || e != nil {
		t.Fatalf("queue %v %v", found, e)
	}
	if fx.core.calls != 1 {
		t.Fatalf("trusted group-scoped trigger must admit, calls=%d", fx.core.calls)
	}
}

func TestDistributionRuleFullUpdateAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	today := weekdayIndex(clock)
	worker := uuid.New()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{worker, "2", []int{today}, "09:00", "18:00"}}, "9", "22")
	q := db.New(fx.svc.pool)
	before, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: fx.company, ID: fx.rule})
	if e != nil {
		t.Fatal(e)
	}
	// One statement persists source + pipeline/stage + mode/active together.
	updated, e := q.UpdateDistributionRulePoint(ctx, db.UpdateDistributionRulePointParams{CompanyID: fx.company, ID: fx.rule, PipelineID: "20", StatusID: "99", Active: true, KeepCurrent: false, Revision: before.Revision, Source: "creation"})
	if e != nil {
		t.Fatal(e)
	}
	if updated.Source != "creation" || updated.StatusID != "99" || updated.KeepCurrent || !updated.Active {
		t.Fatalf("full update must persist all fields atomically: %+v", updated)
	}
	if updated.ExecutionEpoch != before.ExecutionEpoch+1 {
		t.Fatalf("source change must bump execution epoch (cancel guard): %d -> %d", before.ExecutionEpoch, updated.ExecutionEpoch)
	}
	if !updated.LiveStartedAt.Valid || !updated.FirstActivationAt.Valid {
		t.Fatalf("source change must reset the live floor: %+v", updated)
	}
	// Stale revision is all-or-nothing: nothing is written.
	if _, e := q.UpdateDistributionRulePoint(ctx, db.UpdateDistributionRulePointParams{CompanyID: fx.company, ID: fx.rule, PipelineID: "20", StatusID: "77", Active: false, KeepCurrent: true, Revision: before.Revision, Source: "legacy_stage"}); !isNoRows(e) {
		t.Fatalf("stale revision must reject the whole update, got %v", e)
	}
	after, e := q.GetDistributionRule(ctx, db.GetDistributionRuleParams{CompanyID: fx.company, ID: fx.rule})
	if e != nil {
		t.Fatal(e)
	}
	if after.StatusID != "99" || after.Source != "creation" || !after.Active {
		t.Fatalf("rejected update must not partially apply: %+v", after)
	}
	// The entry seen before the source change is below the new live floor.
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	rows, e := q.ListDistributionQueue(ctx, db.ListDistributionQueueParams{CompanyID: fx.company, Limit: 10})
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 0 {
		t.Fatalf("pre-change entry must not be admitted after a mode change, rows=%d", len(rows))
	}
}

func TestDistributionDigitalPipelineUsesTrustedPointAndCancelsStageExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{uuid.New(), "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "55")
	fx.exec("UPDATE distribution_rules SET source='digital_pipeline',pipeline_id='',status_id='' WHERE id=$1", fx.rule)
	fx.exec("UPDATE distribution_observed_entries SET evidence='digital_pipeline_trigger',event_kind='lead.digital_pipeline_trigger',trigger_evidence='{}',trigger_group_id=$1,status_id='99' WHERE id=$2", fx.group, fx.entry)
	if _, e := fx.svc.ProcessDistributionQueue(ctx); e != nil {
		t.Fatal(e)
	}
	row := fx.queueRow(t)
	if row.State != "cancelled" || !row.Settled || row.Reason != "observed_stage_exit" || fx.core.calls != 0 {
		t.Fatalf("stage exit assigned %+v calls%d", row, fx.core.calls)
	}
}

func TestDistributionLaunchSettingsClearHiddenPointAndAllowSeparateTriggerGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	employee := uuid.New()
	fx := newShiftFixture(t, ctx, &clock, []shiftMember{{employee, "2", []int{weekdayIndex(clock)}, "09:00", "18:00"}}, "9", "57")
	update := func(source string, revision int64) distributionRuleDTO {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"expectedRevision": revision, "active": true, "keepCurrentResponsible": true, "source": source})
		response, e := fx.svc.DistributionRuntimeWrite(ctx, fx.owner, "rule", fx.rule, raw)
		if e != nil {
			t.Fatal(e)
		}
		var rule distributionRuleDTO
		if e = json.Unmarshal(response, &rule); e != nil {
			t.Fatal(e)
		}
		return rule
	}
	r := update("creation", 1)
	if r.PipelineID != "20" || r.StatusID != "" {
		t.Fatal("creation retained hidden stage")
	}
	r = update("digital_pipeline", r.Revision)
	if r.PipelineID != "" || r.StatusID != "" {
		t.Fatal("trigger retained manual point")
	}
	second, e := fx.svc.CreateDistributionGroup(ctx, fx.owner, CreateDistributionGroupInput{Name: "Second trigger", MemberIDs: []uuid.UUID{employee}})
	if e != nil {
		t.Fatal(e)
	}
	request, _ := json.Marshal(map[string]any{"bindingId": fx.binding, "bindingRevision": 1, "groupId": second.ID, "source": "digital_pipeline", "active": true})
	raw, e := fx.svc.DistributionRuntimeWrite(ctx, fx.owner, "rules", uuid.Nil, request)
	if e != nil {
		t.Fatal(e)
	}
	var created distributionRuleDTO
	if json.Unmarshal(raw, &created) != nil || created.PipelineID != "" || created.StatusID != "" {
		t.Fatalf("independent trigger group %s", raw)
	}
}
