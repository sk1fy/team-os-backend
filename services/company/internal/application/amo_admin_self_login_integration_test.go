//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAmoAdminSelfLoginPromotesAndReusesAccessLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	companyID, integrationID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO companies (id, name, amo_account_id, status, onboarding_completed_at)
		VALUES ($1, 'Ракурс', '31355990', 'active', $2)`, companyID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO company_integrations (
			id, company_id, provider, external_account_id, status, metadata
		) VALUES ($1, $2, 'rakurs', '31355990', 'active', '{}')`, integrationID, companyID); err != nil {
		t.Fatal(err)
	}
	email := "admin@example.com"
	service := &Service{
		pool: pool, now: func() time.Time { return now },
		externalUsers: staticExternalEmployees{{ID: "101", Name: "Админ", Email: &email}},
	}
	input := AmoAdminSelfLoginInput{
		AmoAccountID: "31355990", SelfUserID: "101",
		Users: []AmoAdminUserAssertion{{ID: "101", IsAdmin: true, IsActive: true}},
	}
	first, err := service.AmoAdminSelfLogin(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AmoAdminSelfLogin(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Allowed || first.Action != "login" || first.Role != "admin" || first.AccessToken == "" ||
		second.AccessToken != first.AccessToken || second.Role != "admin" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	var role, token string
	var auditCount int
	if err = pool.QueryRow(ctx, `
		SELECT u.role, access.token
		FROM users AS u
		JOIN access_links AS access ON access.company_id=u.company_id AND access.user_id=u.id
		WHERE u.company_id=$1 AND u.external_id='101'`, companyID).Scan(&role, &token); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM user_admin_audit
		WHERE company_id=$1 AND action='amo_admin_self_login'`, companyID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if role != "admin" || token != first.AccessToken || auditCount != 2 {
		t.Fatalf("role=%s token=%s audits=%d", role, token, auditCount)
	}

	service.externalUsers = staticExternalEmployees{}
	if _, err = service.AmoAdminSelfLogin(ctx, input); !isCompanyError(err, ErrorNotFound) {
		t.Fatalf("missing reconciled user error=%v", err)
	}
	var deleted, linkExists bool
	if err = pool.QueryRow(ctx, `
		SELECT external_deleted_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM access_links WHERE company_id=users.company_id AND user_id=users.id)
		FROM users WHERE company_id=$1 AND external_id='101'`, companyID).Scan(&deleted, &linkExists); err != nil {
		t.Fatal(err)
	}
	if !deleted || linkExists {
		t.Fatalf("deleted=%v linkExists=%v", deleted, linkExists)
	}
}
