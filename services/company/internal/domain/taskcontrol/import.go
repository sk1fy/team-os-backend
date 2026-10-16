package taskcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var importFields = []struct{ key, code, typ string }{
	{"rule_uid", "RKRS_WT_RULE_UID", "text"}, {"users", "RKRS_WT_USERS", "text"},
	{"pipelines", "RKRS_WT_PIPELINES", "text"}, {"customer_statuses", "RKRS_WT_CUSTOMER_STATUSES", "text"},
	{"show_modal", "RKRS_WT_SHOW_MODAL", "select"}, {"deny_exit", "RKRS_WT_DENY_EXIT", "select"},
	{"check_responsible", "RKRS_WT_CHECK_RESP", "select"}, {"customers", "RKRS_WT_CUSTOMERS", "select"},
	{"enabled", "RKRS_WT_ENABLED", "select"}, {"archived", "RKRS_WT_ARCHIVED", "select"},
}

func importJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := uniqueJSON(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("лишние данные JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return d.Decode(out)
}
func importID(v any) (string, error) {
	var id string
	switch n := v.(type) {
	case json.Number:
		id = n.String()
	case string:
		id = n
	default:
		return "", errors.New("некорректный ID")
	}
	if !ValidCRMID(id) {
		return "", errors.New("небезопасный ID")
	}
	return id, nil
}
func importIDs(v any) ([]string, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, errors.New("ожидается массив ID")
	}
	out := make([]string, 0, len(a))
	seen := map[string]bool{}
	for _, v := range a {
		id, e := importID(v)
		if e != nil || seen[id] {
			return nil, errors.New("некорректные или повторные ID")
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}
func importLegacy(v any) (Rule, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Rule{}, errors.New("ожидается правило")
	}
	required := []string{"uid", "name", "users", "pipelines", "customer_statuses", "show_modal", "deny_exit", "check_responsible", "customers", "enabled", "archived"}
	if len(m) != len(required) {
		return Rule{}, errors.New("неизвестные или отсутствующие поля правила")
	}
	wire := map[string]any{}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return Rule{}, fmt.Errorf("отсутствует %s", k)
		}
	}
	wire["ruleUid"] = m["uid"]
	wire["name"] = m["name"]
	users, e := importIDs(m["users"])
	if e != nil {
		return Rule{}, e
	}
	wire["users"] = users
	statuses, e := importIDs(m["customer_statuses"])
	if e != nil {
		return Rule{}, e
	}
	wire["customerStatuses"] = statuses
	ps, ok := m["pipelines"].([]any)
	if !ok {
		return Rule{}, errors.New("ожидаются воронки")
	}
	pipelines := []Pipeline{}
	for _, v := range ps {
		p, ok := v.(map[string]any)
		if !ok || len(p) != 2 {
			return Rule{}, errors.New("некорректная воронка")
		}
		id, e := importID(p["pipeline_id"])
		if e != nil {
			return Rule{}, e
		}
		ids, e := importIDs(p["status_ids"])
		if e != nil {
			return Rule{}, e
		}
		pipelines = append(pipelines, Pipeline{id, ids})
	}
	wire["pipelines"] = pipelines
	for _, pair := range [][2]string{{"show_modal", "showModal"}, {"deny_exit", "denyExit"}, {"check_responsible", "checkResponsible"}, {"customers", "customers"}, {"enabled", "enabled"}, {"archived", "archived"}} {
		b, ok := m[pair[0]].(bool)
		if !ok {
			return Rule{}, errors.New("флаг должен быть boolean")
		}
		wire[pair[1]] = b
	}
	raw, _ := json.Marshal(wire)
	var r Rule
	e = json.Unmarshal(raw, &r)
	return r, e
}

