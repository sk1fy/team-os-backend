package application

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const provisioningRequestTTL = 24 * time.Hour

func (s *Service) CleanupProvisioningArtifacts(
	ctx context.Context,
	tokenRetention time.Duration,
) (ProvisioningCleanupResult, error) {
	if tokenRetention < 0 {
		return ProvisioningCleanupResult{}, validation("Некорректный срок хранения provisioning-токенов")
	}
	now := s.now().UTC()
	tokenBefore := now.Add(-tokenRetention)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProvisioningCleanupResult{}, internal("Не удалось начать очистку provisioning-данных", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	result := ProvisioningCleanupResult{}
	result.ProvisioningRequests, err = queries.DeleteExpiredProvisioningRequests(ctx, now)
	if err != nil {
		return ProvisioningCleanupResult{}, internal("Не удалось очистить provisioning-запросы", err)
	}
	result.BootstrapActivations, err = queries.DeleteExpiredBootstrapActivations(ctx, tokenBefore)
	if err != nil {
		return ProvisioningCleanupResult{}, internal("Не удалось очистить bootstrap-активации", err)
	}
	result.SSOTokens, err = queries.DeleteExpiredSSOTokens(ctx, tokenBefore)
	if err != nil {
		return ProvisioningCleanupResult{}, internal("Не удалось очистить SSO-токены", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ProvisioningCleanupResult{}, internal("Не удалось завершить очистку provisioning-данных", err)
	}
	return result, nil
}

func (s *Service) ProvisionCompany(ctx context.Context, input ProvisionCompanyInput) (ProvisionCompanyResult, error) {
	input, requestHash, err := normalizeProvisionCompanyInput(input)
	if err != nil {
		return ProvisionCompanyResult{}, err
	}
	now := s.now().UTC()
	// Advisory locks for the idempotency key and external account serialize
	// competing creates. READ COMMITTED ensures a waiter observes the winner's
	// committed rows; a SERIALIZABLE snapshot taken while waiting would be stale
	// and turn an ordinary concurrent retry into a serialization failure.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось начать создание компании", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockProvisioningKey(ctx, db.LockProvisioningKeyParams{
		Provider: input.Provider, IdempotencyKey: input.IdempotencyKey,
	}); err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось заблокировать ключ идемпотентности", err)
	}
	account := db.LockProvisioningAccountParams{
		Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
	}
	if err = queries.LockProvisioningAccount(ctx, account); err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось заблокировать внешний аккаунт", err)
	}

	request, requestErr := queries.GetProvisioningRequestForUpdate(ctx, db.GetProvisioningRequestForUpdateParams{
		Provider: input.Provider, IdempotencyKey: input.IdempotencyKey,
	})
	if requestErr == nil {
		if !bytes.Equal(request.RequestHash, requestHash) || request.ExternalAccountID != input.ExternalAccountID {
			return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeProvisioningConflict, "Ключ идемпотентности уже использован для другого запроса")
		}
		integration, integrationErr := queries.GetCompanyIntegrationByExternalAccountForUpdate(
			ctx,
			db.GetCompanyIntegrationByExternalAccountForUpdateParams{
				Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			},
		)
		if isNoRows(integrationErr) || (integrationErr == nil &&
			(integration.ID != request.IntegrationID || integration.CompanyID != request.CompanyID)) {
			return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeProvisioningConflict, "Существующая интеграция не совпадает с provisioning-запросом")
		}
		if integrationErr != nil {
			return ProvisionCompanyResult{}, internal("Не удалось проверить существующую интеграцию", integrationErr)
		}
		result, replayErr := s.replayProvisioning(ctx, queries, input, requestHash, integration, now, false)
		if replayErr != nil {
			return ProvisionCompanyResult{}, replayErr
		}
		if err = tx.Commit(ctx); err != nil {
			return ProvisionCompanyResult{}, internal("Не удалось завершить повтор provisioning", err)
		}
		return result, nil
	}
	if !isNoRows(requestErr) {
		return ProvisionCompanyResult{}, internal("Не удалось проверить provisioning-запрос", requestErr)
	}
	integration, integrationErr := queries.GetCompanyIntegrationByExternalAccountForUpdate(
		ctx,
		db.GetCompanyIntegrationByExternalAccountForUpdateParams{
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
		},
	)
	if integrationErr == nil {
		result, replayErr := s.replayProvisioning(ctx, queries, input, requestHash, integration, now, true)
		if replayErr != nil {
			return ProvisionCompanyResult{}, replayErr
		}
		if err = tx.Commit(ctx); err != nil {
			return ProvisionCompanyResult{}, internal("Не удалось завершить повтор provisioning", err)
		}
		return result, nil
	}
	if !isNoRows(integrationErr) {
		return ProvisionCompanyResult{}, internal("Не удалось проверить внешний аккаунт", integrationErr)
	}

	companyID, integrationID := uuid.New(), uuid.New()
	amoAccountID := pgtype.Text{}
	if input.Provider == "rakurs" && digitsOnly(input.ExternalAccountID) {
		amoAccountID = pgtype.Text{String: input.ExternalAccountID, Valid: true}
	}
	company, err := queries.CreateProvisioningCompany(ctx, db.CreateProvisioningCompanyParams{
		ID: companyID, Name: input.CompanyName, AmoAccountID: amoAccountID,
	})
	if err != nil {
		return ProvisionCompanyResult{}, provisioningWriteError("Не удалось создать компанию", err)
	}
	if _, err = queries.CreateCompanyIntegration(ctx, db.CreateCompanyIntegrationParams{
		ID: integrationID, CompanyID: companyID, Provider: input.Provider,
		ExternalAccountID: input.ExternalAccountID, Entitlements: []string{},
		LastVerifiedAt: pgTimestamp(now), Metadata: []byte(`{}`),
	}); err != nil {
		return ProvisionCompanyResult{}, provisioningWriteError("Не удалось связать компанию со внешним сервисом", err)
	}

	participants := []struct {
		role  string
		input ProvisioningParticipantInput
	}{{role: "owner", input: input.Owner}, {role: "admin", input: input.Admin}}
	var ownerID, adminID, initiatorID uuid.UUID
	createdUsers := make(map[string]db.User, len(participants))
	initiatorRole := ""
	for _, participant := range participants {
		userID := uuid.New()
		user, createErr := queries.CreateProvisioningUser(ctx, db.CreateProvisioningUserParams{
			ID: userID, CompanyID: companyID, Email: participant.input.Email,
			FirstName: participant.input.FirstName, LastName: pgText(&participant.input.LastName),
			Role: participant.role, Source: "external",
		})
		if createErr != nil {
			return ProvisionCompanyResult{}, provisioningWriteError("Не удалось создать участника онбординга", createErr)
		}
		if _, createErr = queries.CreateUserExternalIdentity(ctx, db.CreateUserExternalIdentityParams{
			ID: uuid.New(), CompanyID: companyID, IntegrationID: integrationID, UserID: userID,
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			ExternalUserID: participant.input.ExternalUserID, LastVerifiedAt: now,
		}); createErr != nil {
			return ProvisionCompanyResult{}, provisioningWriteError("Не удалось сохранить внешнюю личность", createErr)
		}
		if participant.role == "owner" {
			ownerID = userID
		} else {
			adminID = userID
		}
		createdUsers[participant.role] = user
		if participant.input.ExternalUserID == input.InitiatorExternalUserID {
			initiatorID, initiatorRole = userID, participant.role
		}
		if err = s.emit(ctx, queries, companyID, userID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(userFromDB(user, nil), nil),
		}); err != nil {
			return ProvisionCompanyResult{}, err
		}
	}
	if _, err = queries.SetCompanyOwner(ctx, db.SetCompanyOwnerParams{
		ID: companyID, OwnerID: uuid.NullUUID{UUID: ownerID, Valid: true},
	}); err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось назначить владельца", err)
	}
	var token string
	var expiresAt time.Time
	for _, role := range []string{"owner", "admin"} {
		user := createdUsers[role]
		purpose := "second_user"
		if user.ID == initiatorID {
			purpose = "initiator"
		}
		issuedToken, issuedExpiresAt, issueErr := s.issueBootstrapActivation(
			ctx, queries, companyID, user.ID, role, purpose, "", initiatorID, now,
		)
		if issueErr != nil {
			return ProvisionCompanyResult{}, issueErr
		}
		if user.ID == initiatorID {
			token, expiresAt = issuedToken, issuedExpiresAt
		}
	}
	if _, err = queries.CreateProvisioningRequest(ctx, db.CreateProvisioningRequestParams{
		Provider: input.Provider, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash,
		ExternalAccountID: input.ExternalAccountID, CompanyID: companyID, IntegrationID: integrationID,
		InitiatorUserID: initiatorID, CreatedAt: now, ExpiresAt: now.Add(provisioningRequestTTL),
	}); err != nil {
		return ProvisionCompanyResult{}, provisioningWriteError("Не удалось сохранить provisioning-запрос", err)
	}
	if err = s.emit(ctx, queries, companyID, initiatorID, "teamos.company.company.provisioned.v1", map[string]any{
		"companyId": companyID.String(), "name": company.Name,
		"provider": input.Provider, "externalAccountId": input.ExternalAccountID,
		"ownerUserId": ownerID.String(), "adminUserId": adminID.String(),
		"initiatorUserId": initiatorID.String(),
	}); err != nil {
		return ProvisionCompanyResult{}, err
	}
	if err = s.emit(ctx, queries, companyID, initiatorID, "teamos.company.company.created.v1", map[string]any{
		"companyId": companyID.String(), "name": company.Name, "ownerUserId": ownerID.String(),
	}); err != nil {
		return ProvisionCompanyResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось завершить создание компании", err)
	}
	return ProvisionCompanyResult{
		CompanyID: companyID, CompanyStatus: "onboarding", Created: true,
		InitiatorRole: initiatorRole, BootstrapToken: token, BootstrapExpiresAt: expiresAt,
	}, nil
}

