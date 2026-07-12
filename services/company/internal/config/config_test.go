package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "postgres://localhost/company")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("COMPANY_ACCESS_TTL", "10m")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != ":8081" || config.GRPCAddr != ":9081" || config.AccessTTL != 10*time.Minute {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	t.Setenv("COMPANY_DB_URL", "")
	t.Setenv("COMPANY_JWT_PRIVATE_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error")
	}
}
