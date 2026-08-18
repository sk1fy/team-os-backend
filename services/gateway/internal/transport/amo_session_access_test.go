package transport

import (
	"context"
	"net/http"
	"strings"
	"testing"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type amoSessionAccessCompanyServer struct {
	companyv1.UnimplementedCompanyServiceServer
	checkFn func(context.Context, *companyv1.CheckAmoSessionAccessRequest) (*companyv1.CheckAmoSessionAccessResponse, error)
}

func (s *amoSessionAccessCompanyServer) CheckAmoSessionAccess(
	ctx context.Context,
	request *companyv1.CheckAmoSessionAccessRequest,
) (*companyv1.CheckAmoSessionAccessResponse, error) {
	return s.checkFn(ctx, request)
}

func TestCheckAmoSessionAccessReturnsVerifiedSessionResult(t *testing.T) {
	server := &amoSessionAccessCompanyServer{checkFn: func(
		_ context.Context,
		request *companyv1.CheckAmoSessionAccessRequest,
	) (*companyv1.CheckAmoSessionAccessResponse, error) {
		if request.GetAmoAccountId() != "31355990" {
			t.Fatalf("request=%#v", request)
		}
		return &companyv1.CheckAmoSessionAccessResponse{
			Allowed: true, Role: companyv1.UserRole_USER_ROLE_ADMIN, RedirectUrl: "/schedule",
		}, nil
	}}
	recorder := performRequest(
		newTestGateway(t, server), http.MethodPost, "/api/v1/amocrm/session-access",
		`{"amoAccountId":"31355990"}`, nil,
	)
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"allowed":true`) ||
		!strings.Contains(recorder.Body.String(), `"role":"admin"`) ||
		!strings.Contains(recorder.Body.String(), `"redirectUrl":"/schedule"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control=%q", recorder.Header().Get("Cache-Control"))
	}
}

func TestCheckAmoSessionAccessMapsLockedCompany(t *testing.T) {
	server := &amoSessionAccessCompanyServer{checkFn: func(
		context.Context,
		*companyv1.CheckAmoSessionAccessRequest,
	) (*companyv1.CheckAmoSessionAccessResponse, error) {
		value := status.New(codes.AlreadyExists, "Компания TeamOS временно заблокирована")
		value, err := value.WithDetails(&errdetails.ErrorInfo{
			Reason: "AMO_SESSION_ACCESS_LOCKED", Domain: "teamos.company",
		})
		if err != nil {
			t.Fatal(err)
		}
		return nil, value.Err()
	}}
	recorder := performRequest(
		newTestGateway(t, server), http.MethodPost, "/api/v1/amocrm/session-access",
		`{"amoAccountId":"31355990"}`, nil,
	)
	if recorder.Code != http.StatusLocked || responseErrorCode(t, recorder) != "AMO_SESSION_ACCESS_LOCKED" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCheckAmoSessionAccessRejectsBrowserHints(t *testing.T) {
	recorder := performRequest(
		newTestGateway(t, &amoSessionAccessCompanyServer{checkFn: func(
			context.Context,
			*companyv1.CheckAmoSessionAccessRequest,
		) (*companyv1.CheckAmoSessionAccessResponse, error) {
			t.Fatal("company must not be called for invalid body")
			return nil, nil
		}}),
		http.MethodPost, "/api/v1/amocrm/session-access",
		`{"amoAccountId":"31355990","userId":"forged","role":"owner"}`, nil,
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
