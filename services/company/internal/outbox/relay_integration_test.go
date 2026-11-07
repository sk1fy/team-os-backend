//go:build integration

package outbox

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRelayCannotOvertakeLockedCompanyEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("outbox"), postgres.WithUsername("test"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE TABLE outbox (
id uuid PRIMARY KEY, company_id uuid NOT NULL, subject text NOT NULL,
payload jsonb NOT NULL, headers jsonb NOT NULL DEFAULT '{}', event_order bigserial NOT NULL,
occurred_at timestamptz NOT NULL DEFAULT now(), next_attempt_at timestamptz NOT NULL DEFAULT now(),
published_at timestamptz, attempts integer NOT NULL DEFAULT 0, last_error text)`)
	if err != nil {
		t.Fatal(err)
	}
	company, other := uuid.New(), uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, tenant := range []uuid.UUID{company, company, other} {
		event, eventErr := eventbus.NewEvent(tenant.String(), "", map[string]string{"userId": ids[i].String()})
		if eventErr != nil {
			t.Fatal(eventErr)
		}
		event.EventID = ids[i].String()
		body, eventErr := json.Marshal(event)
		if eventErr != nil {
			t.Fatal(eventErr)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO outbox(id,company_id,subject,payload) VALUES($1,$2,$3,$4)`, ids[i], tenant, "teamos.org.user.updated.v1", body); err != nil {
			t.Fatal(err)
		}
	}
	locked, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locked.Rollback(ctx) }()
	if _, err = locked.Exec(ctx, `SELECT id FROM outbox WHERE id=$1 FOR UPDATE`, ids[0]); err != nil {
		t.Fatal(err)
	}
	var published []string
	relay := NewRelay(pool, publisherFunc(func(_ context.Context, _ string, event eventbus.Event, _ ...eventbus.PublishOption) error {
		published = append(published, event.EventID)
		return nil
	}), nil)
	if _, err = relay.publishBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if len(published) != 1 || published[0] != ids[2].String() {
		t.Fatalf("locked first event must block later events of its company, published=%v", published)
	}
	if err = locked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = relay.publishBatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(published) != 3 || published[1] != ids[0].String() || published[2] != ids[1].String() {
		t.Fatalf("company events must resume in order, published=%v", published)
	}
}
