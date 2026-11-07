package transport

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type authCookieServer struct {
	*stubCompanyServer
	logout func(context.Context, *companyv1.LogoutRequest) (*companyv1.LogoutResponse, error)
}

func (s *authCookieServer) Logout(ctx context.Context, req *companyv1.LogoutRequest) (*companyv1.LogoutResponse, error) {
	return s.logout(ctx, req)
}

func TestAuthCookieLateResponsesCannotReplaceNewLogin(t *testing.T) {
	for _, action := range []string{"legacy refresh", "family refresh", "legacy logout", "family logout"} {
		t.Run(action, func(t *testing.T) {
			familyA, familyB := uuid.NewString(), uuid.NewString()
			legacy := action == "legacy refresh" || action == "legacy logout"
			logout := action == "legacy logout" || action == "family logout"
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			origin, _ := url.Parse("https://example.test/api/v1/auth/refresh")
			initial := []*http.Cookie{{Name: refreshCookieName, Value: "legacy-A", Path: "/api/v1/auth", Secure: true}}
			if !legacy {
				initial = append(initial, &http.Cookie{Name: sessionSelectorCookie, Value: familyA, Path: "/api/v1/auth", Secure: true}, &http.Cookie{Name: refreshCookieName + "_" + familyA, Value: "family-A", Path: "/api/v1/auth", Secure: true})
			}
			jar.SetCookies(origin, initial)
			server := &authCookieServer{stubCompanyServer: &stubCompanyServer{
				loginFn: func(context.Context, *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
					session := testAuthSession("access-B", "refresh-B")
					session.FamilyId = familyB
					return &companyv1.LoginResponse{Session: session}, nil
				},
				refreshFn: func(_ context.Context, req *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
					family, token := familyA, "refresh-A-next"
					if req.GetFamilyId() == familyB {
						if req.GetRefreshToken() != "refresh-B" {
							t.Errorf("new family cookie overwritten")
						}
						family, token = familyB, "refresh-B-next"
					}
					session := testAuthSession("access", token)
					session.FamilyId = family
					return &companyv1.RefreshResponse{Session: session}, nil
				},
			}, logout: func(_ context.Context, req *companyv1.LogoutRequest) (*companyv1.LogoutResponse, error) {
				if legacy && req.FamilyId != nil {
					t.Error("legacy logout routed to selected family")
				}
				if !legacy && req.GetFamilyId() != familyA {
					t.Error("logout lost captured family")
				}
				return &companyv1.LogoutResponse{}, nil
			}}
			handler := newTestGateway(t, server)
			path := "/api/v1/auth/refresh"
			if logout {
				path = "/api/v1/auth/logout"
			}
			late := performRequest(handler, http.MethodPost, path, "", jar.Cookies(origin))
			if late.Code != http.StatusOK && late.Code != http.StatusNoContent {
				t.Fatalf("old operation failed: %d %s", late.Code, late.Body.String())
			}
			for _, c := range late.Result().Cookies() {
				if c.Name == sessionSelectorCookie {
					t.Fatal("refresh/logout touched selector")
				}
			}
			fresh := performRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"login":"tm8901912","password":"secret-password"}`, jar.Cookies(origin))
			if fresh.Code != http.StatusOK {
				t.Fatalf("login failed: %d %s", fresh.Code, fresh.Body.String())
			}
			jar.SetCookies(origin, fresh.Result().Cookies())
			jar.SetCookies(origin, late.Result().Cookies())
			selected := ""
			for _, c := range jar.Cookies(origin) {
				if c.Name == sessionSelectorCookie {
					selected = c.Value
				}
			}
			if selected != familyB {
				t.Fatal("late response replaced selected family")
			}
			verified := performRequest(handler, http.MethodPost, "/api/v1/auth/refresh", "", jar.Cookies(origin))
			if verified.Code != http.StatusOK {
				t.Fatalf("new family not refreshable: %d %s", verified.Code, verified.Body.String())
			}
			body := decodeObject(t, verified)
			if decodeStringField(t, body, "sessionId") != familyB {
				t.Fatal("refresh did not bind response to selected family")
			}
		})
	}
}

func TestAuthCookieSelectorPresenceProhibitsLegacyFallback(t *testing.T) {
	for _, selector := range []string{"", "malformed", uuid.NewString()} {
		t.Run(selector, func(t *testing.T) {
			server := &stubCompanyServer{refreshFn: func(context.Context, *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
				t.Error("legacy fallback reached RPC")
				return nil, status.Error(codes.Unauthenticated, "invalid")
			}}
			response := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/refresh", "", []*http.Cookie{{Name: sessionSelectorCookie, Value: selector}, {Name: refreshCookieName, Value: "valid-legacy"}})
			if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
				t.Fatalf("selector fallback response: %d %v", response.Code, response.Header())
			}
		})
	}
}

func TestAuthCookieErrorsNeverDeleteCookies(t *testing.T) {
	for _, rpcCode := range []codes.Code{codes.Unauthenticated, codes.Unavailable, codes.DeadlineExceeded} {
		for _, action := range []string{"refresh", "logout"} {
			t.Run(action+rpcCode.String(), func(t *testing.T) {
				server := &authCookieServer{stubCompanyServer: &stubCompanyServer{refreshFn: func(context.Context, *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
					return nil, status.Error(rpcCode, "unavailable")
				}}, logout: func(context.Context, *companyv1.LogoutRequest) (*companyv1.LogoutResponse, error) {
					return nil, status.Error(rpcCode, "unavailable")
				}}
				response := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/"+action, "", []*http.Cookie{{Name: refreshCookieName, Value: "refresh"}})
				if response.Code < 400 || len(response.Result().Cookies()) != 0 {
					t.Fatalf("failed operation changed cookies: %d %v", response.Code, response.Header())
				}
				if response.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal("credential response is cacheable")
				}
			})
		}
	}
}

func TestAuthPendingLogoutUsesCapturedFamilyAfterNewLogin(t *testing.T) {
	for _, captured := range []string{legacySessionID, uuid.NewString()} {
		t.Run(captured, func(t *testing.T) {
			selected := uuid.NewString()
			server := &authCookieServer{stubCompanyServer: &stubCompanyServer{}, logout: func(_ context.Context, req *companyv1.LogoutRequest) (*companyv1.LogoutResponse, error) {
				if req.GetRefreshToken() != "old-token" || req.GetFamilyId() == selected {
					t.Fatal("pending logout targeted new login")
				}
				if captured != legacySessionID && req.GetFamilyId() != captured {
					t.Fatal("pending logout lost family")
				}
				return &companyv1.LogoutResponse{}, nil
			}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/logout", nil)
			req.Header.Set(sessionRoutingHeader, captured)
			req.AddCookie(&http.Cookie{Name: sessionSelectorCookie, Value: selected})
			req.AddCookie(&http.Cookie{Name: refreshCookieName + "_" + selected, Value: "new-token"})
			oldCookie := refreshCookieName
			if captured != legacySessionID {
				oldCookie += "_" + captured
			}
			req.AddCookie(&http.Cookie{Name: oldCookie, Value: "old-token"})
			response := httptest.NewRecorder()
			newTestGateway(t, server).ServeHTTP(response, req)
			if response.Code != http.StatusNoContent {
				t.Fatalf("logout status %d", response.Code)
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != oldCookie || cookies[0].MaxAge >= 0 {
				t.Fatalf("wrong cookie cleared: %v", cookies)
			}
		})
	}
}

func TestAuthRefreshRejectsMismatchedFamilyResponse(t *testing.T) {
	server := &stubCompanyServer{refreshFn: func(context.Context, *companyv1.RefreshRequest) (*companyv1.RefreshResponse, error) {
		return &companyv1.RefreshResponse{Session: testAuthSession("access", "secret")}, nil
	}}
	family := uuid.NewString()
	response := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/refresh", "", []*http.Cookie{{Name: sessionSelectorCookie, Value: family}, {Name: refreshCookieName + "_" + family, Value: "token"}})
	if response.Code < 400 || len(response.Result().Cookies()) != 0 {
		t.Fatal("mismatched family response installed credentials")
	}
}

func TestAuthSelectorLifetimeIsIndependentOfRefreshSecret(t *testing.T) {
	server := &stubCompanyServer{loginFn: func(context.Context, *companyv1.LoginRequest) (*companyv1.LoginResponse, error) {
		return &companyv1.LoginResponse{Session: testAuthSession("access", "refresh")}, nil
	}}
	response := serveGatewayRequest(t, server, http.MethodPost, "/api/v1/auth/login", `{"login":"tm8901912","password":"secret-password"}`, nil)
	selector := responseCookie(t, response, sessionSelectorCookie)
	secret := responseCookie(t, response, refreshCookieName+"_"+testFamilyID)
	if selector.MaxAge < int((sessionSelectorTTL-time.Hour).Seconds()) || selector.MaxAge > int(sessionSelectorTTL.Seconds()) {
		t.Fatal("selector lacks fixed independent routing lifetime")
	}
	if secret.MaxAge > int((2*time.Hour).Seconds()) || !selector.Expires.After(secret.Expires) {
		t.Fatal("selector extended secret lifetime")
	}
}
