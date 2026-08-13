package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestNormalizeAmoAccount(t *testing.T) {
	tests := []struct {
		name, provider, account   string
		wantProvider, wantAccount string
		wantCode                  string
	}{
		{name: "normalizes whitespace", provider: " rakurs ", account: " 31355990 ", wantProvider: "rakurs", wantAccount: "31355990"},
		{name: "rejects letters", provider: "rakurs", account: "account-1", wantCode: ErrorCodeAmoAccountInvalid},
		{name: "rejects another provider", provider: "other", account: "31355990", wantCode: ErrorCodeAmoAccountInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, account, err := normalizeAmoAccount(test.provider, test.account)
			if test.wantCode != "" {
				var applicationErr *Error
				if !errors.As(err, &applicationErr) || applicationErr.Code != test.wantCode {
					t.Fatalf("error = %v, want code %q", err, test.wantCode)
				}
				return
			}
			if err != nil || provider != test.wantProvider || account != test.wantAccount {
				t.Fatalf("provider=%q account=%q error=%v", provider, account, err)
			}
		})
	}
}

func TestCompanyRegistrationTokenState(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		row  db.CompanyRegistrationToken
		want string
	}{
		{name: "valid", row: db.CompanyRegistrationToken{ExpiresAt: now.Add(time.Hour)}, want: "valid"},
		{name: "expired", row: db.CompanyRegistrationToken{ExpiresAt: now}, want: "expired"},
		{name: "consumed takes precedence", row: db.CompanyRegistrationToken{ExpiresAt: now.Add(-time.Hour), ConsumedAt: pgtype.Timestamptz{Time: now, Valid: true}}, want: "consumed"},
		{name: "revoked", row: db.CompanyRegistrationToken{ExpiresAt: now.Add(time.Hour), RevokedAt: pgtype.Timestamptz{Time: now, Valid: true}}, want: "revoked"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := companyRegistrationTokenState(test.row, now); got != test.want {
				t.Fatalf("state = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCheckAmoAccountFindsLegacyCompany(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("FROM companies AS company").
		WithArgs("31355990", "rakurs", now).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	service := &Service{pool: mock, now: func() time.Time { return now }}
	exists, err := service.CheckAmoAccount(context.Background(), "rakurs", "31355990")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("legacy amoCRM Account ID должен существовать")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueAndValidateCompanyRegistrationToken(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)
	columns := []string{"id", "company_id", "provider", "external_account_id", "token_hash", "expires_at", "consumed_at", "revoked_at", "revocation_reason", "created_at"}

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

	service := &Service{pool: mock, now: func() time.Time { return now }, companyRegistrationTTL: 24 * time.Hour}
	issued, err := service.IssueCompanyRegistrationToken(context.Background(), "rakurs", "31355990")
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Token) < 32 || !issued.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("issued = %#v", issued)
	}

	mock.ExpectQuery("FROM company_registration_tokens").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			uuid.New(), uuid.New(), "rakurs", "31355990", make([]byte, 32), expiresAt, nil, nil, nil, now,
		))
	validated, err := service.ValidateCompanyRegistrationToken(context.Background(), issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !validated.Valid || validated.State != "valid" || validated.ExternalAccountID == nil || *validated.ExternalAccountID != "31355990" {
		t.Fatalf("validated = %#v", validated)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueCompanyRegistrationTokenRejectsLegacyCompany(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("rakurs", "31355990").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM companies AS company").WithArgs("31355990").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	service := &Service{pool: mock, now: time.Now, companyRegistrationTTL: 24 * time.Hour}
	_, err = service.IssueCompanyRegistrationToken(context.Background(), "rakurs", "31355990")
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != ErrorCodeAmoAccountAlreadyExists {
		t.Fatalf("error=%v, want code %q", err, ErrorCodeAmoAccountAlreadyExists)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupCompanyRegistrationTokensUsesRetention(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	before := now.Add(-7 * 24 * time.Hour)
	mock.ExpectExec("DELETE FROM company_registration_tokens").WithArgs(before).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))
	service := &Service{pool: mock, now: func() time.Time { return now }}
	deleted, err := service.CleanupCompanyRegistrationTokens(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d", deleted)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReserveRegistrationLogin(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(registrationLoginReservationTTL)
	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	mock.ExpectExec("DELETE FROM registration_login_reservations").
		WithArgs(now, pgTimestamp(now.Add(-registrationLoginReservationTTL))).
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectQuery("INSERT INTO registration_login_reservations").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), expiresAt, now).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "login", "token_hash", "expires_at", "consumed_at", "created_at",
		}).AddRow(uuid.New(), "tm1234567", make([]byte, 32), expiresAt, pgtype.Timestamptz{}, now))
	mock.ExpectCommit()

	service := &Service{pool: mock, now: func() time.Time { return now }}
	reservation, err := service.ReserveRegistrationLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Login != "tm1234567" || reservation.ReservationToken == "" || !reservation.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("reservation=%+v", reservation)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
