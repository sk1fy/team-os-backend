package httpx

import (
	"context"
	"net/http"
)

// ReadinessCheck verifies one dependency required for serving traffic.
type ReadinessCheck func(context.Context) error

// Healthz is a process-liveness handler. It intentionally performs no I/O.
func Healthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// Readyz runs dependency checks and returns 503 until all of them succeed.
func Readyz(checks map[string]ReadinessCheck) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		result := make(map[string]string, len(checks))
		for name, check := range checks {
			if check == nil || check(r.Context()) != nil {
				status = http.StatusServiceUnavailable
				result[name] = "failed"
				continue
			}
			result[name] = "ok"
		}

		state := "ok"
		if status != http.StatusOK {
			state = "not_ready"
		}
		_ = WriteJSON(w, status, map[string]any{"status": state, "checks": result})
	})
}
