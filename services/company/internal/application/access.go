package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func requireOwner(actor Actor) error {
	if actor.Role != "owner" {
		return forbidden("Управлять доступом сотрудников может только владелец")
	}
	return nil
}

func accessTarget(ctx context.Context, queries *db.Queries, actor Actor, userID uuid.UUID) (db.User, error) {
	user, err := queries.GetUser(ctx, db.GetUserParams{CompanyID: actor.CompanyID, ID: userID})
	return validateAccessTarget(user, err)
}

func accessTargetForUpdate(ctx context.Context, queries *db.Queries, actor Actor, userID uuid.UUID) (db.User, error) {
	user, err := queries.GetUserForAccessUpdate(ctx, db.GetUserForAccessUpdateParams{CompanyID: actor.CompanyID, ID: userID})
	return validateAccessTarget(user, err)
}

func validateAccessTarget(user db.User, err error) (db.User, error) {
	if isNoRows(err) {
		return db.User{}, notFound("Сотрудник")
	}
	if err != nil {
		return db.User{}, internal("Не удалось получить сотрудника", err)
	}
	if user.Role == "owner" {
		return db.User{}, validation("Нельзя изменять доступ владельца")
	}
	if user.Status != "active" {
		return db.User{}, validation("Управлять доступом можно только для активного сотрудника")
	}
	return user, nil
}

func (s *Service) GetUserAccess(ctx context.Context, actor Actor, userID uuid.UUID) (EmployeeAccess, error) {
	if err := requireOwner(actor); err != nil {
		return EmployeeAccess{}, err
	}
	queries := db.New(s.pool)
	if _, err := accessTarget(ctx, queries, actor, userID); err != nil {
		return EmployeeAccess{}, err
	}
	mode, err := queries.GetUserAccessMode(ctx, db.GetUserAccessModeParams{
		CompanyID: actor.CompanyID,
		UserID:    userID,
	})
	if err != nil {
		return EmployeeAccess{}, internal("Не удалось получить способ доступа сотрудника", err)
	}
	result := EmployeeAccess{Mode: mode}
	if mode != "link" {
		return result, nil
	}
	link, err := queries.GetAccessLink(ctx, db.GetAccessLinkParams{CompanyID: actor.CompanyID, UserID: userID})
	if isNoRows(err) {
		return EmployeeAccess{Mode: "none"}, nil
	}
	if err != nil {
		return EmployeeAccess{}, internal("Не удалось получить ссылку доступа", err)
	}
	result.LinkToken = &link.Token
	createdAt := link.CreatedAt.UTC()
	result.LinkCreatedAt = &createdAt
	return result, nil
}

func (s *Service) SetPasswordAccess(
	ctx context.Context,
	actor Actor,
	userID uuid.UUID,
	input SetPasswordAccessInput,
) (string, error) {
	if err := requireOwner(actor); err != nil {
		return "", err
	}
	password := ""
	if input.Password == nil {
		generated, err := domainauth.GeneratePassword()
		if err != nil {
			return "", internal("Не удалось сгенерировать пароль", err)
		}
		password = generated
	} else {
		password = *input.Password
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return "", internal("Не удалось обработать пароль", err)
	}
	passwordHash, err := domainauth.HashPassword(password)
	releasePasswordSlot()
	if err != nil {
		return "", validation(err.Error())
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", internal("Не удалось выдать доступ по паролю", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = accessTargetForUpdate(ctx, queries, actor, userID); err != nil {
		return "", err
	}
	previousMode, err := queries.GetUserAccessMode(ctx, db.GetUserAccessModeParams{CompanyID: actor.CompanyID, UserID: userID})
	if err != nil {
		return "", internal("Не удалось проверить текущий способ доступа", err)
	}
	if err = queries.SetCredential(ctx, db.SetCredentialParams{
		CompanyID: actor.CompanyID, UserID: userID, PasswordHash: passwordHash,
	}); err != nil {
		return "", internal("Не удалось сохранить пароль", err)
	}
	if err = queries.DeleteAccessLink(ctx, db.DeleteAccessLinkParams{CompanyID: actor.CompanyID, UserID: userID}); err != nil {
		return "", internal("Не удалось удалить ссылку доступа", err)
	}
	if err = revokeUserSessions(ctx, queries, userID, s.now().UTC()); err != nil {
		return "", err
	}
	if err = auditAccessChange(ctx, queries, actor, userID, accessAction(previousMode), "password", s.now().UTC()); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", internal("Не удалось выдать доступ по паролю", err)
	}
	return password, nil
}

