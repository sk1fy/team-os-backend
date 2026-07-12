package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	GRPCAddr        string
	DatabaseURL     string
	NATSURL         string
	JWTPublicKey    string
	JWTIssuer       string
	JWTAudience     string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:        envOr("KB_HTTP_ADDR", ":8082"),
		GRPCAddr:        envOr("KB_GRPC_ADDR", ":9082"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("KB_DB_URL")),
		NATSURL:         envOr("KB_NATS_URL", "nats://localhost:4222"),
		JWTPublicKey:    strings.TrimSpace(os.Getenv("KB_JWT_PUBLIC_KEY")),
		JWTIssuer:       envOr("KB_JWT_ISSUER", "teamos-company"),
		JWTAudience:     envOr("KB_JWT_AUDIENCE", "teamos-api"),
		ShutdownTimeout: 30 * time.Second,
	}
	var err error
	if value := strings.TrimSpace(os.Getenv("KB_SHUTDOWN_TIMEOUT")); value != "" {
		config.ShutdownTimeout, err = time.ParseDuration(value)
		if err != nil || config.ShutdownTimeout <= 0 {
			return Config{}, fmt.Errorf("KB_SHUTDOWN_TIMEOUT: %w", errInvalidDuration)
		}
	}
	missing := make([]string, 0, 2)
	if config.DatabaseURL == "" {
		missing = append(missing, "KB_DB_URL")
	}
	if config.JWTPublicKey == "" {
		missing = append(missing, "KB_JWT_PUBLIC_KEY")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("не заданы обязательные переменные: %s", strings.Join(missing, ", "))
	}
	return config, nil
}

var errInvalidDuration = errors.New("ожидается положительная длительность")

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}