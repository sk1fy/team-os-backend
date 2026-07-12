package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := HashPassword("Надёжный-пароль-2026")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("Надёжный-пароль-2026", encoded)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(correct) = %v, %v", ok, err)
	}
	ok, err = VerifyPassword("другой-пароль", encoded)
	if err != nil || ok {
		t.Fatalf("VerifyPassword(wrong) = %v, %v", ok, err)
	}
}

func TestPasswordValidation(t *testing.T) {
	if _, err := HashPassword("short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if _, err := HashPassword(strings.Repeat("x", 257)); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("HashPassword(long) error = %v", err)
	}
	if _, err := VerifyPassword("password", "broken"); !errors.Is(err, ErrInvalidPasswordHash) {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
}
