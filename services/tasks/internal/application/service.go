package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type RecurrenceEnqueuer interface {
	EnqueueRecurrenceTx(ctx context.Context, tx pgx.Tx, companyID, taskID uuid.UUID) error
}

type Service struct {
	pool               *pgxpool.Pool
	now                func() time.Time
	recurrenceEnqueuer RecurrenceEnqueuer
}

func NewService(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, errors.New("соединение с PostgreSQL не задано")
	}
	return &Service{pool: pool, now: time.Now}, nil
}

func (s *Service) SetRecurrenceEnqueuer(enqueuer RecurrenceEnqueuer) {
	s.recurrenceEnqueuer = enqueuer
}

func (s *Service) emit(
	ctx context.Context,
	queries *db.Queries,
	companyID, aggregateID, actorID uuid.UUID,
	subject string,
	payload proto.Message,
) error {
	id := uuid.New()
	occurredAt := s.now().UTC()
	payloadJSON, err := protojson.Marshal(payload)
	if err != nil {
		return internal("Не удалось сформировать payload события", err)
	}
	body, err := json.Marshal(struct {
		EventID    string          `json:"eventId"`
		OccurredAt string          `json:"occurredAt"`
		CompanyID  string          `json:"companyId"`
		ActorID    string          `json:"actorId"`
		Payload    json.RawMessage `json:"payload"`
	}{
		EventID: id.String(), OccurredAt: occurredAt.Format(time.RFC3339Nano),
		CompanyID: companyID.String(), ActorID: actorID.String(), Payload: payloadJSON,
	})
	if err != nil {
		return internal("Не удалось сформировать событие", err)
	}
	headers, _ := json.Marshal(map[string]string{"Nats-Msg-Id": id.String()})
	_, err = queries.CreateOutboxEvent(ctx, db.CreateOutboxEventParams{
		ID: id, CompanyID: companyID, AggregateID: aggregateID, Subject: subject,
		Payload: body, Headers: headers, OccurredAt: occurredAt,
	})
	if err != nil {
		return internal("Не удалось сохранить событие", err)
	}
	return nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
