package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/sk1fy/team-os-backend/services/company/internal/domain/amoauth"
)

type fakeAmoWidgetVerifier struct {
	identity amoauth.Identity
	err      error
}

func (f fakeAmoWidgetVerifier) Verify(string) (amoauth.Identity, error) {
	return f.identity, f.err
}

type fakeWidgetEntitlements struct {
	installed bool
	paid      bool
	err       error
}

func (f fakeWidgetEntitlements) Check(context.Context, string) (bool, bool, error) {
	return f.installed, f.paid, f.err
}

func TestExchangeAmoWidgetSessionRejectsInvalidToken(t *testing.T) {
	service := &Service{
		amoWidgetTokenVerifier: fakeAmoWidgetVerifier{err: amoauth.ErrInvalidToken},
		widgetEntitlements:     fakeWidgetEntitlements{installed: true, paid: true},
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err := service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{Token: "bad-token"})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != ErrorCodeAmoTokenInvalid {
		t.Fatalf("error=%v, want %s", err, ErrorCodeAmoTokenInvalid)
	}
}

func TestExchangeAmoWidgetSessionChecksEntitlement(t *testing.T) {
	identity := amoauth.Identity{AccountID: 31355990, UserID: 42}
	tests := []struct {
		name, code      string
		installed, paid bool
	}{
		{name: "not installed", code: ErrorCodeWidgetNotInstalled},
		{name: "not paid", code: ErrorCodeWidgetNotPaid, installed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{
				amoWidgetTokenVerifier: fakeAmoWidgetVerifier{identity: identity},
				widgetEntitlements:     fakeWidgetEntitlements{installed: test.installed, paid: test.paid},
				logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			_, err := service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{Token: "token"})
			var applicationErr *Error
			if !errors.As(err, &applicationErr) || applicationErr.Code != test.code {
				t.Fatalf("error=%v, want %s", err, test.code)
			}
		})
	}
}

func TestExchangeAmoWidgetSessionRejectsProfileUserMismatch(t *testing.T) {
	service := &Service{
		amoWidgetTokenVerifier: fakeAmoWidgetVerifier{identity: amoauth.Identity{AccountID: 31355990, UserID: 42}},
		widgetEntitlements:     fakeWidgetEntitlements{installed: true, paid: true},
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err := service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{
		Token: "token", ExternalUserID: "43", Email: "admin@example.com",
	})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != ErrorCodeAmoWidgetUserMismatch {
		t.Fatalf("error=%v, want %s", err, ErrorCodeAmoWidgetUserMismatch)
	}
}

func TestExchangeUnsignedAmoWidgetSessionValidatesIDs(t *testing.T) {
	service := &Service{amoWidgetAllowUnsigned: true}
	for _, input := range []AmoWidgetSessionInput{
		{ExternalAccountID: "account", ExternalUserID: "42", Email: "admin@example.com"},
		{ExternalAccountID: "31355990", ExternalUserID: "user", Email: "admin@example.com"},
	} {
		if _, err := service.ExchangeAmoWidgetSession(context.Background(), input); err == nil {
			t.Fatalf("input=%#v must be rejected", input)
		}
	}
}

