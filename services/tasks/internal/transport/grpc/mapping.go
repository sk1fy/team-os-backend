package grpc

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
	domainrecurrence "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/recurrence"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func boardToProto(value application.Board) *tasksv1.Board {
	return &tasksv1.Board{
		Id: value.ID.String(), Name: value.Name, Type: boardTypeToProto(value.Type),
		DepartmentId: optionalUUIDString(value.DepartmentID),
		OwnerId:      optionalUUIDString(value.OwnerID),
		CreatedAt:    timestamppb.New(value.CreatedAt.UTC()),
	}
}

func boardsToProto(values []application.Board) []*tasksv1.Board {
	result := make([]*tasksv1.Board, len(values))
	for index := range values {
		result[index] = boardToProto(values[index])
	}
	return result
}

func columnToProto(value application.TaskColumn) *tasksv1.TaskColumn {
	return &tasksv1.TaskColumn{
		Id: value.ID.String(), BoardId: value.BoardID.String(),
		Name: value.Name, Order: uint32(maxInt32(0, value.Order)),
		Color: optionalString(value.Color),
	}
}

func columnsToProto(values []application.TaskColumn) []*tasksv1.TaskColumn {
	result := make([]*tasksv1.TaskColumn, len(values))
	for index := range values {
		result[index] = columnToProto(values[index])
	}
	return result
}

func labelToProto(value application.Label) *tasksv1.Label {
	return &tasksv1.Label{Id: value.ID.String(), Name: value.Name, Color: value.Color}
}

func labelsToProto(values []application.Label) []*tasksv1.Label {
	result := make([]*tasksv1.Label, len(values))
	for index := range values {
		result[index] = labelToProto(values[index])
	}
	return result
}

func taskToProto(value application.Task) (*tasksv1.Task, error) {
	var description *structpb.Struct
	if len(value.Description) > 0 {
		converted, err := contentToStruct(value.Description)
		if err != nil {
			return nil, err
		}
		description = converted
	}
	var source *tasksv1.TaskSource
	if value.Source != nil {
		source = &tasksv1.TaskSource{
			Type: taskSourceTypeToProto(value.Source.Type),
			Title: value.Source.Title, Url: value.Source.URL,
			FunnelName: optionalString(value.Source.FunnelName),
			StageName:  optionalString(value.Source.StageName),
		}
	}
	var recurrence *tasksv1.RecurrenceRule
	if value.Recurrence != nil {
		recurrence = recurrenceToProto(*value.Recurrence)
	}
	return &tasksv1.Task{
		Id: value.ID.String(), BoardId: value.BoardID.String(),
		ColumnId: value.ColumnID.String(), Order: uint32(maxInt32(0, value.Order)),
		Title: value.Title, Description: description, AuthorId: value.AuthorID.String(),
		AssigneeIds: uuidStrings(value.AssigneeIDs),
		AssigneePositionId: optionalUUIDString(value.AssigneePositionID),
		WatcherIds: uuidStrings(value.WatcherIDs),
		DueDate: optionalTimestamp(value.DueDate),
		Priority: priorityToProto(value.Priority),
		LabelIds: uuidStrings(value.LabelIDs),
		Checklist: checklistToProto(value.Checklist),
		Attachments: attachmentsToProto(value.Attachments),
		Source: source, LinkedArticleIds: uuidStrings(value.LinkedArticleIDs),
		Recurrence: recurrence, CompletedAt: optionalTimestamp(value.CompletedAt),
		CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
		UpdatedAt: timestamppb.New(value.UpdatedAt.UTC()),
	}, nil
}

