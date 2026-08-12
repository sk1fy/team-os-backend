package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

var accessUserColumns = []string{
	"id", "company_id", "email", "first_name", "last_name", "phone", "avatar_url",
	"role", "status", "birth_date", "hired_at", "vacation_allowance", "created_at",
	"updated_at", "source", "external_id", "external_group_id", "external_group_name",
	"avatar_source", "external_deleted_at", "show_in_schedule",
}

func TestEmployeeAccessManagementPolicy(t *testing.T) {
	for _, role := range []string{"owner", "admin"} {
		t.Run(role+"_allowed", func(t *testing.T) {
			actor := Actor{Role: role}
			if !canManageEmployeeAccess(actor) {
				t.Fatalf("%s capability rejected", role)
			}
			if err := requireEmployeeAccessManagement(actor); err != nil {
				t.Fatalf("%s rejected: %v", role, err)
			}
		})
	}
	for _, role := range []string{"employee", "partner"} {
		t.Run(role+"_forbidden", func(t *testing.T) {
			actor := Actor{Role: role}
			if canManageEmployeeAccess(actor) {
				t.Fatalf("%s capability allowed", role)
			}
			err := requireEmployeeAccessManagement(actor)
			var applicationError *Error
			if !errors.As(err, &applicationError) || applicationError.Kind != ErrorForbidden {
				t.Fatalf("error = %#v, want forbidden", err)
			}
			if applicationError.Message != "Недостаточно прав для управления доступом сотрудников" {
				t.Fatalf("message = %q", applicationError.Message)
			}
		})
	}
}

func TestValidateAccessTarget(t *testing.T) {
	active := db.User{ID: uuid.New(), Role: "employee", Status: "active"}
	if got, err := validateAccessTarget(active, nil); err != nil || got.ID != active.ID {
		t.Fatalf("active target = %#v, %v", got, err)
	}

	for _, test := range []struct {
		name string
		user db.User
	}{
		{name: "owner", user: db.User{Role: "owner", Status: "active"}},
		{name: "inactive", user: db.User{Role: "employee", Status: "deactivated"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateAccessTarget(test.user, nil)
			var applicationError *Error
			if !errors.As(err, &applicationError) || applicationError.Kind != ErrorValidation {
				t.Fatalf("error = %#v, want validation", err)
			}
		})
	}
}

func TestAccessActionUsesTargetMethodState(t *testing.T) {
	if got := accessAction(false); got != "issued" {
		t.Fatalf("accessAction(false) = %q, want issued", got)
	}
	if got := accessAction(true); got != "reissued" {
		t.Fatalf("accessAction(true) = %q, want reissued", got)
	}
}

func TestGetUserAccessReportsPasswordAndLinkIndependently(t *testing.T) {
	mock := newAccessMock(t)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 12, 10, 30, 0, 0, time.UTC)
	actor := Actor{CompanyID: companyID, Role: "admin"}

	expectAccessTarget(mock, companyID, userID, now)
	mock.ExpectQuery("SELECT user_login.login").
		WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows([]string{
			"login", "password_enabled", "link_token", "link_created_at",
		}).AddRow("tm8901912", true, "link-token", now))

	service := newAccessService(mock, now)
	access, err := service.GetUserAccess(context.Background(), actor, userID)
	if err != nil {
		t.Fatalf("GetUserAccess() error = %v", err)
	}
	if access.Login != "tm8901912" || !access.PasswordEnabled || !access.LinkEnabled ||
		access.Mode != "link" || access.LinkToken == nil || *access.LinkToken != "link-token" {
		t.Fatalf("GetUserAccess() = %#v", access)
	}
	assertAccessExpectations(t, mock)
}

