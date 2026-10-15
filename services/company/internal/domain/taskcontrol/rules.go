package taskcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Pipeline struct {
	PipelineID string   `json:"pipelineId"`
	StatusIDs  []string `json:"statusIds"`
}
type Rule struct {
	RuleUID          uuid.UUID  `json:"ruleUid"`
	Name             string     `json:"name"`
	Users            []string   `json:"users"`
	Pipelines        []Pipeline `json:"pipelines"`
	Customers        bool       `json:"customers"`
	CustomerStatuses []string   `json:"customerStatuses"`
	ShowModal        bool       `json:"showModal"`
	DenyExit         bool       `json:"denyExit"`
	CheckResponsible bool       `json:"checkResponsible"`
	Enabled          bool       `json:"enabled"`
	Archived         bool       `json:"archived"`
}
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateRules(rules []Rule) []Issue {
	out := []Issue{}
	seen := map[uuid.UUID]bool{}
	add := func(i int, message string) {
		out = append(out, Issue{fmt.Sprintf("rules[%d]", i), "invalid_rule", message})
	}
	if rules == nil || len(rules) > 1000 {
		add(0, "Требуется массив не более 1000 правил")
	}
	ids := func(values []string, required bool) bool {
		if values == nil || len(values) > 1000 || (required && len(values) == 0) {
			return false
		}
		m := map[string]bool{}
		for _, v := range values {
			if !ValidCRMID(v) || m[v] {
				return false
			}
			m[v] = true
		}
		return true
	}
	for i, r := range rules {
		if r.RuleUID == uuid.Nil || r.RuleUID.Version() < 1 || r.RuleUID.Version() > 5 || r.RuleUID.Variant() != uuid.RFC4122 || seen[r.RuleUID] {
			out = append(out, Issue{fmt.Sprintf("rules[%d].ruleUid", i), "duplicate_uid", "Требуется уникальный UUID правила"})
		}
		seen[r.RuleUID] = true
		if strings.TrimSpace(r.Name) == "" || utf8.RuneCountInString(r.Name) > 200 {
			add(i, "Название должно содержать от 1 до 200 символов")
		}
		if !ids(r.Users, true) || !ids(r.CustomerStatuses, false) || r.Pipelines == nil || len(r.Pipelines) > 1000 {
			add(i, "Некорректные идентификаторы или списки")
		}
		ps := map[string]bool{}
		for _, p := range r.Pipelines {
			if !ValidCRMID(p.PipelineID) || ps[p.PipelineID] || !ids(p.StatusIDs, true) {
				add(i, "Некорректная воронка или статусы")
			}
			ps[p.PipelineID] = true
		}
		if !r.Customers && len(r.Pipelines) == 0 {
			add(i, "Активному правилу нужны воронка или покупатели")
		}
	}
	return out
}

// Field order matches the widget's version-1 legacy checksum material.
type LegacyPipeline struct {
	PipelineID int64   `json:"pipeline_id"`
	StatusIDs  []int64 `json:"status_ids"`
}
type LegacyRule struct {
	UID              string           `json:"uid"`
	Name             string           `json:"name"`
	Users            []int64          `json:"users"`
	Pipelines        []LegacyPipeline `json:"pipelines"`
	CustomerStatuses []int64          `json:"customer_statuses"`
	ShowModal        bool             `json:"show_modal"`
	DenyExit         bool             `json:"deny_exit"`
	CheckResponsible bool             `json:"check_responsible"`
	Customers        bool             `json:"customers"`
	Enabled          bool             `json:"enabled"`
	Archived         bool             `json:"archived"`
}

func Legacy(rules []Rule) ([]LegacyRule, string) {
	ids := func(a []string) []int64 {
		out := make([]int64, 0, len(a))
		for _, s := range a {
			v, _ := strconv.ParseInt(s, 10, 64)
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	out := make([]LegacyRule, 0, len(rules))
	for _, r := range rules {
		p := make([]LegacyPipeline, 0, len(r.Pipelines))
		for _, v := range r.Pipelines {
			n, _ := strconv.ParseInt(v.PipelineID, 10, 64)
			p = append(p, LegacyPipeline{n, ids(v.StatusIDs)})
		}
		sort.Slice(p, func(i, j int) bool { return p[i].PipelineID < p[j].PipelineID })
		out = append(out, LegacyRule{r.RuleUID.String(), strings.TrimSpace(r.Name), ids(r.Users), p, ids(r.CustomerStatuses), r.ShowModal, r.DenyExit, r.CheckResponsible, r.Customers, r.Enabled, r.Archived})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(out)
	raw := bytes.TrimSuffix(b.Bytes(), []byte("\n"))
	raw = bytes.ReplaceAll(raw, []byte(`\u2028`), []byte(" "))
	raw = bytes.ReplaceAll(raw, []byte(`\u2029`), []byte(" "))
	h := sha256.Sum256(raw)
	return out, hex.EncodeToString(h[:])
}

func (r *Rule) UnmarshalJSON(raw []byte) error {
	type plain Rule
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"ruleUid", "name", "users", "pipelines", "customers", "customerStatuses", "showModal", "denyExit", "checkResponsible", "enabled", "archived"} {
		if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("обязательное поле %s", key)
		}
	}
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*r = Rule(value)
	return nil
}
