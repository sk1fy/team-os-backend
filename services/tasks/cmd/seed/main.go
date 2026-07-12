package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/seed"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		log.Printf("tasks seed: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("tasks-seed", flag.ContinueOnError)
	fixturesDirectory := flags.String("fixtures", "", "директория экспортированных JSON-фикстур")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("неожиданные аргументы: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*fixturesDirectory) == "" {
		return errors.New("обязателен флаг --fixtures DIRECTORY")
	}
	databaseURL := strings.TrimSpace(getenv("TASKS_DB_URL"))
	if databaseURL == "" {
		return errors.New("TASKS_DB_URL не задан")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("настроить PostgreSQL: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("подключиться к PostgreSQL: %w", err)
	}

	summary, err := seed.Run(ctx, pool, *fixturesDirectory)
	if err != nil {
		return err
	}
	log.Printf(
		"tasks seed завершён: company=%s boards=%d columns=%d tasks=%d labels=%d comments=%d",
		summary.CompanyID, summary.Boards, summary.Columns,
		summary.Tasks, summary.Labels, summary.Comments,
	)
	return nil
}
