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
	HTTPAddr              string
	CompanyGRPCAddr       string
	KbGRPCAddr            string
	TasksGRPCAddr         string
	AcademyGRPCAddr       string
	NotificationsGRPCAddr string
	JWTPublicKey          string
	JWTIssuer             string
	JWTAudience           string
	CORSOrigins           []string
	CookieSecure          bool
	ShutdownTimeout       time.Duration
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:              envOr("GATEWAY_HTTP_ADDR", ":8080"),
		CompanyGRPCAddr:       strings.TrimSpace(os.Getenv("GATEWAY_COMPANY_GRPC_ADDR")),
		KbGRPCAddr:            strings.TrimSpace(os.Getenv("GATEWAY_KB_GRPC_ADDR")),
		TasksGRPCAddr:         strings.TrimSpace(os.Getenv("GATEWAY_TASKS_GRPC_ADDR")),
		AcademyGRPCAddr:       strings.TrimSpace(os.Getenv("GATEWAY_ACADEMY_GRPC_ADDR")),
		NotificationsGRPCAddr: strings.TrimSpace(os.Getenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR")),
		JWTPublicKey:          strings.TrimSpace(os.Getenv("GATEWAY_JWT_PUBLIC_KEY")),
		JWTIssuer:             envOr("GATEWAY_JWT_ISSUER", "teamos-company"),
		JWTAudience:           envOr("GATEWAY_JWT_AUDIENCE", "teamos-api"),
		CORSOrigins:           splitList(envOr("GATEWAY_CORS_ORIGINS", "http://localhost:5173")),
		ShutdownTimeout:       30 * time.Second,
	}
	var err error
	if value := strings.TrimSpace(os.Getenv("GATEWAY_COOKIE_SECURE")); value != "" {
		config.CookieSecure, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("GATEWAY_COOKIE_SECURE: %w", err)
		}
	}
	if value := strings.TrimSpace(os.Getenv("GATEWAY_SHUTDOWN_TIMEOUT")); value != "" {
		config.ShutdownTimeout, err = time.ParseDuration(value)
		if err != nil || config.ShutdownTimeout <= 0 {
			return Config{}, fmt.Errorf("GATEWAY_SHUTDOWN_TIMEOUT: %w", errInvalidDuration)
		}
	}
	missing := make([]string, 0, 4)
	if config.CompanyGRPCAddr == "" {
		missing = append(missing, "GATEWAY_COMPANY_GRPC_ADDR")
	}
	if config.KbGRPCAddr == "" {
		missing = append(missing, "GATEWAY_KB_GRPC_ADDR")
	}
	if config.TasksGRPCAddr == "" {
		missing = append(missing, "GATEWAY_TASKS_GRPC_ADDR")
	}
	if config.AcademyGRPCAddr == "" {
		missing = append(missing, "GATEWAY_ACADEMY_GRPC_ADDR")
	}
	if config.NotificationsGRPCAddr == "" {
		missing = append(missing, "GATEWAY_NOTIFICATIONS_GRPC_ADDR")
	}
	if config.JWTPublicKey == "" {
		missing = append(missing, "GATEWAY_JWT_PUBLIC_KEY")
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

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
