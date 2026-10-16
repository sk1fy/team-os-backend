package taskcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const IntentMaxBytes = 1 << 20

type DepartmentSelection struct {
	DepartmentID       uuid.UUID `json:"departmentId"`
	IncludeDescendants bool      `json:"includeDescendants"`
}
type AudienceSelection struct {
	Kind        string                `json:"kind"`
	EmployeeIDs []uuid.UUID           `json:"employeeIds,omitempty"`
	Departments []DepartmentSelection `json:"departments,omitempty"`
}
type StageSelection struct {
	Kind      string   `json:"kind"`
	StatusIDs []string `json:"statusIds,omitempty"`
}
type PipelineSelection struct {
	PipelineID string         `json:"pipelineId"`
	Stages     StageSelection `json:"stages"`
}
type LeadSelection struct {
	Kind      string              `json:"kind"`
	Pipelines []PipelineSelection `json:"pipelines,omitempty"`
}
type CustomerSelection struct {
	Kind      string   `json:"kind"`
	StatusIDs []string `json:"statusIds,omitempty"`
}
type RuleV2 struct {
	SchemaVersion    int               `json:"schemaVersion"`
	RuleUID          uuid.UUID         `json:"ruleUid"`
	Name             string            `json:"name"`
	Audience         AudienceSelection `json:"audience"`
	Leads            LeadSelection     `json:"leads"`
	Customers        CustomerSelection `json:"customers"`
	ShowModal        bool              `json:"showModal"`
	DenyExit         bool              `json:"denyExit"`
	CheckResponsible bool              `json:"checkResponsible"`
	Enabled          bool              `json:"enabled"`
	Archived         bool              `json:"archived"`
}

// IntentRule retains the explicit v1 representation without inferring dynamic intent.
type IntentRule struct {
	Kind    string
	Dynamic *RuleV2
	Legacy  *Rule
}

