//go:build integration

package application

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestConcurrentTaskOrderingAndRecurrence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграции")
	}
	migration := filepath.Join(filepath.Dir(filename), "..", "..", "migrations", "000001_init.up.sql")
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("tasks"),
		postgres.WithUsername("tasks"),
		postgres.WithPassword("tasks"),
		postgres.WithInitScripts(migration),
	)
	if err != nil {
		t.Fatalf("запуск PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if terminateErr := testcontainers.TerminateContainer(container); terminateErr != nil {
			t.Errorf("остановка PostgreSQL: %v", terminateErr)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("строка подключения: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("подключение к PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(pool)
	if err != nil {
		t.Fatalf("создание сервиса: %v", err)
	}

	companyID := uuid.New()
	userID := uuid.New()
	actor := Actor{CompanyID: companyID, UserID: userID, Role: "admin"}

	t.Run("стандартная доска создаётся идемпотентно", func(t *testing.T) {
		bootstrapCompanyID := uuid.New()
		bootstrapActor := Actor{CompanyID: bootstrapCompanyID, UserID: uuid.New(), Role: "owner"}
		for attempt := 0; attempt < 2; attempt++ {
			boards, getErr := service.GetBoards(ctx, bootstrapActor)
			if getErr != nil {
				t.Fatalf("получение досок: %v", getErr)
			}
			if len(boards) != 1 || boards[0].Name != "Задачи компании" {
				t.Fatalf("доски = %#v", boards)
			}
		}
		var boardsCount, columnsCount int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM boards WHERE company_id = $1`, bootstrapCompanyID).Scan(&boardsCount); err != nil {
			t.Fatalf("подсчёт досок: %v", err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM columns WHERE board_id IN (SELECT id FROM boards WHERE company_id = $1)`, bootstrapCompanyID).Scan(&columnsCount); err != nil {
			t.Fatalf("подсчёт колонок: %v", err)
		}
		if boardsCount != 1 || columnsCount != 3 {
			t.Fatalf("boards=%d columns=%d", boardsCount, columnsCount)
		}
	})

	t.Run("конкурентные перемещения не образуют deadlock", func(t *testing.T) {
		boardID, columnID := seedBoardAndColumn(t, ctx, pool, companyID, userID)
		taskIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		for order, taskID := range taskIDs {
			seedTask(t, ctx, pool, companyID, boardID, columnID, taskID, userID, int32(order), nil, nil)
		}

		start := make(chan struct{})
		errors := make(chan error, 2)
		var wg sync.WaitGroup
		moves := []MoveTaskInput{
			{TaskID: taskIDs[0], ColumnID: columnID, Order: 2},
			{TaskID: taskIDs[1], ColumnID: columnID, Order: 0},
		}
		for _, move := range moves {
			wg.Add(1)
			go func(input MoveTaskInput) {
				defer wg.Done()
				<-start
				moveCtx, moveCancel := context.WithTimeout(ctx, 15*time.Second)
				defer moveCancel()
				_, moveErr := service.MoveTask(moveCtx, actor, input)
				errors <- moveErr
			}(move)
		}
		close(start)
		wg.Wait()
		close(errors)
		for moveErr := range errors {
			if moveErr != nil {
				t.Fatalf("конкурентное перемещение: %v", moveErr)
			}
		}

		var count, distinct int
		var minimum, maximum int32
		if err = pool.QueryRow(ctx, `
			SELECT count(*), count(DISTINCT "order"), min("order"), max("order")
			FROM tasks WHERE column_id = $1`, columnID).Scan(&count, &distinct, &minimum, &maximum); err != nil {
			t.Fatalf("проверка порядка: %v", err)
		}
		if count != 3 || distinct != 3 || minimum != 0 || maximum != 2 {
			t.Fatalf("некорректный порядок: count=%d distinct=%d min=%d max=%d", count, distinct, minimum, maximum)
		}
	})

	t.Run("конкурентное создание получает уникальный порядок", func(t *testing.T) {
		boardID, columnID := seedBoardAndColumn(t, ctx, pool, companyID, userID)
		start := make(chan struct{})
		errors := make(chan error, 5)
		var wg sync.WaitGroup
		for index := 0; index < 5; index++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, createErr := service.CreateTask(ctx, actor, CreateTaskInput{
					BoardID: boardID, ColumnID: columnID, Title: "Задача", Priority: "medium",
				})
				errors <- createErr
			}()
		}
		close(start)
		wg.Wait()
		close(errors)
		for createErr := range errors {
			if createErr != nil {
				t.Fatalf("конкурентное создание: %v", createErr)
			}
		}

		var count, distinct int
		if err = pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT "order") FROM tasks WHERE column_id = $1`, columnID).
			Scan(&count, &distinct); err != nil {
			t.Fatalf("проверка порядка: %v", err)
		}
		if count != 5 || distinct != 5 {
			t.Fatalf("некорректный порядок: count=%d distinct=%d", count, distinct)
		}
	})

	t.Run("повтор создаётся и публикует назначение ровно один раз", func(t *testing.T) {
		boardID, columnID := seedBoardAndColumn(t, ctx, pool, companyID, userID)
		parentID := uuid.New()
		assigneeID := uuid.New()
		completedAt := time.Now().UTC()
		recurrence := []byte(`{"frequency":"daily","interval":1,"weekdays":[]}`)
		seedTask(t, ctx, pool, companyID, boardID, columnID, parentID, userID, 0, recurrence, &completedAt, assigneeID)

		if err = service.ProcessRecurrence(ctx, companyID, parentID); err != nil {
			t.Fatalf("первый запуск recurrence: %v", err)
		}
		if err = service.ProcessRecurrence(ctx, companyID, parentID); err != nil {
			t.Fatalf("повторный запуск recurrence: %v", err)
		}

		var tasksCount, eventsCount int
		var generated bool
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE board_id = $1`, boardID).Scan(&tasksCount); err != nil {
			t.Fatalf("подсчёт задач: %v", err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id IN (SELECT id FROM tasks WHERE board_id = $1)`, boardID).
			Scan(&eventsCount); err != nil {
			t.Fatalf("подсчёт событий: %v", err)
		}
		if err = pool.QueryRow(ctx, `SELECT recurrence_generated_at IS NOT NULL FROM tasks WHERE id = $1`, parentID).Scan(&generated); err != nil {
			t.Fatalf("проверка маркера recurrence: %v", err)
		}
		if tasksCount != 2 || eventsCount != 1 || !generated {
			t.Fatalf("recurrence неидемпотентен: tasks=%d events=%d generated=%v", tasksCount, eventsCount, generated)
		}
	})
}

func seedBoardAndColumn(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID, ownerID uuid.UUID,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	boardID, columnID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO boards (id, company_id, name, type, owner_id) VALUES ($1, $2, 'Доска', 'personal', $3);
		INSERT INTO columns (id, board_id, name, "order") VALUES ($4, $1, 'Колонка', 0)`,
		boardID, companyID, ownerID, columnID); err != nil {
		t.Fatalf("подготовка доски: %v", err)
	}
	return boardID, columnID
}

func seedTask(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID, boardID, columnID, taskID, authorID uuid.UUID,
	order int32,
	recurrence []byte,
	completedAt *time.Time,
	assignees ...uuid.UUID,
) {
	t.Helper()
	if len(assignees) == 0 {
		assignees = []uuid.UUID{authorID}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tasks (
			id, company_id, board_id, column_id, "order", title, author_id,
			assignee_ids, priority, recurrence, completed_at
		) VALUES ($1, $2, $3, $4, $5, 'Задача', $6, $7, 'medium', $8, $9)`,
		taskID, companyID, boardID, columnID, order, authorID, assignees, recurrence, completedAt); err != nil {
		t.Fatalf("подготовка задачи: %v", err)
	}
}
