package org

import (
	"errors"
	"net/mail"
	"strings"
)

var (
	// ErrInvalidInviteEmail indicates that the invitation email is not a
	// syntactically valid mailbox address.
	ErrInvalidInviteEmail = errors.New("Некорректный email")

	// ErrInviteEmailAlreadyExists indicates that a non-deactivated user with
	// the invitation email already exists in the company.
	ErrInviteEmailAlreadyExists = errors.New("Сотрудник с таким email уже есть в компании")
)

// NormalizeEmail returns the canonical representation used for invitation
// comparisons.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateInviteEmail validates the email format and rejects duplicates among
// active or invited users. An email belonging only to a deactivated user may
// be invited again.
func ValidateInviteEmail(email string, users []User) error {
	normalized := NormalizeEmail(email)
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return ErrInvalidInviteEmail
	}

	for _, user := range users {
		if NormalizeEmail(user.Email) == normalized && user.Status != StatusDeactivated {
			return ErrInviteEmailAlreadyExists
		}
	}

	return nil
}
