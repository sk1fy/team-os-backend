package authmw

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
)

type contextKey uint8

const (
	claimsKey contextKey = iota + 1
	tokenKey
)

// Middleware validates every protected REST call before it reaches the BFF
// handler. The original bearer token is forwarded to company, which verifies
// it again rather than trusting gateway-provided identity headers.
func Middleware(verifier *sharedauth.TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions || isPublic(r.Method, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			raw, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				apierror.Write(w, apierror.Unauthorized())
				return
			}
			claims, err := verifier.Verify(raw)
			if err != nil {
				apierror.Write(w, apierror.Unauthorized("Токен недействителен или истёк"))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			ctx = context.WithValue(ctx, tokenKey, raw)
			ctx = httpx.WithLogAttributes(
				ctx,
				slog.String("company_id", claims.CompanyID),
				slog.String("user_id", claims.Subject),
			)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Claims(ctx context.Context) (*sharedauth.Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*sharedauth.Claims)
	return claims, ok
}

func Token(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(tokenKey).(string)
	return token, ok
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func isPublic(method, path string) bool {
	switch {
	case method == http.MethodPost && path == "/api/v1/auth/login":
		return true
	case method == http.MethodPost && strings.HasPrefix(path, "/api/v1/auth/access-link/"):
		return true
	case method == http.MethodPost && path == "/api/v1/auth/register":
		return true
	case method == http.MethodPost && path == "/api/v1/auth/refresh":
		return true
	case method == http.MethodPost && path == "/api/v1/auth/logout":
		return true
	case strings.HasPrefix(path, "/api/v1/auth/invites/") && (method == http.MethodGet || method == http.MethodPost):
		return true
	default:
		return false
	}
}