func (r IntentRule) MarshalJSON() ([]byte, error) {
	var rule any
	switch {
	case r.Kind == "dynamic" && r.Dynamic != nil && r.Legacy == nil:
		rule = r.Dynamic
	case r.Kind == "legacyExplicit" && r.Legacy != nil && r.Dynamic == nil:
		rule = r.Legacy
	default:
		return nil, errors.New("некорректный вид намерения")
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
		Rule any    `json:"rule"`
	}{r.Kind, rule})
}
func strictObject(raw []byte, target any, fields ...string) error {
	if err := uniqueJSON(json.NewDecoder(bytes.NewReader(raw))); err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if len(m) != len(fields) {
		return errors.New("неверные поля объекта")
	}
	for _, f := range fields {
		v, ok := m[f]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("обязательное поле %s", f)
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func (r *IntentRule) UnmarshalJSON(raw []byte) error {
	var v struct {
		Kind string          `json:"kind"`
		Rule json.RawMessage `json:"rule"`
	}
	if e := strictObject(raw, &v, "kind", "rule"); e != nil {
		return e
	}
	*r = IntentRule{Kind: v.Kind}
	switch v.Kind {
	case "dynamic":
		r.Dynamic = &RuleV2{}
		return json.Unmarshal(v.Rule, r.Dynamic)
	case "legacyExplicit":
		r.Legacy = &Rule{}
		return json.Unmarshal(v.Rule, r.Legacy)
	default:
		return errors.New("неизвестный вид намерения")
	}
}
func (r *RuleV2) UnmarshalJSON(raw []byte) error {
	type plain RuleV2
	var v plain
	err := strictObject(raw, &v, "schemaVersion", "ruleUid", "name", "audience", "leads", "customers", "showModal", "denyExit", "checkResponsible", "enabled", "archived")
	if err == nil {
		*r = RuleV2(v)
	}
	return err
}
func (r *AudienceSelection) UnmarshalJSON(raw []byte) error {
	type plain AudienceSelection
	var v plain
	var k struct {
		Kind string `json:"kind"`
	}
	if e := json.Unmarshal(raw, &k); e != nil {
		return e
	}
	fields := []string{"kind"}
	if k.Kind == "selected" {
		fields = append(fields, "employeeIds", "departments")
	} else if k.Kind != "allLinked" {
		return errors.New("неизвестный выбор сотрудников")
	}
	if e := strictObject(raw, &v, fields...); e != nil {
		return e
	}
	*r = AudienceSelection(v)
	return nil
}
func (r *DepartmentSelection) UnmarshalJSON(raw []byte) error {
	type plain DepartmentSelection
	var v plain
	if e := strictObject(raw, &v, "departmentId", "includeDescendants"); e != nil {
		return e
	}
	*r = DepartmentSelection(v)
	return nil
}
func (r *StageSelection) UnmarshalJSON(raw []byte) error {
	type plain StageSelection
	var v plain
	var k struct {
		Kind string `json:"kind"`
	}
	if e := json.Unmarshal(raw, &k); e != nil {
		return e
	}
	fields := []string{"kind"}
	if k.Kind == "selected" {
		fields = append(fields, "statusIds")
	} else if k.Kind != "allOpen" {
		return errors.New("неизвестный выбор этапов")
	}
	if e := strictObject(raw, &v, fields...); e != nil {
		return e
	}
	*r = StageSelection(v)
	return nil
}
func (r *PipelineSelection) UnmarshalJSON(raw []byte) error {
	type plain PipelineSelection
	var v plain
	if e := strictObject(raw, &v, "pipelineId", "stages"); e != nil {
		return e
	}
	*r = PipelineSelection(v)
	return nil
}
func (r *LeadSelection) UnmarshalJSON(raw []byte) error {
	type plain LeadSelection
	var v plain
	var k struct {
		Kind string `json:"kind"`
	}
	if e := json.Unmarshal(raw, &k); e != nil {
		return e
	}
	fields := []string{"kind"}
	if k.Kind == "selected" {
		fields = append(fields, "pipelines")
	} else if k.Kind != "allOpen" && k.Kind != "disabled" {
		return errors.New("неизвестный выбор сделок")
	}
	if e := strictObject(raw, &v, fields...); e != nil {
		return e
	}
	*r = LeadSelection(v)
	return nil
}
func (r *CustomerSelection) UnmarshalJSON(raw []byte) error {
	type plain CustomerSelection
	var v plain
	var k struct {
		Kind string `json:"kind"`
	}
	if e := json.Unmarshal(raw, &k); e != nil {
		return e
	}
	fields := []string{"kind"}
	if k.Kind == "selected" {
		fields = append(fields, "statusIds")
	} else if k.Kind != "all" && k.Kind != "disabled" {
		return errors.New("неизвестный выбор покупателей")
	}
	if e := strictObject(raw, &v, fields...); e != nil {
		return e
	}
	*r = CustomerSelection(v)
	return nil
}

// ParseIntentRules rejects unknown fields, duplicate object keys and trailing JSON.
func ParseIntentRules(raw []byte) ([]IntentRule, error) {
	if len(raw) > IntentMaxBytes {
		return nil, errors.New("намерение превышает 1 МиБ")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := uniqueJSON(d); e != nil {
		return nil, e
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, errors.New("лишние данные JSON")
	}
	var rules []IntentRule
	if e := json.Unmarshal(raw, &rules); e != nil {
		return nil, e
	}
	if issues := ValidateIntentRules(rules); len(issues) > 0 {
		return nil, errors.New(issues[0].Message)
	}
	return rules, nil
}
func uniqueJSON(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("повторяющееся поле JSON")
			}
			seen[key] = true
			if e := uniqueJSON(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e := uniqueJSON(d); e != nil {
				return e
			}
		}
	default:
		return errors.New("неверный JSON")
	}
	_, e = d.Token()
	return e
}
func validUUID(v uuid.UUID) bool {
	return v != uuid.Nil && v.Variant() == uuid.RFC4122 && v.Version() >= 1 && v.Version() <= 5
}
func crmSet(v []string, required bool) bool {
	if v == nil || len(v) > 1000 || (required && len(v) == 0) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range v {
		if !ValidCRMID(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}

// ValidateIntentRules checks structure only; saving a draft does not require live references.
func ValidateIntentRules(rules []IntentRule) []Issue {
	out := []Issue{}
	add := func(i int, m string) {
		out = append(out, Issue{Path: fmt.Sprintf("intentRules[%d]", i), Code: "invalid_selection", Message: m})
	}
	seen := map[uuid.UUID]bool{}
	if rules == nil || len(rules) > 1000 {
		add(0, "Требуется массив не более 1000 правил")
	}
	for i, item := range rules {
		var id uuid.UUID
		if item.Kind == "legacyExplicit" && item.Legacy != nil && item.Dynamic == nil {
			id = item.Legacy.RuleUID
			for _, e := range ValidateRules([]Rule{*item.Legacy}) {
				add(i, e.Message)
			}
		} else if item.Kind == "dynamic" && item.Dynamic != nil && item.Legacy == nil {
			r := item.Dynamic
			id = r.RuleUID
			if r.SchemaVersion != 2 || strings.TrimSpace(r.Name) == "" || utf8.RuneCountInString(r.Name) > 200 {
				add(i, "Требуются schemaVersion 2 и название от 1 до 200 символов")
			}
			a := r.Audience
			switch a.Kind {
			case "allLinked":
				if a.EmployeeIDs != nil || a.Departments != nil {
					add(i, "allLinked не содержит списков")
				}
			case "selected":
				if a.EmployeeIDs == nil || a.Departments == nil || len(a.EmployeeIDs)+len(a.Departments) == 0 || len(a.EmployeeIDs) > 1000 || len(a.Departments) > 1000 {
					add(i, "Нужен непустой выбор сотрудников или отделов")
				}
				ids := map[uuid.UUID]bool{}
				for _, v := range a.EmployeeIDs {
					if !validUUID(v) || ids[v] {
						add(i, "Некорректный UUID сотрудника")
					}
					ids[v] = true
				}
				ids = map[uuid.UUID]bool{}
				for _, v := range a.Departments {
					if !validUUID(v.DepartmentID) || ids[v.DepartmentID] {
						add(i, "Некорректный UUID отдела")
					}
					ids[v.DepartmentID] = true
				}
			default:
				add(i, "Неизвестный выбор сотрудников")
			}
			l := r.Leads
			switch l.Kind {
			case "disabled", "allOpen":
				if l.Pipelines != nil {
					add(i, "Этот выбор сделок не содержит воронок")
				}
			case "selected":
				if len(l.Pipelines) == 0 || len(l.Pipelines) > 1000 {
					add(i, "Нужен непустой выбор воронок")
				}
				ps := map[string]bool{}
				for _, p := range l.Pipelines {
					if !ValidCRMID(p.PipelineID) || ps[p.PipelineID] {
						add(i, "Некорректная воронка")
					}
					ps[p.PipelineID] = true
					switch p.Stages.Kind {
					case "allOpen":
						if p.Stages.StatusIDs != nil {
							add(i, "allOpen не содержит этапов")
						}
					case "selected":
						if !crmSet(p.Stages.StatusIDs, true) {
							add(i, "Нужен непустой выбор этапов")
						}
						for _, id := range p.Stages.StatusIDs {
							if id == "142" || id == "143" {
								add(i, "Закрытые этапы нельзя выбирать в динамическом правиле")
							}
						}
					default:
						add(i, "Неизвестный выбор этапов")
					}
				}
			default:
				add(i, "Неизвестный выбор сделок")
			}
			switch r.Customers.Kind {
			case "disabled", "all":
				if r.Customers.StatusIDs != nil {
					add(i, "Этот выбор покупателей не содержит этапов")
				}
			case "selected":
				if !crmSet(r.Customers.StatusIDs, true) {
					add(i, "Нужен непустой выбор статусов покупателей")
				}
			default:
				add(i, "Неизвестный выбор покупателей")
			}
			if l.Kind == "disabled" && r.Customers.Kind == "disabled" {
				add(i, "Правилу нужны сделки или покупатели")
			}
		} else {
			add(i, "Некорректный вид намерения")
		}
		if !validUUID(id) || seen[id] {
			add(i, "Требуется уникальный UUID правила")
		}
		seen[id] = true
	}
	return out
}
func sortCRM(v []string) {
	sort.Slice(v, func(i, j int) bool {
		a, _ := strconv.ParseInt(v[i], 10, 64)
		b, _ := strconv.ParseInt(v[j], 10, 64)
		return a < b
	})
}

// IntentChecksum hashes canonical semantic intent without mutating caller-owned slices.
func IntentChecksum(rules []IntentRule) (string, error) {
	if issues := ValidateIntentRules(rules); len(issues) > 0 {
		return "", errors.New(issues[0].Message)
	}
	raw, e := json.Marshal(rules)
	if e != nil {
		return "", e
	}
	var copyRules []IntentRule
	if e = json.Unmarshal(raw, &copyRules); e != nil {
		return "", e
	}
	uid := func(r IntentRule) string {
		if r.Dynamic != nil {
			return r.Dynamic.RuleUID.String()
		}
		return r.Legacy.RuleUID.String()
	}
	sort.Slice(copyRules, func(i, j int) bool { return uid(copyRules[i]) < uid(copyRules[j]) })
	for _, item := range copyRules {
		if r := item.Dynamic; r != nil {
			r.Name = strings.TrimSpace(r.Name)
			sort.Slice(r.Audience.EmployeeIDs, func(i, j int) bool { return r.Audience.EmployeeIDs[i].String() < r.Audience.EmployeeIDs[j].String() })
			sort.Slice(r.Audience.Departments, func(i, j int) bool {
				return r.Audience.Departments[i].DepartmentID.String() < r.Audience.Departments[j].DepartmentID.String()
			})
			sort.Slice(r.Leads.Pipelines, func(i, j int) bool {
				return numericIDLess(r.Leads.Pipelines[i].PipelineID, r.Leads.Pipelines[j].PipelineID)
			})
			for i := range r.Leads.Pipelines {
				sortCRM(r.Leads.Pipelines[i].Stages.StatusIDs)
			}
			sortCRM(r.Customers.StatusIDs)
		} else {
			r := item.Legacy
			r.Name = strings.TrimSpace(r.Name)
			sortCRM(r.Users)
			sortCRM(r.CustomerStatuses)
			sort.Slice(r.Pipelines, func(i, j int) bool { return numericIDLess(r.Pipelines[i].PipelineID, r.Pipelines[j].PipelineID) })
			for i := range r.Pipelines {
				sortCRM(r.Pipelines[i].StatusIDs)
			}
		}
	}
	raw, e = json.Marshal(copyRules)
	if e != nil {
		return "", e
	}
	var mapped any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e = d.Decode(&mapped); e != nil {
		return "", e
	}
	raw, e = canonicalJSON(mapped)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func numericIDLess(a, b string) bool {
	x, _ := strconv.ParseInt(a, 10, 64)
	y, _ := strconv.ParseInt(b, 10, 64)
	return x < y
}
func (r AudienceSelection) MarshalJSON() ([]byte, error) {
	if r.Kind == "selected" {
		return json.Marshal(struct {
			Kind        string                `json:"kind"`
			EmployeeIDs []uuid.UUID           `json:"employeeIds"`
			Departments []DepartmentSelection `json:"departments"`
		}{r.Kind, r.EmployeeIDs, r.Departments})
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{r.Kind})
}
func (r LeadSelection) MarshalJSON() ([]byte, error) {
	if r.Kind == "selected" {
		return json.Marshal(struct {
			Kind      string              `json:"kind"`
			Pipelines []PipelineSelection `json:"pipelines"`
		}{r.Kind, r.Pipelines})
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{r.Kind})
}
func (r StageSelection) MarshalJSON() ([]byte, error) {
	if r.Kind == "selected" {
		return json.Marshal(struct {
			Kind      string   `json:"kind"`
			StatusIDs []string `json:"statusIds"`
		}{r.Kind, r.StatusIDs})
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{r.Kind})
}
func (r CustomerSelection) MarshalJSON() ([]byte, error) {
	if r.Kind == "selected" {
		return json.Marshal(struct {
			Kind      string   `json:"kind"`
			StatusIDs []string `json:"statusIds"`
		}{r.Kind, r.StatusIDs})
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{r.Kind})
}