// ValidateImportCapture checks that the immutable original and the normalized
// preview describe the same complete rules, without repairing identifiers.
func ValidateImportCapture(rules []Rule, raw json.RawMessage, fieldIDs map[string]json.RawMessage, code, kind, catalogID, accountID string) []Issue {
	fail := func(e error) []Issue {
		return []Issue{{"payload.raw", "invalid_source_capture", "Исходный снимок не соответствует правилам: " + e.Error()}}
	}
	if issues := ValidateRules(rules); len(issues) > 0 {
		return issues
	}
	if code != "RKRS_WT_RULE_UID" {
		return fail(errors.New("источник содержит ошибку"))
	}
	var source map[string]any
	if e := importJSON(raw, &source); e != nil {
		return fail(e)
	}
	cat, present := source["catalog"]
	if present && cat != nil || kind == "catalog" {
		metadata, ok := cat.(map[string]any)
		if !ok {
			return fail(errors.New("нет каталога"))
		}
		id, err := importID(metadata["id"])
		if err != nil || id != catalogID || metadata["type"] != "regular" {
			return fail(errors.New("неверный каталог"))
		}
		account, err := importID(metadata["account_id"])
		if err != nil || account != accountID {
			return fail(errors.New("каталог другого аккаунта"))
		}
	}
	var e error
	if len(fieldIDs) != len(importFields) {
		return fail(errors.New("неполная карта полей"))
	}
	mapped := map[string]bool{}
	for _, f := range importFields {
		var value any
		if err := importJSON(fieldIDs[f.key], &value); err != nil {
			return fail(err)
		}
		id, err := importID(value)
		if err != nil || mapped[id] {
			return fail(errors.New("неверная карта полей"))
		}
		mapped[id] = true
	}
	var original []Rule
	if kind == "runtime_config" {
		config := source["runtimeConfig"]
		for i := 0; i < 2; i++ {
			s, ok := config.(string)
			if !ok {
				break
			}
			if e = importJSON([]byte(s), &config); e != nil {
				return fail(e)
			}
		}
		cfg, ok := config.(map[string]any)
		if !ok {
			return fail(errors.New("нет runtime_config"))
		}
		if n, ok := cfg["schema_version"].(json.Number); !ok || n.String() != "1" {
			return fail(errors.New("неподдерживаемая схема"))
		}
		a, ok := cfg["rules"].([]any)
		if !ok {
			return fail(errors.New("нет массива runtime правил"))
		}
		original = []Rule{}
		for _, v := range a {
			r, e := importLegacy(v)
			if e != nil {
				return fail(e)
			}
			original = append(original, r)
		}
		_, hash := Legacy(original)
		pub, ok := cfg["published"].(map[string]any)
		if !ok || pub["hash"] != hash {
			return fail(errors.New("неверный hash runtime_config"))
		}
		storage, ok := cfg["storage"].(map[string]any)
		if !ok {
			return fail(errors.New("нет storage"))
		}
		id, e := importID(storage["catalog_id"])
		if e != nil || id != catalogID {
			return fail(errors.New("storage другого каталога"))
		}
		savedFields, ok := storage["fields"].(map[string]any)
		if !ok || len(savedFields) != len(fieldIDs) {
			return fail(errors.New("неполная storage.fields"))
		}
		for key, encoded := range fieldIDs {
			var current any
			if e := importJSON(encoded, &current); e != nil {
				return fail(e)
			}
			actual, e := importID(current)
			saved, err := importID(savedFields[key])
			if e != nil || err != nil || actual != saved {
				return fail(errors.New("storage.fields изменилась"))
			}
		}
	} else if kind == "catalog" {
		var err error
		original, err = importCatalogRules(source, fieldIDs)
		if err != nil {
			return fail(err)
		}
	} else {
		return fail(errors.New("неизвестный тип источника"))
	}
	if issues := ValidateRules(original); len(issues) > 0 {
		return fail(errors.New(issues[0].Message))
	}
	_, a := Legacy(original)
	_, b := Legacy(rules)
	if a != b {
		return fail(errors.New("UID, состав или поля правил отличаются"))
	}
	return []Issue{}
}

func importCatalogRules(source map[string]any, fieldIDs map[string]json.RawMessage) ([]Rule, error) {
	defs, ok := source["fields"].([]any)
	if !ok || len(fieldIDs) != len(importFields) {
		return nil, errors.New("неполная карта полей")
	}
	byID := map[string]map[string]any{}
	byCode := map[string]bool{}
	for _, v := range defs {
		d, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("некорректное поле")
		}
		id, e := importID(d["id"])
		c, _ := d["code"].(string)
		if e != nil || byID[id] != nil || (c != "" && byCode[c]) {
			return nil, errors.New("повтор или неверный ID поля")
		}
		byID[id] = d
		byCode[c] = true
	}
	mapping := map[string]string{}
	for _, f := range importFields {
		var v any
		if e := importJSON(fieldIDs[f.key], &v); e != nil {
			return nil, e
		}
		id, e := importID(v)
		if e != nil {
			return nil, e
		}
		d := byID[id]
		if d == nil || d["code"] != f.code || d["type"] != f.typ {
			return nil, errors.New("карта и определения полей расходятся")
		}
		mapping[f.key] = id
	}
	elements, ok := source["elements"].([]any)
	if !ok || len(elements) > 1000 {
		return nil, errors.New("нет полного массива элементов")
	}
	out := []Rule{}
	seen := map[string]bool{}
	for _, v := range elements {
		element, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("неверный элемент")
		}
		id, e := importID(element["id"])
		if e != nil || seen[id] {
			return nil, errors.New("повтор или неверный ID элемента")
		}
		seen[id] = true
		if deleted, _ := element["is_deleted"].(bool); deleted {
			continue
		}
		values, ok := element["custom_fields_values"].([]any)
		if !ok {
			return nil, errors.New("нет значений полей")
		}
		fields := map[string]map[string]any{}
		for _, v := range values {
			f, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("неверное значение поля")
			}
			id, e := importID(f["field_id"])
			if e != nil || fields[id] != nil {
				return nil, errors.New("повтор или неверный ID значения")
			}
			fields[id] = f
		}
		legacy := map[string]any{"name": element["name"]}
		for _, f := range importFields {
			field := fields[mapping[f.key]]
			if field == nil {
				return nil, errors.New("отсутствует поле " + f.key)
			}
			a, ok := field["values"].([]any)
			if !ok || len(a) != 1 {
				return nil, errors.New("ожидается одно значение поля")
			}
			entry, ok := a[0].(map[string]any)
			if !ok {
				return nil, errors.New("некорректное значение")
			}
			val := entry["value"]
			switch f.key {
			case "rule_uid":
				legacy["uid"] = val
			case "users", "pipelines", "customer_statuses":
				if s, ok := val.(string); ok {
					if e := importJSON([]byte(s), &val); e != nil {
						return nil, e
					}
				}
				legacy[f.key] = val
			default:
				if enum, exists := entry["enum_id"]; exists {
					id, e := importID(enum)
					if e != nil {
						return nil, e
					}
					options, ok := byID[mapping[f.key]]["enums"].([]any)
					if !ok {
						return nil, errors.New("нет enum")
					}
					found := false
					for _, v := range options {
						o, ok := v.(map[string]any)
						if !ok {
							continue
						}
						oid, _ := importID(o["id"])
						if oid == id {
							val = o["value"]
							found = true
						}
					}
					if !found {
						return nil, errors.New("неизвестный enum")
					}
				}
				switch b := val.(type) {
				case string:
					if b != "0" && b != "1" {
						return nil, errors.New("неверный boolean enum")
					}
					legacy[f.key] = b == "1"
				case bool:
					legacy[f.key] = b
				case json.Number:
					if b.String() != "0" && b.String() != "1" {
						return nil, errors.New("неверный boolean")
					}
					legacy[f.key] = b.String() == "1"
				default:
					return nil, errors.New("нет boolean")
				}
			}
		}
		r, e := importLegacy(legacy)
		if e != nil {
			return nil, e
		}
		r.Name = strings.TrimSpace(r.Name)
		out = append(out, r)
	}
	return out, nil
}

