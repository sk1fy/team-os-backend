package application

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
	"time"
)

func nullableTimestamp(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func confirmedQueueEmployee(row db.DistributionQueue, visible bool, employee *uuid.UUID) *uuid.UUID {
	if visible && row.Settled && (row.State == "confirmed" || row.State == "kept") {
		return employee
	}
	return nil
}

func queueVisibleReason(row db.DistributionQueue, now time.Time) string {
	if !row.Settled && row.OperationID.Valid && !row.WaitingDeadlineAt.After(now) {
		return "waiting_expired_cancel_pending"
	}
	return row.Reason
}

// Expiration does not wait behind CRM I/O, inbox retries or observation work.
func (s *Service) RunDistributionQueueExpiry(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		q := db.New(s.pool)
		_, e := q.ExpireUndispatchedDistributionQueue(work)
		if e == nil {
			_, e = q.RequestExpiredDistributionCancellation(work)
		}
		cancel()
		if e != nil && s.logger != nil {
			s.logger.Warn("distribution queue expiration deferred", "error", e)
		}
	}
}
