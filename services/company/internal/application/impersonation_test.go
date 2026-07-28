package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestValidateImpersonationTarget(t *testing.T) {
	companyID := uuid.New()
	ownerID := uuid.New()
	activePartner := db.User{
		ID:        uuid.New(),
		CompanyID: companyID,
		Role:      "partner",
		Status:    "active",
	}

	tests := []struct {
		name      string
		actor     Actor
		user      db.User
		wantKind  ErrorKind
		wantError bool
	}{
		{
			name:  "owner may impersonate active partner",
			actor: Actor{UserID: ownerID, CompanyID: companyID, Role: "owner"},
			user:  activePartner,
		},
		{
			name:      "admin is forbidden",
			actor:     Actor{UserID: uuid.New(), CompanyID: companyID, Role: "admin"},
			user:      activePartner,
			wantKind:  ErrorForbidden,
			wantError: true,
		},
		{
			name:  "cross-company target is hidden",
			actor: Actor{UserID: ownerID, CompanyID: companyID, Role: "owner"},
			user: db.User{
				ID: uuid.New(), CompanyID: uuid.New(), Role: "employee", Status: "active",
			},
			wantKind:  ErrorNotFound,
			wantError: true,
		},
		{
			name:  "owner target is rejected",
			actor: Actor{UserID: ownerID, CompanyID: companyID, Role: "owner"},
			user: db.User{
				ID: ownerID, CompanyID: companyID, Role: "owner", Status: "active",
			},
			wantKind:  ErrorValidation,
			wantError: true,
		},
		{
			name:  "inactive target is rejected",
			actor: Actor{UserID: ownerID, CompanyID: companyID, Role: "owner"},
			user: db.User{
				ID: uuid.New(), CompanyID: companyID, Role: "employee", Status: "deactivated",
			},
			wantKind:  ErrorValidation,
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateImpersonationTarget(test.actor, test.user)
			if !test.wantError {
				if err != nil {
					t.Fatalf("validateImpersonationTarget() error = %v", err)
				}
				return
			}
			var applicationError *Error
			if !errors.As(err, &applicationError) || applicationError.Kind != test.wantKind {
				t.Fatalf("error = %#v, want kind %v", err, test.wantKind)
			}
		})
	}
}

func TestImpersonateUserCreatesSessionWithoutChangingLoginMode(t *testing.T) {
	mock := newAccessMock(t)
	companyID, ownerID, targetID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.July, 28, 10, 30, 0, 0, time.UTC)
	actor := Actor{
		UserID: ownerID, CompanyID: companyID, Role: "owner", RequestID: "req-impersonate",
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, company_id, email.+FROM users").
		WithArgs(companyID, targetID).
		WillReturnRows(pgxmock.NewRows(accessUserColumns).AddRow(
			targetID, companyID, "partner@example.com", "QA", "Partner", nil, nil,
			"partner", "active", nil, nil, nil, now, now, "local", nil, nil, nil, nil,
			nil,
		))
	mock.ExpectQuery("SELECT position_id").
		WithArgs(companyID, targetID).
		WillReturnRows(pgxmock.NewRows([]string{"position_id"}))
	mock.ExpectQuery("WITH RECURSIVE direct_departments").
		WithArgs(companyID, targetID).
		WillReturnRows(pgxmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT section").
		WithArgs(companyID, targetID).
		WillReturnRows(pgxmock.NewRows([]string{"section"}))
	mock.ExpectQuery("INSERT INTO sessions").
		WithArgs(
			pgxmock.AnyArg(), companyID, targetID, pgxmock.AnyArg(), pgxmock.AnyArg(),
			uuid.NullUUID{}, pgtype.Text{}, pgxmock.AnyArg(),
		).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "company_id", "user_id", "refresh_hash", "expires_at", "created_at",
			"last_used_at", "revoked_at", "rotated_from", "replaced_by", "user_agent", "ip_address",
		}).AddRow(
			uuid.New(), companyID, targetID, []byte("refresh-hash"),
			now.Add(30*24*time.Hour), now, nil, nil, nil, nil, nil, nil,
		))
	expectAccessMode(mock, companyID, targetID, "password")
	mock.ExpectCommit()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		pool:       mock,
		issuer:     sharedauth.NewTokenIssuer(privateKey, "teamos-company", "teamos-api", time.Minute),
		refreshTTL: 30 * 24 * time.Hour,
		now:        func() time.Time { return now },
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	result, err := service.ImpersonateUser(
		context.Background(),
		actor,
		targetID,
		SessionMeta{},
	)
	if err != nil {
		t.Fatalf("ImpersonateUser() error = %v", err)
	}
	if result.User.ID != targetID || result.User.Role != "partner" ||
		result.User.AccessMode != "password" || result.AccessToken == "" ||
		result.RefreshToken == "" {
		t.Fatalf("ImpersonateUser() = %#v", result)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected credential/access mutation or missing query: %v", err)
	}
}
