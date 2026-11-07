package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestActivityCORSPreflight(t *testing.T) {
	const allowedOrigin = "http://localhost:5173"
	requestedHeaders := []string{"authorization", "content-type", "idempotency-key", "x-activity-binding-id", "x-teamos-session"}
	for _, test := range []struct {
		name   string
		origin string
		method string
		allow  bool
	}{
		{name: "allowed read", origin: allowedOrigin, method: http.MethodGet, allow: true},
		{name: "allowed write", origin: allowedOrigin, method: http.MethodPatch, allow: true},
		{name: "unapproved origin", origin: "https://unapproved.example", method: http.MethodPatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := corsMiddleware([]string{allowedOrigin})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("preflight reached the application handler")
			}))
			request := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/activity-control/panels", nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Access-Control-Request-Method", test.method)
			request.Header.Set("Access-Control-Request-Headers", strings.Join(requestedHeaders, ","))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if !test.allow {
				if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
					t.Errorf("unapproved origin allowed: %q", got)
				}
				return
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
			}
			if got := response.Header().Get("Access-Control-Allow-Methods"); got != test.method {
				t.Errorf("Access-Control-Allow-Methods = %q, want %q", got, test.method)
			}
			if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
				t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
			}
			allowedHeaders := strings.Split(response.Header().Get("Access-Control-Allow-Headers"), ",")
			for _, header := range requestedHeaders {
				if !slices.ContainsFunc(allowedHeaders, func(allowed string) bool {
					return strings.EqualFold(strings.TrimSpace(allowed), header)
				}) {
					t.Errorf("Access-Control-Allow-Headers = %q, missing %q", allowedHeaders, header)
				}
			}
		})
	}
}
