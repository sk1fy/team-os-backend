//go:build integration

package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlImportIsolationVerificationAndReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, owner, employee, binding, install, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor := Actor{CompanyID: company, UserID: owner}
	seedAccessCompany(t, ctx, pool, company, owner, []accessTestUser{{owner, "owner", "active"}, {employee, "employee", "active"}})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO company_integrations(id,company_id,provider,external_account_id,status)VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company)
	exec(`INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id)VALUES($1,$2,$3,$4,'123',1,$5,$6,'7')`, company, binding, install, integration, uuid.New(), owner)
	now := time.Now().UTC().Truncate(time.Second)
	core := &referenceTestCore{status: tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute), RuntimeVersion: "1.2.1", RuntimeCapabilities: []string{"dynamicRulesV2"}}}
	public, key, _ := ed25519.GenerateKey(rand.Reader)
	svc := &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core, taskControlKeys: map[string]ed25519.PublicKey{"key": public}}
	proof := func(request uuid.UUID) string {
		a := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: company, InstallationID: install, IntegrationID: integration, AccountID: "123", ActorID: "7", Product: tc.Product, Generation: 1, Admin: true, Purpose: "adminBridge", RequestID: request}
		raw, _ := json.Marshal(a)
		prefix := "key." + base64.RawURLEncoding.EncodeToString(raw)
		return prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("teamos-task-control/v1\n"+prefix)))
	}
	rules := []tc.Rule{{RuleUID: uuid.MustParse("0c736c20-f044-45aa-b982-38b0e78f4760"), Name: "Отдел продаж", Users: []string{"2", "3"}, Pipelines: []tc.Pipeline{{PipelineID: "10", StatusIDs: []string{"20", "142", "143"}}}, CustomerStatuses: []string{"30"}, Customers: true, Enabled: false, Archived: true, ShowModal: true, DenyExit: true}}
	_, sum := tc.Legacy(rules)
	original, e := os.ReadFile("../domain/taskcontrol/testdata/r04-catalog-source.json")
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]json.RawMessage{}
	for i, k := range []string{"rule_uid", "users", "pipelines", "customer_statuses", "show_modal", "deny_exit", "check_responsible", "customers", "enabled", "archived"} {
		ids[k] = json.RawMessage(fmt.Sprint(100 + i))
	}
	payload := TaskControlSourcePayload{Rules: rules, Raw: original, CatalogID: "77", FieldIDs: ids, Code: "RKRS_WT_RULE_UID"}
	create := func() TaskControlSourceRequest {
		t.Helper()
		r, e := svc.CreateTaskControlSource(ctx, actor, binding, TaskControlSourceInput{1, 1, uuid.New(), "catalog", "77"})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	submit := func(r TaskControlSourceRequest, p TaskControlSourcePayload) TaskControlSourceRequest {
		t.Helper()
		_, hash := tc.Legacy(p.Rules)
		out, e := svc.SubmitTaskControlSource(ctx, uuid.Nil, r.ID, TaskControlSourceResult{proof(r.ID), true, hash, now, p})
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	request := create()
	pending, e := svc.PendingTaskControlSources(ctx, uuid.Nil, proof(uuid.New()))
	if e != nil || len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("pending=%+v %v", pending, e)
	}
	forged := TaskControlSourceResult{proof(uuid.New()), true, sum, now, payload}
	if _, e = svc.SubmitTaskControlSource(ctx, uuid.Nil, request.ID, forged); e == nil {
		t.Fatal("wrong request proof accepted")
	}
	partial := create()
	out, e := svc.SubmitTaskControlSource(ctx, uuid.Nil, partial.ID, TaskControlSourceResult{CoreAssertion: proof(partial.ID), Complete: false})
	if e != nil || out.State != "failed" || out.SourceSnapshotID != nil {
		t.Fatalf("partial source: %+v %v", out, e)
	}
	// JSONB removes duplicate keys. Reject the untouched input before persistence,
	// even when its last value exactly matches the valid source.
	duplicateRequest := create()
	duplicatePayload := payload
	duplicatePayload.Raw = bytes.Replace(payload.Raw, []byte(`"account_id": 123`), []byte(`"account_id": 999, "account_id": 123`), 1)
	duplicateResult, e := svc.SubmitTaskControlSource(ctx, uuid.Nil, duplicateRequest.ID, TaskControlSourceResult{proof(duplicateRequest.ID), true, sum, now, duplicatePayload})
	if e != nil || duplicateResult.State != "failed" || duplicateResult.SourceSnapshotID != nil || duplicateResult.ErrorCode != "source_invalid" || len(duplicateResult.Issues) == 0 {
		t.Fatalf("duplicate raw keys accepted before JSONB: %+v %v", duplicateResult, e)
	}
	duplicateSaved, e := svc.GetTaskControlSource(ctx, actor, binding, duplicateRequest.ID)
	if e != nil || duplicateSaved.State != "failed" || len(duplicateSaved.Issues) == 0 || duplicateSaved.SourceSnapshotID != nil {
		t.Fatalf("invalid capture lost diagnostics after JSONB: %+v %v", duplicateSaved, e)
	}
	if _, e = svc.PreviewTaskControlImport(ctx, actor, binding, TaskControlImportInput{1, 1, uuid.New(), 1, duplicateRequest.ID}, false); e == nil {
		t.Fatal("invalid capture unexpectedly usable as import snapshot")
	}
	source := submit(request, payload)
	previewIn := TaskControlImportInput{1, 1, uuid.New(), 1, *source.SourceSnapshotID}
	preview, e := svc.PreviewTaskControlImport(ctx, actor, binding, previewIn, false)
	if e != nil || !preview.Complete || len(preview.Rules) != 1 {
		t.Fatalf("preview %+v %v", preview, e)
	}
	apply := TaskControlImportApplyInput{1, 1, uuid.New(), 1, sum, *source.SourceSnapshotID, false}
	if _, e = svc.ApplyTaskControlImport(ctx, actor, binding, preview.ID, apply, false); e == nil {
		t.Fatal("initial source accepted as verification")
	}
	changed := payload
	changed.Raw = append(json.RawMessage(nil), payload.Raw...)
	changed.Raw = bytes.Replace(changed.Raw, []byte("preserved"), []byte("changed"), 1)
	changedSource := submit(create(), changed)
	apply.VerificationSnapshotID = *changedSource.SourceSnapshotID
	if _, e = svc.ApplyTaskControlImport(ctx, actor, binding, preview.ID, apply, false); e == nil {
		t.Fatal("changed raw source accepted")
	}
	verification := submit(create(), payload)
	apply.VerificationSnapshotID = *verification.SourceSnapshotID
	var wg sync.WaitGroup
	responses := make([]json.RawMessage, 2)
	errs := make([]error, 2)
	for i := range responses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses[i], errs[i] = svc.ApplyTaskControlImport(ctx, actor, binding, preview.ID, apply, false)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw := responses[0]
	if tcImportSemanticHash(raw) != tcImportSemanticHash(responses[1]) {
		t.Fatal("concurrent idempotent apply diverged")
	}

	var draft TaskControlDraft
	if e = json.Unmarshal(raw, &draft); e != nil || draft.Revision != 2 || draft.Rules[0].RuleUID != rules[0].RuleUID || !draft.Rules[0].Archived || draft.Rules[0].Pipelines[0].StatusIDs[2] != "143" {
		t.Fatalf("draft %+v %v", draft, e)
	}
	// Replay survives both source and preview expiry, while current authorization is required.
	now = now.Add(31 * time.Minute)
	core.status.CheckedAt = now
	core.status.FreshUntil = now.Add(5 * time.Minute)
	replay, e := svc.ApplyTaskControlImport(ctx, actor, binding, preview.ID, apply, false)
	if e != nil || tcImportSemanticHash(replay) != tcImportSemanticHash(raw) {
		t.Fatalf("durable replay %s %v", replay, e)
	}
	exec(`INSERT INTO task_control_permissions(company_id,user_id,can_read,can_manage)VALUES($1,$2,true,false)`, company, owner)
	if _, e = svc.ApplyTaskControlImport(ctx, actor, binding, preview.ID, apply, false); e == nil {
		t.Fatal("revoked manage permission bypassed by replay")
	}
	q := db.New(pool)
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: company, ID: binding})
	if e != nil || c.Mode != "standalone" || c.PublicationID.Valid || c.ModeEpoch != 1 {
		t.Fatalf("import changed publication/mode %+v %v", c, e)
	}
	var backups int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM task_control_import_objects WHERE company_id=$1 AND binding_id=$2 AND kind='apply' AND payload->'backup'->>'revision'='1'`, company, binding).Scan(&backups); e != nil || backups != 1 {
		t.Fatalf("backup count %d %v", backups, e)
	}
	// Schema-2 import wraps the exact explicit legacy rule, retaining the v2 binding.
	exec(`UPDATE task_control_permissions SET can_manage=true WHERE company_id=$1 AND user_id=$2`, company, owner)
	exec(`UPDATE task_control_connections SET runtime_schema_version=2,mode_epoch=2 WHERE company_id=$1 AND id=$2`, company, binding)
	createV2 := func() TaskControlSourceRequest {
		t.Helper()
		r, e := svc.CreateTaskControlSource(ctx, actor, binding, TaskControlSourceInput{1, 2, uuid.New(), "catalog", "77"})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	v2source := submit(createV2(), payload)
	v2in := TaskControlImportInput{1, 2, uuid.New(), 2, *v2source.SourceSnapshotID}
	if _, e = svc.PreviewTaskControlImport(ctx, actor, binding, v2in, false); e == nil {
		t.Fatal("v1 endpoint accepted schema-2 binding")
	}
	v2preview, e := svc.PreviewTaskControlImport(ctx, actor, binding, v2in, true)
	if e != nil {
		t.Fatal(e)
	}
	v2verification := submit(createV2(), payload)
	v2raw, e := svc.ApplyTaskControlImport(ctx, actor, binding, v2preview.ID, TaskControlImportApplyInput{1, 2, uuid.New(), 2, sum, *v2verification.SourceSnapshotID, false}, true)
	if e != nil {
		t.Fatal(e)
	}
	var v2draft TaskControlDraftV2
	if e = json.Unmarshal(v2raw, &v2draft); e != nil || len(v2draft.IntentRules) != 1 || v2draft.IntentRules[0].Kind != "legacyExplicit" || v2draft.IntentRules[0].Legacy.RuleUID != rules[0].RuleUID || v2draft.IntentRules[0].Dynamic != nil {
		t.Fatalf("v2 import %+v %v", v2draft, e)
	}
	if _, e = svc.GetTaskControlImport(ctx, actor, binding, v2preview.ID); e != nil {
		t.Fatal(e)
	}

}
