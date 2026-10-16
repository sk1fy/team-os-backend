package taskcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// ReferenceRevision hashes the sorted semantic DTO using the same JCS subset as Core.
func ReferenceRevision(s ReferenceSnapshot) (string, error) {
	raw, e := json.Marshal(s)
	if e != nil {
		return "", e
	}
	var c ReferenceSnapshot
	if e = json.Unmarshal(raw, &c); e != nil {
		return "", e
	}
	numeric := func(a, b string) bool {
		x, _ := strconv.ParseInt(a, 10, 64)
		y, _ := strconv.ParseInt(b, 10, 64)
		return x < y
	}
	sort.Slice(c.Users, func(i, j int) bool { return numeric(c.Users[i].ID, c.Users[j].ID) })
	sort.Slice(c.Groups, func(i, j int) bool { return numeric(c.Groups[i].ID, c.Groups[j].ID) })
	sort.Slice(c.Pipelines, func(i, j int) bool { return numeric(c.Pipelines[i].ID, c.Pipelines[j].ID) })
	for i := range c.Pipelines {
		st := c.Pipelines[i].Statuses
		sort.Slice(st, func(i, j int) bool { return numeric(st[i].ID, st[j].ID) })
	}
	sort.Slice(c.Customers.Statuses, func(i, j int) bool { return numeric(c.Customers.Statuses[i].ID, c.Customers.Statuses[j].ID) })
	semantic := map[string]any{"scope": c.Scope, "users": c.Users, "groups": c.Groups, "pipelines": c.Pipelines, "customers": map[string]any{"availability": c.Customers.Availability, "statuses": c.Customers.Statuses}}
	raw, e = json.Marshal(semantic)
	if e != nil {
		return "", e
	}
	var mapped any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if e = dec.Decode(&mapped); e != nil {
		return "", e
	}
	raw, e = canonicalJSON(mapped)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func canonicalJSON(value any) ([]byte, error) {
	// json.Marshal uses escaping compatible with ECMAScript except HTML and the two
	// line separators. Decode/encode every string explicitly to preserve literal
	// backslash-u sequences while allowing literal Unicode separator characters.
	switch v := value.(type) {
	case string:
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return nil, err
		}
		r := strings.TrimSuffix(b.String(), "\n")
		// Replace escapes only when preceded by an even number of backslashes.
		var out strings.Builder
		for i := 0; i < len(r); {
			if r[i] == '\\' && i+1 < len(r) {
				if strings.HasPrefix(r[i:], `\u2028`) {
					out.WriteRune('\u2028')
					i += 6
					continue
				}
				if strings.HasPrefix(r[i:], `\u2029`) {
					out.WriteRune('\u2029')
					i += 6
					continue
				}
				out.WriteString(r[i : i+2])
				i += 2
				continue
			}
			out.WriteByte(r[i])
			i++
		}
		return []byte(out.String()), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b bytes.Buffer
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := canonicalJSON(k)
			b.Write(kb)
			b.WriteByte(':')
			vb, e := canonicalJSON(v[k])
			if e != nil {
				return nil, e
			}
			b.Write(vb)
		}
		b.WriteByte('}')
		return b.Bytes(), nil
	case []any:
		var b bytes.Buffer
		b.WriteByte('[')
		for i, x := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			xb, e := canonicalJSON(x)
			if e != nil {
				return nil, e
			}
			b.Write(xb)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	default:
		return json.Marshal(value)
	}
}
