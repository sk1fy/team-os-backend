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

func canManageEmployeeAccess(actor Actor) bool {
	return actor.Role == "owner" || actor.Role == "admin"
}

func requireEmployeeAccessManagement(actor Actor) error {
	if !canManageEmployeeAccess(actor) {
		return forbidden("Недостаточно прав для управления доступом сотрудников")
	}
	return nil
}

func accessTarget(ctx context.Context, queries *db.Queries, actor Actor, userID uuid.UUID) (db.User, error) {
	user, err := queries.GetUser(ctx, db.GetUserParams{CompanyID: actor.CompanyID, ID: userID})
	return validateAccessTarget(user, err)
}

func accessTargetForUpdate(ctx context.Context, queries *db.Queries, actor Actor, userID uuid.UUID) (db.User, error) {
	user, err := queries.GetUserForAccessUpdate(ctx, db.GetUserForAccessUpdateParams{CompanyID: actor.CompanyID, ID: userID})
	user, err = validateAccessTarget(user, err)
	if err != nil {
		return db.User{}, err
	}
	return user, nil
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
	if err := requireEmployeeAccessManagement(actor); err != nil {
		return EmployeeAccess{}, err
	}
	queries := db.New(s.pool)
	if _, err := accessTarget(ctx, queries, actor, userID); err != nil {
		return EmployeeAccess{}, err
	}
	details, err := queries.GetUserAccessDetails(ctx, db.GetUserAccessDetailsParams{
		CompanyID: actor.CompanyID,
		UserID:    userID,
	})
	if err != nil {
		return EmployeeAccess{}, internal("Не удалось получить способ доступа сотрудника", err)
	}
	result := EmployeeAccess{
		Mode: "none", Login: details.Login,
		PasswordEnabled: details.PasswordEnabled,
		LinkEnabled:     details.LinkToken.Valid,
	}
	if details.PasswordEnabled {
		result.Mode = "password"
	}
	if details.LinkToken.Valid {
		result.Mode = "link"
		result.LinkToken = &details.LinkToken.String
		createdAt := details.LinkCreatedAt.Time.UTC()
		result.LinkCreatedAt = &createdAt
	}
	return result, nil
}

