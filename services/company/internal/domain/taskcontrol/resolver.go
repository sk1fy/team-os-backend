package taskcontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

const RuntimeMaxBytes = 1 << 20
const ResolverVersion = 1
const ResolutionMaxIssues = 10000

type OrganizationEmployee struct {
	ID            uuid.UUID
	Active        bool
	DepartmentIDs []uuid.UUID
}
type OrganizationDepartment struct {
	ID       uuid.UUID
	ParentID *uuid.UUID
}

// EmployeeMapping is a confirmed identity proof, scoped to one company/account.
type EmployeeMapping struct {
	EmployeeID uuid.UUID
	AccountID  string
	CRMUserID  string
}
type OrganizationSnapshot struct {
	CompanyID            uuid.UUID
	AccountID            string
	OrganizationRevision int64
	MappingRevision      int64
	Employees            []OrganizationEmployee
	Departments          []OrganizationDepartment
	Mappings             []EmployeeMapping
}
type IntentIssue struct {
	RuleUID  uuid.UUID `json:"ruleUid"`
	Code     string    `json:"code"`
	Path     string    `json:"path"`
	Message  string    `json:"message"`
	Blocking bool      `json:"blocking"`
}
type Resolution struct {
	Rules            []Rule        `json:"-"`
	RuntimeRules     []LegacyRule  `json:"runtimeRules"`
	ResolvedChecksum string        `json:"resolvedChecksum"`
	Issues           []IntentIssue `json:"issues"`
}

