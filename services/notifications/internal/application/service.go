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
	mu          sync.Mutex
	subscribers map[uuid.UUID]map[chan uuid.UUID]struct{}
}

func New(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool обязателен")
	}
	return &Service{pool: pool, subscribers: map[uuid.UUID]map[chan uuid.UUID]struct{}{}}, nil
}
func (s *Service) List(ctx context.Context, companyID, userID uuid.UUID) ([]Notification, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,type,title,body,link,read,created_at FROM notifications WHERE company_id=$1 AND user_id=$2 ORDER BY created_at DESC`, companyID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Notification{}
	for rows.Next() {
		var n Notification
		if err = rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Link, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}
func (s *Service) Count(ctx context.Context, companyID, userID uuid.UUID) (uint32, error) {
	var n uint32
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE company_id=$1 AND user_id=$2 AND read=false`, companyID, userID).Scan(&n)
	return n, err
}
func (s *Service) MarkRead(ctx context.Context, companyID, userID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE notifications SET read=true WHERE id=$1 AND company_id=$2 AND user_id=$3`, id, companyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
func (s *Service) MarkAllRead(ctx context.Context, companyID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET read=true WHERE company_id=$1 AND user_id=$2 AND read=false`, companyID, userID)
	return err
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
	tag, err := tx.Exec(ctx, `INSERT INTO processed_events(event_id,company_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, eventID, companyID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	ids := make([]uuid.UUID, 0, len(userIDs))
	for _, userID := range userIDs {
		id := uuid.New()
		_, err = tx.Exec(ctx, `INSERT INTO notifications(id,company_id,user_id,type,title,body,link) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, companyID, userID, typ, title, body, link)
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
