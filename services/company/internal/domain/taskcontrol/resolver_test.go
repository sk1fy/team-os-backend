package taskcontrol

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func resolverFixture() ([]IntentRule, ReferenceSnapshot, OrganizationSnapshot) {
	rules := intentFixture()
	rules[0].Dynamic.Leads = LeadSelection{Kind: "allOpen"}
	company := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	d := rules[0].Dynamic.Audience.Departments[0].DepartmentID
	child := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	e1 := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	e2 := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	e3 := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	refs := ReferenceSnapshot{Scope: ReferenceScope{CompanyID: company, AccountID: "123"}, Users: []ReferenceUser{{ID: "101", IsActive: true}, {ID: "102", IsActive: true}, {ID: "103", IsActive: true}}, Pipelines: []ReferencePipeline{{ID: "10", Statuses: []ReferenceStatus{{ID: "2", Kind: "open"}, {ID: "142", Kind: "won"}, {ID: "143", Kind: "lost"}}}}, Customers: ReferenceCustomers{Availability: "supported", Statuses: []ReferenceCustomerStatus{{ID: "900"}}}}
	org := OrganizationSnapshot{CompanyID: company, AccountID: "123", OrganizationRevision: 1, MappingRevision: 1, Departments: []OrganizationDepartment{{ID: d}, {ID: child, ParentID: &d}}, Employees: []OrganizationEmployee{{ID: e1, Active: true, DepartmentIDs: []uuid.UUID{d}}, {ID: e2, Active: true, DepartmentIDs: []uuid.UUID{child}}, {ID: e3, Active: true, DepartmentIDs: []uuid.UUID{}}}, Mappings: []EmployeeMapping{{e1, "123", "101"}, {e2, "123", "102"}, {e3, "123", "103"}}}
	return rules, refs, org
}
func TestResolveDynamicFutureMembershipAndPipeline(t *testing.T) {
	rules, refs, org := resolverFixture()
	r, e := ResolveIntentRules(rules, refs, org, true)
	if e != nil || r.HasBlockingIssues() {
		t.Fatalf("resolution failed %+v %v", r.Issues, e)
	}
	if !reflect.DeepEqual(r.Rules[0].Users, []string{"101", "102"}) || !reflect.DeepEqual(r.Rules[0].Pipelines, []Pipeline{{"10", []string{"2"}}}) {
		t.Fatalf("unexpected resolution %+v", r.Rules)
	}
	originalChecksum, _ := IntentChecksum(rules)
	refs.Pipelines = append(refs.Pipelines, ReferencePipeline{ID: "20", Statuses: []ReferenceStatus{{ID: "22", Kind: "open"}}})
	refs.Pipelines[0].Statuses = append(refs.Pipelines[0].Statuses, ReferenceStatus{ID: "3", Kind: "open"})
	org.Employees[2].DepartmentIDs = []uuid.UUID{org.Departments[1].ID}
	next, e := ResolveIntentRules(rules, refs, org, false)
	if e != nil || next.HasBlockingIssues() {
		t.Fatal(e, next.Issues)
	}
	if len(next.Rules[0].Users) != 3 || len(next.Rules[0].Pipelines) != 2 || len(next.Rules[0].Pipelines[0].StatusIDs) != 2 || next.ResolvedChecksum == r.ResolvedChecksum {
		t.Fatal("future changes not resolved")
	}
	after, _ := IntentChecksum(rules)
	if originalChecksum != after {
		t.Fatal("resolver rewrote intent")
	}
	rules[0].Dynamic.Audience.Departments[0].IncludeDescendants = false
	r, _ = ResolveIntentRules(rules, refs, org, false)
	if !reflect.DeepEqual(r.Rules[0].Users, []string{"101"}) {
		t.Fatal("direct selection included descendants")
	}
}
func TestResolveEligibilityAndAccountIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		mutate     func(*OrganizationSnapshot, *ReferenceSnapshot)
	}{
		{"foreign account mapping", "mapping_missing", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Mappings[0].AccountID = "999" }},
		{"inactive employee", "employee_inactive", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Employees[0].Active = false }},
		{"inactive CRM user", "crm_user_inactive", func(_ *OrganizationSnapshot, r *ReferenceSnapshot) { r.Users[0].IsActive = false }},
		{"missing CRM user", "crm_user_missing", func(_ *OrganizationSnapshot, r *ReferenceSnapshot) { r.Users = r.Users[1:] }},
		{"ambiguous proof", "mapping_ambiguous", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) {
			o.Mappings = append(o.Mappings, EmployeeMapping{o.Employees[0].ID, "123", "103"})
		}},
		{"shared CRM identity", "mapping_ambiguous", func(o *OrganizationSnapshot, _ *ReferenceSnapshot) { o.Mappings[2].CRMUserID = "101" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules, refs, org := resolverFixture()
			rules[0].Dynamic.Audience = AudienceSelection{Kind: "selected", EmployeeIDs: []uuid.UUID{org.Employees[0].ID}, Departments: []DepartmentSelection{}}
			tc.mutate(&org, &refs)
			r, e := ResolveIntentRules(rules, refs, org, true)
			if e != nil {
				t.Fatal(e)
			}
			if !r.HasBlockingIssues() || len(r.Rules) != 0 {
				t.Fatal("publish accepted explicit invalid employee", r)
			}
			found := false
			for _, i := range r.Issues {
				if i.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s issue %+v", tc.code, r.Issues)
			}
			r, e = ResolveIntentRules(rules, refs, org, false)
			if e != nil || r.HasBlockingIssues() || len(r.Rules) != 0 {
				t.Fatal("reconciliation must exclude invalid employee", r, e)
			}
		})
	}
}
func TestResolveEmptyAndDeletedReferences(t *testing.T) {
	rules, refs, org := resolverFixture()
	org.Employees = []OrganizationEmployee{}
	r, e := ResolveIntentRules(rules, refs, org, true)
	if e != nil || r.HasBlockingIssues() || len(r.Rules) != 0 {
		t.Fatal("valid empty department must remain publishable", e, r)
	}
	org.Departments = []OrganizationDepartment{}
	r, e = ResolveIntentRules(rules, refs, org, true)
	if e != nil || !r.HasBlockingIssues() {
		t.Fatal("deleted explicit department publishable", e, r)
	}
	r, e = ResolveIntentRules(rules, refs, org, false)
	if e != nil || r.HasBlockingIssues() || len(r.Rules) != 0 {
		t.Fatal("deleted department must exclude only", e, r)
	}
	rules[0].Dynamic.Enabled = false
	r, e = ResolveIntentRules(rules, refs, org, true)
	if e != nil || r.HasBlockingIssues() {
		t.Fatal("disabled references should not block", e, r)
	}
}
func TestResolveCustomersSeparateAndLegacyPreserved(t *testing.T) {
	rules, refs, org := resolverFixture()
	rules[0].Dynamic.Customers = CustomerSelection{Kind: "all"}
	refs.Customers.Availability = "unknown"
	r, e := ResolveIntentRules(rules, refs, org, false)
	if e != nil || !r.HasBlockingIssues() {
		t.Fatal("unknown customers must block snapshot", e, r)
	}
	rules[0].Dynamic.Customers = CustomerSelection{Kind: "disabled"}
	refs.Pipelines[0].Name = "Покупатели"
	r, e = ResolveIntentRules(rules, refs, org, true)
	if e != nil || r.HasBlockingIssues() || r.Rules[0].Customers {
		t.Fatal("lead pipeline was treated as customers", e, r)
	}
	legacy := Rule{RuleUID: uuid.New(), Name: "explicit", Users: []string{"103"}, Pipelines: []Pipeline{{PipelineID: "10", StatusIDs: []string{"142"}}}, CustomerStatuses: []string{}, Enabled: true}
	org.Mappings = nil
	r, e = ResolveIntentRules([]IntentRule{{Kind: "legacyExplicit", Legacy: &legacy}}, refs, org, true)
	if e != nil || r.HasBlockingIssues() || len(r.Rules) != 1 || r.Rules[0].Pipelines[0].StatusIDs[0] != "142" {
		t.Fatal("legacy semantics changed", e, r)
	}
}
func TestResolveNoDepartmentOnlyExplicitPeopleAndActorProjection(t *testing.T) {
	rules, refs, org := resolverFixture()
	rules[0].Dynamic.Audience = AudienceSelection{Kind: "selected", EmployeeIDs: []uuid.UUID{org.Employees[2].ID}, Departments: []DepartmentSelection{}}
	r, e := ResolveIntentRules(rules, refs, org, true)
	if e != nil || r.HasBlockingIssues() || !reflect.DeepEqual(r.Rules[0].Users, []string{"103"}) {
		t.Fatal(e, r)
	}
	projection, _ := ProjectRuntimeRules(r.Rules, "103")
	if len(projection) != 1 || !reflect.DeepEqual(projection[0].Users, []int64{103}) {
		t.Fatal(projection)
	}
	none, _ := ProjectRuntimeRules(r.Rules, "101")
	if len(none) != 0 {
		t.Fatal(none)
	}
}
func TestResolveRejectCycleAndScope(t *testing.T) {
	rules, refs, org := resolverFixture()
	org.Departments[0].ParentID = &org.Departments[1].ID
	if _, e := ResolveIntentRules(rules, refs, org, true); e == nil {
		t.Fatal("accepted cycle")
	}
	org.Departments[0].ParentID = nil
	org.AccountID = "999"
	if _, e := ResolveIntentRules(rules, refs, org, true); e == nil {
		t.Fatal("accepted foreign account organization")
	}
}
func TestResolveReadOnlyConcurrent(t *testing.T) {
	rules, refs, org := resolverFixture()
	rules[0].Dynamic.Leads = LeadSelection{Kind: "selected", Pipelines: []PipelineSelection{{PipelineID: "20", Stages: StageSelection{Kind: "allOpen"}}, {PipelineID: "10", Stages: StageSelection{Kind: "allOpen"}}}}
	before, _ := json.Marshal(rules)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if _, e := ResolveIntentRules(rules, refs, org, false); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	wg.Wait()
	after, _ := json.Marshal(rules)
	if string(before) != string(after) {
		t.Fatal("resolution mutated shared intent")
	}
}
func TestResolvePayloadLimit(t *testing.T) {
	rules, refs, org := resolverFixture()
	rules[0].Dynamic.Audience = AudienceSelection{Kind: "allLinked"}
	base := *rules[0].Dynamic
	rules = []IntentRule{}
	for range 1000 {
		r := base
		r.RuleUID = uuid.New()
		r.Name = strings.Repeat("я", 200)
		rules = append(rules, IntentRule{Kind: "dynamic", Dynamic: &r})
	}
	org.Employees = []OrganizationEmployee{}
	org.Mappings = []EmployeeMapping{}
	refs.Users = []ReferenceUser{}
	for id := 1000; id < 1200; id++ {
		employeeID := uuid.New()
		crmID := strconv.Itoa(id)
		org.Employees = append(org.Employees, OrganizationEmployee{ID: employeeID, Active: true})
		org.Mappings = append(org.Mappings, EmployeeMapping{EmployeeID: employeeID, AccountID: "123", CRMUserID: crmID})
		refs.Users = append(refs.Users, ReferenceUser{ID: crmID, IsActive: true})
	}
	r, e := ResolveIntentRules(rules, refs, org, false)
	if e != nil {
		t.Fatal(e)
	}
	if !r.HasBlockingIssues() {
		t.Fatal("oversized payload accepted")
	}
	found := false
	for _, i := range r.Issues {
		if i.Code == "runtime_payload_too_large" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing payload limit diagnostic")
	}
}

