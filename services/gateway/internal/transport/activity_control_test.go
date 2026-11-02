package transport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
)

type activityTestClient struct {
	v.CompanyServiceClient
	binding string
	calls   int
	t       *testing.T
}

func (c *activityTestClient) ActivityControl(_ context.Context, in *v.ActivityControlRequest, _ ...grpc.CallOption) (*v.ActivityControlResponse, error) {
	c.calls++
	if in.BindingId != c.binding {
		c.t.Fatalf("lost expected binding: %q", in.BindingId)
	}
	return &v.ActivityControlResponse{JsonBody: []byte(`{}`)}, nil
}
func TestActivityRoutesRequireAndForwardExpectedBinding(t *testing.T) {
	binding := uuid.NewString()
	client := &activityTestClient{binding: binding, t: t}
	router := api.Handler(&Handler{company: client})
	id := uuid.NewString()
	for _, route := range []struct{ method, path string }{{"GET", "panels"}, {"GET", "employees"}, {"GET", "task-panel"}, {"POST", "panels"}, {"PATCH", "panels/" + id}, {"POST", "panels/" + id + "/share-link"}, {"PATCH", "task-panel"}} {
		for _, header := range []string{"", binding} {
			r := httptest.NewRequestWithContext(context.Background(), route.method, "/api/v1/activity-control/"+route.path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", uuid.NewString())
			if header != "" {
				r.Header.Set("X-Activity-Binding-Id", header)
			}
			w := httptest.NewRecorder()
			before := client.calls
			router.ServeHTTP(w, r)
			if header == "" {
				if w.Code != 400 || client.calls != before {
					t.Fatalf("unfenced route %s %s status=%d calls=%d", route.method, route.path, w.Code, client.calls-before)
				}
			} else if w.Code != 200 || client.calls != before+1 {
				t.Fatalf("fenced route %s %s status=%d", route.method, route.path, w.Code)
			}
		}
	}
}
