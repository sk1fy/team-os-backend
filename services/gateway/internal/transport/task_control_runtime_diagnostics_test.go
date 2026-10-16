package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestTaskControlDiagnosticRoutesKeepSeparateCredentials(t *testing.T) {
	for _, test := range []struct {
		method, path, body string
		action             v.TaskControlConnectionRequest_Action
		service            bool
	}{
		{http.MethodPost, "/api/v1/task-control/runtime/ack", `{"runtimeVersion":"1.2.2"}`, v.TaskControlConnectionRequest_ACTION_ACK, true},
		{http.MethodPost, "/api/v2/task-control/runtime/ack", `{"runtimeVersion":"1.2.2"}`, v.TaskControlConnectionRequest_ACTION_ACK_V2, true},
		{http.MethodPost, "/api/v1/task-control/runtime/telemetry", `{"event":"received"}`, v.TaskControlConnectionRequest_ACTION_TELEMETRY, true},
		{http.MethodGet, "/api/v1/task-control/connections/a0000000-0000-4000-8000-000000000001/publications/b0000000-0000-4000-8000-000000000001/application", "", v.TaskControlConnectionRequest_ACTION_APPLICATION, false},
	} {
		t.Run(test.path, func(t *testing.T) {
			client := &importControlClient{}
			router := api.Handler(&Handler{company: client, companyServiceToken: "gateway-only-secret"})
			req := httptest.NewRequestWithContext(context.Background(), test.method, test.path, strings.NewReader(test.body))
			token := strings.Repeat("x", 43)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			want := http.StatusOK
			if test.service {
				want = http.StatusNoContent
			}
			if w.Code != want || len(client.calls) != 1 {
				t.Fatalf("%d %s calls %d", w.Code, w.Body.String(), len(client.calls))
			}
			call := client.calls[0]
			if call.Action != test.action || client.service[0] != test.service || (test.service && call.Credential != token) || (!test.service && call.Credential != "") {
				t.Fatal("diagnostic credential crossed identity boundary", call)
			}
			if test.body != "" && string(call.JsonBody) != test.body {
				t.Fatal("event changed in transit")
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("diagnostics must not be cached")
			}
		})
	}
}

func TestTaskControlDiagnosticUploadsRejectOversizeAndAmbiguousJSON(t *testing.T) {
	for _, path := range []string{"/api/v1/task-control/runtime/ack", "/api/v2/task-control/runtime/ack", "/api/v1/task-control/runtime/telemetry"} {
		for _, test := range []struct {
			body, token string
			want        int
		}{
			{`{"event":"received","event":"applied"}`, strings.Repeat("x", 43), http.StatusBadRequest},
			{`{"message":"` + strings.Repeat("x", 8192) + `"}`, strings.Repeat("x", 43), http.StatusRequestEntityTooLarge},
			{`{}`, "jwt.payload.signature", http.StatusUnauthorized},
		} {
			client := &importControlClient{}
			router := api.Handler(&Handler{company: client, companyServiceToken: "gateway-only-secret"})
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer "+test.token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != test.want || len(client.calls) != 0 {
				t.Fatalf("%s: %d %s calls %d", path, w.Code, w.Body.String(), len(client.calls))
			}
		}
	}
}
