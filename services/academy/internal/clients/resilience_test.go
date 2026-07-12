package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCallWithResilienceRetriesTransientFailure(t *testing.T) {
	breaker := newCircuitBreaker()
	calls := 0
	result, err := callWithResilience(context.Background(), "", breaker, func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", status.Error(codes.Unavailable, "temporary")
		}
		return "ok", nil
	})
	if err != nil || result != "ok" || calls != 2 {
		t.Fatalf("result=%q calls=%d err=%v", result, calls, err)
	}
}

func TestCallWithResilienceDoesNotRetryPermanentFailure(t *testing.T) {
	breaker := newCircuitBreaker()
	calls := 0
	_, err := callWithResilience(context.Background(), "", breaker, func(context.Context) (string, error) {
		calls++
		return "", status.Error(codes.PermissionDenied, "denied")
	})
	if status.Code(err) != codes.PermissionDenied || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	breaker := newCircuitBreaker()
	now := time.Now()
	breaker.now = func() time.Time { return now }
	breaker.threshold = 2
	breaker.cooldown = time.Second
	operation := func(context.Context) (string, error) {
		return "", status.Error(codes.Unavailable, "down")
	}
	for range 2 {
		_, _ = callWithResilience(context.Background(), "", breaker, operation)
	}
	_, err := callWithResilience(context.Background(), "", breaker, operation)
	if !errors.Is(err, errCircuitOpen) {
		t.Fatalf("ожидался открытый circuit breaker, получено %v", err)
	}
	now = now.Add(2 * time.Second)
	_, err = callWithResilience(context.Background(), "", breaker, func(context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("circuit breaker не восстановился: %v", err)
	}
}
