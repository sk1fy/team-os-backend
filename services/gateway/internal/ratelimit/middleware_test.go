package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestLimiterRejectsExcessAuthRequests(t *testing.T) {
	limiter := New(2, time.Minute)
	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for index, wantStatus := range []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf("request %d status = %d, want %d", index, response.Code, wantStatus)
		}
	}
}

func TestLimiterUsesForwardedIPOnlyFromTrustedProxy(t *testing.T) {
	trusted := netip.MustParsePrefix("10.0.0.0/8")
	limiter := New(1, time.Minute, trusted)
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.99, 192.0.2.10")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("trusted proxy status = %d", response.Code)
	}

	request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.99, 192.0.2.11")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("different forwarded client status = %d", response.Code)
	}

	request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "198.51.100.1:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("untrusted peer first status = %d", response.Code)
	}
	request.Header.Set("X-Forwarded-For", "203.0.113.2")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("untrusted peer second status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
}

func TestLimiterIgnoresNonAuthRoutes(t *testing.T) {
	limiter := New(1, time.Minute)
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/org/users", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d", response.Code)
		}
	}
}
