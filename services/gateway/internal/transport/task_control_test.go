package transport

import (
	"strings"
	"testing"

	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestTaskControlContractsHaveNoRemainingStubs(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/api/v1/task-control/") && !strings.HasPrefix(path, "/api/v2/task-control/") {
			continue
		}
		for method, operation := range item.Operations() {
			if operation.Responses.Value("501") != nil {
				t.Fatalf("%s %s still advertises a stub", method, path)
			}
		}
	}
}
