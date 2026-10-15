package transport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestTaskControlRoutesDoNotClaimUnimplementedWritesSucceeded(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	router := api.Handler(&Handler{})
	param := regexp.MustCompile(`\{[^}]+\}`)
	count := 0
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/api/v1/task-control/") {
			continue
		}
		for method, operation := range item.Operations() {
			if operation.Responses.Value("501") == nil {
				continue
			}
			count++
			t.Run(method+path, func(t *testing.T) {
				req := httptest.NewRequestWithContext(context.Background(), method, param.ReplaceAllString(path, "a0000000-0000-4000-8000-000000000001"), nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != 501 {
					t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
				var body struct {
					Error struct {
						Message string
						Status  int
						Code    string
					}
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Status != 501 || body.Error.Code != "task_control_not_implemented" || !strings.Contains(body.Error.Message, "ещё не реализован") {
					t.Fatalf("invalid error: %s", rec.Body.String())
				}
			})
		}
	}
	if count != 9 {
		t.Fatalf("checked %d operations, expected 9", count)
	}
}
