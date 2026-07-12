package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
)

type Summary struct {
	CompanyID   string
	Users       int
	Departments int
	Positions   int
	Invites     int
}

// Run loads and validates fixtures, hashes the shared development password
// with the company auth implementation, and writes all records in one
// serializable PostgreSQL transaction.
func Run(
	ctx context.Context,
	pool *pgxpool.Pool,
	directory string,
	password string,
) (Summary, error) {
	if pool == nil {
		return Summary{}, errors.New("соединение с PostgreSQL не задано")
	}
	if strings.TrimSpace(password) == "" {
		return Summary{}, errors.New("COMPANY_SEED_PASSWORD не задан")
	}
	fixtures, err := Load(directory)
	if err != nil {
		return Summary{}, err
	}
	dataset, err := Normalize(fixtures, time.Now())
	if err != nil {
		return Summary{}, fmt.Errorf("проверить фикстуры: %w", err)
	}
	passwordHash, err := domainauth.HashPassword(password)
	if err != nil {
		return Summary{}, fmt.Errorf("хэшировать COMPANY_SEED_PASSWORD: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Summary{}, fmt.Errorf("начать seed-транзакцию: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := Apply(ctx, tx, dataset, passwordHash); err != nil {
		return Summary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("зафиксировать seed-транзакцию: %w", err)
	}

	return Summary{
		CompanyID:   dataset.Company.ID.String(),
		Users:       len(dataset.Users),
		Departments: len(dataset.Departments),
		Positions:   len(dataset.Positions),
		Invites:     len(dataset.Invites),
	}, nil
}
