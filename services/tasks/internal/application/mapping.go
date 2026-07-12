package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	domainrecurrence "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/recurrence"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

func boardFromDB(value db.Board) Board {
	return Board{
		ID: value.ID, CompanyID: value.CompanyID, Name: value.Name, Type: value.Type,
		DepartmentID: uuidPointer(value.DepartmentID), OwnerID: uuidPointer(value.OwnerID),
		CreatedAt: value.CreatedAt,
	}
}

func columnFromDB(value db.Column) TaskColumn {
	return TaskColumn{
		ID: value.ID, BoardID: value.BoardID, Name: value.Name,
		Order: value.Order, Color: stringPointer(value.Color),
	}
}

func labelFromDB(value db.Label) Label {
	return Label{
		ID: value.ID, CompanyID: value.CompanyID, Name: value.Name, Color: value.Color,
	}
}

func taskFromDB(value db.Task) (Task, error) {
	checklist, err := checklistFromJSON(value.Checklist)
	if err != nil {
		return Task{}, err
	}
	attachments, err := attachmentsFromJSON(value.Attachments)
	if err != nil {
		return Task{}, err
	}
	source, err := sourceFromJSON(value.Source)
	if err != nil {
		return Task{}, err
	}
	recurrence, err := recurrenceFromJSON(value.Recurrence)
	if err != nil {
		return Task{}, err
	}
	return Task{
		ID: value.ID, CompanyID: value.CompanyID, BoardID: value.BoardID,
		ColumnID: value.ColumnID, Order: value.Order, Title: value.Title,
		Description: append(json.RawMessage(nil), value.Description...),
		AuthorID: value.AuthorID,
		AssigneeIDs: append([]uuid.UUID(nil), value.AssigneeIds...),
		AssigneePositionID: uuidPointer(value.AssigneePositionID),
		WatcherIDs: append([]uuid.UUID(nil), value.WatcherIds...),
		DueDate: timePointer(value.DueDate), Priority: value.Priority,
		LabelIDs: append([]uuid.UUID(nil), value.LabelIds...),
		Checklist: checklist, Attachments: attachments, Source: source,
		LinkedArticleIDs: append([]uuid.UUID(nil), value.LinkedArticleIds...),
		Recurrence: recurrence, CompletedAt: timePointer(value.CompletedAt),
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

func commentFromDB(value db.Comment) Comment {
	return Comment{
		ID: value.ID, TaskID: value.TaskID, AuthorID: value.AuthorID,
		Content: append(json.RawMessage(nil), value.Content...),
		CreatedAt: value.CreatedAt,
	}
}

func checklistToJSON(items []ChecklistItem) ([]byte, error) {
	if items == nil {
		items = []ChecklistItem{}
	}
	payload := make([]map[string]any, len(items))
	for index, item := range items {
		payload[index] = map[string]any{
			"id": item.ID.String(), "text": item.Text, "done": item.Done,
		}
	}
	return json.Marshal(payload)
}

func checklistFromJSON(raw []byte) ([]ChecklistItem, error) {
	if len(raw) == 0 {
		return []ChecklistItem{}, nil
	}
	var payload []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("разобрать checklist: %w", err)
	}
	result := make([]ChecklistItem, 0, len(payload))
	for _, item := range payload {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return nil, validation("Некорректный идентификатор пункта чеклиста")
		}
		result = append(result, ChecklistItem{ID: id, Text: item.Text, Done: item.Done})
	}
	return result, nil
}

func attachmentsToJSON(items []Attachment) ([]byte, error) {
	if items == nil {
		items = []Attachment{}
	}
	payload := make([]map[string]any, len(items))
	for index, item := range items {
		payload[index] = map[string]any{
			"id": item.ID.String(), "name": item.Name, "url": item.URL,
			"size": item.Size, "mimeType": item.MimeType,
		}
	}
	return json.Marshal(payload)
}

func attachmentsFromJSON(raw []byte) ([]Attachment, error) {
	if len(raw) == 0 {
		return []Attachment{}, nil
	}
	var payload []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Size     int64  `json:"size"`
		MimeType string `json:"mimeType"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("разобрать attachments: %w", err)
	}
	result := make([]Attachment, 0, len(payload))
	for _, item := range payload {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return nil, validation("Некорректный идентификатор вложения")
		}
		result = append(result, Attachment{
			ID: id, Name: item.Name, URL: item.URL, Size: item.Size, MimeType: item.MimeType,
		})
	}
	return result, nil
}

func sourceToJSON(value *TaskSource) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	payload := map[string]any{
		"type": value.Type, "title": value.Title, "url": value.URL,
	}
	if value.FunnelName != nil {
		payload["funnelName"] = *value.FunnelName
	}
	if value.StageName != nil {
		payload["stageName"] = *value.StageName
	}
	return json.Marshal(payload)
}

func sourceFromJSON(raw []byte) (*TaskSource, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload struct {
		Type       string  `json:"type"`
		Title      string  `json:"title"`
		URL        string  `json:"url"`
		FunnelName *string `json:"funnelName"`
		StageName  *string `json:"stageName"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("разобрать source: %w", err)
	}
	return &TaskSource{
		Type: payload.Type, Title: payload.Title, URL: payload.URL,
		FunnelName: payload.FunnelName, StageName: payload.StageName,
	}, nil
}

func recurrenceToJSON(value *RecurrenceRule) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	payload := map[string]any{
		"frequency": string(value.Frequency),
		"interval":  value.Interval,
	}
	if len(value.Weekdays) > 0 {
		payload["weekdays"] = value.Weekdays
	}
	return json.Marshal(payload)
}

func recurrenceFromJSON(raw []byte) (*RecurrenceRule, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload struct {
		Frequency string `json:"frequency"`
		Interval  int    `json:"interval"`
		Weekdays  []int  `json:"weekdays"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("разобрать recurrence: %w", err)
	}
	if err := validateRecurrenceFrequency(payload.Frequency); err != nil {
		return nil, err
	}
	if payload.Interval < 1 {
		return nil, validation("Интервал повторения должен быть не меньше 1")
	}
	return &RecurrenceRule{
		Frequency: domainrecurrence.Frequency(payload.Frequency),
		Interval:  payload.Interval,
		Weekdays:  append([]int(nil), payload.Weekdays...),
	}, nil
}

func validatePriority(value string) error {
	switch value {
	case "low", "medium", "high", "urgent":
		return nil
	default:
		return validation("Некорректный приоритет задачи")
	}
}

func validateRecurrenceFrequency(value string) error {
	switch value {
	case string(domainrecurrence.FrequencyDaily), string(domainrecurrence.FrequencyWeekly), string(domainrecurrence.FrequencyMonthly):
		return nil
	default:
		return validation("Некорректная частота повторения")
	}
}

func requiredText(value, message string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", validation(message)
	}
	return normalized, nil
}

func parseUUIDList(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func uuidPointer(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func stringPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}

func nullableTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}