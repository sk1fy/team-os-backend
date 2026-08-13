//go:build integration

package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestEmployeeAccessManagementRolesAndLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := companyAccessTestPool(t, ctx)
	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	service := &Service{
		pool:          pool,
		now:           func() time.Time { return now },
		passwordSlots: make(chan struct{}, 1),
	}

	firstCompanyID, secondCompanyID := uuid.New(), uuid.New()
	owner := Actor{UserID: uuid.New(), CompanyID: firstCompanyID, Role: "owner"}
	admin := Actor{UserID: uuid.New(), CompanyID: firstCompanyID, Role: "admin"}
	employee := Actor{UserID: uuid.New(), CompanyID: firstCompanyID, Role: "employee"}
	partner := Actor{UserID: uuid.New(), CompanyID: firstCompanyID, Role: "partner"}
	targetUserID, invitedUserID, deactivatedUserID := uuid.New(), uuid.New(), uuid.New()
	otherOwner := Actor{UserID: uuid.New(), CompanyID: secondCompanyID, Role: "owner"}
	otherAdmin := Actor{UserID: uuid.New(), CompanyID: secondCompanyID, Role: "admin"}
	otherTargetUserID := uuid.New()

	seedAccessCompany(t, ctx, pool, firstCompanyID, owner.UserID, []accessTestUser{
		{id: owner.UserID, role: owner.Role, status: "active"},
		{id: admin.UserID, role: admin.Role, status: "active"},
		{id: employee.UserID, role: employee.Role, status: "active"},
		{id: partner.UserID, role: partner.Role, status: "active"},
		{id: targetUserID, role: "employee", status: "active"},
		{id: invitedUserID, role: "employee", status: "invited"},
		{id: deactivatedUserID, role: "employee", status: "deactivated"},
	})
	seedAccessCompany(t, ctx, pool, secondCompanyID, otherOwner.UserID, []accessTestUser{
		{id: otherOwner.UserID, role: otherOwner.Role, status: "active"},
		{id: otherAdmin.UserID, role: otherAdmin.Role, status: "active"},
		{id: otherTargetUserID, role: "employee", status: "active"},
	})

	t.Run("owner and admin see actual access mode", func(t *testing.T) {
		assertAccessMode(t, ctx, service, owner, targetUserID, "none", "")
		assertAccessMode(t, ctx, service, admin, targetUserID, "none", "")

		sessionID := seedAccessSession(t, ctx, pool, firstCompanyID, targetUserID)
		password := "employee-password"
		if issued, err := service.SetPasswordAccess(
			ctx, admin, targetUserID, SetPasswordAccessInput{Password: &password},
		); err != nil || issued.Password != password || issued.Login == "" {
			t.Fatalf("admin password access: access=%+v err=%v", issued, err)
		}
		assertAccessMode(t, ctx, service, admin, targetUserID, "password", "")
		assertSessionRevoked(t, ctx, pool, sessionID)
		assertAccessRows(t, ctx, pool, firstCompanyID, targetUserID, 1, 0)

		sessionID = seedAccessSession(t, ctx, pool, firstCompanyID, targetUserID)
		firstLink, err := service.SetLinkAccess(ctx, admin, targetUserID)
		if err != nil || firstLink.Token == "" || firstLink.CreatedAt.IsZero() {
			t.Fatalf("admin link access: link=%+v err=%v", firstLink, err)
		}
		assertAccessMode(t, ctx, service, admin, targetUserID, "link", firstLink.Token)
		assertSessionRevoked(t, ctx, pool, sessionID)
		assertAccessRows(t, ctx, pool, firstCompanyID, targetUserID, 1, 1)

		secondLink, err := service.SetLinkAccess(ctx, admin, targetUserID)
		if err != nil {
			t.Fatalf("admin link rotation: %v", err)
		}
		if secondLink.Token == firstLink.Token {
			t.Fatal("повторная выдача ссылки не сменила токен")
		}
		var oldLinkCount int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM access_links WHERE token=$1`, firstLink.Token).
			Scan(&oldLinkCount); err != nil || oldLinkCount != 0 {
			t.Fatalf("старая ссылка сохранилась: count=%d err=%v", oldLinkCount, err)
		}

		sessionID = seedAccessSession(t, ctx, pool, firstCompanyID, targetUserID)
		if _, err = service.SetPasswordAccess(
			ctx, admin, targetUserID, SetPasswordAccessInput{Password: &password},
		); err != nil {
			t.Fatalf("admin switches link to password: %v", err)
		}
		assertAccessMode(t, ctx, service, admin, targetUserID, "link", secondLink.Token)
		assertSessionRevoked(t, ctx, pool, sessionID)
		assertAccessRows(t, ctx, pool, firstCompanyID, targetUserID, 1, 1)

		sessionID = seedAccessSession(t, ctx, pool, firstCompanyID, targetUserID)
		if err = service.RevokePasswordAccess(ctx, admin, targetUserID); err != nil {
			t.Fatalf("admin revokes password access: %v", err)
		}
		assertAccessMode(t, ctx, service, admin, targetUserID, "link", secondLink.Token)
		assertSessionRevoked(t, ctx, pool, sessionID)
		assertAccessRows(t, ctx, pool, firstCompanyID, targetUserID, 0, 1)

		if _, err = service.SetPasswordAccess(
			ctx, admin, targetUserID, SetPasswordAccessInput{Password: &password},
		); err != nil {
			t.Fatalf("admin restores password access: %v", err)
		}
		sessionID = seedAccessSession(t, ctx, pool, firstCompanyID, targetUserID)
		if err = service.RevokeLinkAccess(ctx, admin, targetUserID); err != nil {
			t.Fatalf("admin revokes link access: %v", err)
		}
		assertAccessMode(t, ctx, service, admin, targetUserID, "password", "")
		assertSessionRevoked(t, ctx, pool, sessionID)
		assertAccessRows(t, ctx, pool, firstCompanyID, targetUserID, 1, 0)

		if _, err = service.SetLinkAccess(ctx, admin, targetUserID); err != nil {
			t.Fatalf("admin restores link access: %v", err)
		}

		sessionID = seedAccessSession(t, ctx, pool, firstCompanyID, targetUserID)
		if err = service.RevokeAccess(ctx, admin, targetUserID); err != nil {
			t.Fatalf("admin revokes access: %v", err)
		}
		assertAccessMode(t, ctx, service, admin, targetUserID, "none", "")
		assertSessionRevoked(t, ctx, pool, sessionID)
		assertAccessRows(t, ctx, pool, firstCompanyID, targetUserID, 0, 0)

		if _, err = service.SetLinkAccess(ctx, owner, targetUserID); err != nil {
			t.Fatalf("owner issues link: %v", err)
		}
		if err = service.RevokeAccess(ctx, owner, targetUserID); err != nil {
			t.Fatalf("owner revokes access: %v", err)
		}
	})

	t.Run("employee and partner receive forbidden", func(t *testing.T) {
		for _, actor := range []Actor{employee, partner} {
			t.Run(actor.Role, func(t *testing.T) {
				password := "employee-password"
				operations := []struct {
					name string
					run  func() error
				}{
					{name: "get", run: func() error {
						_, err := service.GetUserAccess(ctx, actor, targetUserID)
						return err
					}},
					{name: "password", run: func() error {
						_, err := service.SetPasswordAccess(
							ctx, actor, targetUserID, SetPasswordAccessInput{Password: &password},
						)
						return err
					}},
					{name: "link", run: func() error {
						_, err := service.SetLinkAccess(ctx, actor, targetUserID)
						return err
					}},
					{name: "revoke", run: func() error {
						return service.RevokeAccess(ctx, actor, targetUserID)
					}},
				}
				for _, operation := range operations {
					t.Run(operation.name, func(t *testing.T) {
						assertAccessErrorKind(t, operation.run(), ErrorForbidden)
					})
				}
			})
		}
	})

	t.Run("owner target cannot be managed", func(t *testing.T) {
		password := "employee-password"
		operations := []func() error{
			func() error {
				_, err := service.GetUserAccess(ctx, admin, owner.UserID)
				return err
			},
			func() error {
				_, err := service.SetPasswordAccess(
					ctx, admin, owner.UserID, SetPasswordAccessInput{Password: &password},
				)
				return err
			},
			func() error {
				_, err := service.SetLinkAccess(ctx, admin, owner.UserID)
				return err
			},
			func() error {
				return service.RevokeAccess(ctx, admin, owner.UserID)
			},
		}
		for _, operation := range operations {
			assertAccessErrorKind(t, operation(), ErrorValidation)
		}
	})

	t.Run("inactive and missing targets cannot receive access", func(t *testing.T) {
		for _, userID := range []uuid.UUID{invitedUserID, deactivatedUserID} {
			if _, err := service.SetLinkAccess(ctx, admin, userID); err == nil {
				t.Fatalf("доступ выдан неактивному сотруднику %s", userID)
			} else {
				assertAccessErrorKind(t, err, ErrorValidation)
			}
		}
		deletedUserID := uuid.New()
		if _, err := service.SetLinkAccess(ctx, admin, deletedUserID); err == nil {
			t.Fatal("доступ выдан удалённому сотруднику")
		} else {
			assertAccessErrorKind(t, err, ErrorNotFound)
		}
	})

	t.Run("cross-company access is isolated", func(t *testing.T) {
		password := "other-company-password"
		operations := []func() error{
			func() error {
				_, err := service.GetUserAccess(ctx, admin, otherTargetUserID)
				return err
			},
			func() error {
				_, err := service.SetPasswordAccess(
					ctx, admin, otherTargetUserID, SetPasswordAccessInput{Password: &password},
				)
				return err
			},
			func() error {
				_, err := service.SetLinkAccess(ctx, admin, otherTargetUserID)
				return err
			},
			func() error {
				return service.RevokeAccess(ctx, admin, otherTargetUserID)
			},
		}
		for _, operation := range operations {
			assertAccessErrorKind(t, operation(), ErrorNotFound)
		}
		assertAccessRows(t, ctx, pool, secondCompanyID, otherTargetUserID, 0, 0)
	})
}

type accessTestUser struct {
	id     uuid.UUID
	role   string
	status string
}

func companyAccessTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	initScripts := make([]string, 0, 17)
	for migration := 1; migration <= 17; migration++ {
		initScripts = append(initScripts, filepath.Join(
			migrationsDir, fmt.Sprintf("%06d_%s.up.sql", migration, accessMigrationName(migration)),
		))
	}
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("company"),
		postgres.WithUsername("company"),
		postgres.WithPassword("company"),
		postgres.WithInitScripts(initScripts...),
		postgres.BasicWaitStrategies(),
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
		t.Fatalf("строка подключения: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("подключение к PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func accessMigrationName(migration int) string {
	return map[int]string{
		1:  "init",
		2:  "phase6_schedule_distribution",
		3:  "amo_users",
		4:  "remove_amo_from_user_names",
		5:  "validate_phone",
		6:  "employee_access",
		7:  "user_profiles_access_audit",
		8:  "employee_sections_lifecycle",
		9:  "provisioning",
		10: "company_registration_tokens",
		11: "legacy_amo_integrations",
		12: "user_schedule_visibility",
		13: "amo_group_organization",
		14: "department_root",
		15: "position_levels",
		16: "user_logins",
		17: "company_scoped_emails_and_login_reservations",
	}[migration]
}

func seedAccessCompany(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	ownerID uuid.UUID,
	users []accessTestUser,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO companies (id, name) VALUES ($1, $2)`,
		companyID, "Тестовая компания"); err != nil {
		t.Fatalf("создание компании: %v", err)
	}
	for index, user := range users {
		email := fmt.Sprintf("%s-%d@example.com", companyID, index)
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (id, company_id, email, first_name, last_name, role, status)
			VALUES ($1, $2, $3, 'Тест', 'Пользователь', $4, $5)`,
			user.id, companyID, email, user.role, user.status,
		); err != nil {
			t.Fatalf("создание пользователя %s: %v", user.id, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE companies SET owner_id=$2 WHERE id=$1`, companyID, ownerID); err != nil {
		t.Fatalf("назначение владельца: %v", err)
	}
}

func seedAccessSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	userID uuid.UUID,
) uuid.UUID {
	t.Helper()
	sessionID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sessions (id, company_id, user_id, refresh_hash, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '1 day')`,
		sessionID, companyID, userID, sessionID[:],
	); err != nil {
		t.Fatalf("создание сессии: %v", err)
	}
	return sessionID
}

func assertAccessMode(
	t *testing.T,
	ctx context.Context,
	service *Service,
	actor Actor,
	userID uuid.UUID,
	wantMode string,
	wantToken string,
) {
	t.Helper()
	access, err := service.GetUserAccess(ctx, actor, userID)
	if err != nil {
		t.Fatalf("GetUserAccess(): %v", err)
	}
	if access.Mode != wantMode {
		t.Fatalf("mode=%q, want %q", access.Mode, wantMode)
	}
	if wantToken == "" {
		if access.LinkToken != nil || access.LinkCreatedAt != nil {
			t.Fatalf("неожиданные данные ссылки: %+v", access)
		}
		return
	}
	if access.LinkToken == nil || *access.LinkToken != wantToken ||
		access.LinkCreatedAt == nil || access.LinkCreatedAt.IsZero() {
		t.Fatalf("данные ссылки: %+v, want token=%q", access, wantToken)
	}
}

func assertAccessRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	userID uuid.UUID,
	wantCredentials int,
	wantLinks int,
) {
	t.Helper()
	var credentials, links int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM credentials WHERE company_id=$1 AND user_id=$2`,
		companyID, userID,
	).Scan(&credentials); err != nil {
		t.Fatalf("проверка пароля: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM access_links WHERE company_id=$1 AND user_id=$2`,
		companyID, userID,
	).Scan(&links); err != nil {
		t.Fatalf("проверка ссылки: %v", err)
	}
	if credentials != wantCredentials || links != wantLinks {
		t.Fatalf(
			"способы доступа: credentials=%d links=%d, want credentials=%d links=%d",
			credentials, links, wantCredentials, wantLinks,
		)
	}
}

func assertSessionRevoked(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID uuid.UUID) {
	t.Helper()
	var revoked bool
	if err := pool.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1`, sessionID,
	).Scan(&revoked); err != nil {
		t.Fatalf("проверка сессии: %v", err)
	}
	if !revoked {
		t.Fatalf("сессия %s осталась активной", sessionID)
	}
}

func assertAccessErrorKind(t *testing.T, err error, want ErrorKind) {
	t.Helper()
	var applicationError *Error
	if !errors.As(err, &applicationError) || applicationError.Kind != want {
		t.Fatalf("error=%v, want kind=%d", err, want)
	}
}
