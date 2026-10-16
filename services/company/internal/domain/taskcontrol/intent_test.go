package taskcontrol

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func intentFixture() []IntentRule {
	return []IntentRule{{Kind: "dynamic", Dynamic: &RuleV2{SchemaVersion: 2, RuleUID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Name: " Контроль 🦊 ", Audience: AudienceSelection{Kind: "selected", EmployeeIDs: []uuid.UUID{}, Departments: []DepartmentSelection{{DepartmentID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), IncludeDescendants: true}}}, Leads: LeadSelection{Kind: "selected", Pipelines: []PipelineSelection{{PipelineID: "10", Stages: StageSelection{Kind: "selected", StatusIDs: []string{"11", "2"}}}}}, Customers: CustomerSelection{Kind: "disabled"}, Enabled: true, ShowModal: true}}}
}
func TestIntentStrictUnion(t *testing.T) {
	raw, e := json.Marshal(intentFixture())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ParseIntentRules(raw); e != nil {
		t.Fatal(e)
	}
	cases := []struct{ name, old, replacement string }{
		{"unknown union field", `"kind":"dynamic"`, `"kind":"dynamic","users":[]`},
		{"mixed selector", `"kind":"selected","employeeIds":[]`, `"kind":"allLinked","employeeIds":[]`},
		{"required bool", `"includeDescendants":true`, `"ignored":true`},
		{"unknown nested", `"pipelineId":"10"`, `"pipelineId":"10","foo":true`},
		{"duplicate key", `"enabled":true`, `"enabled":false,"enabled":true`},
		{"null", `"archived":false`, `"archived":null`},
		{"closed stage", `"11","2"`, `"142","2"`},
		{"empty stages", `"statusIds":["11","2"]`, `"statusIds":[]`},
		{"foreign schema", `"schemaVersion":2`, `"schemaVersion":3`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(string(raw), tc.old, tc.replacement, 1)
			if mutated == string(raw) {
				t.Fatal("bad test mutation")
			}
			if _, e := ParseIntentRules([]byte(mutated)); e == nil {
				t.Fatal("accepted invalid intent")
			}
		})
	}
	if _, e := ParseIntentRules(append(raw, []byte(`[]`)...)); e == nil {
		t.Fatal("accepted trailing value")
	}
	if _, e := ParseIntentRules([]byte("null")); e == nil {
		t.Fatal("accepted null rules")
	}
}
func TestIntentChecksumCanonicalSetsAndRace(t *testing.T) {
	rules := intentFixture()
	h, e := IntentChecksum(rules)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(rules)
	var permuted []IntentRule
	if e = json.Unmarshal(raw, &permuted); e != nil {
		t.Fatal(e)
	}
	permuted[0].Dynamic.Name = strings.TrimSpace(permuted[0].Dynamic.Name)
	permuted[0].Dynamic.Leads.Pipelines[0].Stages.StatusIDs = []string{"2", "11"}
	other, e := IntentChecksum(permuted)
	if e != nil || h != other {
		t.Fatalf("canonical hash mismatch %s %s %v", h, other, e)
	}
	permuted[0].Dynamic.Audience.Departments[0].IncludeDescendants = false
	other, _ = IntentChecksum(permuted)
	if h == other {
		t.Fatal("semantic change retained checksum")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				sum, e := IntentChecksum(rules)
				if e != nil || sum != h {
					t.Error("unstable concurrent checksum")
				}
			}
		}()
	}
	wg.Wait()
	if rules[0].Dynamic.Leads.Pipelines[0].Stages.StatusIDs[0] != "11" {
		t.Fatal("checksum mutated input")
	}
}
func TestIntentUIDAcrossKinds(t *testing.T) {
	rules := intentFixture()
	r := Rule{RuleUID: rules[0].Dynamic.RuleUID, Name: "legacy", Users: []string{"5"}, Pipelines: []Pipeline{{"10", []string{"142"}}}, CustomerStatuses: []string{}, Enabled: true}
	rules = append(rules, IntentRule{Kind: "legacyExplicit", Legacy: &r})
	if len(ValidateIntentRules(rules)) == 0 {
		t.Fatal("accepted duplicate UID across kinds")
	}
}

func TestIntentSelectionStorageRoundTrip(t *testing.T) {
	for _, peopleOnly := range []bool{false, true} {
		rules := intentFixture()
		if peopleOnly {
			rules[0].Dynamic.Audience.EmployeeIDs = []uuid.UUID{uuid.New()}
			rules[0].Dynamic.Audience.Departments = []DepartmentSelection{}
		}
		raw, e := json.Marshal(rules)
		if e != nil {
			t.Fatal(e)
		}
		decoded, e := ParseIntentRules(raw)
		if e != nil {
			t.Fatal("stored intent not readable", string(raw), e)
		}
		rawAgain, e := json.Marshal(decoded)
		if e != nil || string(rawAgain) != string(raw) {
			t.Fatal("round-trip changed selected union", string(raw), string(rawAgain), e)
		}
		before, e := IntentChecksum(rules)
		if e != nil {
			t.Fatal(e)
		}
		after, e := IntentChecksum(decoded)
		if e != nil || before != after {
			t.Fatal("storage changed intent checksum", e)
		}
	}
}
func TestIntentLegacyUnknownFieldsRejected(t *testing.T) {
	r := Rule{RuleUID: uuid.New(), Name: "legacy", Users: []string{"5"}, Pipelines: []Pipeline{{"10", []string{"142"}}}, CustomerStatuses: []string{}, Enabled: true}
	raw, e := json.Marshal([]IntentRule{{Kind: "legacyExplicit", Legacy: &r}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ old, replacement string }{{`"name":"legacy"`, `"name":"legacy","unexpected":true`}, {`"pipelineId":"10"`, `"pipelineId":"10","unexpected":true`}} {
		mutated := strings.Replace(string(raw), tc.old, tc.replacement, 1)
		if _, e := ParseIntentRules([]byte(mutated)); e == nil {
			t.Fatal("accepted legacy unknown field", mutated)
		}
	}
}