func (s *Service) GetProvisionedCompanyStatus(
	ctx context.Context,
	provider string,
	externalAccountID string,
) (ProvisionedCompanyStatus, error) {
	provider, externalAccountID, err := normalizeProvisionedCompanyLookup(provider, externalAccountID)
	if err != nil {
		return ProvisionedCompanyStatus{}, err
	}
	row, err := db.New(s.pool).GetProvisionedCompanyStatus(ctx, db.GetProvisionedCompanyStatusParams{
		Provider: provider, ExternalAccountID: externalAccountID,
	})
	if isNoRows(err) {
		return ProvisionedCompanyStatus{Exists: false}, nil
	}
	if err != nil {
		return ProvisionedCompanyStatus{}, internal("Не удалось проверить внешний аккаунт", err)
	}
	companyID := row.CompanyID
	companyStatus := row.CompanyStatus
	return ProvisionedCompanyStatus{
		Exists: true, CompanyID: &companyID, CompanyStatus: &companyStatus,
	}, nil
}

func (s *Service) replayProvisioning(
	ctx context.Context,
	queries *db.Queries,
	input ProvisionCompanyInput,
	requestHash []byte,
	integration db.CompanyIntegration,
	now time.Time,
	createRequest bool,
) (ProvisionCompanyResult, error) {
	if integration.Status != "active" {
		return ProvisionCompanyResult{}, integrationFrozen()
	}
	company, err := queries.GetCompany(ctx, integration.CompanyID)
	if err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось получить созданную компанию", err)
	}
	if company.Status == "frozen" || company.Status == "suspended" {
		return ProvisionCompanyResult{}, integrationFrozen()
	}
	if company.Status != "onboarding" {
		return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
	}
	users, err := queries.GetProvisioningUsers(ctx, db.GetProvisioningUsersParams{
		Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
	})
	if err != nil {
		return ProvisionCompanyResult{}, internal("Не удалось проверить участников онбординга", err)
	}
	var initiator *db.GetProvisioningUsersRow
	seenRoles := make(map[string]bool, 2)
	for index := range users {
		row := &users[index]
		var expected ProvisioningParticipantInput
		switch row.Role {
		case "owner":
			expected = input.Owner
		case "admin":
			expected = input.Admin
		default:
			return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeProvisioningConflict, "Состав участников существующей компании не совпадает с запросом")
		}
		if seenRoles[row.Role] || row.ExternalUserID != expected.ExternalUserID || row.Email != expected.Email ||
			row.FirstName != expected.FirstName || textValue(row.LastName) != expected.LastName {
			return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeProvisioningConflict, "Состав участников существующей компании не совпадает с запросом")
		}
		seenRoles[row.Role] = true
		if row.ExternalUserID == input.InitiatorExternalUserID {
			initiator = &users[index]
		}
	}
	if initiator == nil || len(users) != 2 || !seenRoles["owner"] || !seenRoles["admin"] {
		return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeProvisioningConflict, "Состав участников существующей компании не совпадает с запросом")
	}
	if initiator.Status != "invited" {
		return ProvisionCompanyResult{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Учётная запись уже активирована; используйте вход через внешний сервис")
	}
	token, expiresAt, err := s.issueBootstrapActivation(
		ctx, queries, integration.CompanyID, initiator.ID, initiator.Role, "initiator",
		"provisioning_replayed", initiator.ID, now,
	)
	if err != nil {
		return ProvisionCompanyResult{}, err
	}
	if createRequest {
		if _, err = queries.CreateProvisioningRequest(ctx, db.CreateProvisioningRequestParams{
			Provider: input.Provider, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash,
			ExternalAccountID: input.ExternalAccountID, CompanyID: integration.CompanyID,
			IntegrationID: integration.ID, InitiatorUserID: initiator.ID,
			CreatedAt: now, ExpiresAt: now.Add(provisioningRequestTTL),
		}); err != nil {
			return ProvisionCompanyResult{}, provisioningWriteError("Не удалось сохранить provisioning-запрос", err)
		}
	}
	return ProvisionCompanyResult{
		CompanyID: integration.CompanyID, CompanyStatus: company.Status, Created: false,
		InitiatorRole: initiator.Role, BootstrapToken: token, BootstrapExpiresAt: expiresAt,
	}, nil
}

