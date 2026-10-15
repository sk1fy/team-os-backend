package transport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type taskControlClient struct {
	v.CompanyServiceClient
	calls int
	t     *testing.T
}

func (c *taskControlClient) TaskControlConnection(ctx context.Context, r *v.TaskControlConnectionRequest, _ ...grpc.CallOption) (*v.TaskControlConnectionResponse, error) {
	c.calls++
	md, _ := metadata.FromOutgoingContext(ctx)
	if strings.Join(md.Get("x-teamos-service"), "") != "task-control" || strings.Join(md.Get("x-teamos-service-token"), "") != "backend-service-key" {
		c.t.Fatal("missing internal service auth")
	}
	if r.Action == v.TaskControlConnectionRequest_ACTION_EXCHANGE && r.Credential != "signed-core-assertion" {
		c.t.Fatal("wrong credential")
	}
	return &v.TaskControlConnectionResponse{JsonBody: []byte(`{"state":"active"}`)}, nil
}
func TestTaskControlRuntimeBoundary(t *testing.T) {
	client := &taskControlClient{t: t}
	router := api.Handler(&Handler{company: client, companyServiceToken: "backend-service-key"})
	for _, test := range []struct {
		method, path, body, auth string
		status                   int
	}{
		{"POST", "/api/v1/widget/task-control/sessions", `{"coreAssertion":"signed-core-assertion"}`, "", 200},
		{"POST", "/api/v1/widget/task-control/sessions", `{"coreAssertion":"signed-core-assertion","companyId":"forged"}`, "", 400},
		{"GET", "/api/v1/widget/task-control/bootstrap", "", "", 401},
		{"GET", "/api/v1/widget/task-control/bootstrap", "", "Bearer " + strings.Repeat("x", 43), 200},
	} {
		r := httptest.NewRequestWithContext(context.Background(), test.method, test.path, strings.NewReader(test.body))
		if test.auth != "" {
			r.Header.Set("Authorization", test.auth)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.path, w.Code, w.Body.String())
		}
	}
	if client.calls != 2 {
		t.Fatalf("unvalidated input reached company: %d", client.calls)
	}
}
