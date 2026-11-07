package application

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const refreshReplayWindow = 60 * time.Second
const maxRefreshTokenLength = 512

func deriveRefreshToken(parent string, familyID, childID uuid.UUID, nonce []byte) string {
	mac := hmac.New(sha256.New, []byte(parent))
	mac.Write([]byte("teamos.refresh.child.v1\x00"))
	mac.Write(familyID[:])
	mac.Write(childID[:])
	mac.Write(nonce)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// User locks precede family locks. Account access changes hold the exclusive user
// lock, while all generations of a login share the same family lock.
func lockRefreshFamily(ctx context.Context, q *db.Queries, session db.Session, expected []uuid.UUID) (db.Session, db.SessionFamily, error) {
	familyID := session.FamilyID.UUID
	if !session.FamilyID.Valid {
		var err error
		familyID, err = q.GetLegacySessionRoot(ctx, session.ID)
		if err != nil {
			return db.Session{}, db.SessionFamily{}, internal("Не удалось прочитать историю сессии", err)
		}
		if err = q.CreateSessionFamily(ctx, db.CreateSessionFamilyParams{ID: familyID, CompanyID: session.CompanyID, UserID: session.UserID}); err != nil {
			return db.Session{}, db.SessionFamily{}, internal("Не удалось восстановить сессию", err)
		}
	}
	if len(expected) > 0 && expected[0] != familyID {
		return db.Session{}, db.SessionFamily{}, invalidSession()
	}
	family, err := q.GetSessionFamilyForUpdate(ctx, familyID)
	if err != nil {
		return db.Session{}, db.SessionFamily{}, internal("Не удалось заблокировать сессию", err)
	}
	if family.CompanyID != session.CompanyID || family.UserID != session.UserID {
		return db.Session{}, db.SessionFamily{}, invalidSession()
	}
	if !session.FamilyID.Valid {
		if err = q.AssignLegacySessionFamily(ctx, uuid.NullUUID{UUID: familyID, Valid: true}); err != nil {
			return db.Session{}, db.SessionFamily{}, internal("Не удалось восстановить сессию", err)
		}
	}
	session, err = q.GetSessionByHash(ctx, session.RefreshHash)
	if err != nil {
		return db.Session{}, db.SessionFamily{}, internal("Не удалось прочитать сессию", err)
	}
	return session, family, nil
}

func (s *Service) Refresh(ctx context.Context, token string, meta SessionMeta, expectedFamily ...uuid.UUID) (AuthResult, error) {
	if strings.TrimSpace(token) == "" || len(token) > maxRefreshTokenLength {
		return AuthResult{}, invalidSession()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось обновить сессию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	session, err := q.GetSessionByHash(ctx, domainauth.HashRefreshToken(token))
	if isNoRows(err) {
		return AuthResult{}, invalidSession()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось прочитать сессию", err)
	}
	user, err := q.GetUserForSession(ctx, db.GetUserForSessionParams{CompanyID: session.CompanyID, ID: session.UserID})
	if isNoRows(err) {
		return AuthResult{}, invalidSession()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить доступ", err)
	}
	if user.Status != "active" {
		return AuthResult{}, invalidSession()
	}
	session, family, err := lockRefreshFamily(ctx, q, session, expectedFamily)
	if err != nil {
		return AuthResult{}, err
	}
	now := s.now().UTC()
	if family.RevokedAt.Valid || !session.ExpiresAt.After(now) {
		return AuthResult{}, invalidSession()
	}
	resultToken, expiresAt := token, session.ExpiresAt
	if session.RevokedAt.Valid {
		if !session.ReplacedBy.Valid || !session.ReplayUntil.Valid || !now.Before(session.ReplayUntil.Time) || len(session.ReplayNonce) != 32 {
			return AuthResult{}, invalidSession()
		}
		child, lookupErr := q.GetCurrentFamilySession(ctx, session.FamilyID)
		if isNoRows(lookupErr) {
			return AuthResult{}, invalidSession()
		}
		if lookupErr != nil {
			return AuthResult{}, internal("Не удалось прочитать сессию", lookupErr)
		}
		resultToken = deriveRefreshToken(token, family.ID, child.ID, session.ReplayNonce)
		if child.ID != session.ReplacedBy.UUID || !child.ExpiresAt.After(now) || !bytes.Equal(domainauth.HashRefreshToken(resultToken), child.RefreshHash) {
			return AuthResult{}, invalidSession()
		}
		expiresAt = child.ExpiresAt
	} else if !session.RotationNotBefore.Valid || !now.Before(session.RotationNotBefore.Time) {
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err != nil {
			return AuthResult{}, internal("Не удалось выпустить refresh token", err)
		}
		childID := uuid.New()
		resultToken = deriveRefreshToken(token, family.ID, childID, nonce)
		deadline := pgTimestamp(now.Add(refreshReplayWindow))
		if _, err = q.RotateSession(ctx, db.RotateSessionParams{ID: session.ID, RevokedAt: pgTimestamp(now), ReplayUntil: deadline, ReplayNonce: nonce}); err != nil {
			return AuthResult{}, internal("Не удалось обновить сессию", err)
		}
		expiresAt = now.Add(s.refreshTTL)
		if _, err = q.CreateSession(ctx, db.CreateSessionParams{
			ID: childID, CompanyID: session.CompanyID, UserID: session.UserID, FamilyID: session.FamilyID,
			RefreshHash: domainauth.HashRefreshToken(resultToken), ExpiresAt: expiresAt,
			RotatedFrom: uuid.NullUUID{UUID: session.ID, Valid: true}, RotationNotBefore: deadline,
			UserAgent: pgtype.Text{String: meta.UserAgent, Valid: meta.UserAgent != ""}, IpAddress: parseIPAddress(meta.IPAddress),
		}); err != nil {
			return AuthResult{}, internal("Не удалось сохранить сессию", err)
		}
		if err = q.SetSessionReplacement(ctx, db.SetSessionReplacementParams{ID: session.ID, ReplacedBy: uuid.NullUUID{UUID: childID, Valid: true}}); err != nil {
			return AuthResult{}, internal("Не удалось обновить сессию", err)
		}
	}
	result, err := s.authResultForToken(ctx, q, user, family.ID, resultToken, expiresAt)
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось обновить сессию", err)
	}
	return result, nil
}

func (s *Service) Logout(ctx context.Context, token string, expectedFamily ...uuid.UUID) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if len(token) > maxRefreshTokenLength {
		return invalidSession()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	session, err := q.GetSessionByHash(ctx, domainauth.HashRefreshToken(token))
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return internal("Не удалось прочитать сессию", err)
	}
	_, family, err := lockRefreshFamily(ctx, q, session, expectedFamily)
	if err != nil {
		return err
	}
	if err = q.RevokeSessionFamily(ctx, db.RevokeSessionFamilyParams{ID: family.ID, RevokedAt: pgTimestamp(s.now().UTC())}); err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	return nil
}
