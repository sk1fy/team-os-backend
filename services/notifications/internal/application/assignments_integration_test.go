//go:build integration

package application

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTaskAssignmentSnapshotDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, file, _, _ := runtime.Caller(0)
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("notifications"), postgres.WithUsername("notifications"), postgres.WithPassword("notifications"), postgres.WithInitScripts(filepath.Join(filepath.Dir(file), "../../migrations/000001_init.up.sql")), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Error(err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	service, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	companyID, memberID, authorID, positionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	raw, err := json.Marshal(map[string]any{"assigneeUserIds": []string{memberID.String(), memberID.String(), authorID.String()}, "assigneePositionId": positionID.String(), "authorId": authorID.String(), "title": "Отчёт", "link": "/tasks"})
	if err != nil {
		t.Fatal(err)
	}
	event := eventbus.Event{EventID: uuid.NewString(), CompanyID: companyID.String(), ActorID: authorID.String(), Payload: raw, OccurredAt: time.Now().UTC()}
	handler := service.Handle("teamos.tasks.task.assigned.v1")
	for attempt := 0; attempt < 2; attempt++ {
		handled, err := handler(ctx, event)
		if err != nil || handled != (attempt == 0) {
			t.Fatalf("attempt=%d handled=%v err=%v", attempt, handled, err)
		}
	}
	notifications, err := service.List(ctx, companyID, memberID)
	if err != nil || len(notifications) != 1 || notifications[0].Title != "Вам назначена задача: Отчёт" || notifications[0].Type != "task_assigned" {
		t.Fatalf("notifications=%+v err=%v", notifications, err)
	}
	authorNotifications, err := service.List(ctx, companyID, authorID)
	if err != nil || len(authorNotifications) != 0 {
		t.Fatalf("author notifications=%+v err=%v", authorNotifications, err)
	}
	otherTenantNotifications, err := service.List(ctx, uuid.New(), memberID)
	if err != nil || len(otherTenantNotifications) != 0 {
		t.Fatalf("cross tenant notifications=%+v err=%v", otherTenantNotifications, err)
	}
	legacy := event
	legacy.EventID = uuid.NewString()
	legacy.Payload = []byte(`{"assigneePositionId":"` + positionID.String() + `","title":"Legacy"}`)
	if handled, err := handler(ctx, legacy); err == nil || handled {
		t.Fatalf("legacy unresolved event handled=%v err=%v", handled, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM processed_events WHERE event_id=$1`, legacy.EventID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy marker=%d err=%v", count, err)
	}
}
