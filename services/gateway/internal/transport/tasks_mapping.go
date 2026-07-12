package transport

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func boardFromProto(value *tasksv1.Board) (api.Board, error) {
	if value == nil {
		return api.Board{}, errors.New("tasks returned an empty board")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Board{}, err
	}
	boardType, err := boardTypeFromProto(value.GetType())
	if err != nil {
		return api.Board{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	result := api.Board{
		Id: id, Name: value.GetName(), Type: boardType, CreatedAt: createdAt,
	}
	if value.DepartmentId != nil {
		departmentID, parseErr := uuid.Parse(value.GetDepartmentId())
		if parseErr != nil {
			return api.Board{}, parseErr
		}
		result.DepartmentId = &departmentID
	}
	if value.OwnerId != nil {
		ownerID, parseErr := uuid.Parse(value.GetOwnerId())
		if parseErr != nil {
			return api.Board{}, parseErr
		}
		result.OwnerId = &ownerID
	}
	return result, nil
}

func boardsFromProto(values []*tasksv1.Board) ([]api.Board, error) {
	result := make([]api.Board, len(values))
	for index, value := range values {
		converted, err := boardFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func taskColumnFromProto(value *tasksv1.TaskColumn) (api.TaskColumn, error) {
	if value == nil {
		return api.TaskColumn{}, errors.New("tasks returned an empty column")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.TaskColumn{}, err
	}
	boardID, err := uuid.Parse(value.GetBoardId())
	if err != nil {
		return api.TaskColumn{}, err
	}
	return api.TaskColumn{
		Id: id, BoardId: boardID, Name: value.GetName(),
		Order: int(value.GetOrder()), Color: value.Color,
	}, nil
}

func taskColumnsFromProto(values []*tasksv1.TaskColumn) ([]api.TaskColumn, error) {
	result := make([]api.TaskColumn, len(values))
	for index, value := range values {
		converted, err := taskColumnFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func labelFromProto(value *tasksv1.Label) (api.Label, error) {
	if value == nil {
		return api.Label{}, errors.New("tasks returned an empty label")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Label{}, err
	}
	return api.Label{Id: id, Name: value.GetName(), Color: value.GetColor()}, nil
}

func labelsFromProto(values []*tasksv1.Label) ([]api.Label, error) {
	result := make([]api.Label, len(values))
	for index, value := range values {
		converted, err := labelFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func taskFromProto(value *tasksv1.Task) (api.Task, error) {
	if value == nil {
		return api.Task{}, errors.New("tasks returned an empty task")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Task{}, err
	}
	boardID, err := uuid.Parse(value.GetBoardId())
	if err != nil {
		return api.Task{}, err
	}
	columnID, err := uuid.Parse(value.GetColumnId())
	if err != nil {
		return api.Task{}, err
	}
	authorID, err := uuid.Parse(value.GetAuthorId())
	if err != nil {
		return api.Task{}, err
	}
	assigneeIDs, err := UUIDsFromStrings(value.GetAssigneeIds())
	if err != nil {
		return api.Task{}, err
	}
	watcherIDs, err := UUIDsFromStrings(value.GetWatcherIds())
	if err != nil {
		return api.Task{}, err
	}
	labelIDs, err := UUIDsFromStrings(value.GetLabelIds())
	if err != nil {
		return api.Task{}, err
	}
	linkedArticleIDs, err := UUIDsFromStrings(value.GetLinkedArticleIds())
	if err != nil {
		return api.Task{}, err
	}
	priority, err := taskPriorityFromProto(value.GetPriority())
	if err != nil {
		return api.Task{}, err
	}
	checklist, err := checklistFromProto(value.GetChecklist())
	if err != nil {
		return api.Task{}, err
	}
	attachments, err := attachmentsFromProto(value.GetAttachments())
	if err != nil {
		return api.Task{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	updatedAt := time.Time{}
	if value.GetUpdatedAt() != nil {
		updatedAt = value.GetUpdatedAt().AsTime()
	}
	result := api.Task{
		Id: id, BoardId: boardID, ColumnId: columnID, Order: int(value.GetOrder()),
		Title: value.GetTitle(), AuthorId: authorID, AssigneeIds: assigneeIDs,
		WatcherIds: watcherIDs, Priority: priority, LabelIds: labelIDs,
		Checklist: checklist, Attachments: attachments, LinkedArticleIds: linkedArticleIDs,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if value.Description != nil {
		description, convertErr := richTextFromStruct(value.GetDescription())
		if convertErr != nil {
			return api.Task{}, convertErr
		}
		result.Description = &description
	}
	if value.AssigneePositionId != nil {
		positionID, parseErr := uuid.Parse(value.GetAssigneePositionId())
		if parseErr != nil {
			return api.Task{}, parseErr
		}
		result.AssigneePositionId = &positionID
	}
	if value.GetDueDate() != nil {
		dueDate := value.GetDueDate().AsTime()
		result.DueDate = &dueDate
	}
	if value.Source != nil {
		source, convertErr := taskSourceFromProto(value.GetSource())
		if convertErr != nil {
			return api.Task{}, convertErr
		}
		result.Source = &source
	}
	if value.Recurrence != nil {
		recurrence, convertErr := recurrenceFromProto(value.GetRecurrence())
		if convertErr != nil {
			return api.Task{}, convertErr
		}
		result.Recurrence = &recurrence
	}
	if value.GetCompletedAt() != nil {
		completedAt := value.GetCompletedAt().AsTime()
		result.CompletedAt = &completedAt
	}
	return result, nil
}

func tasksFromProto(values []*tasksv1.Task) ([]api.Task, error) {
	result := make([]api.Task, len(values))
	for index, value := range values {
		converted, err := taskFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func taskCommentFromProto(value *tasksv1.TaskComment) (api.TaskComment, error) {
	if value == nil {
		return api.TaskComment{}, errors.New("tasks returned an empty comment")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.TaskComment{}, err
	}
	taskID, err := uuid.Parse(value.GetTaskId())
	if err != nil {
		return api.TaskComment{}, err
	}
	authorID, err := uuid.Parse(value.GetAuthorId())
	if err != nil {
		return api.TaskComment{}, err
	}
	content, err := richTextFromStruct(value.GetContent())
	if err != nil {
		return api.TaskComment{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	return api.TaskComment{
		Id: id, TaskId: taskID, AuthorId: authorID, Content: content, CreatedAt: createdAt,
	}, nil
}

func taskCommentsFromProto(values []*tasksv1.TaskComment) ([]api.TaskComment, error) {
	result := make([]api.TaskComment, len(values))
	for index, value := range values {
		converted, err := taskCommentFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func updateTaskToProto(id string, input api.UpdateTaskInput) (*tasksv1.UpdateTaskRequest, error) {
	request := &tasksv1.UpdateTaskRequest{Id: id}
	if input.Title != nil {
		request.Title = input.Title
	}
	if input.Description != nil {
		description, err := richTextToStruct(*input.Description)
		if err != nil {
			return nil, err
		}
		request.Description = description
	}
	if input.AssigneeIds != nil {
		set := true
		request.AssigneeIdsSet = &set
		request.AssigneeIds = idStrings(*input.AssigneeIds)
	}
	if input.AssigneePositionId != nil {
		positionID := input.AssigneePositionId.String()
		request.AssigneePositionId = &positionID
	}
	if input.WatcherIds != nil {
		set := true
		request.WatcherIdsSet = &set
		request.WatcherIds = idStrings(*input.WatcherIds)
	}
	if err := applyClearableDateTimeToUpdateTask(request, input.DueDate, input.CompletedAt); err != nil {
		return nil, err
	}
	if input.Priority != nil {
		priority, convertErr := taskPriorityToProto(*input.Priority)
		if convertErr != nil {
			return nil, convertErr
		}
		request.Priority = &priority
	}
	if input.LabelIds != nil {
		set := true
		request.LabelIdsSet = &set
		request.LabelIds = idStrings(*input.LabelIds)
	}
	if input.Checklist != nil {
		checklist, convertErr := checklistToProto(*input.Checklist)
		if convertErr != nil {
			return nil, convertErr
		}
		set := true
		request.ChecklistSet = &set
		request.Checklist = checklist
	}
	if input.Attachments != nil {
		attachments, convertErr := attachmentsToProto(*input.Attachments)
		if convertErr != nil {
			return nil, convertErr
		}
		set := true
		request.AttachmentsSet = &set
		request.Attachments = attachments
	}
	if input.LinkedArticleIds != nil {
		set := true
		request.LinkedArticleIdsSet = &set
		request.LinkedArticleIds = idStrings(*input.LinkedArticleIds)
	}
	if input.Recurrence != nil {
		recurrence, convertErr := recurrenceToProto(*input.Recurrence)
		if convertErr != nil {
			return nil, convertErr
		}
		request.Recurrence = recurrence
	}
	return request, nil
}

func checklistFromProto(values []*tasksv1.ChecklistItem) ([]api.ChecklistItem, error) {
	result := make([]api.ChecklistItem, len(values))
	for index, value := range values {
		if value == nil {
			return nil, errors.New("tasks returned an empty checklist item")
		}
		id, err := uuid.Parse(value.GetId())
		if err != nil {
			return nil, err
		}
		result[index] = api.ChecklistItem{Id: id, Text: value.GetText(), Done: value.GetDone()}
	}
	return result, nil
}

func checklistToProto(values []api.ChecklistItem) ([]*tasksv1.ChecklistItem, error) {
	result := make([]*tasksv1.ChecklistItem, len(values))
	for index, value := range values {
		result[index] = &tasksv1.ChecklistItem{
			Id: value.Id.String(), Text: value.Text, Done: value.Done,
		}
	}
	return result, nil
}

func attachmentsFromProto(values []*tasksv1.Attachment) ([]api.Attachment, error) {
	result := make([]api.Attachment, len(values))
	for index, value := range values {
		if value == nil {
			return nil, errors.New("tasks returned an empty attachment")
		}
		id, err := uuid.Parse(value.GetId())
		if err != nil {
			return nil, err
		}
		result[index] = api.Attachment{
			Id: id, Name: value.GetName(), Url: value.GetUrl(),
			Size: int(value.GetSize()), MimeType: value.GetMimeType(),
		}
	}
	return result, nil
}

func attachmentsToProto(values []api.Attachment) ([]*tasksv1.Attachment, error) {
	result := make([]*tasksv1.Attachment, len(values))
	for index, value := range values {
		if value.Size < 0 {
			return nil, fmt.Errorf("attachment size must be non-negative")
		}
		result[index] = &tasksv1.Attachment{
			Id: value.Id.String(), Name: value.Name, Url: value.Url,
			Size: uint64(value.Size), MimeType: value.MimeType,
		}
	}
	return result, nil
}

func taskSourceFromProto(value *tasksv1.TaskSource) (api.TaskSource, error) {
	if value == nil {
		return api.TaskSource{}, errors.New("tasks returned an empty task source")
	}
	sourceType, err := taskSourceTypeFromProto(value.GetType())
	if err != nil {
		return api.TaskSource{}, err
	}
	return api.TaskSource{
		Type: sourceType, Title: value.GetTitle(), Url: value.GetUrl(),
		FunnelName: value.FunnelName, StageName: value.StageName,
	}, nil
}

func recurrenceFromProto(value *tasksv1.RecurrenceRule) (api.RecurrenceRule, error) {
	if value == nil {
		return api.RecurrenceRule{}, errors.New("tasks returned an empty recurrence rule")
	}
	frequency, err := recurrenceFrequencyFromProto(value.GetFrequency())
	if err != nil {
		return api.RecurrenceRule{}, err
	}
	result := api.RecurrenceRule{
		Frequency: frequency, Interval: int(value.GetInterval()),
	}
	if weekdays := value.GetWeekdays(); len(weekdays) > 0 {
		converted := make([]int, len(weekdays))
		for index, weekday := range weekdays {
			converted[index] = int(weekday)
		}
		result.Weekdays = &converted
	}
	return result, nil
}

func recurrenceToProto(value api.RecurrenceRule) (*tasksv1.RecurrenceRule, error) {
	frequency, err := recurrenceFrequencyToProto(value.Frequency)
	if err != nil {
		return nil, err
	}
	if value.Interval < 1 {
		return nil, fmt.Errorf("recurrence interval must be at least 1")
	}
	result := &tasksv1.RecurrenceRule{
		Frequency: frequency, Interval: uint32(value.Interval),
	}
	if value.Weekdays != nil {
		weekdays := make([]uint32, len(*value.Weekdays))
		for index, weekday := range *value.Weekdays {
			if weekday < 0 || weekday > 6 {
				return nil, fmt.Errorf("recurrence weekday out of range: %d", weekday)
			}
			weekdays[index] = uint32(weekday)
		}
		result.Weekdays = weekdays
	}
	return result, nil
}

func applyClearableDateTimeToUpdateTask(
	request *tasksv1.UpdateTaskRequest,
	dueDate *api.ClearableDateTime,
	completedAt *api.ClearableDateTime,
) error {
	if dueDate != nil {
		if datetime, err := dueDate.AsISODateTime(); err == nil {
			request.DueDate = timestamppb.New(datetime.UTC())
		} else {
			text, err := dueDate.AsClearableDateTime1()
			if err != nil {
				return err
			}
			if text == "" {
				clear := true
				request.ClearDueDate = &clear
			}
		}
	}
	if completedAt != nil {
		if datetime, err := completedAt.AsISODateTime(); err == nil {
			request.CompletedAt = timestamppb.New(datetime.UTC())
		} else {
			text, err := completedAt.AsClearableDateTime1()
			if err != nil {
				return err
			}
			if text == "" {
				clear := true
				request.ClearCompletedAt = &clear
			}
		}
	}
	return nil
}

func boardTypeFromProto(value tasksv1.BoardType) (api.BoardType, error) {
	switch value {
	case tasksv1.BoardType_BOARD_TYPE_PERSONAL:
		return api.BoardTypePersonal, nil
	case tasksv1.BoardType_BOARD_TYPE_DEPARTMENT:
		return api.BoardTypeDepartment, nil
	case tasksv1.BoardType_BOARD_TYPE_PROJECT:
		return api.BoardTypeProject, nil
	default:
		return "", fmt.Errorf("unsupported board type: %v", value)
	}
}

func taskPriorityFromProto(value tasksv1.TaskPriority) (api.TaskPriority, error) {
	switch value {
	case tasksv1.TaskPriority_TASK_PRIORITY_LOW:
		return api.Low, nil
	case tasksv1.TaskPriority_TASK_PRIORITY_MEDIUM:
		return api.Medium, nil
	case tasksv1.TaskPriority_TASK_PRIORITY_HIGH:
		return api.High, nil
	case tasksv1.TaskPriority_TASK_PRIORITY_URGENT:
		return api.Urgent, nil
	case tasksv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED:
		return api.Medium, nil
	default:
		return "", fmt.Errorf("unsupported task priority: %v", value)
	}
}

func taskPriorityToProto(value api.TaskPriority) (tasksv1.TaskPriority, error) {
	switch value {
	case api.Low:
		return tasksv1.TaskPriority_TASK_PRIORITY_LOW, nil
	case api.Medium:
		return tasksv1.TaskPriority_TASK_PRIORITY_MEDIUM, nil
	case api.High:
		return tasksv1.TaskPriority_TASK_PRIORITY_HIGH, nil
	case api.Urgent:
		return tasksv1.TaskPriority_TASK_PRIORITY_URGENT, nil
	default:
		return tasksv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED, fmt.Errorf("unsupported task priority: %q", value)
	}
}

func taskSourceTypeFromProto(value tasksv1.TaskSourceType) (api.TaskSourceType, error) {
	switch value {
	case tasksv1.TaskSourceType_TASK_SOURCE_TYPE_TASK:
		return api.TaskSourceTypeTask, nil
	case tasksv1.TaskSourceType_TASK_SOURCE_TYPE_CONTACT:
		return api.TaskSourceTypeContact, nil
	case tasksv1.TaskSourceType_TASK_SOURCE_TYPE_COMPANY:
		return api.TaskSourceTypeCompany, nil
	case tasksv1.TaskSourceType_TASK_SOURCE_TYPE_DEAL:
		return api.TaskSourceTypeDeal, nil
	default:
		return "", fmt.Errorf("unsupported task source type: %v", value)
	}
}

func recurrenceFrequencyFromProto(value tasksv1.RecurrenceFrequency) (api.RecurrenceFrequency, error) {
	switch value {
	case tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY:
		return api.Daily, nil
	case tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY:
		return api.Weekly, nil
	case tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY:
		return api.Monthly, nil
	default:
		return "", fmt.Errorf("unsupported recurrence frequency: %v", value)
	}
}

func recurrenceFrequencyToProto(value api.RecurrenceFrequency) (tasksv1.RecurrenceFrequency, error) {
	switch value {
	case api.Daily:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, nil
	case api.Weekly:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY, nil
	case api.Monthly:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY, nil
	default:
		return tasksv1.RecurrenceFrequency_RECURRENCE_FREQUENCY_UNSPECIFIED, fmt.Errorf("unsupported recurrence frequency: %q", value)
	}
}
