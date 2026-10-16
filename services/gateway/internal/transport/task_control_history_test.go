package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestTaskControlHistoryAndRestoreUseAuthenticatedCompanyActor(t *testing.T) {
	const binding = "a0000000-0000-4000-8000-000000000001"
	const body = `{"expectedBindingRevision":3,"expectedModeEpoch":7,"requestId":"a0000000-0000-4000-8000-000000000002","sourcePublicationId":"a0000000-0000-4000-8000-000000000003","expectedPublishedVersion":4,"reason":"restore"}`
	for _, tc := range []struct {
		method, path, body string
		action             v.TaskControlConnectionRequest_Action
	}{
		{"GET", "/api/v1/task-control/connections/" + binding + "/history?cursor=scope-bound&limit=7", "", v.TaskControlConnectionRequest_ACTION_HISTORY},
		{"GET", "/api/v2/task-control/connections/" + binding + "/history?cursor=scope-bound&limit=7", "", v.TaskControlConnectionRequest_ACTION_HISTORY},
		{"GET", "/api/v1/task-control/connections/" + binding + "/publications?cursor=scope-bound&limit=7", "", v.TaskControlConnectionRequest_ACTION_PUBLICATIONS},
		{"POST", "/api/v1/task-control/connections/" + binding + "/rollback", body, v.TaskControlConnectionRequest_ACTION_ROLLBACK},
	} {
		t.Run(tc.path, func(t *testing.T) {
			client := &importControlClient{}
			router := api.Handler(&Handler{company: client, companyServiceToken: "must-not-be-used"})
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 43))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 200 || len(client.calls) != 1 {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			got := client.calls[0]
			if got.Action != tc.action || got.Id != binding || client.service[0] || got.Credential != "" {
				t.Fatal("wrong authorization boundary", got)
			}
			var payload map[string]any
			if err := json.Unmarshal(got.JsonBody, &payload); err != nil {
				t.Fatal(err)
			}
			if tc.method == http.MethodGet {
				if payload["cursor"] != "scope-bound" || payload["limit"] != float64(7) {
					t.Fatal("pagination lost", payload)
				}
			} else if payload["reason"] != "restore" || payload["expectedPublishedVersion"] != float64(4) {
				t.Fatal("restore fences lost", payload)
			}
			if rec.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("history cacheable")
			}
		})
	}
}

func TestTaskControlRestoreRejectsMalformedBodyBeforeRPC(t *testing.T) {
	client := &importControlClient{}
	router := api.Handler(&Handler{company: client})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/task-control/connections/a0000000-0000-4000-8000-000000000001/rollback", strings.NewReader(`{"reason":`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 || len(client.calls) != 0 {
		t.Fatalf("%d %s calls %d", rec.Code, rec.Body.String(), len(client.calls))
	}
}
