package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		incoming   string
		wantExact  string
		wantLength int
	}{
		{name: "preserves caller ID", incoming: "edge-123", wantExact: "edge-123"},
		{name: "generates UUID", wantLength: 36},
		{name: "replaces oversized ID", incoming: strings.Repeat("x", 129), wantLength: 36},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var contextID string
			handler := httpx.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				contextID = httpx.RequestIDFromContext(r.Context())
			}))
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			request.Header.Set(httpx.RequestIDHeader, tt.incoming)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if tt.wantExact != "" && contextID != tt.wantExact {
				t.Fatalf("request id = %q, want %q", contextID, tt.wantExact)
			}
			if tt.wantLength != 0 && len(contextID) != tt.wantLength {
				t.Fatalf("request id length = %d, want %d", len(contextID), tt.wantLength)
			}
			if response.Header().Get(httpx.RequestIDHeader) != contextID {
				t.Fatal("response and context request IDs differ")
			}
		})
	}
}

func TestRecovererWritesSafeAPIError(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := httpx.Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret panic") }),
		httpx.RequestID,
		httpx.Recoverer(logger),
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}

	var envelope apierror.Envelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Error.Message != apierror.DefaultInternalMessage {
		t.Fatalf("message = %q", envelope.Error.Message)
	}
	if !strings.Contains(logs.String(), "secret panic") || !strings.Contains(logs.String(), "request_id") {
		t.Fatalf("panic was not logged with context: %s", logs.String())
	}
}

func TestLoggingRecordsResponse(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	withCompany := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := httpx.WithLogAttributes(r.Context(), slog.String("company_id", "company-1"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	handler := httpx.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "ok")
		}),
		httpx.RequestID,
		withCompany,
		httpx.Logging(logger),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users", nil))
	logLine := logs.String()
	for _, expected := range []string{`"method":"POST"`, `"path":"/users"`, `"status":201`, `"response_bytes":2`, `"company_id":"company-1"`} {
		if !strings.Contains(logLine, expected) {
			t.Fatalf("log does not contain %s: %s", expected, logLine)
		}
	}
}

func TestLoggingRedactsInviteToken(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := httpx.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/invites/secret-token/accept", nil,
	)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(logs.String(), "secret-token") || !strings.Contains(logs.String(), ":token/accept") {
		t.Fatalf("invite token was not redacted: %s", logs.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	type input struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name     string
		body     string
		limit    int64
		wantErr  string
		wantName string
	}{
		{name: "valid", body: `{"name":"Команда"}`, wantName: "Команда"},
		{name: "empty", body: "", wantErr: "Тело запроса не должно быть пустым"},
		{name: "additive unknown field", body: `{"name":"Команда","futureOptionalField":true}`, wantName: "Команда"},
		{name: "multiple values", body: `{"name":"A"}{"name":"B"}`, wantErr: "Тело запроса должно содержать один JSON-объект"},
		{name: "too large", body: `{"name":"Команда"}`, limit: 5, wantErr: "Тело запроса превышает допустимый размер"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			var decoded input
			err := httpx.DecodeJSON(response, request, &decoded, tt.limit)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if decoded.Name != tt.wantName {
					t.Fatalf("decoded name = %q, want %q", decoded.Name, tt.wantName)
				}
				return
			}
			if err == nil || err.Message != tt.wantErr {
				t.Fatalf("error = %#v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestReadiness(t *testing.T) {
	t.Parallel()

	handler := httpx.Readyz(map[string]httpx.ReadinessCheck{
		"postgres": func(context.Context) error { return nil },
		"nats":     func(context.Context) error { return errors.New("offline") },
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"status":"not_ready"`) || !strings.Contains(response.Body.String(), `"nats":"failed"`) {
		t.Fatalf("unexpected readiness response: %s", response.Body.String())
	}
}
