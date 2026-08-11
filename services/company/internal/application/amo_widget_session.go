package application

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/domain/amoauth"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const amoWidgetProvider = "rakurs"

func (s *Service) ExchangeAmoWidgetSession(
	ctx context.Context,
	input AmoWidgetSessionInput,
) (AmoWidgetSessionResult, error) {
	if strings.TrimSpace(input.Token) == "" && s.amoWidgetAllowUnsigned {
		return s.exchangeUnsignedAmoWidgetSession(ctx, input)
	}
	identity, accountID, err := s.verifyAmoWidgetAccess(ctx, input.Token)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	if strings.TrimSpace(input.ExternalUserID) == "" {
		return s.exchangeLegacyAmoWidgetSession(ctx, accountID)
	}
	externalUserID := strings.TrimSpace(input.ExternalUserID)
	if externalUserID != strconv.FormatInt(identity.UserID, 10) {
		return AmoWidgetSessionResult{}, coded(
			ErrorUnauthenticated,
			ErrorCodeAmoWidgetUserMismatch,
			"Пользователь amoCRM не совпадает с пользователем подписанного токена",
		)
	}
	email, firstName, lastName, companyName, err := normalizeAmoWidgetProfile(input, accountID)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	return s.provisionAmoWidgetSession(
		ctx, accountID, externalUserID, email, firstName, lastName, companyName, false, true,
	)
}

func (s *Service) exchangeUnsignedAmoWidgetSession(
	ctx context.Context,
	input AmoWidgetSessionInput,
) (AmoWidgetSessionResult, error) {
	_, accountID, err := normalizeAmoAccount(amoWidgetProvider, input.ExternalAccountID)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	externalUserID := strings.TrimSpace(input.ExternalUserID)
	parsedUserID, parseErr := strconv.ParseInt(externalUserID, 10, 64)
	if parseErr != nil || parsedUserID <= 0 || strconv.FormatInt(parsedUserID, 10) != externalUserID {
		return AmoWidgetSessionResult{}, validation("Некорректный ID пользователя amoCRM")
	}
	email, firstName, lastName, companyName, err := normalizeAmoWidgetProfile(input, accountID)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	return s.provisionAmoWidgetSession(
		ctx, accountID, externalUserID, email, firstName, lastName, companyName, true, false,
	)
}

func normalizeAmoWidgetProfile(
	input AmoWidgetSessionInput,
	accountID string,
) (email, firstName, lastName, companyName string, err error) {
	email, err = normalizeEmail(input.Email)
	if err != nil {
		return "", "", "", "", err
	}
	firstName, lastName, _ = splitEmployeeName(input.UserName)
	companyName = strings.TrimSpace(input.CompanyName)
	if companyName == "" {
		companyName = "Компания amoCRM " + accountID
	}
	if len([]rune(companyName)) > 255 || len([]rune(firstName)) > 255 || len([]rune(lastName)) > 255 {
		return "", "", "", "", validation("Слишком длинное имя пользователя или компании")
	}
	return email, firstName, lastName, companyName, nil
}

func (s *Service) verifyAmoWidgetAccess(
	ctx context.Context,
	token string,
) (amoauth.Identity, string, error) {
	if s.amoWidgetTokenVerifier == nil || s.widgetEntitlements == nil {
		return amoauth.Identity{}, "", coded(
			ErrorUpstream, ErrorCodeAmoWidgetSessionUnavailable, "Вход через amoCRM временно недоступен",
		)
	}
	identity, err := s.amoWidgetTokenVerifier.Verify(token)
	if errors.Is(err, amoauth.ErrNotConfigured) {
		return amoauth.Identity{}, "", coded(
			ErrorUpstream, ErrorCodeAmoWidgetSessionUnavailable, "Вход через amoCRM временно недоступен",
		)
	}
	if err != nil {
		return amoauth.Identity{}, "", coded(
			ErrorUnauthenticated, ErrorCodeAmoTokenInvalid, "Токен amoCRM недействителен или истёк",
		)
	}
	accountID := strconv.FormatInt(identity.AccountID, 10)
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "amoCRM widget session verified",
		slog.String("amo_account_id", accountID),
		slog.Int64("amo_user_id", identity.UserID),
	)
	installed, paid, err := s.widgetEntitlements.Check(ctx, accountID)
	if err != nil {
		return amoauth.Identity{}, "", coded(
			ErrorUpstream, ErrorCodeAmoWidgetSessionUnavailable, "Не удалось проверить доступ к виджету amoCRM",
		)
	}
	if !installed {
		return amoauth.Identity{}, "", coded(
			ErrorForbidden, ErrorCodeWidgetNotInstalled, "Виджет «Контроль активности» не установлен или выключен",
		)
	}
	if !paid {
		return amoauth.Identity{}, "", coded(
			ErrorForbidden, ErrorCodeWidgetNotPaid, "Срок оплаты виджета «Контроль активности» истёк",
		)
	}
	return identity, accountID, nil
}

