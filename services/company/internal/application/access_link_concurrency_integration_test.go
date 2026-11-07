//go:build integration

package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type accessLinkBarrierIssuer struct {
	wrapped accessTokenIssuer
	reached chan struct{}
	release chan struct{}
}

func (b accessLinkBarrierIssuer) Issue(subject, companyID, role string, positions, departments []string, sections ...[]string) (string, time.Time, error) {
	select {
	case b.reached <- struct{}{}:
	default:
	}
	<-b.release
	return b.wrapped.Issue(subject, companyID, role, positions, departments, sections...)
}

func TestConcurrentAccessLinkEntryContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	first, second := companyRegistrationTestService(t, pool, clock), companyRegistrationTestService(t, pool, clock)
	companyID, userID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, companyID, userID, []accessTestUser{{id: userID, role: "owner", status: "active"}})
	link, err := db.New(pool).UpsertAccessLink(ctx, db.UpsertAccessLinkParams{CompanyID: companyID, UserID: userID, Token: "concurrent-first-entry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE access_links SET entry_context='company_created' WHERE token=$1`, link.Token); err != nil {
		t.Fatal(err)
	}
	barrier := accessLinkBarrierIssuer{wrapped: first.issuer, reached: make(chan struct{}, 2), release: make(chan struct{})}
	first.issuer, second.issuer = barrier, barrier
	var release sync.Once
	defer release.Do(func() { close(barrier.release) })
	type outcome struct {
		result AccessLinkAuthResult
		err    error
	}
	done := make(chan outcome, 2)
	for _, service := range []*Service{first, second} {
		go func() {
			result, err := service.LoginWithAccessLink(ctx, link.Token, SessionMeta{})
			done <- outcome{result, err}
		}()
	}
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	deadline := time.Now().Add(10 * time.Second)
	blockedOrReached := false
	for time.Now().Before(deadline) {
		select {
		case <-barrier.reached:
			blockedOrReached = true
		default:
		}
		var waiting int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '-- name: GetUserByAccessToken :one%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 || blockedOrReached {
			blockedOrReached = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blockedOrReached {
		t.Fatal("second access-link exchange did not reach the controlled overlap")
	}
	release.Do(func() { close(barrier.release) })
	entryContexts := 0
	for range 2 {
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatalf("concurrent valid link exchange failed: %v", result.err)
			}
			if result.result.EntryContext == "company_created" {
				entryContexts++
			}
			if _, err = first.Refresh(ctx, result.result.Session.RefreshToken, SessionMeta{}); err != nil {
				t.Fatalf("new family unusable: %v", err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if entryContexts != 1 {
		t.Fatalf("entry context returned %d times, want exactly once", entryContexts)
	}
}
