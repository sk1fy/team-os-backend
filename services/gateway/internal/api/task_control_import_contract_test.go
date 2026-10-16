package api

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTaskControlImportActualWidgetWireMatchesOpenAPI(t *testing.T) {
	spec, e := GetSwagger()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ file, schema string }{{"task-control-import-source-result.json", "TaskControlBridgeResult"}, {"task-control-import-preview.json", "TaskControlImportPreview"}, {"task-control-import-partial-result.json", "TaskControlBridgeResult"}} {
		raw, e := os.ReadFile("../../../../contracts/fixtures/" + tc.file)
		if e != nil {
			t.Fatal(e)
		}
		var input map[string]any
		if e = json.Unmarshal(raw, &input); e != nil {
			t.Fatal(e)
		}
		if e = spec.Components.Schemas[tc.schema].Value.VisitJSON(input); e != nil {
			t.Fatalf("%s actual wire violates schema: %v", tc.schema, e)
		}
	}
	for _, path := range []string{"/api/v1/task-control/connections/{bindingId}/imports", "/api/v1/task-control/connections/{bindingId}/source-requests", "/api/v2/task-control/connections/{bindingId}/imports/{importId}/apply", "/api/v1/widget/task-control/source-requests", "/api/v1/widget/task-control/source-requests/{sourceRequestId}/result", "/api/v1/widget/task-control/connection"} {
		item := spec.Paths.Find(path)
		if item == nil {
			t.Fatal("missing working API", path)
		}
		for _, op := range item.Operations() {
			if op.Responses.Value("501") != nil {
				t.Fatal("implemented import still advertises 501", path)
			}
		}
	}
}
