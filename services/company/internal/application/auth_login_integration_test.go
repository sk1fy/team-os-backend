//go:build integration

package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestLoginPasswordQueueDoesNotHoldDatabaseAndRechecksAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	observer := companyAccessTestPool(t, ctx)
	config := observer.Config().Copy()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	service := companyRegistrationTestService(t, pool, clock)
	companyID, userID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, observer, companyID, userID, []accessTestUser{{id: userID, role: "owner", status: "active"}})
	q := db.New(observer)
	hash, err := domainauth.HashPassword("original-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = q.SetCredential(ctx, db.SetCredentialParams{CompanyID: companyID, UserID: userID, PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	login, err := q.GetUserLogin(ctx, db.GetUserLoginParams{CompanyID: companyID, UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	service.passwordSlots = make(chan struct{}, 1)
	service.passwordSlots <- struct{}{}
	done := make(chan error, 1)
	go func() {
		_, err := service.Login(ctx, LoginInput{Login: login, Password: "original-password"}, SessionMeta{})
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		var count int
		if err = observer.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE query LIKE '-- name: GetUserForLogin :one%'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			observed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observed {
		t.Fatal("login did not read credential")
	}
	availableCtx, availableCancel := context.WithTimeout(ctx, time.Second)
	connection, acquireErr := pool.Acquire(availableCtx)
	availableCancel()
	if acquireErr != nil {
		t.Fatalf("Argon2 queue held the only DB connection: %v", acquireErr)
	}
	connection.Release()
	tx, err := observer.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, userID); err != nil {
		t.Fatalf("Argon2 queue held user row lock: %v", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE credentials SET password_hash=$2 WHERE user_id=$1`, userID, "changed-while-verifying"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-service.passwordSlots
	var appErr *Error
	if err = <-done; !errors.As(err, &appErr) || appErr.Kind != ErrorUnauthenticated {
		t.Fatalf("stale credential created session: %v", err)
	}
	var sessions int
	if err = observer.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, userID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("stale login persisted %d sessions: %v", sessions, err)
	}
}
