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

func TestFreshObservationRequiresBoundedExactResponse(t *testing.T) {
	scope := Scope{CompanyID: uuid.New(), BindingID: uuid.New(), BindingRevision: 1, InstallationID: uuid.New(), IntegrationID: uuid.New(), AccountID: "1"}
	reason := "not_found_or_deleted"
	good := LeadObservation{Scope: scope, LeadID: "10", Absent: true, AbsenceReason: &reason, ObservedAt: time.Now().UTC(), ObservationRevision: 1}
	raw, _ := json.Marshal(good)
	for name, body := range map[string]string{"valid": string(raw), "trailing": string(raw) + `{}`, "empty": "", "unknown": strings.TrimSuffix(string(raw), "}") + `,"untrusted":true}`, "oversized": strings.Repeat(" ", 4<<20) + string(raw)} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			c, _ := New(server.URL, "test", strings.Repeat("s", 32))
			c.http = server.Client()
			out, err := c.ReadLead(context.Background(), scope, "10")
			if name == "valid" {
				if err != nil || !out.Absent {
					t.Fatalf("%+v %v", out, err)
				}
			} else if err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	good.Scope.BindingRevision = 2
	raw, _ = json.Marshal(good)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) }))
	defer server.Close()
	c, _ := New(server.URL, "test", strings.Repeat("s", 32))
	c.http = server.Client()
	if _, e := c.ReadLead(context.Background(), scope, "10"); e == nil {
		t.Fatal("immutable scope mismatch accepted")
	}
}
