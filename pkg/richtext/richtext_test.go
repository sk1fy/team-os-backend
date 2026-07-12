package richtext

import (
	"encoding/json"
	"testing"
)

func TestPlainText(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"text","text":"Привет"},
				{"type":"text","text":" мир"}
			]}
		]
	}`)
	if got := PlainText(raw); got != "Привет мир" {
		t.Fatalf("PlainText() = %q, want %q", got, "Привет мир")
	}
}

func TestPlainTextInvalid(t *testing.T) {
	t.Parallel()
	if got := PlainText(json.RawMessage(`{"type":"paragraph"}`)); got != "" {
		t.Fatalf("PlainText() = %q, want empty", got)
	}
}

func TestMentions(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"type":"doc",
		"content":[
			{"type":"paragraph","content":[
				{"type":"mention","attrs":{"id":"user-2","label":"Иван"}},
				{"type":"text","text":" и "},
				{"type":"mention","attrs":{"id":"user-3","label":"Пётр"}}
			]}
		]
	}`)
	mentions := Mentions(raw)
	if len(mentions) != 2 {
		t.Fatalf("Mentions() len = %d, want 2", len(mentions))
	}
}