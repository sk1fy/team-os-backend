package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"/auth/me", "/auth/me"},
		{"/auth/invites/${id(token)}/accept", "/auth/invites/{param}/accept"},
		{"/academy/lessons?courseId=${id(courseId)}", "/academy/lessons"},
		{"/tasks${boardId ", "/tasks"},
		{"/kb/articles${sectionId ", "/kb/articles"},
	}
	for _, test := range tests {
		if got := normalizePath(test.raw); got != test.want {
			t.Errorf("normalizePath(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestMatchSpec(t *testing.T) {
	spec := map[string]struct{}{
		"GET /api/v1/org/users":              {},
		"PATCH /api/v1/org/users/{id}":       {},
		"POST /api/v1/org/users/{id}/access": {},
	}
	tests := []struct {
		path, method string
		want         bool
	}{
		{"/api/v1/org/users", "GET", true},
		{"/api/v1/org/users", "POST", false},
		{"/api/v1/org/users/{param}", "PATCH", true},
		{"/api/v1/org/users/{param}/access", "POST", true},
		{"/api/v1/org/users/literal", "PATCH", false},
		{"/api/v1/org/{param}", "GET", false},
	}
	for _, test := range tests {
		if _, got := matchSpec(test.path, test.method, spec); got != test.want {
			t.Errorf("matchSpec(%q, %q) = %v, want %v", test.path, test.method, got, test.want)
		}
	}
}

func TestCollectCallsDiscoversNestedRuntimeFiles(t *testing.T) {
	frontendDir := t.TempDir()
	writeFrontendFile(t, frontendDir, "src/api/http.ts", "request('/auth/me')")
	writeFrontendFile(t, frontendDir, "src/api/nested/files.ts", "request('/files', 'POST')")
	writeFrontendFile(t, frontendDir, "src/api/nested/files.test.ts", "request('/not-a-real-endpoint')")

	calls, err := collectCalls(frontendDir)
	if err != nil {
		t.Fatalf("collectCalls() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("collectCalls() вернул %d вызовов, ожидалось 2: %+v", len(calls), calls)
	}
	if calls[0].File != "src/api/http.ts" || calls[0].Method != "GET" || calls[0].Path != "/auth/me" {
		t.Errorf("первый вызов = %+v", calls[0])
	}
	if calls[1].File != "src/api/nested/files.ts" || calls[1].Method != "POST" || calls[1].Path != "/files" {
		t.Errorf("вложенный вызов = %+v", calls[1])
	}
}

func TestCollectCallsRequiresAPISources(t *testing.T) {
	if _, err := collectCalls(t.TempDir()); err == nil {
		t.Fatal("collectCalls() должен возвращать ошибку, если src/api отсутствует")
	}
}

func writeFrontendFile(t *testing.T, frontendDir, relativePath, content string) {
	t.Helper()
	path := filepath.Join(frontendDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("создать каталог fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("записать fixture: %v", err)
	}
}
