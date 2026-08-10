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

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type provisioningCompanyServer struct {
	companyv1.UnimplementedCompanyServiceServer

	checkAmoAccountFn                  func(context.Context, *companyv1.CheckAmoAccountRequest) (*companyv1.CheckAmoAccountResponse, error)
	issueCompanyRegistrationTokenFn    func(context.Context, *companyv1.IssueCompanyRegistrationTokenRequest) (*companyv1.IssueCompanyRegistrationTokenResponse, error)
	validateCompanyRegistrationTokenFn func(context.Context, *companyv1.ValidateCompanyRegistrationTokenRequest) (*companyv1.ValidateCompanyRegistrationTokenResponse, error)
}

func (s *provisioningCompanyServer) CheckAmoAccount(ctx context.Context, request *companyv1.CheckAmoAccountRequest) (*companyv1.CheckAmoAccountResponse, error) {
	if s.checkAmoAccountFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected CheckAmoAccount call")
	}
	return s.checkAmoAccountFn(ctx, request)
}

func (s *provisioningCompanyServer) IssueCompanyRegistrationToken(ctx context.Context, request *companyv1.IssueCompanyRegistrationTokenRequest) (*companyv1.IssueCompanyRegistrationTokenResponse, error) {
	if s.issueCompanyRegistrationTokenFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected IssueCompanyRegistrationToken call")
	}
	return s.issueCompanyRegistrationTokenFn(ctx, request)
}

func (s *provisioningCompanyServer) ValidateCompanyRegistrationToken(ctx context.Context, request *companyv1.ValidateCompanyRegistrationTokenRequest) (*companyv1.ValidateCompanyRegistrationTokenResponse, error) {
	if s.validateCompanyRegistrationTokenFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ValidateCompanyRegistrationToken call")
	}
	return s.validateCompanyRegistrationTokenFn(ctx, request)
}

