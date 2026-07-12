package workers

import (
	"context"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
)

type RecurrenceArgs struct {
	CompanyID string `json:"companyId"`
	TaskID    string `json:"taskId"`
}

func (RecurrenceArgs) Kind() string { return "tasks_recurrence" }

type RecurrenceWorker struct {
	river.WorkerDefaults[RecurrenceArgs]
	service *application.Service
}

func NewRecurrenceWorker(service *application.Service) *RecurrenceWorker {
	return &RecurrenceWorker{service: service}
}

func (w *RecurrenceWorker) Work(ctx context.Context, job *river.Job[RecurrenceArgs]) error {
	companyID, err := uuid.Parse(job.Args.CompanyID)
	if err != nil {
		return err
	}
	taskID, err := uuid.Parse(job.Args.TaskID)
	if err != nil {
		return err
	}
	return w.service.ProcessRecurrence(ctx, companyID, taskID)
}
