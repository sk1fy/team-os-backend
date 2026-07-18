package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/storage/db"
)

type Notification struct {
	ID, UserID  uuid.UUID
	Type, Title string
	Body, Link  *string
	Read        bool
	CreatedAt   time.Time
}
type Service struct {
	pool        *pgxpool.Pool
	queries     *db.Queries
	mu          sync.Mutex
	subscribers map[uuid.UUID]map[chan uuid.UUID]struct{}
}

func New(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool обязателен")
	}
	return &Service{pool: pool, queries: db.New(pool), subscribers: map[uuid.UUID]map[chan uuid.UUID]struct{}{}}, nil
}
func (s *Service) List(ctx context.Context, companyID, userID uuid.UUID) ([]Notification, error) {
	rows, err := s.queries.ListNotifications(ctx, db.ListNotificationsParams{CompanyID: companyID, UserID: userID})
	if err != nil {
		return nil, err
	}
	result := make([]Notification, 0, len(rows))
	for _, row := range rows {
		result = append(result, Notification{
			ID: row.ID, UserID: row.UserID,
			Type: row.Type, Title: row.Title,
			Body: row.Body, Link: row.Link,
			Read: row.Read, CreatedAt: row.CreatedAt,
		})
	}
	return result, nil
}
func (s *Service) Count(ctx context.Context, companyID, userID uuid.UUID) (uint32, error) {
	n, err := s.queries.CountUnread(ctx, db.CountUnreadParams{CompanyID: companyID, UserID: userID})
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}
func (s *Service) MarkRead(ctx context.Context, companyID, userID, id uuid.UUID) error {
	affected, err := s.queries.MarkRead(ctx, db.MarkReadParams{ID: id, CompanyID: companyID, UserID: userID})
	if err != nil {
		return err
	}
	if affected == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
func (s *Service) MarkAllRead(ctx context.Context, companyID, userID uuid.UUID) error {
	return s.queries.MarkAllRead(ctx, db.MarkAllReadParams{CompanyID: companyID, UserID: userID})
}
func (s *Service) Subscribe(userID uuid.UUID) (<-chan uuid.UUID, func()) {
	ch := make(chan uuid.UUID, 16)
	s.mu.Lock()
	if s.subscribers[userID] == nil {
		s.subscribers[userID] = map[chan uuid.UUID]struct{}{}
	}
	s.subscribers[userID][ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() { s.mu.Lock(); delete(s.subscribers[userID], ch); close(ch); s.mu.Unlock() }
}
func (s *Service) notify(userID, id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers[userID] {
		select {
		case ch <- id:
		default:
		}
	}
}
func (s *Service) Create(ctx context.Context, event eventbus.Event, userID uuid.UUID, typ, title string, body, link *string) (bool, error) {
	return s.CreateMany(ctx, event, []uuid.UUID{userID}, typ, title, body, link)
}
func (s *Service) CreateMany(ctx context.Context, event eventbus.Event, userIDs []uuid.UUID, typ, title string, body, link *string) (bool, error) {
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return false, err
	}
	companyID, err := uuid.Parse(event.CompanyID)
	if err != nil {
		return false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	inserted, err := queries.InsertProcessedEvent(ctx, db.InsertProcessedEventParams{EventID: eventID, CompanyID: companyID})
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		return false, nil
	}
	ids := make([]uuid.UUID, 0, len(userIDs))
	for _, userID := range userIDs {
		id := uuid.New()
		err = queries.InsertNotification(ctx, db.InsertNotificationParams{
			ID: id, CompanyID: companyID, UserID: userID,
			Type: typ, Title: title, Body: body, Link: link,
		})
		if err != nil {
			return false, err
		}
		ids = append(ids, id)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	for index, id := range ids {
		s.notify(userIDs[index], id)
	}
	return len(ids) > 0, nil
}
