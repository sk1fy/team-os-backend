//go:build integration

package application

import (
	"crypto/sha256"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlR08DynamicReleaseMembershipScopesAndEmptyProjection(t *testing.T) {
	f := newR05Fixture(t)
	q := db.New(f.pool)
	c, e := q.GetTaskControlConnection(f.ctx, db.GetTaskControlConnectionParams{CompanyID: f.actor.CompanyID, ID: f.binding})
	if e != nil {
		t.Fatal(e)
	}
	core := f.svc.taskControlCore.(*referenceTestCore)
	now := f.svc.now()
	refs := tc.ReferenceSnapshot{SchemaVersion: 1, Scope: referenceScope(c), Complete: true, Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute), Users: []tc.ReferenceUser{{ID: "7", Name: "Первый", IsActive: true}, {ID: "8", Name: "Второй", IsActive: true}}, Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{{ID: "10", Name: "Продажи", Sort: 1, Statuses: []tc.ReferenceStatus{{ID: "11", Name: "Открыт", Sort: 1, Kind: "open"}, {ID: "142", Name: "Успех", Sort: 2, Kind: "won"}, {ID: "143", Name: "Отказ", Sort: 3, Kind: "lost"}}}}, Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
	saveSource := func() {
		t.Helper()
		c, e = q.GetTaskControlConnection(f.ctx, db.GetTaskControlConnectionParams{CompanyID: f.actor.CompanyID, ID: f.binding})
		if e != nil {
			t.Fatal(e)
		}
		refs.Revision, e = tc.ReferenceRevision(refs)
		if e != nil {
			t.Fatal(e)
		}
		core.snapshot = refs
		f.exec(t, `INSERT INTO task_control_reference_cache(company_id,binding_id,scope_key,payload)VALUES($1,$2,$3,$4)ON CONFLICT(company_id,binding_id,scope_key)DO UPDATE SET payload=excluded.payload,last_error=NULL`, f.actor.CompanyID, f.binding, referenceScopeKey(c), mustR05JSON(t, refs))
	}
	saveSource()
	department := uuid.New()
	f.exec(t, `INSERT INTO departments(id,company_id,name,"order")VALUES($1,$2,'Продажи',100)`, department, f.actor.CompanyID)
	f.exec(t, `INSERT INTO user_departments(company_id,user_id,department_id)VALUES($1,$2,$3)`, f.actor.CompanyID, f.reader.UserID, department)
	f.exec(t, `INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id)VALUES($1,'123',$2,'7'),($1,'123',$3,'8')`, f.actor.CompanyID, f.reader.UserID, f.other.UserID)
	if _, e = f.svc.ActivateTaskControlV2(f.ctx, f.actor, f.binding, TaskControlActivateInputV2{uuid.New(), 1, 1}); e != nil {
		t.Fatal(e)
	}
	dynamic := tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Отдел: все будущие этапы", Audience: tc.AudienceSelection{Kind: "selected", EmployeeIDs: []uuid.UUID{}, Departments: []tc.DepartmentSelection{{DepartmentID: department, IncludeDescendants: true}}}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true, ShowModal: true}
	selected := dynamic
	selected.RuleUID = uuid.New()
	selected.Name = "Пересекающийся явный этап"
	selected.Leads = tc.LeadSelection{Kind: "selected", Pipelines: []tc.PipelineSelection{{PipelineID: "10", Stages: tc.StageSelection{Kind: "selected", StatusIDs: []string{"11"}}}}}
	selected.ShowModal = false
	selected.DenyExit = true
	selected.CheckResponsible = true
	rules := []tc.IntentRule{{Kind: "dynamic", Dynamic: &dynamic}, {Kind: "dynamic", Dynamic: &selected}}
	draft, e := f.svc.UpdateTaskControlDraftV2(f.ctx, f.actor, f.binding, TaskControlDraftInputV2{1, 1, 2, rules})
	if e != nil {
		t.Fatal(e)
	}
	pub, e := f.svc.PublishTaskControlDraftV2(f.ctx, f.actor, f.binding, TaskControlPublishInputV2{RequestID: uuid.New(), ExpectedBindingRevision: 1, ExpectedModeEpoch: 2, ExpectedDraftRevision: draft.Revision, ExpectedChecksum: draft.Checksum})
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE task_control_connections SET mode='managed' WHERE company_id=$1 AND id=$2`, f.actor.CompanyID, f.binding)
	token := func(actor string) string {
		t.Helper()
		token := strings.Repeat(actor, 43)
		hash := sha256.Sum256([]byte(token))
		f.exec(t, `INSERT INTO task_control_runtime_sessions(token_hash,company_id,binding_id,actor_id,binding_revision,mode_epoch,generation,expires_at)VALUES($1,$2,$3,$4,1,2,1,$5)`, hash[:], f.actor.CompanyID, f.binding, actor, now.Add(5*time.Minute))
		return token
	}
	one, two := token("7"), token("8")
	bootstrap := func(token string) TaskControlRuntimeEnvelopeV2 {
		t.Helper()
		out, err := f.svc.TaskControlBootstrapV2(f.ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		if out.PublicationID != pub.ID || out.PublicationVersion != pub.Version || out.IntentChecksum != pub.IntentChecksum || out.Scope.ModeEpoch != 2 || out.Scope.BindingID != f.binding || out.Scope.AccountID != "123" {
			t.Fatal("scope or publication changed", out)
		}
		return out
	}
	reconcile := func() {
		t.Helper()
		if err := q.DrainTaskControlV2SourceChanges(f.ctx); err != nil {
			t.Fatal(err)
		}
		// The normal sweep also picks up refreshed CRM references without an org event.
		if err := q.QueueTaskControlV2Jobs(f.ctx); err != nil {
			t.Fatal(err)
		}
		worked, err := f.svc.resolveTaskControlV2Job(f.ctx)
		if err != nil || !worked {
			t.Fatal(worked, err)
		}
	}
	first := bootstrap(one)
	if len(first.RuntimeRules) != 2 || len(bootstrap(two).RuntimeRules) != 0 {
		t.Fatal("initial actor projection", first)
	}
	// A linked employee joins the selected department, with no draft/publish action.
	f.exec(t, `INSERT INTO user_departments(company_id,user_id,department_id)VALUES($1,$2,$3)`, f.actor.CompanyID, f.other.UserID, department)
	reconcile()
	joined := bootstrap(two)
	if joined.RuntimeRevision <= first.RuntimeRevision || len(joined.RuntimeRules) != 2 {
		t.Fatal("new member not projected", joined)
	}
	for _, r := range joined.RuntimeRules {
		if !reflect.DeepEqual(r.Users, []int64{8}) {
			t.Fatal("other actor identity leaked", r)
		}
	}
	// New stages and pipelines enter allOpen but never the selected stage rule.
	refs.Pipelines[0].Statuses = append(refs.Pipelines[0].Statuses, tc.ReferenceStatus{ID: "12", Name: "Новый", Kind: "open", Sort: 4})
	refs.Pipelines = append(refs.Pipelines, tc.ReferencePipeline{ID: "20", Name: "Новая воронка", Sort: 2, Statuses: []tc.ReferenceStatus{{ID: "21", Name: "Открыт", Kind: "open", Sort: 1}, {ID: "142", Name: "Успех", Kind: "won", Sort: 2}, {ID: "143", Name: "Отказ", Kind: "lost", Sort: 3}}})
	saveSource()
	reconcile()
	expanded := bootstrap(one)
	if expanded.RuntimeRevision <= joined.RuntimeRevision {
		t.Fatal("CRM change did not advance runtime revision")
	}
	for _, r := range expanded.RuntimeRules {
		if r.UID == dynamic.RuleUID.String() {
			if !reflect.DeepEqual(r.Pipelines, []tc.LegacyPipeline{{PipelineID: 10, StatusIDs: []int64{11, 12}}, {PipelineID: 20, StatusIDs: []int64{21}}}) {
				t.Fatal("future open scope", r)
			}
		} else if r.UID == selected.RuleUID.String() {
			if !reflect.DeepEqual(r.Pipelines, []tc.LegacyPipeline{{PipelineID: 10, StatusIDs: []int64{11}}}) || !r.DenyExit || !r.CheckResponsible || r.ShowModal {
				t.Fatal("overlap changed explicit scope/flags", r)
			}
		} else {
			t.Fatal("unexpected runtime rule", r)
		}
	}
	f.exec(t, `DELETE FROM user_departments WHERE company_id=$1 AND user_id=$2`, f.actor.CompanyID, f.reader.UserID)
	reconcile()
	removed := bootstrap(one)
	if len(removed.RuntimeRules) != 0 || removed.RuntimeRules == nil || removed.RuntimeRevision <= expanded.RuntimeRevision {
		t.Fatal("removed employee retained restriction or empty became all", removed)
	}
	if len(bootstrap(two).RuntimeRules) != 2 {
		t.Fatal("removing one employee affected another")
	}
	f.exec(t, `DELETE FROM user_departments WHERE company_id=$1`, f.actor.CompanyID)
	reconcile()
	empty := bootstrap(two)
	if len(empty.RuntimeRules) != 0 || empty.RuntimeRules == nil || empty.RuntimeRevision <= removed.RuntimeRevision {
		t.Fatal("empty department retained prior snapshot", empty)
	}
	// Returning to the department rematerializes the same immutable intent.
	f.exec(t, `INSERT INTO user_departments(company_id,user_id,department_id)VALUES($1,$2,$3)`, f.actor.CompanyID, f.reader.UserID, department)
	reconcile()
	if returned := bootstrap(one); len(returned.RuntimeRules) != 2 || returned.RuntimeRevision <= empty.RuntimeRevision {
		t.Fatal("returning member not restored", returned)
	}
	current, e := f.svc.GetTaskControlDraftV2(f.ctx, f.actor, f.binding)
	if e != nil || current.Revision != draft.Revision || current.Checksum != draft.Checksum {
		t.Fatal("resolver rewrote saved intent", current, e)
	}
	var publications int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_control_publications WHERE company_id=$1 AND binding_id=$2`, f.actor.CompanyID, f.binding).Scan(&publications); e != nil || publications != 1 {
		t.Fatal("source reconciliation published a new intent", publications, e)
	}
}
