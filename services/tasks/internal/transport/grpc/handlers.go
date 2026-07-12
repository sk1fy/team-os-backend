package grpc

import (
	"context"

	"github.com/google/uuid"
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
)

func (s *Server) GetBoards(ctx context.Context, _ *tasksv1.GetBoardsRequest) (*tasksv1.GetBoardsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	boards, err := s.application.GetBoards(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.GetBoardsResponse{Boards: boardsToProto(boards)}, nil
}

func (s *Server) GetColumns(ctx context.Context, request *tasksv1.GetColumnsRequest) (*tasksv1.GetColumnsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	boardID, err := parseUUID(request.GetBoardId())
	if err != nil {
		return nil, err
	}
	columns, err := s.application.GetColumns(ctx, actor, boardID)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.GetColumnsResponse{Columns: columnsToProto(columns)}, nil
}

func (s *Server) CreateColumn(ctx context.Context, request *tasksv1.CreateColumnRequest) (*tasksv1.CreateColumnResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	boardID, err := parseUUID(request.GetBoardId())
	if err != nil {
		return nil, err
	}
	column, err := s.application.CreateColumn(ctx, actor, application.CreateColumnInput{
		BoardID: boardID, Name: request.GetName(), Color: optionalString(request.Color),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.CreateColumnResponse{Column: columnToProto(column)}, nil
}

func (s *Server) UpdateColumn(ctx context.Context, request *tasksv1.UpdateColumnRequest) (*tasksv1.UpdateColumnResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	input := application.UpdateColumnInput{ID: id, Name: request.Name, Color: request.Color}
	column, err := s.application.UpdateColumn(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.UpdateColumnResponse{Column: columnToProto(column)}, nil
}

func (s *Server) GetTasks(ctx context.Context, request *tasksv1.GetTasksRequest) (*tasksv1.GetTasksResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	var boardID *uuid.UUID
	if request.BoardId != nil {
		parsed, parseErr := parseUUID(request.GetBoardId())
		if parseErr != nil {
			return nil, parseErr
		}
		boardID = &parsed
	}
	tasks, err := s.application.GetTasks(ctx, actor, boardID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := tasksToProto(tasks)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.GetTasksResponse{Tasks: converted}, nil
}

func (s *Server) GetTask(ctx context.Context, request *tasksv1.GetTaskRequest) (*tasksv1.GetTaskResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	task, err := s.application.GetTask(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := taskToProto(task)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.GetTaskResponse{Task: converted}, nil
}

func (s *Server) CreateTask(ctx context.Context, request *tasksv1.CreateTaskRequest) (*tasksv1.CreateTaskResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	boardID, err := parseUUID(request.GetBoardId())
	if err != nil {
		return nil, err
	}
	columnID, err := parseUUID(request.GetColumnId())
	if err != nil {
		return nil, err
	}
	input := application.CreateTaskInput{
		BoardID: boardID, ColumnID: columnID, Title: request.GetTitle(),
	}
	if request.Priority != nil {
		priority, priorityErr := priorityFromProto(request.GetPriority())
		if priorityErr != nil {
			return nil, priorityErr
		}
		input.Priority = priority
	}
	task, err := s.application.CreateTask(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := taskToProto(task)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.CreateTaskResponse{Task: converted}, nil
}

func (s *Server) UpdateTask(ctx context.Context, request *tasksv1.UpdateTaskRequest) (*tasksv1.UpdateTaskResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	input := application.UpdateTaskInput{ID: id}
	if request.Title != nil {
		input.Title = request.Title
	}
	if request.Description != nil {
		content, mapErr := contentFromStruct(request.Description)
		if mapErr != nil {
			return nil, mapErr
		}
		input.Description = content
	}
	if request.GetAssigneeIdsSet() {
		assigneeIDs, parseErr := parseUUIDStrings(request.GetAssigneeIds())
		if parseErr != nil {
			return nil, invalidArgument("Некорректные assigneeIds")
		}
		input.AssigneeIDs = assigneeIDs
		input.AssigneeIDsSet = true
	}
	if request.AssigneePositionId != nil {
		positionID, parseErr := parseOptionalUUID(request.AssigneePositionId)
		if parseErr != nil {
			return nil, parseErr
		}
		input.AssigneePositionID = positionID
	}
	if request.GetClearAssigneePositionId() {
		input.ClearAssigneePositionID = true
	}
	if request.GetWatcherIdsSet() {
		watcherIDs, parseErr := parseUUIDStrings(request.GetWatcherIds())
		if parseErr != nil {
			return nil, invalidArgument("Некорректные watcherIds")
		}
		input.WatcherIDs = watcherIDs
		input.WatcherIDsSet = true
	}
	if request.DueDate != nil {
		input.DueDate = optionalTimestampPtr(request.DueDate)
	}
	if request.GetClearDueDate() {
		input.ClearDueDate = true
	}
	if request.Priority != nil {
		priority, priorityErr := priorityFromProto(request.GetPriority())
		if priorityErr != nil {
			return nil, priorityErr
		}
		input.Priority = &priority
	}
	if request.GetLabelIdsSet() {
		labelIDs, parseErr := parseUUIDStrings(request.GetLabelIds())
		if parseErr != nil {
			return nil, invalidArgument("Некорректные labelIds")
		}
		input.LabelIDs = labelIDs
		input.LabelIDsSet = true
	}
	if request.GetChecklistSet() {
		checklist, mapErr := checklistFromProto(request.GetChecklist())
		if mapErr != nil {
			return nil, mapErr
		}
		input.Checklist = checklist
		input.ChecklistSet = true
	}
	if request.GetAttachmentsSet() {
		attachments, mapErr := attachmentsFromProto(request.GetAttachments())
		if mapErr != nil {
			return nil, mapErr
		}
		input.Attachments = attachments
		input.AttachmentsSet = true
	}
	if request.GetLinkedArticleIdsSet() {
		linkedArticleIDs, parseErr := parseUUIDStrings(request.GetLinkedArticleIds())
		if parseErr != nil {
			return nil, invalidArgument("Некорректные linkedArticleIds")
		}
		input.LinkedArticleIDs = linkedArticleIDs
		input.LinkedArticleIDsSet = true
	}
	if request.Recurrence != nil {
		recurrence, mapErr := recurrenceFromProto(request.Recurrence)
		if mapErr != nil {
			return nil, mapErr
		}
		input.Recurrence = recurrence
	}
	if request.GetClearRecurrence() {
		input.ClearRecurrence = true
	}
	if request.CompletedAt != nil {
		input.CompletedAt = optionalTimestampPtr(request.CompletedAt)
	}
	if request.GetClearCompletedAt() {
		input.ClearCompletedAt = true
	}
	task, err := s.application.UpdateTask(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := taskToProto(task)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.UpdateTaskResponse{Task: converted}, nil
}

func (s *Server) MoveTask(ctx context.Context, request *tasksv1.MoveTaskRequest) (*tasksv1.MoveTaskResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUUID(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	columnID, err := parseUUID(request.GetColumnId())
	if err != nil {
		return nil, err
	}
	task, err := s.application.MoveTask(ctx, actor, application.MoveTaskInput{
		TaskID: taskID, ColumnID: columnID, Order: int32(request.GetOrder()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := taskToProto(task)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.MoveTaskResponse{Task: converted}, nil
}

func (s *Server) GetComments(ctx context.Context, request *tasksv1.GetCommentsRequest) (*tasksv1.GetCommentsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUUID(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	comments, err := s.application.GetComments(ctx, actor, taskID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := commentsToProto(comments)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.GetCommentsResponse{Comments: converted}, nil
}

func (s *Server) AddComment(ctx context.Context, request *tasksv1.AddCommentRequest) (*tasksv1.AddCommentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUUID(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	content, err := contentFromStruct(request.GetContent())
	if err != nil {
		return nil, err
	}
	comment, err := s.application.AddComment(ctx, actor, application.AddCommentInput{
		TaskID: taskID, Content: content,
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := commentToProto(comment)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.AddCommentResponse{Comment: converted}, nil
}

func (s *Server) GetLabels(ctx context.Context, _ *tasksv1.GetLabelsRequest) (*tasksv1.GetLabelsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	labels, err := s.application.GetLabels(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &tasksv1.GetLabelsResponse{Labels: labelsToProto(labels)}, nil
}
