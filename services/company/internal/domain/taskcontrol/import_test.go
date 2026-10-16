package taskcontrol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func importFixture(t *testing.T) (json.RawMessage, map[string]json.RawMessage, []Rule) {
	t.Helper()
	raw, e := os.ReadFile("testdata/r04-catalog-source.json")
	if e != nil {
		t.Fatal(e)
	}
	var source map[string]any
	if e = importJSON(raw, &source); e != nil {
		t.Fatal(e)
	}
	ids := map[string]json.RawMessage{}
	for i, f := range importFields {
		b, _ := json.Marshal(100 + i)
		ids[f.key] = b
	}
	rules, e := importCatalogRules(source, ids)
	if e != nil {
		t.Fatal(e)
	}
	return raw, ids, rules
}
func TestImportCaptureWidgetParity(t *testing.T) {
	raw, ids, rules := importFixture(t)
	if _, hash := Legacy(rules); hash != "9435589f1030ec9c20ab573907e3d4ade48e0179a30f2ec864ab9195cfafaddf" {
		t.Fatal("widget checksum differs:", hash)
	}
	if issues := ValidateImportCapture(rules, raw, ids, "RKRS_WT_RULE_UID", "catalog", "77", "123"); len(issues) != 0 {
		t.Fatal(issues)
	}
	if len(rules) != 1 || rules[0].Enabled || !rules[0].Archived || len(rules[0].Pipelines[0].StatusIDs) != 3 {
		t.Fatalf("legacy flags/counts lost: %#v", rules)
	}
	// Runtime source must contain the exact complete published legacy set as well.
	var source map[string]any
	if e := importJSON(raw, &source); e != nil {
		t.Fatal(e)
	}
	legacy, hash := Legacy(rules)
	source["runtimeConfig"] = map[string]any{"schema_version": 1, "storage": map[string]any{"catalog_id": 77, "fields": ids}, "published": map[string]any{"hash": hash}, "rules": legacy}
	cfg, _ := json.Marshal(source["runtimeConfig"])
	source["runtimeConfig"] = string(cfg)
	runtime, _ := json.Marshal(source)
	if issues := ValidateImportCapture(rules, runtime, ids, "RKRS_WT_RULE_UID", "runtime_config", "77", "123"); len(issues) != 0 {
		t.Fatal(issues)
	}
}
func TestImportCaptureRejectsSilentRepair(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*json.RawMessage, map[string]json.RawMessage, *[]Rule)
	}{
		{"partial normalized rules", func(_ *json.RawMessage, _ map[string]json.RawMessage, r *[]Rule) { *r = (*r)[:0] }},
		{"changed boolean", func(_ *json.RawMessage, _ map[string]json.RawMessage, r *[]Rule) {
			(*r)[0].DenyExit = !(*r)[0].DenyExit
		}},
		{"field mismatch", func(_ *json.RawMessage, ids map[string]json.RawMessage, _ *[]Rule) {
			ids["users"] = json.RawMessage(`102`)
		}},
		{"duplicate outer key", func(raw *json.RawMessage, _ map[string]json.RawMessage, _ *[]Rule) {
			*raw = json.RawMessage(strings.Replace(string(*raw), `"catalog":`, `"catalog":{},"catalog":`, 1))
		}},
		{"unsafe raw id", func(raw *json.RawMessage, _ map[string]json.RawMessage, _ *[]Rule) {
			*raw = json.RawMessage(strings.Replace(string(*raw), `"id": 77`, `"id": 9007199254740992`, 1))
		}},
		{"foreign account", func(raw *json.RawMessage, _ map[string]json.RawMessage, _ *[]Rule) {
			*raw = json.RawMessage(strings.Replace(string(*raw), `"account_id": 123`, `"account_id": 124`, 1))
		}},
		{"duplicate uid", func(_ *json.RawMessage, _ map[string]json.RawMessage, r *[]Rule) { *r = append(*r, (*r)[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, ids, rules := importFixture(t)
			test.mutate(&raw, ids, &rules)
			if len(ValidateImportCapture(rules, raw, ids, "RKRS_WT_RULE_UID", "catalog", "77", "123")) == 0 {
				t.Fatal("accepted inconsistent original")
			}
		})
	}
}

func TestImportCaptureRejectsRawIdentifierRepair(t *testing.T) {
	for _, value := range []string{`[2,2,3]`, `[2,9007199254740992]`, `[2,3.5]`, `[2,"03"]`, `{"users":[2],"users":[3]}`} {
		t.Run(value, func(t *testing.T) {
			raw, ids, rules := importFixture(t)
			var source map[string]any
			if e := importJSON(raw, &source); e != nil {
				t.Fatal(e)
			}
			element := source["elements"].([]any)[0].(map[string]any)
			fields := element["custom_fields_values"].([]any)
			for _, v := range fields {
				f := v.(map[string]any)
				id, _ := importID(f["field_id"])
				if id == "101" {
					f["values"] = []any{map[string]any{"value": value}}
				}
			}
			raw, e := json.Marshal(source)
			if e != nil {
				t.Fatal(e)
			}
			if len(ValidateImportCapture(rules, raw, ids, "RKRS_WT_RULE_UID", "catalog", "77", "123")) == 0 {
				t.Fatal("accepted normalization that changes raw IDs")
			}
		})
	}
}

func TestImportCaptureComparisonIsComputedFromOriginal(t *testing.T) {
	raw, ids, rules := importFixture(t)
	var source map[string]any
	if e := importJSON(raw, &source); e != nil {
		t.Fatal(e)
	}
	legacy, hash := Legacy(rules)
	source["runtimeConfig"] = map[string]any{"schema_version": 1, "storage": map[string]any{"catalog_id": 77, "fields": ids}, "published": map[string]any{"hash": hash}, "rules": legacy}
	encoded, _ := json.Marshal(source)
	if issues := ImportCaptureDiscrepancies(rules, encoded, ids, "RKRS_WT_RULE_UID", "catalog", "77", "123"); len(issues) > 0 {
		t.Fatal(issues)
	}

	runtimeOnly, _ := json.Marshal(map[string]any{"runtimeConfig": source["runtimeConfig"]})
	if issues := ValidateImportCapture(rules, runtimeOnly, ids, "RKRS_WT_RULE_UID", "runtime_config", "77", "123"); len(issues) > 0 {
		t.Fatal(issues)
	}
	if issues := ImportCaptureDiscrepancies(rules, runtimeOnly, ids, "RKRS_WT_RULE_UID", "runtime_config", "77", "123"); len(issues) != 1 || issues[0].Code != "catalog_invalid" {
		t.Fatal(issues)
	}
	// A valid but different published snapshot needs explicit choice, even if the
	// browser omits its own discrepancy list.
	other := append([]Rule{}, rules...)
	other[0].Name = "Опубликованное имя"
	otherLegacy, otherHash := Legacy(other)
	cfg := source["runtimeConfig"].(map[string]any)
	cfg["rules"] = otherLegacy
	cfg["published"] = map[string]any{"hash": otherHash}
	encoded, _ = json.Marshal(source)
	issues := ImportCaptureDiscrepancies(rules, encoded, ids, "RKRS_WT_RULE_UID", "catalog", "77", "123")
	if len(issues) != 1 || issues[0].Code != "catalog_runtime_divergence" {
		t.Fatal(issues)
	}
	cfg["published"] = map[string]any{"hash": "corrupt"}
	encoded, _ = json.Marshal(source)
	issues = ImportCaptureDiscrepancies(rules, encoded, ids, "RKRS_WT_RULE_UID", "catalog", "77", "123")
	if len(issues) != 1 || issues[0].Code != "runtime_config_invalid" {
		t.Fatal(issues)
	}
	source["elements"] = []any{}
	cfg["published"] = map[string]any{"hash": otherHash}
	encoded, _ = json.Marshal(source)
	issues = ImportCaptureDiscrepancies(other, encoded, ids, "RKRS_WT_RULE_UID", "runtime_config", "77", "123")
	if len(issues) != 1 || issues[0].Code != "catalog_runtime_divergence" {
		t.Fatal(issues)
	}
	source["elements"] = nil
	encoded, _ = json.Marshal(source)
	issues = ImportCaptureDiscrepancies(other, encoded, ids, "RKRS_WT_RULE_UID", "runtime_config", "77", "123")
	if len(issues) != 1 || issues[0].Code != "catalog_invalid" {
		t.Fatal(issues)
	}
}
