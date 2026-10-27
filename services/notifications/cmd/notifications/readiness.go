package main

import (
	"context"
	"net/http"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/httpx"
)

func readinessHandler(postgresCheck, natsCheck httpx.ReadinessCheck) http.Handler {
	return httpx.Readyz(map[string]httpx.ReadinessCheck{
		"postgres": postgresCheck,
		"nats": func(ctx context.Context) error {
			checkContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return natsCheck(checkContext)
		},
	})
}
