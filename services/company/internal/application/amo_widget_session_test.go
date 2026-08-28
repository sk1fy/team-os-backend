package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/sk1fy/team-os-backend/services/company/internal/domain/amoauth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type fakeAmoWidgetVerifier struct {
	identity amoauth.Identity
	err      error
}

func (f fakeAmoWidgetVerifier) Verify(context.Context, string) (amoauth.Identity, error) {
	return f.identity, f.err
}

func TestExchangeAmoWidgetSessionRejectsInvalidToken(t *testing.T) {
	service := &Service{
		amoWidgetTokenVerifier: fakeAmoWidgetVerifier{err: amoauth.ErrInvalidToken},
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err := service.ExchangeAmoWidgetSession(context.Background(), AmoWidgetSessionInput{Token: "bad-token"})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != ErrorCodeAmoTokenInvalid {
		t.Fatalf("error=%v, want %s", err, ErrorCodeAmoTokenInvalid)
	}
}

func TestVerifyAmoWidgetAccessFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind ErrorKind
	}{
		{name: "verifier unavailable", err: amoauth.ErrUnavailable, kind: ErrorUpstream},
		{name: "user forbidden", err: amoauth.ErrForbidden, kind: ErrorForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{
				amoWidgetTokenVerifier: fakeAmoWidgetVerifier{err: test.err},
				logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			_, _, err := service.verifyAmoWidgetAccess(context.Background(), "verified-token-abcdefghijklmnopqrstuvwxyz")
			var applicationErr *Error
			if !errors.As(err, &applicationErr) || applicationErr.Kind != test.kind {
				t.Fatalf("error=%v, want kind %v", err, test.kind)
			}
		})
	}
}

func TestVerifyAmoWidgetAccessRejectsNonAdminResponse(t *testing.T) {
	service := &Service{
		amoWidgetTokenVerifier: fakeAmoWidgetVerifier{identity: amoauth.Identity{AccountID: 31355990, UserID: 42}},
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, _, err := service.verifyAmoWidgetAccess(context.Background(), "verified-token-abcdefghijklmnopqrstuvwxyz")
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorForbidden {
		t.Fatalf("error=%v, want forbidden", err)
	}
}

func TestVerifiedAmoAdminSessionInputUsesOnlyVerifierIdentity(t *testing.T) {
	identity := amoauth.Identity{
		AccountID: 31355990, AccountName: "Проверенная компания",
		UserID: 42, UserEmail: "verified@example.com", UserName: "Проверенный Администратор",
		IsAdmin: true,
	}
	input := verifiedAmoAdminSessionInput(identity, "31355990")
	if input.ExternalAccountID != "31355990" || input.CompanyName != identity.AccountName ||
		input.ExternalUserID != "42" || input.Email != identity.UserEmail ||
		input.UserName != identity.UserName || input.DesiredRole != "admin" {
		t.Fatalf("input=%#v", input)
	}
	identity.IsOwner = true
	if owner := verifiedAmoAdminSessionInput(identity, "31355990"); owner.DesiredRole != "owner" {
		t.Fatalf("owner input=%#v", owner)
	}
}

func TestProvisionAmoAdminSessionValidatesTrustedRole(t *testing.T) {
	service := &Service{}
	_, err := service.ProvisionAmoAdminSession(context.Background(), AmoAdminSessionInput{
		Provider: "rakurs", ExternalAccountID: "31355990", ExternalUserID: "42",
		Email: "admin@example.com", DesiredRole: "employee",
	})
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorValidation {
		t.Fatalf("error=%v, want validation", err)
	}
}

func TestPlanAmoAdminRoleChange(t *testing.T) {
	userID, previousOwnerID := uuid.New(), uuid.New()
	tests := []struct {
		name         string
		currentRole  string
		desiredRole  string
		currentOwner uuid.NullUUID
		want         amoAdminRoleChange
	}{
		{
			name: "first admin leaves company without owner", currentRole: "employee", desiredRole: "admin",
			want: amoAdminRoleChange{TargetRole: "admin"},
		},
		{
			name: "owner assertion transfers ownership", currentRole: "employee", desiredRole: "owner",
			currentOwner: uuid.NullUUID{UUID: previousOwnerID, Valid: true},
			want: amoAdminRoleChange{
				TargetRole: "owner", AssignOwner: true,
				PreviousOwnerID: uuid.NullUUID{UUID: previousOwnerID, Valid: true},
			},
		},
		{
			name: "admin assertion never demotes current owner", currentRole: "owner", desiredRole: "admin",
			currentOwner: uuid.NullUUID{UUID: userID, Valid: true},
			want:         amoAdminRoleChange{TargetRole: "owner"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := planAmoAdminRoleChange(userID, test.currentRole, test.desiredRole, test.currentOwner); got != test.want {
				t.Fatalf("change=%#v, want %#v", got, test.want)
			}
		})
	}
}

func TestValidateAmoWidgetUserStateRejectsCompanyDeactivation(t *testing.T) {
	tests := []db.User{
		{Status: "deactivated"},
		{Status: "active", ExternalDeletedAt: pgTimestamp(time.Now().UTC())},
	}
	for _, user := range tests {
		err := validateAmoWidgetUserState(user)
		var applicationErr *Error
		if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorForbidden ||
			applicationErr.Code != ErrorCodeAmoWidgetSessionUnavailable {
			t.Fatalf("error=%v, want forbidden deactivated TeamOS user", err)
		}
	}
	if err := validateAmoWidgetUserState(db.User{Status: "active"}); err != nil {
		t.Fatalf("active user error=%v", err)
	}
}

func TestEnsureAmoWidgetAccessLinkReusesExistingToken(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	companyID, userID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT company_id, user_id, token").
		WithArgs(companyID, userID).
		WillReturnRows(pgxmock.NewRows([]string{"company_id", "user_id", "token", "created_at", "updated_at"}).
			AddRow(companyID, userID, "stable-access-token", now, now))
	link, err := ensureAmoWidgetAccessLink(context.Background(), db.New(mock), companyID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if link.Token != "stable-access-token" {
		t.Fatalf("token=%q", link.Token)
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

func registrationTokenColumns() []string {
	return []string{
		"id", "company_id", "provider", "external_account_id", "token_hash", "expires_at",
		"consumed_at", "revoked_at", "revocation_reason", "created_at",
	}
}