func TestRevokePasswordAccessPreservesLink(t *testing.T) {
	mock := newAccessMock(t)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 12, 10, 30, 0, 0, time.UTC)
	actor := Actor{UserID: uuid.New(), CompanyID: companyID, Role: "owner"}

	mock.ExpectBegin()
	expectAccessTarget(mock, companyID, userID, now)
	mock.ExpectExec("DELETE FROM credentials").
		WithArgs(companyID, userID).
		WillReturnResult(pgconn.NewCommandTag("DELETE 1"))
	expectSessionRevocation(mock, userID, now)
	expectAccessAudit(mock, actor, userID, "revoked", "password", now)
	mock.ExpectCommit()

	service := newAccessService(mock, now)
	if err := service.RevokePasswordAccess(context.Background(), actor, userID); err != nil {
		t.Fatalf("RevokePasswordAccess() error = %v", err)
	}
	assertAccessExpectations(t, mock)
}

func TestSetLinkAccessPreservesPasswordAndRevokesSessions(t *testing.T) {
	mock := newAccessMock(t)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.July, 17, 10, 30, 0, 0, time.UTC)
	actor := Actor{CompanyID: companyID, Role: "owner"}

	mock.ExpectBegin()
	expectAccessTarget(mock, companyID, userID, now)
	expectAccessDetails(mock, companyID, userID, true, nil, now)
	mock.ExpectQuery("INSERT INTO access_links").
		WithArgs(companyID, userID, pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"company_id", "user_id", "token", "created_at", "updated_at"}).
			AddRow(companyID, userID, "new-link-token", now, now))
	expectSessionRevocation(mock, userID, now)
	expectAccessAudit(mock, actor, userID, "issued", "link", now)
	mock.ExpectCommit()

	service := newAccessService(mock, now)
	result, err := service.SetLinkAccess(context.Background(), actor, userID)
	if err != nil {
		t.Fatalf("SetLinkAccess() error = %v", err)
	}
	if result.Token != "new-link-token" || !result.CreatedAt.Equal(now) {
		t.Fatalf("SetLinkAccess() = %#v", result)
	}
	assertAccessExpectations(t, mock)
}

func TestSetPasswordAccessPreservesLinkAndRevokesSessions(t *testing.T) {
	mock := newAccessMock(t)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.July, 17, 10, 30, 0, 0, time.UTC)
	actor := Actor{CompanyID: companyID, Role: "owner"}
	password := "secure-password"

	mock.ExpectBegin()
	expectAccessTarget(mock, companyID, userID, now)
	linkToken := "existing-link-token"
	expectAccessDetails(mock, companyID, userID, false, &linkToken, now)
	mock.ExpectExec("INSERT INTO credentials").
		WithArgs(companyID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgconn.NewCommandTag("INSERT 0 1"))
	expectSessionRevocation(mock, userID, now)
	expectAccessAudit(mock, actor, userID, "issued", "password", now)
	expectAccessLogin(mock, companyID, userID, "tm8901912")
	mock.ExpectCommit()

	service := newAccessService(mock, now)
	result, err := service.SetPasswordAccess(
		context.Background(), actor, userID, SetPasswordAccessInput{Password: &password},
	)
	if err != nil {
		t.Fatalf("SetPasswordAccess() error = %v", err)
	}
	if result.Password != password || result.Login != "tm8901912" {
		t.Fatalf("SetPasswordAccess() = %#v", result)
	}
	assertAccessExpectations(t, mock)
}

func TestRevokeAccessDeletesBothModesAndRevokesSessions(t *testing.T) {
	mock := newAccessMock(t)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.July, 17, 10, 30, 0, 0, time.UTC)
	actor := Actor{CompanyID: companyID, Role: "owner"}

	mock.ExpectBegin()
	expectAccessTarget(mock, companyID, userID, now)
	linkToken := "existing-link-token"
	expectAccessDetails(mock, companyID, userID, true, &linkToken, now)
	mock.ExpectExec("DELETE FROM credentials").
		WithArgs(companyID, userID).
		WillReturnResult(pgconn.NewCommandTag("DELETE 1"))
	mock.ExpectExec("DELETE FROM access_links").
		WithArgs(companyID, userID).
		WillReturnResult(pgconn.NewCommandTag("DELETE 1"))
	expectSessionRevocation(mock, userID, now)
	expectAccessAudit(mock, actor, userID, "revoked", "password", now)
	expectAccessAudit(mock, actor, userID, "revoked", "link", now)
	mock.ExpectCommit()

	service := newAccessService(mock, now)
	if err := service.RevokeAccess(context.Background(), actor, userID); err != nil {
		t.Fatalf("RevokeAccess() error = %v", err)
	}
	assertAccessExpectations(t, mock)
}

