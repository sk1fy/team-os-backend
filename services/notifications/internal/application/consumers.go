package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

type payload struct {
	UserID                              string   `json:"userId"`
	MentionedUserID                     string   `json:"mentionedUserId"`
	AssigneeUserIDs                     []string `json:"assigneeUserIds"`
	RecipientUserIDs                    []string `json:"recipientUserIds"`
	Title, TaskTitle, CourseTitle, Link string
	AuthorID                            string `json:"authorId"`
	RequiresAcknowledgement             bool   `json:"requiresAcknowledgement"`
	User                                struct {
		UserID        string   `json:"userId"`
		Status        string   `json:"status"`
		PositionIDs   []string `json:"positionIds"`
		DepartmentIDs []string `json:"departmentIds"`
	} `json:"user"`
	Audience struct {
		Scope         string   `json:"scope"`
		UserIDs       []string `json:"userIds"`
		PositionIDs   []string `json:"positionIds"`
		DepartmentIDs []string `json:"departmentIds"`
	} `json:"audience"`
}

func (s *Service) Handle(subject string) eventbus.HandlerFunc {
	return func(ctx context.Context, event eventbus.Event) (bool, error) {
		var p payload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return false, fmt.Errorf("decode %s: %w", subject, err)
		}
		switch subject {
		case "teamos.org.user.created.v1", "teamos.org.user.updated.v1":
			return s.upsertUserProjection(ctx, event, p.User.UserID,
				p.User.Status == "ORG_USER_STATUS_ACTIVE", p.User.PositionIDs, p.User.DepartmentIDs,
				subject == "teamos.org.user.created.v1")
		case "teamos.org.user.deactivated.v1":
			return s.deactivateUserProjection(ctx, event, p.UserID)
		}
		typ, title, body := eventNotification(subject, p)
		recipients := append([]string{}, p.RecipientUserIDs...)
		if subject == "teamos.tasks.task.assigned.v1" {
			recipients = p.AssigneeUserIDs
		}
		if subject == "teamos.academy.course.assigned.v1" {
			if p.UserID != "" {
				recipients = []string{p.UserID}
			}
		}
		if subject == "teamos.kb.article.published.v1" {
			resolved, err := s.resolveArticleAudience(ctx, event.CompanyID,
				p.Audience.Scope, p.Audience.UserIDs, p.Audience.PositionIDs, p.Audience.DepartmentIDs)
			if err != nil {
				return false, err
			}
			recipients = uuidStrings(resolved)
		}
		if strings.Contains(subject, "mention") {
			recipients = []string{p.MentionedUserID}
		}
		if subject == "teamos.org.user.created.v1" {
			recipients = []string{p.User.UserID}
		}
		seen := map[uuid.UUID]struct{}{}
		ids := []uuid.UUID{}
		authorID := p.AuthorID
		if authorID == "" {
			authorID = event.ActorID
		}
		for _, raw := range recipients {
			id, err := uuid.Parse(raw)
			if err != nil {
				continue
			}
			if id.String() == authorID {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		return s.CreateMany(ctx, event, ids, typ, title, body, stringPtr(p.Link))
	}
}
func eventNotification(subject string, p payload) (string, string, *string) {
	title := p.Title
	if title == "" {
		title = p.TaskTitle
	}
	if title == "" {
		title = p.CourseTitle
	}
	switch subject {
	case "teamos.tasks.task.assigned.v1":
		return "task_assigned", "Вам назначена задача: " + title, nil
	case "teamos.tasks.comment.added.v1":
		return "task_comment", "Новый комментарий к задаче: " + title, nil
	case "teamos.tasks.task.due_soon.v1":
		return "task_due", "Скоро срок задачи: " + title, nil
	case "teamos.kb.article.published.v1":
		return "article_published", "Опубликована статья: " + title, nil
	case "teamos.academy.course.assigned.v1":
		return "course_assigned", "Вам назначен курс: " + title, nil
	case "teamos.academy.course.due_soon.v1":
		return "course_due", "Скоро срок курса: " + title, nil
	case "teamos.tasks.mention.created.v1", "teamos.kb.mention.created.v1":
		return "mention", "Вас упомянули: " + title, nil
	case "teamos.org.user.created.v1":
		return "article_published", "Добро пожаловать в TeamOS", nil
	default:
		return "article_published", title, nil
	}
}
func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