func (s *Service) exchangeLegacyAmoWidgetSession(
	ctx context.Context,
	accountID string,
) (AmoWidgetSessionResult, error) {
	issued, err := s.IssueCompanyRegistrationToken(ctx, amoWidgetProvider, accountID)
	if err != nil {
		var applicationErr *Error
		if errors.As(err, &applicationErr) && applicationErr.Code == ErrorCodeAmoAccountAlreadyExists {
			return AmoWidgetSessionResult{Action: "login", ExternalAccountID: accountID}, nil
		}
		return AmoWidgetSessionResult{}, err
	}
	expiresAt := issued.ExpiresAt
	return AmoWidgetSessionResult{
		Action: "register", ExternalAccountID: accountID,
		RegistrationToken: issued.Token, ExpiresAt: &expiresAt,
	}, nil
}

func (s *Service) provisionAmoWidgetSession(
	ctx context.Context,
	accountID, externalUserID, email, firstName, lastName, companyName string,
	createOnly, syncExternalUsers bool,
) (AmoWidgetSessionResult, error) {
	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось начать создание компании", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoAccount(ctx, db.LockAmoAccountParams{
		Provider: amoWidgetProvider, ExternalAccountID: accountID,
	}); err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось заблокировать аккаунт amoCRM", err)
	}

	integration, company, created, err := s.getOrCreateAmoWidgetCompany(
		ctx, queries, accountID, companyName, now, createOnly,
	)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	if integration.Status != "active" || company.Status == "frozen" || company.Status == "suspended" {
		return AmoWidgetSessionResult{}, coded(
			ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable, "Компания TeamOS временно недоступна",
		)
	}

	user, externalIdentity, userCreated, err := s.getOrCreateAmoWidgetUser(
		ctx, queries, company.ID, integration.ID, accountID,
		externalUserID, email, firstName, lastName, now,
	)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	if created {
		user, err = queries.PromoteAmoWidgetOwner(ctx, db.PromoteAmoWidgetOwnerParams{
			UpdatedAt: now, CompanyID: company.ID, UserID: user.ID,
		})
		if err != nil {
			return AmoWidgetSessionResult{}, internal("Не удалось назначить владельца компании", err)
		}
		company, err = queries.SetCompanyOwner(ctx, db.SetCompanyOwnerParams{
			ID: company.ID, OwnerID: uuid.NullUUID{UUID: user.ID, Valid: true},
		})
		if err != nil {
			return AmoWidgetSessionResult{}, internal("Не удалось назначить владельца компании", err)
		}
		if err = s.emit(ctx, queries, company.ID, user.ID, "teamos.company.company.created.v1", map[string]any{
			"companyId": company.ID.String(), "name": company.Name, "ownerUserId": user.ID.String(),
		}); err != nil {
			return AmoWidgetSessionResult{}, err
		}
	}
	if userCreated {
		if user.Role == "employee" {
			if err = replaceEmployeeSections(
				ctx, queries, company.ID, user.ID, append([]string(nil), defaultEmployeeSections...), nil,
			); err != nil {
				return AmoWidgetSessionResult{}, err
			}
		}
		if err = s.emit(ctx, queries, company.ID, user.ID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(userFromDB(user, nil), nil),
		}); err != nil {
			return AmoWidgetSessionResult{}, err
		}
	}

	hasPassword, err := queries.AmoWidgetUserHasPassword(ctx, db.AmoWidgetUserHasPasswordParams{
		CompanyID: company.ID, UserID: user.ID,
	})
	if err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось проверить пароль пользователя", err)
	}
	if _, err = queries.RevokeActiveAmoWidgetContinuations(ctx, db.RevokeActiveAmoWidgetContinuationsParams{
		RevokedAt: pgTimestamp(now), ExternalIdentityID: externalIdentity.ID,
	}); err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось обновить ссылку входа", err)
	}
	continuationToken, err := domainauth.NewOpaqueToken()
	if err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось выпустить ссылку входа", err)
	}
	expiresAt := now.Add(s.amoWidgetSessionTTL)
	if _, err = queries.CreateAmoWidgetContinuation(ctx, db.CreateAmoWidgetContinuationParams{
		ID: uuid.New(), CompanyID: company.ID, UserID: user.ID, ExternalIdentityID: externalIdentity.ID,
		TokenHash: domainauth.HashOpaqueToken(continuationToken), ExpiresAt: expiresAt, CreatedAt: now,
	}); err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось сохранить ссылку входа", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return AmoWidgetSessionResult{}, internal("Не удалось завершить вход из amoCRM", err)
	}

	if syncExternalUsers && s.externalUsers != nil {
		_ = s.syncAmoUsers(ctx, Actor{CompanyID: company.ID, UserID: user.ID, Role: user.Role})
	}
	action := "login"
	if created {
		action = "register"
	}
	return AmoWidgetSessionResult{
		Action: action, ExternalAccountID: accountID, SessionToken: continuationToken,
		Email: user.Email, CompanyName: company.Name, RequiresPasswordSetup: !hasPassword,
		ExpiresAt: &expiresAt,
	}, nil
}

