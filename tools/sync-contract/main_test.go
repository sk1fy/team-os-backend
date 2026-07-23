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
		{"/academy/reports/internal${buildQuery({\nq, page,\n})}", "/academy/reports/internal"},
		{"/academy/courses/${encodeId(courseId)}${buildQuery({ page })}", "/academy/courses/{param}"},
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

func TestCollectCallsRecognizesAcademyWrapperSignatures(t *testing.T) {
	frontendDir := t.TempDir()
	fixture := "\n" +
		"academyGet(\n" +
		"  \"/academy/learning/me\",\n" +
		"  options,\n" +
		")\n" +
		"academyGet(`/academy/courses/${encodeId(courseId)}/versions`, options)\n" +
		"academyMutate(\n" +
		"  `/academy/course-version-lessons/${encodeId(lessonId)}`,\n" +
		"  \"PATCH\",\n" +
		"  input,\n" +
		")\n" +
		"externalGet(`/public/academy/access/${encodeId(token)}`, options)\n" +
		"externalMutate(\n" +
		"  `/public/academy/access/${encodeId(token)}/activate`,\n" +
		"  'POST',\n" +
		"  {},\n" +
		")\n" +
		"academyGet(`/academy/reports/internal${buildQuery({\n" +
		"  q: filters.q,\n" +
		"  page: filters.page,\n" +
		"})}`, options)\n"
	writeFrontendFile(t, frontendDir, "src/api/academy/calls.ts", fixture)

	calls, err := collectCalls(frontendDir)
	if err != nil {
		t.Fatalf("collectCalls() error = %v", err)
	}
	want := []call{
		{Method: "GET", Path: "/academy/learning/me", File: "src/api/academy/calls.ts", Line: 2},
		{Method: "GET", Path: "/academy/courses/{param}/versions", File: "src/api/academy/calls.ts", Line: 6},
		{Method: "PATCH", Path: "/academy/course-version-lessons/{param}", File: "src/api/academy/calls.ts", Line: 7},
		{Method: "GET", Path: "/public/academy/access/{param}", File: "src/api/academy/calls.ts", Line: 12},
		{Method: "POST", Path: "/public/academy/access/{param}/activate", File: "src/api/academy/calls.ts", Line: 13},
		{Method: "GET", Path: "/academy/reports/internal", File: "src/api/academy/calls.ts", Line: 18},
	}
	if len(calls) != len(want) {
		t.Fatalf("collectCalls() вернул %d вызовов, ожидалось %d: %+v", len(calls), len(want), calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Errorf("calls[%d] = %+v, want %+v", index, calls[index], want[index])
		}
	}
}

func TestCollectCallsRejectsDynamicAcademyMutationMethod(t *testing.T) {
	frontendDir := t.TempDir()
	writeFrontendFile(t, frontendDir, "src/api/academy/calls.ts", `
academyMutate('/academy/courses', method, input)
`)

	if _, err := collectCalls(frontendDir); err == nil {
		t.Fatal("collectCalls() должен отклонять mutation с неразобранным методом")
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
