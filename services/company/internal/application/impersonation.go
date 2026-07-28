package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// ImpersonateUser creates a regular user session without changing the target
// user's password or access-link mode. Only the company owner may do this.
func (s *Service) ImpersonateUser(
	ctx context.Context,
	actor Actor,
	userID uuid.UUID,
	meta SessionMeta,
) (AuthResult, error) {
	if actor.Role != "owner" {
		return AuthResult{}, forbidden("Войти под пользователем может только владелец компании")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось начать вход под пользователем", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	user, err := queries.GetUserForAccessUpdate(ctx, db.GetUserForAccessUpdateParams{
		CompanyID: actor.CompanyID,
		ID:        userID,
	})
	if isNoRows(err) {
		return AuthResult{}, notFound("Сотрудник")
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось получить сотрудника", err)
	}
	if err = validateImpersonationTarget(actor, user); err != nil {
		return AuthResult{}, err
	}

	result, err := s.createSession(ctx, queries, user, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось создать сессию пользователя", err)
	}

	if s.logger != nil {
		s.logger.InfoContext(
			ctx,
			"владелец вошёл под пользователем",
			"company_id", actor.CompanyID.String(),
			"actor_user_id", actor.UserID.String(),
			"target_user_id", userID.String(),
			"request_id", actor.RequestID,
		)
	}
	return result, nil
}

func validateImpersonationTarget(actor Actor, user db.User) error {
	if actor.Role != "owner" {
		return forbidden("Войти под пользователем может только владелец компании")
	}
	if user.CompanyID != actor.CompanyID {
		return notFound("Сотрудник")
	}
	if user.Role == "owner" || user.ID == actor.UserID {
		return validation("Нельзя войти под владельцем компании")
	}
	if user.Status != "active" {
		return validation("Войти можно только под активным пользователем")
	}
	return nil
}
