package application

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

func TestTaskControlFullExportPreservesInactiveRulesAndFlags(t *testing.T) {
	old := tc.Rule{RuleUID: uuid.New(), Name: "Архив", Users: []string{"7"}, Pipelines: []tc.Pipeline{{PipelineID: "10", StatusIDs: []string{"142", "143"}}}, CustomerStatuses: []string{}, Enabled: false, Archived: true, ShowModal: true, DenyExit: true, CheckResponsible: true}
	dynamic := tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Весь отдел выключен", Audience: tc.AudienceSelection{Kind: "allLinked"}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: false, Archived: false}
	original := []tc.IntentRule{{Kind: "legacyExplicit", Legacy: &old}, {Kind: "dynamic", Dynamic: &dynamic}}
	before, _ := json.Marshal(original)
	material, e := taskControlExportIntents(original)
	if e != nil {
		t.Fatal(e)
	}
	if !material[0].Legacy.Enabled || material[0].Legacy.Archived || !material[1].Dynamic.Enabled {
		t.Fatal("inactive rules were omitted")
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("export edited publication intent")
	}
	computed := tc.Rule{RuleUID: dynamic.RuleUID, Name: dynamic.Name, Users: []string{"7", "8"}, Pipelines: []tc.Pipeline{{PipelineID: "10", StatusIDs: []string{"20"}}}, CustomerStatuses: []string{}, Enabled: true}
	result, e := taskControlRestoreExportFlags(original, tc.Resolution{Rules: []tc.Rule{*material[0].Legacy, computed}, Issues: []tc.IntentIssue{}})
	if e != nil {
		t.Fatal(e)
	}
	if len(result.RuntimeRules) != 2 || result.RuntimeRules[0].Enabled || result.RuntimeRules[1].Enabled {
		t.Fatal("full export lost disabled flags", result.RuntimeRules)
	}
	foundArchive := false
	for _, r := range result.RuntimeRules {
		if r.UID == old.RuleUID.String() {
			foundArchive = r.Archived && r.DenyExit && r.CheckResponsible && len(r.Pipelines[0].StatusIDs) == 2
		}
	}
	if !foundArchive {
		t.Fatal("archive or closed legacy stages changed")
	}
	_, sum := tc.Legacy(result.Rules)
	if sum != result.ResolvedChecksum {
		t.Fatal("export checksum does not cover complete rules")
	}
	if _, e = taskControlRestoreExportFlags(original, tc.Resolution{Rules: []tc.Rule{computed}}); e == nil {
		t.Fatal("partial export silently omitted archive")
	}
}
func TestTaskControlFullExportRetainsExplicitEmptyConfirmation(t *testing.T) {
	rule := tc.RuleV2{SchemaVersion: 2, RuleUID: uuid.New(), Name: "Нет связанных сотрудников", Audience: tc.AudienceSelection{Kind: "allLinked"}, Leads: tc.LeadSelection{Kind: "allOpen"}, Customers: tc.CustomerSelection{Kind: "disabled"}, Enabled: true}
	original := []tc.IntentRule{{Kind: "dynamic", Dynamic: &rule}}
	result, e := taskControlRestoreExportFlags(original, tc.Resolution{Rules: []tc.Rule{}, Issues: []tc.IntentIssue{{RuleUID: rule.RuleUID, Code: "empty_resolution", Path: "intentRules[0]", Message: "Нет состава"}}})
	if e != nil || len(result.RuntimeRules) != 0 {
		t.Fatal("explicit empty export regression", e)
	}
	rule.Enabled = false
	if _, e = taskControlRestoreExportFlags(original, tc.Resolution{Rules: []tc.Rule{}}); e == nil {
		t.Fatal("empty inactive metadata silently discarded")
	}
}
