package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidKey = errors.New("некорректный ключ Ed25519")

// ParsePrivateKey accepts a PKCS#8 PEM, a base64-encoded Ed25519 seed, or a
// base64-encoded private key. Keeping parsing here gives every binary the same
// deployment contract.
func ParsePrivateKey(value string) (ed25519.PrivateKey, error) {
	raw := []byte(strings.TrimSpace(value))
	if privateKey, ok, err := parsePrivatePEM(raw); ok {
		return privateKey, err
	}

	decoded, err := decodeBase64(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	if privateKey, ok, err := parsePrivatePEM(decoded); ok {
		return privateKey, err
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(decoded), nil
	default:
		return nil, ErrInvalidKey
	}
}

// ParsePublicKey accepts a PKIX PEM or a base64-encoded raw public key.
func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	raw := []byte(strings.TrimSpace(value))
	if publicKey, ok, err := parsePublicPEM(raw); ok {
		return publicKey, err
	}

	decoded, err := decodeBase64(string(raw))
	if err != nil {
		return nil, ErrInvalidKey
	}
	if publicKey, ok, err := parsePublicPEM(decoded); ok {
		return publicKey, err
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, ErrInvalidKey
	}
	return ed25519.PublicKey(decoded), nil
}

func parsePrivatePEM(raw []byte) (ed25519.PrivateKey, bool, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, false, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, true, ErrInvalidKey
	}
	return privateKey, true, nil
}

func parsePublicPEM(raw []byte) (ed25519.PublicKey, bool, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, false, nil
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, true, ErrInvalidKey
	}
	return publicKey, true, nil
}

func decodeBase64(value string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
