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

func (s *Service) ProvisionAmoAdminSession(
	ctx context.Context,
	input AmoAdminSessionInput,
) (AmoAdminSessionResult, error) {
	provider, accountID, err := normalizeAmoAccount(input.Provider, input.ExternalAccountID)
	if err != nil {
		return AmoAdminSessionResult{}, err
	}
	externalUserID := strings.TrimSpace(input.ExternalUserID)
	parsedUserID, parseErr := strconv.ParseInt(externalUserID, 10, 64)
	if parseErr != nil || parsedUserID <= 0 || strconv.FormatInt(parsedUserID, 10) != externalUserID {
		return AmoAdminSessionResult{}, validation("Некорректный ID пользователя amoCRM")
	}
	desiredRole := strings.TrimSpace(input.DesiredRole)
	if desiredRole != "admin" && desiredRole != "owner" {
		return AmoAdminSessionResult{}, validation("Роль пользователя amoCRM должна быть admin или owner")
	}
	email, firstName, lastName, companyName, err := normalizeAmoWidgetProfile(AmoWidgetSessionInput{
		Email: input.Email, UserName: input.UserName, CompanyName: input.CompanyName,
	}, accountID)
	if err != nil {
		return AmoAdminSessionResult{}, err
	}

	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AmoAdminSessionResult{}, internal("Не удалось начать вход администратора amoCRM", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoAccount(ctx, db.LockAmoAccountParams{
		Provider: provider, ExternalAccountID: accountID,
	}); err != nil {
		return AmoAdminSessionResult{}, internal("Не удалось заблокировать аккаунт amoCRM", err)
	}
	integration, company, companyCreated, err := s.getOrCreateAmoWidgetCompany(
		ctx, queries, accountID, companyName, now, false,
	)
	if err != nil {
		return AmoAdminSessionResult{}, err
	}
	if integration.Status != "active" || company.Status == "frozen" || company.Status == "suspended" {
		return AmoAdminSessionResult{}, coded(
			ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable, "Компания TeamOS временно недоступна",
		)
	}
	user, _, userCreated, err := s.getOrCreateAmoWidgetUser(
		ctx, queries, company.ID, integration.ID, accountID,
		externalUserID, email, firstName, lastName, now,
	)
	if err != nil {
		return AmoAdminSessionResult{}, err
	}
	previousRole := user.Role
	previousOwnerID := company.OwnerID
	roleChange := planAmoAdminRoleChange(user.ID, previousRole, desiredRole, previousOwnerID)
	if roleChange.AssignOwner {
		user, err = queries.PromoteAmoWidgetOwner(ctx, db.PromoteAmoWidgetOwnerParams{
			UpdatedAt: now, CompanyID: company.ID, UserID: user.ID,
		})
		if err != nil {
			return AmoAdminSessionResult{}, internal("Не удалось назначить владельца компании", err)
		}
		company, err = queries.SetCompanyOwner(ctx, db.SetCompanyOwnerParams{
			ID: company.ID, OwnerID: uuid.NullUUID{UUID: user.ID, Valid: true},
		})
		if err != nil {
			return AmoAdminSessionResult{}, internal("Не удалось назначить владельца компании", err)
		}
		if roleChange.PreviousOwnerID.Valid {
			var demoted int64
			if demoted, err = queries.DemotePreviousAmoWidgetOwner(ctx, db.DemotePreviousAmoWidgetOwnerParams{
				UpdatedAt: now, CompanyID: company.ID, PreviousOwnerID: roleChange.PreviousOwnerID.UUID, NewOwnerID: user.ID,
			}); err != nil {
				return AmoAdminSessionResult{}, internal("Не удалось обновить прежнего владельца компании", err)
			}
			if err = revokeUserSessions(ctx, queries, roleChange.PreviousOwnerID.UUID, now); err != nil {
				return AmoAdminSessionResult{}, err
			}
			if demoted > 0 {
				previousOwner, ownerErr := queries.GetUser(ctx, db.GetUserParams{
					CompanyID: company.ID, ID: roleChange.PreviousOwnerID.UUID,
				})
				if ownerErr != nil {
					return AmoAdminSessionResult{}, internal("Не удалось получить прежнего владельца компании", ownerErr)
				}
				if err = s.emitAmoWidgetRoleChanged(ctx, queries, previousOwner, user.ID); err != nil {
					return AmoAdminSessionResult{}, err
				}
			}
		}
	} else {
		user, err = queries.PromoteAmoWidgetAdmin(ctx, db.PromoteAmoWidgetAdminParams{
			UpdatedAt: now, CompanyID: company.ID, UserID: user.ID,
		})
		if err != nil {
			return AmoAdminSessionResult{}, internal("Не удалось назначить администратора компании", err)
		}
	}
	if previousRole != user.Role {
		if err = replaceEmployeeSections(ctx, queries, company.ID, user.ID, nil, nil); err != nil {
			return AmoAdminSessionResult{}, err
		}
		if err = revokeUserSessions(ctx, queries, user.ID, now); err != nil {
			return AmoAdminSessionResult{}, err
		}
	}
	if companyCreated {
		ownerUserID := ""
		if company.OwnerID.Valid {
			ownerUserID = company.OwnerID.UUID.String()
		}
		if err = s.emit(ctx, queries, company.ID, user.ID, "teamos.company.company.created.v1", map[string]any{
			"companyId": company.ID.String(), "name": company.Name, "ownerUserId": ownerUserID,
		}); err != nil {
			return AmoAdminSessionResult{}, err
		}
	}
	if userCreated {
		createdUser, loginErr := userFromDBWithLogin(ctx, queries, user, nil)
		if loginErr != nil {
			return AmoAdminSessionResult{}, loginErr
		}
		if err = s.emit(ctx, queries, company.ID, user.ID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(createdUser, nil),
		}); err != nil {
			return AmoAdminSessionResult{}, err
		}
	} else if previousRole != user.Role {
		if err = s.emitAmoWidgetRoleChanged(ctx, queries, user, user.ID); err != nil {
			return AmoAdminSessionResult{}, err
		}
	}
	accessLink, err := ensureAmoWidgetAccessLink(ctx, queries, company.ID, user.ID)
	if err != nil {
		return AmoAdminSessionResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AmoAdminSessionResult{}, internal("Не удалось завершить вход администратора amoCRM", err)
	}

	if s.externalUsers != nil {
		_ = s.syncAmoUsers(ctx, Actor{CompanyID: company.ID, UserID: user.ID, Role: user.Role})
	}
	action := "login"
	if companyCreated {
		action = "register"
	}
	return AmoAdminSessionResult{
		Action: action, ExternalAccountID: accountID, CompanyID: company.ID,
		UserID: user.ID, Role: user.Role, AccessToken: accessLink.Token,
	}, nil
}

