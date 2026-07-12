package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const academyLink = "/academy"

type Service struct {
	pool    *pgxpool.Pool
	kb      KbClient
	company CompanyClient
	logger  *slog.Logger
	now     func() time.Time
}

func NewService(pool *pgxpool.Pool, kb KbClient, company CompanyClient, logger *slog.Logger) (*Service, error) {
	if pool == nil {
		return nil, errors.New("соединение с PostgreSQL не задано")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{pool: pool, kb: kb, company: company, logger: logger, now: time.Now}, nil
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
