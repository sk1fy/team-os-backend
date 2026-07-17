package transport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testUserID       = "1940fa16-cc83-448f-bd82-31dd8b15ce11"
	testDepartmentID = "9ee20c36-c91f-4b89-89ff-a60232867b82"
	testChildID      = "bf943c3b-bcd1-4b38-99a0-a4711509ed61"
)

type stubCompanyServer struct {
	companyv1.UnimplementedCompanyServiceServer

	loginFn            func(context.Context, *companyv1.LoginRequest) (*companyv1.LoginResponse, error)
	registerFn         func(context.Context, *companyv1.RegisterRequest) (*companyv1.RegisterResponse, error)
	refreshFn          func(context.Context, *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error)
	getDepartmentsFn   func(context.Context, *companyv1.GetDepartmentsRequest) (*companyv1.GetDepartmentsResponse, error)
	updateDepartmentFn func(context.Context, *companyv1.UpdateDepartmentRequest) (*companyv1.UpdateDepartmentResponse, error)
	deleteUserFn       func(context.Context, *companyv1.DeleteUserRequest) (*companyv1.DeleteUserResponse, error)
	updateUserCardFn   func(context.Context, *companyv1.UpdateUserCardRequest) (*companyv1.UpdateUserCardResponse, error)
}

func (s *stubCompanyServer) DeleteUser(ctx context.Context, request *companyv1.DeleteUserRequest) (*companyv1.DeleteUserResponse, error) {
	if s.deleteUserFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected DeleteUser call")
	}
	return s.deleteUserFn(ctx, request)
}

func (s *stubCompanyServer) UpdateUserCard(ctx context.Context, request *companyv1.UpdateUserCardRequest) (*companyv1.UpdateUserCardResponse, error) {
	if s.updateUserCardFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected UpdateUserCard call")
	}
	return s.updateUserCardFn(ctx, request)
}

func TestGatewayDeleteUserBridgesToCompany(t *testing.T) {
	requests := make(chan *companyv1.DeleteUserRequest, 1)
	server := &stubCompanyServer{deleteUserFn: func(_ context.Context, request *companyv1.DeleteUserRequest) (*companyv1.DeleteUserResponse, error) {
		requests <- request
		return &companyv1.DeleteUserResponse{}, nil
	}}
	recorder := serveGatewayRequest(t, server, http.MethodDelete, "/api/v1/org/users/"+testUserID, "", nil)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", recorder.Code, recorder.Body.String())
	}
	if request := <-requests; request.GetId() != testUserID {
		t.Fatalf("id = %q", request.GetId())
	}
}

func TestGatewayUpdateUserCardBridgesAtomicRequest(t *testing.T) {
	requests := make(chan *companyv1.UpdateUserCardRequest, 1)
	server := &stubCompanyServer{updateUserCardFn: func(_ context.Context, request *companyv1.UpdateUserCardRequest) (*companyv1.UpdateUserCardResponse, error) {
		requests <- request
		user := testAuthSession("access", "refresh").User
		user.FirstName = "Grace"
		user.LastName = "Hopper"
		return &companyv1.UpdateUserCardResponse{
			User: user,
			Schedule: &companyv1.UserSchedule{UserId: testUserID, Template: &companyv1.ScheduleTemplate{
				Type: "week", Days: []uint32{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00",
			}},
		}, nil
	}}
	recorder := serveGatewayRequest(t, server, http.MethodPatch, "/api/v1/org/users/"+testUserID+"/card", `{
		"user":{"firstName":"Grace","lastName":"Hopper"},
		"schedule":{"template":{"type":"week","days":[1,2,3,4,5],"start":"09:00","end":"18:00"}}
	}`, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", recorder.Code, recorder.Body.String())
	}
	request := <-requests
	if request.GetUser().GetId() != testUserID || request.GetUser().GetFirstName() != "Grace" || request.GetUser().GetLastName() != "Hopper" {
		t.Fatalf("user request = %#v", request.GetUser())
	}
	if request.GetTemplate().GetType() != "week" || len(request.GetTemplate().GetDays()) != 5 {
		t.Fatalf("schedule request = %#v", request.GetTemplate())
	}
	body := decodeObject(t, recorder)
	if _, ok := body["user"]; !ok {
		t.Fatalf("response has no user: %s", recorder.Body.String())
	}
	if _, ok := body["schedule"]; !ok {
		t.Fatalf("response has no schedule: %s", recorder.Body.String())
	}
}

func (s *stubCompanyServer) Login(ctx context.Context, request *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
	if s.loginFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected Login call")
	}
	return s.loginFn(ctx, request)
}

