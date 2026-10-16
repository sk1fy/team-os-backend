//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type r05Fixture struct {
	ctx                  context.Context
	pool                 *pgxpool.Pool
	svc                  *Service
	actor, reader, other Actor
	binding              uuid.UUID
}

func newR05Fixture(t *testing.T) r05Fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	pool := companyAccessTestPool(t, ctx)
	company, owner, employee, admin, binding, install, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, owner, []accessTestUser{{owner, "owner", "active"}, {employee, "employee", "active"}, {admin, "admin", "active"}})
	now := time.Now().UTC().Truncate(time.Second)
	core := &referenceTestCore{status: tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute), RuntimeVersion: "1.2.1", RuntimeCapabilities: []string{"dynamicRulesV2"}}}
	f := r05Fixture{ctx, pool, &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core}, Actor{CompanyID: company, UserID: owner}, Actor{CompanyID: company, UserID: employee}, Actor{CompanyID: company, UserID: admin}, binding}
	f.exec(t, `INSERT INTO company_integrations(id,company_id,provider,external_account_id,status)VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company)
	f.exec(t, `INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id)VALUES($1,$2,$3,$4,'123',1,$5,$6,'7')`, company, binding, install, integration, uuid.New(), owner)
	f.exec(t, `INSERT INTO task_control_installation_snapshots(company_id,account_id,payload,checked_at,fresh_until)VALUES($1,'123',$2,$3,$4)`, company, mustR05JSON(t, core.status), now, now.Add(5*time.Minute))
	f.exec(t, `INSERT INTO task_control_permissions(company_id,user_id,can_read,can_manage)VALUES($1,$2,true,false)`, company, employee)
	return f
}
func (f r05Fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, sql, args...); e != nil {
		t.Fatal(e)
	}
}
func mustR05JSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func (f r05Fixture) publish(t *testing.T) TaskControlPublication {
	t.Helper()
	d, e := f.svc.GetTaskControlDraft(f.ctx, f.actor, f.binding)
	if e != nil {
		t.Fatal(e)
	}
	// First mutation creates the empty draft; later publications deliberately share its intent.
	if d.Revision == 0 {
		t.Fatal("missing draft revision")
	}
	p, e := f.svc.PublishTaskControlDraft(f.ctx, f.actor, f.binding, TaskControlPublishInput{ExpectedBindingRevision: 1, ExpectedModeEpoch: 1, RequestID: uuid.New(), ExpectedDraftRevision: d.Revision, ExpectedChecksum: d.Checksum, Reason: "Проверка истории"})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func r05Page(t *testing.T, v any) ([]map[string]json.RawMessage, string) {
	t.Helper()
	var page struct {
		Items      []map[string]json.RawMessage `json:"items"`
		NextCursor string                       `json:"nextCursor"`
	}
	if e := json.Unmarshal(mustR05JSON(t, v), &page); e != nil {
		t.Fatal(e)
	}
	return page.Items, page.NextCursor
}
func r05String(t *testing.T, item map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if e := json.Unmarshal(item[key], &s); e != nil {
		t.Fatalf("%s: %v", key, e)
	}
	return s
}

func TestTaskControlR05StablePagingBeyondHundredAndConcurrentPublish(t *testing.T) {
	f := newR05Fixture(t)
	if _, e := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, TaskControlDraftInput{1, 1, 1, []tc.Rule{}}); e != nil {
		t.Fatal(e)
	}
	expected := map[string]bool{}
	for range 113 {
		p := f.publish(t)
		expected[p.ID.String()] = true
	}
	for _, history := range []bool{false, true} {
		seen := map[string]bool{}
		cursor := ""
		pages := 0
		publicationCount := 0
		for {
			var page any
			var e error
			if history {
				page, e = f.svc.TaskControlHistory(f.ctx, f.reader, f.binding, TaskControlHistoryOptions{Cursor: cursor, Limit: 17})
			} else {
				page, e = f.svc.TaskControlPublications(f.ctx, f.reader, f.binding, uuid.Nil, TaskControlPublicationOptionsV2{Cursor: cursor, Limit: 17})
			}
			if e != nil {
				t.Fatal(e)
			}
			items, next := r05Page(t, page)
			for _, item := range items {
				id := r05String(t, item, "id")
				if seen[id] {
					t.Fatalf("duplicate page item %s", id)
				}
				seen[id] = true
				if !history {
					if !expected[id] {
						t.Fatalf("unexpected publication %s", id)
					}
					publicationCount++
				} else if r05String(t, item, "eventType") == "publication_published" {
					publicationCount++
				}
			}
			pages++
			if pages == 1 {
				f.publish(t)
			} // New entries cannot shift or duplicate the remaining keyset pages.
			if next == "" {
				break
			}
			cursor = next
			if pages > 20 {
				t.Fatal("pagination did not terminate")
			}
		}
		want := 113
		if history {
			want++
		} // The publication inserted while reading the prior list is visible initially.
		if publicationCount != want {
			t.Fatalf("history=%v: got %d publications, want %d", history, publicationCount, want)
		}
	}
	foreign := f.reader
	foreign.CompanyID = uuid.New()
	if _, e := f.svc.TaskControlHistory(f.ctx, foreign, f.binding, TaskControlHistoryOptions{}); e == nil {
		t.Fatal("foreign history accepted")
	}
	if _, e := f.svc.TaskControlPublications(f.ctx, foreign, f.binding, uuid.Nil); e == nil {
		t.Fatal("foreign publication list accepted")
	}
	if _, e := f.svc.TaskControlHistory(f.ctx, f.reader, f.binding, TaskControlHistoryOptions{Cursor: "broken"}); e == nil {
		t.Fatal("malformed cursor accepted")
	}
}

func TestTaskControlR05RestoreIdempotencyRightsDraftAndTransaction(t *testing.T) {
	f := newR05Fixture(t)
	if _, e := f.svc.UpdateTaskControlDraft(f.ctx, f.actor, f.binding, TaskControlDraftInput{1, 1, 1, []tc.Rule{}}); e != nil {
		t.Fatal(e)
	}
	source := f.publish(t)
	current := f.publish(t)
	d, e := f.svc.GetTaskControlDraft(f.ctx, f.actor, f.binding)
	if e != nil {
		t.Fatal(e)
	}
	edited, e := f.svc.UpdateTaskControlDraft(f.ctx, f.other, f.binding, TaskControlDraftInput{1, 1, d.Revision, []tc.Rule{}})
	if e != nil {
		t.Fatal(e)
	}
	in := TaskControlRollbackInputV2{RequestID: uuid.New(), SourcePublicationID: source.ID, ExpectedPublishedVersion: current.Version, ExpectedBindingRevision: 1, ExpectedModeEpoch: 1, Reason: "Восстановить проверенную версию"}
	var results [2]TaskControlPublication
	var errs [2]error
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.svc.RollbackTaskControl(f.ctx, f.actor, f.binding, in)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || results[0].ID != results[1].ID || results[0].Version != 3 {
		t.Fatalf("concurrent restore: %+v %+v", results, errs)
	}
	if results[0].RestoredFromID == nil || *results[0].RestoredFromID != source.ID {
		t.Fatal("restore lineage omitted")
	}
	after, e := f.svc.GetTaskControlDraft(f.ctx, f.actor, f.binding)
	if e != nil || after.Revision != edited.Revision || after.UpdatedBy != f.other.UserID {
		t.Fatal("restore replaced another editor's draft", after, e)
	}
	wrong := in
	wrong.Reason = "Измененная операция"
	if _, e = f.svc.RollbackTaskControl(f.ctx, f.actor, f.binding, wrong); e == nil {
		t.Fatal("changed replay accepted")
	}
	if _, e = f.svc.RollbackTaskControl(f.ctx, f.reader, f.binding, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("read-only restored", e)
	}
	foreign := f.actor
	foreign.CompanyID = uuid.New()
	if _, e = f.svc.RollbackTaskControl(f.ctx, foreign, f.binding, in); e == nil {
		t.Fatal("foreign restore accepted")
	}
	f.exec(t, `INSERT INTO task_control_permissions(company_id,user_id,can_read,can_manage)VALUES($1,$2,true,false)`, f.actor.CompanyID, f.actor.UserID)
	if _, e = f.svc.RollbackTaskControl(f.ctx, f.actor, f.binding, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("revoked permissions replayed success", e)
	}
	f.exec(t, `DELETE FROM task_control_permissions WHERE company_id=$1 AND user_id=$2`, f.actor.CompanyID, f.actor.UserID)
	// A failure after publication INSERT must roll back the new version, pointer, audit and idempotency result.
	f.exec(t, `CREATE FUNCTION r05_fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'r05 injected audit failure'; END $$`)
	f.exec(t, `CREATE TRIGGER r05_failure BEFORE INSERT ON task_control_audit_events FOR EACH ROW EXECUTE FUNCTION r05_fail_audit()`)
	next := in
	next.RequestID = uuid.New()
	next.ExpectedPublishedVersion = 3
	if _, e = f.svc.RollbackTaskControl(f.ctx, f.actor, f.binding, next); e == nil {
		t.Fatal("injected transaction failure ignored")
	}
	var count int
	var pointer uuid.UUID
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_control_publications WHERE company_id=$1 AND binding_id=$2`, f.actor.CompanyID, f.binding).Scan(&count); e != nil || count != 3 {
		t.Fatal("failed transaction retained publication", count, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT publication_id FROM task_control_connections WHERE company_id=$1 AND id=$2`, f.actor.CompanyID, f.binding).Scan(&pointer); e != nil || pointer != results[0].ID {
		t.Fatal("failed transaction moved active publication", pointer, e)
	}
	f.exec(t, `DROP TRIGGER r05_failure ON task_control_audit_events`)
	retry, e := f.svc.RollbackTaskControl(f.ctx, f.actor, f.binding, next)
	if e != nil || retry.Version != 4 {
		t.Fatal("rolled back key cannot be retried", retry, e)
	}
	page, e := f.svc.TaskControlHistory(f.ctx, f.reader, f.binding, TaskControlHistoryOptions{Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	items, _ := r05Page(t, page)
	restores := 0
	for _, item := range items {
		if r05String(t, item, "eventType") == "publication_restored" {
			restores++
		}
	}
	if restores != 2 {
		t.Fatalf("restore audit duplicated or absent: %d", restores)
	}
}

func TestTaskControlR05DynamicAuditCurrentReferencesAndRetention(t *testing.T) {
	f := newR05Fixture(t)
	q := db.New(f.pool)
	c, e := q.GetTaskControlConnection(f.ctx, db.GetTaskControlConnectionParams{CompanyID: f.actor.CompanyID, ID: f.binding})
	if e != nil {
		t.Fatal(e)
	}
	now := f.svc.now()
	core := f.svc.taskControlCore.(*referenceTestCore)
	refs := tc.ReferenceSnapshot{SchemaVersion: 1, Scope: referenceScope(c), Complete: true, Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute), Users: []tc.ReferenceUser{{ID: "7", IsActive: true}, {ID: "8", IsActive: true}}, Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{{ID: "10", Statuses: []tc.ReferenceStatus{{ID: "11", Kind: "open"}}}}, Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
	refs.Revision, e = tc.ReferenceRevision(refs)
	if e != nil {
		t.Fatal(e)
	}
	core.snapshot = refs
	f.exec(t, `INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload)VALUES($1,$2,$3,$4)`, c.CompanyID, c.ID, referenceScopeKey(c), mustR05JSON(t, refs))
	f.exec(t, `INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'7')`, c.CompanyID, f.actor.UserID)
	if _, e = f.svc.ActivateTaskControlV2(f.ctx, f.actor, c.ID, TaskControlActivateInputV2{uuid.New(), 1, 1}); e != nil {
		t.Fatal(e)
	}
	rules := []tc.IntentRule{{Kind: "dynamic", Dynamic: &tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Все связанные", Audience: tc.AudienceSelection{Kind: "allLinked"}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true, ShowModal: true}}}
	draft, e := f.svc.UpdateTaskControlDraftV2(f.ctx, f.actor, c.ID, TaskControlDraftInputV2{1, 1, 2, rules})
	if e != nil {
		t.Fatal(e)
	}
	original, e := f.svc.PublishTaskControlDraftV2(f.ctx, f.actor, c.ID, TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedBindingRevision: 1, ExpectedModeEpoch: 2, ExpectedDraftRevision: draft.Revision, ExpectedChecksum: draft.Checksum})
	if e != nil {
		t.Fatal(e)
	}
	public, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	f.svc.taskControlKeys = map[string]ed25519.PublicKey{"r05": public}
	request := uuid.New()
	assertion := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: c.CompanyID, InstallationID: c.InstallationID, IntegrationID: c.IntegrationID, AccountID: c.AccountID, ActorID: "7", Product: tc.Product, Generation: 1, Admin: true, Purpose: "mode", RequestID: request}
	prefix := "r05." + base64.RawURLEncoding.EncodeToString(mustR05JSON(t, assertion))
	proof := prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("teamos-task-control/v1\n"+prefix)))
	preview, e := f.svc.PreviewTaskControlModeV2(f.ctx, f.actor, c.ID, TaskControlModePreviewInputV2{"managed", original.ID})
	if e != nil {
		t.Fatal(e)
	}
	m := preview.(map[string]any)
	if _, e = f.svc.SetTaskControlModeV2(f.ctx, f.actor, c.ID, TaskControlModeInputV2{RequestID: request, ExpectedBindingRevision: 1, ExpectedModeEpoch: 2, TargetMode: "managed", PublicationID: original.ID, CoreAssertion: proof, ExportChecksum: m["exportChecksum"].(string), ExpectedSourceRevisions: m["sourceRevisions"].(TaskControlSourceRevisions), ConfirmDynamicLoss: true, ConfirmEmptyExport: true}); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'8')`, c.CompanyID, f.other.UserID)
	if e = q.QueueTaskControlV2Jobs(f.ctx); e != nil {
		t.Fatal(e)
	}
	if worked, err := f.svc.resolveTaskControlV2Job(f.ctx); err != nil || !worked {
		t.Fatal("resolver", worked, err)
	}
	restored, e := f.svc.RollbackTaskControlV2(f.ctx, f.actor, c.ID, TaskControlRollbackInputV2{RequestID: uuid.New(), SourcePublicationID: original.ID, ExpectedPublishedVersion: 1, ExpectedBindingRevision: 1, ExpectedModeEpoch: 3, Reason: "Текущее содержимое справочников"})
	if e != nil {
		t.Fatal(e)
	}
	if restored.IntentChecksum != original.IntentChecksum || restored.Version != 2 {
		t.Fatal("restore changed immutable intent", restored)
	}
	snapshot, e := q.GetTaskControlV2Snapshot(f.ctx, db.GetTaskControlV2SnapshotParams{CompanyID: c.CompanyID, BindingID: c.ID, ID: restored.InitialSnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	var resolved TaskControlResolvedSnapshot
	if e = json.Unmarshal(snapshot.Payload, &resolved); e != nil || len(resolved.RuntimeRules) != 1 || len(resolved.RuntimeRules[0].Users) != 2 {
		t.Fatal("restore reused historical employees", resolved, e)
	}
	history, e := f.svc.TaskControlHistory(f.ctx, f.reader, c.ID, TaskControlHistoryOptions{Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	items, _ := r05Page(t, history)
	events := map[string]int{}
	var recomputeID string
	var recomputeSnapshotID string
	dynamicDiffFound := false
	for _, item := range items {
		kind := r05String(t, item, "eventType")
		events[kind]++
		if kind == "draft_updated" {
			var details struct {
				RuleChanges []struct {
					UID string `json:"uid"`
				} `json:"ruleChanges"`
			}
			if e = json.Unmarshal(item["details"], &details); e != nil {
				t.Fatal(e)
			}
			for _, change := range details.RuleChanges {
				if change.UID == rules[0].Dynamic.RuleUID.String() {
					dynamicDiffFound = true
				}
			}
		}
		if kind == "mode_changed" {
			if actor := r05String(t, item, "actorId"); actor != f.actor.UserID.String() {
				t.Fatal("mode actor missing", actor)
			}
		}
		if kind == "runtime_recomputed" {
			recomputeID = r05String(t, item, "id")
			var details map[string]json.RawMessage
			if e = json.Unmarshal(item["details"], &details); e != nil || len(details["sourceRevisions"]) == 0 || r05String(t, details, "intentChecksum") != original.IntentChecksum {
				t.Fatal("recompute provenance", details, e)
			}
			recomputeSnapshotID = r05String(t, details, "snapshotId")
			if string(item["actorId"]) != "null" {
				t.Fatal("worker attributed to human", string(item["actorId"]))
			}
		}
	}
	if events["publication_published"] != 1 || events["publication_restored"] != 1 || events["mode_changed"] != 1 || events["runtime_recomputed"] < 1 {
		t.Fatal("intent and recompute audit indistinguishable", events)
	}
	if !dynamicDiffFound {
		t.Fatal("dynamic draft diff lost rule UID")
	}
	// Expired materializations can be pruned without removing durable audit provenance.
	f.exec(t, `UPDATE task_control_v2_snapshots SET created_at=now()-interval '31 days' WHERE company_id=$1 AND id<>$2`, c.CompanyID, restored.InitialSnapshotID)
	if e = q.PruneTaskControlV2Snapshots(f.ctx); e != nil {
		t.Fatal(e)
	}
	var snapshotRetained bool
	if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM task_control_v2_snapshots WHERE id=$1)`, recomputeSnapshotID).Scan(&snapshotRetained); e != nil || snapshotRetained {
		t.Fatal("expired worker snapshot was not pruned", snapshotRetained, e)
	}
	var retained bool
	if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM task_control_audit_events WHERE id=$1)`, recomputeID).Scan(&retained); e != nil || !retained {
		t.Fatal("snapshot retention pruned audit", retained, e)
	}
	if _, e = f.pool.Exec(f.ctx, `DELETE FROM task_control_audit_events WHERE id=$1`, recomputeID); e == nil {
		t.Fatal("append-only audit deleted")
	}
}
