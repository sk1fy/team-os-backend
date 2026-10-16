package taskcontrolbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

func TestReferencesSignedScopeBoundsAndRedirects(t *testing.T) {
	company := uuid.New()
	secret := strings.Repeat("s", 32)
	mode := "good"
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.RequestURI() != "/internal/task-control/accounts/123/references" {
			t.Error("incorrect reference path", r.URL)
		}
		empty := sha256.Sum256(nil)
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(strings.Join([]string{"key", r.Method, r.URL.RequestURI(), company.String(), "123", tc.Product, r.Header.Get("X-Task-Control-Timestamp"), r.Header.Get("X-Task-Control-Nonce"), hex.EncodeToString(empty[:])}, "\n")))
		if r.Header.Get("X-Task-Control-Signature") != hex.EncodeToString(m.Sum(nil)) {
			t.Error("invalid signed path")
		}
		switch mode {
		case "redirect":
			http.Redirect(w, r, "/other", http.StatusFound)
			return
		case "oversize":
			_, _ = w.Write([]byte(strings.Repeat(" ", tc.ReferenceMaxBytes+1)))
			return
		case "denied":
			w.WriteHeader(403)
			return
		}
		now := time.Now().UTC()
		s := tc.ReferenceSnapshot{SchemaVersion: 1, Scope: tc.ReferenceScope{CompanyID: company, AccountID: "123", IntegrationID: uuid.New(), InstallationID: uuid.New(), Generation: 1, Product: tc.Product}, Revision: "sha256:" + strings.Repeat("a", 64), Provenance: tc.ReferenceProvenance{Source: "coreAmocrm", FetchedAt: now, FetchStartedAt: now}, FreshUntil: now.Add(5 * time.Minute), Complete: true, Users: []tc.ReferenceUser{}, Groups: []tc.ReferenceGroup{}, Pipelines: []tc.ReferencePipeline{}, Customers: tc.ReferenceCustomers{Availability: "unsupported", Statuses: []tc.ReferenceCustomerStatus{}, CheckedAt: now}}
		s.Revision, _ = tc.ReferenceRevision(s)
		if mode == "foreign" {
			s.Scope.CompanyID = uuid.New()
		}
		if mode == "partial" {
			s.Complete = false
		}
		if mode == "invalidKind" {
			s.Pipelines = []tc.ReferencePipeline{{ID: "1", Statuses: []tc.ReferenceStatus{{ID: "142", Kind: "open"}}}}
		}
		_ = json.NewEncoder(w).Encode(s)
	}))
	defer server.Close()
	c, e := New(server.URL, "key", secret)
	if e != nil {
		t.Fatal(e)
	}
	c.HTTP = server.Client()
	if _, e = c.References(context.Background(), company, "123"); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"foreign", "partial", "invalidKind", "redirect", "oversize", "denied"} {
		mode = v
		before := calls
		if _, e = c.References(context.Background(), company, "123"); e == nil {
			t.Fatal("accepted", v)
		}
		if v == "foreign" {
			var scoped ReferenceError
			if !errors.As(e, &scoped) || scoped.Code != "references_scope_mismatch" {
				t.Fatal("lost typed scope error", e)
			}
		}
		if calls != before+1 {
			t.Fatal("followed redirect")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.References(ctx, company, "123"); e == nil {
		t.Fatal("ignored cancellation")
	}
}
