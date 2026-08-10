package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("GATEWAY_COMPANY_GRPC_ADDR", "company:9081")
	t.Setenv("GATEWAY_KB_GRPC_ADDR", "kb:9082")
	t.Setenv("GATEWAY_TASKS_GRPC_ADDR", "tasks:9083")
	t.Setenv("GATEWAY_ACADEMY_GRPC_ADDR", "academy:9084")
	t.Setenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR", "notifications:9085")
	t.Setenv("GATEWAY_FILES_GRPC_ADDR", "files:9086")
	t.Setenv("GATEWAY_JWT_PUBLIC_KEY", "public")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", "gateway-provisioning-test-secret-0001")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_PROVIDER", "rakurs")
	t.Setenv("GATEWAY_COMPANY_SERVICE_TOKEN", "gateway-company-test-secret-0000001")
	t.Setenv("GATEWAY_CORS_ORIGINS", "http://localhost:5173, https://team.example")
	t.Setenv("GATEWAY_COOKIE_SECURE", "true")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.CookieSecure || len(config.CORSOrigins) != 2 || config.CompanyGRPCAddr != "company:9081" || config.TasksGRPCAddr != "tasks:9083" || config.AcademyGRPCAddr != "academy:9084" || config.NotificationsGRPCAddr != "notifications:9085" || config.FilesGRPCAddr != "files:9086" || config.ProvisioningServiceToken != "gateway-provisioning-test-secret-0001" || config.ProvisioningServiceProvider != "rakurs" || config.CompanyServiceToken != "gateway-company-test-secret-0000001" {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestLoadRequiresProvisioningServiceToken(t *testing.T) {
	t.Setenv("GATEWAY_COMPANY_GRPC_ADDR", "company:9081")
	t.Setenv("GATEWAY_KB_GRPC_ADDR", "kb:9082")
	t.Setenv("GATEWAY_TASKS_GRPC_ADDR", "tasks:9083")
	t.Setenv("GATEWAY_ACADEMY_GRPC_ADDR", "academy:9084")
	t.Setenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR", "notifications:9085")
	t.Setenv("GATEWAY_FILES_GRPC_ADDR", "files:9086")
	t.Setenv("GATEWAY_JWT_PUBLIC_KEY", "public")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", "too-short")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_PROVIDER", "rakurs")
	t.Setenv("GATEWAY_COMPANY_SERVICE_TOKEN", "gateway-company-test-secret-0000001")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for a short provisioning service key")
	}
}

func TestLoadAllowsMissingProvisioningServiceTokenWhenExplicitlyEnabled(t *testing.T) {
	t.Setenv("GATEWAY_COMPANY_GRPC_ADDR", "company:9081")
	t.Setenv("GATEWAY_KB_GRPC_ADDR", "kb:9082")
	t.Setenv("GATEWAY_TASKS_GRPC_ADDR", "tasks:9083")
	t.Setenv("GATEWAY_ACADEMY_GRPC_ADDR", "academy:9084")
	t.Setenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR", "notifications:9085")
	t.Setenv("GATEWAY_FILES_GRPC_ADDR", "files:9086")
	t.Setenv("GATEWAY_JWT_PUBLIC_KEY", "public")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", "")
	t.Setenv("GATEWAY_PROVISIONING_ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_PROVIDER", "rakurs")
	t.Setenv("GATEWAY_COMPANY_SERVICE_TOKEN", "gateway-company-test-secret-0000001")

	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.ProvisioningAllowUnauthenticated || config.ProvisioningServiceToken != "" {
		t.Fatalf("unexpected provisioning config: %#v", config)
	}
}

func TestLoadRejectsInvalidProvisioningUnauthenticatedFlag(t *testing.T) {
	t.Setenv("GATEWAY_PROVISIONING_ALLOW_UNAUTHENTICATED", "sometimes")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for an invalid unauthenticated provisioning flag")
	}
}

func TestLoadRequiresCompanyServiceToken(t *testing.T) {
	t.Setenv("GATEWAY_COMPANY_GRPC_ADDR", "company:9081")
	t.Setenv("GATEWAY_KB_GRPC_ADDR", "kb:9082")
	t.Setenv("GATEWAY_TASKS_GRPC_ADDR", "tasks:9083")
	t.Setenv("GATEWAY_ACADEMY_GRPC_ADDR", "academy:9084")
	t.Setenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR", "notifications:9085")
	t.Setenv("GATEWAY_FILES_GRPC_ADDR", "files:9086")
	t.Setenv("GATEWAY_JWT_PUBLIC_KEY", "public")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", "gateway-provisioning-test-secret-0001")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_PROVIDER", "rakurs")
	t.Setenv("GATEWAY_COMPANY_SERVICE_TOKEN", "too-short")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for a short company service token")
	}
}

func TestLoadRequiresValidProvisioningServiceProvider(t *testing.T) {
	t.Setenv("GATEWAY_COMPANY_GRPC_ADDR", "company:9081")
	t.Setenv("GATEWAY_KB_GRPC_ADDR", "kb:9082")
	t.Setenv("GATEWAY_TASKS_GRPC_ADDR", "tasks:9083")
	t.Setenv("GATEWAY_ACADEMY_GRPC_ADDR", "academy:9084")
	t.Setenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR", "notifications:9085")
	t.Setenv("GATEWAY_FILES_GRPC_ADDR", "files:9086")
	t.Setenv("GATEWAY_JWT_PUBLIC_KEY", "public")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", "gateway-provisioning-test-secret-0001")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_PROVIDER", "Rakurs invalid")
	t.Setenv("GATEWAY_COMPANY_SERVICE_TOKEN", "gateway-company-test-secret-0000001")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for an invalid provisioning service provider")
	}
}

func TestLoadRequiresDistinctServiceTokens(t *testing.T) {
	const sharedToken = "shared-service-token-must-not-cross-boundaries"
	t.Setenv("GATEWAY_COMPANY_GRPC_ADDR", "company:9081")
	t.Setenv("GATEWAY_KB_GRPC_ADDR", "kb:9082")
	t.Setenv("GATEWAY_TASKS_GRPC_ADDR", "tasks:9083")
	t.Setenv("GATEWAY_ACADEMY_GRPC_ADDR", "academy:9084")
	t.Setenv("GATEWAY_NOTIFICATIONS_GRPC_ADDR", "notifications:9085")
	t.Setenv("GATEWAY_FILES_GRPC_ADDR", "files:9086")
	t.Setenv("GATEWAY_JWT_PUBLIC_KEY", "public")
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_TOKEN", sharedToken)
	t.Setenv("GATEWAY_PROVISIONING_SERVICE_PROVIDER", "rakurs")
	t.Setenv("GATEWAY_COMPANY_SERVICE_TOKEN", sharedToken)

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error when external and internal service tokens match")
	}
}
