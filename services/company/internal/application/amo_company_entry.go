package application

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type AmoEntrySubject struct {
	AccountID      string `json:"accountId"`
	ActorID        string `json:"actorId"`
	IntegrationID  string `json:"integrationId"`
	InstallationID string `json:"installationId"`
}

type AmoEntrySubjectChecker interface {
	Check(context.Context, AmoEntrySubject) (bool, error)
}

func WithAmoEntrySubjectChecker(checker AmoEntrySubjectChecker) ServiceOption {
	return func(s *Service) { s.amoEntrySubjectChecker = checker }
}

type AmoWidgetEntryInput struct {
	AmoEntrySubject
	Intent string
	Name   string
	Email  string
}

type AmoWidgetEntryResult struct {
	CompanyID uuid.UUID
	UserID    uuid.UUID
	Code      string
	ExpiresAt time.Time
}

type AmoEntryCompanyStatus struct {
	State     string
	CompanyID *uuid.UUID
}

func (s *Service) GetAmoEntryCompanyStatus(ctx context.Context, accountID string) (AmoEntryCompanyStatus, error) {
	if !canonicalAmoEntryID(accountID) {
		return AmoEntryCompanyStatus{}, validation("Некорректный ID аккаунта amoCRM")
	}
	return amoEntryCompanyStatus(ctx, db.New(s.pool), accountID)
}

func amoEntryCompanyStatus(ctx context.Context, q *db.Queries, accountID string) (AmoEntryCompanyStatus, error) {
	rows, err := q.GetAmoEntryCompanyStatus(ctx, pgtype.Text{String: accountID, Valid: true})
	if err != nil {
		return AmoEntryCompanyStatus{}, internal("Не удалось проверить компанию TeamOS", err)
	}
	if len(rows) == 0 {
		return AmoEntryCompanyStatus{State: "absent"}, nil
	}
	if len(rows) != 1 {
		return AmoEntryCompanyStatus{State: "blocked"}, nil
	}
	id := rows[0].ID
	state := "present"
	if rows[0].Status != "active" || rows[0].IntegrationStatus != "active" {
		state = "blocked"
	}
	return AmoEntryCompanyStatus{State: state, CompanyID: &id}, nil
}

func canonicalAmoEntryID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

func validateAmoEntrySubject(subject AmoEntrySubject) error {
	if !canonicalAmoEntryID(subject.AccountID) || !canonicalAmoEntryID(subject.ActorID) {
		return validation("Некорректный субъект amoCRM")
	}
	for _, raw := range []string{subject.IntegrationID, subject.InstallationID} {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw {
			return validation("Некорректная установка amoCRM")
		}
	}
	return nil
}