func (s *Service) issueBootstrapActivation(
	ctx context.Context,
	queries *db.Queries,
	companyID, userID uuid.UUID,
	role, purpose, revocationReason string,
	actorID uuid.UUID,
	now time.Time,
) (string, time.Time, error) {
	if revocationReason != "" {
		if _, err := queries.RevokeBootstrapActivations(ctx, db.RevokeBootstrapActivationsParams{
			RevokedAt: pgTimestamp(now), RevocationReason: pgtype.Text{String: revocationReason, Valid: true},
			CompanyID: companyID, UserID: userID,
		}); err != nil {
			return "", time.Time{}, internal("Не удалось аннулировать прежнюю ссылку активации", err)
		}
	}
	token, err := domainauth.NewOpaqueToken()
	if err != nil {
		return "", time.Time{}, internal("Не удалось выпустить ссылку активации", err)
	}
	expiresAt := now.Add(s.bootstrapDuration())
	activationID := uuid.New()
	if _, err = queries.CreateBootstrapActivation(ctx, db.CreateBootstrapActivationParams{
		ID: activationID, CompanyID: companyID, UserID: userID, Role: role, Purpose: purpose,
		TokenHash: domainauth.HashOpaqueToken(token), ExpiresAt: expiresAt, CreatedAt: now,
	}); err != nil {
		return "", time.Time{}, internal("Не удалось сохранить ссылку активации", err)
	}
	if err = s.emit(ctx, queries, companyID, actorID, "teamos.org.user.activation_created.v1", map[string]any{
		"activationId": activationID.String(), "userId": userID.String(), "role": orgRoleEventValue(role),
		"purpose": bootstrapPurposeEventValue(purpose), "expiresAt": expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *Service) bootstrapDuration() time.Duration {
	if s.bootstrapTTL > 0 {
		return s.bootstrapTTL
	}
	return defaultBootstrapTTL
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

func provisioningWriteError(message string, err error) error {
	if isUniqueViolation(err) {
		return conflict("Компания или пользователь уже существует")
	}
	return internal(message, err)
}

func pgTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func normalizeIssueSsoInput(input IssueSsoTokenInput) (IssueSsoTokenInput, error) {
	var err error
	input.Provider, err = normalizeExternalIdentifier(input.Provider, 32, "Укажите провайдера")
	if err != nil || !validProvider(input.Provider) {
		return IssueSsoTokenInput{}, validation("Некорректный провайдер")
	}
	input.ExternalAccountID, err = normalizeExternalIdentifier(input.ExternalAccountID, 255, "Укажите внешний аккаунт")
	if err != nil {
		return IssueSsoTokenInput{}, err
	}
	input.ExternalUserID, err = normalizeExternalIdentifier(input.ExternalUserID, 255, "Укажите внешнего пользователя")
	if err != nil {
		return IssueSsoTokenInput{}, err
	}
	return input, nil
}

func (s *Service) GetBootstrapActivation(ctx context.Context, token string) (BootstrapActivation, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return BootstrapActivation{}, bootstrapInvalid()
	}
	row, err := db.New(s.pool).GetBootstrapActivation(ctx, domainauth.HashOpaqueToken(token))
	if isNoRows(err) {
		return BootstrapActivation{}, bootstrapInvalid()
	}
	if err != nil {
		return BootstrapActivation{}, internal("Не удалось проверить ссылку активации", err)
	}
	if row.BootstrapActivation.ConsumedAt.Valid {
		return BootstrapActivation{}, bootstrapConsumed()
	}
	if !row.BootstrapActivation.ExpiresAt.After(s.now().UTC()) {
		return BootstrapActivation{}, bootstrapExpired()
	}
	if row.BootstrapActivation.RevokedAt.Valid {
		return BootstrapActivation{}, bootstrapInvalid()
	}
	if row.CompanyStatus == "frozen" || row.CompanyStatus == "suspended" || row.IntegrationStatus != "active" {
		return BootstrapActivation{}, integrationFrozen()
	}
	if row.CompanyStatus != "onboarding" {
		return BootstrapActivation{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
	}
	if row.User.Status == "deactivated" || row.User.ExternalDeletedAt.Valid || row.ExternalIdentityStatus != "active" {
		return BootstrapActivation{}, externalUserDeactivated()
	}
	if row.User.Status != "invited" {
		return BootstrapActivation{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
	}
	if !isProvisioningRole(row.User.Role) || row.User.Role != row.BootstrapActivation.Role {
		return BootstrapActivation{}, bootstrapInvalid()
	}
	return bootstrapActivationFromRow(
		row.BootstrapActivation, row.User, row.CompanyName, row.CompanyStatus, "pending",
	), nil
}

func (s *Service) CompleteBootstrapActivation(
	ctx context.Context,
	input CompleteBootstrapInput,
	meta SessionMeta,
) (CompleteBootstrapResult, error) {
	input.Token = strings.TrimSpace(input.Token)
	if input.Token == "" {
		return CompleteBootstrapResult{}, bootstrapInvalid()
	}
	// Reject random, expired and revoked links before spending CPU on Argon2id.
	// The activation is checked again under a row lock below, so this cheap
	// preflight does not weaken single-use semantics.
	if _, err := s.GetBootstrapActivation(ctx, input.Token); err != nil {
		return CompleteBootstrapResult{}, err
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось обработать пароль", err)
	}
	passwordHash, err := domainauth.HashPassword(input.Password)
	releasePasswordSlot()
	if err != nil {
		return CompleteBootstrapResult{}, validation(err.Error())
	}
	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось начать активацию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetBootstrapActivationForUpdate(ctx, domainauth.HashOpaqueToken(input.Token))
	if isNoRows(err) {
		return CompleteBootstrapResult{}, bootstrapInvalid()
	}
	if err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось проверить ссылку активации", err)
	}
	activation := row.BootstrapActivation
	if activation.ConsumedAt.Valid {
		return CompleteBootstrapResult{}, bootstrapConsumed()
	}
	if !activation.ExpiresAt.After(now) {
		return CompleteBootstrapResult{}, bootstrapExpired()
	}
	if activation.RevokedAt.Valid {
		return CompleteBootstrapResult{}, bootstrapInvalid()
	}
	if row.CompanyStatus == "frozen" || row.CompanyStatus == "suspended" || row.IntegrationStatus != "active" {
		return CompleteBootstrapResult{}, integrationFrozen()
	}
	if row.CompanyStatus != "onboarding" {
		return CompleteBootstrapResult{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
	}
	if row.User.Status == "deactivated" || row.User.ExternalDeletedAt.Valid || row.ExternalIdentityStatus != "active" {
		return CompleteBootstrapResult{}, externalUserDeactivated()
	}
	if row.User.Status != "invited" {
		return CompleteBootstrapResult{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
	}
	if !isProvisioningRole(row.User.Role) || row.User.Role != activation.Role {
		return CompleteBootstrapResult{}, bootstrapInvalid()
	}
	if _, err = queries.ConsumeBootstrapActivation(ctx, db.ConsumeBootstrapActivationParams{
		ConsumedAt: pgTimestamp(now), ID: activation.ID,
	}); isNoRows(err) {
		return CompleteBootstrapResult{}, bootstrapConsumed()
	} else if err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось погасить ссылку активации", err)
	}
	user, err := queries.ActivateBootstrapUser(ctx, db.ActivateBootstrapUserParams{
		ActivatedAt: now, CompanyID: activation.CompanyID, UserID: activation.UserID, Role: activation.Role,
	})
	if isNoRows(err) {
		return CompleteBootstrapResult{}, bootstrapConsumed()
	} else if err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось активировать учётную запись", err)
	}
	if err = queries.SetCredential(ctx, db.SetCredentialParams{
		CompanyID: user.CompanyID, UserID: user.ID, PasswordHash: passwordHash,
	}); err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось сохранить пароль", err)
	}
	onboarding := OnboardingState{CompanyID: user.CompanyID, CompanyStatus: "active", Completed: true}
	if _, err = queries.CompleteCompanyOnboarding(ctx, db.CompleteCompanyOnboardingParams{
		CompletedAt: pgTimestamp(now), CompanyID: user.CompanyID,
	}); isNoRows(err) {
		onboarding.CompanyStatus = "onboarding"
		onboarding.Completed = false
		pending, pendingErr := queries.GetPendingOnboardingUser(ctx, db.GetPendingOnboardingUserParams{
			CompanyID: user.CompanyID, ActivatedUserID: user.ID,
		})
		if pendingErr != nil {
			return CompleteBootstrapResult{}, internal("Не удалось найти второго участника онбординга", pendingErr)
		}
		nextToken, expiresAt, issueErr := s.issueBootstrapActivation(
			ctx, queries, pending.CompanyID, pending.ID, pending.Role, "second_user", "reissued", user.ID, now,
		)
		if issueErr != nil {
			return CompleteBootstrapResult{}, issueErr
		}
		pendingParticipant := bootstrapParticipantFromUser(pending)
		onboarding.PendingUser = &pendingParticipant
		onboarding.ActivationToken = &nextToken
		onboarding.ExpiresAt = &expiresAt
	} else if err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось завершить онбординг", err)
	} else {
		participants, listErr := queries.ListOnboardingParticipants(ctx, user.CompanyID)
		if listErr != nil {
			return CompleteBootstrapResult{}, internal("Не удалось получить участников завершённого онбординга", listErr)
		}
		var ownerID, adminID string
		for _, participant := range participants {
			switch participant.Role {
			case "owner":
				ownerID = participant.ID.String()
			case "admin":
				adminID = participant.ID.String()
			}
		}
		if err = s.emit(ctx, queries, user.CompanyID, user.ID, "teamos.company.onboarding.completed.v1", map[string]any{
			"companyId": user.CompanyID.String(), "ownerUserId": ownerID, "adminUserId": adminID,
		}); err != nil {
			return CompleteBootstrapResult{}, err
		}
	}
	if err = s.emit(ctx, queries, user.CompanyID, user.ID, "teamos.org.user.activated.v1", map[string]any{
		"user": userEventSnapshot(userFromDB(user, nil), nil), "activatedAt": now.Format(time.RFC3339Nano),
	}); err != nil {
		return CompleteBootstrapResult{}, err
	}
	if err = s.emit(ctx, queries, user.CompanyID, user.ID, "teamos.org.user.updated.v1", map[string]any{
		"user": userEventSnapshot(userFromDB(user, nil), nil), "changedFields": []string{"status"},
	}); err != nil {
		return CompleteBootstrapResult{}, err
	}
	session, err := s.createSession(ctx, queries, user, meta, uuid.NullUUID{})
	if err != nil {
		return CompleteBootstrapResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CompleteBootstrapResult{}, internal("Не удалось завершить активацию", err)
	}
	return CompleteBootstrapResult{Session: session, Onboarding: onboarding}, nil
}

func (s *Service) IssueSsoToken(ctx context.Context, input IssueSsoTokenInput) (IssueSsoTokenResult, error) {
	input, err := normalizeIssueSsoInput(input)
	if err != nil {
		return IssueSsoTokenResult{}, err
	}
	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось начать выпуск SSO-токена", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockProvisioningAccount(ctx, db.LockProvisioningAccountParams{
		Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
	}); err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось заблокировать внешний аккаунт", err)
	}
	row, err := queries.GetExternalIdentityForSSO(ctx, db.GetExternalIdentityForSSOParams{
		Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
		ExternalUserID: input.ExternalUserID,
	})
	if isNoRows(err) {
		return IssueSsoTokenResult{}, notFound("Пользователь внешнего сервиса")
	}
	if err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось найти внешнюю учётную запись", err)
	}
	if row.CompanyIntegration.Status != "active" || row.CompanyStatus == "frozen" || row.CompanyStatus == "suspended" {
		return IssueSsoTokenResult{}, integrationFrozen()
	}
	if row.UserExternalIdentity.Status != "active" || row.User.ExternalDeletedAt.Valid {
		return IssueSsoTokenResult{}, externalUserDeactivated()
	}
	if !isProvisioningRole(row.User.Role) {
		return IssueSsoTokenResult{}, forbidden("Вход из внешнего сервиса разрешён только владельцу и администратору")
	}
	if row.User.Status == "invited" {
		if row.CompanyStatus != "onboarding" {
			return IssueSsoTokenResult{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
		}
		activation, activationErr := queries.GetOpenBootstrapActivationForUser(ctx, db.GetOpenBootstrapActivationForUserParams{
			CompanyID: row.User.CompanyID, UserID: row.User.ID,
		})
		if isNoRows(activationErr) {
			return IssueSsoTokenResult{}, coded(ErrorConflict, ErrorCodeNoPendingActivation, "Для пользователя нет ожидающей активации")
		}
		if activationErr != nil {
			return IssueSsoTokenResult{}, internal("Не удалось проверить активацию пользователя", activationErr)
		}
		token, expiresAt, issueErr := s.issueBootstrapActivation(
			ctx, queries, row.User.CompanyID, row.User.ID, row.User.Role, activation.Purpose,
			"reissued", row.User.ID, now,
		)
		if issueErr != nil {
			return IssueSsoTokenResult{}, issueErr
		}
		if _, issueErr = queries.MarkExternalIdentityVerified(ctx, db.MarkExternalIdentityVerifiedParams{
			VerifiedAt: now, CompanyID: row.User.CompanyID, ID: row.UserExternalIdentity.ID,
		}); issueErr != nil {
			return IssueSsoTokenResult{}, internal("Не удалось отметить внешнюю личность", issueErr)
		}
		if issueErr = tx.Commit(ctx); issueErr != nil {
			return IssueSsoTokenResult{}, internal("Не удалось завершить выпуск ссылки активации", issueErr)
		}
		return IssueSsoTokenResult{Kind: "onboarding", Token: token, ExpiresAt: expiresAt}, nil
	}
	if row.User.Status != "active" {
		return IssueSsoTokenResult{}, externalUserDeactivated()
	}
	if _, err = queries.RevokeActiveSSOTokens(ctx, db.RevokeActiveSSOTokensParams{
		RevokedAt: pgTimestamp(now), ExternalIdentityID: row.UserExternalIdentity.ID,
	}); err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось аннулировать прежний SSO-токен", err)
	}
	token, err := domainauth.NewOpaqueToken()
	if err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось выпустить SSO-токен", err)
	}
	expiresAt := now.Add(s.ssoDuration())
	if _, err = queries.CreateSSOToken(ctx, db.CreateSSOTokenParams{
		ID: uuid.New(), CompanyID: row.User.CompanyID, UserID: row.User.ID,
		ExternalIdentityID: row.UserExternalIdentity.ID, TokenHash: domainauth.HashOpaqueToken(token),
		ExpiresAt: expiresAt, CreatedAt: now,
	}); err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось сохранить SSO-токен", err)
	}
	if _, err = queries.MarkExternalIdentityVerified(ctx, db.MarkExternalIdentityVerifiedParams{
		VerifiedAt: now, CompanyID: row.User.CompanyID, ID: row.UserExternalIdentity.ID,
	}); err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось отметить внешнюю личность", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return IssueSsoTokenResult{}, internal("Не удалось завершить выпуск SSO-токена", err)
	}
	return IssueSsoTokenResult{Kind: "sso", Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) ExchangeSsoToken(
	ctx context.Context,
	token string,
	meta SessionMeta,
) (AuthResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return AuthResult{}, ssoInvalid()
	}
	now := s.now().UTC()
	// READ COMMITTED is intentional: a concurrent waiter must observe the
	// winner's consumed_at after FOR UPDATE is released and return the stable
	// SSO_CONSUMED code instead of a serialization error from a stale snapshot.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AuthResult{}, internal("Не удалось начать SSO-вход", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetSSOTokenByHashForUpdate(ctx, domainauth.HashOpaqueToken(token))
	if isNoRows(err) {
		return AuthResult{}, ssoInvalid()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить SSO-токен", err)
	}
	if row.ConsumedAt.Valid {
		return AuthResult{}, ssoConsumed()
	}
	if !row.ExpiresAt.After(now) {
		return AuthResult{}, ssoExpired()
	}
	if row.RevokedAt.Valid {
		return AuthResult{}, ssoInvalid()
	}
	if row.IntegrationStatus != "active" || row.CompanyStatus == "frozen" || row.CompanyStatus == "suspended" {
		return AuthResult{}, integrationFrozen()
	}
	if row.ExternalIdentityStatus != "active" || row.User.ExternalDeletedAt.Valid {
		return AuthResult{}, externalUserDeactivated()
	}
	if row.User.Status != "active" {
		return AuthResult{}, externalUserDeactivated()
	}
	if !isProvisioningRole(row.User.Role) {
		return AuthResult{}, ssoInvalid()
	}
	if _, err = queries.ConsumeSSOToken(ctx, db.ConsumeSSOTokenParams{
		ConsumedAt: pgTimestamp(now), ID: row.SsoTokenID,
	}); isNoRows(err) {
		return AuthResult{}, ssoConsumed()
	} else if err != nil {
		return AuthResult{}, internal("Не удалось погасить SSO-токен", err)
	}
	sessionID := uuid.New()
	result, err := s.createSessionWithID(ctx, queries, row.User, meta, sessionID, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = s.emit(ctx, queries, row.User.CompanyID, row.User.ID, "teamos.company.auth.sso_login.v1", map[string]any{
		"userId": row.User.ID.String(), "provider": row.Provider,
		"externalAccountId": row.ExternalAccountID, "externalUserId": row.ExternalUserID,
		"sessionId": sessionID.String(),
	}); err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось завершить SSO-вход", err)
	}
	return result, nil
}