func TestLoginWithRevokedAccessLinkIsRejected(t *testing.T) {
	mock := newAccessMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery("FROM users u JOIN access_links").
		WithArgs("revoked-link-token").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectRollback()

	service := newAccessService(mock, time.Now())
	_, err := service.LoginWithAccessLink(context.Background(), "revoked-link-token", SessionMeta{})
	var applicationError *Error
	if !errors.As(err, &applicationError) || applicationError.Kind != ErrorUnauthenticated {
		t.Fatalf("LoginWithAccessLink() error = %#v, want unauthenticated", err)
	}
	if applicationError.Message != "Ссылка доступа недействительна" {
		t.Fatalf("LoginWithAccessLink() message = %q", applicationError.Message)
	}
	assertAccessExpectations(t, mock)
}

func newAccessMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("create pgx mock: %v", err)
	}
	t.Cleanup(mock.Close)
	return mock
}

func newAccessService(pool databasePool, now time.Time) *Service {
	return &Service{
		pool:          pool,
		now:           func() time.Time { return now },
		passwordSlots: make(chan struct{}, 1),
	}
}

func expectAccessTarget(mock pgxmock.PgxPoolIface, companyID, userID uuid.UUID, now time.Time) {
	mock.ExpectQuery("SELECT id, company_id, email.+FROM users WHERE company_id").
		WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows(accessUserColumns).AddRow(
			userID, companyID, "employee@example.com", "Иван", "Иванов", nil, nil,
			"employee", "active", nil, nil, nil, now, now, "local", nil, nil, nil, nil,
			nil, true,
		))
}

func expectAccessLogin(mock pgxmock.PgxPoolIface, companyID, userID uuid.UUID, login string) {
	mock.ExpectQuery("SELECT login FROM user_logins").
		WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"login"}).AddRow(login))
}

func expectAccessDetails(
	mock pgxmock.PgxPoolIface,
	companyID, userID uuid.UUID,
	passwordEnabled bool,
	linkToken *string,
	now time.Time,
) {
	var token, createdAt any
	if linkToken != nil {
		token = *linkToken
		createdAt = now
	}
	mock.ExpectQuery("SELECT user_login.login").
		WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows([]string{
			"login", "password_enabled", "link_token", "link_created_at",
		}).AddRow("tm8901912", passwordEnabled, token, createdAt))
}

func expectAccessMode(mock pgxmock.PgxPoolIface, companyID, userID uuid.UUID, mode string) {
	mock.ExpectQuery("SELECT CASE").WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"access_mode"}).AddRow(mode))
}

func expectAccessAudit(mock pgxmock.PgxPoolIface, actor Actor, userID uuid.UUID, action, mode string, now time.Time) {
	mock.ExpectExec("INSERT INTO employee_access_audit").
		WithArgs(
			pgxmock.AnyArg(), actor.CompanyID,
			uuid.NullUUID{UUID: userID, Valid: true},
			uuid.NullUUID{UUID: actor.UserID, Valid: true},
			action, mode, now,
		).
		WillReturnResult(pgconn.NewCommandTag("INSERT 0 1"))
}

func expectSessionRevocation(mock pgxmock.PgxPoolIface, userID uuid.UUID, now time.Time) {
	mock.ExpectExec("UPDATE sessions SET revoked_at").
		WithArgs(userID, pgtype.Timestamptz{Time: now, Valid: true}).
		WillReturnResult(pgconn.NewCommandTag("UPDATE 2"))
}

func assertAccessExpectations(t *testing.T, mock pgxmock.PgxPoolIface) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
