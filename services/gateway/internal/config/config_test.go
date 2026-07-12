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
	t.Setenv("GATEWAY_CORS_ORIGINS", "http://localhost:5173, https://team.example")
	t.Setenv("GATEWAY_COOKIE_SECURE", "true")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !config.CookieSecure || len(config.CORSOrigins) != 2 || config.CompanyGRPCAddr != "company:9081" || config.TasksGRPCAddr != "tasks:9083" || config.AcademyGRPCAddr != "academy:9084" || config.NotificationsGRPCAddr != "notifications:9085" || config.FilesGRPCAddr != "files:9086" {
		t.Fatalf("unexpected config: %#v", config)
	}
}
