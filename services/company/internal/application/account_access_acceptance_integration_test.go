//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
)

func TestAccountAccessRegistrationRejectionsAreAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	service := companyRegistrationTestService(t, pool, clock)
	input := RegisterInput{CompanyName: "Проверка регистрации", FirstName: "Тест", LastName: "Владелец", Email: "owner@example.test", Password: "acceptance-only-password"}
	register := func(token string) (AuthResult, error) {
		candidate := input
		candidate.RegistrationToken = token
		return service.Register(ctx, candidate, SessionMeta{})
	}
	used, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", "830001")
	if err != nil {
		t.Fatal(err)
	}
	registered, err := register(used.Token)
	if err != nil {
		t.Fatal(err)
	}
	if registered.User.Role != "owner" {
		t.Fatalf("registered role=%s", registered.User.Role)
	}
	expired, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", "830002")
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(25 * time.Hour)
	revoked, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", "830003")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssueCompanyRegistrationToken(ctx, "rakurs", "830003"); err != nil {
		t.Fatal(err)
	}
	before := accountAccessSnapshot(t, ctx, pool, uuid.Nil)
	for _, test := range []struct{ name, token, code string }{
		{"unknown token", "unknown-registration-token-at-least-32-bytes", ErrorCodeRegistrationTokenInvalid},
		{"consumed token", used.Token, ErrorCodeRegistrationTokenConsumed},
		{"expired token", expired.Token, ErrorCodeRegistrationTokenExpired},
		{"revoked token", revoked.Token, ErrorCodeRegistrationTokenRevoked},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := register(test.token)
			assertCompanyRegistrationCode(t, err, test.code)
			if !reflect.DeepEqual(before, accountAccessSnapshot(t, ctx, pool, uuid.Nil)) {
				t.Fatal("rejected registration changed existing tenant, credentials or sessions")
			}
		})
	}
	existingTenant := accountAccessSnapshot(t, ctx, pool, registered.User.CompanyID)
	standalone, err := register("")
	if err != nil {
		t.Fatal(err)
	}
	var linked bool
	if err = pool.QueryRow(ctx, `SELECT amo_account_id IS NOT NULL OR EXISTS(SELECT 1 FROM company_integrations WHERE company_id=companies.id) FROM companies WHERE id=$1`, standalone.User.CompanyID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked || standalone.User.CompanyID == registered.User.CompanyID {
		t.Fatal("registration without token attached to existing CRM account")
	}
	if !reflect.DeepEqual(existingTenant, accountAccessSnapshot(t, ctx, pool, registered.User.CompanyID)) {
		t.Fatal("standalone registration changed existing tenant")
	}
}

