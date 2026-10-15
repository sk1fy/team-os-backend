package taskcontrolbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

func TestSignedStatusScopeAndRedirects(t *testing.T) {
	company, install, integration := uuid.New(), uuid.New(), uuid.New()
	secret := strings.Repeat("s", 32)
	mode := "good"
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if mode == "redirect" {
			http.Redirect(w, r, "/other", http.StatusFound)
			return
		}
		empty := sha256.Sum256(nil)
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(strings.Join([]string{"key", r.Method, r.URL.RequestURI(), company.String(), "123", tc.Product, r.Header.Get("X-Task-Control-Timestamp"), r.Header.Get("X-Task-Control-Nonce"), hex.EncodeToString(empty[:])}, "\n")))
		if r.Header.Get("X-Task-Control-Signature") != hex.EncodeToString(m.Sum(nil)) {
			t.Error("signature mismatch")
		}
		now := time.Now()
		out := tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &install, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute)}
		if mode == "foreign" {
			out.CompanyID = uuid.New()
		}
		if mode == "stale" {
			out.FreshUntil = now.Add(-time.Minute)
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, e := New(server.URL, "key", secret)
	if e != nil {
		t.Fatal(e)
	}
	client.HTTP = server.Client()
	if _, e = client.Installation(context.Background(), company, "123"); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"foreign", "stale", "redirect"} {
		mode = v
		before := calls
		if _, e = client.Installation(context.Background(), company, "123"); e == nil {
			t.Fatal("accepted", v)
		}
		if calls != before+1 {
			t.Fatal("redirect followed")
		}
	}
}
