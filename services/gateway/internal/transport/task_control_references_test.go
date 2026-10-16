package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
)

type taskControlReferenceClient struct {
	v.CompanyServiceClient
	request *v.TaskControlConnectionRequest
}

func (c *taskControlReferenceClient) TaskControlConnection(_ context.Context, in *v.TaskControlConnectionRequest, _ ...grpc.CallOption) (*v.TaskControlConnectionResponse, error) {
	c.request = in
	return &v.TaskControlConnectionResponse{JsonBody: []byte(`{"state":"pending"}`)}, nil
}
func TestTaskControlReferenceRoutesUseActorRPCAndAsyncRefreshStatus(t *testing.T) {
	client := &taskControlReferenceClient{}
	router := api.Handler(&Handler{company: client})
	binding, selected, department, refresh := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	prefix := "/api/v2/task-control/connections/" + binding + "/references"
	for _, test := range []struct {
		method, path string
		action       v.TaskControlConnectionRequest_Action
		status       int
	}{
		{http.MethodGet, prefix + "?search=employee&limit=10&offset=20&departmentId=" + department + "&selectedEmployeeIds=" + selected, v.TaskControlConnectionRequest_ACTION_REFERENCES, 200},
		{http.MethodPost, prefix + "/refresh", v.TaskControlConnectionRequest_ACTION_REFRESH_REFERENCES, 202},
		{http.MethodGet, prefix + "/refresh/" + refresh, v.TaskControlConnectionRequest_ACTION_REFERENCES_REFRESH, 200},
		{http.MethodGet, "/api/v1/task-control/connections/" + binding + "/references", v.TaskControlConnectionRequest_ACTION_REFERENCES, 200},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), test.method, test.path, nil))
		if response.Code != test.status || client.request.Action != test.action || client.request.Id != binding {
			t.Fatalf("%s: status%d request%+v", test.path, response.Code, client.request)
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("reference response cached")
		}
		if client.request.Credential != "" {
			t.Fatal("browser credential used for management")
		}
		if test.path == prefix+"/refresh/"+refresh && client.request.AccountId != refresh {
			t.Fatal("refresh identity lost")
		}
		if len(test.path) >= len(prefix) && test.path[0:len(prefix)] == prefix && test.method == http.MethodGet && len(client.request.JsonBody) > 0 {
			var options map[string]any
			if err := json.Unmarshal(client.request.JsonBody, &options); err != nil {
				t.Fatal(err)
			}
			if options["Offset"] != float64(20) || options["Limit"] != float64(10) || options["DepartmentID"] != department || options["Search"] != "employee" {
				t.Fatal(options)
			}
		}
	}
}
