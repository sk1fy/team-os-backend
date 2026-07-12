// Package consumers wires durable NATS subscriptions that replicate knowledge
// base changes into link lessons (§10.2).
package consumers

import (
	"context"
	"log/slog"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

const stream = "TEAMOS"

// Start subscribes to kb article events. Subscriptions drain when ctx ends.
func Start(ctx context.Context, bus *eventbus.Bus, service *application.Service, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	subscriptions := []struct {
		subject string
		durable string
		handler eventbus.HandlerFunc
	}{
		{
			subject: "teamos.kb.article.updated.v1",
			durable: "academy-kb-article-updated",
			handler: service.HandleKbArticleUpdated,
		},
		{
			subject: "teamos.kb.article.deleted.v1",
			durable: "academy-kb-article-deleted",
			handler: service.HandleKbArticleDeleted,
		},
	}
	for _, subscription := range subscriptions {
		subject := subscription.subject
		if _, err := bus.Subscribe(ctx, eventbus.ConsumerConfig{
			Subject:    subject,
			Stream:     stream,
			Durable:    subscription.durable,
			DLQSubject: "teamos.dlq.academy.consumer.v1",
			AckWait:    30 * time.Second,
			NakDelay:   5 * time.Second,
			MaxDeliver: 5,
			OnError: func(err error) {
				logger.Error("academy consumer failed", "subject", subject, "error", err)
			},
		}, subscription.handler); err != nil {
			return err
		}
	}
	return nil
}
