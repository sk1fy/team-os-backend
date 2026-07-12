package auth

import (
	"bytes"
	"testing"
)

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
