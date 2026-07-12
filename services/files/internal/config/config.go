package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultAllowedTypes = "image/jpeg,image/png,image/webp,image/gif,application/pdf,text/plain,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type Config struct {
	HTTPAddr, GRPCAddr, DatabaseURL                                            string
	JWTPublicKey, JWTIssuer, JWTAudience                                       string
	S3Endpoint, S3PublicEndpoint, S3AccessKey, S3SecretKey, S3Bucket, S3Region string
	S3Secure, S3CreateBucket, TrustedMetadata                                  bool
	MaxFileSize                                                                int64
	AllowedTypes                                                               map[string]struct{}
	DownloadURLTTL, ShutdownTimeout                                            time.Duration
	TempDir                                                                    string
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr: env("FILES_HTTP_ADDR", ":8086"), GRPCAddr: env("FILES_GRPC_ADDR", ":9086"),
		DatabaseURL:  strings.TrimSpace(os.Getenv("FILES_DB_URL")),
		JWTPublicKey: strings.TrimSpace(os.Getenv("FILES_JWT_PUBLIC_KEY")), JWTIssuer: env("FILES_JWT_ISSUER", "teamos-company"), JWTAudience: env("FILES_JWT_AUDIENCE", "teamos-api"),
		S3Endpoint: env("FILES_S3_ENDPOINT", "localhost:9000"), S3AccessKey: strings.TrimSpace(os.Getenv("FILES_S3_ACCESS_KEY")), S3SecretKey: strings.TrimSpace(os.Getenv("FILES_S3_SECRET_KEY")), S3Bucket: env("FILES_S3_BUCKET", "teamos-files"), S3Region: env("FILES_S3_REGION", "us-east-1"),
		S3Secure: envBool("FILES_S3_SECURE", false), S3CreateBucket: envBool("FILES_S3_CREATE_BUCKET", false), TrustedMetadata: envBool("FILES_TRUSTED_METADATA", false),
		MaxFileSize: 25 << 20, DownloadURLTTL: 15 * time.Minute, ShutdownTimeout: 30 * time.Second,
		AllowedTypes: parseTypes(env("FILES_ALLOWED_CONTENT_TYPES", defaultAllowedTypes)), TempDir: strings.TrimSpace(os.Getenv("FILES_TEMP_DIR")),
	}
	c.S3PublicEndpoint = env("FILES_S3_PUBLIC_ENDPOINT", c.S3Endpoint)
	var err error
	if raw := strings.TrimSpace(os.Getenv("FILES_MAX_SIZE_BYTES")); raw != "" {
		c.MaxFileSize, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || c.MaxFileSize <= 0 {
			return Config{}, fmt.Errorf("FILES_MAX_SIZE_BYTES: %w", errPositive)
		}
	}
	if c.DownloadURLTTL, err = duration("FILES_DOWNLOAD_URL_TTL", c.DownloadURLTTL); err != nil {
		return Config{}, err
	}
	if c.ShutdownTimeout, err = duration("FILES_SHUTDOWN_TIMEOUT", c.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	missing := make([]string, 0, 5)
	for name, value := range map[string]string{"FILES_DB_URL": c.DatabaseURL, "FILES_S3_ACCESS_KEY": c.S3AccessKey, "FILES_S3_SECRET_KEY": c.S3SecretKey} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if c.JWTPublicKey == "" && !c.TrustedMetadata {
		missing = append(missing, "FILES_JWT_PUBLIC_KEY")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("не заданы обязательные переменные: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

var errPositive = errors.New("ожидается положительное значение")

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s: %w", name, errPositive)
	}
	return v, nil
}
func env(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}
func parseTypes(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, v := range strings.Split(raw, ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}
