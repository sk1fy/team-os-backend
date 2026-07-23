package richtext

import (
	"encoding/json"
	"reflect"
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

func TestFileIDs(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"type":"doc","content":[
		{"type":"attachment","attrs":{"fileId":"b"}},
		{"type":"image","attrs":{"file":{"id":"a"}}},
		{"type":"mention","attrs":{"id":"not-a-file"}},
		{"type":"attachment","attrs":{"fileId":"b"}}
	]}`)
	got := FileIDs(raw)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("FileIDs() = %v", got)
	}
}

func TestReplaceFileIDs(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"type":"doc","content":[{"type":"image","attrs":{"fileId":"old-a","title":"x"}},{"type":"attachment","attrs":{"file":{"id":"old-b","name":"doc"}}}]}`)
	rewritten, err := ReplaceFileIDs(raw, map[string]string{"old-a": "new-a", "old-b": "new-b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := FileIDs(rewritten); !reflect.DeepEqual(got, []string{"new-a", "new-b"}) {
		t.Fatalf("FileIDs() = %#v", got)
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

func TestNormalizeVideo(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"type":"doc","content":[{"type":"video","attrs":{"src":"https://WWW.YouTube.com/embed/abc#fragment"}}]}`)
	normalized, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(normalized) != `{"content":[{"attrs":{"provider":"youtube","src":"https://www.youtube.com/embed/abc"},"type":"video"}],"type":"doc"}` {
		t.Fatalf("Normalize() = %s", normalized)
	}
}

func TestRejectUnsafeVideo(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"type":"doc","content":[{"type":"video","attrs":{"src":"javascript:alert(1)"}}]}`,
		`{"type":"doc","content":[{"type":"video","attrs":{"src":"https://127.0.0.1/video.mp4"}}]}`,
		`{"type":"doc","content":[{"type":"video","attrs":{"src":"https://evil.example/embed/1"}}]}`,
	} {
		if _, err := Normalize(json.RawMessage(raw)); err == nil {
			t.Fatalf("Normalize(%s) unexpectedly succeeded", raw)
		}
	}
}
