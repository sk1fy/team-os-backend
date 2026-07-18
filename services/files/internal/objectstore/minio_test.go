package objectstore

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestStoreUsesIndependentInternalAndPublicSchemes(t *testing.T) {
	store, err := New(
		"minio:9000",
		"storage.example.ru",
		"access",
		"secret",
		"us-east-1",
		"teamos-files",
		false,
		true,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := store.client.EndpointURL().Scheme; got != "http" {
		t.Fatalf("внутренняя схема = %q, ожидалась http", got)
	}

	rawURL, err := store.DownloadURL(context.Background(), "company/file.txt", time.Minute)
	if err != nil {
		t.Fatalf("DownloadURL() error = %v", err)
	}
	presignedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("разобрать presigned URL: %v", err)
	}
	if presignedURL.Scheme != "https" || presignedURL.Host != "storage.example.ru" {
		t.Fatalf("presigned URL = %q, ожидался HTTPS host storage.example.ru", rawURL)
	}
}
