//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
	"github.com/sk1fy/team-os-backend/services/company/internal/taskcontrolbridge"
)

type referenceTestCore struct {
	status   tc.Installation
	snapshot tc.ReferenceSnapshot
	err      error
	calls    int
	during   func()
}

func (c *referenceTestCore) Installation(context.Context, uuid.UUID, string) (tc.Installation, error) {
	return c.status, nil
}
func (c *referenceTestCore) References(context.Context, uuid.UUID, string) (tc.ReferenceSnapshot, error) {
	c.calls++
	if c.during != nil {
		c.during()
	}
	return c.snapshot, c.err
}
func TestTaskControlReferencesDurableIsolationAndRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, employeeID, binding, install, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}, {employeeID, "employee", "active"}})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO company_integrations(id,company_id,provider,external_account_id,status) VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company)
	exec(`INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id) VALUES($1,$2,$3,$4,'123',1,$5,$6,'1')`, company, binding, install, integration, uuid.New(), ownerID)
	now := time.Now().UTC().Truncate(time.Second)
	actor := Actor{CompanyID: company, UserID: ownerID}
	core := &referenceTestCore{status: tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute)}}
	core.snapshot = tc.ReferenceSnapshot{SchemaVersion: 1, Scope: tc.ReferenceScope{CompanyID: company, AccountID: "123", InstallationID: install, IntegrationID: integration, Generation: 1, Product: tc.Product}, Complete: true, Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute), Users: []tc.ReferenceUser{{ID: "7", Name: "CRM employee", IsActive: true}}, Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{}, Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
	core.snapshot.Revision, _ = tc.ReferenceRevision(core.snapshot)
	svc := &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: core}
	q := db.New(pool)
	before, e := svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{})
	if e != nil || core.calls != 0 || before.CRM != nil || before.State != "refreshing" {
		t.Fatalf("first GET must return immediately with durable job: %+v %v calls%d", before, e, core.calls)
	}
	again, err := svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{})
	if err != nil || again.State != "refreshing" {
		t.Fatal(again, err)
	}
	var queued int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM task_control_reference_refreshes WHERE company_id=$1`, company).Scan(&queued); err != nil || queued != 1 {
		t.Fatal("cold GET not coalesced", queued, err)
	}
	job, e := q.ClaimTaskControlReferenceRefresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	svc.runReferenceRefresh(ctx, job)
	// Read-only access exposes the selector, but cannot trigger explicit refresh.
	exec(`INSERT INTO task_control_permissions(company_id,user_id,can_read,can_manage) VALUES($1,$2,true,false)`, company, employeeID)
	readOnly := Actor{CompanyID: company, UserID: employeeID}
	if _, err = svc.GetTaskControlReferences(ctx, readOnly, binding, TaskControlReferenceOptions{}); err != nil {
		t.Fatal("read-only selector", err)
	}
	if _, err = svc.RequestTaskControlReferencesRefresh(ctx, readOnly, binding); !isCompanyError(err, ErrorForbidden) {
		t.Fatal("read-only refreshed", err)
	}
	// Exact totals and paging continue beyond the old panel's hundred-person cap.
	exec(`INSERT INTO users(id,company_id,email,first_name,last_name,role,status) SELECT gen_random_uuid(),$1,'page-'||n||'@example.com','Page',n::text,'employee','active' FROM generate_series(1,113) n`, company)
	page, err := svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{Offset: 100, Limit: 10, WithoutDepartment: true, SelectedEmployeeIDs: []uuid.UUID{employeeID}})
	if err != nil || page.Total != 115 || len(page.Employees) != 10 || len(page.SelectedEmployees) != 1 || len(page.SelectedEmployees[0].DepartmentIDs) != 0 {
		t.Fatalf("paging/exact count/no department: %+v %v", page, err)
	}
	orgRevision := page.OrganizationRevision
	exec(`UPDATE users SET first_name='Renamed' WHERE company_id=$1 AND id=$2`, company, employeeID)
	page, err = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{Limit: 1})
	if err != nil || page.OrganizationRevision <= orgRevision {
		t.Fatal("organization revision not bumped", page, err)
	}
	// Direct department membership includes a position's owning department.
	department, position := uuid.New(), uuid.New()
	exec(`INSERT INTO departments(id,company_id,name,parent_id,"order") VALUES($1,$2,'Reference department',(SELECT id FROM departments WHERE company_id=$2 AND source='system'),0)`, department, company)
	exec(`INSERT INTO positions(id,company_id,name,department_id) VALUES($1,$2,'Engineer',$3)`, position, company, department)
	exec(`INSERT INTO user_positions(company_id,user_id,position_id) VALUES($1,$2,$3)`, company, employeeID, position)
	page, err = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{DepartmentID: &department, SelectedEmployeeIDs: []uuid.UUID{employeeID}})
	if err != nil || page.Total != 1 || len(page.Employees) != 1 || page.Employees[0].PositionName != "Engineer" || len(page.Employees[0].DepartmentIDs) != 1 {
		t.Fatalf("position department absent: %+v %v", page, err)
	}
	// Mapping from a different account is never usable even when IDs/names match.
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id) VALUES($1,'999',$2,'7')`, company, employeeID)
	got, e := svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{SelectedEmployeeIDs: []uuid.UUID{employeeID}})
	if e != nil || got.State != "fresh" || len(got.SelectedEmployees) != 1 || *got.SelectedEmployees[0].IneligibleReason != "mapping_missing" {
		t.Fatalf("foreign mapping accepted: %+v %v", got, e)
	}
	rev := got.MappingRevision
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id) VALUES($1,'123',$2,'7')`, company, employeeID)
	missing := uuid.New()
	got, e = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{Search: "nonexistent needle", Limit: 1, SelectedEmployeeIDs: []uuid.UUID{employeeID, missing}})
	if e != nil || got.Total != 0 || len(got.Employees) != 0 || len(got.SelectedEmployees) != 2 || !got.SelectedEmployees[0].Eligible || got.SelectedEmployees[1].IneligibleReason == nil || got.MappingRevision <= rev {
		t.Fatalf("selection preservation/revision: %+v %v", got, e)
	}
	// A revoked references capability invalidates even a still-fresh snapshot.
	core.err = taskcontrolbridge.ReferenceError{Code: "references_denied"}
	refresh, e := svc.RequestTaskControlReferencesRefresh(ctx, actor, binding)
	if e != nil {
		t.Fatal(e)
	}
	job, e = q.ClaimTaskControlReferenceRefresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	svc.runReferenceRefresh(ctx, job)
	status, e := svc.GetTaskControlReferencesRefresh(ctx, actor, binding, refresh.RefreshID)
	if e != nil || status.State != "failed" || status.ErrorCode == nil || *status.ErrorCode != "references_denied" {
		t.Fatalf("lost Core error: %+v %v", status, e)
	}
	got, e = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{})
	if e != nil || got.CRM != nil || got.State != "error" {
		t.Fatalf("denied capability retained data: %+v %v", got, e)
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: company, ID: binding})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.taskControlFreshReferencesTx(ctx, q, c); e == nil {
		t.Fatal("denied capability accepted for publication")
	}
	// A former lease cannot complete a refresh reclaimed after a crash.
	core.err = nil
	_, e = svc.RequestTaskControlReferencesRefresh(ctx, actor, binding)
	if e != nil {
		t.Fatal(e)
	}
	job, e = q.ClaimTaskControlReferenceRefresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE task_control_reference_refreshes SET lease_until=now()-interval '1 minute' WHERE id=$1`, job.ID)
	newer, e := q.ClaimTaskControlReferenceRefresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	svc.runReferenceRefresh(ctx, job)
	current, e := q.GetTaskControlReferenceRefresh(ctx, db.GetTaskControlReferenceRefreshParams{CompanyID: company, BindingID: binding, ScopeKey: referenceScopeKey(c), ID: job.ID})
	if e != nil || current.State != "running" {
		t.Fatal("expired lease overwrote new owner", current, e)
	}
	svc.runReferenceRefresh(ctx, newer)

	// A full, valid snapshot from a foreign integration invalidates the prior cache.
	core.snapshot.Scope.IntegrationID = uuid.New()
	core.snapshot.Revision, _ = tc.ReferenceRevision(core.snapshot)
	_, e = svc.RequestTaskControlReferencesRefresh(ctx, actor, binding)
	if e != nil {
		t.Fatal(e)
	}
	job, e = q.ClaimTaskControlReferenceRefresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	svc.runReferenceRefresh(ctx, job)
	status, e = svc.GetTaskControlReferencesRefresh(ctx, actor, binding, job.ID)
	if e != nil || status.ErrorCode == nil || *status.ErrorCode != "references_scope_mismatch" {
		t.Fatal("foreign integration misclassified", status, e)
	}
	got, e = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{})
	if e != nil || got.CRM != nil {
		t.Fatal("foreign scope retained prior cache", got, e)
	}
	if _, e = svc.taskControlFreshReferencesTx(ctx, q, c); e == nil {
		t.Fatal("foreign scope left publishable cache")
	}
	core.snapshot.Scope.IntegrationID = integration
	core.snapshot.Revision, _ = tc.ReferenceRevision(core.snapshot)
	// Binding changes during the network call fence the result and polling ID.
	_, e = svc.RequestTaskControlReferencesRefresh(ctx, actor, binding)
	if e != nil {
		t.Fatal(e)
	}
	job, e = q.ClaimTaskControlReferenceRefresh(ctx)
	if e != nil {
		t.Fatal(e)
	}
	core.during = func() {
		exec(`UPDATE task_control_connections SET mode_epoch=mode_epoch+1 WHERE company_id=$1 AND id=$2`, company, binding)
	}
	svc.runReferenceRefresh(ctx, job)
	core.during = nil
	got, e = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{})
	if e != nil || got.CRM != nil {
		t.Fatal("former epoch returned", got, e)
	}
	if _, e = svc.GetTaskControlReferencesRefresh(ctx, actor, binding, job.ID); !isCompanyError(e, ErrorNotFound) {
		t.Fatal("poll exposed former scope", e)
	}
	// Installation revocation denies the reference read itself.
	exec(`UPDATE task_control_connections SET state='revoked' WHERE company_id=$1 AND id=$2`, company, binding)
	if _, e = svc.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{}); !isCompanyError(e, ErrorForbidden) {
		t.Fatal("revoked binding returned", e)
	}
	// Ensure mapping revision is durable and valid, not a hash masquerading as number.
	revisions, e := q.GetTaskControlReferenceRevisions(ctx, company)
	if e != nil || revisions.OrganizationRevision < 1 || revisions.MappingRevision < 3 {
		t.Fatal(revisions, e)
	}
}
