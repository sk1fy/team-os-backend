package application

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const maxAmoAdminAssertionUsers = 250

func (s *Service) AmoAdminSelfLogin(
	ctx context.Context,
	input AmoAdminSelfLoginInput,
) (AmoAdminSelfLoginResult, error) {
	_, accountID, err := normalizeAmoAccount(amoWidgetProvider, input.AmoAccountID)
	if err != nil {
		return AmoAdminSelfLoginResult{}, err
	}
	selfUserID, assertion, err := validateAmoAdminAssertion(input.SelfUserID, input.Users)
	if err != nil {
		return AmoAdminSelfLoginResult{}, err
	}
	queries := db.New(s.pool)
	integration, err := queries.GetCompanyIntegrationByExternalAccount(ctx, db.GetCompanyIntegrationByExternalAccountParams{
		Provider: amoWidgetProvider, ExternalAccountID: accountID,
	})
	if isNoRows(err) {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorNotFound, ErrorCodeAmoAdminSelfLoginNotFound, "Компания для аккаунта amoCRM не найдена",
		)
	}
	if err != nil {
		return AmoAdminSelfLoginResult{}, internal("Не удалось проверить аккаунт amoCRM", err)
	}
	company, err := queries.GetCompany(ctx, integration.CompanyID)
	if err != nil {
		return AmoAdminSelfLoginResult{}, internal("Не удалось проверить компанию TeamOS", err)
	}
	if !company.AmoAccountID.Valid || company.AmoAccountID.String != accountID {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorConflict, ErrorCodeAmoAdminAssertionInvalid, "Привязка аккаунта amoCRM не совпадает с компанией",
		)
	}
	if integration.Status != "active" || company.Status != "active" {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorForbidden, ErrorCodeAmoAdminSelfLoginForbidden, "Компания TeamOS временно недоступна",
		)
	}
	if s.externalUsers == nil {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorUpstream, ErrorCodeAmoAdminSelfLoginUnavailable, "Импорт сотрудников amoCRM временно недоступен",
		)
	}
	actorID := uuid.Nil
	if company.OwnerID.Valid {
		actorID = company.OwnerID.UUID
	}
	// This deliberately bypasses the normal TTL guard. The client-provided
	// assertion is spoofable and is only an additional signal after the server's
	// authoritative employee reconciliation has completed successfully.
	if syncErr := s.syncAmoUsersNow(ctx, Actor{CompanyID: integration.CompanyID, UserID: actorID, Role: "admin"}); syncErr != nil {
		return AmoAdminSelfLoginResult{}, &Error{
			Kind: ErrorUpstream, Code: ErrorCodeAmoAdminSelfLoginUnavailable,
			Message: "Не удалось обновить сотрудников amoCRM", Cause: syncErr,
		}
	}
	if !assertion.IsAdmin || !assertion.IsActive {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorForbidden, ErrorCodeAmoAdminSelfLoginForbidden,
			"Текущий пользователь не является активным администратором amoCRM",
		)
	}

	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AmoAdminSelfLoginResult{}, internal("Не удалось начать вход администратора amoCRM", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txQueries := db.New(tx)
	if err = txQueries.LockAmoAccount(ctx, db.LockAmoAccountParams{
		Provider: amoWidgetProvider, ExternalAccountID: accountID,
	}); err != nil {
		return AmoAdminSelfLoginResult{}, internal("Не удалось заблокировать аккаунт amoCRM", err)
	}
	locked, lockedCompany, _, err := s.getOrCreateAmoWidgetCompany(
		ctx, txQueries, accountID, company.Name, now, false,
	)
	if err != nil {
		return AmoAdminSelfLoginResult{}, err
	}
	if locked.ID != integration.ID || locked.CompanyID != integration.CompanyID ||
		locked.Status != "active" || lockedCompany.Status != "active" ||
		!lockedCompany.AmoAccountID.Valid || lockedCompany.AmoAccountID.String != accountID {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorConflict, ErrorCodeAmoAdminAssertionInvalid, "Привязка аккаунта amoCRM изменилась",
		)
	}
	user, err := txQueries.FindAmoWidgetUserForUpdate(ctx, db.FindAmoWidgetUserForUpdateParams{
		CompanyID:  integration.CompanyID,
		ExternalID: pgtype.Text{String: selfUserID, Valid: true},
	})
	if isNoRows(err) {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorNotFound, ErrorCodeAmoAdminSelfLoginNotFound, "Пользователь amoCRM не найден в TeamOS",
		)
	}
	if err != nil {
		return AmoAdminSelfLoginResult{}, internal("Не удалось найти пользователя amoCRM", err)
	}
	if user.Source != "amo" || !user.ExternalID.Valid || user.ExternalID.String != selfUserID ||
		user.ExternalDeletedAt.Valid || user.Status != "active" {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorForbidden, ErrorCodeAmoAdminSelfLoginForbidden, "Учётная запись TeamOS отключена",
		)
	}
	user, identity, _, err := s.getOrCreateAmoWidgetUser(
		ctx, txQueries, integration.CompanyID, integration.ID, accountID, selfUserID,
		user.Email, user.FirstName, textValue(user.LastName), now,
	)
	if err != nil {
		return AmoAdminSelfLoginResult{}, err
	}
	if identity.ExternalAccountID != accountID || identity.ExternalUserID != selfUserID || identity.UserID != user.ID {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorConflict, ErrorCodeAmoAdminAssertionInvalid, "Внешняя учётная запись связана некорректно",
		)
	}
	previousRole := user.Role
	if user.Role == "employee" {
		user, err = txQueries.PromoteAmoWidgetAdmin(ctx, db.PromoteAmoWidgetAdminParams{
			UpdatedAt: now, CompanyID: integration.CompanyID, UserID: user.ID,
		})
		if err != nil {
			return AmoAdminSelfLoginResult{}, internal("Не удалось назначить администратора компании", err)
		}
		if err = replaceEmployeeSections(ctx, txQueries, integration.CompanyID, user.ID, nil, nil); err != nil {
			return AmoAdminSelfLoginResult{}, err
		}
		if err = revokeUserSessions(ctx, txQueries, user.ID, now); err != nil {
			return AmoAdminSelfLoginResult{}, err
		}
		if err = s.emitAmoWidgetRoleChanged(ctx, txQueries, user, user.ID); err != nil {
			return AmoAdminSelfLoginResult{}, err
		}
	} else if user.Role != "admin" && user.Role != "owner" {
		return AmoAdminSelfLoginResult{}, coded(
			ErrorForbidden, ErrorCodeAmoAdminSelfLoginForbidden, "Роль пользователя TeamOS не допускает вход",
		)
	}
	if err = createUserAdminAudit(
		ctx, txQueries, integration.CompanyID, &user.ID, nil, "system", "amo_admin_self_login",
		map[string]any{"role": previousRole, "assertionSource": "client_snapshot"},
		map[string]any{"role": user.Role, "assertionSource": "client_snapshot"},
		input.RequestID, now,
	); err != nil {
		return AmoAdminSelfLoginResult{}, err
	}
	link, err := ensureAmoWidgetAccessLink(ctx, txQueries, integration.CompanyID, user.ID)
	if err != nil {
		return AmoAdminSelfLoginResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AmoAdminSelfLoginResult{}, internal("Не удалось завершить вход администратора amoCRM", err)
	}
	return AmoAdminSelfLoginResult{Allowed: true, Action: "login", Role: user.Role, AccessToken: link.Token}, nil
}

func validateAmoAdminAssertion(
	selfUserID string,
	users []AmoAdminUserAssertion,
) (string, AmoAdminUserAssertion, error) {
	if !canonicalAmoID(selfUserID) || len(users) == 0 || len(users) > maxAmoAdminAssertionUsers {
		return "", AmoAdminUserAssertion{}, validation("Некорректный снимок пользователей amoCRM")
	}
	seen := make(map[string]struct{}, len(users))
	var target AmoAdminUserAssertion
	found := false
	for _, user := range users {
		id := user.ID
		if !canonicalAmoID(id) {
			return "", AmoAdminUserAssertion{}, validation("Некорректный ID пользователя amoCRM")
		}
		if _, duplicate := seen[id]; duplicate {
			return "", AmoAdminUserAssertion{}, validation("Снимок пользователей amoCRM содержит дубликаты")
		}
		seen[id] = struct{}{}
		if id == selfUserID {
			target = user
			target.ID = id
			found = true
		}
	}
	if !found {
		return "", AmoAdminUserAssertion{}, validation("Текущий пользователь отсутствует в снимке amoCRM")
	}
	return selfUserID, target, nil
}

func canonicalAmoID(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}
