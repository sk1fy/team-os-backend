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
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type provisioningCompanyServer struct {
	companyv1.UnimplementedCompanyServiceServer

	provisionCompanyFn            func(context.Context, *companyv1.ProvisionCompanyRequest) (*companyv1.ProvisionCompanyResponse, error)
	getProvisionedCompanyStatusFn func(context.Context, *companyv1.GetProvisionedCompanyStatusRequest) (*companyv1.GetProvisionedCompanyStatusResponse, error)
	issueSsoTokenFn               func(context.Context, *companyv1.IssueSsoTokenRequest) (*companyv1.IssueSsoTokenResponse, error)
	getBootstrapActivationFn      func(context.Context, *companyv1.GetBootstrapActivationRequest) (*companyv1.GetBootstrapActivationResponse, error)
	completeBootstrapActivationFn func(context.Context, *companyv1.CompleteBootstrapActivationRequest) (*companyv1.CompleteBootstrapActivationResponse, error)
	exchangeSsoTokenFn            func(context.Context, *companyv1.ExchangeSsoTokenRequest) (*companyv1.ExchangeSsoTokenResponse, error)
}

func (s *provisioningCompanyServer) GetProvisionedCompanyStatus(ctx context.Context, request *companyv1.GetProvisionedCompanyStatusRequest) (*companyv1.GetProvisionedCompanyStatusResponse, error) {
	if s.getProvisionedCompanyStatusFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected GetProvisionedCompanyStatus call")
	}
	return s.getProvisionedCompanyStatusFn(ctx, request)
}

func (s *provisioningCompanyServer) ProvisionCompany(ctx context.Context, request *companyv1.ProvisionCompanyRequest) (*companyv1.ProvisionCompanyResponse, error) {
	if s.provisionCompanyFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ProvisionCompany call")
	}
	return s.provisionCompanyFn(ctx, request)
}

func (s *provisioningCompanyServer) IssueSsoToken(ctx context.Context, request *companyv1.IssueSsoTokenRequest) (*companyv1.IssueSsoTokenResponse, error) {
	if s.issueSsoTokenFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected IssueSsoToken call")
	}
	return s.issueSsoTokenFn(ctx, request)
}

func (s *provisioningCompanyServer) GetBootstrapActivation(ctx context.Context, request *companyv1.GetBootstrapActivationRequest) (*companyv1.GetBootstrapActivationResponse, error) {
	if s.getBootstrapActivationFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected GetBootstrapActivation call")
	}
	return s.getBootstrapActivationFn(ctx, request)
}

func (s *provisioningCompanyServer) CompleteBootstrapActivation(ctx context.Context, request *companyv1.CompleteBootstrapActivationRequest) (*companyv1.CompleteBootstrapActivationResponse, error) {
	if s.completeBootstrapActivationFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected CompleteBootstrapActivation call")
	}
	return s.completeBootstrapActivationFn(ctx, request)
}

func (s *provisioningCompanyServer) ExchangeSsoToken(ctx context.Context, request *companyv1.ExchangeSsoTokenRequest) (*companyv1.ExchangeSsoTokenResponse, error) {
	if s.exchangeSsoTokenFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ExchangeSsoToken call")
	}
	return s.exchangeSsoTokenFn(ctx, request)
}

func TestProvisionCompanyRequiresServiceAuthProviderAndIdempotency(t *testing.T) {
	body := provisionCompanyJSON(testProvisioningProvider)
	tests := []struct {
		name           string
		authorization  string
		idempotencyKey string
		body           string
		wantStatus     int
		wantCode       string
	}{
		{name: "missing service auth", idempotencyKey: "account-31355990", body: body, wantStatus: http.StatusUnauthorized, wantCode: "SERVICE_AUTH_INVALID"},
		{name: "bearer is not service auth", authorization: "Bearer " + testProvisioningServiceToken, idempotencyKey: "account-31355990", body: body, wantStatus: http.StatusUnauthorized, wantCode: "SERVICE_AUTH_INVALID"},
		{name: "wrong service credential", authorization: "Service wrong-token", idempotencyKey: "account-31355990", body: body, wantStatus: http.StatusUnauthorized, wantCode: "SERVICE_AUTH_INVALID"},
		{name: "provider outside principal scope", authorization: "Service " + testProvisioningServiceToken, idempotencyKey: "account-31355990", body: provisionCompanyJSON("other"), wantStatus: http.StatusForbidden, wantCode: "SERVICE_PROVIDER_FORBIDDEN"},
		{name: "missing idempotency key", authorization: "Service " + testProvisioningServiceToken, body: body, wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := &provisioningCompanyServer{provisionCompanyFn: func(context.Context, *companyv1.ProvisionCompanyRequest) (*companyv1.ProvisionCompanyResponse, error) {
				calls.Add(1)
				return nil, status.Error(codes.Internal, "unexpected call")
			}}
			recorder := performProvisioningRequest(t, newTestGateway(t, server), http.MethodPost, "/api/v1/provisioning/companies", test.body, test.authorization, test.idempotencyKey)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if calls.Load() != 0 {
				t.Fatalf("ProvisionCompany calls = %d, want 0", calls.Load())
			}
			if test.wantCode != "" && responseErrorCode(t, recorder) != test.wantCode {
				t.Fatalf("error code = %q, want %q", responseErrorCode(t, recorder), test.wantCode)
			}
		})
	}
}