func TestResolveOverlappingDepartmentsAndExplicitEmployee(t *testing.T) {
	rules, refs, org := resolverFixture()
	a := &rules[0].Dynamic.Audience
	a.EmployeeIDs = []uuid.UUID{org.Employees[1].ID}
	a.Departments = append(a.Departments, DepartmentSelection{DepartmentID: org.Departments[1].ID, IncludeDescendants: false})
	r, e := ResolveIntentRules(rules, refs, org, true)
	if e != nil || r.HasBlockingIssues() || len(r.Rules[0].Users) != 2 {
		t.Fatal("overlapping selection was not deduplicated", e, r)
	}
	org.Employees[1].DepartmentIDs = []uuid.UUID{}
	r, e = ResolveIntentRules(rules, refs, org, false)
	if e != nil || len(r.Rules[0].Users) != 2 {
		t.Fatal("explicit employee removed with department membership", e, r)
	}
	org.Mappings[1].CRMUserID = "103"
	org.Mappings = org.Mappings[:2]
	r, e = ResolveIntentRules(rules, refs, org, false)
	if e != nil || !reflect.DeepEqual(r.Rules[0].Users, []string{"101", "103"}) {
		t.Fatal("new confirmed employee mapping did not change CRM identity", e, r)
	}
}
func TestResolveExplicitStagesDoNotExpandAndMissingStatusBlocksPublish(t *testing.T) {
	rules, refs, org := resolverFixture()
	rules[0].Dynamic.Leads = LeadSelection{Kind: "selected", Pipelines: []PipelineSelection{{PipelineID: "10", Stages: StageSelection{Kind: "selected", StatusIDs: []string{"2"}}}}}
	refs.Pipelines[0].Statuses = append(refs.Pipelines[0].Statuses, ReferenceStatus{ID: "3", Kind: "open"})
	r, e := ResolveIntentRules(rules, refs, org, true)
	if e != nil || !reflect.DeepEqual(r.Rules[0].Pipelines[0].StatusIDs, []string{"2"}) {
		t.Fatal("explicit stages expanded", e, r)
	}
	refs.Pipelines[0].Statuses = refs.Pipelines[0].Statuses[1:]
	r, e = ResolveIntentRules(rules, refs, org, true)
	if e != nil || !r.HasBlockingIssues() || len(r.Rules) != 0 {
		t.Fatal("deleted explicit stage published", e, r)
	}
	r, e = ResolveIntentRules(rules, refs, org, false)
	if e != nil || r.HasBlockingIssues() || len(r.Rules) != 0 {
		t.Fatal("deleted stage did not resolve to empty safely", e, r)
	}
}

func TestResolveDiagnosticsBoundIsBlocking(t *testing.T) {
	rules, refs, org := resolverFixture()
	rules[0].Dynamic.Audience = AudienceSelection{Kind: "allLinked"}
	base := *rules[0].Dynamic
	rules = []IntentRule{}
	for range 11 {
		r := base
		r.RuleUID = uuid.New()
		rules = append(rules, IntentRule{Kind: "dynamic", Dynamic: &r})
	}
	org.Mappings = nil
	org.Employees = []OrganizationEmployee{}
	for range 1000 {
		org.Employees = append(org.Employees, OrganizationEmployee{ID: uuid.New(), Active: true})
	}
	r, e := ResolveIntentRules(rules, refs, org, false)
	if e != nil {
		t.Fatal(e)
	}
	if !r.HasBlockingIssues() || len(r.Issues) > ResolutionMaxIssues+1 || len(r.RuntimeRules) != 0 {
		t.Fatal("diagnostics overflow did not block materialization", len(r.Issues))
	}
	if r.Issues[len(r.Issues)-1].Code != "runtime_payload_too_large" {
		t.Fatal("missing typed limit issue")
	}
}
