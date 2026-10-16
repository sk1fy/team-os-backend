package taskcontrol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// The release matrix distinguishes selectors that follow future membership from
// saved explicit sets. Every case resolves the same immutable published intent.
func TestR08DynamicAudienceReleaseMatrix(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*OrganizationSnapshot, *ReferenceSnapshot)
		want   []string
	}{
		{"current department and descendants", func(*OrganizationSnapshot, *ReferenceSnapshot) {}, []string{"101", "102"}},
		{"employee joins descendant", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) {
			o.Employees[2].DepartmentIDs = []uuid.UUID{o.Departments[1].ID}
		}, []string{"101", "102", "103"}},
		{"employee leaves selected tree", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Employees[0].DepartmentIDs = []uuid.UUID{} }, []string{"102"}},
		{"employee becomes inactive", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Employees[0].Active = false }, []string{"102"}},
		{"mapping removed", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Mappings = o.Mappings[1:] }, []string{"102"}},
		{"CRM account mapping is foreign", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Mappings[0].AccountID = "999" }, []string{"102"}},
		{"CRM identity becomes inactive", func(_ *OrganizationSnapshot, r *ReferenceSnapshot) { r.Users[0].IsActive = false }, []string{"102"}},
		{"child leaves selected tree", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Departments[1].ParentID = nil }, []string{"101"}},
		{"empty department never means all", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) {
			for i := range o.Employees {
				o.Employees[i].DepartmentIDs = []uuid.UUID{}
			}
		}, []string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rules, refs, org := resolverFixture()
			before, _ := json.Marshal(rules)
			c.change(&org, &refs)
			got, e := ResolveIntentRules(rules, refs, org, false)
			if e != nil || got.HasBlockingIssues() {
				t.Fatal(e, got.Issues)
			}
			users := []string{}
			if len(got.Rules) > 0 {
				users = got.Rules[0].Users
			}
			if !reflect.DeepEqual(users, c.want) {
				t.Fatalf("users=%v want=%v", users, c.want)
			}
			after, _ := json.Marshal(rules)
			if string(before) != string(after) {
				t.Fatal("source change rewrote published intent")
			}
			projection, _ := ProjectRuntimeRules(got.Rules, "103")
			wantActor := false
			for _, id := range c.want {
				wantActor = wantActor || id == "103"
			}
			if (len(projection) > 0) != wantActor {
				t.Fatal("actor projection disagrees with membership", projection)
			}
		})
	}
}

func TestR08FutureScopesAndLegacyExplicitRemainDistinct(t *testing.T) {
	rules, refs, org := resolverFixture()
	base := *rules[0].Dynamic
	selected := base
	selected.RuleUID = uuid.New()
	selected.Leads = LeadSelection{Kind: "selected", Pipelines: []PipelineSelection{{PipelineID: "10", Stages: StageSelection{Kind: "selected", StatusIDs: []string{"2"}}}}}
	allInSelected := base
	allInSelected.RuleUID = uuid.New()
	allInSelected.Leads = LeadSelection{Kind: "selected", Pipelines: []PipelineSelection{{PipelineID: "10", Stages: StageSelection{Kind: "allOpen"}}}}
	explicit := Rule{RuleUID: uuid.New(), Name: "Автономное явное правило", Users: []string{"101"}, Pipelines: []Pipeline{{PipelineID: "10", StatusIDs: []string{"2", "142", "143"}}}, CustomerStatuses: []string{}, Enabled: true, DenyExit: true, CheckResponsible: true}
	rules = append(rules, IntentRule{Kind: "dynamic", Dynamic: &selected}, IntentRule{Kind: "dynamic", Dynamic: &allInSelected}, IntentRule{Kind: "legacyExplicit", Legacy: &explicit})
	before, _ := json.Marshal(rules)
	refs.Pipelines[0].Statuses = append(refs.Pipelines[0].Statuses, ReferenceStatus{ID: "3", Kind: "open"})
	// Even malformed upstream metadata must not expand closed stages into allOpen.
	refs.Pipelines[0].Statuses[1].Kind = "open"
	refs.Pipelines[0].Statuses[2].Kind = "open"
	refs.Pipelines = append(refs.Pipelines, ReferencePipeline{ID: "20", Statuses: []ReferenceStatus{{ID: "21", Kind: "open"}, {ID: "22", Kind: "won"}}})
	org.Employees[2].DepartmentIDs = []uuid.UUID{org.Departments[0].ID}
	got, e := ResolveIntentRules(rules, refs, org, false)
	if e != nil || got.HasBlockingIssues() || len(got.Rules) != 4 {
		t.Fatal(e, got)
	}
	want := [][]Pipeline{
		{{PipelineID: "10", StatusIDs: []string{"2", "3"}}, {PipelineID: "20", StatusIDs: []string{"21"}}},
		{{PipelineID: "10", StatusIDs: []string{"2"}}},
		{{PipelineID: "10", StatusIDs: []string{"2", "3"}}},
		{{PipelineID: "10", StatusIDs: []string{"2", "142", "143"}}},
	}
	for i, r := range got.Rules {
		if !reflect.DeepEqual(r.Pipelines, want[i]) {
			t.Fatalf("rule %d pipelines=%v want=%v", i, r.Pipelines, want[i])
		}
	}
	if !reflect.DeepEqual(got.Rules[3].Users, []string{"101"}) || !got.Rules[3].DenyExit || !got.Rules[3].CheckResponsible {
		t.Fatal("legacy explicit scope or flags expanded", got.Rules[3])
	}
	actor, _ := ProjectRuntimeRules(got.Rules, "101")
	if len(actor) != 4 {
		t.Fatal("overlapping rules collapsed", actor)
	}
	outsider, _ := ProjectRuntimeRules(got.Rules, "999")
	if len(outsider) != 0 {
		t.Fatal("empty actor projection expanded", outsider)
	}
	after, _ := json.Marshal(rules)
	if string(before) != string(after) {
		t.Fatal("future source change mutated explicit or dynamic intent")
	}
}