func TestIssueSsoTokenRequiresServiceAuth(t *testing.T) {
	var calls atomic.Int32
	server := &provisioningCompanyServer{issueSsoTokenFn: func(context.Context, *companyv1.IssueSsoTokenRequest) (*companyv1.IssueSsoTokenResponse, error) {
		calls.Add(1)
		return nil, status.Error(codes.Internal, "unexpected call")
	}}
	handler := newTestGateway(t, server)
	body := `{"provider":"rakurs","externalAccountId":"31355990","externalUserId":"101"}`
	for _, test := range []struct {
		name          string
		authorization string
	}{
		{name: "missing service auth"},
		{name: "wrong service credential", authorization: "Service wrong-token"},
		{name: "user bearer is forbidden", authorization: "Bearer " + testProvisioningServiceToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := performProvisioningRequest(
				t, handler, http.MethodPost, "/api/v1/provisioning/sessions",
				body, test.authorization, "",
			)
			if recorder.Code != http.StatusUnauthorized || responseErrorCode(t, recorder) != "SERVICE_AUTH_INVALID" {
				t.Fatalf("status=%d code=%q body=%s", recorder.Code, responseErrorCode(t, recorder), recorder.Body.String())
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("IssueSsoToken calls=%d, want 0", calls.Load())
	}
}

func TestProvisionCompanyForwardsOnlyTrustedInternalMetadata(t *testing.T) {
	expiresAt := timestamppb.New(time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC))
	server := &provisioningCompanyServer{provisionCompanyFn: func(ctx context.Context, request *companyv1.ProvisionCompanyRequest) (*companyv1.ProvisionCompanyResponse, error) {
		incoming, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			t.Fatal("missing incoming metadata")
		}
		if got := incoming.Get("x-teamos-service"); len(got) != 1 || got[0] != provisioningServiceMarker {
			t.Fatalf("x-teamos-service = %v", got)
		}
		if got := incoming.Get("x-teamos-service-token"); len(got) != 1 || got[0] != testCompanyServiceToken {
			t.Fatalf("x-teamos-service-token = %v", got)
		}
		if got := incoming.Get("authorization"); len(got) != 0 {
			t.Fatalf("external Authorization was forwarded: %v", got)
		}
		if request.GetProvider() != testProvisioningProvider || request.GetIdempotencyKey() != "account-31355990" {
			t.Fatalf("request = %#v", request)
		}
		return &companyv1.ProvisionCompanyResponse{
			CompanyId: testDepartmentID, CompanyStatus: companyv1.CompanyStatus_COMPANY_STATUS_ONBOARDING,
			Created: true, InitiatorRole: companyv1.UserRole_USER_ROLE_OWNER,
			BootstrapToken: "bootstrap+token/=", BootstrapExpiresAt: expiresAt,
		}, nil
	}}

	recorder := performProvisioningRequest(
		t, newTestGateway(t, server), http.MethodPost, "/api/v1/provisioning/companies",
		provisionCompanyJSON(testProvisioningProvider), "Service "+testProvisioningServiceToken, "account-31355990",
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var response struct {
		CompanyStatus string    `json:"companyStatus"`
		Created       bool      `json:"created"`
		InitiatorRole string    `json:"initiatorRole"`
		ContinueURL   string    `json:"continueUrl"`
		ExpiresAt     time.Time `json:"expiresAt"`
	}
	decodeJSON(t, recorder, &response)
	if response.CompanyStatus != "onboarding" || !response.Created || response.InitiatorRole != "owner" ||
		response.ContinueURL != "https://app.example.test/onboarding?token=bootstrap%2Btoken%2F%3D" || !response.ExpiresAt.Equal(expiresAt.AsTime()) {
		t.Fatalf("response = %#v", response)
	}
}

func TestGetProvisionedCompanyStatus(t *testing.T) {
	companyID := testDepartmentID
	tests := []struct {
		name       string
		response   *companyv1.GetProvisionedCompanyStatusResponse
		wantExists bool
	}{
		{
			name: "existing company",
			response: &companyv1.GetProvisionedCompanyStatusResponse{
				Exists: true, CompanyId: &companyID,
				CompanyStatus: companyv1.CompanyStatus_COMPANY_STATUS_ACTIVE,
			},
			wantExists: true,
		},
		{name: "unknown company", response: &companyv1.GetProvisionedCompanyStatusResponse{}, wantExists: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &provisioningCompanyServer{getProvisionedCompanyStatusFn: func(ctx context.Context, request *companyv1.GetProvisionedCompanyStatusRequest) (*companyv1.GetProvisionedCompanyStatusResponse, error) {
				incoming, ok := metadata.FromIncomingContext(ctx)
				if !ok || len(incoming.Get("authorization")) != 0 {
					t.Fatalf("untrusted metadata was forwarded: %v", incoming)
				}
				if request.GetProvider() != testProvisioningProvider || request.GetExternalAccountId() != "31355990" {
					t.Fatalf("request = %#v", request)
				}
				return test.response, nil
			}}
			recorder := performProvisioningRequest(
				t, newTestGateway(t, server), http.MethodGet,
				"/api/v1/provisioning/companies/status?externalAccountId=31355990", "",
				"Service "+testProvisioningServiceToken, "",
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			var response struct {
				Exists        bool    `json:"exists"`
				CompanyID     *string `json:"companyId"`
				CompanyStatus *string `json:"companyStatus"`
			}
			decodeJSON(t, recorder, &response)
			if response.Exists != test.wantExists {
				t.Fatalf("response = %#v", response)
			}
			if test.wantExists {
				if response.CompanyID == nil || *response.CompanyID != companyID ||
					response.CompanyStatus == nil || *response.CompanyStatus != "active" {
					t.Fatalf("response = %#v", response)
				}
			} else if response.CompanyID != nil || response.CompanyStatus != nil {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestGetProvisionedCompanyStatusRequiresServiceAuthAndAccountID(t *testing.T) {
	var calls atomic.Int32
	server := &provisioningCompanyServer{getProvisionedCompanyStatusFn: func(context.Context, *companyv1.GetProvisionedCompanyStatusRequest) (*companyv1.GetProvisionedCompanyStatusResponse, error) {
		calls.Add(1)
		return &companyv1.GetProvisionedCompanyStatusResponse{}, nil
	}}
	handler := newTestGateway(t, server)
	for _, test := range []struct {
		name          string
		path          string
		authorization string
		wantStatus    int
	}{
		{name: "missing service auth", path: "/api/v1/provisioning/companies/status?externalAccountId=31355990", wantStatus: http.StatusUnauthorized},
		{name: "missing account id", path: "/api/v1/provisioning/companies/status", authorization: "Service " + testProvisioningServiceToken, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := performProvisioningRequest(t, handler, http.MethodGet, test.path, "", test.authorization, "")
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("GetProvisionedCompanyStatus calls = %d, want 0", calls.Load())
	}
}

func TestIssueSsoTokenBuildsStrictContinuationURL(t *testing.T) {
	tests := []struct {
		name             string
		kind             companyv1.ProvisioningContinuationKind
		wantContinuation string
	}{
		{name: "pending identity", kind: companyv1.ProvisioningContinuationKind_PROVISIONING_CONTINUATION_KIND_ONBOARDING, wantContinuation: "/onboarding"},
		{name: "active identity", kind: companyv1.ProvisioningContinuationKind_PROVISIONING_CONTINUATION_KIND_SSO, wantContinuation: "/sso"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expiresAt := timestamppb.New(time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC))
			server := &provisioningCompanyServer{issueSsoTokenFn: func(_ context.Context, request *companyv1.IssueSsoTokenRequest) (*companyv1.IssueSsoTokenResponse, error) {
				if request.GetProvider() != testProvisioningProvider {
					t.Fatalf("request = %#v", request)
				}
				return &companyv1.IssueSsoTokenResponse{Token: "one+time/=", ExpiresAt: expiresAt, ContinuationKind: test.kind}, nil
			}}
			recorder := performProvisioningRequest(
				t, newTestGateway(t, server), http.MethodPost, "/api/v1/provisioning/sessions",
				`{"provider":"rakurs","externalAccountId":"31355990","externalUserId":"101"}`,
				"Service "+testProvisioningServiceToken, "",
			)
			if recorder.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			var response struct {
				RedirectURL string `json:"redirectUrl"`
			}
			decodeJSON(t, recorder, &response)
			wantURL := "https://app.example.test" + test.wantContinuation + "?token=one%2Btime%2F%3D"
			if response.RedirectURL != wantURL || strings.Contains(response.RedirectURL, "#token=") {
				t.Fatalf("redirectUrl = %q, want %q", response.RedirectURL, wantURL)
			}
		})
	}
}

func TestBootstrapResponseIsNoStoreAndContainsStableCode(t *testing.T) {
	rpcStatus, err := status.New(codes.AlreadyExists, "Ссылка уже использована").WithDetails(
		&errdetails.ErrorInfo{Reason: "BOOTSTRAP_CONSUMED", Domain: "teamos.company"},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := &provisioningCompanyServer{getBootstrapActivationFn: func(context.Context, *companyv1.GetBootstrapActivationRequest) (*companyv1.GetBootstrapActivationResponse, error) {
		return nil, rpcStatus.Err()
	}}
	recorder := performRequest(newTestGateway(t, server), http.MethodGet, "/api/v1/auth/bootstrap/bootstrap-token", "", nil)
	if recorder.Code != http.StatusConflict || responseErrorCode(t, recorder) != "BOOTSTRAP_CONSUMED" {
		t.Fatalf("status = %d code = %q body = %s", recorder.Code, responseErrorCode(t, recorder), recorder.Body.String())
	}
}

func TestCompleteBootstrapValidatesOnboardingBeforeSettingCookie(t *testing.T) {
	server := &provisioningCompanyServer{completeBootstrapActivationFn: func(context.Context, *companyv1.CompleteBootstrapActivationRequest) (*companyv1.CompleteBootstrapActivationResponse, error) {
		return &companyv1.CompleteBootstrapActivationResponse{Session: testAuthSession("access-token", "refresh-token")}, nil
	}}
	recorder := performRequest(
		newTestGateway(t, server), http.MethodPost, "/api/v1/auth/bootstrap/bootstrap-token/complete",
		`{"password":"correct horse battery staple"}`, nil,
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if values := recorder.Header().Values("Set-Cookie"); len(values) != 0 {
		t.Fatalf("Set-Cookie must be absent on conversion failure: %v", values)
	}
}

func TestExchangeSsoTokenReturnsNoStoreSession(t *testing.T) {
	server := &provisioningCompanyServer{exchangeSsoTokenFn: func(_ context.Context, request *companyv1.ExchangeSsoTokenRequest) (*companyv1.ExchangeSsoTokenResponse, error) {
		if request.GetToken() != "one-time-token" {
			t.Fatalf("token = %q", request.GetToken())
		}
		return &companyv1.ExchangeSsoTokenResponse{Session: testAuthSession("access-token", "refresh-token")}, nil
	}}
	recorder := performRequest(newTestGateway(t, server), http.MethodPost, "/api/v1/auth/sso/exchange", `{"token":"one-time-token"}`, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if cookie := responseCookie(t, recorder, refreshCookieName); cookie.Value != "refresh-token" {
		t.Fatalf("refresh cookie = %q", cookie.Value)
	}
}

func provisionCompanyJSON(provider string) string {
	value := map[string]any{
		"provider": provider, "externalAccountId": "31355990", "companyName": "ООО Ромашка", "initiatorExternalUserId": "101",
		"owner": map[string]string{"externalUserId": "101", "email": "owner@example.com", "firstName": "Иван", "lastName": "Иванов"},
		"admin": map[string]string{"externalUserId": "102", "email": "admin@example.com", "firstName": "Пётр", "lastName": "Петров"},
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func performProvisioningRequest(t *testing.T, handler http.Handler, method, path, body, authorization, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body = %s", err, recorder.Body.String())
	}
	return envelope.Error.Code
}
