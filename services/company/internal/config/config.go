package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr         string
	GRPCAddr         string
	DatabaseURL      string
	NATSURL          string
	JWTPrivateKey    string
	JWTIssuer        string
	JWTAudience      string
	AccessTTL        time.Duration
	ShutdownTimeout  time.Duration
	ExternalAPIURL   string
	AmoAppName       string
	AmoImportEnabled bool
	ExternalTimeout  time.Duration
	AmoSyncInterval  time.Duration
}

func Load() (Config, error) {
	var err error
	config := Config{
		HTTPAddr:         envOr("COMPANY_HTTP_ADDR", ":8081"),
		GRPCAddr:         envOr("COMPANY_GRPC_ADDR", ":9081"),
		DatabaseURL:      strings.TrimSpace(os.Getenv("COMPANY_DB_URL")),
		NATSURL:          envOr("COMPANY_NATS_URL", "nats://localhost:4222"),
		JWTPrivateKey:    strings.TrimSpace(os.Getenv("COMPANY_JWT_PRIVATE_KEY")),
		JWTIssuer:        envOr("COMPANY_JWT_ISSUER", "teamos-company"),
		JWTAudience:      envOr("COMPANY_JWT_AUDIENCE", "teamos-api"),
		AccessTTL:        15 * time.Minute,
		ShutdownTimeout:  30 * time.Second,
		ExternalAPIURL:   envOr("EXTERNAL_API_URL", "https://ssd.rkrs.ru/api/v1/rkrs_activity/getEmployee"),
		AmoAppName:       envOr("APP_NAME", "rkrs_activity"),
		AmoImportEnabled: false,
		ExternalTimeout:  10 * time.Second,
		AmoSyncInterval:  5 * time.Minute,
	}
	if value := strings.TrimSpace(os.Getenv("COMPANY_AMO_IMPORT_ENABLED")); value != "" {
		config.AmoImportEnabled, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("COMPANY_AMO_IMPORT_ENABLED: ожидается true или false")
		}
	}
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
	if value := strings.TrimSpace(os.Getenv("EXTERNAL_API_TIMEOUT")); value != "" {
		seconds, parseErr := time.ParseDuration(value + "s")
		if parseErr != nil || seconds <= 0 {
			return Config{}, fmt.Errorf("EXTERNAL_API_TIMEOUT: %w", errInvalidDuration)
		}
		config.ExternalTimeout = seconds
	}
	if value := strings.TrimSpace(os.Getenv("COMPANY_AMO_SYNC_INTERVAL")); value != "" {
		config.AmoSyncInterval, err = time.ParseDuration(value)
		if err != nil || config.AmoSyncInterval <= 0 {
			return Config{}, fmt.Errorf("COMPANY_AMO_SYNC_INTERVAL: %w", errInvalidDuration)
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
