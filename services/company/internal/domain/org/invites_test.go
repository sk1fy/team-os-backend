package org

import (
	"errors"
	"testing"
)

func TestValidateInviteEmail(t *testing.T) {
	users := []User{
		{
			ID:     "u-1",
			Email:  "active@company.ru",
			Role:   RoleEmployee,
			Status: StatusActive,
		},
		{
			ID:     "u-2",
			Email:  "old@company.ru",
			Role:   RoleEmployee,
			Status: StatusDeactivated,
		},
		{
			ID:     "u-3",
			Email:  "invited@company.ru",
			Role:   RoleEmployee,
			Status: StatusInvited,
		},
	}

	tests := []struct {
		name        string
		email       string
		want        error
		wantMessage string
	}{
		{
			name:        "rejects an active user's email",
			email:       "active@company.ru",
			want:        ErrInviteEmailAlreadyExists,
			wantMessage: "Сотрудник с таким email уже есть в компании",
		},
		{
			name:  "allows a deactivated user's email",
			email: "old@company.ru",
		},
		{
			name:        "rejects an invited user's email",
			email:       "invited@company.ru",
			want:        ErrInviteEmailAlreadyExists,
			wantMessage: "Сотрудник с таким email уже есть в компании",
		},
		{
			name:        "normalizes whitespace and case",
			email:       "  ACTIVE@COMPANY.RU  ",
			want:        ErrInviteEmailAlreadyExists,
			wantMessage: "Сотрудник с таким email уже есть в компании",
		},
		{
			name:        "rejects an empty email",
			email:       "   ",
			want:        ErrInvalidInviteEmail,
			wantMessage: "Некорректный email",
		},
		{
			name:        "rejects a malformed email",
			email:       "not-an-email",
			want:        ErrInvalidInviteEmail,
			wantMessage: "Некорректный email",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ValidateInviteEmail(test.email, users)
			if !errors.Is(got, test.want) {
				t.Errorf("ValidateInviteEmail() error = %v, want %v", got, test.want)
			}
			if got != nil && got.Error() != test.wantMessage {
				t.Errorf("ValidateInviteEmail() message = %q, want %q", got.Error(), test.wantMessage)
			}
		})
	}
}
