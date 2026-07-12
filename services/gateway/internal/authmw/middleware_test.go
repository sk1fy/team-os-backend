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
