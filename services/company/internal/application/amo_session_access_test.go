package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

var amoSessionIntegrationColumns = []string{
	"id", "company_id", "provider", "external_account_id", "app_name", "entitlements",
	"status", "last_verified_at", "frozen_at", "metadata", "created_at", "updated_at",
}

var amoSessionCompanyColumns = []string{
	"id", "name", "logo_url", "owner_id", "created_at", "updated_at",
	"amo_account_id", "status", "onboarding_completed_at",
}

func TestCheckAmoSessionAccessUsesDatabaseRole(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	expectAmoSessionIntegration(mock, companyID, "active", now)
	expectAmoSessionCompany(mock, companyID, "active", now)
	expectAmoSessionUser(mock, companyID, userID, "owner", "active", now)

	result, err := (&Service{pool: mock}).CheckAmoSessionAccess(
		context.Background(),
		Actor{CompanyID: companyID, UserID: userID, Role: "employee"},
		"31355990",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Allowed || result.Role != "owner" || result.RedirectURL != "/schedule" {
		t.Fatalf("result=%#v", result)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAmoSessionAccessFailures(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	companyID, userID := uuid.New(), uuid.New()
	tests := []struct {
		name    string
		prepare func(pgxmock.PgxPoolIface)
		kind    ErrorKind
		code    string
	}{
		{
			name: "integration missing", kind: ErrorNotFound, code: ErrorCodeAmoSessionAccessNotFound,
			prepare: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("FROM company_integrations").WithArgs("rakurs", "31355990").WillReturnError(pgx.ErrNoRows)
			},
		},
		{
			name: "company mismatch", kind: ErrorConflict, code: ErrorCodeAmoSessionAccessMismatch,
			prepare: func(mock pgxmock.PgxPoolIface) {
				expectAmoSessionIntegration(mock, uuid.New(), "active", now)
			},
		},
		{
			name: "integration locked", kind: ErrorConflict, code: ErrorCodeAmoSessionAccessLocked,
			prepare: func(mock pgxmock.PgxPoolIface) {
				expectAmoSessionIntegration(mock, companyID, "frozen", now)
				expectAmoSessionCompany(mock, companyID, "active", now)
			},
		},
		{
			name: "company locked", kind: ErrorConflict, code: ErrorCodeAmoSessionAccessLocked,
			prepare: func(mock pgxmock.PgxPoolIface) {
				expectAmoSessionIntegration(mock, companyID, "active", now)
				expectAmoSessionCompany(mock, companyID, "suspended", now)
			},
		},
		{
			name: "employee role forbidden", kind: ErrorForbidden, code: ErrorCodeAmoSessionAccessForbidden,
			prepare: func(mock pgxmock.PgxPoolIface) {
				expectAmoSessionIntegration(mock, companyID, "active", now)
				expectAmoSessionCompany(mock, companyID, "active", now)
				expectAmoSessionUser(mock, companyID, userID, "employee", "active", now)
			},
		},
		{
			name: "inactive user forbidden", kind: ErrorForbidden, code: ErrorCodeAmoSessionAccessForbidden,
			prepare: func(mock pgxmock.PgxPoolIface) {
				expectAmoSessionIntegration(mock, companyID, "active", now)
				expectAmoSessionCompany(mock, companyID, "active", now)
				expectAmoSessionUser(mock, companyID, userID, "admin", "deactivated", now)
			},
		},
		{
			name: "deleted user forbidden", kind: ErrorForbidden, code: ErrorCodeAmoSessionAccessForbidden,
			prepare: func(mock pgxmock.PgxPoolIface) {
				expectAmoSessionIntegration(mock, companyID, "active", now)
				expectAmoSessionCompany(mock, companyID, "active", now)
				mock.ExpectQuery("FROM users").WithArgs(companyID, userID).WillReturnError(pgx.ErrNoRows)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(mock.Close)
			test.prepare(mock)
			_, err = (&Service{pool: mock}).CheckAmoSessionAccess(
				context.Background(), Actor{CompanyID: companyID, UserID: userID, Role: "owner"}, "31355990",
			)
			var applicationErr *Error
			if !errors.As(err, &applicationErr) || applicationErr.Kind != test.kind || applicationErr.Code != test.code {
				t.Fatalf("error=%v, want kind=%v code=%s", err, test.kind, test.code)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckAmoSessionAccessRejectsInvalidAccountID(t *testing.T) {
	_, err := (&Service{}).CheckAmoSessionAccess(context.Background(), Actor{}, "account")
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorValidation {
		t.Fatalf("error=%v", err)
	}
}

func expectAmoSessionIntegration(mock pgxmock.PgxPoolIface, companyID uuid.UUID, status string, now time.Time) {
	mock.ExpectQuery("FROM company_integrations").
		WithArgs("rakurs", "31355990").
		WillReturnRows(pgxmock.NewRows(amoSessionIntegrationColumns).AddRow(
			uuid.New(), companyID, "rakurs", "31355990", nil, []string{}, status,
			nil, nil, []byte(`{}`), now, now,
		))
}

func expectAmoSessionCompany(mock pgxmock.PgxPoolIface, companyID uuid.UUID, status string, now time.Time) {
	mock.ExpectQuery("FROM companies WHERE id").WithArgs(companyID).
		WillReturnRows(pgxmock.NewRows(amoSessionCompanyColumns).AddRow(
			companyID, "Ракурс", nil, nil, now, now, "31355990", status, now,
		))
}

func expectAmoSessionUser(
	mock pgxmock.PgxPoolIface,
	companyID, userID uuid.UUID,
	role, status string,
	now time.Time,
) {
	mock.ExpectQuery("FROM users").WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows(accessUserColumns).AddRow(
			userID, companyID, "admin@example.com", "Админ", "", nil, nil,
			role, status, nil, nil, nil, now, now, "amo", "42", nil, nil, nil, nil, false,
		))
}