func TestExchangeUnsignedAmoWidgetSessionRejectsExistingCompany(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("rakurs", "31355990").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM company_integrations AS integration").
		WithArgs("rakurs", "31355990").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("FROM companies").
		WithArgs(pgtype.Text{String: "31355990", Valid: true}).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "name", "logo_url", "owner_id", "created_at", "updated_at", "amo_account_id", "status", "onboarding_completed_at",
		}).AddRow(
			uuid.New(), "Ракурс", nil, nil, now, now, "31355990", "active", now,
		))
	mock.ExpectRollback()
	service := &Service{
		pool: mock, now: func() time.Time { return now }, amoWidgetSessionTTL: 10 * time.Minute,
		amoWidgetAllowUnsigned: true, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err = service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{
		ExternalAccountID: "31355990", ExternalUserID: "42", Email: "admin@example.com", CompanyName: "Ракурс",
	})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != ErrorCodeAmoAccountAlreadyExists {
		t.Fatalf("error=%v, want %s", err, ErrorCodeAmoAccountAlreadyExists)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAmoWidgetContinuationState(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		expiresAt         time.Time
		consumedAt        pgtype.Timestamptz
		revokedAt         pgtype.Timestamptz
		userStatus        string
		externalDeletedAt pgtype.Timestamptz
		identityStatus    string
		integrationStatus string
		companyStatus     string
		wantCode          string
	}{
		{name: "valid", expiresAt: now.Add(time.Minute), userStatus: "active", identityStatus: "active", integrationStatus: "active", companyStatus: "active"},
		{name: "consumed", expiresAt: now.Add(time.Minute), consumedAt: pgTimestamp(now), userStatus: "active", identityStatus: "active", integrationStatus: "active", companyStatus: "active", wantCode: ErrorCodeAmoWidgetContinuationConsumed},
		{name: "expired", expiresAt: now, userStatus: "active", identityStatus: "active", integrationStatus: "active", companyStatus: "active", wantCode: ErrorCodeAmoWidgetContinuationExpired},
		{name: "disabled user", expiresAt: now.Add(time.Minute), userStatus: "deactivated", identityStatus: "active", integrationStatus: "active", companyStatus: "active", wantCode: ErrorCodeAmoWidgetSessionUnavailable},
		{name: "frozen company", expiresAt: now.Add(time.Minute), userStatus: "active", identityStatus: "active", integrationStatus: "active", companyStatus: "frozen", wantCode: ErrorCodeAmoWidgetSessionUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAmoWidgetContinuationState(
				test.expiresAt, test.consumedAt, test.revokedAt, test.userStatus,
				test.externalDeletedAt, test.identityStatus, test.integrationStatus, test.companyStatus, now,
			)
			if test.wantCode == "" {
				if err != nil {
					t.Fatalf("error=%v", err)
				}
				return
			}
			var applicationErr *Error
			if !errors.As(err, &applicationErr) || applicationErr.Code != test.wantCode {
				t.Fatalf("error=%v, want %s", err, test.wantCode)
			}
		})
	}
}

func TestExchangeAmoWidgetSessionReturnsLoginForExistingCompany(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("rakurs", "31355990").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM companies AS company").WithArgs("31355990").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
	service := widgetSessionTestService(mock, time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC))
	result, err := service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{Token: "token"})
	if err != nil || result.Action != "login" || result.ExternalAccountID != "31355990" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExchangeAmoWidgetSessionReturnsRegistrationToken(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	columns := registrationTokenColumns()
	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("rakurs", "31355990").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM companies AS company").WithArgs("31355990").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("FROM company_integrations").WithArgs("rakurs", "31355990").WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("FROM company_registration_tokens AS registration_token").WithArgs("rakurs", "31355990").WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("INSERT INTO company_registration_tokens").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "rakurs", "31355990", pgxmock.AnyArg(), expiresAt, now).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			uuid.New(), uuid.New(), "rakurs", "31355990", make([]byte, 32), expiresAt, nil, nil, nil, now,
		))
	mock.ExpectCommit()
	service := widgetSessionTestService(mock, now)
	result, err := service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{Token: "token"})
	if err != nil || result.Action != "register" || len(result.RegistrationToken) < 32 ||
		result.ExpiresAt == nil || !result.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func widgetSessionTestService(pool databasePool, now time.Time) *Service {
	return &Service{
		pool: pool, now: func() time.Time { return now }, companyRegistrationTTL: time.Hour,
		amoWidgetTokenVerifier: fakeAmoWidgetVerifier{identity: amoauth.Identity{AccountID: 31355990, UserID: 42}},
		widgetEntitlements:     fakeWidgetEntitlements{installed: true, paid: true},
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func registrationTokenColumns() []string {
	return []string{"id", "company_id", "provider", "external_account_id", "token_hash", "expires_at", "consumed_at", "revoked_at", "revocation_reason", "created_at"}
}
