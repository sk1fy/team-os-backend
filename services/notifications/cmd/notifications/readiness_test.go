package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadinessSuppliesNATSDeadline(t *testing.T) {
	var deadline time.Time
	handler := readinessHandler(func(context.Context) error { return nil }, func(ctx context.Context) error {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok {
			return errors.New("NATS flush requires a deadline")
		}
		return nil
	})
	before := time.Now()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("healthy dependencies returned %d: %s", response.Code, response.Body.String())
	}
	if deadline.Before(before) || deadline.After(time.Now().Add(time.Second)) {
		t.Fatalf("NATS check has an invalid deadline: %v", deadline)
	}
}

func TestReadinessReportsNATSFailure(t *testing.T) {
	handler := readinessHandler(func(context.Context) error { return nil }, func(context.Context) error {
		return errors.New("NATS unavailable")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable NATS returned %d", response.Code)
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "not_ready" || body.Checks["nats"] != "failed" || body.Checks["postgres"] != "ok" {
		t.Fatalf("unexpected readiness response: %+v", body)
	}
}

func TestReadinessPreservesRequestContext(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "earlier deadline"
		if canceled {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if canceled {
				cancel()
			}
			parentDeadline, _ := ctx.Deadline()
			called := false
			handler := readinessHandler(func(context.Context) error { return nil }, func(checkContext context.Context) error {
				called = true
				deadline, _ := checkContext.Deadline()
				if deadline.After(parentDeadline) {
					t.Fatal("NATS check extended the request deadline")
				}
				if canceled && !errors.Is(checkContext.Err(), context.Canceled) {
					t.Fatal("request cancellation did not reach the NATS check")
				}
				return checkContext.Err()
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx))
			if !called {
				t.Fatal("NATS check was not called")
			}
			want := http.StatusOK
			if canceled {
				want = http.StatusServiceUnavailable
			}
			if response.Code != want {
				t.Fatalf("status = %d, want %d", response.Code, want)
			}
		})
	}
}