func (s *Service) getOrCreateAmoWidgetCompany(
	ctx context.Context,
	queries *db.Queries,
	accountID, companyName string,
	now time.Time,
	createOnly bool,
) (db.CompanyIntegration, db.Company, bool, error) {
	row, err := queries.GetAmoWidgetIntegrationForUpdate(ctx, db.GetAmoWidgetIntegrationForUpdateParams{
		Provider: amoWidgetProvider, ExternalAccountID: accountID,
	})
	if err == nil {
		if createOnly {
			return db.CompanyIntegration{}, db.Company{}, false, coded(
				ErrorConflict, ErrorCodeAmoAccountAlreadyExists,
				"Компания для этого аккаунта amoCRM уже создана",
			)
		}
		return row.CompanyIntegration, row.Company, false, nil
	}
	if !isNoRows(err) {
		return db.CompanyIntegration{}, db.Company{}, false, internal("Не удалось проверить компанию TeamOS", err)
	}
	legacyCompanies, err := queries.ListLegacyAmoWidgetCompaniesForUpdate(
		ctx, pgtype.Text{String: accountID, Valid: true},
	)
	if err != nil {
		return db.CompanyIntegration{}, db.Company{}, false, internal("Не удалось проверить прежнюю привязку amoCRM", err)
	}
	if len(legacyCompanies) > 1 {
		return db.CompanyIntegration{}, db.Company{}, false, coded(
			ErrorConflict, ErrorCodeAmoAccountAlreadyExists,
			"Аккаунт amoCRM связан с несколькими компаниями TeamOS; обратитесь в поддержку",
		)
	}
	if createOnly && len(legacyCompanies) > 0 {
		return db.CompanyIntegration{}, db.Company{}, false, coded(
			ErrorConflict, ErrorCodeAmoAccountAlreadyExists,
			"Компания для этого аккаунта amoCRM уже создана",
		)
	}
	created := len(legacyCompanies) == 0
	var company db.Company
	if created {
		company, err = queries.CreateCompanyFromRegistrationToken(ctx, db.CreateCompanyFromRegistrationTokenParams{
			ID: uuid.New(), Name: companyName,
			AmoAccountID: pgtype.Text{String: accountID, Valid: true}, CompletedAt: pgTimestamp(now),
		})
		if err != nil {
			return db.CompanyIntegration{}, db.Company{}, false, internal("Не удалось создать компанию TeamOS", err)
		}
	} else {
		company = legacyCompanies[0]
	}
	integration, err := queries.CreateCompanyIntegration(ctx, db.CreateCompanyIntegrationParams{
		ID: uuid.New(), CompanyID: company.ID, Provider: amoWidgetProvider, ExternalAccountID: accountID,
		Entitlements: []string{}, LastVerifiedAt: pgTimestamp(now), Metadata: []byte(`{"source":"amo_widget"}`),
	})
	if err != nil {
		return db.CompanyIntegration{}, db.Company{}, false, internal("Не удалось связать компанию с amoCRM", err)
	}
	return integration, company, created, nil
}

