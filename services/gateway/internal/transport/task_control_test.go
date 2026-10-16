package transport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

type taskControlGuardClient struct {
	v.CompanyServiceClient
	denied     bool
	operations []string
}

func (c *taskControlGuardClient) TaskControlConnection(_ context.Context, r *v.TaskControlConnectionRequest, _ ...grpc.CallOption) (*v.TaskControlConnectionResponse, error) {
	if r.Action != v.TaskControlConnectionRequest_ACTION_AUTHORIZE || r.Id == "" || r.Operation == "" {
		return nil, status.Error(codes.Internal, "bad guard")
	}
	c.operations = append(c.operations, r.Operation)
	if c.denied {
		return nil, status.Error(codes.PermissionDenied, "denied")
	}
	return &v.TaskControlConnectionResponse{JsonBody: []byte(`{"authorized":true}`)}, nil
}

func TestTaskControlRoutesDoNotClaimUnimplementedWritesSucceeded(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	client := &taskControlGuardClient{}
	router := api.Handler(&Handler{company: client})
	param := regexp.MustCompile(`\{[^}]+\}`)
	count := 0
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/api/v1/task-control/") {
			continue
		}
		for method, operation := range item.Operations() {
			if operation.Responses.Value("501") == nil {
				continue
			}
			count++
			t.Run(method+path, func(t *testing.T) {
				req := httptest.NewRequestWithContext(context.Background(), method, param.ReplaceAllString(path, "a0000000-0000-4000-8000-000000000001"), nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != 501 {
					t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
				var body struct {
					Error struct {
						Message string
						Status  int
						Code    string
					}
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Status != 501 || body.Error.Code != "task_control_not_implemented" || !strings.Contains(body.Error.Message, "ещё не реализован") {
					t.Fatalf("invalid error: %s", rec.Body.String())
				}
				client.denied = true
				denied := httptest.NewRecorder()
				router.ServeHTTP(denied, req)
				client.denied = false
				if denied.Code != 403 {
					t.Fatalf("guard bypass: %d", denied.Code)
				}
			})
		}
	}
	if count != 9 {
		t.Fatalf("checked %d operations, expected 9", count)
	}
	if len(client.operations) != 18 {
		t.Fatalf("missing guards: %v", client.operations)
	}
}