func (s *stubCompanyServer) Register(ctx context.Context, request *companyv1.RegisterRequest) (*companyv1.RegisterResponse, error) {
	if s.registerFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected Register call")
	}
	return s.registerFn(ctx, request)
}

func (s *stubCompanyServer) Refresh(ctx context.Context, request *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
	if s.refreshFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected Refresh call")
	}
	return s.refreshFn(ctx, request)
}

func (s *stubCompanyServer) GetDepartments(ctx context.Context, request *companyv1.GetDepartmentsRequest) (*companyv1.GetDepartmentsResponse, error) {
	if s.getDepartmentsFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected GetDepartments call")
	}
	return s.getDepartmentsFn(ctx, request)
}

func (s *stubCompanyServer) UpdateDepartment(ctx context.Context, request *companyv1.UpdateDepartmentRequest) (*companyv1.UpdateDepartmentResponse, error) {
	if s.updateDepartmentFn == nil {
		return nil, status.Error(codes.Unimplemented, "unexpected UpdateDepartment call")
	}
	return s.updateDepartmentFn(ctx, request)
}

func TestGatewayLoginBridgesJSONToGRPCAndSetsRefreshCookie(t *testing.T) {
	requests := make(chan *companyv1.LoginRequest, 1)
	server := &stubCompanyServer{
		loginFn: func(_ context.Context, request *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
			requests <- request
			return &companyv1.LoginResponse{Session: testAuthSession("access-login", "refresh-login")}, nil
		},
	}

	recorder := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/login", `{
		"email":"owner@example.com",
		"password":"secret-password"
	}`, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	request := <-requests
	if request.GetEmail() != "owner@example.com" || request.GetPassword() != "secret-password" {
		t.Fatalf("Login RPC request = %#v", request)
	}

	body := decodeObject(t, recorder)
	if got := decodeStringField(t, body, "accessToken"); got != "access-login" {
		t.Fatalf("accessToken = %q, want %q", got, "access-login")
	}
	if _, exposed := body["refreshToken"]; exposed {
		t.Fatal("refreshToken must not be exposed in the JSON response")
	}
	if strings.Contains(recorder.Body.String(), "refresh-login") {
		t.Fatal("refresh token leaked into the JSON response")
	}

	cookie := responseCookie(t, recorder, refreshCookieName)
	if cookie.Value != "refresh-login" {
		t.Fatalf("refresh cookie value = %q, want %q", cookie.Value, "refresh-login")
	}
	if !cookie.HttpOnly {
		t.Fatal("refresh cookie must be HttpOnly")
	}
	if !cookie.Secure {
		t.Fatal("refresh cookie must honor the configured Secure flag")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("refresh cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/api/v1/auth" {
		t.Fatalf("refresh cookie path = %q, want %q", cookie.Path, "/api/v1/auth")
	}
}

func TestGatewayRegisterReturnsCreated(t *testing.T) {
	requests := make(chan *companyv1.RegisterRequest, 1)
	server := &stubCompanyServer{
		registerFn: func(_ context.Context, request *companyv1.RegisterRequest) (*companyv1.RegisterResponse, error) {
			requests <- request
			return &companyv1.RegisterResponse{Session: testAuthSession("access-register", "refresh-register")}, nil
		},
	}

	recorder := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/register", `{
		"companyName":"Acme",
		"email":"owner@example.com",
		"password":"secret-password",
		"firstName":"Ada",
		"lastName":"Lovelace"
	}`, nil)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	request := <-requests
	if request.GetCompanyName() != "Acme" || request.GetEmail() != "owner@example.com" ||
		request.GetPassword() != "secret-password" || request.GetFirstName() != "Ada" || request.GetLastName() != "Lovelace" {
		t.Fatalf("Register RPC request = %#v", request)
	}
	if got := responseCookie(t, recorder, refreshCookieName).Value; got != "refresh-register" {
		t.Fatalf("refresh cookie value = %q, want %q", got, "refresh-register")
	}
}

func TestGatewayRefreshReadsAndRotatesCookie(t *testing.T) {
	requests := make(chan *companyv1.RefreshRequest, 1)
	server := &stubCompanyServer{
		refreshFn: func(_ context.Context, request *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
			requests <- request
			return &companyv1.RefreshResponse{Session: testAuthSession("access-rotated", "refresh-rotated")}, nil
		},
	}

	recorder := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/refresh", "", []*http.Cookie{{
		Name:  refreshCookieName,
		Value: "refresh-original",
	}})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := (<-requests).GetRefreshToken(); got != "refresh-original" {
		t.Fatalf("Refresh RPC token = %q, want %q", got, "refresh-original")
	}
	if got := responseCookie(t, recorder, refreshCookieName).Value; got != "refresh-rotated" {
		t.Fatalf("rotated refresh cookie = %q, want %q", got, "refresh-rotated")
	}
	body := decodeObject(t, recorder)
	if got := decodeStringField(t, body, "accessToken"); got != "access-rotated" {
		t.Fatalf("accessToken = %q, want %q", got, "access-rotated")
	}
	if _, exposed := body["refreshToken"]; exposed || strings.Contains(recorder.Body.String(), "refresh-rotated") {
		t.Fatal("rotated refresh token must only be returned in Set-Cookie")
	}
}

func TestGatewayMapsGRPCErrorsToPublicEnvelope(t *testing.T) {
	tests := []struct {
		name       string
		code       codes.Code
		httpStatus int
		message    string
	}{
		{name: "invalid argument", code: codes.InvalidArgument, httpStatus: http.StatusBadRequest, message: "bad credentials payload"},
		{name: "unauthenticated", code: codes.Unauthenticated, httpStatus: http.StatusUnauthorized, message: "session expired"},
		{name: "not found", code: codes.NotFound, httpStatus: http.StatusNotFound, message: "account not found"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &stubCompanyServer{
				loginFn: func(context.Context, *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
					return nil, status.Error(test.code, test.message)
				},
			}

			recorder := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/login", `{
				"email":"owner@example.com",
				"password":"secret-password"
			}`, nil)

			if recorder.Code != test.httpStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.httpStatus, recorder.Body.String())
			}
			var envelope struct {
				Error struct {
					Message string `json:"message"`
					Status  int    `json:"status"`
				} `json:"error"`
			}
			decodeJSON(t, recorder, &envelope)
			if envelope.Error.Message != test.message || envelope.Error.Status != test.httpStatus {
				t.Fatalf("error envelope = %#v, want message %q and status %d", envelope.Error, test.message, test.httpStatus)
			}
		})
	}
}

func TestGatewayDepartmentsPreserveNullParentAndPatchClearFlags(t *testing.T) {
	parentID := testDepartmentID
	updates := make(chan *companyv1.UpdateDepartmentRequest, 1)
	server := &stubCompanyServer{
		getDepartmentsFn: func(context.Context, *companyv1.GetDepartmentsRequest) (*companyv1.GetDepartmentsResponse, error) {
			return &companyv1.GetDepartmentsResponse{Departments: []*companyv1.Department{
				{Id: testDepartmentID, Name: "Root", Order: 0},
				{Id: testChildID, Name: "Child", ParentId: &parentID, Order: 1},
			}}, nil
		},
		updateDepartmentFn: func(_ context.Context, request *companyv1.UpdateDepartmentRequest) (*companyv1.UpdateDepartmentResponse, error) {
			updates <- request
			return &companyv1.UpdateDepartmentResponse{Department: &companyv1.Department{
				Id: testDepartmentID, Name: "Root", Order: 0,
			}}, nil
		},
	}
	handler := newTestGateway(t, server)

	listRecorder := performRequest(handler, http.MethodGet, "/api/v1/org/departments", "", nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	var departments []map[string]json.RawMessage
	decodeJSON(t, listRecorder, &departments)
	if len(departments) != 2 {
		t.Fatalf("department count = %d, want 2", len(departments))
	}
	if got := strings.TrimSpace(string(departments[0]["parentId"])); got != "null" {
		t.Fatalf("root parentId = %s, want null", got)
	}
	var childParent string
	if err := json.Unmarshal(departments[1]["parentId"], &childParent); err != nil {
		t.Fatalf("decode child parentId: %v", err)
	}
	if childParent != testDepartmentID {
		t.Fatalf("child parentId = %q, want %q", childParent, testDepartmentID)
	}

	patchRecorder := performRequest(handler, http.MethodPatch, "/api/v1/org/departments/"+testDepartmentID, `{
		"headUserId":null,
		"valuableFinalProduct":null
	}`, nil)
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d; body = %s", patchRecorder.Code, http.StatusOK, patchRecorder.Body.String())
	}
	request := <-updates
	if request.GetId() != testDepartmentID {
		t.Fatalf("UpdateDepartment RPC id = %q, want %q", request.GetId(), testDepartmentID)
	}
	if !request.GetClearHeadUserId() || request.HeadUserId != nil {
		t.Fatalf("head user clear mapping = clear:%v value:%v", request.GetClearHeadUserId(), request.HeadUserId)
	}
	if !request.GetClearValuableFinalProduct() || request.ValuableFinalProduct != nil {
		t.Fatalf("valuable final product clear mapping = clear:%v value:%v", request.GetClearValuableFinalProduct(), request.ValuableFinalProduct)
	}

	patched := decodeObject(t, patchRecorder)
	if got := strings.TrimSpace(string(patched["parentId"])); got != "null" {
		t.Fatalf("patched root parentId = %s, want null", got)
	}
}

func TestGatewayRejectsMalformedJSONBeforeRPC(t *testing.T) {
	var called atomic.Bool
	server := &stubCompanyServer{
		loginFn: func(context.Context, *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
			called.Store(true)
			return &companyv1.LoginResponse{Session: testAuthSession("unused", "unused")}, nil
		},
	}

	recorder := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/login", `{"email":`, nil)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if called.Load() {
		t.Fatal("Login RPC must not be called for malformed JSON")
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Status  int    `json:"status"`
		} `json:"error"`
	}
	decodeJSON(t, recorder, &envelope)
	if envelope.Error.Status != http.StatusBadRequest || envelope.Error.Message == "" {
		t.Fatalf("error envelope = %#v", envelope.Error)
	}
}

func newTestGateway(t *testing.T, companyServer companyv1.CompanyServiceServer) http.Handler {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	companyv1.RegisterCompanyServiceServer(grpcServer, companyServer)
	kbv1.RegisterKbServiceServer(grpcServer, kbv1.UnimplementedKbServiceServer{})
	tasksv1.RegisterTasksServiceServer(grpcServer, tasksv1.UnimplementedTasksServiceServer{})
	go func() {
		_ = grpcServer.Serve(listener)
	}()

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	if err != nil {
		grpcServer.Stop()
		_ = listener.Close()
		t.Fatalf("create bufconn gRPC client: %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
		grpcServer.Stop()
		_ = listener.Close()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := companyv1.NewCompanyServiceClient(connection)
	kbClient := kbv1.NewKbServiceClient(connection)
	tasksClient := tasksv1.NewTasksServiceClient(connection)
	academyClient := academyv1.NewAcademyServiceClient(connection)
	return api.Handler(NewHandler(client, kbClient, tasksClient, academyClient, CookieConfig{Secure: true}, logger))
}

func serveGatewayRequest(
	t *testing.T,
	server companyv1.CompanyServiceServer,
	method string,
	path string,
	body string,
	cookies []*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	return performRequest(newTestGateway(t, server), method, path, body, cookies)
}

func performRequest(handler http.Handler, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func testAuthSession(accessToken, refreshToken string) *companyv1.AuthSession {
	return &companyv1.AuthSession{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		RefreshExpiresAt: timestamppb.New(time.Now().Add(2 * time.Hour)),
		User: &companyv1.User{
			Id:          testUserID,
			Email:       "owner@example.com",
			FirstName:   "Ada",
			LastName:    "Lovelace",
			Role:        companyv1.UserRole_USER_ROLE_OWNER,
			Status:      companyv1.UserStatus_USER_STATUS_ACTIVE,
			PositionIds: []string{},
			CreatedAt:   timestamppb.New(time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)),
			Source:      companyv1.UserSource_USER_SOURCE_LOCAL,
		},
	}
}

func responseCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response has no %q cookie; Set-Cookie = %q", name, recorder.Header().Values("Set-Cookie"))
	return nil
}

func decodeObject(t *testing.T, recorder *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	decodeJSON(t, recorder, &object)
	return object
}

func decodeStringField(t *testing.T, object map[string]json.RawMessage, field string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(object[field], &value); err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}
	return value
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response JSON: %v; body = %s", err, recorder.Body.String())
	}
}
