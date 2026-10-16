package taskcontrol

import "testing"

func TestReferenceRulesRejectForeignInactiveAndMisplacedSelections(t *testing.T) {
	refs := ReferenceSnapshot{Users: []ReferenceUser{{ID: "1", IsActive: true}, {ID: "2", IsActive: false}}, Pipelines: []ReferencePipeline{{ID: "10", Statuses: []ReferenceStatus{{ID: "11", Kind: "open"}, {ID: "142", Kind: "won"}}}, {ID: "20", Statuses: []ReferenceStatus{{ID: "21", Kind: "open"}}}}, Customers: ReferenceCustomers{Availability: "supported", Statuses: []ReferenceCustomerStatus{{ID: "31"}}}}
	base := Rule{Users: []string{"1"}, Pipelines: []Pipeline{{PipelineID: "10", StatusIDs: []string{"11", "142"}}}, Enabled: true}
	if issues := ValidateReferenceRules([]Rule{base}, refs); len(issues) > 0 {
		t.Fatal(issues)
	}
	tests := []struct {
		name   string
		change func(*Rule)
		code   string
	}{
		{"foreign user", func(r *Rule) { r.Users = []string{"999"} }, "unknown_crm_id"},
		{"inactive user", func(r *Rule) { r.Users = []string{"2"} }, "inactive_user"},
		{"foreign pipeline", func(r *Rule) { r.Pipelines = []Pipeline{{PipelineID: "99", StatusIDs: []string{"11"}}} }, "unknown_crm_id"},
		{"wrong pipeline status", func(r *Rule) { r.Pipelines = []Pipeline{{PipelineID: "10", StatusIDs: []string{"21"}}} }, "unknown_crm_id"},
		{"foreign customer", func(r *Rule) { r.Customers = true; r.CustomerStatuses = []string{"99"} }, "unknown_crm_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.change(&r)
			issues := ValidateReferenceRules([]Rule{r}, refs)
			if len(issues) != 1 || issues[0].Code != tc.code {
				t.Fatal(issues)
			}
			r.Archived = true
			if issues = ValidateReferenceRules([]Rule{r}, refs); len(issues) != 0 {
				t.Fatal("archive checked", issues)
			}
			r.Archived = false
			r.Enabled = false
			if issues = ValidateReferenceRules([]Rule{r}, refs); len(issues) != 0 {
				t.Fatal("disabled checked", issues)
			}
		})
	}
	refs.Customers.Availability = "unknown"
	if issues := ValidateReferenceRules([]Rule{base}, refs); len(issues) > 0 {
		t.Fatal("unknown customers blocked leads", issues)
	}
	base.Customers = true
	if issues := ValidateReferenceRules([]Rule{base}, refs); len(issues) != 1 || issues[0].Code != "incomplete_source" {
		t.Fatal(issues)
	}
	refs.Customers.Availability = "unsupported"
	if issues := ValidateReferenceRules([]Rule{base}, refs); len(issues) != 1 || issues[0].Code != "unknown_crm_id" {
		t.Fatal(issues)
	}
}
