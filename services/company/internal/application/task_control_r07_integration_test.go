//go:build integration

package application

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"strings"
	"testing"
	"time"
)

func r07Session(t *testing.T, f r05Fixture, actor string) string {
	t.Helper()
	token := strings.Repeat(actor, 43)
	hash := sha256.Sum256([]byte(token))
	f.exec(t, `INSERT INTO task_control_runtime_sessions(token_hash,company_id,binding_id,actor_id,binding_revision,mode_epoch,generation,expires_at)VALUES($1,$2,$3,$4,1,1,1,$5)`, hash[:], f.actor.CompanyID, f.binding, actor, f.svc.now().Add(5*time.Minute))
	return token
}
func r07Envelope(t *testing.T, f r05Fixture, token string) TaskControlTelemetryInput {
	t.Helper()
	got, e := f.svc.TaskControlBootstrap(f.ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	var out TaskControlTelemetryInput
	if e = json.Unmarshal(mustR05JSON(t, got), &out); e != nil {
		t.Fatal(e)
	}
	out.RuntimeInstanceID = uuid.New()
	out.EventID = uuid.New()
	out.CorrelationID = uuid.New()
	out.Event = "received"
	out.OccurredAt = f.svc.now()
	out.RuntimeVersion = "1.2.2"
	return out
}
func r07Report(t *testing.T, f r05Fixture, pub uuid.UUID) map[string]any {
	t.Helper()
	out, e := f.svc.TaskControlApplication(f.ctx, f.reader, f.binding, pub)
	if e != nil {
		t.Fatal(e)
	}
	return out.(map[string]any)
}
func TestTaskControlR07InstancesIsolationLeaseAndRetention(t *testing.T) {
	f := newR05Fixture(t)
	f.exec(t, `UPDATE task_control_connections SET mode='managed' WHERE company_id=$1 AND id=$2`, f.actor.CompanyID, f.binding)
	if _, e := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, TaskControlDraftInput{1, 1, 1, []tc.Rule{}}); e != nil {
		t.Fatal(e)
	}
	pub := f.publish(t)
	token := r07Session(t, f, "7")
	otherToken := r07Session(t, f, "8")
	first := r07Envelope(t, f, token)
	if out := r07Report(t, f, pub.ID); out["runtimeState"] != "runtimeUnknown" || out["allInstancesApplied"] != false {
		t.Fatal(out)
	}
	record := func(in TaskControlTelemetryInput) {
		t.Helper()
		if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, in); e != nil {
			t.Fatal(e)
		}
	}
	record(first)
	record(first) // exact replay does not inflate count
	bad := first
	bad.Event = "applied"
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, bad); e == nil {
		t.Fatal("event id reuse accepted")
	}
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, otherToken, first); e == nil {
		t.Fatal("foreign actor accepted")
	}
	impostor := r07Envelope(t, f, otherToken)
	impostor.RuntimeInstanceID = first.RuntimeInstanceID
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, otherToken, impostor); e == nil {
		t.Fatal("another authorized actor reused instance")
	}
	second := first
	second.RuntimeInstanceID = uuid.New()
	second.EventID = uuid.New()
	second.CorrelationID = uuid.New()
	record(second)
	premature := second
	premature.EventID = uuid.New()
	premature.Event = "heartbeat"
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, premature); e == nil {
		t.Fatal("heartbeat counted as initial apply")
	}
	first.Event = "applied"
	first.EventID = uuid.New()
	record(first)
	out := r07Report(t, f, pub.ID)
	if out["activeInstances"] != 2 || out["appliedInstances"] != 1 || out["allInstancesApplied"] != false || out["runtimeState"] != "appliedByRuntime" {
		t.Fatal(out)
	}
	first.Event = "heartbeat"
	first.EventID = uuid.New()
	record(first)
	f.exec(t, `UPDATE task_control_permissions SET can_read=false WHERE company_id=$1 AND user_id=$2`, f.reader.CompanyID, f.reader.UserID)
	if _, e := f.svc.TaskControlApplication(f.ctx, f.reader, f.binding, pub.ID); e == nil {
		t.Fatal("reader rights bypassed")
	}
	f.exec(t, `UPDATE task_control_permissions SET can_read=true WHERE company_id=$1 AND user_id=$2`, f.reader.CompanyID, f.reader.UserID)
	// Bootstrap failures remain scoped and correlated without a fabricated publication.
	failure := TaskControlTelemetryInput{Scope: first.Scope, RuntimeInstanceID: uuid.New(), ActorID: "7", RuntimeVersion: "1.2.2", Event: "error", EventID: uuid.New(), OccurredAt: f.svc.now(), CorrelationID: uuid.New(), ErrorCode: "config_fetch_failed"}
	record(failure)
	out = r07Report(t, f, pub.ID)
	instances := out["instances"].([]TaskControlRuntimeInstance)
	found := false
	for _, instance := range instances {
		if instance.RuntimeInstanceID == failure.RuntimeInstanceID {
			found = instance.Reason == "config_fetch_failed" && instance.CorrelationID == failure.CorrelationID
		}
	}
	if !found {
		t.Fatal("missing correlated cause", out)
	}
	oldNow := f.svc.now()
	f.svc.now = func() time.Time { return oldNow.Add(4*time.Minute + 59*time.Second) }
	first.EventID = uuid.New()
	first.OccurredAt = f.svc.now()
	record(first)
	// The receipt cannot renew the issued lease. Closed instances become stale, never uninstall.
	f.svc.now = func() time.Time { return oldNow.Add(5*time.Minute + time.Second) }
	out = r07Report(t, f, pub.ID)
	if out["activeInstances"] != 0 || out["runtimeState"] != "runtimeUnknown" {
		t.Fatal(out)
	}
	f.exec(t, `UPDATE task_control_runtime_sessions SET expires_at=$1 WHERE company_id=$2`, f.svc.now().Add(time.Minute), f.actor.CompanyID)
	first.EventID = uuid.New()
	first.OccurredAt = f.svc.now()
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, first); e == nil {
		t.Fatal("expired issued ACK accepted")
	}
	var state string
	if e := f.pool.QueryRow(f.ctx, `SELECT state FROM task_control_connections WHERE company_id=$1 AND id=$2`, f.actor.CompanyID, f.binding).Scan(&state); e != nil || state != "active" {
		t.Fatal(state, e)
	}
	f.exec(t, `UPDATE task_control_runtime_events SET received_at=$1`, oldNow.Add(-31*24*time.Hour))
	f.exec(t, `UPDATE task_control_runtime_issued SET valid_until=$1`, oldNow.Add(-31*24*time.Hour))
	if e := f.svc.CleanupTaskControlRuntime(f.ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_control_runtime_events`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
func TestTaskControlR07RateScopeAndStalePublication(t *testing.T) {
	f := newR05Fixture(t)
	f.exec(t, `UPDATE task_control_connections SET mode='managed' WHERE company_id=$1 AND id=$2`, f.actor.CompanyID, f.binding)
	if _, e := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, TaskControlDraftInput{1, 1, 1, []tc.Rule{}}); e != nil {
		t.Fatal(e)
	}
	pub := f.publish(t)
	token := r07Session(t, f, "7")
	in := r07Envelope(t, f, token)
	wrong := in
	wrong.Scope.AccountID = "999"
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, wrong); e == nil {
		t.Fatal("foreign scope accepted")
	}
	for i := 0; i < 20; i++ {
		in.EventID = uuid.New()
		if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, in); e != nil {
			t.Fatal(i, e)
		}
	}
	in.EventID = uuid.New()
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, in); e == nil {
		t.Fatal("rate limit missing")
	} else {
		ae, ok := e.(*Error)
		if !ok || ae.Code != "telemetry_rate_limited" {
			t.Fatal(e)
		}
	}
	next := f.publish(t)
	if next.ID == pub.ID {
		t.Fatal("new publication not created")
	}
	in.RuntimeInstanceID = uuid.New()
	if e := f.svc.RecordTaskControlTelemetry(f.ctx, token, in); e == nil {
		t.Fatal("old publication ACK accepted")
	}
	out := r07Report(t, f, next.ID)
	if out["appliedInstances"] != 0 || out["allInstancesApplied"] != false {
		t.Fatal(out)
	}
}
