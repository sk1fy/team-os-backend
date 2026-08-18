package amochallenge

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	purpose = "amo_admin_self_login"
	version = 1
)

var ErrInvalid = errors.New("challenge недействителен или истёк")

type Claims struct {
	Type         string `json:"typ"`
	Purpose      string `json:"purpose"`
	Version      int    `json:"version"`
	AmoAccountID string `json:"amoAccountId"`
	JTI          string `json:"jti"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
}

type Manager struct {
	secret   []byte
	ttl      time.Duration
	now      func() time.Time
	mu       sync.Mutex
	consumed map[string]int64
}

func New(secret string, ttl time.Duration) (*Manager, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 32 {
		return nil, errors.New("AMO_BROWSER_CHALLENGE_SECRET: требуется не менее 32 байтов")
	}
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &Manager{secret: []byte(secret), ttl: ttl, now: time.Now, consumed: make(map[string]int64)}, nil
}

func (m *Manager) TTLSeconds() int {
	return int(m.ttl / time.Second)
}

func (m *Manager) Issue(amoAccountID string) (string, error) {
	if !canonicalPositiveID(amoAccountID) {
		return "", ErrInvalid
	}
	now := m.now().UTC()
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", err
	}
	claims := Claims{
		Type: purpose, Purpose: purpose, Version: version, AmoAccountID: amoAccountID,
		JTI: base64.RawURLEncoding.EncodeToString(jtiBytes), IssuedAt: now.Unix(), ExpiresAt: now.Add(m.ttl).Unix(),
	}
	header, err := json.Marshal(map[string]any{"alg": "HS256", "typ": "AMO_BROWSER_CHALLENGE"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	return unsigned + "." + m.signature(unsigned), nil
}

func (m *Manager) VerifyAndConsume(token string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || len(token) > 2048 {
		return Claims{}, ErrInvalid
	}
	unsigned := parts[0] + "." + parts[1]
	want, err := base64.RawURLEncoding.DecodeString(m.signature(unsigned))
	if err != nil {
		return Claims{}, ErrInvalid
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, want) {
		return Claims{}, ErrInvalid
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalid
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if json.Unmarshal(headerBytes, &header) != nil || header.Algorithm != "HS256" || header.Type != "AMO_BROWSER_CHALLENGE" {
		return Claims{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalid
	}
	var claims Claims
	if json.Unmarshal(payload, &claims) != nil {
		return Claims{}, ErrInvalid
	}
	now := m.now().UTC().Unix()
	if claims.Type != purpose || claims.Purpose != purpose || claims.Version != version ||
		!canonicalPositiveID(claims.AmoAccountID) || claims.JTI == "" ||
		claims.IssuedAt > now+5 || claims.ExpiresAt <= now || claims.ExpiresAt-claims.IssuedAt > int64(m.ttl/time.Second) {
		return Claims{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for jti, expiresAt := range m.consumed {
		if expiresAt <= now {
			delete(m.consumed, jti)
		}
	}
	if _, replayed := m.consumed[claims.JTI]; replayed {
		return Claims{}, ErrInvalid
	}
	// Best-effort replay protection is process-local. The client assertion is
	// consciously not proof of amoCRM identity or rights.
	m.consumed[claims.JTI] = claims.ExpiresAt
	return claims, nil
}

func (m *Manager) signature(value string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func canonicalPositiveID(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}
