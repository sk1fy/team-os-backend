package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr, GRPCAddr, DatabaseURL, NATSURL, JWTPublicKey, JWTIssuer, JWTAudience string
	EmailProvider, SMTPHost, SMTPUsername, SMTPPassword, SMTPFrom, SMTPFromName    string
	SMTPPort                                                                       int
	SMTPRequireTLS                                                                 bool
	ExternalEmailKey                                                               []byte
	ExternalEmailKeyID                                                             string
	ShutdownTimeout                                                                time.Duration
}

func Load() (Config, error) {
	externalEmailKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("NOTIFICATIONS_EXTERNAL_EMAIL_KEY")))
	if err != nil || len(externalEmailKey) != 32 {
		return Config{}, fmt.Errorf("NOTIFICATIONS_EXTERNAL_EMAIL_KEY должен быть base64-ключом длиной 32 байта")
	}
	smtpPort, err := strconv.Atoi(env("NOTIFICATIONS_SMTP_PORT", "587"))
	if err != nil || smtpPort < 1 || smtpPort > 65535 {
		return Config{}, fmt.Errorf("NOTIFICATIONS_SMTP_PORT должен быть корректным портом")
	}
	smtpRequireTLS, err := strconv.ParseBool(env("NOTIFICATIONS_SMTP_REQUIRE_TLS", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("NOTIFICATIONS_SMTP_REQUIRE_TLS должен быть true или false")
	}
	c := Config{
		HTTPAddr: env("NOTIFICATIONS_HTTP_ADDR", ":8085"), GRPCAddr: env("NOTIFICATIONS_GRPC_ADDR", ":9085"),
		DatabaseURL: strings.TrimSpace(os.Getenv("NOTIFICATIONS_DB_URL")), NATSURL: env("NOTIFICATIONS_NATS_URL", "nats://localhost:4222"),
		JWTPublicKey: strings.TrimSpace(os.Getenv("NOTIFICATIONS_JWT_PUBLIC_KEY")), JWTIssuer: env("NOTIFICATIONS_JWT_ISSUER", "teamos-company"),
		JWTAudience: env("NOTIFICATIONS_JWT_AUDIENCE", "teamos-api"), EmailProvider: strings.ToLower(env("NOTIFICATIONS_EMAIL_PROVIDER", "log")),
		SMTPHost: env("NOTIFICATIONS_SMTP_HOST", ""), SMTPPort: smtpPort,
		SMTPUsername: strings.TrimSpace(os.Getenv("NOTIFICATIONS_SMTP_USERNAME")), SMTPPassword: os.Getenv("NOTIFICATIONS_SMTP_PASSWORD"),
		SMTPFrom: env("NOTIFICATIONS_SMTP_FROM", ""), SMTPFromName: env("NOTIFICATIONS_SMTP_FROM_NAME", "TeamOS"),
		SMTPRequireTLS: smtpRequireTLS, ExternalEmailKey: externalEmailKey,
		ExternalEmailKeyID: env("NOTIFICATIONS_EXTERNAL_EMAIL_KEY_ID", "v1"), ShutdownTimeout: 30 * time.Second,
	}
	if c.DatabaseURL == "" || c.JWTPublicKey == "" {
		return Config{}, fmt.Errorf("не заданы обязательные переменные: NOTIFICATIONS_DB_URL, NOTIFICATIONS_JWT_PUBLIC_KEY")
	}
	if c.EmailProvider != "log" && c.EmailProvider != "smtp" {
		return Config{}, fmt.Errorf("NOTIFICATIONS_EMAIL_PROVIDER должен быть log или smtp")
	}
	if c.EmailProvider == "smtp" && (c.SMTPHost == "" || c.SMTPFrom == "") {
		return Config{}, fmt.Errorf("для SMTP обязательны NOTIFICATIONS_SMTP_HOST и NOTIFICATIONS_SMTP_FROM")
	}
	return c, nil
}
func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
