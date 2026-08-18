package authmw

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
)

func TestMiddleware(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer := sharedauth.NewTokenIssuer(privateKey, "issuer", "audience", time.Minute)
	verifier := sharedauth.NewTokenVerifier(publicKey, "issuer", "audience")
	token, _, err := issuer.Issue("user", "company", "admin", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := Middleware(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := Claims(r.Context())
		if !ok || claims.Subject != "user" {
			t.Fatal("claims missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/org/users", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestMiddlewareRejectsMissingToken(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	handler := Middleware(sharedauth.NewTokenVerifier(publicKey, "issuer", "audience"))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") }),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/org/users", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestMiddlewareLeavesProvisioningAuthenticationToHandlers(t *testing.T) {
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := Token(r.Context()); ok {
			t.Fatal("provisioning request must not create a user token")
		}
		if _, ok := Claims(r.Context()); ok {
			t.Fatal("provisioning request must not create user claims")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, requestCase := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/provisioning/companies"},
		{method: http.MethodPost, path: "/api/v1/provisioning/sessions"},
		{method: http.MethodGet, path: "/api/v1/provisioning/companies/status"},
	} {
		t.Run(requestCase.path, func(t *testing.T) {
			request := httptest.NewRequestWithContext(context.Background(), requestCase.method, requestCase.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestProvisioningRoutesAreExact(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodPost, path: "/api/v1/provisioning/companies", want: true},
		{method: http.MethodPost, path: "/api/v1/provisioning/sessions", want: true},
		{method: http.MethodGet, path: "/api/v1/provisioning/companies/status", want: true},
		{method: http.MethodGet, path: "/api/v1/provisioning/companies", want: false},
		{method: http.MethodPost, path: "/api/v1/provisioning/companies/status", want: false},
		{method: http.MethodGet, path: "/api/v1/provisioning/companies/status/", want: false},
		{method: http.MethodPost, path: "/api/v1/provisioning/companies/", want: false},
		{method: http.MethodPost, path: "/api/v1/provisioning/companies/anything", want: false},
		{method: http.MethodPost, path: "/api/v1/provisioning", want: false},
	} {
		if got := isProvisioning(test.method, test.path); got != test.want {
			t.Fatalf("isProvisioning(%q, %q) = %v, want %v", test.method, test.path, got, test.want)
		}
	}
}

func TestBootstrapAndSSOAuthRoutesArePublic(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodGet, path: "/api/v1/auth/bootstrap/opaque-token", want: true},
		{method: http.MethodPost, path: "/api/v1/auth/bootstrap/opaque-token/complete", want: true},
		{method: http.MethodPost, path: "/api/v1/auth/sso/exchange", want: true},
		{method: http.MethodPost, path: "/api/v1/auth/bootstrap/opaque-token", want: false},
		{method: http.MethodGet, path: "/api/v1/auth/bootstrap/opaque-token/complete", want: false},
		{method: http.MethodGet, path: "/api/v1/auth/sso/exchange", want: false},
		{method: http.MethodPost, path: "/api/v1/auth/sso/exchange/extra", want: false},
	} {
		if got := isPublic(test.method, test.path); got != test.want {
			t.Fatalf("isPublic(%q, %q) = %v, want %v", test.method, test.path, got, test.want)
		}
	}
}

func TestAccessLinkLoginIsPublic(t *testing.T) {
	if !isPublic(http.MethodPost, "/api/v1/auth/access-link/opaque-token") {
		t.Fatal("access-link login must be public")
	}
	if isPublic(http.MethodGet, "/api/v1/auth/access-link/opaque-token") {
		t.Fatal("only POST access-link login may be public")
	}
}

func TestCompanyRegistrationChecksArePublic(t *testing.T) {
	if !isPublic(http.MethodGet, "/api/v1/public/amocrm/accounts/31355990/exists") {
		t.Fatal("amoCRM account check must be public")
	}
	if !isPublic(http.MethodPost, "/api/v1/public/company-registration-tokens/validate") {
		t.Fatal("company registration token validation must be public")
	}
	if !isPublic(http.MethodPost, "/api/v1/auth/registration-logins") {
		t.Fatal("registration login reservation must be public")
	}
	if !isPublic(http.MethodPost, "/api/v2/auth/login") {
		t.Fatal("v2 login must be public")
	}
	if isPublic(http.MethodPost, "/api/v1/public/amocrm/accounts/31355990/exists") {
		t.Fatal("amoCRM account check only permits GET")
	}
}

func TestAmoWidgetSessionRoutesArePublic(t *testing.T) {
	for _, path := range []string{
		"/api/v1/public/amocrm/widget-sessions",
		"/api/v1/public/amocrm/widget-sessions/validate",
		"/api/v1/auth/amocrm/complete",
	} {
		if !isPublic(http.MethodPost, path) {
			t.Fatalf("POST %s must not require internal TeamOS JWT", path)
		}
		if isPublic(http.MethodPut, path) {
			t.Fatalf("PUT %s must remain protected", path)
		}
		if isPublic(http.MethodPost, path+"/extra") {
			t.Fatalf("POST %s/extra must remain protected", path)
		}
	}
	if isPublic(http.MethodGet, "/api/v1/auth/amocrm/complete") {
		t.Fatal("GET /api/v1/auth/amocrm/complete must remain protected")
	}
}

func TestAmoAdminSessionUsesServiceAuthentication(t *testing.T) {
	const path = "/api/v1/provisioning/amocrm/admin-sessions"
	if !isPublic(http.MethodPost, path) {
		t.Fatal("service-auth route must bypass internal user JWT middleware")
	}
	if isPublic(http.MethodGet, path) || isPublic(http.MethodPost, path+"/extra") {
		t.Fatal("only the exact POST service-auth route may bypass the user JWT middleware")
	}
}

func TestImpersonationRequiresInternalBearer(t *testing.T) {
	if isPublic(http.MethodPost, "/api/v1/auth/impersonate") {
		t.Fatal("impersonation must remain protected by the internal JWT")
	}
}

func TestPublicContentResolversArePublic(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/api/v1/public/academy/courses/00000000-0000-0000-0000-000000000001",
		"/api/v1/public/kb/articles/00000000-0000-0000-0000-000000000001",
	} {
		if !isPublic(http.MethodGet, path) {
			t.Fatalf("GET %s must be public", path)
		}
		if isPublic(http.MethodPost, path) {
			t.Fatalf("POST %s must remain protected", path)
		}
	}
}

func TestExternalAcademyMutationsDoNotRequireInternalJWT(t *testing.T) {
	paths := []string{
		"/api/v1/public/academy/access/token/request-verification",
		"/api/v1/public/academy/access/token/activate",
		"/api/v1/public/academy/verifications/00000000-0000-0000-0000-000000000001/confirm",
		"/api/v1/public/academy/enrollments/00000000-0000-0000-0000-000000000001/lessons/00000000-0000-0000-0000-000000000002/complete",
	}
	for _, path := range paths {
		if !isPublic(http.MethodPost, path) {
			t.Fatalf("POST %s должен использовать external session, а не internal JWT", path)
		}
	}
	if isPublic(http.MethodPost, "/api/v1/public/kb/articles/id") {
		t.Fatal("публичные mutation KB не разрешены")
	}
}