// ImportCaptureDiscrepancies compares the chosen source with the independently
// decoded alternative. The caller must separately validate the chosen capture.
func ImportCaptureDiscrepancies(rules []Rule, raw json.RawMessage, fieldIDs map[string]json.RawMessage, code, kind, catalogID, accountID string) []Issue {
	warning := func(code, message string) []Issue { return []Issue{{"payload.raw", code, message}} }
	var source map[string]any
	if importJSON(raw, &source) != nil {
		return []Issue{}
	}
	var other []Rule
	if kind == "catalog" {
		config := source["runtimeConfig"]
		invalid := func() []Issue {
			return warning("runtime_config_invalid", "Опубликованный автономный снимок повреждён или отсутствует; выбран каталог")
		}
		for i := 0; i < 2; i++ {
			s, ok := config.(string)
			if !ok {
				break
			}
			if importJSON([]byte(s), &config) != nil {
				return invalid()
			}
		}
		cfg, ok := config.(map[string]any)
		if !ok {
			return invalid()
		}
		a, ok := cfg["rules"].([]any)
		if !ok {
			return invalid()
		}
		other = []Rule{}
		for _, v := range a {
			r, e := importLegacy(v)
			if e != nil {
				return invalid()
			}
			other = append(other, r)
		}
		storage, ok := cfg["storage"].(map[string]any)
		if !ok {
			return invalid()
		}
		fields, ok := storage["fields"].(map[string]any)
		if !ok {
			return invalid()
		}
		ids := map[string]json.RawMessage{}
		for k, v := range fields {
			b, e := json.Marshal(v)
			if e != nil {
				return invalid()
			}
			ids[k] = b
		}
		if len(ValidateImportCapture(other, raw, ids, code, "runtime_config", catalogID, accountID)) > 0 {
			return invalid()
		}
	} else if kind == "runtime_config" {
		invalid := func() []Issue {
			return warning("catalog_invalid", "Каталог повреждён; выбран проверенный опубликованный автономный снимок")
		}
		defs, ok := source["fields"].([]any)
		if !ok {
			return invalid()
		}
		ids := map[string]json.RawMessage{}
		for _, v := range defs {
			d, ok := v.(map[string]any)
			if !ok {
				return invalid()
			}
			for _, f := range importFields {
				if d["code"] == f.code {
					if ids[f.key] != nil {
						return invalid()
					}
					b, e := json.Marshal(d["id"])
					if e != nil {
						return invalid()
					}
					ids[f.key] = b
				}
			}
		}
		var e error
		other, e = importCatalogRules(source, ids)
		if e != nil || len(ValidateImportCapture(other, raw, ids, code, "catalog", catalogID, accountID)) > 0 {
			return invalid()
		}
	} else {
		return []Issue{}
	}
	_, a := Legacy(rules)
	_, b := Legacy(other)
	if a != b {
		return warning("catalog_runtime_divergence", "Каталог и опубликованный автономный снимок содержат разные правила; требуется подтверждение выбранного источника")
	}
	return []Issue{}
}