type amoAdminRoleChange struct {
	TargetRole      string
	AssignOwner     bool
	PreviousOwnerID uuid.NullUUID
}

func planAmoAdminRoleChange(
	userID uuid.UUID,
	currentRole, desiredRole string,
	currentOwnerID uuid.NullUUID,
) amoAdminRoleChange {
	if desiredRole == "owner" {
		change := amoAdminRoleChange{TargetRole: "owner", AssignOwner: true}
		if currentOwnerID.Valid && currentOwnerID.UUID != userID {
			change.PreviousOwnerID = currentOwnerID
		}
		return change
	}
	if currentRole == "owner" {
		return amoAdminRoleChange{TargetRole: "owner"}
	}
	return amoAdminRoleChange{TargetRole: "admin"}
}

func ensureAmoWidgetAccessLink(
	ctx context.Context,
	queries *db.Queries,
	companyID, userID uuid.UUID,
) (db.AccessLink, error) {
	accessLink, err := queries.GetAccessLink(ctx, db.GetAccessLinkParams{CompanyID: companyID, UserID: userID})
	if err == nil {
		return accessLink, nil
	}
	if !isNoRows(err) {
		return db.AccessLink{}, internal("Не удалось проверить ссылку доступа", err)
	}
	token, err := domainauth.NewAccessLinkToken()
	if err != nil {
		return db.AccessLink{}, internal("Не удалось создать ссылку доступа", err)
	}
	accessLink, err = queries.UpsertAccessLink(ctx, db.UpsertAccessLinkParams{
		CompanyID: companyID, UserID: userID, Token: token,
	})
	if err != nil {
		return db.AccessLink{}, internal("Не удалось сохранить ссылку доступа", err)
	}
	return accessLink, nil
}

