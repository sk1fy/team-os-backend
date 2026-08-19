package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) CheckAmoAccount(ctx context.Context, provider, externalAccountID string) (bool, error) {
	provider, externalAccountID, err := normalizeAmoAccount(provider, externalAccountID)
	if err != nil {
		return false, err
	}
	exists, err := db.New(s.pool).AmoAccountExists(ctx, db.AmoAccountExistsParams{
		Provider: provider, ExternalAccountID: externalAccountID, Now: s.now().UTC(),
	})
	if err != nil {
		return false, internal("Не удалось проверить аккаунт amoCRM", err)
	}
	return exists, nil
}

func (s *Service) IssueCompanyRegistrationToken(
	ctx context.Context,
	provider, externalAccountID string,
) (CompanyRegistrationTokenResult, error) {
	provider, externalAccountID, err := normalizeAmoAccount(provider, externalAccountID)
	if err != nil {
		return CompanyRegistrationTokenResult{}, err
	}
	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompanyRegistrationTokenResult{}, internal("Не удалось начать выпуск токена регистрации", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoAccount(ctx, db.LockAmoAccountParams{
		Provider: provider, ExternalAccountID: externalAccountID,
	}); err != nil {
		return CompanyRegistrationTokenResult{}, internal("Не удалось заблокировать аккаунт amoCRM", err)
	}
	companyExists, lookupErr := queries.CompanyAmoAccountExists(ctx, externalAccountID)
	if lookupErr != nil {
		return CompanyRegistrationTokenResult{}, internal("Не удалось проверить аккаунт amoCRM", lookupErr)
	}
	if companyExists {
		return CompanyRegistrationTokenResult{}, coded(ErrorConflict, ErrorCodeAmoAccountAlreadyExists, "Этот аккаунт amoCRM уже используется")
	}
	if _, lookupErr := queries.GetCompanyIntegrationByExternalAccount(ctx, db.GetCompanyIntegrationByExternalAccountParams{
		Provider: provider, ExternalAccountID: externalAccountID,
	}); lookupErr == nil {
		return CompanyRegistrationTokenResult{}, coded(ErrorConflict, ErrorCodeAmoAccountAlreadyExists, "Этот аккаунт amoCRM уже используется")
	} else if !isNoRows(lookupErr) {
		return CompanyRegistrationTokenResult{}, internal("Не удалось проверить аккаунт amoCRM", lookupErr)
	}
	active, activeErr := queries.GetActiveCompanyRegistrationTokenForAccount(ctx, db.GetActiveCompanyRegistrationTokenForAccountParams{
		Provider: provider, ExternalAccountID: externalAccountID,
	})
	if activeErr == nil {
		reason := "expired"
		if active.ExpiresAt.After(now) {
			reason = "reissued"
		}
		if _, err = queries.RevokeCompanyRegistrationToken(ctx, db.RevokeCompanyRegistrationTokenParams{
			RevokedAt: pgTimestamp(now), RevocationReason: pgtype.Text{String: reason, Valid: true}, ID: active.ID,
		}); err != nil {
			return CompanyRegistrationTokenResult{}, internal("Не удалось перевыпустить токен регистрации", err)
		}
	} else if !isNoRows(activeErr) {
		return CompanyRegistrationTokenResult{}, internal("Не удалось проверить резервацию аккаунта amoCRM", activeErr)
	}
	token, err := domainauth.NewOpaqueToken()
	if err != nil {
		return CompanyRegistrationTokenResult{}, internal("Не удалось выпустить токен регистрации", err)
	}
	expiresAt := now.Add(s.companyRegistrationTTL)
	if _, err = queries.CreateCompanyRegistrationToken(ctx, db.CreateCompanyRegistrationTokenParams{
		ID: uuid.New(), CompanyID: uuid.New(), Provider: provider, ExternalAccountID: externalAccountID,
		TokenHash: domainauth.HashOpaqueToken(token), ExpiresAt: expiresAt, CreatedAt: now,
	}); err != nil {
		if isUniqueViolation(err) {
			return CompanyRegistrationTokenResult{}, coded(ErrorConflict, ErrorCodeAmoAccountAlreadyExists, "Этот аккаунт amoCRM уже зарезервирован")
		}
		return CompanyRegistrationTokenResult{}, internal("Не удалось сохранить токен регистрации", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return CompanyRegistrationTokenResult{}, internal("Не удалось завершить выпуск токена регистрации", err)
	}
	return CompanyRegistrationTokenResult{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) ValidateCompanyRegistrationToken(
	ctx context.Context,
	token string,
) (CompanyRegistrationTokenValidation, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 512 {
		return CompanyRegistrationTokenValidation{State: "invalid"}, nil
	}
	row, err := db.New(s.pool).GetCompanyRegistrationTokenByHash(ctx, domainauth.HashOpaqueToken(token))
	if isNoRows(err) {
		return CompanyRegistrationTokenValidation{State: "invalid"}, nil
	}
	if err != nil {
		return CompanyRegistrationTokenValidation{}, internal("Не удалось проверить токен регистрации", err)
	}
	state := companyRegistrationTokenState(row, s.now().UTC())
	result := CompanyRegistrationTokenValidation{Valid: state == "valid", State: state}
	if result.Valid {
		externalAccountID, expiresAt := row.ExternalAccountID, row.ExpiresAt
		result.ExternalAccountID, result.ExpiresAt = &externalAccountID, &expiresAt
	}
	return result, nil
}

func normalizeAmoAccount(provider, externalAccountID string) (string, string, error) {
	provider = strings.TrimSpace(provider)
	externalAccountID = strings.TrimSpace(externalAccountID)
	if provider != "rakurs" || len(externalAccountID) == 0 || len(externalAccountID) > 255 || !digitsOnly(externalAccountID) {
		return "", "", coded(ErrorValidation, ErrorCodeAmoAccountInvalid, "Некорректный amoCRM Account ID")
	}
	return provider, externalAccountID, nil
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func pgTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func companyRegistrationTokenState(row db.CompanyRegistrationToken, now time.Time) string {
	switch {
	case row.ConsumedAt.Valid:
		return "consumed"
	case row.RevokedAt.Valid:
		return "revoked"
	case !row.ExpiresAt.After(now):
		return "expired"
	default:
		return "valid"
	}
}

func companyRegistrationTokenError(row db.CompanyRegistrationToken, now time.Time) error {
	switch companyRegistrationTokenState(row, now) {
	case "valid":
		return nil
	case "expired":
		return registrationTokenExpired()
	case "consumed":
		return registrationTokenConsumed()
	case "revoked":
		return registrationTokenRevoked()
	default:
		return registrationTokenInvalid()
	}
}

func (s *Service) CleanupCompanyRegistrationTokens(
	ctx context.Context,
	tokenRetention time.Duration,
) (int64, error) {
	if tokenRetention < 0 {
		return 0, validation("Некорректный срок хранения токенов регистрации")
	}
	before := s.now().UTC().Add(-tokenRetention)
	deleted, err := db.New(s.pool).DeleteOldCompanyRegistrationTokens(ctx, before)
	if err != nil {
		return 0, internal("Не удалось очистить токены регистрации компаний", err)
	}
	return deleted, nil
}
