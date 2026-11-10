package companyentrybridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

func TestSubjectCheckHTTPContractAndFailures(t *testing.T) {
	subject := application.AmoEntrySubject{AccountID: "123", ActorID: "7", IntegrationID: "49c1494b-3260-4236-a7fc-1069c414758a", InstallationID: "66424148-9e49-48ad-a9ce-f7985fe8ff68"}
	key := strings.Repeat("k", 32)
	for _, test := range []struct {
		name    string
		status  int
		body    string
		allowed bool
		failed  bool
	}{
		{"allowed", 200, `{"allowed":true}`, true, false},
		{"denied", 403, `{"error":{"message":"нет доступа"}}`, false, false},
		{"outage", 503, `{}`, false, true},
		{"bad service credential", 401, `{}`, false, true},
		{"malformed", 200, `{`, false, true},
		{"unknown payload", 200, `{"allowed":true,"secret":"x"}`, false, true},
		{"trailing", 200, `{"allowed":true}{}`, false, true},
		{"oversize", 200, strings.Repeat(" ", 4096) + `{"allowed":true}`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/v1/company-entry/subject-check" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Service "+key {
					t.Error("incorrect service contract")
				}
				raw, _ := io.ReadAll(r.Body)
				var decoded application.AmoEntrySubject
				if json.Unmarshal(raw, &decoded) != nil || decoded != subject {
					t.Error("subject was modified")
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, err := New(server.URL+"/internal/v1/company-entry/subject-check", key)
			if err != nil {
				t.Fatal(err)
			}
			client.http.Transport = server.Client().Transport
			allowed, err := client.Check(t.Context(), subject)
			if allowed != test.allowed || (err != nil) != test.failed {
				t.Fatalf("result allowed=%v err=%v", allowed, err)
			}
		})
	}
}

func TestSubjectCheckDoesNotRedirectOrOutliveContext(t *testing.T) {
	key := strings.Repeat("k", 32)
	var redirected atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		_, _ = io.WriteString(w, `{"allowed":true}`)
	}))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client, err := New(origin.URL+"/internal/v1/company-entry/subject-check", key)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = origin.Client().Transport
	if _, err = client.Check(t.Context(), application.AmoEntrySubject{}); err == nil || redirected.Load() {
		t.Fatal("callback followed redirect")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = client.Check(ctx, application.AmoEntrySubject{}); err == nil {
		t.Fatal("callback ignored context")
	}
	if client.http.Timeout != 5*time.Second {
		t.Fatal("missing bounded callback timeout")
	}
}

func TestSubjectCheckConfiguration(t *testing.T) {
	key := strings.Repeat("k", 32)
	for _, endpoint := range []string{"http://example.test/internal/v1/company-entry/subject-check", "https://example.test/wrong", "https://user@example.test/internal/v1/company-entry/subject-check", "https://example.test/internal/v1/company-entry/subject-check?secret=x"} {
		if _, err := New(endpoint, key); err == nil {
			t.Fatal("unsafe callback URL accepted")
		}
	}
	for _, badKey := range []string{"", strings.Repeat("k", 31), strings.Repeat("k", 32) + " "} {
		if _, err := New("https://example.test/internal/v1/company-entry/subject-check", badKey); err == nil {
			t.Fatal("unsafe callback key accepted")
		}
	}
}
