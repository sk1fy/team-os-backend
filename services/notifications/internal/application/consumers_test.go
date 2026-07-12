package application

import "testing"

func TestEventNotificationUsesRussianTextAndTypes(t *testing.T) {
	tests := []struct{ subject, wantType, wantTitle string }{
		{"teamos.tasks.task.assigned.v1", "task_assigned", "Вам назначена задача: Отчёт"},
		{"teamos.tasks.comment.added.v1", "task_comment", "Новый комментарий к задаче: Отчёт"},
		{"teamos.academy.course.due_soon.v1", "course_due", "Скоро срок курса: Онбординг"},
		{"teamos.kb.mention.created.v1", "mention", "Вас упомянули: Правила"},
	}
	for _, test := range tests {
		t.Run(test.subject, func(t *testing.T) {
			p := payload{}
			switch test.subject {
			case "teamos.tasks.task.assigned.v1", "teamos.tasks.comment.added.v1":
				p.Title = "Отчёт"
			case "teamos.academy.course.due_soon.v1":
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
