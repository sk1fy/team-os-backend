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
	provisionAmoAdminSessionFn         func(context.Context, *companyv1.ProvisionAmoAdminSessionRequest) (*companyv1.ProvisionAmoAdminSessionResponse, error)
	validateCompanyRegistrationTokenFn func(context.Context, *companyv1.ValidateCompanyRegistrationTokenRequest) (*companyv1.ValidateCompanyRegistrationTokenResponse, error)
	exchangeAmoWidgetSessionFn         func(context.Context, *companyv1.ExchangeAmoWidgetSessionRequest) (*companyv1.ExchangeAmoWidgetSessionResponse, error)
	validateAmoWidgetContinuationFn    func(context.Context, *companyv1.ValidateAmoWidgetContinuationRequest) (*companyv1.ValidateAmoWidgetContinuationResponse, error)
	completeAmoWidgetContinuationFn    func(context.Context, *companyv1.CompleteAmoWidgetContinuationRequest) (*companyv1.CompleteAmoWidgetContinuationResponse, error)
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

func (s *provisioningCompanyServer) ProvisionAmoAdminSession(ctx context.Context, request *companyv1.ProvisionAmoAdminSessionRequest) (*companyv1.ProvisionAmoAdminSessionResponse, error) {
	if s.provisionAmoAdminSessionFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ProvisionAmoAdminSession call")
	}
	return s.provisionAmoAdminSessionFn(ctx, request)
}

func (s *provisioningCompanyServer) ValidateCompanyRegistrationToken(ctx context.Context, request *companyv1.ValidateCompanyRegistrationTokenRequest) (*companyv1.ValidateCompanyRegistrationTokenResponse, error) {
	if s.validateCompanyRegistrationTokenFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ValidateCompanyRegistrationToken call")
	}
	return s.validateCompanyRegistrationTokenFn(ctx, request)
}

func (s *provisioningCompanyServer) ExchangeAmoWidgetSession(ctx context.Context, request *companyv1.ExchangeAmoWidgetSessionRequest) (*companyv1.ExchangeAmoWidgetSessionResponse, error) {
	if s.exchangeAmoWidgetSessionFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ExchangeAmoWidgetSession call")
	}
	return s.exchangeAmoWidgetSessionFn(ctx, request)
}

func (s *provisioningCompanyServer) ValidateAmoWidgetContinuation(ctx context.Context, request *companyv1.ValidateAmoWidgetContinuationRequest) (*companyv1.ValidateAmoWidgetContinuationResponse, error) {
	if s.validateAmoWidgetContinuationFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected ValidateAmoWidgetContinuation call")
	}
	return s.validateAmoWidgetContinuationFn(ctx, request)
}

func (s *provisioningCompanyServer) CompleteAmoWidgetContinuation(ctx context.Context, request *companyv1.CompleteAmoWidgetContinuationRequest) (*companyv1.CompleteAmoWidgetContinuationResponse, error) {
	if s.completeAmoWidgetContinuationFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected CompleteAmoWidgetContinuation call")
	}
	return s.completeAmoWidgetContinuationFn(ctx, request)
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