func tasksToProto(values []application.Task) ([]*tasksv1.Task, error) {
	result := make([]*tasksv1.Task, len(values))
	for index, value := range values {
		converted, err := taskToProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func commentToProto(value application.Comment) (*tasksv1.TaskComment, error) {
	content, err := contentToStruct(value.Content)
	if err != nil {
		return nil, err
	}
	return &tasksv1.TaskComment{
		Id: value.ID.String(), TaskId: value.TaskID.String(),
		AuthorId: value.AuthorID.String(), Content: content,
		CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
	}, nil
}

func commentsToProto(values []application.Comment) ([]*tasksv1.TaskComment, error) {
	result := make([]*tasksv1.TaskComment, len(values))
	for index, value := range values {
		converted, err := commentToProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func checklistToProto(values []application.ChecklistItem) []*tasksv1.ChecklistItem {
	result := make([]*tasksv1.ChecklistItem, len(values))
	for index, value := range values {
		result[index] = &tasksv1.ChecklistItem{
			Id: value.ID.String(), Text: value.Text, Done: value.Done,
		}
	}
	return result
}

func checklistFromProto(values []*tasksv1.ChecklistItem) ([]application.ChecklistItem, error) {
	result := make([]application.ChecklistItem, 0, len(values))
	for _, value := range values {
		id, err := parseUUID(value.GetId())
		if err != nil {
			return nil, err
		}
		result = append(result, application.ChecklistItem{
			ID: id, Text: value.GetText(), Done: value.GetDone(),
		})
	}
	return result, nil
}

func attachmentsToProto(values []application.Attachment) []*tasksv1.Attachment {
	result := make([]*tasksv1.Attachment, len(values))
	for index, value := range values {
		result[index] = &tasksv1.Attachment{
			Id: value.ID.String(), Name: value.Name, Url: value.URL,
			Size: uint64(maxInt64(0, value.Size)), MimeType: value.MimeType,
		}
	}
	return result
}

func attachmentsFromProto(values []*tasksv1.Attachment) ([]application.Attachment, error) {
	result := make([]application.Attachment, 0, len(values))
	for _, value := range values {
		id, err := parseUUID(value.GetId())
		if err != nil {
			return nil, err
		}
		result = append(result, application.Attachment{
			ID: id, Name: value.GetName(), URL: value.GetUrl(),
			Size: int64(value.GetSize()), MimeType: value.GetMimeType(),
		})
	}
	return result, nil
}

func recurrenceToProto(value application.RecurrenceRule) *tasksv1.RecurrenceRule {
	weekdays := make([]uint32, len(value.Weekdays))
	for index, weekday := range value.Weekdays {
		weekdays[index] = uint32(weekday)
	}
	return &tasksv1.RecurrenceRule{
		Frequency: recurrenceFrequencyToProto(value.Frequency),
		Interval:  uint32(maxInt(1, value.Interval)),
		Weekdays:  weekdays,
	}
}

func recurrenceFromProto(value *tasksv1.RecurrenceRule) (*application.RecurrenceRule, error) {
	if value == nil {
		return nil, nil
	}
	frequency, err := recurrenceFrequencyFromProto(value.GetFrequency())
	if err != nil {
		return nil, err
	}
	weekdays := make([]int, 0, len(value.GetWeekdays()))
	for _, weekday := range value.GetWeekdays() {
		weekdays = append(weekdays, int(weekday))
	}
	return &application.RecurrenceRule{
		Frequency: frequency, Interval: int(value.GetInterval()), Weekdays: weekdays,
	}, nil
}

func contentToStruct(raw json.RawMessage) (*structpb.Struct, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return structpb.NewStruct(payload)
}

func contentFromStruct(value *structpb.Struct) (json.RawMessage, error) {
	if value == nil {
		return nil, invalidArgument("Некорректное содержимое")
	}
	raw, err := value.MarshalJSON()
	if err != nil {
		return nil, invalidArgument("Некорректное содержимое")
	}
	return raw, nil
}

func boardTypeToProto(value string) tasksv1.BoardType {
	switch value {
	case "personal":
		return tasksv1.BoardType_BOARD_TYPE_PERSONAL
	case "department":
		return tasksv1.BoardType_BOARD_TYPE_DEPARTMENT
	case "project":
		return tasksv1.BoardType_BOARD_TYPE_PROJECT
	default:
		return tasksv1.BoardType_BOARD_TYPE_UNSPECIFIED
	}
}

func priorityToProto(value string) tasksv1.TaskPriority {
	switch value {
	case "low":
		return tasksv1.TaskPriority_TASK_PRIORITY_LOW
	case "high":
		return tasksv1.TaskPriority_TASK_PRIORITY_HIGH
	case "urgent":
		return tasksv1.TaskPriority_TASK_PRIORITY_URGENT
	default:
		return tasksv1.TaskPriority_TASK_PRIORITY_MEDIUM
	}
}

func priorityFromProto(value tasksv1.TaskPriority) (string, error) {
	switch value {
	case tasksv1.TaskPriority_TASK_PRIORITY_LOW:
		return "low", nil
	case tasksv1.TaskPriority_TASK_PRIORITY_MEDIUM:
		return "medium", nil
	case tasksv1.TaskPriority_TASK_PRIORITY_HIGH:
		return "high", nil
	case tasksv1.TaskPriority_TASK_PRIORITY_URGENT:
		return "urgent", nil
	default:
		return "", invalidArgument("Некорректный приоритет задачи")
	}
}

func taskSourceTypeToProto(value string) tasksv1.TaskSourceType {
	switch value {
	case "task":
		return tasksv1.TaskSourceType_TASK_SOURCE_TYPE_TASK
	case "contact":
		return tasksv1.TaskSourceType_TASK_SOURCE_TYPE_CONTACT
	case "company":
		return tasksv1.TaskSourceType_TASK_SOURCE_TYPE_COMPANY
	case "deal":
		return tasksv1.TaskSourceType_TASK_SOURCE_TYPE_DEAL
	default:
		return tasksv1.TaskSourceType_TASK_SOURCE_TYPE_UNSPECIFIED
	}
}

func recurrenceFrequencyToProto(value domainrecurrence.Frequency) tasksv1.RecurrenceFrequency {
	switch value {
	case domainrecurrence.FrequencyDaily:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY
	case domainrecurrence.FrequencyWeekly:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY
	case domainrecurrence.FrequencyMonthly:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY
	default:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_UNSPECIFIED
	}
}

func recurrenceFrequencyFromProto(value tasksv1.RecurrenceFrequency) (domainrecurrence.Frequency, error) {
	switch value {
	case tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY:
		return domainrecurrence.FrequencyDaily, nil
	case tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY:
		return domainrecurrence.FrequencyWeekly, nil
	case tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY:
		return domainrecurrence.FrequencyMonthly, nil
	default:
		return "", invalidArgument("Некорректная частота повторения")
	}
}

func parseUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalidArgument("Некорректный идентификатор")
	}
	return parsed, nil
}

func parseOptionalUUID(value *string) (*uuid.UUID, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed, err := parseUUID(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseUUIDStrings(values []string) ([]uuid.UUID, error) {
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

func optionalUUIDString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func optionalString(value *string) *string {
	if value == nil {
		return nil
	}
	return value
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func maxInt32(left, right int32) int32 {
	if left > right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func optionalTimestampPtr(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	result := value.AsTime().UTC()
	return &result
}