// ResolveIntentRules materializes one coherent source snapshot. In publish mode
// explicit missing references are blocking. Later reconciliation excludes them
// without changing the saved intent. Source freshness is enforced by the caller.
func ResolveIntentRules(rules []IntentRule, refs ReferenceSnapshot, org OrganizationSnapshot, publish bool) (Resolution, error) {
	result := Resolution{Rules: []Rule{}, RuntimeRules: []LegacyRule{}, Issues: []IntentIssue{}}
	if issues := ValidateIntentRules(rules); len(issues) > 0 {
		return result, errors.New(issues[0].Message)
	}
	if org.CompanyID != refs.Scope.CompanyID || org.AccountID != refs.Scope.AccountID {
		return result, errors.New("снимок организации принадлежит другому аккаунту")
	}
	if len(org.Employees) > 10000 || len(org.Departments) > 1000 || len(org.Mappings) > 20000 {
		return result, errors.New("превышен лимит снимка организации")
	}
	employees := map[uuid.UUID]OrganizationEmployee{}
	departments := map[uuid.UUID]OrganizationDepartment{}
	users := map[string]bool{}
	mappings := map[uuid.UUID]map[string]bool{}
	reverseMappings := map[string]map[uuid.UUID]bool{}
	pipelines := map[string]ReferencePipeline{}
	customerStatuses := map[string]bool{}
	for _, e := range org.Employees {
		if !validUUID(e.ID) {
			return result, errors.New("некорректный UUID сотрудника")
		}
		if _, exists := employees[e.ID]; exists {
			return result, errors.New("повтор сотрудника в снимке")
		}
		employees[e.ID] = e
	}
	for _, d := range org.Departments {
		if !validUUID(d.ID) {
			return result, errors.New("некорректный UUID отдела")
		}
		if _, exists := departments[d.ID]; exists {
			return result, errors.New("повтор отдела в снимке")
		}
		departments[d.ID] = d
	}
	// Reject a cycle even outside the selected branch; otherwise descendant semantics
	// would depend on traversal order and cannot represent a coherent organization.
	for id := range departments {
		seen := map[uuid.UUID]bool{}
		current := id
		for {
			if seen[current] {
				return result, errors.New("цикл дерева отделов")
			}
			seen[current] = true
			d, ok := departments[current]
			if !ok || d.ParentID == nil {
				break
			}
			current = *d.ParentID
		}
	}
	for _, u := range refs.Users {
		users[u.ID] = u.IsActive
	}
	for _, p := range refs.Pipelines {
		pipelines[p.ID] = p
	}
	for _, s := range refs.Customers.Statuses {
		customerStatuses[s.ID] = true
	}
	for _, m := range org.Mappings {
		if m.AccountID != org.AccountID {
			continue
		}
		if !ValidCRMID(m.CRMUserID) {
			return result, errors.New("некорректная подтверждённая связь CRM")
		}
		if mappings[m.EmployeeID] == nil {
			mappings[m.EmployeeID] = map[string]bool{}
		}
		mappings[m.EmployeeID][m.CRMUserID] = true
		if reverseMappings[m.CRMUserID] == nil {
			reverseMappings[m.CRMUserID] = map[uuid.UUID]bool{}
		}
		reverseMappings[m.CRMUserID][m.EmployeeID] = true
	}
	issueLimitReached := false
	add := func(id uuid.UUID, path, code, message string, blocking bool) {
		if len(result.Issues) >= ResolutionMaxIssues {
			if !issueLimitReached {
				result.Issues = append(result.Issues, IntentIssue{RuleUID: id, Path: path, Code: "runtime_payload_too_large", Message: "Превышен лимит диагностики материализации", Blocking: true})
			}
			issueLimitReached = true
			return
		}
		result.Issues = append(result.Issues, IntentIssue{id, code, path, message, blocking})
	}
	for index, item := range rules {
		if issueLimitReached {
			return result, nil
		}
		path := fmt.Sprintf("intentRules[%d].rule", index)
		if old := item.Legacy; old != nil {
			if !old.Enabled || old.Archived {
				continue
			}
			for _, e := range ValidateReferenceRules([]Rule{*old}, refs) {
				add(old.RuleUID, path+e.Path[len("rules[0]"):], e.Code, e.Message, publish)
			}
			r := *old
			r.Users = []string{}
			for _, id := range old.Users {
				if users[id] {
					r.Users = append(r.Users, id)
				}
			}
			r.Pipelines = []Pipeline{}
			for _, p := range old.Pipelines {
				source, ok := pipelines[p.PipelineID]
				if !ok {
					continue
				}
				available := map[string]bool{}
				for _, s := range source.Statuses {
					available[s.ID] = true
				}
				statuses := []string{}
				for _, id := range p.StatusIDs {
					if available[id] {
						statuses = append(statuses, id)
					}
				}
				if len(statuses) > 0 {
					r.Pipelines = append(r.Pipelines, Pipeline{p.PipelineID, statuses})
				}
			}
			r.CustomerStatuses = []string{}
			r.Customers = old.Customers && refs.Customers.Availability == "supported"
			for _, id := range old.CustomerStatuses {
				if customerStatuses[id] {
					r.CustomerStatuses = append(r.CustomerStatuses, id)
				}
			}
			if old.Customers && refs.Customers.Availability == "unknown" {
				add(old.RuleUID, path+".customers", "incomplete_source", "Не подтверждён справочник покупателей", true)
			}
			if old.Customers && len(old.CustomerStatuses) > 0 && len(r.CustomerStatuses) == 0 {
				r.Customers = false
			}
			if len(r.Users) > 0 && (len(r.Pipelines) > 0 || r.Customers) {
				result.Rules = append(result.Rules, r)
			} else {
				add(old.RuleUID, path, "empty_resolution", "Правило не содержит исполняемых строк", false)
			}
			continue
		}
		d := item.Dynamic
		if !d.Enabled || d.Archived {
			continue
		}
		selected := map[uuid.UUID]bool{}
		explicit := map[uuid.UUID]bool{}
		if d.Audience.Kind == "allLinked" {
			for id := range employees {
				selected[id] = true
			}
		} else {
			for i, id := range d.Audience.EmployeeIDs {
				explicit[id] = true
				if _, ok := employees[id]; !ok {
					add(d.RuleUID, fmt.Sprintf("%s.audience.employeeIds[%d]", path, i), "employee_missing", "Сотрудник отсутствует", publish)
					continue
				}
				selected[id] = true
			}
			for i, selection := range d.Audience.Departments {
				if _, ok := departments[selection.DepartmentID]; !ok {
					add(d.RuleUID, fmt.Sprintf("%s.audience.departments[%d]", path, i), "department_missing", "Отдел отсутствует", publish)
					continue
				}
				included := map[uuid.UUID]bool{selection.DepartmentID: true}
				if selection.IncludeDescendants {
					for id := range departments {
						current := id
						for {
							if current == selection.DepartmentID {
								included[id] = true
								break
							}
							p, ok := departments[current]
							if !ok || p.ParentID == nil {
								break
							}
							current = *p.ParentID
						}
					}
				}
				for id, e := range employees {
					for _, departmentID := range e.DepartmentIDs {
						if included[departmentID] {
							selected[id] = true
							break
						}
					}
				}
			}
		}
		crmIDs := map[string]bool{}
		employeeIDs := make([]uuid.UUID, 0, len(selected))
		for id := range selected {
			employeeIDs = append(employeeIDs, id)
		}
		sort.Slice(employeeIDs, func(i, j int) bool { return employeeIDs[i].String() < employeeIDs[j].String() })
		for _, id := range employeeIDs {
			e := employees[id]
			code := ""
			crmID := ""
			if !e.Active {
				code = "employee_inactive"
			} else if len(mappings[id]) == 0 {
				code = "mapping_missing"
			} else if len(mappings[id]) != 1 {
				code = "mapping_ambiguous"
			} else {
				for candidate := range mappings[id] {
					crmID = candidate
				}
				active, exists := users[crmID]
				if len(reverseMappings[crmID]) != 1 {
					code = "mapping_ambiguous"
				} else if !exists {
					code = "crm_user_missing"
				} else if !active {
					code = "crm_user_inactive"
				}
			}
			if code != "" {
				add(d.RuleUID, path+".audience", code, "Сотрудник исключён: отсутствует однозначная активная связь CRM", publish && explicit[id])
				continue
			}
			crmIDs[crmID] = true
		}
		r := Rule{RuleUID: d.RuleUID, Name: d.Name, Users: []string{}, Pipelines: []Pipeline{}, CustomerStatuses: []string{}, ShowModal: d.ShowModal, DenyExit: d.DenyExit, CheckResponsible: d.CheckResponsible, Enabled: d.Enabled, Archived: d.Archived}
		for id := range crmIDs {
			r.Users = append(r.Users, id)
		}
		sortCRM(r.Users)
		selections := append([]PipelineSelection(nil), d.Leads.Pipelines...)
		if d.Leads.Kind == "allOpen" {
			selections = []PipelineSelection{}
			for id := range pipelines {
				selections = append(selections, PipelineSelection{PipelineID: id, Stages: StageSelection{Kind: "allOpen"}})
			}
			sort.Slice(selections, func(i, j int) bool { return numericIDLess(selections[i].PipelineID, selections[j].PipelineID) })
		}
		for i, selection := range selections {
			p, ok := pipelines[selection.PipelineID]
			pPath := fmt.Sprintf("%s.leads.pipelines[%d]", path, i)
			if !ok {
				add(d.RuleUID, pPath, "pipeline_missing", "Воронка отсутствует", publish)
				continue
			}
			statuses := map[string]ReferenceStatus{}
			for _, s := range p.Statuses {
				statuses[s.ID] = s
			}
			ids := []string{}
			if selection.Stages.Kind == "allOpen" {
				for _, s := range p.Statuses {
					if s.Kind == "open" && s.ID != "142" && s.ID != "143" {
						ids = append(ids, s.ID)
					}
				}
			} else {
				for _, id := range selection.Stages.StatusIDs {
					s, exists := statuses[id]
					if !exists {
						add(d.RuleUID, pPath+".stages", "status_missing", "Этап отсутствует в выбранной воронке", publish)
					} else if s.Kind != "open" || id == "142" || id == "143" {
						add(d.RuleUID, pPath+".stages", "closed_status", "Закрытый этап нельзя выбрать", publish)
					} else {
						ids = append(ids, id)
					}
				}
			}
			sortCRM(ids)
			if len(ids) > 0 {
				r.Pipelines = append(r.Pipelines, Pipeline{p.ID, ids})
			}
		}
		if d.Customers.Kind != "disabled" {
			switch refs.Customers.Availability {
			case "supported":
				r.Customers = true
				if d.Customers.Kind == "selected" {
					for _, id := range d.Customers.StatusIDs {
						if customerStatuses[id] {
							r.CustomerStatuses = append(r.CustomerStatuses, id)
						} else {
							add(d.RuleUID, path+".customers", "customer_status_missing", "Статус покупателя отсутствует", publish)
						}
					}
					if len(r.CustomerStatuses) == 0 {
						r.Customers = false
					}
				}
			case "unsupported":
				add(d.RuleUID, path+".customers", "customers_unsupported", "Покупатели недоступны", publish)
			default:
				add(d.RuleUID, path+".customers", "incomplete_source", "Не подтверждён справочник покупателей", true)
			}
		}
		if len(r.Users) > 1000 || len(r.Pipelines) > 1000 {
			add(d.RuleUID, path, "runtime_payload_too_large", "Превышен лимит материализации правила", true)
			continue
		}
		if len(r.Users) > 0 && (len(r.Pipelines) > 0 || r.Customers) {
			result.Rules = append(result.Rules, r)
		} else {
			add(d.RuleUID, path, "empty_resolution", "Правило не содержит исполняемых строк", false)
		}
	}
	if issueLimitReached {
		return result, nil
	}
	result.RuntimeRules, result.ResolvedChecksum = Legacy(result.Rules)
	payload, e := json.Marshal(result.RuntimeRules)
	if e != nil {
		return result, e
	}
	if len(payload) > RuntimeMaxBytes {
		add(uuid.Nil, "runtimeRules", "runtime_payload_too_large", "Исполняемый набор превышает 1 МиБ", true)
	}
	return result, nil
}

// HasBlockingIssues reports whether this resolution may replace the current snapshot.
func (r Resolution) HasBlockingIssues() bool {
	for _, i := range r.Issues {
		if i.Blocking {
			return true
		}
	}
	return false
}

// ProjectRuntimeRules returns only rules addressing the authenticated CRM actor.
// The full user sets never leave company through this actor-specific projection.
func ProjectRuntimeRules(rules []Rule, actorID string) ([]LegacyRule, string) {
	projected := []Rule{}
	for _, r := range rules {
		if !r.Enabled || r.Archived {
			continue
		}
		for _, id := range r.Users {
			if id == actorID {
				copyRule := r
				copyRule.Users = []string{actorID}
				projected = append(projected, copyRule)
				break
			}
		}
	}
	return Legacy(projected)
}