func (s *Service) SetPasswordAccess(
	ctx context.Context,
	actor Actor,
	userID uuid.UUID,
	input SetPasswordAccessInput,
) (EmployeePasswordAccess, error) {
	if err := requireEmployeeAccessManagement(actor); err != nil {
		return EmployeePasswordAccess{}, err
	}
	password := ""
	if input.Password == nil {
		generated, err := domainauth.GeneratePassword()
		if err != nil {
			return EmployeePasswordAccess{}, internal("Не удалось сгенерировать пароль", err)
		}
		password = generated
	} else {
		password = *input.Password
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return EmployeePasswordAccess{}, internal("Не удалось обработать пароль", err)
	}
	passwordHash, err := domainauth.HashPassword(password)
	releasePasswordSlot()
	if err != nil {
		return EmployeePasswordAccess{}, validation(err.Error())
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EmployeePasswordAccess{}, internal("Не удалось выдать доступ по паролю", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	user, err := accessTargetForUpdate(ctx, queries, actor, userID)
	if err != nil {
		return EmployeePasswordAccess{}, err
	}
	previousAccess, err := queries.GetUserAccessDetails(ctx, db.GetUserAccessDetailsParams{
		CompanyID: actor.CompanyID,
		UserID:    userID,
	})
	if err != nil {
		return EmployeePasswordAccess{}, internal("Не удалось проверить текущий способ доступа", err)
	}
	if err = queries.SetCredential(ctx, db.SetCredentialParams{
		CompanyID: actor.CompanyID, UserID: userID, PasswordHash: passwordHash,
	}); err != nil {
		return EmployeePasswordAccess{}, internal("Не удалось сохранить пароль", err)
	}
	if err = revokeUserSessions(ctx, queries, userID, s.now().UTC()); err != nil {
		return EmployeePasswordAccess{}, err
	}
	if err = auditAccessChange(ctx, queries, actor, userID, accessAction(previousAccess.PasswordEnabled), "password", s.now().UTC()); err != nil {
		return EmployeePasswordAccess{}, err
	}
	login, err := queries.GetUserLogin(ctx, db.GetUserLoginParams{
		CompanyID: actor.CompanyID,
		UserID:    user.ID,
	})
	if err != nil {
		return EmployeePasswordAccess{}, internal("Не удалось получить логин сотрудника", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return EmployeePasswordAccess{}, internal("Не удалось выдать доступ по паролю", err)
	}
	return EmployeePasswordAccess{Login: login, Password: password}, nil
}

func (s *Service) SetLinkAccess(ctx context.Context, actor Actor, userID uuid.UUID) (EmployeeLinkAccess, error) {
	if err := requireEmployeeAccessManagement(actor); err != nil {
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
	previousAccess, err := queries.GetUserAccessDetails(ctx, db.GetUserAccessDetailsParams{
		CompanyID: actor.CompanyID,
		UserID:    userID,
	})
	if err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось проверить текущий способ доступа", err)
	}
	link, err := queries.UpsertAccessLink(ctx, db.UpsertAccessLinkParams{
		CompanyID: actor.CompanyID, UserID: userID, Token: token,
	})
	if err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось сохранить ссылку доступа", err)
	}
	if err = revokeUserSessions(ctx, queries, userID, s.now().UTC()); err != nil {
		return EmployeeLinkAccess{}, err
	}
	if err = auditAccessChange(ctx, queries, actor, userID, accessAction(previousAccess.LinkToken.Valid), "link", s.now().UTC()); err != nil {
		return EmployeeLinkAccess{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EmployeeLinkAccess{}, internal("Не удалось выдать доступ по ссылке", err)
	}
	return EmployeeLinkAccess{Token: link.Token, CreatedAt: link.CreatedAt.UTC()}, nil
}

func (s *Service) RevokePasswordAccess(ctx context.Context, actor Actor, userID uuid.UUID) error {
	return s.revokeAccessMethod(ctx, actor, userID, "password")
}

func (s *Service) RevokeLinkAccess(ctx context.Context, actor Actor, userID uuid.UUID) error {
	return s.revokeAccessMethod(ctx, actor, userID, "link")
}

func (s *Service) revokeAccessMethod(ctx context.Context, actor Actor, userID uuid.UUID, mode string) error {
	if err := requireEmployeeAccessManagement(actor); err != nil {
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
	switch mode {
	case "password":
		err = queries.DeleteCredential(ctx, db.DeleteCredentialParams{CompanyID: actor.CompanyID, UserID: userID})
	case "link":
		err = queries.DeleteAccessLink(ctx, db.DeleteAccessLinkParams{CompanyID: actor.CompanyID, UserID: userID})
	default:
		return validation("Некорректный способ доступа")
	}
	if err != nil {
		return internal("Не удалось отозвать доступ", err)
	}
	if err = revokeUserSessions(ctx, queries, userID, s.now().UTC()); err != nil {
		return err
	}
	if err = auditAccessChange(ctx, queries, actor, userID, "revoked", mode, s.now().UTC()); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось отозвать доступ", err)
	}
	return nil
}

func (s *Service) RevokeAccess(ctx context.Context, actor Actor, userID uuid.UUID) error {
	if err := requireEmployeeAccessManagement(actor); err != nil {
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
	previousAccess, err := queries.GetUserAccessDetails(ctx, db.GetUserAccessDetailsParams{
		CompanyID: actor.CompanyID,
		UserID:    userID,
	})
	if err != nil {
		return internal("Не удалось проверить текущий способ доступа", err)
	}
	if err = queries.DeleteCredential(ctx, db.DeleteCredentialParams{CompanyID: actor.CompanyID, UserID: userID}); err != nil {
		return internal("Не удалось удалить пароль", err)
	}
	if err = queries.DeleteAccessLink(ctx, db.DeleteAccessLinkParams{CompanyID: actor.CompanyID, UserID: userID}); err != nil {
		return internal("Не удалось удалить ссылку доступа", err)
	}
	revokedAt := s.now().UTC()
	if err = revokeUserSessions(ctx, queries, userID, revokedAt); err != nil {
		return err
	}
	audited := false
	if previousAccess.PasswordEnabled {
		if err = auditAccessChange(ctx, queries, actor, userID, "revoked", "password", revokedAt); err != nil {
			return err
		}
		audited = true
	}
	if previousAccess.LinkToken.Valid {
		if err = auditAccessChange(ctx, queries, actor, userID, "revoked", "link", revokedAt); err != nil {
			return err
		}
		audited = true
	}
	if !audited {
		if err = auditAccessChange(ctx, queries, actor, userID, "revoked", "none", revokedAt); err != nil {
			return err
		}
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

func accessAction(previouslyEnabled bool) string {
	if previouslyEnabled {
		return "reissued"
	}
	return "issued"
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
		ID: uuid.New(), CompanyID: actor.CompanyID,
		TargetUserID: uuid.NullUUID{UUID: targetUserID, Valid: true},
		ActorUserID:  uuid.NullUUID{UUID: actor.UserID, Valid: true},
		Action:       action, Mode: mode, CreatedAt: createdAt,
	}); err != nil {
		return internal("Не удалось записать аудит доступа", err)
	}
	return nil
}