func (s *Service) ProvisionAmoWidgetEntry(ctx context.Context, input AmoWidgetEntryInput) (AmoWidgetEntryResult, error) {
	if err := validateAmoEntrySubject(input.AmoEntrySubject); err != nil {
		return AmoWidgetEntryResult{}, err
	}
	if input.Intent != "create" && input.Intent != "enter" {
		return AmoWidgetEntryResult{}, validation("Укажите действие create или enter")
	}
	if s.amoEntrySubjectChecker == nil {
		return AmoWidgetEntryResult{}, amoEntryUnavailable()
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AmoWidgetEntryResult{}, internal("Не удалось начать вход через amoCRM", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if err = q.LockAmoAccount(ctx, db.LockAmoAccountParams{Provider: amoWidgetProvider, ExternalAccountID: input.AccountID}); err != nil {
		return AmoWidgetEntryResult{}, internal("Не удалось заблокировать аккаунт amoCRM", err)
	}
	status, err := amoEntryCompanyStatus(ctx, q, input.AccountID)
	if err != nil {
		return AmoWidgetEntryResult{}, err
	}
	if status.State == "blocked" {
		return AmoWidgetEntryResult{}, forbidden("Компания TeamOS временно недоступна")
	}
	if status.State == "absent" && input.Intent == "enter" {
		return AmoWidgetEntryResult{}, conflict("Компания не найдена. Обновите настройки виджета")
	}
	now := s.now().UTC()
	integration, company, companyCreated, err := s.getOrCreateAmoWidgetCompany(ctx, q, input.AccountID, "Компания amoCRM "+input.AccountID, now, false)
	if err != nil {
		return AmoWidgetEntryResult{}, err
	}
	if company.Status != "active" || integration.Status != "active" || integration.ExternalAccountID != input.AccountID || (company.AmoAccountID.Valid && company.AmoAccountID.String != input.AccountID) {
		return AmoWidgetEntryResult{}, forbidden("Компания TeamOS временно недоступна")
	}
	user, identity, userCreated, err := s.resolveAmoEntryUser(ctx, q, company.ID, integration.ID, input, now)
	if err != nil {
		return AmoWidgetEntryResult{}, err
	}
	previousRole := user.Role
	if companyCreated {
		user, err = q.PromoteAmoWidgetOwner(ctx, db.PromoteAmoWidgetOwnerParams{UpdatedAt: now, CompanyID: company.ID, UserID: user.ID})
		if err == nil {
			company, err = q.SetCompanyOwner(ctx, db.SetCompanyOwnerParams{ID: company.ID, OwnerID: uuid.NullUUID{UUID: user.ID, Valid: true}})
		}
	} else {
		user, err = q.PromoteAmoWidgetAdmin(ctx, db.PromoteAmoWidgetAdminParams{UpdatedAt: now, CompanyID: company.ID, UserID: user.ID})
	}
	if err != nil {
		return AmoWidgetEntryResult{}, internal("Не удалось назначить администратора TeamOS", err)
	}
	if previousRole != user.Role {
		if err = replaceEmployeeSections(ctx, q, company.ID, user.ID, nil, nil); err != nil {
			return AmoWidgetEntryResult{}, err
		}
		if err = revokeUserSessions(ctx, q, user.ID, now); err != nil {
			return AmoWidgetEntryResult{}, err
		}
	}
	if companyCreated {
		if err = s.emit(ctx, q, company.ID, user.ID, "teamos.company.company.created.v1", map[string]any{"companyId": company.ID.String(), "name": company.Name, "ownerUserId": user.ID.String()}); err != nil {
			return AmoWidgetEntryResult{}, err
		}
	}
	if userCreated {
		createdUser, e := userFromDBWithLogin(ctx, q, user, nil)
		if e != nil {
			return AmoWidgetEntryResult{}, e
		}
		if err = s.emit(ctx, q, company.ID, user.ID, "teamos.org.user.created.v1", map[string]any{"user": userEventSnapshot(createdUser, nil)}); err != nil {
			return AmoWidgetEntryResult{}, err
		}
	} else if previousRole != user.Role {
		if err = s.emitAmoWidgetRoleChanged(ctx, q, user, user.ID); err != nil {
			return AmoWidgetEntryResult{}, err
		}
	}
	code, err := domainauth.NewAccessLinkToken()
	if err != nil {
		return AmoWidgetEntryResult{}, internal("Не удалось создать ссылку входа", err)
	}
	expires := now.Add(time.Minute)
	_, err = q.CreateAmoCompanyEntry(ctx, db.CreateAmoCompanyEntryParams{
		ID: uuid.New(), CompanyID: company.ID, UserID: user.ID, ExternalIdentityID: identity.ID,
		AccountID: input.AccountID, ActorID: input.ActorID, CoreIntegrationID: uuid.MustParse(input.IntegrationID), CoreInstallationID: uuid.MustParse(input.InstallationID),
		TokenHash: domainauth.HashOpaqueToken(code), CreatedAt: now, ExpiresAt: expires,
	})
	if err != nil {
		return AmoWidgetEntryResult{}, internal("Не удалось сохранить ссылку входа", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return AmoWidgetEntryResult{}, internal("Не удалось завершить подготовку входа", err)
	}
	return AmoWidgetEntryResult{CompanyID: company.ID, UserID: user.ID, Code: code, ExpiresAt: expires}, nil
}

func (s *Service) resolveAmoEntryUser(ctx context.Context, q *db.Queries, companyID, integrationID uuid.UUID, input AmoWidgetEntryInput, now time.Time) (db.User, db.UserExternalIdentity, bool, error) {
	row, err := q.GetAmoWidgetUserByIdentity(ctx, db.GetAmoWidgetUserByIdentityParams{CompanyID: companyID, IntegrationID: integrationID, Provider: amoWidgetProvider, ExternalUserID: input.ActorID})
	if err == nil {
		if row.UserExternalIdentity.Status != "active" || row.UserExternalIdentity.ExternalAccountID != input.AccountID {
			return db.User{}, db.UserExternalIdentity{}, false, forbidden("Учётная запись TeamOS отключена")
		}
		if err = validateAmoWidgetUserState(row.User); err != nil {
			return db.User{}, db.UserExternalIdentity{}, false, err
		}
		identity, touchErr := q.TouchActiveAmoEntryIdentity(ctx, db.TouchActiveAmoEntryIdentityParams{VerifiedAt: now, CompanyID: companyID, IdentityID: row.UserExternalIdentity.ID})
		if touchErr != nil {
			return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось подтвердить пользователя amoCRM", touchErr)
		}
		return row.User, identity, false, nil
	}
	if !isNoRows(err) {
		return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось проверить пользователя amoCRM", err)
	}
	user, err := q.FindAmoEntryLegacyUserForUpdate(ctx, db.FindAmoEntryLegacyUserForUpdateParams{CompanyID: companyID, ActorID: pgtype.Text{String: input.ActorID, Valid: true}})
	created := false
	if isNoRows(err) {
		email, emailErr := normalizeEmail(input.Email)
		if emailErr != nil {
			return db.User{}, db.UserExternalIdentity{}, false, coded(ErrorValidation, "AMO_PROFILE_UNAVAILABLE", "В профиле amoCRM отсутствует корректный email. Обновите профиль и повторите вход")
		}
		first, last, _ := splitEmployeeName(input.Name)
		if len([]rune(first)) > 255 || len([]rune(last)) > 255 {
			return db.User{}, db.UserExternalIdentity{}, false, validation("Слишком длинное имя пользователя amoCRM")
		}
		user, err = q.CreateAmoUser(ctx, db.CreateAmoUserParams{ID: uuid.New(), CompanyID: companyID, Email: email, FirstName: first, LastName: pgText(&last), ExternalID: pgtype.Text{String: input.ActorID, Valid: true}})
		created = err == nil
	}
	if isUniqueViolation(err) {
		return db.User{}, db.UserExternalIdentity{}, false, conflict("Email уже используется другой учётной записью TeamOS")
	}
	if err != nil {
		return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось создать пользователя TeamOS", err)
	}
	if err = validateAmoWidgetUserState(user); err != nil {
		return db.User{}, db.UserExternalIdentity{}, false, err
	}
	identity, err := q.CreateAmoWidgetIdentity(ctx, db.CreateAmoWidgetIdentityParams{ID: uuid.New(), CompanyID: companyID, IntegrationID: integrationID, UserID: user.ID, Provider: amoWidgetProvider, ExternalAccountID: input.AccountID, ExternalUserID: input.ActorID, LastVerifiedAt: now})
	if isUniqueViolation(err) {
		return db.User{}, db.UserExternalIdentity{}, false, conflict("Пользователь amoCRM уже связан с другой учётной записью TeamOS")
	}
	if err != nil {
		return db.User{}, db.UserExternalIdentity{}, false, internal("Не удалось сохранить пользователя amoCRM", err)
	}
	return user, identity, created, nil
}

func amoEntryInvalid() error {
	return coded(ErrorUnauthenticated, "AMO_ENTRY_INVALID", "Ссылка входа недействительна или истекла. Вернитесь в виджет amoCRM")
}
func amoEntryUnavailable() error {
	return coded(ErrorUpstream, "AMO_ENTRY_UNAVAILABLE", "Вход временно недоступен. Повторите попытку из виджета amoCRM")
}

func (s *Service) ExchangeAmoCompanyEntry(ctx context.Context, code string, meta SessionMeta) (AuthResult, error) {
	if code == "" || len(code) > 512 || strings.TrimSpace(code) != code {
		return AuthResult{}, amoEntryInvalid()
	}
	hash := domainauth.HashOpaqueToken(code)
	entry, err := db.New(s.pool).GetAmoCompanyEntry(ctx, hash)
	if isNoRows(err) {
		return AuthResult{}, amoEntryInvalid()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить ссылку входа", err)
	}
	if entry.ConsumedAt.Valid || !entry.ExpiresAt.After(s.now().UTC()) {
		return AuthResult{}, amoEntryInvalid()
	}
	if s.amoEntrySubjectChecker == nil {
		return AuthResult{}, amoEntryUnavailable()
	}
	subject := AmoEntrySubject{AccountID: entry.AccountID, ActorID: entry.ActorID, IntegrationID: entry.CoreIntegrationID.String(), InstallationID: entry.CoreInstallationID.String()}
	allowed, err := s.amoEntrySubjectChecker.Check(ctx, subject)
	if err != nil {
		return AuthResult{}, amoEntryUnavailable()
	}
	if !allowed {
		return AuthResult{}, forbidden("Доступ администратора amoCRM отключён. Вернитесь в виджет")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось начать вход", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	row, err := q.GetAmoCompanyEntryForUpdate(ctx, hash)
	if isNoRows(err) {
		return AuthResult{}, amoEntryInvalid()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить доступ", err)
	}
	locked := row.AmoCompanyEntry
	now := s.now().UTC()
	if locked.ID != entry.ID || locked.AccountID != entry.AccountID || locked.ActorID != entry.ActorID || locked.CoreIntegrationID != entry.CoreIntegrationID || locked.CoreInstallationID != entry.CoreInstallationID || locked.ConsumedAt.Valid || !locked.ExpiresAt.After(now) {
		return AuthResult{}, amoEntryInvalid()
	}
	if row.IdentityStatus != "active" || row.IntegrationStatus != "active" || row.CompanyStatus != "active" || row.Provider != amoWidgetProvider || row.IdentityProvider != amoWidgetProvider || row.IdentityAccountID != locked.AccountID || row.IdentityActorID != locked.ActorID || row.IntegrationAccountID != locked.AccountID || (row.CompanyAccountID.Valid && row.CompanyAccountID.String != locked.AccountID) || (row.User.Role != "admin" && row.User.Role != "owner") {
		return AuthResult{}, forbidden("Доступ к компании TeamOS отключён")
	}
	if err = validateAmoWidgetUserState(row.User); err != nil {
		return AuthResult{}, err
	}
	consumed, err := q.ConsumeAmoCompanyEntry(ctx, db.ConsumeAmoCompanyEntryParams{Now: pgTimestamp(now), ID: locked.ID})
	if err != nil {
		return AuthResult{}, internal("Не удалось погасить ссылку входа", err)
	}
	if consumed != 1 {
		return AuthResult{}, amoEntryInvalid()
	}
	result, err := s.createSession(ctx, q, row.User, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось завершить вход", err)
	}
	return result, nil
}