func (s *Service) getOrCreateAmoWidgetUser(
	ctx context.Context,
	queries *db.Queries,
	companyID, integrationID uuid.UUID,
	accountID, externalUserID, email, firstName, lastName string,
	now time.Time,
) (db.User, db.UserExternalIdentity, bool, error) {
	identityRow, err := queries.GetAmoWidgetUserByIdentity(ctx, db.GetAmoWidgetUserByIdentityParams{
		CompanyID: companyID, IntegrationID: integrationID, Provider: amoWidgetProvider,
		ExternalUserID: externalUserID,
	})
	if err == nil {
		if identityRow.User.Status != "active" || identityRow.User.ExternalDeletedAt.Valid {
			return db.User{}, db.UserExternalIdentity{}, false, coded(
				ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable, "Учётная запись TeamOS отключена",
			)
		}
		externalIdentity, activateErr := queries.ActivateAmoWidgetIdentity(ctx, db.ActivateAmoWidgetIdentityParams{
			VerifiedAt: now, CompanyID: companyID, IdentityID: identityRow.UserExternalIdentity.ID,
		})
		if activateErr != nil {
			return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось подтвердить пользователя amoCRM", activateErr)
		}
		return identityRow.User, externalIdentity, false, nil
	}
	if !isNoRows(err) {
		return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось найти пользователя amoCRM", err)
	}

	user, err := queries.FindAmoWidgetUserForUpdate(ctx, db.FindAmoWidgetUserForUpdateParams{
		CompanyID: companyID, ExternalID: pgtype.Text{String: externalUserID, Valid: true},
	})
	created := false
	if isNoRows(err) {
		user, err = queries.CreateAmoUser(ctx, db.CreateAmoUserParams{
			ID: uuid.New(), CompanyID: companyID, Email: email, FirstName: firstName,
			LastName: pgText(&lastName), ExternalID: pgtype.Text{String: externalUserID, Valid: true},
		})
		created = err == nil
	}
	if isUniqueViolation(err) {
		return db.User{}, db.UserExternalIdentity{}, false, conflict("Пользователь с таким email уже зарегистрирован в TeamOS")
	}
	if err != nil {
		return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось создать пользователя TeamOS", err)
	}
	if user.Status != "active" || user.ExternalDeletedAt.Valid {
		return db.User{}, db.UserExternalIdentity{}, false, coded(
			ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable, "Учётная запись TeamOS отключена",
		)
	}
	externalIdentity, err := queries.CreateAmoWidgetIdentity(ctx, db.CreateAmoWidgetIdentityParams{
		ID: uuid.New(), CompanyID: companyID, IntegrationID: integrationID, UserID: user.ID,
		Provider: amoWidgetProvider, ExternalAccountID: accountID, ExternalUserID: externalUserID,
		LastVerifiedAt: now,
	})
	if isUniqueViolation(err) {
		return db.User{}, db.UserExternalIdentity{}, false, coded(
			ErrorConflict, ErrorCodeAmoWidgetUserMismatch, "Пользователь amoCRM уже связан с другой учётной записью TeamOS",
		)
	}
	if err != nil {
		return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось сохранить пользователя amoCRM", err)
	}
	return user, externalIdentity, created, nil
}

