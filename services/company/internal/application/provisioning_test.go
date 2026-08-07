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

func TestGetProvisionedCompanyStatus(t *testing.T) {
	companyID := uuid.New()

	t.Run("existing company", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(mock.Close)
		mock.ExpectQuery("SELECT company.id AS company_id").
			WithArgs("rakurs", "31355990").
			WillReturnRows(pgxmock.NewRows([]string{"company_id", "company_status"}).
				AddRow(companyID, "active"))

		result, err := (&Service{pool: mock}).GetProvisionedCompanyStatus(
			context.Background(), " rakurs ", " 31355990 ",
		)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Exists || result.CompanyID == nil || *result.CompanyID != companyID ||
			result.CompanyStatus == nil || *result.CompanyStatus != "active" {
			t.Fatalf("result = %#v", result)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unknown company", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(mock.Close)
		mock.ExpectQuery("SELECT company.id AS company_id").
			WithArgs("rakurs", "missing").
			WillReturnError(pgx.ErrNoRows)

		result, err := (&Service{pool: mock}).GetProvisionedCompanyStatus(
			context.Background(), "rakurs", "missing",
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Exists || result.CompanyID != nil || result.CompanyStatus != nil {
			t.Fatalf("result = %#v", result)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCompleteBootstrapRejectsUnknownTokenBeforePasswordSlot(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	mock.ExpectQuery("FROM bootstrap_activations AS activation").
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	service := &Service{
		pool: mock,
		now:  func() time.Time { return time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC) },
		// A nil channel can never grant an Argon2 slot. The method can only
		// return BOOTSTRAP_INVALID before the deadline if the cheap lookup runs
		// before acquirePasswordSlot.
		passwordSlots: nil,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, err = service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
		Token: "unknown-token", Password: "valid-password",
	}, SessionMeta{})
	assertProvisioningErrorCode(t, err, ErrorCodeBootstrapInvalid)
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupProvisioningArtifactsUsesRequestTTLAndTokenRetention(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	retention := 7 * 24 * time.Hour
	tokenBefore := now.Add(-retention)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM provisioning_requests").
		WithArgs(now).
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec("DELETE FROM bootstrap_activations").
		WithArgs(tokenBefore).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))
	mock.ExpectExec("DELETE FROM sso_tokens").
		WithArgs(tokenBefore).
		WillReturnResult(pgxmock.NewResult("DELETE", 4))
	mock.ExpectCommit()

	service := &Service{pool: mock, now: func() time.Time { return now }}
	result, err := service.CleanupProvisioningArtifacts(context.Background(), retention)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProvisioningRequests != 2 || result.BootstrapActivations != 3 || result.SSOTokens != 4 {
		t.Fatalf("result = %+v", result)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func assertProvisioningErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != code {
		t.Fatalf("error=%v code=%q, want %q", err, applicationErrCodeForTest(applicationErr), code)
	}
}

func applicationErrCodeForTest(err *Error) string {
	if err == nil {
		return ""
	}
	return err.Code
}
