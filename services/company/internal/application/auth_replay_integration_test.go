//go:build integration

package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type failedAuthIssuer struct{}

func (failedAuthIssuer) Issue(string, string, string, []string, []string, ...[]string) (string, time.Time, error) {
	return "", time.Time{}, errors.New("signing unavailable")
}

func TestRefreshFamilyRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Now().UTC().Truncate(time.Second)}
	first := companyRegistrationTestService(t, pool, clock)
	second := companyRegistrationTestService(t, pool, clock)
	companyID, userID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, companyID, userID, []accessTestUser{{id: userID, role: "owner", status: "active"}})
	q := db.New(pool)
	user, err := q.GetUser(ctx, db.GetUserParams{CompanyID: companyID, ID: userID})
	if err != nil {
		t.Fatal(err)
	}
	newSession := func(t *testing.T) AuthResult {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		result, err := first.createSession(ctx, db.New(tx), user, SessionMeta{}, uuid.NullUUID{})
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return result
	}
	mustRefresh := func(t *testing.T, service *Service, token string) AuthResult {
		t.Helper()
		result, err := service.Refresh(ctx, token, SessionMeta{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	rejected := func(t *testing.T, token string) {
		t.Helper()
		_, err := second.Refresh(ctx, token, SessionMeta{})
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Kind != ErrorUnauthenticated {
			t.Fatalf("expected unauthenticated, got %v", err)
		}
	}
	t.Run("lost committed response recovers on another replica and child stays frozen", func(t *testing.T) {
		parent := newSession(t)
		child := mustRefresh(t, first, parent.RefreshToken)
		for i := 0; i < 3; i++ {
			replay := mustRefresh(t, second, parent.RefreshToken)
			frozen := mustRefresh(t, first, child.RefreshToken)
			if replay.RefreshToken != child.RefreshToken || frozen.RefreshToken != child.RefreshToken || !replay.RefreshExpiresAt.Equal(child.RefreshExpiresAt) {
				t.Fatal("replay rotated or extended child")
			}
			clock.Add(19 * time.Second)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE family_id=$1`, parent.FamilyID).Scan(&count); err != nil || count != 2 {
			t.Fatalf("history count=%d err=%v", count, err)
		}
		stored, err := q.GetSessionByHash(ctx, domainauth.HashRefreshToken(parent.RefreshToken))
		if err != nil {
			t.Fatal(err)
		}
		if !stored.ReplayUntil.Time.Equal(clock.Now().Add(3 * time.Second)) {
			t.Fatal("replay extended deadline")
		}
		if len(stored.ReplayNonce) != 32 || bytes.Equal(stored.RefreshHash, []byte(parent.RefreshToken)) || bytes.Equal(stored.ReplayNonce, []byte(child.RefreshToken)) {
			t.Fatal("invalid persisted hash/nonce")
		}
		var raw string
		if err := pool.QueryRow(ctx, `SELECT json_agg(s)::text FROM sessions s WHERE family_id=$1`, parent.FamilyID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, parent.RefreshToken) || strings.Contains(raw, child.RefreshToken) {
			t.Fatal("refresh secret persisted")
		}
		changed := append([]byte(nil), stored.ReplayNonce...)
		changed[0] ^= 1
		if deriveRefreshToken(parent.RefreshToken, parent.FamilyID, stored.ReplacedBy.UUID, changed) == child.RefreshToken {
			t.Fatal("nonce does not affect child")
		}
		clock.Add(3 * time.Second)
		rejected(t, parent.RefreshToken)
		grandchild := mustRefresh(t, second, child.RefreshToken)
		if grandchild.RefreshToken == child.RefreshToken {
			t.Fatal("child did not rotate at exact deadline")
		}
		rejected(t, parent.RefreshToken)
		mustRefresh(t, first, grandchild.RefreshToken)
	})
	t.Run("deadline sampled after family lock", func(t *testing.T) {
		parent := newSession(t)
		child := mustRefresh(t, first, parent.RefreshToken)
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = lock.Rollback(ctx) }()
		if _, err = lock.Exec(ctx, `SELECT id FROM session_families WHERE id=$1 FOR UPDATE`, parent.FamilyID); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := second.Refresh(ctx, parent.RefreshToken, SessionMeta{}); done <- err }()
		waitForAuthLockWaiters(t, ctx, pool, 1)
		clock.Add(refreshReplayWindow)
		if err = lock.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err = <-done; err == nil {
			t.Fatal("lock waiter accepted expired replay")
		}
		mustRefresh(t, first, child.RefreshToken)
	})
	for _, logoutFirst := range []bool{true, false} {
		name := "refresh before ancestor logout"
		if logoutFirst {
			name = "ancestor logout before refresh"
		}
		t.Run(name, func(t *testing.T) {
			parent := newSession(t)
			child := mustRefresh(t, first, parent.RefreshToken)
			other := newSession(t)
			clock.Add(refreshReplayWindow)
			lock, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Rollback(ctx) }()
			if _, err = lock.Exec(ctx, `SELECT id FROM session_families WHERE id=$1 FOR UPDATE`, parent.FamilyID); err != nil {
				t.Fatal(err)
			}
			logoutDone, refreshDone := make(chan error, 1), make(chan error, 1)
			logout := func() { logoutDone <- first.Logout(ctx, parent.RefreshToken) }
			refresh := func() { _, err := second.Refresh(ctx, child.RefreshToken, SessionMeta{}); refreshDone <- err }
			if logoutFirst {
				go logout()
			} else {
				go refresh()
			}
			waitForAuthLockWaiters(t, ctx, pool, 1)
			if logoutFirst {
				go refresh()
			} else {
				go logout()
			}
			waitForAuthLockWaiters(t, ctx, pool, 2)
			if err = lock.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-logoutDone; err != nil {
				t.Fatal(err)
			}
			refreshErr := <-refreshDone
			if logoutFirst && refreshErr == nil {
				t.Fatal("refresh survived earlier logout")
			}
			if !logoutFirst && refreshErr != nil {
				t.Fatal(refreshErr)
			}
			rejected(t, parent.RefreshToken)
			rejected(t, child.RefreshToken)
			var active int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE family_id=$1 AND revoked_at IS NULL`, parent.FamilyID).Scan(&active); err != nil || active != 0 {
				t.Fatalf("live child after logout: %d %v", active, err)
			}
			mustRefresh(t, second, other.RefreshToken)
			later := newSession(t)
			if err = second.Logout(ctx, parent.RefreshToken); err != nil {
				t.Fatal(err)
			}
			mustRefresh(t, first, later.RefreshToken)
		})
	}
	t.Run("issuer and storage failure roll back rotation", func(t *testing.T) {
		parent := newSession(t)
		goodIssuer := first.issuer
		first.issuer = failedAuthIssuer{}
		if _, err := first.Refresh(ctx, parent.RefreshToken, SessionMeta{}); err == nil {
			t.Fatal("issuer failure accepted")
		}
		first.issuer = goodIssuer
		stored, err := q.GetSessionByHash(ctx, domainauth.HashRefreshToken(parent.RefreshToken))
		if err != nil || stored.RevokedAt.Valid {
			t.Fatalf("issuer failure consumed parent: %v", err)
		}
		if _, err = pool.Exec(ctx, `CREATE FUNCTION fail_auth_child() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.rotated_from IS NOT NULL THEN RAISE EXCEPTION 'injected child insert failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_auth_child BEFORE INSERT ON sessions FOR EACH ROW EXECUTE FUNCTION fail_auth_child()`); err != nil {
			t.Fatal(err)
		}
		_, failed := first.Refresh(ctx, parent.RefreshToken, SessionMeta{})
		if _, err = pool.Exec(ctx, `DROP TRIGGER fail_auth_child ON sessions; DROP FUNCTION fail_auth_child()`); err != nil {
			t.Fatal(err)
		}
		if failed == nil {
			t.Fatal("storage failure accepted")
		}
		mustRefresh(t, second, parent.RefreshToken)
	})
	t.Run("inactive user and captured family mismatch", func(t *testing.T) {
		parent := newSession(t)
		if _, err := second.Refresh(ctx, parent.RefreshToken, SessionMeta{}, uuid.New()); err == nil {
			t.Fatal("wrong family accepted")
		}
		if err := second.Logout(ctx, parent.RefreshToken, uuid.New()); err == nil {
			t.Fatal("wrong-family logout accepted")
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET status='deactivated' WHERE id=$1`, userID); err != nil {
			t.Fatal(err)
		}
		rejected(t, parent.RefreshToken)
		if _, err := pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, userID); err != nil {
			t.Fatal(err)
		}
		mustRefresh(t, first, parent.RefreshToken)
	})
	t.Run("legacy null family and expired history pruning", func(t *testing.T) {
		token, hash, err := domainauth.NewRefreshToken()
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := q.CreateSession(ctx, db.CreateSessionParams{ID: uuid.New(), CompanyID: companyID, UserID: userID, RefreshHash: hash, ExpiresAt: clock.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		child := mustRefresh(t, first, token)
		if child.FamilyID != legacy.ID {
			t.Fatal("legacy root family not preserved")
		}
		replay := mustRefresh(t, second, token)
		if replay.RefreshToken != child.RefreshToken {
			t.Fatal("legacy replay changed child")
		}
		clock.Add(refreshReplayWindow)
		if _, err = pool.Exec(ctx, `UPDATE sessions SET expires_at=$2 WHERE id=$1`, legacy.ID, clock.Now().Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err = q.DeleteExpiredSessions(ctx, clock.Now()); err != nil {
			t.Fatal(err)
		}
		rejected(t, token)
		mustRefresh(t, first, child.RefreshToken)
	})
	t.Run("account revoke waits for refresh and prevents all future children", func(t *testing.T) {
		parent := newSession(t)
		blocker, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocker.Rollback(ctx) }()
		if _, err = blocker.Exec(ctx, `SELECT id FROM session_families WHERE id=$1 FOR UPDATE`, parent.FamilyID); err != nil {
			t.Fatal(err)
		}
		refreshed := make(chan error, 1)
		go func() { _, err := first.Refresh(ctx, parent.RefreshToken, SessionMeta{}); refreshed <- err }()
		waitForAuthLockWaiters(t, ctx, pool, 1)
		revoked := make(chan error, 1)
		go func() {
			tx, err := pool.Begin(ctx)
			if err != nil {
				revoked <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			query := db.New(tx)
			if _, err = query.GetUserForAccessUpdate(ctx, db.GetUserForAccessUpdateParams{CompanyID: companyID, ID: userID}); err == nil {
				err = revokeUserSessions(ctx, query, userID, clock.Now())
			}
			if err == nil {
				err = tx.Commit(ctx)
			}
			revoked <- err
		}()
		if err = blocker.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err = <-refreshed; err != nil {
			t.Fatal(err)
		}
		if err = <-revoked; err != nil {
			t.Fatal(err)
		}
		rejected(t, parent.RefreshToken)
		var active int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&active); err != nil || active != 0 {
			t.Fatalf("revocation left children %d %v", active, err)
		}
	})
	t.Run("access link revocation wins over blocked login", func(t *testing.T) {
		link, err := q.UpsertAccessLink(ctx, db.UpsertAccessLinkParams{CompanyID: companyID, UserID: userID, Token: "revocation-race-link"})
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		locked := db.New(tx)
		if _, err = locked.GetUserForAccessUpdate(ctx, db.GetUserForAccessUpdateParams{CompanyID: companyID, ID: userID}); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { _, err := second.LoginWithAccessLink(ctx, link.Token, SessionMeta{}); result <- err }()
		deadline := time.Now().Add(10 * time.Second)
		waiting := false
		for time.Now().Before(deadline) {
			var count int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '-- name: GetUserByAccessToken%'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count > 0 {
				waiting = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !waiting {
			t.Fatal("link login did not wait for user lock")
		}
		if err = locked.DeleteAccessLink(ctx, db.DeleteAccessLinkParams{CompanyID: companyID, UserID: userID}); err != nil {
			t.Fatal(err)
		}
		if err = revokeUserSessions(ctx, locked, userID, clock.Now()); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err = <-result; err == nil {
			t.Fatal("revoked access link created new family after revocation committed")
		}
	})

}
