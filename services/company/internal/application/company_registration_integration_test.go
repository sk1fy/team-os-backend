//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
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

	exists, err := service.CheckAmoAccount(ctx, "rakurs", "31355990")
	if err != nil || exists {
		t.Fatalf("initial exists=%v error=%v", exists, err)
	}
	issued, err := service.IssueCompanyRegistrationToken(ctx, "rakurs", "31355990")
	if err != nil {
		t.Fatal(err)
	}
	exists, err = service.CheckAmoAccount(ctx, "rakurs", "31355990")
	if err != nil || !exists {
		t.Fatalf("reserved exists=%v error=%v", exists, err)
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
	var amoAccountID string
	var integrations, consumed int
	if err = pool.QueryRow(ctx, `SELECT amo_account_id FROM companies WHERE id=$1`, registered.User.CompanyID).Scan(&amoAccountID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM company_integrations WHERE company_id=$1 AND provider='rakurs' AND external_account_id='31355990'`, registered.User.CompanyID).Scan(&integrations); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM company_registration_tokens WHERE company_id=$1 AND consumed_at IS NOT NULL`, registered.User.CompanyID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if amoAccountID != "31355990" || integrations != 1 || consumed != 1 {
		t.Fatalf("amoAccountID=%q integrations=%d consumed=%d", amoAccountID, integrations, consumed)
	}
	validation, err = service.ValidateCompanyRegistrationToken(ctx, issued.Token)
	if err != nil || validation.Valid || validation.State != "consumed" {
		t.Fatalf("consumed validation=%+v error=%v", validation, err)
	}
	_, err = service.IssueCompanyRegistrationToken(ctx, "rakurs", "31355990")
	assertCompanyRegistrationCode(t, err, ErrorCodeAmoAccountAlreadyExists)

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
	names := []string{
		"init", "phase6_schedule_distribution", "amo_users", "remove_amo_from_user_names",
		"validate_phone", "employee_access", "user_profiles_access_audit",
		"employee_sections_lifecycle", "provisioning", "company_registration_tokens",
	}
	initScripts := make([]string, 0, len(names))
	for migration, name := range names {
		initScripts = append(initScripts, filepath.Join(migrationsDir, fmt.Sprintf("%06d_%s.up.sql", migration+1, name)))
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