func (s *Service) ValidateAmoWidgetContinuation(
	ctx context.Context,
	token string,
) (AmoWidgetContinuation, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 512 {
		return AmoWidgetContinuation{}, amoWidgetContinuationInvalid()
	}
	row, err := db.New(s.pool).GetAmoWidgetContinuation(ctx, domainauth.HashOpaqueToken(token))
	if isNoRows(err) {
		return AmoWidgetContinuation{}, amoWidgetContinuationInvalid()
	}
	if err != nil {
		return AmoWidgetContinuation{}, internal("Не удалось проверить ссылку входа", err)
	}
	if err = validateAmoWidgetContinuationState(
		row.ExpiresAt, row.ConsumedAt, row.RevokedAt, row.UserStatus, row.ExternalDeletedAt,
		row.IdentityStatus, row.IntegrationStatus, row.CompanyStatus, s.now().UTC(),
	); err != nil {
		return AmoWidgetContinuation{}, err
	}
	return AmoWidgetContinuation{
		Email: row.Email, CompanyName: row.CompanyName,
		RequiresPasswordSetup: !row.HasPassword, ExpiresAt: row.ExpiresAt,
	}, nil
}

func (s *Service) CompleteAmoWidgetContinuation(
	ctx context.Context,
	token, password string,
	meta SessionMeta,
) (AuthResult, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 512 {
		return AuthResult{}, amoWidgetContinuationInvalid()
	}
	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AuthResult{}, internal("Не удалось начать вход из amoCRM", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetAmoWidgetContinuationForUpdate(ctx, domainauth.HashOpaqueToken(token))
	if isNoRows(err) {
		return AuthResult{}, amoWidgetContinuationInvalid()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить ссылку входа", err)
	}
	if err = validateAmoWidgetContinuationState(
		row.ExpiresAt, row.ConsumedAt, row.RevokedAt, row.User.Status, row.User.ExternalDeletedAt,
		row.IdentityStatus, row.IntegrationStatus, row.CompanyStatus, now,
	); err != nil {
		return AuthResult{}, err
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось обработать пароль", err)
	}
	if row.PasswordHash == "" {
		var passwordHash string
		passwordHash, err = domainauth.HashPassword(password)
		releasePasswordSlot()
		if err != nil {
			return AuthResult{}, validation(err.Error())
		}
		if err = queries.SetCredential(ctx, db.SetCredentialParams{
			CompanyID: row.User.CompanyID, UserID: row.User.ID, PasswordHash: passwordHash,
		}); err != nil {
			return AuthResult{}, internal("Не удалось сохранить пароль", err)
		}
	} else {
		var valid bool
		valid, err = domainauth.VerifyPassword(password, row.PasswordHash)
		releasePasswordSlot()
		if err != nil || !valid {
			return AuthResult{}, coded(
				ErrorUnauthenticated, ErrorCodeAmoWidgetPasswordInvalid, "Неверный пароль",
			)
		}
	}
	if _, err = queries.ConsumeAmoWidgetContinuation(ctx, db.ConsumeAmoWidgetContinuationParams{
		ConsumedAt: pgTimestamp(now), TokenID: row.TokenID,
	}); isNoRows(err) {
		return AuthResult{}, amoWidgetContinuationConsumed()
	} else if err != nil {
		return AuthResult{}, internal("Не удалось погасить ссылку входа", err)
	}
	result, err := s.createSession(ctx, queries, row.User, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось завершить вход из amoCRM", err)
	}
	return result, nil
}

func validateAmoWidgetContinuationState(
	expiresAt time.Time,
	consumedAt, revokedAt pgtype.Timestamptz,
	userStatus string,
	externalDeletedAt pgtype.Timestamptz,
	identityStatus, integrationStatus, companyStatus string,
	now time.Time,
) error {
	switch {
	case consumedAt.Valid:
		return amoWidgetContinuationConsumed()
	case revokedAt.Valid:
		return amoWidgetContinuationInvalid()
	case !expiresAt.After(now):
		return amoWidgetContinuationExpired()
	case userStatus != "active" || externalDeletedAt.Valid || identityStatus != "active":
		return coded(ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable, "Учётная запись TeamOS отключена")
	case integrationStatus != "active" || companyStatus == "frozen" || companyStatus == "suspended":
		return coded(ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable, "Компания TeamOS временно недоступна")
	default:
		return nil
	}
}

func (s *Service) CleanupAmoWidgetContinuations(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := db.New(s.pool).DeleteExpiredAmoWidgetContinuations(ctx, before.UTC())
	if err != nil {
		return 0, internal("Не удалось очистить ссылки входа из amoCRM", err)
	}
	return deleted, nil
}
