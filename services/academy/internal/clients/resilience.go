package clients

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	rpcTimeout       = 500 * time.Millisecond
	rpcAttempts      = 2
	retryDelay       = 25 * time.Millisecond
	breakerThreshold = 3
	breakerCooldown  = 10 * time.Second
)

var errCircuitOpen = errors.New("circuit breaker is open")

type circuitBreaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	now       func() time.Time
	threshold int
	cooldown  time.Duration
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{now: time.Now, threshold: breakerThreshold, cooldown: breakerCooldown}
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.openUntil.IsZero() {
		return true
	}
	if now.Before(b.openUntil) {
		return false
	}
	b.openUntil = time.Time{}
	b.failures = 0
	return true
}

func (b *circuitBreaker) success() {
	b.mu.Lock()
	b.failures = 0
	b.openUntil = time.Time{}
	b.mu.Unlock()
}

func (b *circuitBreaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = b.now().Add(b.cooldown)
	}
}

func callWithResilience[T any](
	ctx context.Context,
	token string,
	breaker *circuitBreaker,
	operation func(context.Context) (T, error),
) (T, error) {
	var zero T
	if !breaker.allow() {
		return zero, errCircuitOpen
	}
	callContext, cancel := outgoing(ctx, token)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < rpcAttempts; attempt++ {
		result, err := operation(callContext)
		if err == nil {
			breaker.success()
			return result, nil
		}
		lastErr = err
		if !isRetryableRPC(err) || attempt+1 == rpcAttempts {
			break
		}
		timer := time.NewTimer(retryDelay * time.Duration(attempt+1))
		select {
		case <-callContext.Done():
			timer.Stop()
			lastErr = callContext.Err()
			attempt = rpcAttempts
		case <-timer.C:
		}
	}
	if isRetryableRPC(lastErr) || errors.Is(lastErr, context.DeadlineExceeded) {
		breaker.failure()
	}
	return zero, lastErr
}

func isRetryableRPC(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

func outgoing(ctx context.Context, token string) (context.Context, context.CancelFunc) {
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	return context.WithTimeout(ctx, rpcTimeout)
}
