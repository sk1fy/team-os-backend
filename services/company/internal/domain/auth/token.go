package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const refreshTokenBytes = 32

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
