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
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
)

func TestBootstrapAmoCompanyCreatesUsersWithServerRoles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		pool: pool, now: func() time.Time { return now }, refreshTTL: 24 * time.Hour,
		issuer: sharedauth.NewTokenIssuer(privateKey, "teamos-company", "teamos-api", 15*time.Minute),
	}

	result, err := service.BootstrapAmoCompany(ctx, AmoCompanyBootstrapInput{
		AmoAccountID: "31355990", CompanyName: "Тестовая компания", Subdomain: "test",
		SelfUserID: "101", RequestID: "bootstrap-request",
		Users: []AmoCompanyBootstrapUser{
			{ID: "101", Email: "owner@example.com", Name: "Иван Иванов", IsAdmin: true, IsActive: true},
			{ID: "102", Email: "admin@example.com", Name: "Пётр Петров", IsAdmin: true, IsActive: true},
			{ID: "103", Email: "employee@example.com", Name: "Анна", IsActive: true},
			{ID: "104", Email: "inactive@example.com", Name: "Неактивный"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "register" || result.Role != "owner" || result.AccessToken == "" {
		t.Fatalf("result=%#v", result)
	}

	rows, err := pool.Query(ctx, `
		SELECT external_id, role, status
		FROM users
		WHERE company_id=$1
		ORDER BY external_id`, result.CompanyID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := [][3]string{
		{"101", "owner", "active"},
		{"102", "admin", "active"},
		{"103", "employee", "active"},
		{"104", "employee", "deactivated"},
	}
	index := 0
	for rows.Next() {
		if index >= len(want) {
			t.Fatal("unexpected extra user")
		}
		var externalID, role, status string
		if err = rows.Scan(&externalID, &role, &status); err != nil {
			t.Fatal(err)
		}
		if got := [3]string{externalID, role, status}; got != want[index] {
			t.Fatalf("user[%d]=%v want=%v", index, got, want[index])
		}
		index++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(want) {
		t.Fatalf("users=%d want=%d", index, len(want))
	}

	var ownerExternalID, accessToken, entryContext string
	var entryContextConsumed bool
	if err = pool.QueryRow(ctx, `
		SELECT owner.external_id, access.token, access.entry_context,
		       access.entry_context_consumed_at IS NOT NULL
		FROM companies AS company
		JOIN users AS owner ON owner.id=company.owner_id AND owner.company_id=company.id
		JOIN access_links AS access ON access.company_id=company.id AND access.user_id=owner.id
		WHERE company.id=$1`, result.CompanyID).
		Scan(&ownerExternalID, &accessToken, &entryContext, &entryContextConsumed); err != nil {
		t.Fatal(err)
	}
	if ownerExternalID != "101" || accessToken != result.AccessToken ||
		entryContext != "company_created" || entryContextConsumed {
		t.Fatalf(
			"owner=%s token=%s context=%s consumed=%t",
			ownerExternalID, accessToken, entryContext, entryContextConsumed,
		)
	}

	firstLogin, err := service.LoginWithAccessLink(ctx, result.AccessToken, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if firstLogin.EntryContext != "company_created" {
		t.Fatalf("first entry context=%q", firstLogin.EntryContext)
	}
	secondLogin, err := service.LoginWithAccessLink(ctx, result.AccessToken, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if secondLogin.EntryContext != "" {
		t.Fatalf("second entry context=%q, want empty", secondLogin.EntryContext)
	}

	var employeeID string
	if err = pool.QueryRow(ctx, `
		SELECT id::text FROM users WHERE company_id=$1 AND external_id='103'`, result.CompanyID).
		Scan(&employeeID); err != nil {
		t.Fatal(err)
	}
	employeeUUID, parseErr := uuid.Parse(employeeID)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	ordinaryLink, err := service.SetLinkAccess(
		ctx,
		Actor{CompanyID: result.CompanyID, UserID: result.UserID, Role: "owner"},
		employeeUUID,
	)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryLogin, err := service.LoginWithAccessLink(ctx, ordinaryLink.Token, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if ordinaryLogin.EntryContext != "" {
		t.Fatalf("ordinary entry context=%q, want empty", ordinaryLogin.EntryContext)
	}

	if _, err = pool.Exec(ctx, `
		UPDATE access_links
		SET entry_context_consumed_at=NULL
		WHERE token=$1`, result.AccessToken); err != nil {
		t.Fatal(err)
	}
	contexts := make(chan string, 2)
	errors := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			login, loginErr := service.LoginWithAccessLink(ctx, result.AccessToken, SessionMeta{})
			if loginErr != nil {
				errors <- loginErr
				return
			}
			contexts <- login.EntryContext
		}()
	}
	wait.Wait()
	close(errors)
	for loginErr := range errors {
		t.Fatal(loginErr)
	}
	close(contexts)
	contextCount := 0
	for value := range contexts {
		if value == "company_created" {
			contextCount++
		}
	}
	if contextCount != 1 {
		t.Fatalf("concurrent entry contexts=%d, want 1", contextCount)
	}

	if _, err = service.BootstrapAmoCompany(ctx, AmoCompanyBootstrapInput{
		AmoAccountID: "31355990", CompanyName: "Повтор", SelfUserID: "101",
		Users: []AmoCompanyBootstrapUser{
			{ID: "101", Email: "owner@example.com", Name: "Иван", IsAdmin: true, IsActive: true},
		},
	}); !isCompanyError(err, ErrorConflict) {
		t.Fatalf("replay error=%v", err)
	}
}
