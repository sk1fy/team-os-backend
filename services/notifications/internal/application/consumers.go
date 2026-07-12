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
	UserID                              string `json:"userId"`
	MentionedUserID                     string `json:"mentionedUserId"`
	AssigneeUserIDs, RecipientUserIDs   []string
	Title, TaskTitle, CourseTitle, Link string
	AuthorID                            string `json:"authorId"`
	RequiresAcknowledgement             bool   `json:"requiresAcknowledgement"`
	User                                struct {
		UserID string `json:"userId"`
	} `json:"user"`
	Audience struct {
		UserIDs []string `json:"userIds"`
	} `json:"audience"`
}

func (s *Service) Handle(subject string) eventbus.HandlerFunc {
	return func(ctx context.Context, event eventbus.Event) (bool, error) {
		var p payload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return false, fmt.Errorf("decode %s: %w", subject, err)
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
			recipients = p.Audience.UserIDs
		}
		if strings.Contains(subject, "mention") {
			recipients = []string{p.MentionedUserID}
		}
		if subject == "teamos.org.user.created.v1" {
			recipients = []string{p.User.UserID}
		}
		seen := map[uuid.UUID]struct{}{}
		ids := []uuid.UUID{}
		for _, raw := range recipients {
			id, err := uuid.Parse(raw)
			if err != nil {
				continue
			}
			if id.String() == p.AuthorID {
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
