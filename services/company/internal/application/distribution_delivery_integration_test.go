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

type deliveryFake struct {
	observation corebridge.LeadObservation
	operation   corebridge.Operation
	reads       int
	hook        func()
	readErr     error
}

func (f *deliveryFake) ReadLead(_ context.Context, s corebridge.Scope, id string) (corebridge.LeadObservation, error) {
	f.reads++
	if f.readErr != nil {
		return corebridge.LeadObservation{}, f.readErr
	}
	f.observation.Scope = s
	f.observation.LeadID = id
	f.observation.ObservationRevision++
	f.observation.ObservedAt = time.Now().UTC()
	if f.observation.Snapshot != nil {
		f.observation.Snapshot.ObservedAt = f.observation.ObservedAt
	}
	out := f.observation
	if out.Snapshot != nil {
		copy := *out.Snapshot
		out.Snapshot = &copy
	}
	if f.hook != nil {
		hook := f.hook
		f.hook = nil
		hook()
	}
	return out, nil
}
func (f *deliveryFake) Operation(_ context.Context, _ corebridge.Scope, _ uuid.UUID) (corebridge.Operation, error) {
	return f.operation, nil
}
func TestDistributionDeliveryDurabilityAndHistoricalResults(t *testing.T) {
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
	event := DistributionEventEnvelope{SchemaVersion: 1, MessageID: uuid.New(), EventID: uuid.New(), Scope: scope, ReceivedAt: now, EmittedAt: now, CorrelationID: uuid.New(), Event: DistributionCRMEvent{Kind: "lead.created", LeadID: "10", ObservationRevision: 1, SourceEvidence: &DistributionSourceEvidence{PipelineID: &p, StatusID: &st}}}
	fake.observation = corebridge.LeadObservation{Snapshot: &corebridge.LeadSnapshot{LeadID: "10", PipelineID: p, StatusID: st, ResponsibleUserID: "1", ObservedAt: now}}
	first, e := svc.ReceiveDistributionEvent(ctx, event)
	if e != nil {
		t.Fatal(e)
	}
	repeat, e := svc.ReceiveDistributionEvent(ctx, event)
	if e != nil || repeat.ReceiptID != first.ReceiptID || repeat.Disposition != "duplicate" {
		t.Fatalf("ACK loss: %+v %v", repeat, e)
	}
	changed := event
	changed.Event.Kind = "lead.deleted"
	if _, e = svc.ReceiveDistributionEvent(ctx, changed); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("changed message %v", e)
	}
	restarted := &Service{pool: pool, now: time.Now, deliveryCore: fake, distributionCore: links}
	if found, e := restarted.ProcessDistributionDelivery(ctx); !found || e != nil {
		t.Fatalf("restart %v %v", found, e)
	}
	var seq int64
	var state string
	if e = pool.QueryRow(ctx, "SELECT last_sequence FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&seq); e != nil || seq != 1 {
		t.Fatalf("head %d %v", seq, e)
	}
	event.MessageID = uuid.New()
	if _, e = svc.ReceiveDistributionEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT state FROM distribution_delivery_inbox WHERE message_id=$1", event.MessageID).Scan(&state); e != nil || state != "ignored" {
		t.Fatalf("business duplicate %s %v", state, e)
	}
	send := func(kind string) {
		t.Helper()
		event.MessageID = uuid.New()
		event.EventID = uuid.New()
		event.Event.Kind = kind
		event.Event.ObservationRevision++
		if _, e = svc.ReceiveDistributionEvent(ctx, event); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.ProcessDistributionDelivery(ctx); e != nil {
			t.Fatal(e)
		}
	}
	fake.observation.Snapshot.StatusID = "40"
	send("lead.status_changed")
	fake.observation.Snapshot.StatusID = "30"
	send("lead.status_changed")
	if e = pool.QueryRow(ctx, "SELECT last_sequence FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&seq); e != nil || seq != 3 {
		t.Fatalf("reentry %d %v", seq, e)
	}
	// Allocate generations before GET; a slower earlier GET cannot overwrite a
	// newer completed observation, even when its inbox lease is still valid.
	fake.hook = func() { fake.observation.Snapshot.StatusID = "50"; send("lead.status_changed") }
	send("lead.status_changed")
	var snapshot []byte
	if e = pool.QueryRow(ctx, "SELECT snapshot FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&snapshot); e != nil {
		t.Fatal(e)
	}
	var projected corebridge.LeadSnapshot
	if e = json.Unmarshal(snapshot, &projected); e != nil || projected.StatusID != "50" {
		t.Fatalf("stale GET regressed head %+v %v", projected, e)
	}
	fake.observation.Snapshot.StatusID = "30"
	send("lead.status_changed")
	if e = pool.QueryRow(ctx, "SELECT last_sequence FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&seq); e != nil || seq != 5 {
		t.Fatalf("observed sequences %d %v", seq, e)
	}
	// X@100 -> source X/Y@101 -> fresh X@102 proves an unobserved gap,
	// not permission to reuse the earlier waiting candidate.
	time100 := time.Now().UTC().Add(-3 * time.Second)
	time101 := time100.Add(time.Second)
	time102 := time101.Add(time.Second)
	previousAt100 := *fake.observation.Snapshot
	previousAt100.SourceUpdatedAt = &time100
	previousRaw, _ := json.Marshal(previousAt100)
	if _, e = pool.Exec(ctx, "UPDATE distribution_lead_heads SET snapshot=$1::jsonb WHERE account_id='123' AND lead_id='10'", previousRaw); e != nil {
		t.Fatal(e)
	}
	x, y := "30", "40"
	event.Event.SourceEvidence = &DistributionSourceEvidence{PipelineID: &p, StatusID: &y, OldStatusID: &x}
	event.SourceOccurredAt = &time101
	fake.observation.Snapshot.SourceUpdatedAt = &time102
	send("lead.status_changed")
	if e = pool.QueryRow(ctx, "SELECT state FROM distribution_observed_entries WHERE sequence=5").Scan(&state); e != nil || state != "needs_configuration" {
		t.Fatalf("unobserved exit/reentry retained old wait %s %v", state, e)
	}
	// Clearly older source claims do not demote a later verified candidate.
	if _, e = pool.Exec(ctx, "UPDATE distribution_observed_entries SET state='checking',evidence='observed_transition' WHERE sequence=5"); e != nil {
		t.Fatal(e)
	}
	older := time100.Add(-time.Second)
	event.SourceOccurredAt = &older
	send("lead.status_changed")
	if e = pool.QueryRow(ctx, "SELECT state FROM distribution_observed_entries WHERE sequence=5").Scan(&state); e != nil || state != "checking" {
		t.Fatalf("old source demoted fresh entry %s %v", state, e)
	}
	event.SourceOccurredAt = nil
	old := "40"
	event.Event.SourceEvidence = &DistributionSourceEvidence{PipelineID: &p, StatusID: &st, OldStatusID: &old}
	send("lead.status_changed")
	if e = pool.QueryRow(ctx, "SELECT state FROM distribution_observed_entries WHERE sequence=5").Scan(&state); e != nil || state != "needs_configuration" {
		t.Fatalf("unproven reentry %s %v", state, e)
	}
	// A newer reserved generation whose read fails must not discard the older
	// successful observation. Only successfully applied generations fence writes.
	fake.observation.Snapshot.StatusID = "60"
	fake.hook = func() {
		pending := event
		pending.MessageID = uuid.New()
		pending.EventID = uuid.New()
		pending.Event.Kind = "lead.snapshot_reconciled"
		pending.Event.ObservationRevision++
		if _, err := svc.ReceiveDistributionEvent(ctx, pending); err != nil {
			t.Fatal(err)
		}
		fake.readErr = errors.New("source unavailable")
		if _, err := svc.ProcessDistributionDelivery(ctx); err == nil {
			t.Fatal("unavailable read became observation")
		}
		fake.readErr = nil
		if _, err := pool.Exec(ctx, "UPDATE distribution_delivery_inbox SET next_attempt_at=now()+interval '1 hour' WHERE message_id=$1", pending.MessageID); err != nil {
			t.Fatal(err)
		}
	}
	send("lead.status_changed")
	if e = pool.QueryRow(ctx, "SELECT snapshot FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&snapshot); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(snapshot, &projected); e != nil || projected.StatusID != "60" {
		t.Fatalf("new unavailable GET erased valid observation %+v %v", projected, e)
	}
	fake.observation.Snapshot = nil
	fake.observation.Absent = true
	reason := "not_found_or_deleted"
	fake.observation.AbsenceReason = &reason
	send("lead.deleted")
	if e = pool.QueryRow(ctx, "SELECT cancellation_reason FROM distribution_observed_entries WHERE sequence=6").Scan(&state); e != nil || state != "observed_missing" {
		t.Fatalf("absence %s %v", state, e)
	}
	var deleted, absent bool
	if e = pool.QueryRow(ctx, "SELECT deleted,absent FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&deleted, &absent); e != nil || deleted || !absent {
		t.Fatalf("absence fabricated deletion %v %v %v", deleted, absent, e)
	}
	op := corebridge.Operation{LeadID: "10", Scope: scope, OperationID: uuid.New(), EpisodeID: uuid.New(), DecisionID: uuid.New(), RuleID: uuid.New(), GroupID: uuid.New(), EventID: uuid.New(), CorrelationID: uuid.New(), RuleRevision: 1, AvailabilityRevision: 1, ClaimRevision: 1, TargetResponsibleUserID: "2", State: "queued", ExternalEffectState: "no_attempt", ResultVersion: 1, ResolutionEvidence: corebridge.OperationEvidence{Kind: "no_request_sent", ObservedAt: now}, AcceptedAt: now, UpdatedAt: now}
	result := DistributionResultEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: scope, EventID: op.EventID, CorrelationID: op.CorrelationID, ReceivedAt: now, EmittedAt: now, Result: op}
	result.Result.LeadID = "" // immutable stage19 frozen payload
	if _, e = svc.ReceiveDistributionResult(ctx, result); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProcessDistributionDelivery(ctx); e == nil {
		t.Fatal("unregistered result applied")
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_operation_mirrors").Scan(&count); e != nil || count != 0 {
		t.Fatal("delivery registered decision")
	}
	fake.operation = op
	if e = svc.RegisterDistributionOperationMirror(ctx, owner, op); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProcessDistributionDelivery(ctx); e != nil {
		t.Fatal(e)
	}
	// Pull and push compare operation semantics, independent of envelope timestamps.
	result.Result.LeadID = op.LeadID
	result.MessageID = uuid.New()
	result.EmittedAt = now.Add(time.Second)
	if _, e = svc.ReceiveDistributionResult(ctx, result); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProcessDistributionDelivery(ctx); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_operation_mirror_versions").Scan(&count); e != nil || count != 1 {
		t.Fatalf("semantic duplicate %d %v", count, e)
	}
	bad := result
	bad.MessageID = uuid.New()
	bad.Result.ResolutionEvidence.Explanation = "different"
	if _, e = svc.ReceiveDistributionResult(ctx, bad); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("version payload conflict %v", e)
	}
	if _, e = pool.Exec(ctx, "UPDATE distribution_bindings SET state='revoked' WHERE id=$1", scope.BindingID); e != nil {
		t.Fatal(e)
	}
	fake.operation.ResultVersion = 2
	fake.operation.State = "cancelled"
	fake.operation.ExternalEffectState = "no_attempt"
	fake.operation.ResolutionEvidence.GuardReleasable = true
	outcome := "cancelled"
	fake.operation.Outcome = &outcome
	fake.operation.UpdatedAt = now.Add(time.Second)
	if _, e = pool.Exec(ctx, "UPDATE distribution_operation_mirrors SET next_reconcile_at=now()-interval '1 minute'"); e != nil {
		t.Fatal(e)
	}
	if e = svc.ReconcileDistributionOperations(ctx, 20); e != nil {
		t.Fatal(e)
	}
	var version int64
	var unfinished bool
	if e = pool.QueryRow(ctx, "SELECT result_version,unfinished FROM distribution_operation_mirrors WHERE operation_id=$1", op.OperationID).Scan(&version, &unfinished); e != nil || version != 2 || unfinished {
		t.Fatalf("historical GET %d %v %v", version, unfinished, e)
	}
	beforeReads := fake.reads
	event.MessageID = uuid.New()
	event.EventID = uuid.New()
	event.Event.ObservationRevision++
	if _, e = svc.ReceiveDistributionEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProcessDistributionDelivery(ctx); e != nil {
		t.Fatal(e)
	}
	if fake.reads != beforeReads {
		t.Fatal("revoked event queried current CRM")
	}
	// Same-company rebind refreshes the current head while retaining prior entries.
	newConnection, err := svc.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "test"})
	if err != nil {
		t.Fatal(err)
	}
	newScope := corebridge.Scope{CompanyID: company, BindingID: newConnection.BindingID, BindingRevision: 1, InstallationID: newConnection.InstallationID, IntegrationID: newConnection.IntegrationID, AccountID: "123"}
	event.Scope = newScope
	event.MessageID = uuid.New()
	event.EventID = uuid.New()
	event.Event.Kind = "lead.snapshot_reconciled"
	event.Event.ObservationRevision++
	fake.observation.Absent = false
	fake.observation.AbsenceReason = nil
	fake.observation.Snapshot = &corebridge.LeadSnapshot{LeadID: "10", PipelineID: "20", StatusID: "30", ResponsibleUserID: "1", ObservedAt: time.Now()}
	if _, e = svc.ReceiveDistributionEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProcessDistributionDelivery(ctx); e != nil {
		t.Fatal(e)
	}
	var currentBinding uuid.UUID
	if e = pool.QueryRow(ctx, "SELECT binding_id FROM distribution_lead_heads WHERE account_id='123' AND lead_id='10'").Scan(&currentBinding); e != nil || currentBinding != newScope.BindingID {
		t.Fatalf("rebind projection %v %v", currentBinding, e)
	}
	// Historical account/lead ownership is never reassigned by a delivery.
	foreignCompany, foreignOwner, foreignBinding := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, foreignCompany, foreignOwner, []accessTestUser{{foreignOwner, "owner", "active"}})
	if _, e = pool.Exec(ctx, "INSERT INTO distribution_bindings(id,company_id,installation_id,integration_id,account_id,state,intent_id,initiated_by,initiator_snapshot,expires_at) VALUES($1,$2,$3,$4,'123','revoked',$5,$6,$6,now())", foreignBinding, foreignCompany, uuid.New(), uuid.New(), uuid.New(), foreignOwner); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO distribution_binding_versions(company_id,binding_id,revision,installation_id,integration_id,account_id,state) SELECT company_id,id,1,installation_id,integration_id,account_id,state FROM distribution_bindings WHERE id=$1", foreignBinding); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision) VALUES('123','11',$1,$2,1)", foreignCompany, foreignBinding); e != nil {
		t.Fatal(e)
	}
	event.Event.LeadID = "11"
	event.MessageID = uuid.New()
	event.EventID = uuid.New()
	if _, e = svc.ReceiveDistributionEvent(ctx, event); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProcessDistributionDelivery(ctx); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("foreign projection %v", e)
	}
	var savedCompany uuid.UUID
	if e = pool.QueryRow(ctx, "SELECT company_id FROM distribution_lead_heads WHERE account_id='123' AND lead_id='11'").Scan(&savedCompany); e != nil || savedCompany != foreignCompany {
		t.Fatal("projection ownership moved")
	}

	// A pull commits while receipt validation is waiting for the mirror row lock.
	// The receiver must re-read the newly stored semantic version before ACK.
	pull := fake.operation
	pull.ResultVersion = 3
	pull.ResolutionEvidence.Explanation = "background pull"
	push := pull
	push.ResolutionEvidence.Explanation = "different signed push"
	competing := DistributionResultEnvelope{SchemaVersion: 1, MessageID: uuid.New(), Scope: scope, EventID: push.EventID, CorrelationID: push.CorrelationID, ReceivedAt: now, EmittedAt: now, Result: push}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if _, err = q.GetDistributionOperationMirror(ctx, op.OperationID); err != nil {
		t.Fatal(err)
	}
	response := make(chan error, 1)
	go func() { _, err := svc.ReceiveDistributionResult(ctx, competing); response <- err }()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%FROM distribution_operation_mirrors%')").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("receiver did not contend on mirror lock")
	}
	if err = svc.applyOperationResult(ctx, q, pull); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-response; !isCompanyError(err, ErrorConflict) {
		t.Fatalf("pull/push race ACKed conflict: %v", err)
	}

}
