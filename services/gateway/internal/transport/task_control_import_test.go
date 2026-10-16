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

type importControlClient struct {
	v.CompanyServiceClient
	calls   []*v.TaskControlConnectionRequest
	service []bool
}

func (c *importControlClient) TaskControlConnection(ctx context.Context, in *v.TaskControlConnectionRequest, _ ...grpc.CallOption) (*v.TaskControlConnectionResponse, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	c.calls = append(c.calls, in)
	c.service = append(c.service, len(md.Get("x-teamos-service-token")) == 1)
	return &v.TaskControlConnectionResponse{JsonBody: []byte(`{"state":"pending"}`)}, nil
}
func TestTaskControlImportRoutesSeparateAdminBridgeAndTeamOSActor(t *testing.T) {
	c := &importControlClient{}
	router := api.Handler(&Handler{company: c, companyServiceToken: "test-key"})
	binding := "a0000000-0000-4000-8000-000000000001"
	id := "b0000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		method, path, body, proof, operation string
		action                               v.TaskControlConnectionRequest_Action
		service                              bool
	}{
		{"POST", "/api/v1/task-control/connections/" + binding + "/source-requests", `{"kind":"catalog","catalogId":"77"}`, "", "", v.TaskControlConnectionRequest_ACTION_SOURCE_CREATE, false},
		{"GET", "/api/v1/task-control/connections/" + binding + "/source-requests/" + id, "", "", "", v.TaskControlConnectionRequest_ACTION_SOURCE_GET, false},
		{"POST", "/api/v1/task-control/connections/" + binding + "/imports", `{"sourceSnapshotId":"` + id + `"}`, "", "", v.TaskControlConnectionRequest_ACTION_IMPORT_PREVIEW, false},
		{"POST", "/api/v2/task-control/connections/" + binding + "/imports", `{"sourceSnapshotId":"` + id + `"}`, "", "v2", v.TaskControlConnectionRequest_ACTION_IMPORT_PREVIEW, false},
		{"POST", "/api/v2/task-control/connections/" + binding + "/imports/" + id + "/apply", `{"acknowledgeDiscrepancies":true}`, "", "v2", v.TaskControlConnectionRequest_ACTION_IMPORT_APPLY, false},
		{"GET", "/api/v1/widget/task-control/source-requests", "", "key.payload.signature", "", v.TaskControlConnectionRequest_ACTION_SOURCE_PENDING, true},
		{"PUT", "/api/v1/widget/task-control/source-requests/" + id + "/result", `{"coreAssertion":"key.payload.signature","complete":false}`, "key.payload.signature", "", v.TaskControlConnectionRequest_ACTION_SOURCE_SUBMIT, true},
		{"GET", "/api/v1/widget/task-control/connection", "", strings.Repeat("x", 43), "", v.TaskControlConnectionRequest_ACTION_RUNTIME_CONNECTION, true},
	} {
		r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, strings.NewReader(tc.body))
		if tc.proof != "" {
			r.Header.Set("Authorization", "Bearer "+tc.proof)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s %d %s", tc.path, w.Code, w.Body.String())
		}
		in := c.calls[len(c.calls)-1]
		if in.Action != tc.action || in.Operation != tc.operation || in.Credential != tc.proof || c.service[len(c.service)-1] != tc.service {
			t.Fatal("wrong identity boundary", in)
		}
		if tc.body != "" {
			var got, want any
			if json.Unmarshal(in.JsonBody, &got) != nil || json.Unmarshal([]byte(tc.body), &want) != nil {
				t.Fatal("lost source body")
			}
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(want)
			if string(a) != string(b) {
				t.Fatal("import content changed")
			}
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("source response cacheable")
		}
	}
	calls := len(c.calls)
	for _, proof := range []string{"", strings.Repeat("x", 43), "Bearer malformed"} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/widget/task-control/source-requests", nil)
		if proof != "" {
			r.Header.Set("Authorization", "Bearer "+proof)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("runtime bearer entered admin bridge", w.Code)
		}
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/widget/task-control/source-requests/"+id+"/result", strings.NewReader(`{"raw":"`+strings.Repeat("x", 1<<20)+`"}`))
	r.Header.Set("Authorization", "Bearer key.payload.signature")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal("unbounded upload", w.Code)
	}
	r = httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/widget/task-control/source-requests/"+id+"/result", strings.NewReader(`{"complete":true,"complete":false}`))
	r.Header.Set("Authorization", "Bearer key.payload.signature")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("duplicate bridge keys accepted", w.Code)
	}
	if len(c.calls) != calls {
		t.Fatal("invalid bridge input reached company")
	}
}
