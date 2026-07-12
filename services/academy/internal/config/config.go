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
	KbGRPCAddr      string
	CompanyGRPCAddr string
	JWTPublicKey    string
	JWTIssuer       string
	JWTAudience     string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:        envOr("ACADEMY_HTTP_ADDR", ":8084"),
		GRPCAddr:        envOr("ACADEMY_GRPC_ADDR", ":9084"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("ACADEMY_DB_URL")),
		NATSURL:         envOr("ACADEMY_NATS_URL", "nats://localhost:4222"),
		KbGRPCAddr:      strings.TrimSpace(os.Getenv("ACADEMY_KB_GRPC_ADDR")),
		CompanyGRPCAddr: strings.TrimSpace(os.Getenv("ACADEMY_COMPANY_GRPC_ADDR")),
		JWTPublicKey:    strings.TrimSpace(os.Getenv("ACADEMY_JWT_PUBLIC_KEY")),
		JWTIssuer:       envOr("ACADEMY_JWT_ISSUER", "teamos-company"),
		JWTAudience:     envOr("ACADEMY_JWT_AUDIENCE", "teamos-api"),
		ShutdownTimeout: 30 * time.Second,
	}
	var err error
	if value := strings.TrimSpace(os.Getenv("ACADEMY_SHUTDOWN_TIMEOUT")); value != "" {
		config.ShutdownTimeout, err = time.ParseDuration(value)
		if err != nil || config.ShutdownTimeout <= 0 {
			return Config{}, fmt.Errorf("ACADEMY_SHUTDOWN_TIMEOUT: %w", errInvalidDuration)
		}
	}
	missing := make([]string, 0, 4)
	if config.DatabaseURL == "" {
		missing = append(missing, "ACADEMY_DB_URL")
	}
	if config.KbGRPCAddr == "" {
		missing = append(missing, "ACADEMY_KB_GRPC_ADDR")
	}
	if config.CompanyGRPCAddr == "" {
		missing = append(missing, "ACADEMY_COMPANY_GRPC_ADDR")
	}
	if config.JWTPublicKey == "" {
		missing = append(missing, "ACADEMY_JWT_PUBLIC_KEY")
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
