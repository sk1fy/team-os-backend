//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTaskControlSharedMappingPreservesAccountProvenance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, owner, employee := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, owner, []accessTestUser{{owner, "owner", "active"}, {employee, "employee", "active"}})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(account string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_control_confirmed_employee_mappings WHERE company_id=$1 AND account_id=$2 AND employee_id=$3`, company, account, employee).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	binding, mapping := uuid.New(), uuid.New()
	// Historical mapping is company-local verified identity, not a product grant.
	// No Core integration/installation or Distribution permission is created.
	exec(`INSERT INTO distribution_bindings(id,company_id,installation_id,integration_id,account_id,state,intent_id,initiator_snapshot,expires_at) VALUES($1,$2,$3,$4,'123','revoked',$5,$6,now()+interval '1 hour')`, binding, company, uuid.New(), uuid.New(), uuid.New(), owner)
	exec(`INSERT INTO distribution_employee_mappings(id,company_id,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at) VALUES($1,$2,$3,$4,$4,'7','verified',now())`, mapping, company, binding, employee)
	if count("123") != 1 || count("999") != 0 {
		t.Fatal("shared mapping escaped account")
	}
	exec(`INSERT INTO task_control_employee_mappings(company_id,account_id,employee_id,crm_user_id) VALUES($1,'123',$2,'7')`, company, employee)
	if count("123") != 1 {
		t.Fatal("identical proof was duplicated")
	}
	var before, after int64
	if err := pool.QueryRow(ctx, `SELECT mapping_revision FROM task_control_reference_revisions WHERE company_id=$1`, company).Scan(&before); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE distribution_employee_mappings SET state='unavailable',revision=revision+1 WHERE id=$1`, mapping)
	exec(`DELETE FROM task_control_employee_mappings WHERE company_id=$1`, company)
	if count("123") != 0 {
		t.Fatal("withdrawn identity proof retained")
	}
	if err := pool.QueryRow(ctx, `SELECT mapping_revision FROM task_control_reference_revisions WHERE company_id=$1`, company).Scan(&after); err != nil || after <= before {
		t.Fatal("mapping revision did not advance", before, after, err)
	}
	// An old imported profile has no account provenance: same numeric ID in a new
	// account must not silently recreate the withdrawn mapping.
	exec(`UPDATE users SET source='amo',external_id='7' WHERE company_id=$1 AND id=$2`, company, employee)
	exec(`UPDATE companies SET amo_account_id='999' WHERE id=$1`, company)
	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE company_id=$1 AND id=$2`, company, employee).Scan(&email); err != nil {
		t.Fatal(err)
	}
	svc := &Service{pool: pool, now: time.Now, externalUsers: staticExternalEmployees{{ID: "7", Name: "CRM User", Email: &email}}}
	if err := svc.syncAmoUsersNow(ctx, Actor{CompanyID: company, UserID: owner}); err != nil {
		t.Fatal(err)
	}
	if count("999") != 0 || count("123") != 0 {
		t.Fatal("historical external ID created an unproven account mapping")
	}
}
