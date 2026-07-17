package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	const (
		samples         = 100
		expectedLength  = 14
		allowedAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	)

	passwords := make(map[string]struct{}, samples)
	for range samples {
		password, err := GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(password) != expectedLength {
			t.Fatalf("GeneratePassword() length = %d, want %d", len(password), expectedLength)
		}
		for _, character := range password {
			if !strings.ContainsRune(allowedAlphabet, character) {
				t.Fatalf("GeneratePassword() contains character %q outside the allowed alphabet", character)
			}
		}
		if _, exists := passwords[password]; exists {
			t.Fatalf("GeneratePassword() returned duplicate password %q", password)
		}
		passwords[password] = struct{}{}
	}
}

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
