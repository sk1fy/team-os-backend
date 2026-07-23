//go:build integration

package storage_test

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newMigrationTestPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return pgxpool.NewWithConfig(ctx, config)
}
