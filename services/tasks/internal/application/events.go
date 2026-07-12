package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

const tasksLink = "/tasks"

func assigneeRecipients(task Task, exclude ...uuid.UUID) []string {
	excluded := make(map[uuid.UUID]struct{}, len(exclude))
	for _, id := range exclude {
		excluded[id] = struct{}{}
	}
	seen := make(map[uuid.UUID]struct{}, len(task.AssigneeIDs))
	result := make([]string, 0, len(task.AssigneeIDs))
	for _, assigneeID := range task.AssigneeIDs {
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
	recipients := assigneeRecipients(task, task.AuthorID)
	if len(recipients) == 0 && task.AssigneePositionID == nil {
		return nil
	}
	payload := map[string]any{
		"taskId": task.ID.String(),
		"title":  task.Title,
		"authorId": task.AuthorID.String(),
		"assigneeUserIds": recipients,
		"link": tasksLink,
	}
	if task.AssigneePositionID != nil {
		payload["assigneePositionId"] = task.AssigneePositionID.String()
	}
	return s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.tasks.task.assigned.v1", payload)
}

func (s *Service) emitCommentAdded(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	task Task,
	comment Comment,
) error {
	return s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.tasks.comment.added.v1", map[string]any{
		"commentId": comment.ID.String(),
		"taskId": task.ID.String(),
		"taskTitle": task.Title,
		"authorId": comment.AuthorID.String(),
		"recipientUserIds": commentRecipients(task, comment.AuthorID),
		"link": tasksLink,
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
	for _, mentionedUserID := range richtext.Mentions(content) {
		mentionedUserID = strings.TrimSpace(mentionedUserID)
		if mentionedUserID == "" {
			continue
		}
		if err := s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.tasks.mention.created.v1", map[string]any{
			"sourceService": "MENTION_SOURCE_SERVICE_TASKS",
			"sourceEntity": "comment",
			"sourceEntityId": comment.ID.String(),
			"mentionedUserId": mentionedUserID,
			"authorId": actor.UserID.String(),
			"title": task.Title,
			"link": tasksLink,
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
	return s.emit(ctx, queries, task.CompanyID, task.AuthorID, "teamos.tasks.task.due_soon.v1", map[string]any{
		"taskId": task.ID.String(),
		"title": task.Title,
		"dueDate": task.DueDate.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"recipientUserIds": recipients,
		"link": tasksLink,
	})
}

func assigneesChanged(before, after []uuid.UUID) bool {
	if len(before) != len(after) {
		return true
	}
	seen := make(map[uuid.UUID]struct{}, len(before))
	for _, id := range before {
		seen[id] = struct{}{}
	}
	for _, id := range after {
		if _, ok := seen[id]; !ok {
			return true
		}
	}
	return false
}