package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr, GRPCAddr, DatabaseURL, NATSURL, JWTPublicKey, JWTIssuer, JWTAudience string
	ShutdownTimeout                                                                time.Duration
}

func Load() (Config, error) {
	c := Config{HTTPAddr: env("NOTIFICATIONS_HTTP_ADDR", ":8085"), GRPCAddr: env("NOTIFICATIONS_GRPC_ADDR", ":9085"), DatabaseURL: strings.TrimSpace(os.Getenv("NOTIFICATIONS_DB_URL")), NATSURL: env("NOTIFICATIONS_NATS_URL", "nats://localhost:4222"), JWTPublicKey: strings.TrimSpace(os.Getenv("NOTIFICATIONS_JWT_PUBLIC_KEY")), JWTIssuer: env("NOTIFICATIONS_JWT_ISSUER", "teamos-company"), JWTAudience: env("NOTIFICATIONS_JWT_AUDIENCE", "teamos-api"), ShutdownTimeout: 30 * time.Second}
	if c.DatabaseURL == "" || c.JWTPublicKey == "" {
		return Config{}, fmt.Errorf("не заданы обязательные переменные: NOTIFICATIONS_DB_URL, NOTIFICATIONS_JWT_PUBLIC_KEY")
	}
	return c, nil
}
func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
