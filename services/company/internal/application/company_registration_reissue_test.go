package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
)

func TestIssueCompanyRegistrationTokenReissuesActiveReservation(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	oldExpiresAt := now.Add(30 * time.Minute)
	newExpiresAt := now.Add(time.Hour)
	oldID := uuid.New()
	columns := registrationTokenColumns()
	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("rakurs", "31355990").WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("FROM companies AS company").WithArgs("31355990").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("FROM company_integrations").WithArgs("rakurs", "31355990").WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("FROM company_registration_tokens AS registration_token").WithArgs("rakurs", "31355990").
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			oldID, uuid.New(), "rakurs", "31355990", make([]byte, 32), oldExpiresAt, nil, nil, nil, now.Add(-time.Minute),
		))
	mock.ExpectQuery("UPDATE company_registration_tokens").WithArgs(
		pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Text{String: "reissued", Valid: true}, oldID,
	).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			oldID, uuid.New(), "rakurs", "31355990", make([]byte, 32), oldExpiresAt, nil, now, "reissued", now.Add(-time.Minute),
		))
	mock.ExpectQuery("INSERT INTO company_registration_tokens").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "rakurs", "31355990", pgxmock.AnyArg(), newExpiresAt, now).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			uuid.New(), uuid.New(), "rakurs", "31355990", make([]byte, 32), newExpiresAt, nil, nil, nil, now,
		))
	mock.ExpectCommit()
	service := &Service{pool: mock, now: func() time.Time { return now }, companyRegistrationTTL: time.Hour}
	issued, err := service.IssueCompanyRegistrationToken(context.Background(), "rakurs", "31355990")
	if err != nil || !issued.ExpiresAt.Equal(newExpiresAt) {
		t.Fatalf("issued=%#v error=%v", issued, err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
