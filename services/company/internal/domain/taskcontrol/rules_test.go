package taskcontrol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRulesFixtureAndValidation(t *testing.T) {
	raw, e := os.ReadFile("../../../../../contracts/fixtures/task-control-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Rules    []Rule
		Checksum string
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if issues := ValidateRules(f.Rules); len(issues) > 0 {
		t.Fatal(issues)
	}
	_, sum := Legacy(f.Rules)
	if sum != f.Checksum {
		t.Fatal(sum)
	}
	f.Rules[0].Users = []string{"9007199254740992"}
	if len(ValidateRules(f.Rules)) == 0 {
		t.Fatal("unsafe ID accepted")
	}
}
func TestLegacyCanonicalOrder(t *testing.T) {
	raw, e := os.ReadFile("../../../../../contracts/fixtures/task-control-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Rules    []Rule
		Checksum string
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	f.Rules[0].Name = "  " + f.Rules[0].Name + "  "
	f.Rules[0].Users = []string{"7", "2"}
	f.Rules[0].Pipelines[0].StatusIDs = []string{"201", "200"}
	f.Rules[0], f.Rules[1] = f.Rules[1], f.Rules[0]
	_, sum := Legacy(f.Rules)
	if sum != f.Checksum {
		t.Fatal("normalization mismatch", sum)
	}
	var rule Rule
	if json.Unmarshal([]byte(`{"ruleUid":"a0000000-0000-4000-8000-000000000001","name":"a"}`), &rule) == nil {
		t.Fatal("missing flags accepted")
	}
}
