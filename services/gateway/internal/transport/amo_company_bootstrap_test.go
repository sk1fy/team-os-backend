package transport

import (
	"context"
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
	"google.golang.org/grpc"
)

type amoCompanyBootstrapClient struct {
	companyv1.CompanyServiceClient
	bootstrapFn func(context.Context, *companyv1.BootstrapAmoCompanyRequest) (*companyv1.BootstrapAmoCompanyResponse, error)
}

func (c *amoCompanyBootstrapClient) BootstrapAmoCompany(
	ctx context.Context,
	request *companyv1.BootstrapAmoCompanyRequest,
	_ ...grpc.CallOption,
) (*companyv1.BootstrapAmoCompanyResponse, error) {
	return c.bootstrapFn(ctx, request)
}

func TestAmoCompanyBootstrapChallengeAndPayload(t *testing.T) {
	manager, err := amochallenge.New(strings.Repeat("s", 32), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	client := &amoCompanyBootstrapClient{bootstrapFn: func(
		_ context.Context,
		request *companyv1.BootstrapAmoCompanyRequest,
	) (*companyv1.BootstrapAmoCompanyResponse, error) {
		calls.Add(1)
		if request.GetAmoAccountId() != "31355990" || request.GetSelfUserId() != "101" ||
			request.GetCompanyName() != "Компания" || request.GetSubdomain() != "example" || len(request.GetUsers()) != 2 ||
			!request.GetUsers()[0].GetIsAdmin() || !request.GetUsers()[0].GetIsActive() ||
			request.GetUsers()[1].GetIsAdmin() || !request.GetUsers()[1].GetIsActive() {
			t.Fatalf("bootstrap request=%#v", request)
		}
		return &companyv1.BootstrapAmoCompanyResponse{
			Action:      companyv1.AmoWidgetSessionAction_AMO_WIDGET_SESSION_ACTION_REGISTER,
			CompanyId:   "11111111-1111-4111-8111-111111111111",
			UserId:      "22222222-2222-4222-8222-222222222222",
			Role:        companyv1.UserRole_USER_ROLE_OWNER,
			AccessToken: "stable_access_token_abcdefghijklmnopqrstuvwxyz",
		}, nil
	}}
	handler := NewHandler(
		client, nil, nil, nil,
		CookieConfig{PublicAppURL: "https://company.rkrs.ru"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	token, err := manager.IssueForPurpose("31355990", amochallenge.PurposeCompanyBootstrap)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := amochallenge.Middleware(manager)(http.HandlerFunc(handler.BootstrapAmoCompany))
	body := `{"account":{"id":"31355990","name":"Компания","subdomain":"example"},"selfUserId":"101","users":[{"id":"101","email":"owner@example.ru","name":"Иван","isAdmin":true,"isActive":true,"role":"owner"},{"id":"102","email":"user@example.ru","name":"Анна","isAdmin":false,"isActive":true,"role":"admin"}]}`
	call := func(authorization string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(
			context.Background(), http.MethodPost, "/api/v1/public/amocrm/company-bootstrap", strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", authorization)
		recorder := httptest.NewRecorder()
		endpoint.ServeHTTP(recorder, request)
		return recorder
	}
	first := call("Bearer " + token)
	if first.Code != http.StatusCreated ||
		!strings.Contains(first.Body.String(), `"action":"register"`) ||
		!strings.Contains(first.Body.String(), `"redirectUrl":"https://company.rkrs.ru/access/stable_access_token_abcdefghijklmnopqrstuvwxyz"`) {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := call("Bearer " + token)
	if second.Code != http.StatusUnauthorized || calls.Load() != 1 {
		t.Fatalf("replay status=%d calls=%d body=%s", second.Code, calls.Load(), second.Body.String())
	}
}
