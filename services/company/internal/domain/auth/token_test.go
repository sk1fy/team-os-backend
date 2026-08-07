package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestNewAccessLinkToken(t *testing.T) {
	const (
		samples              = 100
		expectedDecodedBytes = 32
	)

	tokens := make(map[string]struct{}, samples)
	for range samples {
		token, err := NewAccessLinkToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != base64.RawURLEncoding.EncodedLen(expectedDecodedBytes) {
			t.Fatalf(
				"NewAccessLinkToken() length = %d, want %d",
				len(token),
				base64.RawURLEncoding.EncodedLen(expectedDecodedBytes),
			)
		}
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil {
			t.Fatalf("NewAccessLinkToken() returned invalid base64url: %v", err)
		}
		if len(decoded) != expectedDecodedBytes {
			t.Fatalf("decoded token length = %d, want %d", len(decoded), expectedDecodedBytes)
		}
		if _, exists := tokens[token]; exists {
			t.Fatalf("NewAccessLinkToken() returned duplicate token %q", token)
		}
		tokens[token] = struct{}{}
	}
}

func TestNewRefreshToken(t *testing.T) {
	token, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || len(hash) != 32 {
		t.Fatalf("unexpected token/hash lengths: %d/%d", len(token), len(hash))
	}
	if !bytes.Equal(hash, HashRefreshToken(token)) {
		t.Fatal("returned hash does not match token")
	}
}

func TestOpaqueTokenIsURLSafeAndStoredAsHash(t *testing.T) {
	first, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) < 32 {
		t.Fatalf("одноразовые токены не уникальны: %q, %q", first, second)
	}
	if _, err = base64.RawURLEncoding.Strict().DecodeString(first); err != nil {
		t.Fatalf("токен не URL-safe: %v", err)
	}
	firstHash := HashOpaqueToken(first)
	secondHash := HashOpaqueToken(second)
	if len(firstHash) != sha256.Size || bytes.Equal(firstHash, secondHash) {
		t.Fatalf("некорректные хеши: %x, %x", firstHash, secondHash)
	}
}
