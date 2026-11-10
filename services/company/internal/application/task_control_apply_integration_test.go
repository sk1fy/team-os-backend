//go:build integration

package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlSaveAppliesRules(t *testing.T) {
	f := newR05Fixture(t)
	originalToken := taskControlApplySession(t, f, 1)
	in := TaskControlDraftInput{
		ExpectedBindingRevision: 1, ExpectedModeEpoch: 1, ExpectedRevision: 1,
		Rules: []tc.Rule{taskControlApplyRule()},
	}
	requestID := uuid.New()

	if _, err := f.svc.UpdateTaskControlDraft(f.ctx, f.reader, f.binding, in, &requestID); !isCompanyError(err, ErrorForbidden) {
		t.Fatalf("read-only actor saved rules: %v", err)
	}
	if _, err := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, in, &requestID); !isCompanyError(err, ErrorUpstream) {
		t.Fatalf("save without fresh references: %v", err)
	}
	if current, err := f.svc.GetTaskControlDraft(f.ctx, f.actor, f.binding); err != nil || current.Revision != 1 || current.Scope.ModeEpoch != 1 || len(current.Rules) != 0 {
		t.Fatalf("failed save changed editor: %+v, %v", current, err)
	}
	bootstrap, err := f.svc.TaskControlBootstrap(f.ctx, originalToken)
	if err != nil || bootstrap.(map[string]any)["mode"] != "standalone" {
		t.Fatalf("failed save changed runtime: %+v, %v", bootstrap, err)
	}

	taskControlApplyReferences(t, f)
	saved, err := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, in, &requestID)
	if err != nil || saved.Revision != 2 || saved.Scope.ModeEpoch != 2 {
		t.Fatalf("save: %+v, %v", saved, err)
	}
	if _, err = f.svc.TaskControlBootstrap(f.ctx, originalToken); err == nil {
		t.Fatal("pre-save runtime session survived mode change")
	}
	token := taskControlApplySession(t, f, saved.Scope.ModeEpoch)
	bootstrap, err = f.svc.TaskControlBootstrap(f.ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	wire := struct {
		Mode        string
		Publication struct {
			Version      int64
			RuntimeRules []tc.LegacyRule
		}
	}{}
	if err = json.Unmarshal(mustR05JSON(t, bootstrap), &wire); err != nil || wire.Mode != "managed" || wire.Publication.Version != 1 || len(wire.Publication.RuntimeRules) != 1 || !wire.Publication.RuntimeRules[0].DenyExit {
		t.Fatalf("runtime after save: %+v, %v", wire, err)
	}

	replayed, err := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, in, &requestID)
	if err != nil || !bytes.Equal(mustR05JSON(t, saved), mustR05JSON(t, replayed)) {
		t.Fatalf("same request did not replay saved receipt: %+v, %v", replayed, err)
	}
	in.Rules[0].Name = "Другая настройка"
	if _, err = f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, in, &requestID); !isCompanyError(err, ErrorConflict) {
		t.Fatalf("changed input accepted for same request: %v", err)
	}
	_, err = f.svc.SetTaskControlMode(f.ctx, f.actor, f.binding, TaskControlModeInput{
		ExpectedBindingRevision: 1, ExpectedModeEpoch: saved.Scope.ModeEpoch, RequestID: uuid.New(), TargetMode: "standalone",
	})
	var modeErr *Error
	if !errors.As(err, &modeErr) || modeErr.Kind != ErrorConflict || modeErr.Code != "task_control_managed_only" {
		t.Fatalf("adopted connection accepted legacy mode transition: %v", err)
	}

	page, err := f.svc.TaskControlPublications(f.ctx, f.actor, f.binding, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := r05Page(t, page)
	if len(items) != 1 {
		t.Fatalf("expected one publication, got %d", len(items))
	}
}

