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
	"github.com/sk1fy/team-os-backend/pkg/httpx"
)

const defaultPollInterval = 500 * time.Millisecond

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

// Run drains committed outbox rows until ctx is cancelled. Multiple replicas
// safely share the work through FOR UPDATE SKIP LOCKED.
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
		SELECT id, subject, payload, headers
		FROM outbox
		WHERE published_at IS NULL AND next_attempt_at <= now()
		ORDER BY occurred_at
		FOR UPDATE SKIP LOCKED
		LIMIT 50`)
	if err != nil {
		return 0, fmt.Errorf("select outbox: %w", err)
	}
	type row struct {
		id      uuid.UUID
		subject string
		payload []byte
		headers []byte
	}
	batch := make([]row, 0, 50)
	for rows.Next() {
		var item row
		if err = rows.Scan(&item.id, &item.subject, &item.payload, &item.headers); err != nil {
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
	r.updateMetrics(ctx)
	return len(batch), nil
}

func (r *Relay) updateMetrics(ctx context.Context) {
	var age float64
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(EPOCH FROM now() - MIN(occurred_at)), 0) FROM outbox WHERE published_at IS NULL`).Scan(&age); err == nil {
		httpx.SetGauge("teamos_outbox_oldest_pending_age_seconds", "", age)
	}
}

func markFailure(ctx context.Context, tx pgx.Tx, id uuid.UUID, cause error) error {
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
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
