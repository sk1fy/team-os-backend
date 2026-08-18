package transport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/amochallenge"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
)

type amoSelfLoginClient struct {
	companyv1.CompanyServiceClient
	checkFn func(context.Context, *companyv1.CheckAmoAccountRequest) (*companyv1.CheckAmoAccountResponse, error)
	loginFn func(context.Context, *companyv1.AmoAdminSelfLoginRequest) (*companyv1.AmoAdminSelfLoginResponse, error)
}

func (c *amoSelfLoginClient) CheckAmoAccount(
	ctx context.Context,
	request *companyv1.CheckAmoAccountRequest,
	_ ...grpc.CallOption,
) (*companyv1.CheckAmoAccountResponse, error) {
	return c.checkFn(ctx, request)
}

func (c *amoSelfLoginClient) AmoAdminSelfLogin(
	ctx context.Context,
	request *companyv1.AmoAdminSelfLoginRequest,
	_ ...grpc.CallOption,
) (*companyv1.AmoAdminSelfLoginResponse, error) {
	return c.loginFn(ctx, request)
}

func TestAmoAdminSelfLoginChallengeAndReplay(t *testing.T) {
	manager, err := amochallenge.New(strings.Repeat("s", 32), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var loginCalls atomic.Int32
	client := &amoSelfLoginClient{
		checkFn: func(_ context.Context, request *companyv1.CheckAmoAccountRequest) (*companyv1.CheckAmoAccountResponse, error) {
			switch request.GetExternalAccountId() {
			case "31355990":
				return &companyv1.CheckAmoAccountResponse{Exists: true, AdminSelfLoginEligible: true}, nil
			case "31355992":
				return &companyv1.CheckAmoAccountResponse{Exists: true}, nil
			default:
				return &companyv1.CheckAmoAccountResponse{}, nil
			}
		},
		loginFn: func(_ context.Context, request *companyv1.AmoAdminSelfLoginRequest) (*companyv1.AmoAdminSelfLoginResponse, error) {
			loginCalls.Add(1)
			if request.GetAmoAccountId() != "31355990" || request.GetSelfUserId() != "101" ||
				len(request.GetUsers()) != 1 || !request.GetUsers()[0].GetIsAdmin() || !request.GetUsers()[0].GetIsActive() {
				t.Fatalf("login request=%#v", request)
			}
			return &companyv1.AmoAdminSelfLoginResponse{
				Allowed: true, Action: companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_LOGIN,
				Role: companyv1.UserRole_USER_ROLE_ADMIN, AccessToken: "stable_access_token_abcdefghijklmnopqrstuvwxyz",
			}, nil
		},
	}
	handler := NewHandler(client, nil, nil, nil, CookieConfig{PublicAppURL: "https://company.rkrs.ru"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler.SetProvisioningServiceProvider("rakurs")
	handler.SetAmoChallengeManager(manager)
	selfLoginEndpoint := amochallenge.Middleware(manager)(http.HandlerFunc(handler.AmoAdminSelfLogin))
	missingAuthRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/public/amocrm/admin-self-login",
		strings.NewReader(`{"selfUserId":"101","users":[{"id":"101","isAdmin":true,"isActive":true}]}`),
	)
	missingAuthRequest.Header.Set("Content-Type", "application/json")
	missingAuthRecorder := httptest.NewRecorder()
	selfLoginEndpoint.ServeHTTP(missingAuthRecorder, missingAuthRequest)
	if missingAuthRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status=%d body=%s", missingAuthRecorder.Code, missingAuthRecorder.Body.String())
	}
	existsRecorder := httptest.NewRecorder()
	handler.CheckAmoAccount(existsRecorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil), "31355990")
	if existsRecorder.Code != http.StatusOK {
		t.Fatalf("exists status=%d body=%s", existsRecorder.Code, existsRecorder.Body.String())
	}
	var exists api.AmoAccountAvailabilityResponse
	decodeRecorderJSON(t, existsRecorder, &exists)
	if !exists.Exists || !exists.AdminSelfLoginEligible || exists.ChallengeToken == nil ||
		exists.ExpiresIn == nil || *exists.ExpiresIn != 120 ||
		exists.TokenType == nil || *exists.TokenType != "Bearer" {
		t.Fatalf("exists=%#v", exists)
	}
	reservedRecorder := httptest.NewRecorder()
	handler.CheckAmoAccount(
		reservedRecorder,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil),
		"31355992",
	)
	var reserved api.AmoAccountAvailabilityResponse
	decodeRecorderJSON(t, reservedRecorder, &reserved)
	if !reserved.Exists || reserved.AdminSelfLoginEligible || reserved.ChallengeToken != nil ||
		reserved.TokenType != nil || reserved.ExpiresIn != nil {
		t.Fatalf("reserved=%#v", reserved)
	}
	missingRecorder := httptest.NewRecorder()
	handler.CheckAmoAccount(missingRecorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil), "31355991")
	var missing api.AmoAccountAvailabilityResponse
	decodeRecorderJSON(t, missingRecorder, &missing)
	if missing.Exists || missing.AdminSelfLoginEligible || missing.ChallengeToken != nil ||
		missing.TokenType != nil || missing.ExpiresIn != nil {
		t.Fatalf("missing=%#v", missing)
	}
	body := `{"selfUserId":"101","users":[{"id":"101","isAdmin":true,"isActive":true}]}`
	call := func() *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/amocrm/admin-self-login", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+*exists.ChallengeToken)
		recorder := httptest.NewRecorder()
		selfLoginEndpoint.ServeHTTP(recorder, request)
		return recorder
	}
	first := call()
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"redirectUrl":"https://company.rkrs.ru/access/stable_access_token_abcdefghijklmnopqrstuvwxyz"`) {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := call()
	if second.Code != http.StatusUnauthorized || loginCalls.Load() != 1 {
		t.Fatalf("replay status=%d calls=%d body=%s", second.Code, loginCalls.Load(), second.Body.String())
	}
	strictToken, err := manager.Issue("31355990")
	if err != nil {
		t.Fatal(err)
	}
	strictRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/public/amocrm/admin-self-login",
		strings.NewReader(`{"selfUserId":"101","users":[{"id":"101","isAdmin":true,"isActive":true}],"role":"owner"}`),
	)
	strictRequest.Header.Set("Content-Type", "application/json")
	strictRequest.Header.Set("Authorization", "Bearer "+strictToken)
	strictRecorder := httptest.NewRecorder()
	selfLoginEndpoint.ServeHTTP(strictRecorder, strictRequest)
	if strictRecorder.Code != http.StatusBadRequest || loginCalls.Load() != 1 {
		t.Fatalf("strict status=%d calls=%d body=%s", strictRecorder.Code, loginCalls.Load(), strictRecorder.Body.String())
	}
}

func decodeRecorderJSON(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
