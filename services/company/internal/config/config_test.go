package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "gateway-service-token-at-least-32-bytes")
	t.Setenv("COMPANY_ACCESS_TTL", "10m")
	t.Setenv("COMPANY_REGISTRATION_TOKEN_TTL", "2h")
	t.Setenv("COMPANY_AMO_WIDGET_SESSION_TTL", "12m")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != ":8081" || config.GRPCAddr != ":9081" || config.AccessTTL != 10*time.Minute ||
		config.RegistrationTokenTTL != 2*time.Hour || config.AmoWidgetSessionTTL != 12*time.Minute || config.AmoCRMTokenMaxTTL != time.Hour ||
		config.AmoCRMTokenClockSkew != 10*time.Second || config.AmoCRMWidgetCacheTTL != 5*time.Minute || config.AmoImportEnabled {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestLoadRejectsInvalidAmoWidgetDuration(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "gateway-service-token-at-least-32-bytes")
	t.Setenv("AMOCRM_WIDGET_LIST_TIMEOUT", "soon")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for invalid widget list timeout")
	}
}

func TestLoadRejectsInvalidRegistrationTokenTTL(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "gateway-service-token-at-least-32-bytes")
	t.Setenv("COMPANY_REGISTRATION_TOKEN_TTL", "never")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for invalid registration token TTL")
	}
}

func TestLoadEnablesAmoImportExplicitly(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "gateway-service-token-at-least-32-bytes")
	t.Setenv("COMPANY_AMO_IMPORT_ENABLED", "true")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.AmoImportEnabled {
		t.Fatal("amo import must be enabled explicitly")
	}
}

func TestLoadRejectsInvalidAmoImportFlag(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "gateway-service-token-at-least-32-bytes")
	t.Setenv("COMPANY_AMO_IMPORT_ENABLED", "sometimes")
	if _, err := Load(); err == nil {
		t.Fatal("invalid amo import flag must fail")
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error")
	}
}

func TestLoadRejectsShortGatewayServiceToken(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_GATEWAY_SERVICE_TOKEN", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for a short gateway service token")
	}
}
