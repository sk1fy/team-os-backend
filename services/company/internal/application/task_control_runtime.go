package application

import (
	"context"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const taskControlSessionTTL = 5 * time.Minute
const taskControlMaxActorSessions = 64

// CleanupTaskControlRuntime removes expired records in bounded, indexed batches.
// Expiry is checked on every read independently of garbage collection.
func (s *Service) CleanupTaskControlRuntime(ctx context.Context) error {
	q := db.New(s.pool)
	if _, err := q.PruneTaskControlRuntimeSessions(ctx, s.now()); err != nil {
		return err
	}
	_, err := q.PruneTaskControlAssertions(ctx, s.now())
	return err
}
func (s *Service) RunTaskControlRuntimeCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.CleanupTaskControlRuntime(cleanupCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			s.logger.Error("task control runtime cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
