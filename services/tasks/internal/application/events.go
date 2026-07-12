package application

import (
	"context"

	"github.com/google/uuid"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const tasksLink = "/tasks"

func assigneeRecipients(assigneeIDs []uuid.UUID, exclude ...uuid.UUID) []string {
	excluded := make(map[uuid.UUID]struct{}, len(exclude))
	for _, id := range exclude {
		excluded[id] = struct{}{}
	}
	seen := make(map[uuid.UUID]struct{}, len(assigneeIDs))
	result := make([]string, 0, len(assigneeIDs))
	for _, assigneeID := range assigneeIDs {
		if _, skip := excluded[assigneeID]; skip {
			continue
		}
		if _, ok := seen[assigneeID]; ok {
			continue
		}
		seen[assigneeID] = struct{}{}
		result = append(result, assigneeID.String())
	}
	return result
}

func commentRecipients(task Task, commentAuthorID uuid.UUID) []string {
	seen := make(map[uuid.UUID]struct{})
	result := make([]string, 0)
	add := func(id uuid.UUID) {
		if id == commentAuthorID {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		result = append(result, id.String())
	}
	add(task.AuthorID)
	for _, assigneeID := range task.AssigneeIDs {
		add(assigneeID)
	}
	for _, watcherID := range task.WatcherIDs {
		add(watcherID)
	}
	return result
}

func dueSoonRecipients(task Task) []string {
	seen := make(map[uuid.UUID]struct{})
	result := make([]string, 0)
	add := func(id uuid.UUID) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		result = append(result, id.String())
	}
	for _, assigneeID := range task.AssigneeIDs {
		add(assigneeID)
	}
	for _, watcherID := range task.WatcherIDs {
		add(watcherID)
	}
	return result
}

func (s *Service) emitTaskAssigned(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
) error {
	return s.emitTaskAssignedTo(ctx, queries, actor, task, task.AssigneeIDs, task.AssigneePositionID)
}

func (s *Service) emitTaskAssignedTo(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
	assigneeIDs []uuid.UUID,
	assigneePositionID *uuid.UUID,
) error {
	recipients := assigneeRecipients(assigneeIDs, task.AuthorID)
	if len(recipients) == 0 && assigneePositionID == nil {
		return nil
	}
	payload := &eventsv1.TasksTaskAssignedPayload{
		TaskId: task.ID.String(), Title: task.Title, AuthorId: task.AuthorID.String(),
		AssigneeUserIds: recipients, Link: tasksLink,
	}
	if assigneePositionID != nil {
		value := assigneePositionID.String()
		payload.AssigneePositionId = &value
	}
	return s.emit(ctx, queries, actor.CompanyID, task.ID, actor.UserID, "teamos.tasks.task.assigned.v1", payload)
}

func (s *Service) emitCommentAdded(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
	comment Comment,
) error {
	return s.emit(ctx, queries, actor.CompanyID, task.ID, actor.UserID, "teamos.tasks.comment.added.v1", &eventsv1.TasksCommentAddedPayload{
		CommentId: comment.ID.String(), TaskId: task.ID.String(), TaskTitle: task.Title,
		AuthorId: comment.AuthorID.String(), RecipientUserIds: commentRecipients(task, comment.AuthorID),
		Link: tasksLink,
	})
}

func (s *Service) emitMentions(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
	comment Comment,
	content []byte,
) error {
	return s.emitMentionsForEntity(ctx, queries, actor, task, "comment", comment.ID, content)
}

func (s *Service) emitTaskMentions(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
	content []byte,
) error {
	return s.emitMentionsForEntity(ctx, queries, actor, task, "task", task.ID, content)
}

func (s *Service) emitMentionsForEntity(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
	entity string,
	entityID uuid.UUID,
	content []byte,
) error {
	for _, rawUserID := range richtext.Mentions(content) {
		mentionedUserID, err := uuid.Parse(rawUserID)
		if err != nil || mentionedUserID == actor.UserID {
			continue
		}
		if err = s.emit(ctx, queries, actor.CompanyID, task.ID, actor.UserID, "teamos.tasks.mention.created.v1", &eventsv1.MentionCreatedPayload{
			SourceService: eventsv1.MentionSourceService_MENTION_SOURCE_SERVICE_TASKS,
			SourceEntity:  entity, SourceEntityId: entityID.String(),
			MentionedUserId: mentionedUserID.String(), AuthorId: actor.UserID.String(),
			Title: task.Title, Link: tasksLink,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) emitTaskDueSoon(
	ctx context.Context,
	queries *db.Queries,
	task Task,
) error {
	if task.DueDate == nil {
		return nil
	}
	recipients := dueSoonRecipients(task)
	if len(recipients) == 0 {
		return nil
	}
	return s.emit(ctx, queries, task.CompanyID, task.ID, task.AuthorID, "teamos.tasks.task.due_soon.v1", &eventsv1.TasksTaskDueSoonPayload{
		TaskId: task.ID.String(), Title: task.Title,
		DueDate: timestamppb.New(task.DueDate.UTC()), RecipientUserIds: recipients,
		Link: tasksLink,
	})
}

func addedUUIDs(before, after []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(before))
	for _, id := range before {
		seen[id] = struct{}{}
	}
	result := make([]uuid.UUID, 0, len(after))
	for _, id := range after {
		if _, exists := seen[id]; !exists {
			result = append(result, id)
		}
	}
	return result
}
