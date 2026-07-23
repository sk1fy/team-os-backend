// Package richtext extracts plain text and mention nodes from TipTap JSON documents.
package richtext

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
)

var ErrInvalidDocument = errors.New("некорректный TipTap-документ")

// Document is the minimal TipTap root node: { type: "doc", content?: [...] }.
type Document struct {
	Type    string            `json:"type"`
	Content []json.RawMessage `json:"content,omitempty"`
}

// Validate checks the public RichTextContent invariant without constraining
// nested TipTap nodes, whose schema is intentionally extensible.
func Validate(raw json.RawMessage) error {
	var root map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil || root == nil {
		return ErrInvalidDocument
	}
	if len(root) < 1 || len(root) > 2 {
		return ErrInvalidDocument
	}
	for field := range root {
		if field != "type" && field != "content" {
			return ErrInvalidDocument
		}
	}
	if stringValue(root["type"]) != "doc" {
		return ErrInvalidDocument
	}
	if content, ok := root["content"]; ok {
		var nodes []json.RawMessage
		if err := json.Unmarshal(content, &nodes); err != nil || nodes == nil {
			return ErrInvalidDocument
		}
	}
	if _, err := Normalize(raw); err != nil {
		return err
	}
	return nil
}

// Normalize validates structured video nodes and returns canonical TipTap JSON.
// It never accepts embed HTML; video nodes must use attrs.src/provider/mimeType.
func Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil || root == nil || root["type"] != "doc" {
		return nil, ErrInvalidDocument
	}
	if err := normalizeNode(root); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(root)
	if err != nil {
		return nil, ErrInvalidDocument
	}
	return normalized, nil
}

func normalizeNode(node map[string]any) error {
	if node["type"] == "video" {
		attrs, ok := node["attrs"].(map[string]any)
		if !ok {
			return fmt.Errorf("%w: у видео отсутствуют attrs", ErrInvalidDocument)
		}
		if _, hasHTML := attrs["html"]; hasHTML {
			return fmt.Errorf("%w: HTML-код видео запрещён", ErrInvalidDocument)
		}
		src, ok := attrs["src"].(string)
		if !ok {
			return fmt.Errorf("%w: у видео отсутствует HTTPS URL", ErrInvalidDocument)
		}
		normalizedURL, provider, err := normalizeVideoURL(src, attrs)
		if err != nil {
			return err
		}
		attrs["src"] = normalizedURL
		attrs["provider"] = provider
	}
	if content, ok := node["content"].([]any); ok {
		for _, child := range content {
			childNode, ok := child.(map[string]any)
			if !ok {
				return ErrInvalidDocument
			}
			if err := normalizeNode(childNode); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeVideoURL(raw string, attrs map[string]any) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", "", fmt.Errorf("%w: разрешены только HTTPS-ссылки на видео", ErrInvalidDocument)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if unsafeVideoHost(host) {
		return "", "", fmt.Errorf("%w: локальные адреса для видео запрещены", ErrInvalidDocument)
	}
	provider := ""
	switch host {
	case "youtube.com", "www.youtube.com", "youtu.be", "youtube-nocookie.com", "www.youtube-nocookie.com":
		provider = "youtube"
	case "vimeo.com", "www.vimeo.com", "player.vimeo.com":
		provider = "vimeo"
	case "rutube.ru", "www.rutube.ru":
		provider = "rutube"
	default:
		extension := strings.ToLower(path.Ext(parsed.Path))
		allowedMIME := map[string]string{".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg"}
		expectedMIME, allowed := allowedMIME[extension]
		if !allowed {
			return "", "", fmt.Errorf("%w: домен iframe не входит в allowlist", ErrInvalidDocument)
		}
		if mimeType, exists := attrs["mimeType"].(string); exists && mimeType != "" && mimeType != expectedMIME {
			return "", "", fmt.Errorf("%w: недопустимый MIME-тип видео", ErrInvalidDocument)
		}
		attrs["mimeType"] = expectedMIME
		provider = "direct"
	}
	parsed.Scheme = "https"
	if port := parsed.Port(); port != "" && port != "443" {
		return "", "", fmt.Errorf("%w: нестандартный порт видео запрещён", ErrInvalidDocument)
	}
	parsed.Host = host
	parsed.Fragment = ""
	return parsed.String(), provider, nil
}

func unsafeVideoHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	return net.ParseIP(host) != nil
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

// FileIDs returns unique file identifiers referenced by TipTap file-like nodes.
// Generic attrs.id values are deliberately ignored so mention and application
// identifiers are never mistaken for attachments.
func FileIDs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil || document.Type != "doc" {
		return nil
	}
	seen := make(map[string]struct{})
	collectFileIDs(document.Content, seen)
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// ReplaceFileIDs rewrites file references in TipTap attrs.fileId and
// attrs.file.id while preserving all unrelated extensible node attributes.
func ReplaceFileIDs(raw json.RawMessage, replacements map[string]string) (json.RawMessage, error) {
	if err := Validate(raw); err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, ErrInvalidDocument
	}
	replaceFileIDsInNode(root, replacements)
	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, ErrInvalidDocument
	}
	return encoded, nil
}

func replaceFileIDsInNode(node map[string]any, replacements map[string]string) {
	if attrs, ok := node["attrs"].(map[string]any); ok {
		if fileID, ok := attrs["fileId"].(string); ok {
			if replacement, exists := replacements[fileID]; exists {
				attrs["fileId"] = replacement
			}
		}
		if file, ok := attrs["file"].(map[string]any); ok {
			if fileID, ok := file["id"].(string); ok {
				if replacement, exists := replacements[fileID]; exists {
					file["id"] = replacement
				}
			}
		}
	}
	if content, ok := node["content"].([]any); ok {
		for _, child := range content {
			if childNode, ok := child.(map[string]any); ok {
				replaceFileIDsInNode(childNode, replacements)
			}
		}
	}
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

func collectFileIDs(nodes []json.RawMessage, seen map[string]struct{}) {
	for _, node := range nodes {
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(node, &generic); err != nil {
			continue
		}
		if attrs, ok := generic["attrs"]; ok {
			var values struct {
				FileID string `json:"fileId"`
				File   *struct {
					ID string `json:"id"`
				} `json:"file"`
			}
			if err := json.Unmarshal(attrs, &values); err == nil {
				if id := strings.TrimSpace(values.FileID); id != "" {
					seen[id] = struct{}{}
				}
				if values.File != nil {
					if id := strings.TrimSpace(values.File.ID); id != "" {
						seen[id] = struct{}{}
					}
				}
			}
		}
		if content, ok := generic["content"]; ok {
			var children []json.RawMessage
			if err := json.Unmarshal(content, &children); err == nil {
				collectFileIDs(children, seen)
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
