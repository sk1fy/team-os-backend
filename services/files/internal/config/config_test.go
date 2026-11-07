package config

import "testing"

func TestLegacyTrustedMetadataDoesNotDisableJWTRequirement(t *testing.T) {
	t.Setenv("FILES_DB_URL", "postgres://files")
	t.Setenv("FILES_S3_ACCESS_KEY", "access")
	t.Setenv("FILES_S3_SECRET_KEY", "secret")
	t.Setenv("FILES_TRUSTED_METADATA", "true")
	t.Setenv("FILES_JWT_PUBLIC_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("legacy trusted-metadata flag must not disable authenticated access")
	}
}

func TestLoadSeparatesInternalAndPublicS3Schemes(t *testing.T) {
	t.Setenv("FILES_DB_URL", "postgres://files")
	t.Setenv("FILES_S3_ACCESS_KEY", "access")
	t.Setenv("FILES_S3_SECRET_KEY", "secret")
	t.Setenv("FILES_JWT_PUBLIC_KEY", "test-public-key")
	t.Setenv("FILES_S3_SECURE", "false")
	t.Setenv("FILES_S3_PUBLIC_SECURE", "true")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.S3Secure {
		t.Error("внутреннее соединение с MinIO должно использовать HTTP")
	}
	if !config.S3PublicSecure {
		t.Error("публичные presigned-ссылки должны использовать HTTPS")
	}
}

func TestLoadDefaultsPublicS3SchemeToInternalScheme(t *testing.T) {
	t.Setenv("FILES_DB_URL", "postgres://files")
	t.Setenv("FILES_S3_ACCESS_KEY", "access")
	t.Setenv("FILES_S3_SECRET_KEY", "secret")
	t.Setenv("FILES_JWT_PUBLIC_KEY", "test-public-key")
	t.Setenv("FILES_S3_SECURE", "true")
	t.Setenv("FILES_S3_PUBLIC_SECURE", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !config.S3PublicSecure {
		t.Error("без отдельной настройки публичная схема должна наследовать внутреннюю")
	}
}