func (s *Service) GetOnboardingStatus(ctx context.Context, actor Actor) (OnboardingState, error) {
	if err := requireAdministrator(actor); err != nil {
		return OnboardingState{}, err
	}
	queries := db.New(s.pool)
	company, err := queries.GetCompany(ctx, actor.CompanyID)
	if isNoRows(err) {
		return OnboardingState{}, notFound("Компания")
	}
	if err != nil {
		return OnboardingState{}, internal("Не удалось получить состояние онбординга", err)
	}
	if company.Status == "frozen" || company.Status == "suspended" {
		return OnboardingState{}, integrationFrozen()
	}
	state := OnboardingState{
		CompanyID: company.ID, CompanyStatus: company.Status, Completed: company.Status == "active",
	}
	if state.Completed {
		return state, nil
	}
	if company.Status != "onboarding" {
		return OnboardingState{}, integrationFrozen()
	}
	integration, err := queries.GetOnboardingIntegration(ctx, actor.CompanyID)
	if isNoRows(err) {
		return OnboardingState{}, coded(ErrorConflict, ErrorCodeNoPendingActivation, "Интеграция онбординга не найдена")
	}
	if err != nil {
		return OnboardingState{}, internal("Не удалось проверить интеграцию онбординга", err)
	}
	if integration.Status != "active" {
		return OnboardingState{}, integrationFrozen()
	}
	pending, err := queries.GetPendingOnboardingUserForActor(ctx, db.GetPendingOnboardingUserForActorParams{
		CompanyID: actor.CompanyID, ActorUserID: actor.UserID,
	})
	if isNoRows(err) {
		return OnboardingState{}, coded(ErrorConflict, ErrorCodeNoPendingActivation, "Нет пользователя, ожидающего активации")
	}
	if err != nil {
		return OnboardingState{}, internal("Не удалось получить ожидающего участника", err)
	}
	participant := bootstrapParticipantFromUser(pending)
	state.PendingUser = &participant
	activation, activationErr := queries.GetOpenBootstrapActivationForUser(ctx, db.GetOpenBootstrapActivationForUserParams{
		CompanyID: pending.CompanyID, UserID: pending.ID,
	})
	if activationErr == nil {
		expiresAt := activation.ExpiresAt.UTC()
		state.ExpiresAt = &expiresAt
	} else if !isNoRows(activationErr) {
		return OnboardingState{}, internal("Не удалось получить срок действия ссылки", activationErr)
	}
	return state, nil
}