func TestCheckAmoAccountIsPublicAndForwardsConfiguredProvider(t *testing.T) {
	server := &provisioningCompanyServer{checkAmoAccountFn: func(_ context.Context, request *companyv1.CheckAmoAccountRequest) (*companyv1.CheckAmoAccountResponse, error) {
		if request.Provider != testProvisioningProvider || request.ExternalAccountId != "31355990" {
			t.Fatalf("request = %#v", request)
		}
		return &companyv1.CheckAmoAccountResponse{Exists: true}, nil
	}}
	recorder := performRequest(newTestGateway(t, server), http.MethodGet, "/api/v1/public/amocrm/accounts/31355990/exists", "", nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"exists":true`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestIssueCompanyRegistrationTokenRequiresServiceAuth(t *testing.T) {
	var calls atomic.Int32
	server := &provisioningCompanyServer{issueCompanyRegistrationTokenFn: func(ctx context.Context, request *companyv1.IssueCompanyRegistrationTokenRequest) (*companyv1.IssueCompanyRegistrationTokenResponse, error) {
		calls.Add(1)
		if request.Provider != testProvisioningProvider || request.ExternalAccountId != "31355990" {
			t.Fatalf("request = %#v", request)
		}
		incoming, _ := metadata.FromIncomingContext(ctx)
		if values := incoming.Get("x-teamos-service"); len(values) != 1 || values[0] != provisioningServiceMarker {
			t.Fatalf("service metadata = %v", values)
		}
		return &companyv1.IssueCompanyRegistrationTokenResponse{
			Token:     "registration-token-abcdefghijklmnopqrstuvwxyz",
			ExpiresAt: timestamppb.New(time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)),
		}, nil
	}}
	handler := newTestGateway(t, server)
	unauthorized := performProvisioningRequest(handler, http.MethodPost, "/api/v1/provisioning/amocrm/registration-tokens", `{"amoAccountId":"31355990"}`, "")
	if unauthorized.Code != http.StatusUnauthorized || responseErrorCode(t, unauthorized) != "SERVICE_AUTH_INVALID" || calls.Load() != 0 {
		t.Fatalf("unauthorized status=%d calls=%d body=%s", unauthorized.Code, calls.Load(), unauthorized.Body.String())
	}
	authorized := performProvisioningRequest(handler, http.MethodPost, "/api/v1/provisioning/amocrm/registration-tokens", `{"amoAccountId":"31355990"}`, "Service "+testProvisioningServiceToken)
	if authorized.Code != http.StatusCreated || calls.Load() != 1 || !strings.Contains(authorized.Body.String(), `"registrationToken"`) {
		t.Fatalf("authorized status=%d calls=%d body=%s", authorized.Code, calls.Load(), authorized.Body.String())
	}
}

func TestIssueCompanyRegistrationTokenAllowsMissingServiceAuthWhenEnabled(t *testing.T) {
	var calls atomic.Int32
	server := &provisioningCompanyServer{issueCompanyRegistrationTokenFn: func(_ context.Context, request *companyv1.IssueCompanyRegistrationTokenRequest) (*companyv1.IssueCompanyRegistrationTokenResponse, error) {
		calls.Add(1)
		if request.Provider != testProvisioningProvider || request.ExternalAccountId != "31355990" {
			t.Fatalf("request = %#v", request)
		}
		return &companyv1.IssueCompanyRegistrationTokenResponse{
			Token:     "registration-token-abcdefghijklmnopqrstuvwxyz",
			ExpiresAt: timestamppb.New(time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)),
		}, nil
	}}
	handler := newTestGateway(t, server, true)
	recorder := performProvisioningRequest(handler, http.MethodPost, "/api/v1/provisioning/amocrm/registration-tokens", `{"amoAccountId":"31355990"}`, "")
	if recorder.Code != http.StatusCreated || calls.Load() != 1 || !strings.Contains(recorder.Body.String(), `"registrationToken"`) {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls.Load(), recorder.Body.String())
	}
}

func TestValidateCompanyRegistrationTokenIsPublicAndNoStore(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	server := &provisioningCompanyServer{validateCompanyRegistrationTokenFn: func(_ context.Context, request *companyv1.ValidateCompanyRegistrationTokenRequest) (*companyv1.ValidateCompanyRegistrationTokenResponse, error) {
		if request.Token != "registration-token" {
			t.Fatalf("token = %q", request.Token)
		}
		accountID := "31355990"
		return &companyv1.ValidateCompanyRegistrationTokenResponse{
			Valid:             true,
			State:             companyv1.CompanyRegistrationTokenState_COMPANY_REGISTRATION_TOKEN_STATE_VALID,
			ExternalAccountId: &accountID,
			ExpiresAt:         timestamppb.New(expiresAt),
		}, nil
	}}
	recorder := performRequest(newTestGateway(t, server), http.MethodPost, "/api/v1/public/company-registration-tokens/validate", `{"registrationToken":"registration-token"}`, nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"valid":true`) || !strings.Contains(recorder.Body.String(), `"amoAccountId":"31355990"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestRetiredRegistrationRoutesReturnGone(t *testing.T) {
	handler := newTestGateway(t, &provisioningCompanyServer{})
	tests := []struct {
		method        string
		path          string
		body          string
		authorization string
	}{
		{http.MethodPost, "/api/v1/provisioning/companies", `{}`, "Service " + testProvisioningServiceToken},
		{http.MethodGet, "/api/v1/provisioning/companies/status?externalAccountId=31355990", "", "Service " + testProvisioningServiceToken},
		{http.MethodPost, "/api/v1/provisioning/sessions", `{}`, "Service " + testProvisioningServiceToken},
		{http.MethodGet, "/api/v1/auth/bootstrap/token-token-token-token-token-12", "", ""},
		{http.MethodPost, "/api/v1/auth/sso/exchange", `{}`, ""},
	}
	for _, test := range tests {
		recorder := performProvisioningRequest(handler, test.method, test.path, test.body, test.authorization)
		if recorder.Code != http.StatusGone || responseErrorCode(t, recorder) != "REGISTRATION_FLOW_RETIRED" {
			t.Fatalf("%s %s: status=%d body=%s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func performProvisioningRequest(handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if method == http.MethodPost && path == "/api/v1/provisioning/companies" {
		request.Header.Set("Idempotency-Key", "retired-flow-test")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, recorder.Body.String())
	}
	return response.Error.Code
}
