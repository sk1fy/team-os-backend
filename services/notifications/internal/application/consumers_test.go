package application

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestEventNotificationUsesRussianTextAndTypes(t *testing.T) {
	tests := []struct{ subject, wantType, wantTitle string }{
		{"teamos.tasks.task.assigned.v1", "task_assigned", "Вам назначена задача: Отчёт"},
		{"teamos.tasks.comment.added.v1", "task_comment", "Новый комментарий к задаче: Отчёт"},
		{"teamos.academy.course.due_soon.v1", "course_due", "Скоро срок курса: Онбординг"},
		{"teamos.academy.course.version.published.v1", "course_published", "Партнёр опубликовал курс: Онбординг"},
		{"teamos.academy.course.distribution.changed.v1", "course_restriction", "Изменены ограничения курса: Онбординг"},
		{"teamos.kb.mention.created.v1", "mention", "Вас упомянули: Правила"},
	}
	for _, test := range tests {
		t.Run(test.subject, func(t *testing.T) {
			p := payload{}
			switch test.subject {
			case "teamos.tasks.task.assigned.v1", "teamos.tasks.comment.added.v1":
				p.Title = "Отчёт"
			case "teamos.academy.course.due_soon.v1", "teamos.academy.course.version.published.v1", "teamos.academy.course.distribution.changed.v1":
				p.CourseTitle = "Онбординг"
			default:
				p.Title = "Правила"
			}
			gotType, gotTitle, _ := eventNotification(test.subject, p)
			if gotType != test.wantType || gotTitle != test.wantTitle {
				t.Fatalf("got (%q, %q), want (%q, %q)", gotType, gotTitle, test.wantType, test.wantTitle)
			}
		})
	}
}

func TestPayloadDecodesOrgProjectionAndArticleAudience(t *testing.T) {
	userID, positionID, departmentID := uuid.New(), uuid.New(), uuid.New()
	raw := []byte(`{
		"user":{"userId":"` + userID.String() + `","status":"ORG_USER_STATUS_ACTIVE","positionIds":["` + positionID.String() + `"],"departmentIds":["` + departmentID.String() + `"]},
		"audience":{"scope":"ARTICLE_AUDIENCE_SCOPE_COMPANY","userIds":["` + userID.String() + `"],"positionIds":["` + positionID.String() + `"],"departmentIds":["` + departmentID.String() + `"]}
	}`)
	var got payload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.User.UserID != userID.String() || got.User.Status != "ORG_USER_STATUS_ACTIVE" {
		t.Fatalf("user projection decoded incorrectly: %+v", got.User)
	}
	if got.Audience.Scope != "ARTICLE_AUDIENCE_SCOPE_COMPANY" || len(got.Audience.PositionIDs) != 1 || len(got.Audience.DepartmentIDs) != 1 {
		t.Fatalf("article audience decoded incorrectly: %+v", got.Audience)
	}
}

func TestParseUUIDStringsReturnsNonNilEmptySlice(t *testing.T) {
	got, err := parseUUIDStrings(nil)
	if err != nil {
		t.Fatalf("parseUUIDStrings() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("parseUUIDStrings(nil) = %v, ожидался непустой slice с длиной 0", got)
	}
}
