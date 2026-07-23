package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr           string
	GRPCAddr           string
	DatabaseURL        string
	NATSURL            string
	KbGRPCAddr         string
	CompanyGRPCAddr    string
	FilesGRPCAddr      string
	ExternalSecret     string
	ExternalEmailKey   []byte
	ExternalEmailKeyID string
	JWTPublicKey       string
	JWTIssuer          string
	JWTAudience        string
	ShutdownTimeout    time.Duration
}

func Load() (Config, error) {
	var err error
	config := Config{
		HTTPAddr:           envOr("ACADEMY_HTTP_ADDR", ":8084"),
		GRPCAddr:           envOr("ACADEMY_GRPC_ADDR", ":9084"),
		DatabaseURL:        strings.TrimSpace(os.Getenv("ACADEMY_DB_URL")),
		NATSURL:            envOr("ACADEMY_NATS_URL", "nats://localhost:4222"),
		KbGRPCAddr:         strings.TrimSpace(os.Getenv("ACADEMY_KB_GRPC_ADDR")),
		CompanyGRPCAddr:    strings.TrimSpace(os.Getenv("ACADEMY_COMPANY_GRPC_ADDR")),
		FilesGRPCAddr:      strings.TrimSpace(os.Getenv("ACADEMY_FILES_GRPC_ADDR")),
		ExternalSecret:     strings.TrimSpace(os.Getenv("ACADEMY_EXTERNAL_TOKEN_SECRET")),
		ExternalEmailKeyID: envOr("ACADEMY_EXTERNAL_EMAIL_KEY_ID", "v1"),
		JWTPublicKey:       strings.TrimSpace(os.Getenv("ACADEMY_JWT_PUBLIC_KEY")),
		JWTIssuer:          envOr("ACADEMY_JWT_ISSUER", "teamos-company"),
		JWTAudience:        envOr("ACADEMY_JWT_AUDIENCE", "teamos-api"),
		ShutdownTimeout:    30 * time.Second,
	}
	encodedEmailKey := strings.TrimSpace(os.Getenv("ACADEMY_EXTERNAL_EMAIL_KEY"))
	if encodedEmailKey != "" {
		config.ExternalEmailKey, err = base64.StdEncoding.DecodeString(encodedEmailKey)
		if err != nil || len(config.ExternalEmailKey) != 32 {
			return Config{}, errors.New("ACADEMY_EXTERNAL_EMAIL_KEY должен быть base64-ключом AES-256")
		}
	}
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
	if config.FilesGRPCAddr == "" {
		missing = append(missing, "ACADEMY_FILES_GRPC_ADDR")
	}
	if len(config.ExternalSecret) < 32 {
		missing = append(missing, "ACADEMY_EXTERNAL_TOKEN_SECRET (минимум 32 символа)")
	}
	if len(config.ExternalEmailKey) != 32 {
		missing = append(missing, "ACADEMY_EXTERNAL_EMAIL_KEY")
	}
	if strings.TrimSpace(config.ExternalEmailKeyID) == "" {
		missing = append(missing, "ACADEMY_EXTERNAL_EMAIL_KEY_ID")
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
