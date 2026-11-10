package transport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/authmw"
)

func TestBrowserEntryDeniedBeforeReadingIdentityOrCallingCompany(t *testing.T) {
	// A nil client makes any accidental identity lookup/provisioning fail this test.
	h := NewHandler(nil, nil, nil, nil, CookieConfig{}, nil)
	router := authmw.Middleware(nil)(api.Handler(h))
	encode := base64.RawURLEncoding.EncodeToString
	oldHeader := encode([]byte(`{"alg":"HS256","typ":"AMO_BROWSER_CHALLENGE"}`))
	oldTokenFor := func(purpose string) string {
		payload, _ := json.Marshal(map[string]any{
			"typ": purpose, "purpose": purpose, "version": 1, "amoAccountId": "31355990",
			"jti": "old-issued-challenge", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		})
		unsigned := oldHeader + "." + encode(payload)
		mac := hmac.New(sha256.New, []byte("old-browser-challenge-secret-at-least-32-bytes"))
		_, _ = mac.Write([]byte(unsigned))
		return unsigned + "." + encode(mac.Sum(nil))
	}
	for _, path := range []string{"/api/v1/public/amocrm/admin-self-login", "/api/v1/public/amocrm/company-bootstrap"} {
		purpose := "amo_admin_self_login"
		if strings.HasSuffix(path, "company-bootstrap") {
			purpose = "amo_company_bootstrap"
		}
		for authIndex, authorization := range []string{"", "Bearer " + oldTokenFor(purpose), "Service test-service-token", "Bearer arbitrary-session"} {
			for bodyIndex, body := range []string{
				`{"account":{"id":"31355990"},"selfUserId":"101","users":[{"id":"101","isAdmin":true,"isActive":true}]}`,
				`{"account":{"id":"31355991"},"selfUserId":"102","users":[{"id":"102","email":"spoof@example.test","name":"Подмена","isAdmin":true,"isActive":true,"role":"owner"}]}`,
				`{"selfUserId":"103","users":[{"id":"103","isAdmin":false,"isActive":false}]}`,
				`{`,
			} {
				t.Run(fmt.Sprintf("%s/auth_%d/payload_%d", path, authIndex, bodyIndex), func(t *testing.T) {
					req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
					req.Header.Set("Authorization", authorization)
					req.Header.Set("Content-Type", "application/json")
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					if rec.Code != http.StatusForbidden || responseErrorCode(t, rec) != "AMO_BROWSER_ENTRY_DISABLED" {
						t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
					}
					if rec.Header().Get("Cache-Control") != "private, no-store" || len(rec.Result().Cookies()) != 0 {
						t.Fatalf("unexpected cache/cookie headers: %v", rec.Header())
					}
					for _, field := range []string{"accessToken", "redirectUrl", "challengeToken"} {
						if strings.Contains(rec.Body.String(), field) {
							t.Fatalf("denied entry leaked %s", field)
						}
					}
				})
			}
		}
	}
}

func TestProvisioningCannotTrustBrowserWithoutServiceCredential(t *testing.T) {
	paths := []string{
		"/api/v1/provisioning/amocrm/registration-tokens",
		"/api/v1/provisioning/amocrm/admin-sessions",
		"/api/v1/provisioning/amocrm/company-status",
		"/api/v1/provisioning/amocrm/widget-entries",
	}
	for _, token := range []string{"", "too-short", testProvisioningServiceToken} {
		h := NewHandler(nil, nil, nil, nil, CookieConfig{}, nil)
		h.SetProvisioningServiceToken(token)
		h.SetProvisioningServiceProvider("rakurs")
		router := api.Handler(h)
		for _, path := range paths {
			for _, credentials := range [][]string{nil, {"Bearer " + testProvisioningServiceToken}, {"Service "}, {"Service too-short"}, {"Service wrong"}, {"Service " + testProvisioningServiceToken, "Service wrong"}} {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"amoAccountId":"31355990","selfUserId":"101","isAdmin":true,"isActive":true}`))
				for _, credential := range credentials {
					req.Header.Add("Authorization", credential)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnauthorized || responseErrorCode(t, rec) != "SERVICE_AUTH_INVALID" {
					t.Fatalf("path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
				}
			}
		}
	}
}
