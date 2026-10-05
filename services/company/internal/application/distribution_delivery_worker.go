package application

import (
	"context"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// RunDistributionDelivery resumes committed inbox work and registered operation
// mirrors after restart. Every pass is bounded and stops with application shutdown.
func (s *Service) RunDistributionDelivery(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for n := 0; n < 20; n++ {
			work, cancel := context.WithTimeout(ctx, 20*time.Second)
			found, err := s.ProcessDistributionDelivery(work)
			cancel()
			if err != nil && s.logger != nil {
				s.logger.Warn("distribution inbox processing deferred", "error", err)
			}
			if !found || ctx.Err() != nil {
				break
			}
		}
		for n := 0; n < 20; n++ {
			work, cancel := context.WithTimeout(ctx, 20*time.Second)
			found, e := s.ProcessDistributionObservation(work)
			cancel()
			if e != nil && s.logger != nil {
				s.logger.Warn("distribution observation deferred", "error", e)
			}
			if !found || ctx.Err() != nil {
				break
			}
		}
		for n := 0; n < 20; n++ {
			work, cancel := context.WithTimeout(ctx, 20*time.Second)
			found, e := s.ProcessDistributionQueue(work)
			cancel()
			if e != nil && s.logger != nil {
				s.logger.Warn("distribution queue deferred", "error", e)
			}
			if !found || ctx.Err() != nil {
				break
			}
		}
		work, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.ReconcileDistributionOperations(work, 20)
		if err != nil && s.logger != nil {
			s.logger.Warn("distribution operation reconciliation deferred", "error", err)
		}
		if s.logger != nil {
			if d, e := db.New(s.pool).DistributionDeliveryDiagnostics(work); e == nil {
				s.logger.Info("distribution delivery backlog", "backlog", d.Backlog, "blocked", d.Blocked, "oldestSeconds", d.OldestSeconds, "unfinishedOperations", d.Unfinished)
			}
		}
		cancel()
	}
}