func (s *Service) ReissueOnboardingActivation(ctx context.Context, actor Actor) (OnboardingState, error) {
	if err := requireAdministrator(actor); err != nil {
		return OnboardingState{}, err
	}
	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return OnboardingState{}, internal("Не удалось начать перевыпуск ссылки", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	company, err := queries.GetCompanyForOnboardingUpdate(ctx, actor.CompanyID)
	if isNoRows(err) {
		return OnboardingState{}, notFound("Компания")
	}
	if err != nil {
		return OnboardingState{}, internal("Не удалось проверить онбординг", err)
	}
	if company.Status == "frozen" || company.Status == "suspended" {
		return OnboardingState{}, integrationFrozen()
	}
	if company.Status != "onboarding" {
		return OnboardingState{}, coded(ErrorConflict, ErrorCodeOnboardingCompleted, "Онбординг компании уже завершён")
	}
	integration, err := queries.GetOnboardingIntegrationForUpdate(ctx, actor.CompanyID)
	if isNoRows(err) {
		return OnboardingState{}, coded(ErrorConflict, ErrorCodeNoPendingActivation, "Интеграция онбординга не найдена")
	}
	if err != nil {
		return OnboardingState{}, internal("Не удалось проверить интеграцию онбординга", err)
	}
	if integration.Status != "active" {
		return OnboardingState{}, integrationFrozen()
	}
	pending, err := queries.GetPendingOnboardingUserForActor(ctx, db.GetPendingOnboardingUserForActorParams{
		CompanyID: actor.CompanyID, ActorUserID: actor.UserID,
	})
	if isNoRows(err) {
		return OnboardingState{}, coded(ErrorConflict, ErrorCodeNoPendingActivation, "Нет пользователя, ожидающего активации")
	}
	if err != nil {
		return OnboardingState{}, internal("Не удалось найти ожидающего участника", err)
	}
	token, expiresAt, err := s.issueBootstrapActivation(
		ctx, queries, pending.CompanyID, pending.ID, pending.Role, "second_user", "reissued", actor.UserID, now,
	)
	if err != nil {
		return OnboardingState{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OnboardingState{}, internal("Не удалось завершить перевыпуск ссылки", err)
	}
	participant := bootstrapParticipantFromUser(pending)
	return OnboardingState{
		CompanyID: company.ID, CompanyStatus: company.Status, Completed: false,
		PendingUser: &participant, ActivationToken: &token, ExpiresAt: &expiresAt,
	}, nil
}

func (s *Service) ssoDuration() time.Duration {
	if s.ssoTTL > 0 {
		return s.ssoTTL
	}
	return defaultSsoTTL
}

func isProvisioningRole(role string) bool {
	return role == "owner" || role == "admin"
}

func bootstrapActivationFromRow(
	activation db.BootstrapActivation,
	user db.User,
	companyName string,
	companyStatus string,
	state string,
) BootstrapActivation {
	return BootstrapActivation{
		CompanyID: activation.CompanyID, CompanyName: companyName, CompanyStatus: companyStatus,
		State: state, Participant: bootstrapParticipantFromUser(user), ExpiresAt: activation.ExpiresAt,
	}
}

func bootstrapParticipantFromUser(user db.User) BootstrapParticipant {
	return BootstrapParticipant{
		UserID: user.ID, Email: user.Email, FirstName: user.FirstName,
		LastName: textValue(user.LastName), Role: user.Role, Status: user.Status,
	}
}

func bootstrapPurposeEventValue(purpose string) string {
	return "ORG_USER_ACTIVATION_PURPOSE_" + strings.ToUpper(purpose)
}
