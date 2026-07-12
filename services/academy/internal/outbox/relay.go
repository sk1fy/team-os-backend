package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

const (
	defaultPollInterval = 500 * time.Millisecond
	maxPublishAttempts  = 5
	dlqSubject          = "teamos.dlq.academy.publisher.v1"
)

type Relay struct {
	pool         *pgxpool.Pool
	publisher    eventbus.Publisher
	logger       *slog.Logger
	pollInterval time.Duration
}

func NewRelay(pool *pgxpool.Pool, publisher eventbus.Publisher, logger *slog.Logger) *Relay {
	if logger == nil {
		logger = slog.Default()
	}
	return &Relay{pool: pool, publisher: publisher, logger: logger, pollInterval: defaultPollInterval}
}

func (r *Relay) Run(ctx context.Context) error {
	if r.pool == nil || r.publisher == nil {
		return errors.New("outbox relay dependencies are required")
	}
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		processed, err := r.publishBatch(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.ErrorContext(ctx, "outbox batch failed", "error", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if processed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Relay) publishBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin outbox transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT candidate.id, candidate.subject, candidate.payload, candidate.headers, candidate.attempts
		FROM outbox AS candidate
		WHERE candidate.published_at IS NULL
		  AND candidate.next_attempt_at <= now()
		  AND NOT EXISTS (
		      SELECT 1
		      FROM outbox AS earlier
		      WHERE earlier.company_id = candidate.company_id
		        AND earlier.aggregate_id = candidate.aggregate_id
		        AND earlier.published_at IS NULL
		        AND earlier.event_order < candidate.event_order
		  )
		ORDER BY candidate.event_order
		FOR UPDATE OF candidate SKIP LOCKED
		LIMIT 50`)
	if err != nil {
		return 0, fmt.Errorf("select outbox: %w", err)
	}
	type row struct {
		id       uuid.UUID
		subject  string
		payload  []byte
		headers  []byte
		attempts int32
	}
	batch := make([]row, 0, 50)
	for rows.Next() {
		var item row
		if err = rows.Scan(&item.id, &item.subject, &item.payload, &item.headers, &item.attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan outbox: %w", err)
		}
		batch = append(batch, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate outbox: %w", err)
	}
	rows.Close()

	for _, item := range batch {
		event, decodeErr := eventbus.DecodeEvent(item.payload)
		if decodeErr != nil {
			if item.attempts+1 >= maxPublishAttempts {
				dlqEvent, eventErr := eventbus.NewEvent(uuid.Nil.String(), "", map[string]any{
					"outboxId": item.id.String(), "raw": string(item.payload), "error": decodeErr.Error(),
				})
				if eventErr == nil {
					dlqEvent.EventID = item.id.String() + "-dlq"
					eventErr = r.publisher.Publish(ctx, dlqSubject, dlqEvent,
						eventbus.WithHeader("Teamos-Original-Subject", item.subject),
						eventbus.WithHeader("Teamos-Publish-Error", decodeErr.Error()))
				}
				if eventErr == nil {
					if err = markDeadLettered(ctx, tx, item.id, decodeErr); err != nil {
						return 0, err
					}
					continue
				}
			}
			if err = markFailure(ctx, tx, item.id, decodeErr); err != nil {
				return 0, err
			}
			continue
		}
		var headers map[string]string
		if len(item.headers) != 0 {
			_ = json.Unmarshal(item.headers, &headers)
		}
		options := make([]eventbus.PublishOption, 0, len(headers))
		for name, value := range headers {
			if name != "Nats-Msg-Id" {
				options = append(options, eventbus.WithHeader(name, value))
			}
		}
		publishErr := r.publisher.Publish(ctx, item.subject, event, options...)
		if publishErr != nil {
			if item.attempts+1 >= maxPublishAttempts {
				dlqEvent := event
				dlqEvent.EventID += "-dlq"
				dlqErr := r.publisher.Publish(ctx, dlqSubject, dlqEvent,
					eventbus.WithHeader("Teamos-Original-Subject", item.subject),
					eventbus.WithHeader("Teamos-Publish-Error", publishErr.Error()))
				if dlqErr == nil {
					if err = markDeadLettered(ctx, tx, item.id, publishErr); err != nil {
						return 0, err
					}
					continue
				}
			}
			if err = markFailure(ctx, tx, item.id, publishErr); err != nil {
				return 0, err
			}
			continue
		}
		if _, err = tx.Exec(ctx, `
			UPDATE outbox
			SET published_at = now(), attempts = attempts + 1, last_error = NULL
			WHERE id = $1`, item.id); err != nil {
			return 0, fmt.Errorf("mark outbox published: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit outbox transaction: %w", err)
	}
	return len(batch), nil
}

func markDeadLettered(ctx context.Context, tx pgx.Tx, id uuid.UUID, cause error) error {
	message := truncateError(cause)
	if _, err := tx.Exec(ctx, `
		UPDATE outbox
		SET published_at = now(), attempts = attempts + 1, last_error = $2
		WHERE id = $1`, id, message); err != nil {
		return fmt.Errorf("mark outbox dead-lettered: %w", err)
	}
	return nil
}

func markFailure(ctx context.Context, tx pgx.Tx, id uuid.UUID, cause error) error {
	message := truncateError(cause)
	if _, err := tx.Exec(ctx, `
		UPDATE outbox
		SET attempts = attempts + 1,
		    last_error = $2,
		    next_attempt_at = now() + (
		        least(300, power(2, least(attempts, 8))::integer)::text || ' seconds'
		    )::interval
		WHERE id = $1`, id, message); err != nil {
		return fmt.Errorf("mark outbox failure: %w", err)
	}
	return nil
}

func truncateError(cause error) string {
	message := cause.Error()
	if len(message) > 2000 {
		return message[:2000]
	}
	return message
}