func TestTaskControlSaveAppliesRulesV2(t *testing.T) {
	f := newR05Fixture(t)
	taskControlApplyReferences(t, f)
	if _, err := f.svc.ActivateTaskControlV2(f.ctx, f.actor, f.binding, TaskControlActivateInputV2{uuid.New(), 1, 1}); err != nil {
		t.Fatal(err)
	}
	rule := taskControlApplyRule()
	in := TaskControlDraftInputV2{1, 1, 2, []tc.IntentRule{{Kind: "legacyExplicit", Legacy: &rule}}}
	requestID := uuid.New()
	saved, err := f.svc.UpdateTaskControlDraftV2(f.ctx, f.actor, f.binding, in, &requestID)
	if err != nil || saved.Revision != 2 || saved.Scope.ModeEpoch != 3 {
		t.Fatalf("save: %+v, %v", saved, err)
	}
	token := taskControlApplySession(t, f, saved.Scope.ModeEpoch)
	wire, err := f.svc.TaskControlBootstrapV2(f.ctx, token)
	if err != nil || wire.Scope != saved.Scope || wire.PublicationVersion != 1 || len(wire.RuntimeRules) != 1 || !wire.RuntimeRules[0].DenyExit {
		t.Fatalf("runtime after save: %+v, %v", wire, err)
	}
	replayed, err := f.svc.UpdateTaskControlDraftV2(f.ctx, f.actor, f.binding, in, &requestID)
	if err != nil || !bytes.Equal(mustR05JSON(t, saved), mustR05JSON(t, replayed)) {
		t.Fatalf("same request did not replay saved receipt: %+v, %v", replayed, err)
	}
	_, err = f.svc.SetTaskControlModeV2(f.ctx, f.actor, f.binding, TaskControlModeInputV2{
		ExpectedBindingRevision: 1, ExpectedModeEpoch: saved.Scope.ModeEpoch, RequestID: uuid.New(), TargetMode: "standalone",
	})
	var modeErr *Error
	if !errors.As(err, &modeErr) || modeErr.Kind != ErrorConflict || modeErr.Code != "task_control_managed_only" {
		t.Fatalf("adopted connection accepted legacy mode transition: %v", err)
	}

}

func taskControlApplyRule() tc.Rule {
	return tc.Rule{
		RuleUID: uuid.New(), Name: "Новая настройка", Users: []string{"7"}, CustomerStatuses: []string{},
		Pipelines: []tc.Pipeline{{PipelineID: "10", StatusIDs: []string{"11"}}}, DenyExit: true, Enabled: true,
	}
}

func taskControlApplyReferences(t *testing.T, f r05Fixture) {
	t.Helper()
	c, err := db.New(f.pool).GetTaskControlConnection(f.ctx, db.GetTaskControlConnectionParams{CompanyID: f.actor.CompanyID, ID: f.binding})
	if err != nil {
		t.Fatal(err)
	}
	now := f.svc.now()
	refs := tc.ReferenceSnapshot{
		SchemaVersion: 1, Scope: referenceScope(c), Complete: true,
		Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now},
		FreshUntil: now.Add(5 * time.Minute), Users: []tc.ReferenceUser{{ID: "7", Name: "Менеджер", IsActive: true}},
		Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{{ID: "10", Name: "Продажи", Sort: 1, Statuses: []tc.ReferenceStatus{
			{ID: "11", Name: "Открыт", Sort: 1, Kind: "open"},
			{ID: "142", Name: "Успех", Sort: 2, Kind: "won"},
			{ID: "143", Name: "Отказ", Sort: 3, Kind: "lost"},
		}}},
		Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now},
	}
	refs.Revision, err = tc.ReferenceRevision(refs)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.taskControlCore.(*referenceTestCore).snapshot = refs
	f.exec(t, "INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload) VALUES($1,$2,$3,$4)", f.actor.CompanyID, f.binding, referenceScopeKey(c), mustR05JSON(t, refs))
}

func taskControlApplySession(t *testing.T, f r05Fixture, epoch int64) string {
	t.Helper()
	token := (uuid.NewString() + uuid.NewString())[:43]
	hash := sha256.Sum256([]byte(token))
	f.exec(t, "INSERT INTO task_control_runtime_sessions(token_hash,company_id,binding_id,actor_id,binding_revision,mode_epoch,generation,expires_at) VALUES($1,$2,$3,'7',1,$4,1,$5)", hash[:], f.actor.CompanyID, f.binding, epoch, f.svc.now().Add(time.Minute))
	return token
}
