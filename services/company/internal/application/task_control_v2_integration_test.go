//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlDynamicPublicationRuntimeAndCAS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employeeID, binding, install, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor := Actor{CompanyID: company, UserID: ownerID}
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employeeID, "employee", "active"}})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO company_integrations(id,company_id,provider,external_account_id,status) VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company)
	exec(`INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id) VALUES($1,$2,$3,$4,'123',1,$5,$6,'7')`, company, binding, install, integration, uuid.New(), ownerID)
	now := time.Now().UTC().Truncate(time.Second)
	core := &referenceTestCore{status: tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute), RuntimeVersion: "1.2.0", RuntimeCapabilities: []string{"dynamicRulesV2"}}}
	keypub, key, _ := ed25519.GenerateKey(rand.Reader)
	svc := &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core, taskControlKeys: map[string]ed25519.PublicKey{"key": keypub}}
	assertion := func(purpose string, request uuid.UUID) string {
		a := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: company, InstallationID: install, IntegrationID: integration, AccountID: "123", ActorID: "7", Product: tc.Product, Generation: 1, Admin: true, Purpose: purpose, RequestID: request}
		raw, _ := json.Marshal(a)
		prefix := "key." + base64.RawURLEncoding.EncodeToString(raw)
		return prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("teamos-task-control/v1\n"+prefix)))
	}
	q := db.New(pool)
	connection := func() db.TaskControlConnection {
		t.Helper()
		c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: company, ID: binding})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	saveSource := func() {
		t.Helper()
		c := connection()
		core.status.CheckedAt = now
		core.status.FreshUntil = now.Add(5 * time.Minute)
		core.snapshot = tc.ReferenceSnapshot{SchemaVersion: 1, Scope: referenceScope(c), Complete: true, Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute), Users: []tc.ReferenceUser{{ID: "7", Name: "One", IsActive: true}, {ID: "8", Name: "Two", IsActive: true}}, Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{{ID: "10", Name: "Sales", Sort: 1, Statuses: []tc.ReferenceStatus{{ID: "11", Name: "Open", Sort: 1, Kind: "open"}, {ID: "142", Name: "Won", Sort: 2, Kind: "won"}, {ID: "143", Name: "Lost", Sort: 3, Kind: "lost"}}}}, Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
		core.snapshot.Revision, _ = tc.ReferenceRevision(core.snapshot)
		raw, _ := json.Marshal(core.snapshot)
		exec(`INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload)VALUES($1,$2,$3,$4)ON CONFLICT(company_id,binding_id,scope_key)DO UPDATE SET payload=excluded.payload,last_error=NULL`, company, binding, referenceScopeKey(c), raw)
		status, _ := json.Marshal(core.status)
		exec(`INSERT INTO task_control_installation_snapshots(company_id,account_id,payload,checked_at,fresh_until)VALUES($1,'123',$2,$3,$4)ON CONFLICT(company_id,account_id)DO UPDATE SET payload=excluded.payload,checked_at=excluded.checked_at,fresh_until=excluded.fresh_until`, company, status, now, now.Add(5*time.Minute))
	}
	saveSource()
	activate := TaskControlActivateInputV2{uuid.New(), 1, 1}
	out, e := svc.ActivateTaskControlV2(ctx, actor, binding, activate)
	if e != nil {
		t.Fatal("activate", e)
	}
	scope := out.(map[string]any)["scope"].(TaskControlScope)
	if scope.ModeEpoch != 2 {
		t.Fatal(scope)
	}
	if _, e = svc.ActivateTaskControlV2(ctx, actor, binding, activate); e != nil {
		t.Fatal("activation replay", e)
	}
	if _, e = svc.UpdateTaskControlDraft(ctx, actor, binding, TaskControlDraftInput{}); e == nil {
		t.Fatal("v1 write accepted")
	}
	rules := []tc.IntentRule{{Kind: "dynamic", Dynamic: &tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Dynamic", Audience: tc.AudienceSelection{Kind: "allLinked"}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true, ShowModal: true}}}
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'7')`, company, employeeID)
	draft, e := svc.UpdateTaskControlDraftV2(ctx, actor, binding, TaskControlDraftInputV2{1, 1, 2, rules})
	if e != nil {
		t.Fatal("draft", e)
	}
	preview, e := svc.ValidateTaskControlDraftV2(ctx, actor, binding, TaskControlValidateInputV2{draft.Revision})
	if e != nil || preview.(map[string]any)["valid"] != true {
		t.Fatal("validate", preview, e)
	}
	publish := TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedDraftRevision: draft.Revision, ExpectedChecksum: draft.Checksum, ExpectedBindingRevision: 1, ExpectedModeEpoch: 2}
	pub, e := svc.PublishTaskControlDraftV2(ctx, actor, binding, publish)
	if e != nil {
		t.Fatal("publish", e)
	}
	if pub.RuntimeRevision != 1 || pub.Version != 1 {
		t.Fatal(pub)
	}

	// Resolving sources holds installation and org revision pointers until promotion commits.
	fencedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fencedQ := db.New(fencedTx)
	if _, _, _, err = svc.taskControlResolveTx(ctx, fencedQ, connection(), rules, false); err != nil {
		_ = fencedTx.Rollback(ctx)
		t.Fatal(err)
	}
	blocked, boundedCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = pool.Exec(blocked, `UPDATE task_control_installation_snapshots SET checked_at=checked_at+interval '1 second' WHERE company_id=$1`, company)
	boundedCancel()
	if err == nil || blocked.Err() != context.DeadlineExceeded {
		_ = fencedTx.Rollback(ctx)
		t.Fatal("installationpointerchangedwhilepromoting", err)
	}
	blocked, boundedCancel = context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = pool.Exec(blocked, `UPDATE task_control_reference_revisions SET mapping_revision=mapping_revision+1 WHERE company_id=$1`, company)
	boundedCancel()
	if err == nil || blocked.Err() != context.DeadlineExceeded {
		_ = fencedTx.Rollback(ctx)
		t.Fatal("orgpointerchangedwhilepromoting", err)
	}
	if err = fencedTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Unsaved intent must never leak into reconciliation of the active publication.
	changed := rules[0]
	copyRule := *changed.Dynamic
	copyRule.Name = "UNPUBLISHED"
	changed.Dynamic = &copyRule
	newDraft, e := svc.UpdateTaskControlDraftV2(ctx, actor, binding, TaskControlDraftInputV2{draft.Revision, 1, 2, []tc.IntentRule{changed}})
	if e != nil {
		t.Fatal(e)
	}
	replay, e := svc.PublishTaskControlDraftV2(ctx, actor, binding, publish)
	if e != nil || replay.ID != pub.ID {
		t.Fatal("durable idempotency afterdraftchange", replay, e)
	}
	exec(`UPDATE task_control_connections SET mode='managed' WHERE company_id=$1 AND id=$2`, company, binding)
	if _, e = svc.ExchangeTaskControlSession(ctx, assertion("runtime", uuid.New())); e == nil {
		t.Fatal("old client negotiatedv2")
	}
	session, e := svc.ExchangeTaskControlSessionNegotiated(ctx, assertion("runtime", uuid.New()), "1.2.0", []string{"dynamicRulesV2"})
	if e != nil {
		t.Fatal("session", e)
	}
	envelope, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || len(envelope.RuntimeRules) != 1 || envelope.RuntimeRules[0].Name != "Dynamic" || envelope.RuntimeRevision != 1 || len(envelope.RuntimeRules[0].Users) != 1 || envelope.RuntimeRules[0].Users[0] != 7 {
		t.Fatal("runtime", envelope, e)
	}
	if _, e = svc.TaskControlBootstrap(ctx, session.AccessToken); e == nil {
		t.Fatal("v1 runtime emittedv2")
	}
	ack := TaskControlAckInputV2{envelope.Scope, uuid.New(), "7", envelope.SnapshotID, envelope.PublicationID, envelope.PublicationVersion, envelope.RuntimeRevision, envelope.PayloadChecksum, now, "1.2.0"}
	if e = svc.AckTaskControlV2(ctx, session.AccessToken, ack); e != nil {
		t.Fatal("ack", e)
	}
	bad := ack
	bad.PayloadChecksum = "bad"
	if e = svc.AckTaskControlV2(ctx, session.AccessToken, bad); e == nil {
		t.Fatal("forgedackaccepted")
	}
	application, e := svc.TaskControlApplicationV2(ctx, actor, binding, pub.ID)
	if e != nil || application.(map[string]any)["runtimeState"] != "appliedByRuntime" {
		t.Fatal(application, e)
	}
	// New mapping changes provenance even when this actor's payload is unchanged.
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'8')`, company, ownerID)
	if e = q.DrainTaskControlV2SourceChanges(ctx); e != nil {
		t.Fatal(e)
	}
	worked, e := svc.resolveTaskControlV2Job(ctx)
	if e != nil || !worked {
		t.Fatal("worker", worked, e)
	}
	next, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || next.RuntimeRevision <= envelope.RuntimeRevision || next.PublicationID != pub.ID || next.PayloadChecksum != envelope.PayloadChecksum {
		t.Fatal("dynamicrevision", next, e)
	}
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	same, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || same.RuntimeRevision != next.RuntimeRevision {
		t.Fatal("duplicatejoballocatedrevision", same, e)
	}
	if e = svc.AckTaskControlV2(ctx, session.AccessToken, ack); e != nil {
		t.Fatal("historicalack", e)
	}
	application, e = svc.TaskControlApplicationV2(ctx, actor, binding, pub.ID)
	if e != nil || application.(map[string]any)["runtimeState"] == "appliedByRuntime" {
		t.Fatal("oldackmarkedcurrent", application, e)
	}
	gotDraft, e := svc.GetTaskControlDraftV2(ctx, actor, binding)
	if e != nil || gotDraft.Revision != newDraft.Revision || gotDraft.IntentRules[0].Dynamic.Name != "UNPUBLISHED" {
		t.Fatal("workerchangedDraft", gotDraft, e)
	}

	// Repeated ACKs heartbeat an unchanged revision beyond the first five minutes.
	currentAck := TaskControlAckInputV2{next.Scope, uuid.New(), "7", next.SnapshotID, next.PublicationID, next.PublicationVersion, next.RuntimeRevision, next.PayloadChecksum, now, "1.2.0"}
	if e = svc.AckTaskControlV2(ctx, session.AccessToken, currentAck); e != nil {
		t.Fatal(e)
	}
	now = now.Add(6 * time.Minute)
	saveSource()
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	session, e = svc.ExchangeTaskControlSessionNegotiated(ctx, assertion("runtime", uuid.New()), "1.2.0", []string{"dynamicRulesV2"})
	if e != nil {
		t.Fatal(e)
	}
	heartbeat, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || heartbeat.RuntimeRevision != next.RuntimeRevision {
		t.Fatal("unchangedsourceallocatedrevision", heartbeat, e)
	}
	currentAck.AppliedAt = now
	if e = svc.AckTaskControlV2(ctx, session.AccessToken, currentAck); e != nil {
		t.Fatal("ACKheartbeat", e)
	}
	application, e = svc.TaskControlApplicationV2(ctx, actor, binding, pub.ID)
	if e != nil || application.(map[string]any)["runtimeState"] != "appliedByRuntime" {
		t.Fatal("heartbeatnotcurrent", application, e)
	}
	// A new CRM open stage changes this actor's payload at the same publication version.
	core.snapshot.Pipelines[0].Statuses = append(core.snapshot.Pipelines[0].Statuses, tc.ReferenceStatus{ID: "12", Name: "New stage", Sort: 4, Kind: "open"})
	core.snapshot.Revision, _ = tc.ReferenceRevision(core.snapshot)
	sourceRaw, _ := json.Marshal(core.snapshot)
	exec(`UPDATE task_control_reference_cache SET payload=$1 WHERE company_id=$2 AND binding_id=$3`, sourceRaw, company, binding)
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	expanded, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || expanded.RuntimeRevision <= heartbeat.RuntimeRevision || expanded.PublicationVersion != pub.Version || expanded.PayloadChecksum == heartbeat.PayloadChecksum || len(expanded.RuntimeRules[0].Pipelines[0].StatusIDs) != 2 {
		t.Fatal("newstageignored", expanded, e)
	}
	// Unlinking the actor immediately removes his rows after the durable resolver job.
	exec(`DELETE FROM task_control_employee_mappings WHERE company_id=$1 AND employee_id=$2`, company, employeeID)
	if e = q.DrainTaskControlV2SourceChanges(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	excluded, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || len(excluded.RuntimeRules) != 0 || excluded.RuntimeRevision <= expanded.RuntimeRevision {
		t.Fatal("unlinkedactorstillguarded", excluded, e)
	}
	// Source expiry makes issuance fail-open; a retry cannot extend the old lease.
	now = now.Add(6 * time.Minute)
	core.status.CheckedAt = now
	core.status.FreshUntil = now.Add(5 * time.Minute)
	session, e = svc.ExchangeTaskControlSessionNegotiated(ctx, assertion("runtime", uuid.New()), "1.2.0", []string{"dynamicRulesV2"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.TaskControlBootstrapV2(ctx, session.AccessToken); e == nil {
		t.Fatal("expiredsourceissuedruntime")
	}
	saveSource()
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.TaskControlBootstrapV2(ctx, session.AccessToken); e != nil {
		t.Fatal("source recovery", e)
	}
	// No references: publication and runtime remain unchanged; issued lease cannot be extended.
	exec(`UPDATE task_control_reference_cache SET last_error='references_denied' WHERE company_id=$1`, company)
	if _, e = svc.TaskControlBootstrapV2(ctx, session.AccessToken); e == nil {
		t.Fatal("revokedsourceissuedlease")
	}
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if worked, err := svc.resolveTaskControlV2Job(ctx); err != nil || !worked {
		t.Fatal("failedbindingshouldnotpoisonqueue", worked, err)
	}
	var pendingJobs int
	if e = pool.QueryRow(ctx, `SELECT count(*)FROM task_control_v2_jobs WHERE company_id=$1 AND binding_id=$2`, company, binding).Scan(&pendingJobs); e != nil || pendingJobs != 0 {
		t.Fatal("poisonjobnotdequeued", pendingJobs, e)
	}
	if _, e = q.GetTaskControlV2Failure(ctx, db.GetTaskControlV2FailureParams{CompanyID: company, BindingID: binding}); e != nil {
		t.Fatal("missingfailurediagnostic", e)
	}

	publish.RequestID = uuid.New()
	publish.ExpectedDraftRevision = newDraft.Revision
	publish.ExpectedChecksum = newDraft.Checksum
	if _, e = svc.PublishTaskControlDraftV2(ctx, actor, binding, publish); e == nil {
		t.Fatal("publishedwithoutfreshsource")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM task_control_publications WHERE company_id=$1`, company).Scan(&count); e != nil || count != 1 {
		t.Fatal("failedpublishallocatedversion", count, e)
	}
	// Department descendants remain intent: moving a child changes runtime, never the draft.
	saveSource()
	if e = q.QueueTaskControlV2Jobs(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = q.GetTaskControlV2Failure(ctx, db.GetTaskControlV2FailureParams{CompanyID: company, BindingID: binding}); !isNoRows(e) {
		t.Fatal("recoveredbindingkeptfailure", e)
	}
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'7')`, company, employeeID)
	parent, child := uuid.New(), uuid.New()
	exec(`INSERT INTO departments(id,company_id,name,parent_id,"order") VALUES($1,$2,'Parent',(SELECT id FROM departments WHERE company_id=$2 AND source='system'),99)`, parent, company)
	exec(`INSERT INTO departments(id,company_id,name,parent_id,"order")VALUES($1,$2,'Child',$3,0)`, child, company, parent)
	exec(`INSERT INTO user_departments(company_id,user_id,department_id)VALUES($1,$2,$3)`, company, employeeID, child)
	selected := *rules[0].Dynamic
	selected.Audience = tc.AudienceSelection{Kind: "selected", EmployeeIDs: []uuid.UUID{}, Departments: []tc.DepartmentSelection{{DepartmentID: parent, IncludeDescendants: true}}}
	departmentDraft, e := svc.UpdateTaskControlDraftV2(ctx, actor, binding, TaskControlDraftInputV2{newDraft.Revision, 1, 2, []tc.IntentRule{{Kind: "dynamic", Dynamic: &selected}}})
	if e != nil {
		t.Fatal(e)
	}
	departmentPub, e := svc.PublishTaskControlDraftV2(ctx, actor, binding, TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedDraftRevision: departmentDraft.Revision, ExpectedChecksum: departmentDraft.Checksum, ExpectedBindingRevision: 1, ExpectedModeEpoch: 2})
	if e != nil {
		t.Fatal(e)
	}
	departmentRuntime, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || len(departmentRuntime.RuntimeRules) != 1 {
		t.Fatal("descendantmissing", departmentRuntime, e)
	}
	exec(`UPDATE departments SET "order"=100,parent_id=(SELECT id FROM departments WHERE company_id=$1 AND source='system') WHERE company_id=$1 AND id=$2`, company, child)
	if e = q.DrainTaskControlV2SourceChanges(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.resolveTaskControlV2Job(ctx); e != nil {
		t.Fatal(e)
	}
	moved, e := svc.TaskControlBootstrapV2(ctx, session.AccessToken)
	if e != nil || len(moved.RuntimeRules) != 0 || moved.PublicationID != departmentPub.ID || moved.RuntimeRevision <= departmentRuntime.RuntimeRevision {
		t.Fatal("movedchildstillincluded", moved, e)
	}

}
