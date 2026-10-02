package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeClient struct {
	v.CompanyServiceClient
	read  func(*v.ReadDistributionRuntimeRequest) (*v.ReadDistributionRuntimeResponse, error)
	write func(*v.WriteDistributionRuntimeRequest) (*v.WriteDistributionRuntimeResponse, error)
}

func (c runtimeClient) ReadDistributionRuntime(_ context.Context, r *v.ReadDistributionRuntimeRequest, _ ...grpc.CallOption) (*v.ReadDistributionRuntimeResponse, error) {
	return c.read(r)
}
func (c runtimeClient) WriteDistributionRuntime(_ context.Context, r *v.WriteDistributionRuntimeRequest, _ ...grpc.CallOption) (*v.WriteDistributionRuntimeResponse, error) {
	return c.write(r)
}
func TestRuntimeQueuePaginationAndScopedRPC(t *testing.T) {
	calls := 0
	h := &Handler{company: runtimeClient{read: func(r *v.ReadDistributionRuntimeRequest) (*v.ReadDistributionRuntimeResponse, error) {
		calls++
		if r.Kind != "queue" || r.Id != "" || r.Limit != 50 || r.Offset != 0 {
			t.Fatalf("request %#v", r)
		}
		return &v.ReadDistributionRuntimeResponse{Payload: []byte(`{"items":[],"limit":50,"offset":0}`)}, nil
	}}}
	w := httptest.NewRecorder()
	h.GetDistributionQueue(w, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil), api.GetDistributionQueueParams{})
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	l := int32(101)
	w = httptest.NewRecorder()
	h.GetDistributionQueue(w, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil), api.GetDistributionQueueParams{Limit: &l})
	if w.Code != 400 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
}
func TestRuntimeRuleUpdateRequiresExplicitKeepAndPreservesFalse(t *testing.T) {
	id := uuid.New()
	calls := 0
	h := &Handler{company: runtimeClient{write: func(r *v.WriteDistributionRuntimeRequest) (*v.WriteDistributionRuntimeResponse, error) {
		calls++
		if r.Kind != "rule" || r.Id != id.String() {
			t.Fatal(r)
		}
		var in map[string]any
		if json.Unmarshal(r.Payload, &in) != nil || in["keepCurrentResponsible"] != false || in["active"] != false {
			t.Fatal(string(r.Payload))
		}
		return nil, status.Error(codes.PermissionDenied, "Недостаточно прав")
	}}}
	for _, body := range []string{`{"expectedRevision":1,"active":false}`, `{"expectedRevision":1,"active":false,"keepCurrentResponsible":null}`, `{"expectedRevision":1,"active":false,"keepCurrentResponsible":false,"extra":1}`} {
		w := httptest.NewRecorder()
		h.UpdateDistributionRule(w, httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/", strings.NewReader(body)), id)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.UpdateDistributionRule(w, httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/", strings.NewReader(`{"expectedRevision":1,"active":false,"keepCurrentResponsible":false}`)), id)
	if w.Code != 403 || calls != 1 {
		t.Fatal(w.Code, calls, w.Body.String())
	}
}
func TestRuntimeMalformedServiceResponseFailsClosed(t *testing.T) {
	for _, raw := range []string{`null`, `{`, `{"timezone":42,"revision":1}`} {
		w := httptest.NewRecorder()
		runtimeOutput(w, &v.ReadDistributionRuntimeResponse{Payload: []byte(raw)}, &api.DistributionRuntimeSettings{})
		if w.Code != 502 {
			t.Fatal(raw, w.Code, w.Body.String())
		}
	}
}

func TestRuntimeHistoryNeverSerializesPrivateCommandOrFutureFields(t *testing.T) {
	w := httptest.NewRecorder()
	raw := `{"items":[{"id":1,"state":"dispatching","reason":"decision_ready","createdAt":"2026-10-02T00:00:00Z","payload":{"leadId":"123","actor":{"crmUserId":"456"},"futureSecret":"private-evidence"}}],"limit":50,"offset":0}`
	runtimeOutput(w, &v.ReadDistributionRuntimeResponse{Payload: []byte(raw)}, &api.DistributionRuntimeHistory{})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"payload":{}`) || strings.Contains(w.Body.String(), "123") || strings.Contains(w.Body.String(), "456") || strings.Contains(w.Body.String(), "private-evidence") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestRuntimeQueuePreservesPermissionRedaction(t *testing.T) {
	id := uuid.NewString()
	w := httptest.NewRecorder()
	raw := `{"items":[{"id":"` + id + `","entryId":"` + id + `","ruleId":"` + id + `","groupId":"` + id + `","accountId":"1","leadId":null,"state":"waiting","reason":"outside_shift","nextAttemptAt":"2026-10-02T00:00:00Z","operationId":null,"plannedEmployeeId":null,"plannedAt":null,"createdAt":"2026-10-01T00:00:00Z"}],"limit":50,"offset":0}`
	runtimeOutput(w, &v.ReadDistributionRuntimeResponse{Payload: []byte(raw)}, &api.DistributionRuntimeQueue{})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"leadId":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
