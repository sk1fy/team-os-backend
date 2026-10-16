package application

import (
	"context"
	"crypto/sha256"

	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// TaskControlRuntimeConnection reports the authenticated connection in either mode.
// It does not expose rules or create a managed runtime lease.
func (s *Service) TaskControlRuntimeConnection(ctx context.Context, token string) (TaskControlConnection, error) {
	out, _, e := s.AuthorizeTaskControlRuntime(ctx, token)
	if e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return TaskControlConnection{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if e = q.LockTaskControlAccount(ctx, out.Scope.AccountID); e != nil {
		return TaskControlConnection{}, e
	}
	hash := sha256.Sum256([]byte(token))
	session, e := q.GetTaskControlRuntimeSession(ctx, db.GetTaskControlRuntimeSessionParams{TokenHash: hash[:], ExpiresAt: s.now()})
	if e != nil {
		return TaskControlConnection{}, forbidden("Сессия подключения отозвана")
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: session.CompanyID, ID: session.BindingID})
	if e != nil {
		return TaskControlConnection{}, e
	}
	if c.State != "active" || taskControlScope(c) != out.Scope || c.BindingRevision != session.BindingRevision || c.ModeEpoch != session.ModeEpoch || c.Generation != session.Generation {
		return TaskControlConnection{}, forbidden("Сессия подключения отозвана")
	}
	if e = s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation); e != nil {
		return TaskControlConnection{}, e
	}
	out.Mode = c.Mode
	out.State = c.State
	out.UpdatedAt = c.UpdatedAt
	return out, tx.Commit(ctx)
}
