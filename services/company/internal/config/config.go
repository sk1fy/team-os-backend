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
	JWTPrivateKey   string
	JWTIssuer       string
	JWTAudience     string
	AccessTTL       time.Duration
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:        envOr("COMPANY_HTTP_ADDR", ":8081"),
		GRPCAddr:        envOr("COMPANY_GRPC_ADDR", ":9081"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("COMPANY_DB_URL")),
		NATSURL:         envOr("COMPANY_NATS_URL", "nats://localhost:4222"),
		JWTPrivateKey:   strings.TrimSpace(os.Getenv("COMPANY_JWT_PRIVATE_KEY")),
		JWTIssuer:       envOr("COMPANY_JWT_ISSUER", "teamos-company"),
		JWTAudience:     envOr("COMPANY_JWT_AUDIENCE", "teamos-api"),
		AccessTTL:       15 * time.Minute,
		ShutdownTimeout: 30 * time.Second,
	}
	var err error
	if value := strings.TrimSpace(os.Getenv("COMPANY_ACCESS_TTL")); value != "" {
		config.AccessTTL, err = time.ParseDuration(value)
		if err != nil || config.AccessTTL <= 0 {
			return Config{}, fmt.Errorf("COMPANY_ACCESS_TTL: %w", errInvalidDuration)
		}
	}
	if value := strings.TrimSpace(os.Getenv("COMPANY_SHUTDOWN_TIMEOUT")); value != "" {
		config.ShutdownTimeout, err = time.ParseDuration(value)
		if err != nil || config.ShutdownTimeout <= 0 {
			return Config{}, fmt.Errorf("COMPANY_SHUTDOWN_TIMEOUT: %w", errInvalidDuration)
		}
	}
	missing := make([]string, 0, 2)
	if config.DatabaseURL == "" {
		missing = append(missing, "COMPANY_DB_URL")
	}
	if config.JWTPrivateKey == "" {
		missing = append(missing, "COMPANY_JWT_PRIVATE_KEY")
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
