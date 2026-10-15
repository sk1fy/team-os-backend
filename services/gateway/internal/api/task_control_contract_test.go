package api

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTaskControlContractPreservesRulesAndRejectsUnsafeInputs(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../../contracts/fixtures/task-control-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rules []map[string]any `json:"rules"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	rule := spec.Components.Schemas["TaskControlRule"].Value
	for _, value := range fixture.Rules {
		if err = rule.VisitJSON(value); err != nil {
			t.Fatalf("legacy rule lost: %v", err)
		}
	}
	cases := []struct {
		name   string
		modify func(map[string]any)
	}{
		{"CRM UUID is not numeric ID", func(v map[string]any) { v["users"] = []any{"a0000000-0000-4000-8000-000000000001"} }},
		{"missing archived", func(v map[string]any) { delete(v, "archived") }},
		{"missing responsible flag", func(v map[string]any) { delete(v, "checkResponsible") }},
		{"unknown field", func(v map[string]any) { v["companyId"] = "a0000000-0000-4000-8000-000000000001" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := map[string]any{}
			for k, x := range fixture.Rules[0] {
				v[k] = x
			}
			tc.modify(v)
			if rule.VisitJSON(v) == nil {
				t.Fatal("invalid rule accepted")
			}
		})
	}
	input := map[string]any{"expectedBindingRevision": float64(1), "expectedModeEpoch": float64(1), "expectedRevision": float64(1), "rules": []any{fixture.Rules[0]}}
	draft := spec.Components.Schemas["TaskControlDraftInput"].Value
	if err = draft.VisitJSON(input); err != nil {
		t.Fatal(err)
	}
	input["companyId"] = "a0000000-0000-4000-8000-000000000001"
	if draft.VisitJSON(input) == nil {
		t.Fatal("caller-supplied tenant accepted")
	}
	delete(input, "companyId")
	input["expectedRevision"] = float64(9007199254740992)
	if draft.VisitJSON(input) == nil {
		t.Fatal("unsafe JS revision accepted")
	}
}
