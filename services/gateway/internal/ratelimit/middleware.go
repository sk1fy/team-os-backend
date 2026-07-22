package ratelimit

import (
	"net"
	"net/http"
	"net/netip"
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
	mu             sync.Mutex
	entries        map[string]entry
	limit          int
	window         time.Duration
	now            func() time.Time
	lastGC         time.Time
	trustedProxies []netip.Prefix
}

func New(limit int, window time.Duration, trustedProxies ...netip.Prefix) *Limiter {
	if limit <= 0 {
		limit = 30
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{entries: make(map[string]entry), limit: limit, window: window, now: time.Now, trustedProxies: append([]netip.Prefix(nil), trustedProxies...)}
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !rateLimitedPath(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		allowed, retryAfter := l.allow(l.clientIP(r))
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Round(time.Second).Seconds()))))
			apierror.Write(w, apierror.New(http.StatusTooManyRequests, "Слишком много запросов. Повторите позже."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimitedPath(method, path string) bool {
	return strings.HasPrefix(path, "/api/v1/auth/") ||
		(method == http.MethodPost && strings.HasPrefix(path, "/api/v1/public/academy/"))
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

func (l *Limiter) clientIP(r *http.Request) string {
	peer := clientIP(r.RemoteAddr)
	peerAddress, err := netip.ParseAddr(peer)
	if err != nil || !contains(l.trustedProxies, peerAddress.Unmap()) {
		return peer
	}
	forwardedValue := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedValue != "" {
		forwarded := strings.Split(forwardedValue, ",")
		addresses := make([]netip.Addr, len(forwarded))
		for index, value := range forwarded {
			address, parseErr := netip.ParseAddr(strings.TrimSpace(value))
			if parseErr != nil {
				return peer
			}
			addresses[index] = address.Unmap()
		}
		// Walk from the proxy nearest to us towards the client. This remains
		// safe when a trusted proxy appends to an existing X-Forwarded-For value.
		for index := len(addresses) - 1; index >= 0; index-- {
			if !contains(l.trustedProxies, addresses[index]) {
				return addresses[index].String()
			}
		}
	}
	if address, parseErr := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); parseErr == nil {
		return address.Unmap().String()
	}
	return peer
}

func contains(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
