//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

func TestActivityMembershipRevocationInPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, user := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, user, []accessTestUser{{user, "owner", "active"}})
	if _, err := pool.Exec(ctx, `INSERT INTO company_integrations(id,company_id,provider,external_account_id,status) VALUES($1,$2,'rakurs','42','active')`, uuid.New(), company); err != nil {
		t.Fatal(err)
	}
	core := &activityCoreFake{}
	s := &Service{pool: pool}
	WithActivity(core, []dto.ActivityScope{{CompanyId: company, InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "42"}})(s)
	actor := Actor{CompanyID: company, UserID: user, Role: "owner"}
	account := "42"
	if _, err := s.UpdateCompany(ctx, actor, UpdateCompanyInput{SetAmoAccountID: true, AmoAccountID: &account}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActivityConnection(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role='employee' WHERE company_id=$1 AND id=$2`, company, user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActivityConnection(ctx, actor); !isCompanyError(err, ErrorForbidden) {
		t.Fatalf("stale owner token accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role='owner' WHERE company_id=$1 AND id=$2`, company, user); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"43", ""} {
		if _, err := s.UpdateCompany(ctx, actor, UpdateCompanyInput{SetAmoAccountID: true, AmoAccountID: &account}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ActivityConnection(ctx, actor); !isCompanyError(err, ErrorForbidden) {
			t.Fatalf("changed/cleared company account accepted: %v", err)
		}
	}
	if core.calls != 1 {
		t.Fatalf("revoked role reached Core %d times", core.calls)
	}
}
