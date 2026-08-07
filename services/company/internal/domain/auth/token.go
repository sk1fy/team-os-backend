package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const (
	refreshTokenBytes    = 32
	accessLinkTokenBytes = 32
	opaqueTokenBytes     = 32
)

// NewOpaqueToken creates a high-entropy, URL-safe token for one-time
// bootstrap and SSO exchanges. Callers must persist only HashOpaqueToken.
func NewOpaqueToken() (string, error) {
	random := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("сгенерировать одноразовый токен: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func HashOpaqueToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func NewAccessLinkToken() (string, error) {
	random := make([]byte, accessLinkTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("сгенерировать токен ссылки доступа: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func NewRefreshToken() (string, []byte, error) {
	random := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", nil, fmt.Errorf("сгенерировать refresh token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	return token, HashRefreshToken(token), nil
}

func HashRefreshToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