func (s *Service) SetLinkAccess(ctx context.Context, actor Actor, userID uuid.UUID) (EmployeeLinkAccess, error) {
	if err := requireOwner(actor); err != nil {
		return EmployeeLinkAccess{}, err
	}
	token, err := domainauth.NewAccessLinkToken()
	if err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось сгенерировать ссылку доступа", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось выдать доступ по ссылке", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = accessTargetForUpdate(ctx, queries, actor, userID); err != nil {
		return EmployeeLinkAccess{}, err
	}
	previousMode, err := queries.GetUserAccessMode(ctx, db.GetUserAccessModeParams{CompanyID: actor.CompanyID, UserID: userID})
	if err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось проверить текущий способ доступа", err)
	}
	link, err := queries.UpsertAccessLink(ctx, db.UpsertAccessLinkParams{
		CompanyID: actor.CompanyID, UserID: userID, Token: token,
	})
	if err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось сохранить ссылку доступа", err)
	}
	if err = queries.DeleteCredential(ctx, db.DeleteCredentialParams{CompanyID: actor.CompanyID, UserID: userID}); err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось удалить пароль", err)
	}
	if err = revokeUserSessions(ctx, queries, userID, s.now().UTC()); err != nil {
		return EmployeeLinkAccess{}, err
	}
	if err = auditAccessChange(ctx, queries, actor, userID, accessAction(previousMode), "link", s.now().UTC()); err != nil {
		return EmployeeLinkAccess{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось выдать доступ по ссылке", err)
	}
	return EmployeeLinkAccess{Token: link.Token, CreatedAt: link.CreatedAt.UTC()}, nil
}

func (s *Service) RevokeAccess(ctx context.Context, actor Actor, userID uuid.UUID) error {
	if err := requireOwner(actor); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return internal("Не удалось отозвать доступ", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if _, err = accessTargetForUpdate(ctx, queries, actor, userID); err != nil {
		return err
	}
	previousMode, err := queries.GetUserAccessMode(ctx, db.GetUserAccessModeParams{CompanyID: actor.CompanyID, UserID: userID})
	if err != nil {
		return internal("Не удалось проверить текущий способ доступа", err)
	}
	if err = queries.DeleteCredential(ctx, db.DeleteCredentialParams{CompanyID: actor.CompanyID, UserID: userID}); err != nil {
		return internal("Не удалось удалить пароль", err)
	}
	if err = queries.DeleteAccessLink(ctx, db.DeleteAccessLinkParams{CompanyID: actor.CompanyID, UserID: userID}); err != nil {
		return internal("Не удалось удалить ссылку доступа", err)
	}
	if err = revokeUserSessions(ctx, queries, userID, s.now().UTC()); err != nil {
		return err
	}
	if err = auditAccessChange(ctx, queries, actor, userID, "revoked", previousMode, s.now().UTC()); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось отозвать доступ", err)
	}
	return nil
}

func (s *Service) LoginWithAccessLink(ctx context.Context, token string, meta SessionMeta) (AuthResult, error) {
	if strings.TrimSpace(token) == "" {
		return AuthResult{}, invalidAccessLink()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось выполнить вход", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	user, err := queries.GetUserByAccessToken(ctx, token)
	if isNoRows(err) {
		return AuthResult{}, invalidAccessLink()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить ссылку доступа", err)
	}
	result, err := s.createSession(ctx, queries, user, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось создать сессию", err)
	}
	return result, nil
}

func revokeUserSessions(ctx context.Context, queries *db.Queries, userID uuid.UUID, revokedAt time.Time) error {
	if err := queries.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{
		UserID: userID, RevokedAt: pgtype.Timestamptz{Time: revokedAt, Valid: true},
	}); err != nil {
		return internal("Не удалось отозвать сессии пользователя", err)
	}
	return nil
}

func accessAction(previousMode string) string {
	if previousMode == "none" {
		return "issued"
	}
	return "reissued"
}

func auditAccessChange(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	targetUserID uuid.UUID,
	action string,
	mode string,
	createdAt time.Time,
) error {
	if err := queries.CreateEmployeeAccessAudit(ctx, db.CreateEmployeeAccessAuditParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, TargetUserID: targetUserID,
		ActorUserID: actor.UserID, Action: action, Mode: mode, CreatedAt: createdAt,
	}); err != nil {
		return internal("Не удалось записать аудит доступа", err)
	}
	return nil
}
