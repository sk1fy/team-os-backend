package workers

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
)

type DueDatesArgs struct{}

func (DueDatesArgs) Kind() string { return "tasks_due_dates" }

type DueDatesWorker struct {
	river.WorkerDefaults[DueDatesArgs]
	service *application.Service
}

func NewDueDatesWorker(service *application.Service) *DueDatesWorker {
	return &DueDatesWorker{service: service}
}

func (w *DueDatesWorker) Work(ctx context.Context, _ *river.Job[DueDatesArgs]) error {
	return w.service.ProcessDueSoon(ctx)
}
