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
	"github.com/sk1fy/team-os-backend/services/notifications/internal/deliverycrypto"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/mailer"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/storage"
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
	pool            *pgxpool.Pool
	queries         *db.Queries
	mu              sync.Mutex
	subscribers     map[uuid.UUID]map[chan uuid.UUID]struct{}
	emailDeliveries verificationEmailDeliveryStore
	emailSender     mailer.Sender
	emailDecryptor  deliverycrypto.Decryptor
	now             func() time.Time
}

func New(pool *pgxpool.Pool) (*Service, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool обязателен")
	}
	queries := db.New(pool)
	emailDeliveries, err := storage.NewEmailDeliveryRepository(queries)
	if err != nil {
		return nil, err
	}
	return &Service{
		pool: pool, queries: queries, subscribers: map[uuid.UUID]map[chan uuid.UUID]struct{}{},
		emailDeliveries: emailDeliveries, emailSender: mailer.NewLogSender(nil), now: time.Now,
	}, nil
}

func (s *Service) SetEmailSender(sender mailer.Sender) error {
	if sender == nil {
		return fmt.Errorf("отправитель email обязателен")
	}
	s.emailSender = sender
	return nil
}

func (s *Service) SetExternalEmailDecryptor(decryptor deliverycrypto.Decryptor) error {
	if decryptor == nil {
		return fmt.Errorf("дешифратор внешних email обязателен")
	}
	s.emailDecryptor = decryptor
	return nil
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

func (s *Service) upsertUserProjection(
	ctx context.Context,
	event eventbus.Event,
	rawUserID string,
	active bool,
	rawPositionIDs, rawDepartmentIDs []string,
	welcome bool,
) (bool, error) {
	eventID, companyID, err := eventIDs(event)
	if err != nil {
		return false, err
	}
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		return false, fmt.Errorf("некорректный userId в событии: %w", err)
	}
	positionIDs, err := parseUUIDStrings(rawPositionIDs)
	if err != nil {
		return false, fmt.Errorf("некорректный positionId в событии: %w", err)
	}
	departmentIDs, err := parseUUIDStrings(rawDepartmentIDs)
	if err != nil {
		return false, fmt.Errorf("некорректный departmentId в событии: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err = queries.UpsertNotificationUser(ctx, db.UpsertNotificationUserParams{
		CompanyID: companyID, UserID: userID, Active: active,
		PositionIds: positionIDs, DepartmentIds: departmentIDs, LastEventAt: event.OccurredAt,
	}); err != nil {
		return false, err
	}
	inserted, err := queries.InsertProcessedEvent(ctx, db.InsertProcessedEventParams{
		EventID: eventID, CompanyID: companyID,
	})
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}

	notificationID := uuid.Nil
	if welcome {
		notificationID = uuid.New()
		if err = queries.InsertNotification(ctx, db.InsertNotificationParams{
			ID: notificationID, CompanyID: companyID, UserID: userID,
			Type: "article_published", Title: "Добро пожаловать в TeamOS",
		}); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	if notificationID != uuid.Nil {
		s.notify(userID, notificationID)
	}
	return true, nil
}

func (s *Service) deactivateUserProjection(ctx context.Context, event eventbus.Event, rawUserID string) (bool, error) {
	eventID, companyID, err := eventIDs(event)
	if err != nil {
		return false, err
	}
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		return false, fmt.Errorf("некорректный userId в событии: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err = queries.DeactivateNotificationUser(ctx, db.DeactivateNotificationUserParams{
		CompanyID: companyID, UserID: userID, LastEventAt: event.OccurredAt,
	}); err != nil {
		return false, err
	}
	inserted, err := queries.InsertProcessedEvent(ctx, db.InsertProcessedEventParams{
		EventID: eventID, CompanyID: companyID,
	})
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) resolveArticleAudience(
	ctx context.Context,
	rawCompanyID, scope string,
	rawUserIDs, rawPositionIDs, rawDepartmentIDs []string,
) ([]uuid.UUID, error) {
	companyID, err := uuid.Parse(rawCompanyID)
	if err != nil {
		return nil, fmt.Errorf("некорректный companyId в событии: %w", err)
	}
	userIDs, err := parseUUIDStrings(rawUserIDs)
	if err != nil {
		return nil, fmt.Errorf("некорректный audience.userId: %w", err)
	}
	positionIDs, err := parseUUIDStrings(rawPositionIDs)
	if err != nil {
		return nil, fmt.Errorf("некорректный audience.positionId: %w", err)
	}
	departmentIDs, err := parseUUIDStrings(rawDepartmentIDs)
	if err != nil {
		return nil, fmt.Errorf("некорректный audience.departmentId: %w", err)
	}
	companyWide := scope == "ARTICLE_AUDIENCE_SCOPE_COMPANY" || scope == "company"
	return s.queries.ResolveArticleAudience(ctx, db.ResolveArticleAudienceParams{
		CompanyID: companyID, CompanyWide: companyWide,
		UserIds: userIDs, PositionIds: positionIDs, DepartmentIds: departmentIDs,
	})
}

func eventIDs(event eventbus.Event) (uuid.UUID, uuid.UUID, error) {
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	companyID, err := uuid.Parse(event.CompanyID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return eventID, companyID, nil
}

func parseUUIDStrings(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, len(values))
	for index, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		result[index] = parsed
	}
	return result, nil
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
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
