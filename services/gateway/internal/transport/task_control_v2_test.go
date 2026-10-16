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
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type dynamicControlClient struct {
	v.CompanyServiceClient
	calls   []*v.TaskControlConnectionRequest
	runtime []bool
}

func (c *dynamicControlClient) TaskControlConnection(ctx context.Context, in *v.TaskControlConnectionRequest, _ ...grpc.CallOption) (*v.TaskControlConnectionResponse, error) {
	c.calls = append(c.calls, in)
	md, _ := metadata.FromOutgoingContext(ctx)
	c.runtime = append(c.runtime, len(md.Get("x-teamos-service-token")) == 1)
	return &v.TaskControlConnectionResponse{JsonBody: []byte(`{"ok":true}`)}, nil
}
func TestTaskControlV2RoutesPreserveIntentAndSeparateRuntimeAuth(t *testing.T) {
	c := &dynamicControlClient{}
	router := api.Handler(&Handler{company: c, companyServiceToken: "test-service-key"})
	id := "a0000000-0000-4000-8000-000000000001"
	base := "/api/v2/task-control/connections/" + id
	for _, tc := range []struct {
		method, path, body string
		action             v.TaskControlConnectionRequest_Action
		runtime            bool
		status             int
	}{
		{"PUT", base + "/draft", `{"expectedRevision":2,"intentRules":[{"kind":"dynamic","rule":{"audience":{"kind":"allLinked"},"leads":{"kind":"allOpen"}}}]}`, v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT_V2, false, 200},
		{"POST", base + "/validate", `{"expectedDraftRevision":2}`, v.TaskControlConnectionRequest_ACTION_VALIDATE_V2, false, 200},
		{"POST", base + "/activate", `{"requestId":"` + id + `"}`, v.TaskControlConnectionRequest_ACTION_ACTIVATE_V2, false, 200},
		{"GET", "/api/v2/task-control/runtime/bootstrap", "", v.TaskControlConnectionRequest_ACTION_BOOTSTRAP_V2, true, 200},
		{"POST", "/api/v2/task-control/runtime/ack", `{"actorId":"7","runtimeRevision":21}`, v.TaskControlConnectionRequest_ACTION_ACK_V2, true, 204},
	} {
		r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, strings.NewReader(tc.body))
		if tc.runtime {
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 43))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		got := c.calls[len(c.calls)-1]
		if got.Action != tc.action || c.runtime[len(c.runtime)-1] != tc.runtime {
			t.Fatal("wrong authorization boundary", got)
		}
		if tc.body != "" {
			var want, actual any
			if json.Unmarshal([]byte(tc.body), &want) != nil || json.Unmarshal(got.JsonBody, &actual) != nil {
				t.Fatal("lost mutation body")
			}
			wb, _ := json.Marshal(want)
			ab, _ := json.Marshal(actual)
			if string(wb) != string(ab) {
				t.Fatal("intent changed", string(ab))
			}
		}
		if tc.status == 204 && w.Body.Len() != 0 {
			t.Fatal("ACK response must be empty")
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("runtime/intent response cacheable")
		}
	}
	before := len(c.calls)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/v2/task-control/runtime/bootstrap"
		if method == http.MethodPost {
			path = "/api/v2/task-control/runtime/ack"
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), method, path, nil))
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if len(c.calls) != before {
		t.Fatal("invalid bearer reached company")
	}
}
