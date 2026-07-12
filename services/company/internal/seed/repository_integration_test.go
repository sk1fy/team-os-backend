package seed

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestApplyAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("COMPANY_SEED_TEST_DB_URL")
	if databaseURL == "" {
		t.Skip("COMPANY_SEED_TEST_DB_URL is not set")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(ctx) }()

	fixtures := minimalFixtures()
	parentID := "department-1"
	fixtures.Departments = []DepartmentFixture{
		{ID: "department-child", Name: "Продажи", ParentID: &parentID, Order: 0},
		{ID: parentID, Name: "Ромашка", Order: 0},
	}
	fixtures.Positions[0].DepartmentID = "department-child"
	fixtures.Positions[0].ArticleIDs = []string{"article-1"}
	positionID := "position-1"
	departmentID := "department-child"
	email := "candidate@example.com"
	fixtures.Invites = []InviteFixture{{
		ID: "invite-1", Email: &email, Token: "integration-token", Role: "employee",
		PositionID: &positionID, DepartmentID: &departmentID, InvitedByID: "user-1",
		Status: "pending", CreatedAt: "2026-07-10T12:00:00Z",
	}}
	dataset, err := Normalize(fixtures, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, cleanupErr := connection.Exec(ctx, "DELETE FROM companies WHERE id = $1", dataset.Company.ID); cleanupErr != nil {
			t.Errorf("cleanup company: %v", cleanupErr)
		}
	}()

	firstTx, err := connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, firstTx, dataset, "$argon2id$integration-test"); err != nil {
		_ = firstTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := firstTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// A second transaction exercises the persisted ON CONFLICT path.
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := Apply(ctx, tx, dataset, "$argon2id$integration-test"); err != nil {
		t.Fatal(err)
	}

	assertCount := func(table string, want int) {
		t.Helper()
		var count int
		if err := tx.QueryRow(
			ctx,
			"SELECT count(*) FROM "+table+" WHERE company_id = $1",
			dataset.Company.ID,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", table, count, want)
		}
	}
	assertCount("users", 1)
	assertCount("credentials", 1)
	assertCount("departments", 2)
	assertCount("positions", 1)
	assertCount("user_positions", 1)
	assertCount("invites", 1)

	var ownerID string
	if err := tx.QueryRow(ctx, "SELECT owner_id::text FROM companies WHERE id = $1", dataset.Company.ID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID != dataset.Company.OwnerID.String() {
		t.Fatalf("owner_id = %s, want %s", ownerID, dataset.Company.OwnerID)
	}
}
