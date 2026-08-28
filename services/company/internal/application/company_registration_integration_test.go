//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestCompanyRegistrationTokenLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := companyRegistrationTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)}
	service := companyRegistrationTestService(t, pool, clock)

	if _, err := pool.Exec(ctx, `
		INSERT INTO companies (name, amo_account_id, status, onboarding_completed_at)
		VALUES ('Ракурс', '31355990', 'active', now())
	`); err != nil {
		t.Fatal(err)
	}
	var legacyIntegrations int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM company_integrations
		WHERE provider = 'rakurs' AND external_account_id = '31355990'
	`).Scan(&legacyIntegrations); err != nil {
		t.Fatal(err)
	}
	if legacyIntegrations != 0 {
		t.Fatalf("legacy integrations=%d, want 0", legacyIntegrations)
	}
	availability, err := service.CheckAmoAccount(ctx, "rakurs", "31355990")
	if err != nil || !availability.Exists {
		t.Fatalf("legacy availability=%+v error=%v", availability, err)
	}
	_, err = service.IssueCompanyRegistrationToken(ctx, "rakurs", "31355990")
	assertCompanyRegistrationCode(t, err, ErrorCodeAmoAccountAlreadyExists)
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к backfill-миграции")
	}
	backfillSQL, err := os.ReadFile(filepath.Join(
		filepath.Dir(filename), "..", "..", "migrations", "000011_legacy_amo_integrations.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(backfillSQL)); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM company_integrations
		WHERE provider = 'rakurs' AND external_account_id = '31355990'
	`).Scan(&legacyIntegrations); err != nil {
		t.Fatal(err)
	}
	if legacyIntegrations != 1 {
		t.Fatalf("backfilled legacy integrations=%d, want 1", legacyIntegrations)
	}
	service.externalUsers = staticExternalEmployees{}
	availability, err = service.CheckAmoAccount(ctx, "rakurs", "31355990")
	if err != nil || !availability.Exists || !availability.AdminSelfLoginEligible {
		t.Fatalf("active integration availability=%+v error=%v", availability, err)
	}

	const accountID = "42424242"
	availability, err = service.CheckAmoAccount(ctx, "rakurs", accountID)
	if err != nil || availability.Exists {
		t.Fatalf("initial availability=%+v error=%v", availability, err)
	}
	issued, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", accountID)
	if err != nil {
		t.Fatal(err)
	}
	availability, err = service.CheckAmoAccount(ctx, "rakurs", accountID)
	if err != nil || !availability.Exists || availability.AdminSelfLoginEligible {
		t.Fatalf("reserved availability=%+v error=%v", availability, err)
	}
	validation, err := service.ValidateCompanyRegistrationToken(ctx, issued.Token)
	if err != nil || !validation.Valid || validation.State != "valid" {
		t.Fatalf("validation=%+v error=%v", validation, err)
	}

	registered, err := service.Register(ctx, RegisterInput{
		CompanyName: "Ромашка", Email: "owner@example.com", Password: "strong-password",
		FirstName: "Иван", LastName: "Иванов", RegistrationToken: issued.Token,
	}, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if registered.User.Role != "owner" || registered.AccessToken == "" || registered.RefreshToken == "" {
		t.Fatalf("registered=%+v", registered)
	}
	if registered.User.ShowInSchedule {
		t.Fatal("new company owner must be inactive")
	}
	var amoAccountID string
	var integrations, consumed int
	if err = pool.QueryRow(ctx, `SELECT amo_account_id FROM companies WHERE id=$1`, registered.User.CompanyID).Scan(&amoAccountID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM company_integrations WHERE company_id=$1 AND provider='rakurs' AND external_account_id=$2`, registered.User.CompanyID, accountID).Scan(&integrations); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM company_registration_tokens WHERE company_id=$1 AND consumed_at IS NOT NULL`, registered.User.CompanyID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if amoAccountID != accountID || integrations != 1 || consumed != 1 {
		t.Fatalf("amoAccountID=%q integrations=%d consumed=%d", amoAccountID, integrations, consumed)
	}
	validation, err = service.ValidateCompanyRegistrationToken(ctx, issued.Token)
	if err != nil || validation.Valid || validation.State != "consumed" {
		t.Fatalf("consumed validation=%+v error=%v", validation, err)
	}
	_, err = service.IssueCompanyRegistrationToken(ctx, "rakurs", accountID)
	assertCompanyRegistrationCode(t, err, ErrorCodeAmoAccountAlreadyExists)

	loginReservation, err := service.ReserveRegistrationLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondCompany, err := service.Register(ctx, RegisterInput{
		CompanyName: "Вторая компания", Email: "owner@example.com", Password: "another-strong-password",
		FirstName: "Иван", LastName: "Иванов", LoginReservationToken: loginReservation.ReservationToken,
	}, SessionMeta{})
	if err != nil {
		t.Fatalf("регистрация одинакового email в другой компании: %v", err)
	}
	if secondCompany.User.CompanyID == registered.User.CompanyID || secondCompany.User.ID == registered.User.ID {
		t.Fatalf("пользователи разных компаний пересеклись: first=%+v second=%+v", registered.User, secondCompany.User)
	}
	if secondCompany.User.Login != loginReservation.Login {
		t.Fatalf("login=%q, want reserved %q", secondCompany.User.Login, loginReservation.Login)
	}
	var usersWithEmail, companiesWithEmail int
	if err = pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT company_id) FROM users WHERE email = 'owner@example.com'
	`).Scan(&usersWithEmail, &companiesWithEmail); err != nil {
		t.Fatal(err)
	}
	if usersWithEmail != 2 || companiesWithEmail != 2 {
		t.Fatalf("users=%d companies=%d, want 2 and 2", usersWithEmail, companiesWithEmail)
	}
	if _, err = service.Login(ctx, LoginInput{
		Login: "owner@example.com", Password: "another-strong-password",
	}, SessionMeta{}); err == nil {
		t.Fatal("вход по email должен быть запрещён")
	}
	if _, err = service.Login(ctx, LoginInput{
		Login: loginReservation.Login, Password: "another-strong-password",
	}, SessionMeta{}); err != nil {
		t.Fatalf("вход по зарезервированному логину: %v", err)
	}

	expiring, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", "98765432")
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(25 * time.Hour)
	validation, err = service.ValidateCompanyRegistrationToken(ctx, expiring.Token)
	if err != nil || validation.Valid || validation.State != "expired" {
		t.Fatalf("expired validation=%+v error=%v", validation, err)
	}
	if _, err = service.IssueCompanyRegistrationToken(ctx, "rakurs", "98765432"); err != nil {
		t.Fatalf("reissue after expiry: %v", err)
	}
}

type companyRegistrationTestClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *companyRegistrationTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (c *companyRegistrationTestClock) Add(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = c.value.Add(delta)
}

func companyRegistrationTestService(t *testing.T, pool *pgxpool.Pool, clock *companyRegistrationTestClock) *Service {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(pool, sharedauth.NewTokenIssuer(privateKey, "test-company", "test-api", time.Minute), WithCompanyRegistrationTTL(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	service.now = clock.Now
	return service
}

func assertCompanyRegistrationCode(t *testing.T, err error, code string) {
	t.Helper()
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != code {
		t.Fatalf("error=%v, want code %q", err, code)
	}
}

func companyRegistrationTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	migrations := []struct {
		version int
		name    string
	}{
		{1, "init"}, {2, "phase6_schedule_distribution"}, {3, "amo_users"},
		{4, "remove_amo_from_user_names"}, {5, "validate_phone"}, {6, "employee_access"},
		{7, "user_profiles_access_audit"}, {8, "employee_sections_lifecycle"}, {9, "provisioning"},
		{10, "company_registration_tokens"},
		// Миграцию 11 тест применяет после вставки legacy-компании, чтобы проверить backfill.
		{12, "user_schedule_visibility"}, {13, "amo_group_organization"},
		{14, "department_root"}, {15, "position_levels"}, {16, "user_logins"},
		{17, "company_scoped_emails_and_login_reservations"},
	}
	initScripts := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		initScripts = append(initScripts, filepath.Join(
			migrationsDir, fmt.Sprintf("%06d_%s.up.sql", migration.version, migration.name),
		))
	}
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("company"), postgres.WithUsername("company"), postgres.WithPassword("company"),
		postgres.WithInitScripts(initScripts...), postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("запуск PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if terminateErr := testcontainers.TerminateContainer(container); terminateErr != nil {
			t.Errorf("остановка PostgreSQL: %v", terminateErr)
		}
	})
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
