package application

import "testing"

func TestNormalizeAmoBootstrapUsersAssignsServerRoles(t *testing.T) {
	users, err := normalizeAmoBootstrapUsers([]AmoCompanyBootstrapUser{
		{ID: "101", Email: "owner@example.ru", Name: "Иван Иванов", IsAdmin: true, IsActive: true},
		{ID: "102", Email: "admin@example.ru", Name: "Пётр Петров", IsAdmin: true, IsActive: true},
		{ID: "103", Email: "employee@example.ru", Name: "Анна", IsActive: true},
		{ID: "104", Email: "inactive@example.ru", Name: "Неактивный"},
	}, "101")
	if err != nil {
		t.Fatal(err)
	}
	wantRoles := []string{"owner", "admin", "employee", "employee"}
	wantStatuses := []string{"active", "active", "active", "deactivated"}
	for index := range users {
		if users[index].role != wantRoles[index] || users[index].status != wantStatuses[index] {
			t.Fatalf("user[%d]=%#v", index, users[index])
		}
	}
}

func TestNormalizeAmoBootstrapUsersRejectsInvalidAssertion(t *testing.T) {
	tests := []struct {
		name  string
		users []AmoCompanyBootstrapUser
	}{
		{
			name: "self is not admin",
			users: []AmoCompanyBootstrapUser{
				{ID: "101", Email: "owner@example.ru", Name: "Иван", IsActive: true},
			},
		},
		{
			name: "duplicate id",
			users: []AmoCompanyBootstrapUser{
				{ID: "101", Email: "owner@example.ru", Name: "Иван", IsAdmin: true, IsActive: true},
				{ID: "101", Email: "other@example.ru", Name: "Пётр", IsAdmin: true, IsActive: true},
			},
		},
		{
			name: "duplicate email",
			users: []AmoCompanyBootstrapUser{
				{ID: "101", Email: "same@example.ru", Name: "Иван", IsAdmin: true, IsActive: true},
				{ID: "102", Email: "SAME@example.ru", Name: "Пётр", IsActive: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeAmoBootstrapUsers(test.users, "101"); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
