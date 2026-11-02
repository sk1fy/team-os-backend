package application

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/activitybridge"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type ActivityCore interface {
	Verify(context.Context, dto.ActivityScope) error
	Panels(context.Context, dto.ActivityScope) (dto.ActivityPanels, error)
	Create(context.Context, dto.ActivityScope, string, dto.ActivityPanelInput) (dto.ActivityPanel, error)
	Update(context.Context, dto.ActivityScope, uuid.UUID, dto.ActivityPanelPatch) (dto.ActivityPanel, error)
	Share(context.Context, dto.ActivityScope, uuid.UUID, string) (dto.ActivityPanel, error)
	Employees(context.Context, dto.ActivityScope) (dto.ActivityDirectory, error)
	TaskPanel(context.Context, dto.ActivityScope) (dto.ActivityTaskPanel, error)
	SaveTaskPanel(context.Context, dto.ActivityScope, dto.ActivityTaskPanel) (dto.ActivityTaskPanel, error)
}

func WithActivity(core ActivityCore, bindings []dto.ActivityScope) ServiceOption {
	return func(s *Service) {
		s.activityCore = core
		s.activityBindings = map[uuid.UUID]dto.ActivityScope{}
		for _, b := range bindings {
			s.activityBindings[b.CompanyId] = b
		}
	}
}
func (s *Service) activityScope(ctx context.Context, a Actor, expected *uuid.UUID) (dto.ActivityScope, bool, error) {
	q := db.New(s.pool)
	company, err := q.GetCompany(ctx, a.CompanyID)
	if err != nil {
		return dto.ActivityScope{}, false, internal("Не удалось проверить компанию", err)
	}
	user, err := q.GetUser(ctx, db.GetUserParams{CompanyID: a.CompanyID, ID: a.UserID})
	if isNoRows(err) {
		return dto.ActivityScope{}, false, forbidden("Пользователь недоступен")
	}
	if err != nil {
		return dto.ActivityScope{}, false, internal("Не удалось проверить пользователя", err)
	}
	if company.Status != "active" || user.Status != "active" || user.ExternalDeletedAt.Valid || (user.Role != "owner" && user.Role != "admin") {
		return dto.ActivityScope{}, false, forbidden("Настройки Activity доступны администраторам компании")
	}
	scope, ok := s.activityBindings[a.CompanyID]
	if !ok || s.activityCore == nil {
		return scope, false, nil
	}
	if expected != nil && *expected != activityBindingID(scope) {
		return scope, false, coded(ErrorConflict, "activity_binding_changed", "Подключение Activity изменилось. Обновите страницу перед изменением настроек")
	}
	if !company.AmoAccountID.Valid || company.AmoAccountID.String != scope.AccountId {
		return scope, false, forbidden("Аккаунт Activity отличается от текущего аккаунта компании")
	}
	integration, err := q.GetCompanyIntegrationByExternalAccount(ctx, db.GetCompanyIntegrationByExternalAccountParams{Provider: "rakurs", ExternalAccountID: scope.AccountId})
	if isNoRows(err) {
		return scope, false, forbidden("Аккаунт Activity не связан с компанией")
	}
	if err != nil {
		return scope, false, internal("Не удалось проверить аккаунт", err)
	}
	if integration.CompanyID != a.CompanyID || integration.Status != "active" {
		return scope, false, forbidden("Аккаунт Activity не связан с компанией")
	}
	if err = s.activityCore.Verify(ctx, scope); err != nil {
		return scope, true, activityError(err)
	}
	return scope, true, nil
}
func activityError(err error) error {
	if err == nil {
		return nil
	}
	var e *activitybridge.Error
	if !errors.As(err, &e) {
		return upstream("Activity временно недоступна", err)
	}
	switch e.Status {
	case 400, 422:
		return validation("Проверьте настройки Activity")
	case 401, 403:
		return coded(ErrorForbidden, "activity_access_denied", "Доступ Activity отключён. Проверьте установку и авторизацию amoCRM")
	case 404:
		return notFound("Панель Activity")
	case 409:
		return coded(ErrorConflict, "activity_revision_conflict", "Настройки уже изменились. Обновите данные и повторите сохранение")
	case 429:
		return &Error{Kind: ErrorRateLimited, Code: "activity_rate_limited", Message: "Слишком много запросов Activity. Повторите позже", Details: map[string]string{"retryAfter": e.RetryAfter}}
	default:
		return upstream("Activity временно недоступна. Изменения не подтверждены", err)
	}
}
func (s *Service) ActivityConnection(ctx context.Context, a Actor) (any, error) {
	scope, configured, err := s.activityScope(ctx, a, nil)
	if err != nil {
		var app *Error
		if errors.As(err, &app) && app.Kind == ErrorUpstream {
			return dto.ActivityConnection1{State: dto.Unavailable, Message: app.Message}, nil
		}
		return nil, err
	}
	if !configured {
		return dto.ActivityConnection1{State: dto.Unconfigured, Message: "Activity ещё не подключена к компании. Обратитесь к администратору сервиса"}, nil
	}
	return dto.ActivityConnection0{State: dto.Configured, Scope: scope, BindingId: activityBindingID(scope)}, nil
}
func (s *Service) activityRequired(ctx context.Context, a Actor, expected uuid.UUID) (dto.ActivityScope, error) {
	scope, ok, err := s.activityScope(ctx, a, &expected)
	if err != nil {
		return scope, err
	}
	if !ok {
		return scope, notFound("Подключение Activity")
	}
	return scope, nil
}
func (s *Service) ActivityPanels(ctx context.Context, a Actor, expected uuid.UUID) (dto.ActivityPanels, error) {
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityPanels{}, err
	}
	out, err := s.activityCore.Panels(ctx, scope)
	return out, activityError(err)
}
func (s *Service) ActivityEmployees(ctx context.Context, a Actor, expected uuid.UUID) (dto.ActivityDirectory, error) {
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityDirectory{}, err
	}
	out, err := s.activityCore.Employees(ctx, scope)
	return out, activityError(err)
}
func (s *Service) ActivityTaskPanel(ctx context.Context, a Actor, expected uuid.UUID) (dto.ActivityTaskPanel, error) {
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityTaskPanel{}, err
	}
	out, err := s.activityCore.TaskPanel(ctx, scope)
	return out, activityError(err)
}
func (s *Service) CreateActivityPanel(ctx context.Context, a Actor, expected uuid.UUID, key uuid.UUID, in dto.ActivityPanelInput) (dto.ActivityPanel, error) {
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityPanel{}, err
	}
	out, err := s.activityCore.Create(ctx, scope, key.String(), in)
	return out, activityError(err)
}
func (s *Service) UpdateActivityPanel(ctx context.Context, a Actor, expected uuid.UUID, id uuid.UUID, in dto.ActivityPanelPatch) (dto.ActivityPanel, error) {
	if in.Revision < 1 {
		return dto.ActivityPanel{}, validation("Требуется версия панели")
	}
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityPanel{}, err
	}
	out, err := s.activityCore.Update(ctx, scope, id, in)
	return out, activityError(err)
}
func (s *Service) ShareActivityPanel(ctx context.Context, a Actor, expected uuid.UUID, id, key uuid.UUID) (dto.ActivityPanel, error) {
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityPanel{}, err
	}
	out, err := s.activityCore.Share(ctx, scope, id, key.String())
	return out, activityError(err)
}
func (s *Service) SaveActivityTaskPanel(ctx context.Context, a Actor, expected uuid.UUID, in dto.ActivityTaskPanel) (dto.ActivityTaskPanel, error) {
	scope, err := s.activityRequired(ctx, a, expected)
	if err != nil {
		return dto.ActivityTaskPanel{}, err
	}
	out, err := s.activityCore.SaveTaskPanel(ctx, scope, in)
	return out, activityError(err)
}

func activityBindingID(scope dto.ActivityScope) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("teamos-activity/v1/"+scope.CompanyId.String()+"/"+scope.AccountId+"/"+scope.IntegrationId.String()+"/"+scope.InstallationId.String()))
}
