package config

import (
	"encoding/base64"
	"testing"
)

func TestLoadDefaultsToSafeLogEmailProvider(t *testing.T) {
	t.Setenv("NOTIFICATIONS_DB_URL", "postgres://notifications")
	t.Setenv("NOTIFICATIONS_JWT_PUBLIC_KEY", "public-key")
	t.Setenv("NOTIFICATIONS_EXTERNAL_EMAIL_KEY", testExternalEmailKey())
	t.Setenv("NOTIFICATIONS_EMAIL_PROVIDER", "")
	t.Setenv("NOTIFICATIONS_SMTP_PORT", "")
	t.Setenv("NOTIFICATIONS_SMTP_REQUIRE_TLS", "")
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.EmailProvider != "log" || !got.SMTPRequireTLS {
		t.Fatal("небезопасные значения email по умолчанию")
	}
}

func TestLoadValidatesSMTPSettings(t *testing.T) {
	t.Setenv("NOTIFICATIONS_DB_URL", "postgres://notifications")
	t.Setenv("NOTIFICATIONS_JWT_PUBLIC_KEY", "public-key")
	t.Setenv("NOTIFICATIONS_EXTERNAL_EMAIL_KEY", testExternalEmailKey())
	t.Setenv("NOTIFICATIONS_EMAIL_PROVIDER", "smtp")
	t.Setenv("NOTIFICATIONS_SMTP_HOST", "")
	t.Setenv("NOTIFICATIONS_SMTP_FROM", "")
	if _, err := Load(); err == nil {
		t.Fatal("ожидалась ошибка для неполной SMTP-конфигурации")
	}
}

func TestLoadRequiresAES256ExternalEmailKey(t *testing.T) {
	t.Setenv("NOTIFICATIONS_EXTERNAL_EMAIL_KEY", base64.StdEncoding.EncodeToString(make([]byte, 16)))
	if _, err := Load(); err == nil {
		t.Fatal("ожидалась ошибка для ключа короче 32 байт")
	}
}

func testExternalEmailKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}
