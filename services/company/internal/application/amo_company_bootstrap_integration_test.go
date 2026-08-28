//go:build integration

package application

import (
	"context"
	"testing"
	"time"
)

func TestBootstrapAmoCompanyCreatesUsersWithServerRoles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	service := &Service{pool: pool, now: func() time.Time { return now }}

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

	var ownerExternalID, accessToken string
	if err = pool.QueryRow(ctx, `
		SELECT owner.external_id, access.token
		FROM companies AS company
		JOIN users AS owner ON owner.id=company.owner_id AND owner.company_id=company.id
		JOIN access_links AS access ON access.company_id=company.id AND access.user_id=owner.id
		WHERE company.id=$1`, result.CompanyID).Scan(&ownerExternalID, &accessToken); err != nil {
		t.Fatal(err)
	}
	if ownerExternalID != "101" || accessToken != result.AccessToken {
		t.Fatalf("owner=%s token=%s", ownerExternalID, accessToken)
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
