//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestSessionConcurrentRefreshAndLogoutScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pool := companyAccessTestPool(t, ctx)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(pool, sharedauth.NewTokenIssuer(privateKey, "test-company", "test-api", time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	replica, err := NewService(pool, sharedauth.NewTokenIssuer(privateKey, "test-company", "test-api", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	services := []*Service{service, replica}

	companyID, ownerID, userID := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, companyID, ownerID, []accessTestUser{
		{id: ownerID, role: "owner", status: "active"},
		{id: userID, role: "admin", status: "active"},
	})
	meta := SessionMeta{UserAgent: "audit", IPAddress: "127.0.0.1"}

	newSession := func(t *testing.T) string {
		t.Helper()
		queries := db.New(pool)
		user, getErr := queries.GetUser(ctx, db.GetUserParams{CompanyID: companyID, ID: userID})
		if getErr != nil {
			t.Fatal(getErr)
		}
		result, createErr := service.createSession(ctx, queries, user, meta, uuid.NullUUID{})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return result.RefreshToken
	}
	activeSessions := func(t *testing.T) int {
		t.Helper()
		var count int
		if scanErr := pool.QueryRow(ctx,
			`SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&count); scanErr != nil {
			t.Fatal(scanErr)
		}
		return count
	}
	revokeAll := func(t *testing.T) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); execErr != nil {
			t.Fatal(execErr)
		}
	}

	t.Run("concurrent refresh with the same cookie", func(t *testing.T) {
		revokeAll(t)
		otherDevice := newSession(t)
		shared := newSession(t)
		t.Logf("active sessions before: %d", activeSessions(t))

		// Hold the session row so both Refresh calls queue on FOR UPDATE.
		blocker, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if _, lockErr := blocker.Exec(ctx, `SELECT 1 FROM session_families WHERE user_id=$1 FOR UPDATE`, userID); lockErr != nil {
			t.Fatal(lockErr)
		}
		type outcome struct {
			result AuthResult
			err    error
		}
		results := make([]outcome, 2)
		var wg sync.WaitGroup
		for index := range results {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				result, refreshErr := services[index%len(services)].Refresh(ctx, shared, meta)
				results[index] = outcome{result: result, err: refreshErr}
			}(index)
		}
		waitForAuthLockWaiters(t, ctx, pool, 2)
		if commitErr := blocker.Commit(ctx); commitErr != nil {
			t.Fatal(commitErr)
		}
		wg.Wait()

		var winner string
		for index, item := range results {
			t.Logf("refresh #%d err=%v gotNewToken=%t", index, item.err, item.result.RefreshToken != "")
			if item.err == nil {
				winner = item.result.RefreshToken
			}
		}
		if count := activeSessions(t); count != 2 {
			t.Errorf("concurrent refresh revoked sessions: got %d active, want 2", count)
		}
		if results[0].err != nil || results[1].err != nil || results[0].result.RefreshToken != results[1].result.RefreshToken {
			t.Errorf("concurrent refresh must return identical child on both calls: errors %v, %v", results[0].err, results[1].err)
		}
		if winner != "" {
			_, winnerErr := service.Refresh(ctx, winner, meta)
			t.Logf("refresh with the WINNER's new token: err=%v", winnerErr)
		}
		_, otherErr := service.Refresh(ctx, otherDevice, meta)
		if otherErr != nil {
			t.Errorf("other device lost session: %v", otherErr)
		}
	})

	t.Run("logout scope", func(t *testing.T) {
		revokeAll(t)
		deviceA := newSession(t)
		deviceB := newSession(t)
		if logoutErr := service.Logout(ctx, deviceA); logoutErr != nil {
			t.Fatal(logoutErr)
		}
		_, deviceBErr := service.Refresh(ctx, deviceB, meta)
		if deviceBErr != nil {
			t.Errorf("logout A revoked B: %v", deviceBErr)
		}

		deviceC := newSession(t)
		if logoutErr := service.Logout(ctx, deviceA); logoutErr != nil {
			t.Fatalf("replayed logout: %v", logoutErr)
		}
		_, deviceCErr := service.Refresh(ctx, deviceC, meta)
		if deviceCErr != nil {
			t.Errorf("replayed logout A revoked later login C: %v", deviceCErr)
		}
	})
}

func waitForAuthLockWaiters(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query ILIKE '%session_families%FOR UPDATE%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected %d lock waiters", want)
}
