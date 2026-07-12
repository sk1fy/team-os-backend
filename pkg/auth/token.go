package auth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("токен недействителен или истёк")

// TokenIssuer signs short-lived access tokens for the company service.
type TokenIssuer struct {
	key      ed25519.PrivateKey
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

func NewTokenIssuer(key ed25519.PrivateKey, issuer, audience string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{key: key, issuer: issuer, audience: audience, ttl: ttl, now: time.Now}
}

func (i *TokenIssuer) Issue(subject, companyID, role string, positions, departments []string) (string, time.Time, error) {
	now := i.now().UTC()
	expiresAt := now.Add(i.ttl)
	claims := Claims{
		CompanyID:     companyID,
		Role:          role,
		PositionIDs:   append([]string(nil), positions...),
		DepartmentIDs: append([]string(nil), departments...),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{i.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(i.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("подписать access token: %w", err)
	}
	return token, expiresAt, nil
}

// TokenVerifier validates access tokens in the gateway and domain services.
type TokenVerifier struct {
	key      ed25519.PublicKey
	issuer   string
	audience string
	now      func() time.Time
}

func NewTokenVerifier(key ed25519.PublicKey, issuer, audience string) *TokenVerifier {
	return &TokenVerifier{key: key, issuer: issuer, audience: audience, now: time.Now}
}

func (v *TokenVerifier) Verify(raw string) (*Claims, error) {
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodEdDSA {
				return nil, ErrInvalidToken
			}
			return v.key, nil
		},
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.now),
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
	)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
