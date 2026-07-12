package org

import "errors"

var (
	ErrMultiplePositions = errors.New("Сотруднику можно назначить только одну должность")
	ErrOwnerRoleChange   = errors.New("Нельзя изменить роль владельца компании")
	ErrOwnerDeactivate   = errors.New("Нельзя деактивировать владельца компании")
	ErrSelfRoleDemotion  = errors.New("Нельзя понизить собственную роль")
	ErrSelfDeactivate    = errors.New("Нельзя деактивировать собственный аккаунт")
)

// UserUpdateInput contains optional role and status changes. Nil means the
// corresponding field is not part of the update.
type UserUpdateInput struct {
	Role   *UserRole
	Status *UserStatus
}

// UserUpdateContext identifies the company owner and the actor performing an
// update.
type UserUpdateContext struct {
	OwnerID       ID
	CurrentUserID ID
}

// ValidatePositionAssignment enforces the company invariant that a user has
// at most one position.
func ValidatePositionAssignment(positionIDs []ID) error {
	if len(positionIDs) > 1 {
		return ErrMultiplePositions
	}

	return nil
}

// ValidateUserUpdate protects the company owner and prevents an actor from
// demoting or deactivating their own account.
func ValidateUserUpdate(user User, input UserUpdateInput, context UserUpdateContext) error {
	isOwner := user.ID == context.OwnerID
	isSelf := user.ID == context.CurrentUserID

	if isOwner {
		if input.Role != nil && *input.Role != RoleOwner {
			return ErrOwnerRoleChange
		}
		if input.Status != nil && *input.Status != StatusActive {
			return ErrOwnerDeactivate
		}
	}

	if isSelf && input.Role != nil && *input.Role != user.Role {
		if roleRank(*input.Role) > roleRank(user.Role) {
			return ErrSelfRoleDemotion
		}
	}

	if isSelf && input.Status != nil && *input.Status == StatusDeactivated {
		return ErrSelfDeactivate
	}

	return nil
}

func roleRank(role UserRole) int {
	switch role {
	case RoleOwner:
		return 0
	case RoleAdmin:
		return 1
	case RoleEmployee:
		return 2
	case RolePartner:
		return 3
	default:
		// UserRole is closed at API boundaries. Keeping unknown roles below the
		// known hierarchy prevents an invalid value from looking like a promotion.
		return len([]UserRole{RoleOwner, RoleAdmin, RoleEmployee, RolePartner})
	}
}
