//go:build integration

package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type fakeDistributionCore struct {
	mu             sync.Mutex
	bindings       map[uuid.UUID]corebridge.Binding
	refs           corebridge.References
	failMaps       bool
	mirrorRevision int64
	mirror         []corebridge.Mapping
	permissionHook func()
	revoked        map[uuid.UUID]bool
}

func (f *fakeDistributionCore) GetBinding(_ context.Context, s corebridge.Scope) (corebridge.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bindings[s.BindingID]
	if !ok {
		return b, &corebridge.Error{Status: 404}
	}
	return b, nil
}
func (f *fakeDistributionCore) Confirm(_ context.Context, in corebridge.Confirmation) (corebridge.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revoked[in.BindingID] {
		return corebridge.Binding{}, &corebridge.Error{Status: 409}
	}
	b := corebridge.Binding{Scope: in.Scope, State: "active"}
	f.bindings[in.BindingID] = b
	return b, nil
}
func (f *fakeDistributionCore) References(context.Context, corebridge.Scope) (corebridge.References, error) {
	return f.refs, nil
}
func (f *fakeDistributionCore) Mappings(_ context.Context, _ corebridge.Scope, rev int64, m []corebridge.Mapping) error {
	if f.failMaps {
		return errors.New("outage")
	}
	if rev < f.mirrorRevision {
		return &corebridge.Error{Status: 409}
	}
	f.mirrorRevision = rev
	f.mirror = append([]corebridge.Mapping(nil), m...)
	return nil
}
func (f *fakeDistributionCore) Permission(_ context.Context, _ corebridge.Scope, in corebridge.PermissionInput) (corebridge.Permission, error) {
	if f.permissionHook != nil {
		f.permissionHook()
	}
	return corebridge.Permission{UserID: in.UserID, CanViewLead: true, Reason: "allowed", CheckedAt: time.Now()}, nil
}
func (f *fakeDistributionCore) Revoke(_ context.Context, s corebridge.Scope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked[s.BindingID] = true
	if b, ok := f.bindings[s.BindingID]; ok {
		b.State = "revoked"
		f.bindings[s.BindingID] = b
	}
	return nil
}
func TestDistributionConnectionMappingLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company := uuid.New()
	owner := Actor{CompanyID: company, UserID: uuid.New(), Role: "owner"}
	employee := Actor{CompanyID: company, UserID: uuid.New(), Role: "employee"}
	foreignCompany := uuid.New()
	foreignOwner := uuid.New()
	seedAccessCompany(t, ctx, pool, company, owner.UserID, []accessTestUser{{owner.UserID, "owner", "active"}, {employee.UserID, "employee", "active"}})
	seedAccessCompany(t, ctx, pool, foreignCompany, foreignOwner, []accessTestUser{{foreignOwner, "owner", "active"}})
	if _, e := pool.Exec(ctx, "INSERT INTO employee_section_access(company_id,user_id,section) VALUES($1,$2,'distribution')", company, employee.UserID); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	fake := &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}, refs: corebridge.References{Timezone: "Europe/Moscow", TimezoneFetchedAt: time.Now(), State: "fresh", FetchedAt: now, FreshUntil: now.Add(10 * time.Minute), Users: []corebridge.User{{ID: "42", IsActive: true}, {ID: "43", IsActive: false}}, Pipelines: []corebridge.Pipeline{{ID: "2", Statuses: []corebridge.Status{{ID: "3"}}}}}}
	svc := &Service{pool: pool, now: time.Now, distributionCore: fake}
	in := DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "verified-test-admin"}
	if _, e := svc.LinkDistributionConnection(ctx, employee, in); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("employee link %v", e)
	}
	connection, e := svc.LinkDistributionConnection(ctx, owner, in)
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := svc.LinkDistributionConnection(ctx, owner, in)
	if e != nil || repeated.BindingID != connection.BindingID {
		t.Fatalf("relink %v %+v", e, repeated)
	}
	wrong := in
	wrong.InstallationID = uuid.New()
	if _, e = svc.LinkDistributionConnection(ctx, owner, wrong); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("wrong installation %v", e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM companies").Scan(&count); e != nil || count != 2 {
		t.Fatalf("duplicate company %d %v", count, e)
	}

	if _, e = pool.Exec(ctx, "UPDATE companies SET amo_account_id='123' WHERE id=$1", company); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE users SET source='amo',external_id='42' WHERE company_id=$1 AND id=$2", company, employee.UserID); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = svc.ReconcileDistributionEmployeeMappings(ctx, owner, connection.BindingID); e != nil {
			t.Fatal(e)
		}
	}
	imported, e := db.New(pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: company, BindingID: connection.BindingID})
	if e != nil || len(imported) != 1 || !imported[0].UserID.Valid || imported[0].UserID.UUID != employee.UserID {
		t.Fatalf("legacy import %+v %v", imported, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE company_id=$1", company).Scan(&count); e != nil || count != 2 {
		t.Fatalf("duplicate employee %d %v", count, e)
	}
	original := fake.refs.Users
	fake.refs.Users = []corebridge.User{}
	fake.refs.FetchedAt = time.Now()
	if e = svc.ReconcileDistributionEmployeeMappings(ctx, owner, connection.BindingID); e != nil {
		t.Fatal(e)
	}
	removed, e := db.New(pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: company, BindingID: connection.BindingID})
	if e != nil || removed[0].State != "unavailable" {
		t.Fatalf("removed %+v %v", removed, e)
	}
	fake.refs.Users = original
	fake.refs.FetchedAt = time.Now()
	mapping, e := svc.SetDistributionEmployeeMapping(ctx, owner, connection.BindingID, employee.UserID, "42")
	if e != nil || mapping.UserID == nil || *mapping.UserID != employee.UserID || mapping.State != "verified" {
		t.Fatalf("mapping %+v %v", mapping, e)
	}
	repeatedMap, e := svc.SetDistributionEmployeeMapping(ctx, owner, connection.BindingID, employee.UserID, "42")
	if e != nil || repeatedMap.ID != mapping.ID {
		t.Fatalf("mapping duplicate %+v %v", repeatedMap, e)
	}
	if _, e = svc.SetDistributionEmployeeMapping(ctx, owner, connection.BindingID, foreignOwner, "42"); !isCompanyError(e, ErrorNotFound) {
		t.Fatalf("cross-company mapping %v", e)
	}
	if _, e = svc.SetDistributionEmployeeMapping(ctx, owner, connection.BindingID, owner.UserID, "42"); !isCompanyError(e, ErrorConflict) {
		t.Fatalf("ambiguous mapping %v", e)
	}
	b, e := db.New(pool).GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: company, ID: connection.BindingID})
	if e != nil {
		t.Fatal(e)
	}
	widget := DistributionWidgetAccessInput{Scope: bindingScope(b), UserID: "42", LeadID: "7"}
	access, e := svc.DistributionWidgetAccess(ctx, widget)
	if e != nil || !access.Allowed || access.EmployeeID != employee.UserID {
		t.Fatalf("widget %+v %v", access, e)
	}

	decision := DistributionDecisionValidationInput{Actor: DistributionDecisionActor{Kind: "system"}, Scope: bindingScope(b), OperationID: uuid.New(), DecisionID: uuid.New(), EpisodeID: uuid.New(), RuleID: uuid.New(), GroupID: uuid.New(), RuleRevision: 1, AvailabilityRevision: 1, ClaimRevision: 1, WorkerFence: 1, TargetEmployeeID: employee.UserID, TargetResponsibleUserID: "42", LeadID: "7", DecisionKind: "assign", ValidUntil: time.Now().Add(5 * time.Second)}
	validation, err := svc.ValidateDistributionDecision(ctx, decision)
	if err != nil || validation.Allowed || validation.Reason != "decision_not_ready" {
		t.Fatalf("missing business registry granted %+v %v", validation, err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'widget-access')", company, b.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.New(pool).GetDistributionServiceGrant(ctx, db.GetDistributionServiceGrantParams{KeyID: "core", CompanyID: company, InstallationID: b.InstallationID, Capability: "decision-validation"}); !isNoRows(err) {
		t.Fatalf("widget grant authorized decision %v", err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability) VALUES('core',$1,$2,'decision-validation')", company, b.InstallationID); err != nil {
		t.Fatal(err)
	}
	if granted, err := db.New(pool).GetDistributionServiceGrant(ctx, db.GetDistributionServiceGrantParams{KeyID: "core", CompanyID: company, InstallationID: b.InstallationID, Capability: "decision-validation"}); err != nil || !granted {
		t.Fatalf("decision grant missing %v", err)
	}
	perm, e := svc.DistributionLeadPermission(ctx, employee, connection.BindingID, "7")
	if e != nil || !perm.CanViewLead {
		t.Fatalf("ordinary employee %+v %v", perm, e)
	}
	fake.failMaps = true
	fake.refs.FetchedAt = time.Now()
	_, e = svc.SetDistributionEmployeeMapping(ctx, owner, connection.BindingID, employee.UserID, "42")
	if !isCompanyError(e, ErrorUpstream) {
		t.Fatalf("pending mirror %v", e)
	}
	access, e = svc.DistributionWidgetAccess(ctx, widget)
	if e != nil || access.Allowed {
		t.Fatalf("pending granted %+v %v", access, e)
	}
	fake.failMaps = false
	if e = svc.SyncDistributionMappings(ctx, owner, connection.BindingID); e != nil {
		t.Fatal(e)
	}
	access, e = svc.DistributionWidgetAccess(ctx, widget)
	if e != nil || !access.Allowed {
		t.Fatalf("recovered %+v %v", access, e)
	}
	fake.permissionHook = func() {
		_, err := pool.Exec(ctx, "DELETE FROM employee_section_access WHERE company_id=$1 AND user_id=$2 AND section='distribution'", company, employee.UserID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, e = svc.DistributionLeadPermission(ctx, employee, connection.BindingID, "7"); !isCompanyError(e, ErrorForbidden) {
		t.Fatalf("concurrent section revocation %v", e)
	}
	fake.permissionHook = nil
	if _, e = pool.Exec(ctx, "UPDATE users SET status='deactivated' WHERE company_id=$1 AND id=$2", company, employee.UserID); e != nil {
		t.Fatal(e)
	}
	rows, e := db.New(pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: company, BindingID: connection.BindingID})
	if e != nil || rows[0].State != "unavailable" {
		t.Fatalf("inactive %+v %v", rows, e)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM users WHERE company_id=$1 AND id=$2", company, employee.UserID); e != nil {
		t.Fatal(e)
	}
	rows, e = db.New(pool).ListDistributionMappings(ctx, db.ListDistributionMappingsParams{CompanyID: company, BindingID: connection.BindingID})
	if e != nil || len(rows) != 1 || rows[0].UserID.Valid || rows[0].UserIDSnapshot != employee.UserID || rows[0].State != "unavailable" {
		t.Fatalf("deleted tombstone %+v %v", rows, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_mapping_versions WHERE company_id=$1 AND mapping_id=$2", company, mapping.ID).Scan(&count); e != nil || count < 4 {
		t.Fatalf("history %d %v", count, e)
	}
	if e = svc.RevokeDistributionConnection(ctx, owner, connection.BindingID); e != nil {
		t.Fatal(e)
	}
	in.IntentID = uuid.New()
	next, e := svc.LinkDistributionConnection(ctx, owner, in)
	if e != nil || next.BindingID == connection.BindingID {
		t.Fatalf("new binding %+v %v", next, e)
	}

	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := svc.LinkDistributionConnection(ctx, owner, in)
			if err == nil && c.BindingID != next.BindingID {
				err = errors.New("concurrent duplicate binding")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM distribution_bindings WHERE company_id=$1 AND state='active'", company).Scan(&count); e != nil || count != 1 {
		t.Fatalf("parallel bindings %d %v", count, e)
	}
	nonce := uuid.New()
	q := db.New(pool)
	if n, err := q.ClaimDistributionNonce(ctx, db.ClaimDistributionNonceParams{KeyID: "core", Nonce: nonce, ExpiresAt: time.Now().Add(time.Minute)}); err != nil || n != 1 {
		t.Fatalf("nonce %d %v", n, err)
	}
	if n, err := q.ClaimDistributionNonce(ctx, db.ClaimDistributionNonceParams{KeyID: "core", Nonce: nonce, ExpiresAt: time.Now().Add(time.Minute)}); err != nil || n != 0 {
		t.Fatalf("replay %d %v", n, err)
	}
	access, e = svc.DistributionWidgetAccess(ctx, widget)
	if e != nil || access.Allowed {
		t.Fatalf("revoked widget %+v %v", access, e)
	}
}
