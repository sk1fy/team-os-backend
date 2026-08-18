package application

import (
	"context"

	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const amoSessionRedirectURL = "/schedule"

func (s *Service) CheckAmoSessionAccess(
	ctx context.Context,
	actor Actor,
	amoAccountID string,
) (AmoSessionAccess, error) {
	_, accountID, err := normalizeAmoAccount(amoWidgetProvider, amoAccountID)
	if err != nil {
		return AmoSessionAccess{}, err
	}
	queries := db.New(s.pool)
	integration, err := queries.GetCompanyIntegrationByExternalAccount(
		ctx,
		db.GetCompanyIntegrationByExternalAccountParams{
			Provider: amoWidgetProvider, ExternalAccountID: accountID,
		},
	)
	if isNoRows(err) {
		return AmoSessionAccess{}, coded(
			ErrorNotFound, ErrorCodeAmoSessionAccessNotFound, "Интеграция amoCRM не найдена",
		)
	}
	if err != nil {
		return AmoSessionAccess{}, internal("Не удалось проверить интеграцию amoCRM", err)
	}
	if integration.CompanyID != actor.CompanyID {
		return AmoSessionAccess{}, coded(
			ErrorConflict, ErrorCodeAmoSessionAccessMismatch,
			"Аккаунт amoCRM связан с другой компанией TeamOS",
		)
	}
	company, err := queries.GetCompany(ctx, actor.CompanyID)
	if err != nil {
		return AmoSessionAccess{}, internal("Не удалось проверить компанию TeamOS", err)
	}
	if integration.Status != "active" || company.Status != "active" {
		return AmoSessionAccess{}, coded(
			ErrorConflict, ErrorCodeAmoSessionAccessLocked, "Компания TeamOS временно заблокирована",
		)
	}
	user, err := queries.GetUser(ctx, db.GetUserParams{CompanyID: actor.CompanyID, ID: actor.UserID})
	if isNoRows(err) {
		return AmoSessionAccess{}, coded(
			ErrorForbidden, ErrorCodeAmoSessionAccessForbidden, "Доступ текущего пользователя отключён",
		)
	}
	if err != nil {
		return AmoSessionAccess{}, internal("Не удалось проверить текущего пользователя", err)
	}
	if user.Status != "active" || user.ExternalDeletedAt.Valid {
		return AmoSessionAccess{}, coded(
			ErrorForbidden, ErrorCodeAmoSessionAccessForbidden, "Доступ текущего пользователя отключён",
		)
	}
	if user.Role != "admin" && user.Role != "owner" {
		return AmoSessionAccess{}, coded(
			ErrorForbidden, ErrorCodeAmoSessionAccessForbidden,
			"Переход из amoCRM доступен только администраторам TeamOS",
		)
	}
	return AmoSessionAccess{Allowed: true, Role: user.Role, RedirectURL: amoSessionRedirectURL}, nil
}