func TestProvisionAmoAdminSessionRequiresServiceAuthAndReturnsAccessURL(t *testing.T) {
	var calls atomic.Int32
	server := &provisioningCompanyServer{provisionAmoAdminSessionFn: func(
		ctx context.Context,
		request *companyv1.ProvisionAmoAdminSessionRequest,
	) (*companyv1.ProvisionAmoAdminSessionResponse, error) {
		calls.Add(1)
		if request.GetProvider() != testProvisioningProvider || request.GetExternalAccountId() != "31355990" ||
			request.GetExternalUserId() != "10912522" || request.GetEmail() != "admin@example.com" ||
			request.GetUserName() != "Иван Петров" || request.GetCompanyName() != "Ракурс" ||
			request.GetDesiredRole() != companyv1.UserRole_USER_ROLE_OWNER {
			t.Fatalf("request = %#v", request)
		}
		incoming, _ := metadata.FromIncomingContext(ctx)
		if values := incoming.Get("x-teamos-service"); len(values) != 1 || values[0] != provisioningServiceMarker {
			t.Fatalf("service metadata = %v", values)
		}
		return &companyv1.ProvisionAmoAdminSessionResponse{
			Action:            companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER,
			ExternalAccountId: "31355990", CompanyId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			UserId: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Role: companyv1.UserRole_USER_ROLE_OWNER,
			AccessToken: "amo-admin-access-token_abcdefghijklmnopqrstuvwxyz",
		}, nil
	}}
	handler := newTestGateway(t, server)
	body := `{"account":{"id":"31355990","name":"Ракурс"},"user":{"id":"10912522","email":"admin@example.com","name":"Иван Петров"},"desiredRole":"owner"}`
	unauthorized := performProvisioningRequest(
		handler, http.MethodPost, "/api/v1/provisioning/amocrm/admin-sessions", body, "",
	)
	if unauthorized.Code != http.StatusUnauthorized || responseErrorCode(t, unauthorized) != "SERVICE_AUTH_INVALID" || calls.Load() != 0 {
		t.Fatalf("unauthorized status=%d calls=%d body=%s", unauthorized.Code, calls.Load(), unauthorized.Body.String())
	}
	authorized := performProvisioningRequest(
		handler, http.MethodPost, "/api/v1/provisioning/amocrm/admin-sessions", body,
		"Service "+testProvisioningServiceToken,
	)
	if authorized.Code != http.StatusCreated || calls.Load() != 1 ||
		!strings.Contains(authorized.Body.String(), `"role":"owner"`) ||
		!strings.Contains(authorized.Body.String(), `"redirectUrl":"https://app.example.test/access/amo-admin-access-token_abcdefghijklmnopqrstuvwxyz"`) {
		t.Fatalf("authorized status=%d calls=%d body=%s", authorized.Code, calls.Load(), authorized.Body.String())
	}
	if authorized.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", authorized.Header().Get("Cache-Control"))
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

func TestExchangeAmoWidgetSessionAcceptsJSONToken(t *testing.T) {
	const token = "amo-widget-token-abcdefghijklmnopqrstuvwxyz"
	expiresAt := time.Date(2026, time.August, 10, 13, 0, 0, 0, time.UTC)
	accountID, registrationToken := "31355990", "registration-token-abcdefghijklmnopqrstuvwxyz"
	server := &provisioningCompanyServer{exchangeAmoWidgetSessionFn: func(_ context.Context, request *companyv1.ExchangeAmoWidgetSessionRequest) (*companyv1.ExchangeAmoWidgetSessionResponse, error) {
		if request.GetToken() != token {
			t.Fatalf("token=%q", request.GetToken())
		}
		return &companyv1.ExchangeAmoWidgetSessionResponse{
			Action:            companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER,
			ExternalAccountId: &accountID, RegistrationToken: &registrationToken, ExpiresAt: timestamppb.New(expiresAt),
		}, nil
	}}
	recorder := performRequest(newTestGateway(t, server), http.MethodPost, "/api/v1/public/amocrm/widget-sessions", `{"token":"`+token+`"}`, nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"action":"register"`) ||
		!strings.Contains(recorder.Body.String(), `"amoAccountId":"31355990"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control=%q", recorder.Header().Get("Cache-Control"))
	}
}

func TestExchangeAmoWidgetSessionAcceptsAuthorizedSDKHeader(t *testing.T) {
	const token = "amo-widget-token-abcdefghijklmnopqrstuvwxyz"
	accountID := "31355990"
	server := &provisioningCompanyServer{exchangeAmoWidgetSessionFn: func(_ context.Context, request *companyv1.ExchangeAmoWidgetSessionRequest) (*companyv1.ExchangeAmoWidgetSessionResponse, error) {
		if request.GetToken() != token {
			t.Fatalf("token=%q", request.GetToken())
		}
		return &companyv1.ExchangeAmoWidgetSessionResponse{
			Action: companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN, ExternalAccountId: &accountID,
		}, nil
	}}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/amocrm/widget-sessions", nil)
	request.Header.Set("X-Auth-Token", token)
	recorder := httptest.NewRecorder()
	newTestGateway(t, server).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"action":"login"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestExchangeAmoWidgetSessionIgnoresBrowserProfile(t *testing.T) {
	const token = "amo-widget-token-abcdefghijklmnopqrstuvwxyz"
	expiresAt := time.Date(2026, time.August, 10, 12, 10, 0, 0, time.UTC)
	accountID, sessionToken := "31355990", "amo-continuation-token-abcdefghijklmnopqrstuvwxyz"
	email, companyName, setup := "admin@example.com", "Ракурс", true
	server := &provisioningCompanyServer{exchangeAmoWidgetSessionFn: func(_ context.Context, request *companyv1.ExchangeAmoWidgetSessionRequest) (*companyv1.ExchangeAmoWidgetSessionResponse, error) {
		if request.GetToken() != token || request.ExternalUserId != nil || request.Email != nil ||
			request.UserName != nil || request.CompanyName != nil || request.ExternalAccountId != nil ||
			request.GetIsAdmin() || request.GetIsOwner() {
			t.Fatalf("request=%#v", request)
		}
		return &companyv1.ExchangeAmoWidgetSessionResponse{
			Action:            companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER,
			ExternalAccountId: &accountID, SessionToken: &sessionToken, Email: &email,
			CompanyName: &companyName, RequiresPasswordSetup: &setup, ExpiresAt: timestamppb.New(expiresAt),
		}, nil
	}}
	request := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/public/amocrm/widget-sessions",
		strings.NewReader(`{"user":{"id":"42","email":"admin@example.com","name":"Иван Петров"},"account":{"name":"Ракурс"}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Auth-Token", token)
	recorder := httptest.NewRecorder()
	newTestGateway(t, server).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"sessionToken":"`+sessionToken+`"`) ||
		!strings.Contains(recorder.Body.String(), `"requiresPasswordSetup":true`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestExchangeAmoWidgetSessionIgnoresBrowserRightsAndReturnsAccessLink(t *testing.T) {
	const token = "amo-widget-token-abcdefghijklmnopqrstuvwxyz"
	accountID, accessToken := "31355990", "amo-access-token_abcdefghijklmnopqrstuvwxyz"
	role := companyv1.UserRole_USER_ROLE_OWNER
	server := &provisioningCompanyServer{exchangeAmoWidgetSessionFn: func(
		_ context.Context,
		request *companyv1.ExchangeAmoWidgetSessionRequest,
	) (*companyv1.ExchangeAmoWidgetSessionResponse, error) {
		if request.GetToken() != token || request.ExternalUserId != nil || request.ExternalAccountId != nil ||
			request.GetIsAdmin() || request.GetIsOwner() {
			t.Fatalf("request=%#v", request)
		}
		return &companyv1.ExchangeAmoWidgetSessionResponse{
			Action:            companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN,
			ExternalAccountId: &accountID, AccessToken: &accessToken, Role: &role,
		}, nil
	}}
	request := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/public/amocrm/widget-sessions",
		strings.NewReader(`{"user":{"id":"42","email":"admin@example.com"},"account":{"id":"31355990"},"isAdmin":true,"isOwner":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Auth-Token", token)
	recorder := httptest.NewRecorder()
	newTestGateway(t, server).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"role":"owner"`) ||
		!strings.Contains(recorder.Body.String(), `"redirectUrl":"https://app.example.test/access/amo-access-token_abcdefghijklmnopqrstuvwxyz"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestExchangeAmoWidgetSessionRejectsUnsignedBrowserProfile(t *testing.T) {
	recorder := performRequest(
		newTestGateway(t, &provisioningCompanyServer{}), http.MethodPost, "/api/v1/public/amocrm/widget-sessions",
		`{"user":{"id":"42","email":"admin@example.com","name":"Иван Петров"},"account":{"id":"31355990","name":"Ракурс"}}`, nil,
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAmoWidgetContinuationValidationAndCompletion(t *testing.T) {
	const sessionToken = "amo-continuation-token-abcdefghijklmnopqrstuvwxyz"
	expiresAt := time.Date(2026, time.August, 10, 12, 10, 0, 0, time.UTC)
	server := &provisioningCompanyServer{
		validateAmoWidgetContinuationFn: func(_ context.Context, request *companyv1.ValidateAmoWidgetContinuationRequest) (*companyv1.ValidateAmoWidgetContinuationResponse, error) {
			if request.GetSessionToken() != sessionToken {
				t.Fatalf("session token=%q", request.GetSessionToken())
			}
			return &companyv1.ValidateAmoWidgetContinuationResponse{
				Email: "admin@example.com", Login: "tm1234567", CompanyName: "Ракурс",
				RequiresPasswordSetup: true, ExpiresAt: timestamppb.New(expiresAt),
			}, nil
		},
		completeAmoWidgetContinuationFn: func(_ context.Context, request *companyv1.CompleteAmoWidgetContinuationRequest) (*companyv1.CompleteAmoWidgetContinuationResponse, error) {
			if request.GetSessionToken() != sessionToken || request.GetPassword() != "reliable-password" {
				t.Fatalf("request=%#v", request)
			}
			return &companyv1.CompleteAmoWidgetContinuationResponse{
				Session: testAuthSession("amo-access", "amo-refresh"),
			}, nil
		},
	}
	handler := newTestGateway(t, server)
	validated := performRequest(handler, http.MethodPost, "/api/v1/public/amocrm/widget-sessions/validate", `{"sessionToken":"`+sessionToken+`"}`, nil)
	if validated.Code != http.StatusOK || !strings.Contains(validated.Body.String(), `"login":"tm1234567"`) {
		t.Fatalf("validation status=%d body=%s", validated.Code, validated.Body.String())
	}
	completed := performRequest(handler, http.MethodPost, "/api/v1/auth/amocrm/complete", `{"sessionToken":"`+sessionToken+`","password":"reliable-password"}`, nil)
	if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), `"accessToken":"amo-access"`) {
		t.Fatalf("complete status=%d body=%s", completed.Code, completed.Body.String())
	}
	if got := responseCookie(t, completed, refreshCookieName).Value; got != "amo-refresh" {
		t.Fatalf("refresh cookie=%q", got)
	}
}

func TestExchangeAmoWidgetSessionRejectsTwoTokenSources(t *testing.T) {
	const token = "amo-widget-token-abcdefghijklmnopqrstuvwxyz"
	request := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/public/amocrm/widget-sessions",
		strings.NewReader(`{"token":"`+token+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Auth-Token", token)
	recorder := httptest.NewRecorder()
	newTestGateway(t, &provisioningCompanyServer{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
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
