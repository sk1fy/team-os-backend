package consumers

import (
	"context"
	"log/slog"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/application"
)

func Start(ctx context.Context, bus *eventbus.Bus, service *application.Service, logger *slog.Logger) error {
	subjects := []string{"teamos.org.user.created.v1", "teamos.org.user.deactivated.v1", "teamos.org.invite.created.v1", "teamos.kb.article.published.v1", "teamos.tasks.task.assigned.v1", "teamos.tasks.comment.added.v1", "teamos.tasks.task.due_soon.v1", "teamos.academy.course.assigned.v1", "teamos.academy.course.due_soon.v1", "teamos.tasks.mention.created.v1", "teamos.kb.mention.created.v1"}
	for _, subject := range subjects {
		subject := subject
		if _, err := bus.Subscribe(ctx, eventbus.ConsumerConfig{Subject: subject, Stream: "TEAMOS", Durable: "notifications-" + subject[7:], DLQSubject: "teamos.dlq.notifications.consumer.v1", AckWait: 30 * time.Second, NakDelay: 5 * time.Second, MaxDeliver: 5, OnError: func(err error) { logger.Error("notifications consumer failed", "subject", subject, "error", err) }}, service.Handle(subject)); err != nil {
			return err
		}
	}
	return nil
}
