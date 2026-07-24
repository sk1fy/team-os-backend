package application

import (
	"encoding/json"
	"testing"
)

func TestValidateVersionLessonFieldsTemplateSnapshot(t *testing.T) {
	t.Parallel()

	content := json.RawMessage(`{"type":"doc","content":[]}`)

	if err := validateVersionLessonFields(content, "template_snapshot", nil, nil, nil, false); err == nil {
		t.Fatal("ручное создание template_snapshot должно быть запрещено")
	}
	if err := validateVersionLessonFields(content, "template_snapshot", nil, nil, nil, true); err != nil {
		t.Fatalf("редактирование инстанцированного урока должно быть разрешено: %v", err)
	}
}
