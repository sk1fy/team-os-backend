package consumers

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/application"
)

var subjects = []string{
	"teamos.org.user.created.v1",
	"teamos.org.user.deactivated.v1",
	"teamos.org.invite.created.v1",
	"teamos.kb.article.published.v1",
	"teamos.tasks.task.assigned.v1",
	"teamos.tasks.comment.added.v1",
	"teamos.tasks.task.due_soon.v1",
	"teamos.academy.course.assigned.v1",
	"teamos.academy.course.due_soon.v1",
	"teamos.tasks.mention.created.v1",
	"teamos.kb.mention.created.v1",
}

func Start(ctx context.Context, bus *eventbus.Bus, service *application.Service, logger *slog.Logger) error {
	for _, subject := range subjects {
		subject := subject
		config := eventbus.ConsumerConfig{
			Subject:    subject,
			Stream:     "TEAMOS",
			Durable:    durableName(subject),
			DLQSubject: "teamos.dlq.notifications.consumer.v1",
			AckWait:    30 * time.Second,
			NakDelay:   5 * time.Second,
			MaxDeliver: 5,
			OnError: func(err error) {
				logger.Error("notifications consumer failed", "subject", subject, "error", err)
			},
		}
		if _, err := bus.Subscribe(ctx, config, service.Handle(subject)); err != nil {
			return err
		}
	}
	return nil
}

func durableName(subject string) string {
	name := strings.TrimPrefix(subject, "teamos.")
	return "notifications-" + strings.ReplaceAll(name, ".", "-")
}
