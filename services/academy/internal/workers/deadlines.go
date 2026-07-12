package workers

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type DeadlinesArgs struct{}

func (DeadlinesArgs) Kind() string { return "academy_deadlines" }

type DeadlinesWorker struct {
	river.WorkerDefaults[DeadlinesArgs]
	service *application.Service
}

func NewDeadlinesWorker(service *application.Service) *DeadlinesWorker {
	return &DeadlinesWorker{service: service}
}

func (w *DeadlinesWorker) Work(ctx context.Context, _ *river.Job[DeadlinesArgs]) error {
	return w.service.ProcessDeadlines(ctx)
}