func TestAccountAccessRegistrationTokenHasOneWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	service := companyRegistrationTestService(t, pool, clock)
	replica := companyRegistrationTestService(t, pool, clock)
	issued, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", "830004")
	if err != nil {
		t.Fatal(err)
	}
	input := RegisterInput{CompanyName: "Однократная регистрация", FirstName: "Тест", LastName: "Владелец", Email: "winner@example.test", Password: "acceptance-only-password", RegistrationToken: issued.Token}
	var wait sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, current := range []*Service{service, replica} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := current.Register(ctx, input, SessionMeta{})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	winners, consumed := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			assertCompanyRegistrationCode(t, err, ErrorCodeRegistrationTokenConsumed)
			consumed++
		}
	}
	if winners != 1 || consumed != 1 {
		t.Fatalf("winners=%d rejected=%d", winners, consumed)
	}
	var companies, owners, sessions int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM companies WHERE amo_account_id='830004'),(SELECT count(*) FROM users WHERE role='owner'),(SELECT count(*) FROM session_families)`).Scan(&companies, &owners, &sessions); err != nil {
		t.Fatal(err)
	}
	if companies != 1 || owners != 1 || sessions != 1 {
		t.Fatalf("companies=%d owners=%d sessions=%d", companies, owners, sessions)
	}
}

func TestAccountAccessWidgetAdminAndEmployeeLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(pool, sharedauth.NewTokenIssuer(private, "account-access", "account-access", 15*time.Minute), WithAmoEntrySubjectChecker(amoEntryCheckerFunc(func(context.Context, AmoEntrySubject) (bool, error) { return true, nil })))
	if err != nil {
		t.Fatal(err)
	}
	verifier := sharedauth.NewTokenVerifier(public, "account-access", "account-access")
	enter := func(input AmoWidgetEntryInput) AuthResult {
		t.Helper()
		entry, err := service.ProvisionAmoWidgetEntry(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	ownerSession := enter(entryTestInput("830005", "101", "owner@example.test"))
	adminInput := entryTestInput("830005", "102", "admin@example.test")
	adminInput.Intent = "enter"
	adminSession := enter(adminInput)
	if ownerSession.User.Role != "owner" || adminSession.User.Role != "admin" || ownerSession.User.ID == adminSession.User.ID || ownerSession.User.CompanyID != adminSession.User.CompanyID {
		t.Fatal("CRM administrators did not receive separate own identities in one company")
	}
	admin := Actor{UserID: adminSession.User.ID, CompanyID: adminSession.User.CompanyID, Role: adminSession.User.Role}
	employee, err := service.CreateUser(ctx, admin, CreateUserInput{FirstName: "Сотрудник", Email: "employee@example.test", Role: "employee"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateUser(ctx, admin, UpdateUserInput{ID: employee.ID, SetSectionAccess: true, SectionAccess: []string{"academy"}}); err != nil {
		t.Fatal(err)
	}
	var credentials, links int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM credentials),(SELECT count(*) FROM access_links)`).Scan(&credentials, &links); err != nil {
		t.Fatal(err)
	}
	if credentials != 0 || links != 0 {
		t.Fatal("widget registration or employee creation issued an implicit password/link")
	}
	link, err := service.SetLinkAccess(ctx, admin, employee.ID)
	if err != nil {
		t.Fatal(err)
	}
	login := func() AuthResult {
		t.Helper()
		result, err := service.LoginWithAccessLink(ctx, link.Token, SessionMeta{})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := verifier.Verify(result.Session.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		if claims.Subject != employee.ID.String() || claims.CompanyID != admin.CompanyID.String() || claims.Role != "employee" || !reflect.DeepEqual(claims.SectionAccess, []string{"academy"}) || result.EntryContext != "" {
			t.Fatal("employee link changed user, tenant, role or granted sections")
		}
		return result.Session
	}
	first, second := login(), login()
	if first.FamilyID == second.FamilyID {
		t.Fatal("two employee entries shared a session family")
	}
	employeeActor := Actor{UserID: employee.ID, CompanyID: admin.CompanyID, Role: "employee", SectionAccess: []string{"academy"}}
	if _, err = service.SetLinkAccess(ctx, employeeActor, admin.UserID); !isCompanyError(err, ErrorForbidden) {
		t.Fatalf("employee gained access management: %v", err)
	}
	if err = service.Logout(ctx, first.RefreshToken, first.FamilyID); err != nil {
		t.Fatal(err)
	}
	assertRefreshRejected := func(session AuthResult) {
		t.Helper()
		if _, err := service.Refresh(ctx, session.RefreshToken, SessionMeta{}, session.FamilyID); !isCompanyError(err, ErrorUnauthenticated) {
			t.Fatalf("revoked session refreshed: %v", err)
		}
	}
	assertRefreshRejected(first)
	refreshed, err := service.Refresh(ctx, second.RefreshToken, SessionMeta{}, second.FamilyID)
	if err != nil || refreshed.User.ID != employee.ID || refreshed.User.Role != "employee" {
		t.Fatalf("other-device refresh failed or changed role: %v", err)
	}
	if err = service.RevokeLinkAccess(ctx, admin, employee.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.LoginWithAccessLink(ctx, link.Token, SessionMeta{}); !isCompanyError(err, ErrorUnauthenticated) {
		t.Fatalf("revoked link entered: %v", err)
	}
	assertRefreshRejected(refreshed)
	revokedToken := link.Token
	link, err = service.SetLinkAccess(ctx, admin, employee.ID)
	if err != nil {
		t.Fatal(err)
	}
	if link.Token == revokedToken {
		t.Fatal("reissue restored revoked bearer")
	}
	if _, err = service.LoginWithAccessLink(ctx, revokedToken, SessionMeta{}); !isCompanyError(err, ErrorUnauthenticated) {
		t.Fatalf("old link worked after reissue: %v", err)
	}
	restored := login()
	inactive := "deactivated"
	if _, err = service.UpdateUser(ctx, admin, UpdateUserInput{ID: employee.ID, Status: &inactive}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.LoginWithAccessLink(ctx, link.Token, SessionMeta{}); !isCompanyError(err, ErrorUnauthenticated) {
		t.Fatalf("deactivated user entered by link: %v", err)
	}
	assertRefreshRejected(restored)
	if _, err = service.Refresh(ctx, adminSession.RefreshToken, SessionMeta{}, adminSession.FamilyID); err != nil {
		t.Fatalf("employee deactivation revoked administrator: %v", err)
	}
	var ownerID uuid.UUID
	if err = pool.QueryRow(ctx, `SELECT owner_id FROM companies WHERE id=$1`, admin.CompanyID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID != ownerSession.User.ID {
		t.Fatal("employee lifecycle changed company owner")
	}
}

func accountAccessSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, companyID uuid.UUID) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{"companies", "company_integrations", "company_registration_tokens", "users", "user_external_identities", "employee_section_access", "credentials", "access_links", "sessions", "session_families", "amo_company_entries", "user_admin_audit", "outbox"} {
		var snapshot string
		query := "SELECT COALESCE(jsonb_agg(to_jsonb(row) ORDER BY to_jsonb(row)::text),'[]'::jsonb)::text FROM " + table + " AS row"
		var args []any
		if companyID != uuid.Nil {
			column := "company_id"
			if table == "companies" {
				column = "id"
			}
			query += " WHERE " + column + "=$1"
			args = append(args, companyID)
		}
		if err := pool.QueryRow(ctx, query, args...).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		result[table] = snapshot
	}
	return result
}
