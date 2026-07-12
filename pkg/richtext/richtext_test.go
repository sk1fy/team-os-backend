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

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "empty document", raw: `{"type":"doc"}`},
		{name: "document with content", raw: `{"type":"doc","content":[]}`},
		{name: "wrong root type", raw: `{"type":"html"}`, wantErr: true},
		{name: "missing type", raw: `{}`, wantErr: true},
		{name: "null content", raw: `{"type":"doc","content":null}`, wantErr: true},
		{name: "spaced null content", raw: `{"type":"doc","content": null }`, wantErr: true},
		{name: "non-array content", raw: `{"type":"doc","content":{}}`, wantErr: true},
		{name: "unknown root property", raw: `{"type":"doc","attrs":{}}`, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(json.RawMessage(test.raw))
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
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
