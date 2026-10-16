package api

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDynamicIntentContractPreservesSelectionKinds(t *testing.T) {
	spec, e := GetSwagger()
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../../../contracts/fixtures/task-control-intent-v2.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		IntentRules []map[string]any `json:"intentRules"`
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	schema := spec.Components.Schemas["TaskControlIntentRuleV2"].Value
	if e = schema.VisitJSON(fixture.IntentRules[0]); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"allLinked cannot carry explicit people", func(r map[string]any) { r["audience"].(map[string]any)["kind"] = "allLinked" }},
		{"whole pipeline cannot carry explicit stages", func(r map[string]any) {
			stage := r["leads"].(map[string]any)["pipelines"].([]any)[0].(map[string]any)["stages"].(map[string]any)
			stage["statusIds"] = []any{"11"}
		}},
		{"missing includeDescendants", func(r map[string]any) {
			delete(r["audience"].(map[string]any)["departments"].([]any)[0].(map[string]any), "includeDescendants")
		}},
		{"caller cannot set company", func(r map[string]any) { r["companyId"] = "a0000000-0000-4000-8000-000000000001" }},
		{"future schema rejected", func(r map[string]any) { r["schemaVersion"] = float64(3) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f struct {
				IntentRules []map[string]any `json:"intentRules"`
			}
			if e = json.Unmarshal(raw, &f); e != nil {
				t.Fatal(e)
			}
			tc.change(f.IntentRules[0]["rule"].(map[string]any))
			if schema.VisitJSON(f.IntentRules[0]) == nil {
				t.Fatal("invalid discriminated intent accepted")
			}
		})
	}
	for _, path := range []string{"/api/v2/task-control/runtime/bootstrap", "/api/v2/task-control/runtime/ack", "/api/v2/task-control/connections/{bindingId}/activate", "/api/v2/task-control/connections/{bindingId}/draft", "/api/v2/task-control/connections/{bindingId}/validate", "/api/v2/task-control/connections/{bindingId}/publications"} {
		if spec.Paths.Find(path) == nil {
			t.Fatalf("missing v2 endpoint %s", path)
		}
	}
}

func TestDynamicRuntimeEnvelopeMatchesProductionWire(t *testing.T) {
	spec, e := GetSwagger()
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../../../contracts/fixtures/task-control-runtime-v2.json")
	if e != nil {
		t.Fatal(e)
	}
	var envelope map[string]any
	if e = json.Unmarshal(raw, &envelope); e != nil {
		t.Fatal(e)
	}
	if e = spec.Components.Schemas["TaskControlRuntimeEnvelopeV2"].Value.VisitJSON(envelope); e != nil {
		t.Fatal("production runtime envelope violates OpenAPI", e)
	}
	if len(envelope["intentChecksum"].(string)) != 64 {
		t.Fatal("intent hash wire format differs from widget")
	}
}
