//go:build integration

package application

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EmployeeImportTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	return companyAccessTestPool(t, ctx)
}
