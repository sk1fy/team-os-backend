package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type amoEntryCompanyServer struct {
	companyv1.UnimplementedCompanyServiceServer
	calls              atomic.Int32
	profileUnavailable bool
}

func (s *amoEntryCompanyServer) GetAmoEntryCompanyStatus(ctx context.Context, r *companyv1.GetAmoEntryCompanyStatusRequest) (*companyv1.GetAmoEntryCompanyStatusResponse, error) {
	s.calls.Add(1)
	if r.AccountId != "424242" {
		panic("incorrect account")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	if md.Get("x-teamos-service-token")[0] != testCompanyServiceToken || md.Get("x-teamos-service")[0] != "provisioning" {
		panic("missing internal service credential")
	}
	return &companyv1.GetAmoEntryCompanyStatusResponse{State: "absent"}, nil
}
func (s *amoEntryCompanyServer) ProvisionAmoWidgetEntry(ctx context.Context, r *companyv1.ProvisionAmoWidgetEntryRequest) (*companyv1.ProvisionAmoWidgetEntryResponse, error) {
	s.calls.Add(1)
	if s.profileUnavailable {
		value, _ := status.New(codes.InvalidArgument, "untrusted message contains secret").WithDetails(&errdetails.ErrorInfo{Domain: "teamos.company", Reason: "AMO_PROFILE_UNAVAILABLE"})
		return nil, value.Err()
	}

	return &companyv1.ProvisionAmoWidgetEntryResponse{CompanyId: uuid.NewString(), UserId: uuid.NewString(), Code: "one-time-code", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}
func (s *amoEntryCompanyServer) ExchangeAmoCompanyEntry(ctx context.Context, r *companyv1.ExchangeAmoCompanyEntryRequest) (*companyv1.ExchangeAmoCompanyEntryResponse, error) {
	s.calls.Add(1)
	return &companyv1.ExchangeAmoCompanyEntryResponse{Session: testAuthSession("new-access", "new-refresh")}, nil
}

func TestAmoCompanyEntryServiceAuthentication(t *testing.T) {
	server := &amoEntryCompanyServer{}
	handler := newTestGateway(t, server, true)
	bodies := map[string]string{
		"/api/v1/provisioning/amocrm/company-status": `{"accountId":"424242"}`,
		"/api/v1/provisioning/amocrm/widget-entries": `{"accountId":"424242","actorId":"7","integrationId":"465c9141-f733-4e72-b557-6a35fc769bd5","installationId":"513555b7-50d3-4b7d-b8d6-d0529aed82ed","intent":"create","name":"Администратор","email":"admin@example.test"}`,
	}
	for path, body := range bodies {
		for _, credential := range []string{"", "Service wrong", "Bearer " + testProvisioningServiceToken} {
			before := server.calls.Load()
			response := performProvisioningRequest(handler, http.MethodPost, path, body, credential)
			if response.Code != http.StatusUnauthorized || server.calls.Load() != before {
				t.Fatalf("unauthorized boundary status %d calls %d", response.Code, server.calls.Load()-before)
			}
		}
		duplicate := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
		duplicate.Header.Add("Authorization", "Service "+testProvisioningServiceToken)
		duplicate.Header.Add("Authorization", "Service wrong")
		denied := httptest.NewRecorder()
		before := server.calls.Load()
		handler.ServeHTTP(denied, duplicate)
		if denied.Code != http.StatusUnauthorized || server.calls.Load() != before {
			t.Fatal("duplicate authorization headers accepted")
		}
		response := performProvisioningRequest(handler, http.MethodPost, path, body, "Service "+testProvisioningServiceToken)
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("authorized boundary %d %s", response.Code, response.Body.String())
		}
		for _, field := range []string{`"companyId":"` + uuid.NewString() + `"`, `"desiredRole":"owner"`, `"isAdmin":true`} {
			malicious := strings.TrimSuffix(body, "}") + "," + field + "}"
			before := server.calls.Load()
			response = performProvisioningRequest(handler, http.MethodPost, path, malicious, "Service "+testProvisioningServiceToken)
			if response.Code != http.StatusBadRequest || server.calls.Load() != before {
				t.Fatalf("extra browser field accepted %d", response.Code)
			}
		}
	}
}

func TestAmoCompanyEntryExchangeCreatesExistingSessionCookies(t *testing.T) {
	server := &amoEntryCompanyServer{}
	handler := newTestGateway(t, server)
	response := performRequest(handler, http.MethodPost, "/api/v1/auth/amocrm/entry-exchange", `{"code":"one-time-code"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("exchange %d %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["accessToken"] != "new-access" || result["sessionId"] != testFamilyID {
		t.Fatalf("session result %v", result)
	}
	if strings.Contains(response.Body.String(), "new-refresh") || strings.Contains(response.Body.String(), "one-time-code") {
		t.Fatal("response leaked credential")
	}
	refreshFound := false
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == refreshCookieName+"_"+testFamilyID {
			refreshFound = cookie.HttpOnly && cookie.Secure && cookie.Value == "new-refresh"
		}
	}
	if !refreshFound {
		t.Fatal("missing secure refresh family cookie")
	}
	response = performRequest(handler, http.MethodPost, "/api/v1/auth/amocrm/entry-exchange", `{"code":"x","actorId":"7"}`, nil)
	if response.Code != http.StatusBadRequest || server.calls.Load() != 1 {
		t.Fatal("exchange accepted principal injection")
	}
}

func TestAmoCompanyEntryProfileUnavailableIsActionable(t *testing.T) {
	server := &amoEntryCompanyServer{profileUnavailable: true}
	response := performProvisioningRequest(newTestGateway(t, server), http.MethodPost, "/api/v1/provisioning/amocrm/widget-entries", `{"accountId":"123","actorId":"7","integrationId":"465c9141-f733-4e72-b557-6a35fc769bd5","installationId":"513555b7-50d3-4b7d-b8d6-d0529aed82ed","intent":"create","name":"Администратор","email":""}`, "Service "+testProvisioningServiceToken)
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response) != "AMO_PROFILE_UNAVAILABLE" || !strings.Contains(response.Body.String(), "email") || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("unsafe profile error %d %s", response.Code, response.Body.String())
	}
}
