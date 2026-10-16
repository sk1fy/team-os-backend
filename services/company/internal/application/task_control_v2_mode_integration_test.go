//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlDynamicModeConfirmationEpochAndRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, owner, employee, binding, install, integration, department := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
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
	exec(`INSERT INTO departments(id,company_id,name,"order")VALUES($1,$2,'Поддержка',999)`, department, company)
	exec(`INSERT INTO user_departments(company_id,user_id,department_id)VALUES($1,$2,$4),($1,$3,$4)`, company, owner, employee, department)
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'7'),($1,'123',$3,'8')`, company, employee, owner)
	now := time.Now().UTC().Truncate(time.Second)
	core := &referenceTestCore{status: tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute), RuntimeVersion: "1.2.0", RuntimeCapabilities: []string{"dynamicRulesV2"}}}
	public, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	svc := &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core, taskControlKeys: map[string]ed25519.PublicKey{"key": public}}
	q := db.New(pool)
	connection := func() db.TaskControlConnection {
		t.Helper()
		c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: company, ID: binding})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	proof := func(purpose string, request uuid.UUID) string {
		a := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: company, InstallationID: install, IntegrationID: integration, AccountID: "123", ActorID: "7", Product: tc.Product, Generation: 1, Admin: true, Purpose: purpose, RequestID: request}
		raw, _ := json.Marshal(a)
		prefix := "key." + base64.RawURLEncoding.EncodeToString(raw)
		return prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("teamos-task-control/v1\n"+prefix)))
	}
	assertCode := func(err error, code string) {
		t.Helper()
		var actual *Error
		if !errors.As(err, &actual) || actual.Code != code {
			t.Fatalf("expected %s, got %v", code, err)
		}
	}
	c := connection()
	source := tc.ReferenceSnapshot{SchemaVersion: 1, Scope: referenceScope(c), Complete: true, Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute), Users: []tc.ReferenceUser{{ID: "7", IsActive: true}, {ID: "8", IsActive: true}}, Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{{ID: "10", Statuses: []tc.ReferenceStatus{{ID: "11", Kind: "open"}, {ID: "142", Kind: "won"}, {ID: "143", Kind: "lost"}}}}, Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
	source.Revision, e = tc.ReferenceRevision(source)
	if e != nil {
		t.Fatal(e)
	}
	core.snapshot = source
	raw, _ := json.Marshal(source)
	exec(`INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload)VALUES($1,$2,$3,$4)`, company, binding, referenceScopeKey(c), raw)
	raw, _ = json.Marshal(core.status)
	exec(`INSERT INTO task_control_installation_snapshots(company_id,account_id,payload,checked_at,fresh_until)VALUES($1,'123',$2,$3,$4)`, company, raw, now, now.Add(5*time.Minute))
	if _, e = svc.ActivateTaskControlV2(ctx, actor, binding, TaskControlActivateInputV2{uuid.New(), 1, 1}); e != nil {
		t.Fatal(e)
	}
	rules := []tc.IntentRule{{Kind: "dynamic", Dynamic: &tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Отдел целиком", Audience: tc.AudienceSelection{Kind: "selected", EmployeeIDs: []uuid.UUID{}, Departments: []tc.DepartmentSelection{{DepartmentID: department, IncludeDescendants: true}}}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true, ShowModal: true}}}
	draft, e := svc.UpdateTaskControlDraftV2(ctx, actor, binding, TaskControlDraftInputV2{1, 1, 2, rules})
	if e != nil {
		t.Fatal(e)
	}
	publish := func(d TaskControlDraftV2) TaskControlPublicationV2 {
		t.Helper()
		c := connection()
		p, e := svc.PublishTaskControlDraftV2(ctx, actor, binding, TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedDraftRevision: d.Revision, ExpectedChecksum: d.Checksum, ExpectedBindingRevision: c.BindingRevision, ExpectedModeEpoch: c.ModeEpoch})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	original := publish(draft)
	oldSession, e := svc.ExchangeTaskControlSessionNegotiated(ctx, proof("runtime", uuid.New()), "1.2.0", []string{"dynamicRulesV2"})
	if e != nil {
		t.Fatal(e)
	}
	modeInput := func(target string, pubID uuid.UUID, preview map[string]any) TaskControlModeInputV2 {
		c := connection()
		request := uuid.New()
		return TaskControlModeInputV2{RequestID: request, ExpectedBindingRevision: c.BindingRevision, ExpectedModeEpoch: c.ModeEpoch, TargetMode: target, PublicationID: pubID, CoreAssertion: proof("mode", request), ExportChecksum: preview["exportChecksum"].(string), ExpectedSourceRevisions: preview["sourceRevisions"].(TaskControlSourceRevisions), ConfirmDynamicLoss: true, ConfirmEmptyExport: true}
	}
	previewMode := func(target string, pubID uuid.UUID) map[string]any {
		t.Helper()
		out, e := svc.PreviewTaskControlModeV2(ctx, actor, binding, TaskControlModePreviewInputV2{target, pubID})
		if e != nil {
			t.Fatal(e)
		}
		return out.(map[string]any)
	}
	preview := previewMode("managed", original.ID)
	enter := modeInput("managed", original.ID, preview)
	entered, e := svc.SetTaskControlModeV2(ctx, actor, binding, enter)
	if e != nil {
		t.Fatal(e)
	}
	scope := entered.(map[string]any)["scope"].(TaskControlScope)
	if scope.ModeEpoch != 3 || connection().Mode != "managed" {
		t.Fatal("managed transition did not advance epoch", scope)
	}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, oldSession.AccessToken); e == nil {
		t.Fatal("pre-transition session still valid")
	}
	if _, e = svc.SetTaskControlModeV2(ctx, actor, binding, enter); e != nil {
		t.Fatal("mode replay", e)
	}
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if worked, e := svc.resolveTaskControlV2Job(ctx); e != nil || !worked {
		t.Fatal("new-epoch resolver", worked, e)
	}
	managedSession, e := svc.ExchangeTaskControlSessionNegotiated(ctx, proof("runtime", uuid.New()), "1.2.0", []string{"dynamicRulesV2"})
	if e != nil {
		t.Fatal(e)
	}
	envelope, e := svc.TaskControlBootstrapV2(ctx, managedSession.AccessToken)
	if e != nil || envelope.Scope.ModeEpoch != 3 || len(envelope.RuntimeRules) != 1 || len(envelope.RuntimeRules[0].Users) != 1 {
		t.Fatal("managed runtime", envelope, e)
	}
	// A confirmation binds the complete export and both organization/mapping revisions.
	preview = previewMode("standalone", original.ID)
	exported := preview["runtimeRules"].([]tc.LegacyRule)
	if !preview["dynamicLoss"].(bool) || len(exported) != 1 || len(exported[0].Users) != 2 {
		t.Fatal("standalone preview must export all users", preview)
	}
	stale := modeInput("standalone", original.ID, preview)
	exec(`UPDATE users SET first_name=first_name||' Новый' WHERE company_id=$1 AND id=$2`, company, employee)
	_, e = svc.SetTaskControlModeV2(ctx, actor, binding, stale)
	assertCode(e, "source_changed")
	if c = connection(); c.Mode != "managed" || c.ModeEpoch != 3 {
		t.Fatal("stale confirmation changed mode", c)
	}
	if _, e = svc.TaskControlBootstrapV2(ctx, managedSession.AccessToken); e != nil {
		t.Fatal("rejected transition invalidated session", e)
	}
	preview = previewMode("standalone", original.ID)
	leave := modeInput("standalone", original.ID, preview)
	unconfirmed := leave
	unconfirmed.ConfirmDynamicLoss = false
	if _, e = svc.SetTaskControlModeV2(ctx, actor, binding, unconfirmed); e == nil {
		t.Fatal("dynamic-loss confirmation omitted")
	}
	left, e := svc.SetTaskControlModeV2(ctx, actor, binding, leave)
	if e != nil {
		t.Fatal(e)
	}
	if left.(map[string]any)["scope"].(TaskControlScope).ModeEpoch != 4 || connection().Mode != "standalone" {
		t.Fatal("standalone transition failed", left)
	}
	if _, _, e = svc.AuthorizeTaskControlRuntime(ctx, managedSession.AccessToken); e == nil {
		t.Fatal("managed session survived epoch change")
	}
	// Restore copies exact intent, resolves against current references, and never edits draft.
	all := *rules[0].Dynamic
	all.Audience = tc.AudienceSelection{Kind: "allLinked"}
	all.Name = "Все связанные"
	allDraft, e := svc.UpdateTaskControlDraftV2(ctx, actor, binding, TaskControlDraftInputV2{draft.Revision, 1, 4, []tc.IntentRule{{Kind: "dynamic", Dynamic: &all}}})
	if e != nil {
		t.Fatal(e)
	}
	current := publish(allDraft)
	restored, e := svc.RollbackTaskControlV2(ctx, actor, binding, TaskControlRollbackInputV2{RequestID: uuid.New(), SourcePublicationID: original.ID, ExpectedPublishedVersion: current.Version, ExpectedBindingRevision: 1, ExpectedModeEpoch: 4})
	if e != nil {
		t.Fatal("restore", e)
	}
	if restored.RestoredFromID == nil || *restored.RestoredFromID != original.ID || restored.IntentChecksum != original.IntentChecksum || restored.IntentRules[0].Dynamic.Audience.Kind != "selected" || restored.IntentRules[0].Dynamic.Audience.Departments[0].DepartmentID != department {
		t.Fatal("restore reinterpreted intent", restored)
	}
	current = publish(allDraft)
	exec(`DELETE FROM user_departments WHERE company_id=$1 AND department_id=$2`, company, department)
	exec(`DELETE FROM departments WHERE company_id=$1 AND id=$2`, company, department)
	before, e := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: company, BindingID: binding})
	if e != nil {
		t.Fatal(e)
	}
	_, e = svc.RollbackTaskControlV2(ctx, actor, binding, TaskControlRollbackInputV2{RequestID: uuid.New(), SourcePublicationID: original.ID, ExpectedPublishedVersion: current.Version, ExpectedBindingRevision: 1, ExpectedModeEpoch: 4})
	assertCode(e, "invalid_selection")
	after, e := q.GetTaskControlV2State(ctx, db.GetTaskControlV2StateParams{CompanyID: company, BindingID: binding})
	if e != nil || before.CurrentSnapshotID != after.CurrentSnapshotID || before.RuntimeRevision != after.RuntimeRevision || connection().PublicationID.UUID != current.ID {
		t.Fatal("failed restore changed active state", before, after, e)
	}
	storedDraft, e := svc.GetTaskControlDraftV2(ctx, actor, binding)
	if e != nil || storedDraft.Checksum != allDraft.Checksum || storedDraft.Revision != allDraft.Revision {
		t.Fatal("restore modified draft", storedDraft, e)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM task_control_publications WHERE company_id=$1 AND binding_id=$2`, company, binding).Scan(&count); e != nil || count != 4 {
		t.Fatal("failed restore allocated publication", count, e)
	}
}
