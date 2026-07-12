package ratelimit

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

type entry struct {
	windowStart time.Time
	count       int
}

// Limiter is a disposable, per-replica auth abuse guard. Losing its state on
// restart does not affect correctness; a shared Redis limiter can replace it
// when gateway replicas require a global quota.
type Limiter struct {
	mu      sync.Mutex
	entries map[string]entry
	limit   int
	window  time.Duration
	now     func() time.Time
	lastGC  time.Time
}

func New(limit int, window time.Duration) *Limiter {
	if limit <= 0 {
		limit = 30
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{entries: make(map[string]entry), limit: limit, window: window, now: time.Now}
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		allowed, retryAfter := l.allow(clientIP(r.RemoteAddr))
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Round(time.Second).Seconds()))))
			apierror.Write(w, apierror.New(http.StatusTooManyRequests, "Слишком много запросов. Повторите позже."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastGC.IsZero() || now.Sub(l.lastGC) >= l.window {
		for candidate, value := range l.entries {
			if now.Sub(value.windowStart) >= l.window {
				delete(l.entries, candidate)
			}
		}
		l.lastGC = now
	}
	value := l.entries[key]
	if value.windowStart.IsZero() || now.Sub(value.windowStart) >= l.window {
		l.entries[key] = entry{windowStart: now, count: 1}
		return true, 0
	}
	if value.count >= l.limit {
		return false, l.window - now.Sub(value.windowStart)
	}
	value.count++
	l.entries[key] = value
	return true, 0
}

func clientIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil && host != "" {
		return host
	}
	return remoteAddress
}
