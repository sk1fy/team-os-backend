package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type Client struct {
	river *river.Client[pgx.Tx]
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
	river.AddWorker(workerBundle, NewDeadlinesWorker(service))
	river.AddWorker(workerBundle, NewFileClonesWorker(service))

	periodicJobs := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return DeadlinesArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Minute),
			func() (river.JobArgs, *river.InsertOpts) { return FileClonesArgs{}, nil },
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
