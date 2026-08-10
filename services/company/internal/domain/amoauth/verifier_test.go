package amoauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testSecret     = "amo-secret"
	testClientUUID = "0b0832f6-d123-4123-9123-e73f236833c"
	testAudience   = "https://company.rkrs.ru"
)

func TestVerifier(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	validPayload := map[string]any{
		"iss": "https://rakurs.amocrm.ru", "aud": testAudience, "jti": "d628f123-5123-473e-a123-ed123ef31f8f",
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(30 * time.Minute).Unix(),
		"account_id": int64(31355990), "user_id": int64(87654321), "client_uuid": testClientUUID,
	}
	verifier := NewVerifier(Config{
		Secret: testSecret, ClientUUID: testClientUUID, Audience: testAudience,
		MaxTTL: time.Hour, ClockSkew: 10 * time.Second, Now: func() time.Time { return now },
	})
	tests := []struct {
		name   string
		mutate func(map[string]any) string
		valid  bool
	}{
		{name: "valid", mutate: func(payload map[string]any) string { return signToken(payload, testSecret) }, valid: true},
		{name: "bad signature", mutate: func(payload map[string]any) string { return signToken(payload, "other-secret") }},
		{name: "foreign client uuid", mutate: func(payload map[string]any) string {
			payload["client_uuid"] = "foreign"
			return signToken(payload, testSecret)
		}},
		{name: "foreign audience", mutate: func(payload map[string]any) string {
			payload["aud"] = "https://other.example"
			return signToken(payload, testSecret)
		}},
		{name: "issuer is not amocrm", mutate: func(payload map[string]any) string {
			payload["iss"] = "https://amocrm.ru.evil.example"
			return signToken(payload, testSecret)
		}},
		{name: "expired", mutate: func(payload map[string]any) string {
			payload["exp"] = now.Add(-time.Minute).Unix()
			return signToken(payload, testSecret)
		}},
		{name: "ttl too long", mutate: func(payload map[string]any) string {
			payload["exp"] = now.Add(2 * time.Hour).Unix()
			return signToken(payload, testSecret)
		}},
		{name: "not base64", mutate: func(map[string]any) string { return "not+base64.payload.signature" }},
		{name: "two segments", mutate: func(map[string]any) string { return "header.payload" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := cloneMap(validPayload)
			identity, err := verifier.Verify(test.mutate(payload))
			if test.valid {
				if err != nil || identity.AccountID != 31355990 || identity.UserID != 87654321 {
					t.Fatalf("identity=%#v error=%v", identity, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("error=%v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestVerifierNotConfigured(t *testing.T) {
	if _, err := NewVerifier(Config{}).Verify("token"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error=%v, want ErrNotConfigured", err)
	}
}

func signToken(payload map[string]any, secret string) string {
	headerJSON, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
	payloadJSON, _ := json.Marshal(payload)
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	body := base64.RawURLEncoding.EncodeToString(payloadJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(header + "." + body))
	return strings.Join([]string{header, body, base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}, ".")
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
