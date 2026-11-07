package config

import "testing"

func TestLoadRequiresCompanyConnectionAndServiceCredential(t *testing.T) {
	t.Setenv("TASKS_DB_URL", "postgres://example/tasks")
	t.Setenv("TASKS_JWT_PUBLIC_KEY", "test")
	t.Setenv("TASKS_COMPANY_GRPC_ADDR", "")
	t.Setenv("TASKS_COMPANY_SERVICE_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing company configuration accepted")
	}
	t.Setenv("TASKS_COMPANY_GRPC_ADDR", "company.example:9081")
	if _, err := Load(); err == nil {
		t.Fatal("missing recurrence credential accepted")
	}
	t.Setenv("TASKS_COMPANY_SERVICE_TOKEN", "shared-secret")
	cfg, err := Load()
	if err != nil || cfg.CompanyGRPCAddr != "company.example:9081" || cfg.CompanyServiceToken != "shared-secret" {
		t.Fatalf("config=%+v err=%v", cfg, err)
	}
}
