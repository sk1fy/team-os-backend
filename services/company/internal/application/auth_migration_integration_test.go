//go:build integration

package application

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
)

func TestRefreshFamilyMigrationPreservesHistoricalChains(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	companyID, userID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, companyID, userID, []accessTestUser{{id: userID, role: "owner", status: "active"}})
	migration := func(suffix string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000043_session_families."+suffix+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if _, err := pool.Exec(ctx, migration("down")); err != nil {
		t.Fatal(err)
	}
	parentID, childID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,company_id,user_id,refresh_hash,expires_at,revoked_at) VALUES($1,$2,$3,$4,now()+interval '1 day',now())`, parentID, companyID, userID, domainauth.HashRefreshToken("legacy-parent")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,company_id,user_id,refresh_hash,expires_at,rotated_from) VALUES($1,$2,$3,$4,now()+interval '1 day',$5)`, childID, companyID, userID, domainauth.HashRefreshToken("legacy-child"), parentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET replaced_by=$2 WHERE id=$1`, parentID, childID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, migration("up")); err != nil {
		t.Fatal(err)
	}
	var families int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT family_id) FROM sessions WHERE user_id=$1`, userID).Scan(&families); err != nil || families != 1 {
		t.Fatalf("backfill split history: %d %v", families, err)
	}
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	service := companyRegistrationTestService(t, pool, clock)
	if _, err := service.Refresh(ctx, "legacy-parent", SessionMeta{}); err == nil {
		t.Fatal("random historical child cannot be reproduced")
	}
	current, err := service.Refresh(ctx, "legacy-child", SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if current.FamilyID != parentID {
		t.Fatal("historical root identity not preserved")
	}
	if err = service.Logout(ctx, "legacy-parent"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Refresh(ctx, current.RefreshToken, SessionMeta{}); err == nil {
		t.Fatal("historical ancestor logout did not revoke child")
	}
	t.Run("cross-user chain rejected transactionally", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, migration("down")); err != nil {
			t.Fatal(err)
		}
		otherID := uuid.New()
		if _, err = tx.Exec(ctx, `INSERT INTO users(id,company_id,email,first_name,role,status) VALUES($1,$2,'other@test.invalid','Other','employee','active')`, otherID, companyID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO sessions(id,company_id,user_id,refresh_hash,expires_at,rotated_from) VALUES($1,$2,$3,$4,now()+interval '1 day',$5)`, uuid.New(), companyID, otherID, domainauth.HashRefreshToken("corrupt-child"), parentID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, migration("up")); err == nil {
			t.Fatal("cross-user history accepted")
		}
	})
}
