//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"slices"
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
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("tasks"),
		postgres.WithUsername("tasks"),
		postgres.WithPassword("tasks"),
		postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_phase3_concurrency.up.sql"),
		),
		postgres.BasicWaitStrategies(),
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

	t.Run("position snapshot and rollback", func(t *testing.T) {
		boardID, columnID := seedBoardAndColumn(t, ctx, pool, companyID, userID)
		taskID, positionID, memberID, explicitID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		seedTask(t, ctx, pool, companyID, boardID, columnID, taskID, userID, 0, nil, nil)
		actor.Token = "user-token"
		resolver := &fakeCompany{users: []uuid.UUID{memberID, explicitID, memberID, userID}}
		service.SetCompanyClient(resolver)
		updated, err := service.UpdateTask(ctx, actor, UpdateTaskInput{ID: taskID, AssigneeIDsSet: true, AssigneeIDs: []uuid.UUID{explicitID}, AssigneePositionID: &positionID})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(updated.AssigneeIDs, []uuid.UUID{explicitID}) || updated.AssigneePositionID == nil || *updated.AssigneePositionID != positionID {
			t.Fatalf("task assignment changed shape: %+v", updated)
		}
		resolver.users = []uuid.UUID{uuid.New()}
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM outbox WHERE aggregate_id=$1`, taskID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Payload struct {
				Users []string `json:"assigneeUserIds"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Payload.Users) != 2 || !slices.Contains(envelope.Payload.Users, memberID.String()) || !slices.Contains(envelope.Payload.Users, explicitID.String()) {
			t.Fatalf("wrong snapshot: %s", raw)
		}
		if resolver.companyID != companyID || resolver.token != "user-token" || resolver.positionID != positionID {
			t.Fatalf("wrong resolver identity: %+v", resolver)
		}
		resolver.err = errors.New("company unavailable")
		otherPosition := uuid.New()
		title := "Не сохранять"
		_, err = service.UpdateTask(ctx, actor, UpdateTaskInput{ID: taskID, Title: &title, AssigneePositionID: &otherPosition})
		if err == nil {
			t.Fatal("dependency failure must fail task update")
		}
		unchanged, err := service.GetTask(ctx, actor, taskID)
		if err != nil || unchanged.Title != "Задача" || *unchanged.AssigneePositionID != positionID {
			t.Fatalf("partial update: %+v %v", unchanged, err)
		}
		var events int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id=$1`, taskID).Scan(&events); err != nil || events != 1 {
			t.Fatalf("events=%d err=%v", events, err)
		}
		resolver.err = nil
		resolver.users = nil
		emptyTask := uuid.New()
		seedTask(t, ctx, pool, companyID, boardID, columnID, emptyTask, userID, 1, nil, nil)
		if _, err := service.UpdateTask(ctx, actor, UpdateTaskInput{ID: emptyTask, AssigneePositionID: &positionID}); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id=$1`, emptyTask).Scan(&events); err != nil || events != 0 {
			t.Fatalf("empty membership events=%d err=%v", events, err)
		}
	})
	t.Run("recurrence position snapshot and dependency failure", func(t *testing.T) {
		boardID, columnID := seedBoardAndColumn(t, ctx, pool, companyID, userID)
		taskID, positionID, memberID := uuid.New(), uuid.New(), uuid.New()
		completed := time.Now().UTC()
		seedTask(t, ctx, pool, companyID, boardID, columnID, taskID, userID, 0, []byte(`{"frequency":"daily","interval":1,"weekdays":[]}`), &completed)
		if _, err := pool.Exec(ctx, `UPDATE tasks SET assignee_position_id=$1 WHERE id=$2`, positionID, taskID); err != nil {
			t.Fatal(err)
		}
		resolver := &fakeCompany{err: errors.New("company unavailable")}
		service.SetCompanyClient(resolver)
		if err := service.ProcessRecurrence(ctx, companyID, taskID); err == nil {
			t.Fatal("expected resolution error")
		}
		var count int
		var generated bool
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE board_id=$1`, boardID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("tasks=%d err=%v", count, err)
		}
		if err := pool.QueryRow(ctx, `SELECT recurrence_generated_at IS NOT NULL FROM tasks WHERE id=$1`, taskID).Scan(&generated); err != nil || generated {
			t.Fatalf("generated=%v err=%v", generated, err)
		}
		resolver.err = nil
		resolver.users = []uuid.UUID{memberID}
		if err := service.ProcessRecurrence(ctx, companyID, taskID); err != nil {
			t.Fatal(err)
		}
		if resolver.token != "" || resolver.companyID != companyID {
			t.Fatalf("recurrence resolver identity: %+v", resolver)
		}
		var recipients []string
		if err := pool.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(payload->'payload'->'assigneeUserIds')) FROM outbox WHERE aggregate_id IN (SELECT id FROM tasks WHERE board_id=$1)`, boardID).Scan(&recipients); err != nil || !slices.Equal(recipients, []string{memberID.String()}) {
			t.Fatalf("recipients=%v err=%v", recipients, err)
		}
		resolver.err = errors.New("company unavailable after completion")
		if err := service.ProcessRecurrence(ctx, companyID, taskID); err != nil {
			t.Fatalf("completed retry should not resolve again: %v", err)
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
	if _, err := pool.Exec(ctx,
		`INSERT INTO boards (id, company_id, name, type, owner_id) VALUES ($1, $2, 'Доска', 'personal', $3)`,
		boardID, companyID, ownerID); err != nil {
		t.Fatalf("подготовка доски: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO columns (id, board_id, name, "order") VALUES ($1, $2, 'Колонка', 0)`,
		columnID, boardID); err != nil {
		t.Fatalf("подготовка колонки: %v", err)
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

type fakeCompany struct {
	users                 []uuid.UUID
	err                   error
	companyID, positionID uuid.UUID
	token                 string
}

func (f *fakeCompany) ResolvePositionUsers(ctx context.Context, companyID uuid.UUID, token string, positionID uuid.UUID) ([]uuid.UUID, error) {
	f.companyID = companyID
	f.positionID = positionID
	f.token = token
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing resolver deadline")
	}
	return f.users, f.err
}
