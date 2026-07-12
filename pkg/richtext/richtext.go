// Package richtext extracts plain text and mention nodes from TipTap JSON documents.
package richtext

import (
	"encoding/json"
	"strings"
)

// Document is the minimal TipTap root node: { type: "doc", content?: [...] }.
type Document struct {
	Type    string           `json:"type"`
	Content []json.RawMessage `json:"content,omitempty"`
}

// PlainText walks a TipTap document and returns concatenated visible text.
func PlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil || document.Type != "doc" {
		return ""
	}
	var builder strings.Builder
	collectText(document.Content, &builder)
	return strings.Join(strings.Fields(builder.String()), " ")
}

// Mentions returns unique user IDs from TipTap mention nodes.
func Mentions(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil || document.Type != "doc" {
		return nil
	}
	seen := make(map[string]struct{})
	collectMentions(document.Content, seen)
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result
}

func collectText(nodes []json.RawMessage, builder *strings.Builder) {
	for _, node := range nodes {
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(node, &generic); err != nil {
			continue
		}
		typeName := stringValue(generic["type"])
		if text := stringValue(generic["text"]); text != "" {
			if builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteString(text)
		}
		if content, ok := generic["content"]; ok {
			var children []json.RawMessage
			if err := json.Unmarshal(content, &children); err == nil {
				collectText(children, builder)
			}
		}
		if typeName == "hardBreak" && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
	}
}

func collectMentions(nodes []json.RawMessage, seen map[string]struct{}) {
	for _, node := range nodes {
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(node, &generic); err != nil {
			continue
		}
		if stringValue(generic["type"]) == "mention" {
			if attrs, ok := generic["attrs"]; ok {
				var mentionAttrs struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(attrs, &mentionAttrs); err == nil {
					id := strings.TrimSpace(mentionAttrs.ID)
					if id != "" {
						seen[id] = struct{}{}
					}
				}
			}
		}
		if content, ok := generic["content"]; ok {
			var children []json.RawMessage
			if err := json.Unmarshal(content, &children); err == nil {
				collectMentions(children, seen)
			}
		}
	}
}

func stringValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}