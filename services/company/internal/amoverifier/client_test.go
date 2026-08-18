package amoverifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/domain/amoauth"
)

const (
	testServiceToken = "verifier-service-token-at-least-32-bytes"
	testWidgetToken  = "amo-widget-token-abcdefghijklmnopqrstuvwxyz"
)

func TestClientVerifyForwardsTokenAndDecodesVerifiedIdentity(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/internal/amocrm/verify-token" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Service "+testServiceToken ||
			r.Header.Get("X-Auth-Token") != testWidgetToken {
			t.Fatalf("headers = %#v", r.Header)
		}
		var body struct {
			AppName string `json:"appName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AppName != "rkrs_activity" {
			t.Fatalf("body=%#v err=%v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"account":{"id":"31355990","name":"ООО Ромашка"},
			"user":{"id":"101","email":"Admin@Example.com","name":"Иван Иванов"},
			"rights":{"isAdmin":true,"isOwner":false},
			"token":{"jti":"verified-jti","expiresAt":%q}
		}`, now.Add(5*time.Minute).Format(time.RFC3339))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(Config{
		URL: server.URL + "/api/internal/amocrm/verify-token", ServiceToken: testServiceToken,
		AppName: "rkrs_activity", HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.Verify(context.Background(), testWidgetToken)
	if err != nil {
		t.Fatal(err)
	}
	if identity.AccountID != 31355990 || identity.AccountName != "ООО Ромашка" ||
		identity.UserID != 101 || identity.UserEmail != "admin@example.com" ||
		identity.UserName != "Иван Иванов" || !identity.IsAdmin || identity.IsOwner ||
		identity.JTI != "verified-jti" || !identity.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("identity=%#v", identity)
	}
}

func TestClientVerifyMapsRemoteFailuresFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       error
	}{
		{name: "invalid token", statusCode: http.StatusUnauthorized, want: amoauth.ErrInvalidToken},
		{name: "inactive or non-admin", statusCode: http.StatusForbidden, want: amoauth.ErrForbidden},
		{name: "invalid configured app", statusCode: http.StatusBadRequest, want: amoauth.ErrUnavailable},
		{name: "upstream unavailable", statusCode: http.StatusServiceUnavailable, want: amoauth.ErrUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(`{"message":"denied"}`))
			}))
			t.Cleanup(server.Close)
			client, err := NewClient(Config{
				URL: server.URL, ServiceToken: testServiceToken, AppName: "rkrs_activity", HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Verify(context.Background(), testWidgetToken); !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestClientVerifyRejectsMalformedSuccess(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "non-admin success", want: amoauth.ErrForbidden,
			body: verifiedResponse(now.Add(time.Minute), false),
		},
		{
			name: "expired verification", want: amoauth.ErrUnavailable,
			body: verifiedResponse(now, true),
		},
		{
			name: "unexpected field", want: amoauth.ErrUnavailable,
			body: `{"unexpected":true}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			client, err := NewClient(Config{
				URL: server.URL, ServiceToken: testServiceToken, AppName: "rkrs_activity",
				HTTPClient: server.Client(), Now: func() time.Time { return now },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Verify(context.Background(), testWidgetToken); !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestClientVerifyRequiresServerConfiguration(t *testing.T) {
	client, err := NewClient(Config{URL: "https://widgets.example.test/verify", AppName: "rkrs_activity"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Verify(context.Background(), testWidgetToken); !errors.Is(err, amoauth.ErrNotConfigured) {
		t.Fatalf("error=%v", err)
	}
	if _, err = client.Verify(context.Background(), "short"); !errors.Is(err, amoauth.ErrNotConfigured) {
		t.Fatalf("unconfigured error=%v", err)
	}
}

func verifiedResponse(expiresAt time.Time, isAdmin bool) string {
	return fmt.Sprintf(`{
		"account":{"id":"31355990","name":"ООО Ромашка"},
		"user":{"id":"101","email":"admin@example.com","name":"Иван Иванов"},
		"rights":{"isAdmin":%t,"isOwner":false},
		"token":{"jti":"verified-jti","expiresAt":%q}
	}`, isAdmin, expiresAt.Format(time.RFC3339))
}
