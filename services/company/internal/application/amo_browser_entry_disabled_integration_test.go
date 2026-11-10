//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBrowserEntryCannotCreateCompanyOrPromoteEmployee(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	service := companyRegistrationTestService(t, pool, clock)
	service.amoEntrySubjectChecker = amoEntryCheckerFunc(func(context.Context, AmoEntrySubject) (bool, error) { return true, nil })
	entry, err := service.ProvisionAmoWidgetEntry(ctx, entryTestInput("820001", "101", "owner@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	employeeID := uuid.New()
	if _, err = pool.Exec(ctx, `INSERT INTO users(id,company_id,email,first_name,role,status,source,external_id) VALUES($1,$2,'employee@example.test','Сотрудник','employee','active','amo','102')`, employeeID, entry.CompanyID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
            'companies',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM companies c),
            'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
            'integrations',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM company_integrations i),
            'identities',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM user_external_identities i),
            'links',(SELECT jsonb_agg(to_jsonb(l) ORDER BY user_id) FROM access_links l),
            'credentials',(SELECT jsonb_agg(to_jsonb(c) ORDER BY user_id) FROM credentials c),
            'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s),
            'entries',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM amo_company_entries e),
            'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM user_admin_audit a),
            'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM outbox o)
        )::text`).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	for _, account := range []string{"820001", "820002"} {
		for _, actor := range []string{"101", "102", "103"} {
			for _, admin := range []bool{false, true} {
				for _, active := range []bool{false, true} {
					_, err = service.BootstrapAmoCompany(ctx, AmoCompanyBootstrapInput{
						AmoAccountID: account, SelfUserID: actor, CompanyName: "Подмена",
						Users: []AmoCompanyBootstrapUser{{ID: actor, Email: "spoof@example.test", Name: "Подмена", IsAdmin: admin, IsActive: active}},
					})
					assertBrowserEntryDisabled(t, err)
					_, err = service.AmoAdminSelfLogin(ctx, AmoAdminSelfLoginInput{
						AmoAccountID: account, SelfUserID: actor, Users: []AmoAdminUserAssertion{{ID: actor, IsAdmin: admin, IsActive: active}},
					})
					assertBrowserEntryDisabled(t, err)
				}
			}
		}
	}
	if after := snapshot(); after != before {
		t.Fatal("retired browser entry mutated company, role, link, session or outbox")
	}
}
