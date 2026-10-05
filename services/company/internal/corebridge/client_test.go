package corebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignedBridgeScopeAndDecimalIDs(t *testing.T) {
	now := time.Unix(1790935200, 0)
	scope := Scope{CompanyID: uuid.New(), BindingID: uuid.New(), BindingRevision: 1, InstallationID: uuid.New(), IntegrationID: uuid.New(), AccountID: "9007199254740993"}
	secret := strings.Repeat("s", 32)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Distribution-Key-Id") != "old" || r.Header.Get("X-Distribution-Company") != scope.CompanyID.String() {
			t.Error("missing scope")
		}
		if r.Header.Get("X-Distribution-Signature") != Signature("old", secret, r.Method, r.URL.RequestURI(), scope.CompanyID.String(), scope.InstallationID.String(), r.Header.Get("X-Distribution-Timestamp"), r.Header.Get("X-Distribution-Nonce"), nil) {
			t.Error("invalid signature")
		}
		_ = json.NewEncoder(w).Encode(Binding{Scope: scope, State: "active"})
	}))
	defer server.Close()
	c, e := New(server.URL, "old", secret)
	if e != nil {
		t.Fatal(e)
	}
	c.http = server.Client()
	c.now = func() time.Time { return now }
	out, e := c.GetBinding(context.Background(), scope)
	if e != nil || out.Scope != scope {
		t.Fatalf("%+v %v", out, e)
	}
	if Signature("new", secret, "GET", "/", scope.CompanyID.String(), scope.InstallationID.String(), "1", "n", nil) == Signature("old", secret, "GET", "/", scope.CompanyID.String(), scope.InstallationID.String(), "1", "n", nil) {
		t.Fatal("key ID is unsigned")
	}
}
func TestBridgeRejectsInsecureConfigAndDoesNotExposeErrorBody(t *testing.T) {
	for _, u := range []string{"http://localhost", "https://user:pass@example.com", "https://example.com/?token=secret", "https://example.com/#fragment"} {
		if _, e := New(u, "k", strings.Repeat("s", 32)); e == nil {
			t.Fatalf("accepted %s", u)
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("sensitive OAuth body"))
	}))
	defer server.Close()
	c, _ := New(server.URL, "k", strings.Repeat("s", 32))
	c.http = server.Client()
	_, e := c.GetBinding(context.Background(), Scope{CompanyID: uuid.New(), InstallationID: uuid.New(), BindingID: uuid.New()})
	if e == nil || strings.Contains(e.Error(), "sensitive") {
		t.Fatalf("error leaked: %v", e)
	}
}

func TestMutationAcknowledgementsCannotBeEmptyOrAsynchronous(t *testing.T) {
	scope := Scope{CompanyID: uuid.New(), BindingID: uuid.New(), BindingRevision: 1, InstallationID: uuid.New(), IntegrationID: uuid.New(), AccountID: "1"}
	mapping := []Mapping{{EmployeeID: uuid.New(), UserID: "2"}}
	for _, tc := range []struct {
		status int
		body   string
	}{{202, `{"state":"synced","count":1,"mappingRevision":3}`}, {204, ""}, {200, ""}, {200, `{"state":"synced","count":1,"mappingRevision":2}`}, {200, `{"state":"revoked","bindingId":"00000000-0000-0000-0000-000000000000"}`}} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c, _ := New(server.URL, "k", strings.Repeat("s", 32))
		c.http = server.Client()
		if e := c.Mappings(context.Background(), scope, 3, mapping); e == nil {
			t.Fatalf("accepted mapping status %d %s", tc.status, tc.body)
		}
		if e := c.Revoke(context.Background(), scope); e == nil {
			t.Fatalf("accepted revoke status %d %s", tc.status, tc.body)
		}
		server.Close()
	}
}