func (s *Service) emitAmoWidgetRoleChanged(
	ctx context.Context,
	queries *db.Queries,
	user db.User,
	actorID uuid.UUID,
) error {
	positionIDs, err := queries.GetUserPositionIDs(ctx, db.GetUserPositionIDsParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	})
	if err != nil {
		return internal("Не удалось получить должность пользователя", err)
	}
	departmentIDs, err := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	})
	if err != nil {
		return internal("Не удалось получить отделы пользователя", err)
	}
	updatedUser, err := userFromDBWithLogin(ctx, queries, user, positionIDs)
	if err != nil {
		return err
	}
	return s.emit(ctx, queries, user.CompanyID, actorID, "teamos.org.user.updated.v1", map[string]any{
		"user": userEventSnapshot(updatedUser, departmentIDs), "changedFields": []string{"role", "sectionAccess"},
	})
}

func (s *Service) ExchangeAmoWidgetSession(
	ctx context.Context,
	input AmoWidgetSessionInput,
) (AmoWidgetSessionResult, error) {
	identity, accountID, err := s.verifyAmoWidgetAccess(ctx, input.Token)
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	adminSession, err := s.ProvisionAmoAdminSession(ctx, verifiedAmoAdminSessionInput(identity, accountID))
	if err != nil {
		return AmoWidgetSessionResult{}, err
	}
	return AmoWidgetSessionResult{
		Action: adminSession.Action, ExternalAccountID: adminSession.ExternalAccountID,
		AccessToken: adminSession.AccessToken, Role: adminSession.Role,
	}, nil
}

func verifiedAmoAdminSessionInput(identity amoauth.Identity, accountID string) AmoAdminSessionInput {
	desiredRole := "admin"
	if identity.IsOwner {
		desiredRole = "owner"
	}
	return AmoAdminSessionInput{
		Provider: amoWidgetProvider, ExternalAccountID: accountID,
		ExternalUserID: strconv.FormatInt(identity.UserID, 10), Email: identity.UserEmail,
		UserName: identity.UserName, CompanyName: identity.AccountName, DesiredRole: desiredRole,
	}
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
	if s.amoWidgetTokenVerifier == nil {
		return amoauth.Identity{}, "", coded(
			ErrorUpstream, ErrorCodeAmoWidgetSessionUnavailable, "Вход через amoCRM временно недоступен",
		)
	}
	identity, err := s.amoWidgetTokenVerifier.Verify(ctx, strings.TrimSpace(token))
	if errors.Is(err, amoauth.ErrNotConfigured) || errors.Is(err, amoauth.ErrUnavailable) {
		return amoauth.Identity{}, "", coded(
			ErrorUpstream, ErrorCodeAmoWidgetSessionUnavailable, "Вход через amoCRM временно недоступен",
		)
	}
	if errors.Is(err, amoauth.ErrForbidden) {
		return amoauth.Identity{}, "", coded(
			ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable,
			"Пользователь amoCRM не имеет доступа администратора к TeamOS",
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
		slog.String("amo_jti", identity.JTI),
	)
	if !identity.IsAdmin {
		return amoauth.Identity{}, "", coded(
			ErrorForbidden, ErrorCodeAmoWidgetSessionUnavailable,
			"Пользователь amoCRM не имеет доступа администратора к TeamOS",
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
		createdUser, loginErr := userFromDBWithLogin(ctx, queries, user, nil)
		if loginErr != nil {
			return AmoWidgetSessionResult{}, loginErr
		}
		if err = s.emit(ctx, queries, company.ID, user.ID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(createdUser, nil),
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
		if stateErr := validateAmoWidgetUserState(identityRow.User); stateErr != nil {
			return db.User{}, db.UserExternalIdentity{}, false, stateErr
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
	if stateErr := validateAmoWidgetUserState(user); stateErr != nil {
		return db.User{}, db.UserExternalIdentity{}, false, stateErr
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

func validateAmoWidgetUserState(user db.User) error {
	if user.Status != "active" || user.ExternalDeletedAt.Valid {
		return coded(
			ErrorForbidden,
			ErrorCodeAmoWidgetSessionUnavailable,
			"Учётная запись TeamOS деактивирована администратором компании",
		)
	}
	return nil
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
		Email: row.Email, Login: row.Login, CompanyName: row.CompanyName,
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
