package org

import (
	"errors"
	"testing"
)

func TestValidateUserUpdate(t *testing.T) {
	owner := User{
		ID:     "owner-1",
		Email:  "owner@company.ru",
		Role:   RoleOwner,
		Status: StatusActive,
	}
	admin := owner
	admin.ID = "admin-1"
	admin.Role = RoleAdmin

	tests := []struct {
		name    string
		user    User
		input   UserUpdateInput
		context UserUpdateContext
		want    error
		message string
	}{
		{
			name:  "rejects changing the owner's role",
			user:  owner,
			input: UserUpdateInput{Role: rolePointer(RoleAdmin)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "admin-1",
			},
			want:    ErrOwnerRoleChange,
			message: "Нельзя изменить роль владельца компании",
		},
		{
			name:  "rejects deactivating the owner",
			user:  owner,
			input: UserUpdateInput{Status: statusPointer(StatusDeactivated)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "owner-1",
			},
			want:    ErrOwnerDeactivate,
			message: "Нельзя деактивировать владельца компании",
		},
		{
			name:  "rejects demoting oneself",
			user:  admin,
			input: UserUpdateInput{Role: rolePointer(RoleEmployee)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "admin-1",
			},
			want:    ErrSelfRoleDemotion,
			message: "Нельзя понизить собственную роль",
		},
		{
			name:  "rejects deactivating oneself",
			user:  admin,
			input: UserUpdateInput{Status: statusPointer(StatusDeactivated)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "admin-1",
			},
			want:    ErrSelfDeactivate,
			message: "Нельзя деактивировать собственный аккаунт",
		},
		{
			name:  "allows a safe unchanged role",
			user:  admin,
			input: UserUpdateInput{Role: rolePointer(RoleAdmin)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "owner-1",
			},
		},
		{
			name:  "allows changing another user's role",
			user:  admin,
			input: UserUpdateInput{Role: rolePointer(RoleEmployee)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "owner-1",
			},
		},
		{
			name:  "allows self-promotion",
			user:  User{ID: "employee-1", Role: RoleEmployee, Status: StatusActive},
			input: UserUpdateInput{Role: rolePointer(RoleAdmin)},
			context: UserUpdateContext{
				OwnerID:       "owner-1",
				CurrentUserID: "employee-1",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ValidateUserUpdate(test.user, test.input, test.context)
			if !errors.Is(got, test.want) {
				t.Errorf("ValidateUserUpdate() error = %v, want %v", got, test.want)
			}
			if got != nil && got.Error() != test.message {
				t.Errorf("ValidateUserUpdate() message = %q, want %q", got.Error(), test.message)
			}
		})
	}
}

func TestValidatePositionAssignment(t *testing.T) {
	tests := []struct {
		name        string
		positionIDs []ID
		want        error
		message     string
	}{
		{
			name: "allows no position",
		},
		{
			name:        "allows one position",
			positionIDs: []ID{"position-1"},
		},
		{
			name:        "rejects multiple positions",
			positionIDs: []ID{"position-1", "position-2"},
			want:        ErrMultiplePositions,
			message:     "Сотруднику можно назначить только одну должность",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ValidatePositionAssignment(test.positionIDs)
			if !errors.Is(got, test.want) {
				t.Errorf("ValidatePositionAssignment() error = %v, want %v", got, test.want)
			}
			if got != nil && got.Error() != test.message {
				t.Errorf("ValidatePositionAssignment() message = %q, want %q", got.Error(), test.message)
			}
		})
	}
}

func rolePointer(role UserRole) *UserRole {
	return &role
}

func statusPointer(status UserStatus) *UserStatus {
	return &status
}
