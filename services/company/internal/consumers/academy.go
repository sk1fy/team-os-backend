// Package consumers reacts to events from other services. Course deletion in
// academy triggers cleanup of positions.required_course_ids here (§10.1).
package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
)

const stream = "TEAMOS"

// Start subscribes to academy events; the subscription drains when ctx ends.
func Start(ctx context.Context, bus *eventbus.Bus, pool *pgxpool.Pool, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	_, err := bus.Subscribe(ctx, eventbus.ConsumerConfig{
		Subject:    "teamos.academy.course.deleted.v1",
		Stream:     stream,
		Durable:    "company-academy-course-deleted",
		DLQSubject: "teamos.dlq.company.consumer.v1",
		AckWait:    30 * time.Second,
		NakDelay:   5 * time.Second,
		MaxDeliver: 5,
		OnError: func(err error) {
			logger.Error("company consumer failed", "subject", "teamos.academy.course.deleted.v1", "error", err)
		},
	}, eventbus.HandlerFunc(func(handlerContext context.Context, event eventbus.Event) (bool, error) {
		return handleCourseDeleted(handlerContext, pool, event)
	}))
	return err
}

func handleCourseDeleted(ctx context.Context, pool *pgxpool.Pool, event eventbus.Event) (bool, error) {
	var payload struct {
		CourseID string `json:"courseId"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode academy.course.deleted payload: %w", err)
	}
	courseID, err := uuid.Parse(payload.CourseID)
	if err != nil {
		return false, fmt.Errorf("academy.course.deleted: invalid courseId %q", payload.CourseID)
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return false, fmt.Errorf("invalid eventId %q", event.EventID)
	}
	companyID, err := uuid.Parse(event.CompanyID)
	if err != nil {
		return false, fmt.Errorf("invalid companyId %q", event.CompanyID)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin consumer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inserted, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, company_id)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`, eventID, companyID)
	if err != nil {
		return false, fmt.Errorf("mark event processed: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `
		UPDATE positions
		SET required_course_ids = array_remove(required_course_ids, $2)
		WHERE company_id = $1 AND $2 = ANY(required_course_ids)`, companyID, courseID); err != nil {
		return false, fmt.Errorf("clean required_course_ids: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit consumer transaction: %w", err)
	}
	return true, nil
}
