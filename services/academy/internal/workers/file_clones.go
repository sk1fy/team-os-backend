package workers

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type FileClonesArgs struct{}

func (FileClonesArgs) Kind() string { return "academy_file_clones" }

type FileClonesWorker struct {
	river.WorkerDefaults[FileClonesArgs]
	service *application.Service
}

func NewFileClonesWorker(service *application.Service) *FileClonesWorker {
	return &FileClonesWorker{service: service}
}

func (w *FileClonesWorker) Work(ctx context.Context, _ *river.Job[FileClonesArgs]) error {
	return w.service.ProcessFileCloneJobs(ctx)
}
