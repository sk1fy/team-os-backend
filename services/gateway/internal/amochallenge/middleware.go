package amochallenge

import (
	"context"
	"net/http"
	"strings"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

type contextKey uint8

const claimsKey contextKey = iota + 1

func Middleware(manager *Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expectedPurpose := ""
			if r.Method == http.MethodPost {
				switch r.URL.Path {
				case "/api/v1/public/amocrm/admin-self-login":
					expectedPurpose = PurposeAdminSelfLogin
				case "/api/v1/public/amocrm/company-bootstrap":
					expectedPurpose = PurposeCompanyBootstrap
				}
			}
			if expectedPurpose == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			if manager == nil {
				apierror.Write(w, apierror.New(http.StatusNotFound, "Самостоятельный вход из amoCRM отключён").WithCode("AMO_ADMIN_SELF_LOGIN_DISABLED"))
				return
			}
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				apierror.Write(w, apierror.Unauthorized("Challenge amoCRM недействителен").WithCode("AMO_BROWSER_CHALLENGE_INVALID"))
				return
			}
			claims, err := manager.VerifyAndConsumeForPurpose(parts[1], expectedPurpose)
			if err != nil {
				apierror.Write(w, apierror.Unauthorized("Challenge amoCRM недействителен или истёк").WithCode("AMO_BROWSER_CHALLENGE_INVALID"))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func FromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(Claims)
	return claims, ok
}
