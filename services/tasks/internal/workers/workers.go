package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
)

type Client struct {
	river *river.Client[pgx.Tx]
}

type enqueuer struct {
	client *river.Client[pgx.Tx]
}

func (e *enqueuer) EnqueueRecurrenceTx(ctx context.Context, tx pgx.Tx, companyID, taskID uuid.UUID) error {
	if e.client == nil {
		return fmt.Errorf("очередь river не инициализирована")
	}
	_, err := e.client.InsertTx(ctx, tx, RecurrenceArgs{
		CompanyID: companyID.String(),
		TaskID:    taskID.String(),
	}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}})
	return err
}

func Setup(ctx context.Context, pool *pgxpool.Pool, service *application.Service) (*Client, error) {
	if pool == nil || service == nil {
		return nil, fmt.Errorf("зависимости river не заданы")
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("инициализировать river migrate: %w", err)
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{}); err != nil {
		return nil, fmt.Errorf("применить миграции river: %w", err)
	}

	workerBundle := river.NewWorkers()
	river.AddWorker(workerBundle, NewRecurrenceWorker(service))
	river.AddWorker(workerBundle, NewDueDatesWorker(service))

	periodicJobs := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return DueDatesArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers:      workerBundle,
		PeriodicJobs: periodicJobs,
	})
	if err != nil {
		return nil, fmt.Errorf("создать river client: %w", err)
	}
	service.SetRecurrenceEnqueuer(&enqueuer{client: client})
	return &Client{river: client}, nil
}

func (c *Client) Start(ctx context.Context) error {
	if c == nil || c.river == nil {
		return fmt.Errorf("river client не инициализирован")
	}
	return c.river.Start(ctx)
}

func (c *Client) Stop(ctx context.Context) error {
	if c == nil || c.river == nil {
		return nil
	}
	return c.river.Stop(ctx)
}
