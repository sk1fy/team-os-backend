//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProvisionAmoAdminSessionRoleAndLinkLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := companyAccessTestPool(t, ctx)
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	service := &Service{pool: pool, now: func() time.Time { return now }}
	accountID := "31355990"
	firstAdmin := AmoAdminSessionInput{
		Provider: "rakurs", ExternalAccountID: accountID, ExternalUserID: "10912522",
		Email: "first-admin@example.com", UserName: "Первый Администратор",
		CompanyName: "Тестовая компания amoCRM", DesiredRole: "admin",
	}

	created, err := service.ProvisionAmoAdminSession(ctx, firstAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if created.Action != "register" || created.Role != "admin" || created.AccessToken == "" {
		t.Fatalf("first admin result = %#v", created)
	}
	assertAmoAdminSessionState(t, ctx, pool, created.CompanyID, created.UserID, uuid.Nil, "admin", created.AccessToken)

	replayed, err := service.ProvisionAmoAdminSession(ctx, firstAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Action != "login" || replayed.CompanyID != created.CompanyID || replayed.UserID != created.UserID ||
		replayed.Role != "admin" || replayed.AccessToken != created.AccessToken {
		t.Fatalf("idempotent replay = %#v, first = %#v", replayed, created)
	}

	firstAdmin.DesiredRole = "owner"
	firstOwner, err := service.ProvisionAmoAdminSession(ctx, firstAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if firstOwner.Role != "owner" || firstOwner.AccessToken != created.AccessToken {
		t.Fatalf("first owner result = %#v", firstOwner)
	}
	assertAmoAdminSessionState(
		t, ctx, pool, created.CompanyID, created.UserID, created.UserID, "owner", created.AccessToken,
	)

	secondOwner, err := service.ProvisionAmoAdminSession(ctx, AmoAdminSessionInput{
		Provider: "rakurs", ExternalAccountID: accountID, ExternalUserID: "10912523",
		Email: "second-owner@example.com", UserName: "Новый Владелец",
		CompanyName: "Тестовая компания amoCRM", DesiredRole: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondOwner.Action != "login" || secondOwner.Role != "owner" || secondOwner.UserID == created.UserID {
		t.Fatalf("second owner result = %#v", secondOwner)
	}
	assertAmoAdminSessionState(
		t, ctx, pool, created.CompanyID, created.UserID, secondOwner.UserID, "admin", created.AccessToken,
	)
	assertAmoAdminSessionState(
		t, ctx, pool, created.CompanyID, secondOwner.UserID, secondOwner.UserID, "owner", secondOwner.AccessToken,
	)

	preserveOwner := AmoAdminSessionInput{
		Provider: "rakurs", ExternalAccountID: accountID, ExternalUserID: "10912523",
		Email: "second-owner@example.com", UserName: "Новый Владелец",
		CompanyName: "Тестовая компания amoCRM", DesiredRole: "admin",
	}
	preserved, err := service.ProvisionAmoAdminSession(ctx, preserveOwner)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Role != "owner" || preserved.UserID != secondOwner.UserID ||
		preserved.AccessToken != secondOwner.AccessToken {
		t.Fatalf("preserved owner result = %#v, previous = %#v", preserved, secondOwner)
	}
	assertAmoAdminSessionState(
		t, ctx, pool, created.CompanyID, secondOwner.UserID, secondOwner.UserID, "owner", secondOwner.AccessToken,
	)
}

func assertAmoAdminSessionState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID, userID, expectedOwnerID uuid.UUID,
	expectedRole, expectedAccessToken string,
) {
	t.Helper()
	var ownerID uuid.NullUUID
	if err := pool.QueryRow(ctx, `SELECT owner_id FROM companies WHERE id=$1`, companyID).Scan(&ownerID); err != nil {
		t.Fatalf("read company owner: %v", err)
	}
	if expectedOwnerID == uuid.Nil {
		if ownerID.Valid {
			t.Fatalf("owner_id = %v, want NULL", ownerID)
		}
	} else if !ownerID.Valid || ownerID.UUID != expectedOwnerID {
		t.Fatalf("owner_id = %v, want %s", ownerID, expectedOwnerID)
	}
	var role, accessToken string
	if err := pool.QueryRow(ctx, `
		SELECT u.role, access.token
		FROM users AS u
		JOIN access_links AS access ON access.company_id=u.company_id AND access.user_id=u.id
		WHERE u.company_id=$1 AND u.id=$2`, companyID, userID,
	).Scan(&role, &accessToken); err != nil {
		t.Fatalf("read user access: %v", err)
	}
	if role != expectedRole || accessToken != expectedAccessToken {
		t.Fatalf("role/token = %q/%q, want %q/%q", role, accessToken, expectedRole, expectedAccessToken)
	}
}
