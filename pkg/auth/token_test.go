package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	issuer := NewTokenIssuer(privateKey, "teamos-company", "teamos-api", 15*time.Minute)
	issuer.now = func() time.Time { return now }
	verifier := NewTokenVerifier(publicKey, "teamos-company", "teamos-api")
	verifier.now = func() time.Time { return now.Add(time.Minute) }

	raw, expiresAt, err := issuer.Issue("user-1", "company-1", "admin", []string{"pos-1"}, []string{"dep-1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(15 * time.Minute); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
	}

	claims, err := verifier.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.CompanyID != "company-1" || claims.Role != "admin" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestParseBase64EncodedPEMKeys(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})

	parsedPrivate, err := ParsePrivateKey(base64.StdEncoding.EncodeToString(privatePEM))
	if err != nil {
		t.Fatal(err)
	}
	parsedPublic, err := ParsePublicKey(base64.StdEncoding.EncodeToString(publicPEM))
	if err != nil {
		t.Fatal(err)
	}
	if !privateKey.Equal(parsedPrivate) || !publicKey.Equal(parsedPublic) {
		t.Fatal("parsed PEM key does not match source")
	}
}

func TestTokenVerifierRejectsExpiredToken(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 12, 10, 0, 0, 0, time.UTC)
	issuer := NewTokenIssuer(privateKey, "issuer", "audience", time.Minute)
	issuer.now = func() time.Time { return now }
	raw, _, err := issuer.Issue("user", "company", "owner", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	verifier := NewTokenVerifier(publicKey, "issuer", "audience")
	verifier.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := verifier.Verify(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
	}
}

func TestParseRawKeys(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateEncoded := base64.RawStdEncoding.EncodeToString(privateKey)
	publicEncoded := base64.RawStdEncoding.EncodeToString(publicKey)

	parsedPrivate, err := ParsePrivateKey(privateEncoded)
	if err != nil {
		t.Fatal(err)
	}
	parsedPublic, err := ParsePublicKey(publicEncoded)
	if err != nil {
		t.Fatal(err)
	}
	if !privateKey.Equal(parsedPrivate) || !publicKey.Equal(parsedPublic) {
		t.Fatal("parsed key does not match source")
	}
}
