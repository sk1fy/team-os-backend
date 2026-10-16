package authmw

import (
	"net/http"
	"testing"
)

func TestTaskControlOnlyDedicatedRuntimeHandlersSkipTeamOSJWT(t *testing.T) {
	for _, v := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/v1/widget/task-control/sessions", true}, {http.MethodGet, "/api/v1/widget/task-control/bootstrap", true},
		{http.MethodGet, "/api/v2/task-control/connections/a0000000-0000-4000-8000-000000000001/references", false},
		{http.MethodPost, "/api/v2/task-control/connections/a0000000-0000-4000-8000-000000000001/references/refresh", false},
		{http.MethodGet, "/api/v1/task-control/connections", false}, {http.MethodPost, "/api/v1/task-control/connections/link-intents", false},
		{http.MethodPost, "/api/v1/widget/task-control/bootstrap", false}, {http.MethodGet, "/api/v1/widget/task-control/config", false},
	} {
		if got := isPublic(v.method, v.path); got != v.want {
			t.Fatalf("%s %s public=%v", v.method, v.path, got)
		}
	}
}
