package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/mail"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const (
	defaultRefreshTTL = 30 * 24 * time.Hour
	defaultInviteTTL  = 7 * 24 * time.Hour
	defaultAmoSyncTTL = 5 * time.Minute
)

type amoSyncState struct {
	mu          sync.Mutex
	lastAttempt time.Time
}

type Service struct {
	pool          databasePool
	issuer        *sharedauth.TokenIssuer
	refreshTTL    time.Duration
	inviteTTL     time.Duration
	now           func() time.Time
	dummyHash     string
	passwordSlots chan struct{}
	externalUsers ExternalEmployeeProvider
	logger        *slog.Logger
	amoSyncTTL    time.Duration
	amoSyncMu     sync.Mutex
	amoSyncStates map[uuid.UUID]*amoSyncState
}

// databasePool is the subset of pgxpool.Pool used by the application layer.
// Keeping the dependency at this boundary makes transactional workflows
// testable without starting PostgreSQL.
type databasePool interface {
	db.DBTX
	Begin(context.Context) (pgx.Tx, error)
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type ExternalEmployeeProvider interface {
	FetchAll(context.Context, string) ([]ExternalEmployee, error)
}

type ServiceOption func(*Service)

func WithExternalEmployeeProvider(provider ExternalEmployeeProvider) ServiceOption {
	return func(service *Service) {
		service.externalUsers = provider
	}
}

func WithLogger(logger *slog.Logger) ServiceOption {
	return func(service *Service) {
		if logger != nil {
			service.logger = logger
		}
	}
}

func WithAmoSyncInterval(interval time.Duration) ServiceOption {
	return func(service *Service) {
		if interval > 0 {
			service.amoSyncTTL = interval
		}
	}
}

func NewService(pool *pgxpool.Pool, issuer *sharedauth.TokenIssuer, options ...ServiceOption) (*Service, error) {
	dummyHash, err := domainauth.HashPassword("teamos-dummy-password")
	if err != nil {
		return nil, err
	}
	service := &Service{
		pool:          pool,
		issuer:        issuer,
		refreshTTL:    defaultRefreshTTL,
		inviteTTL:     defaultInviteTTL,
		now:           time.Now,
		dummyHash:     dummyHash,
		passwordSlots: make(chan struct{}, 4),
		logger:        slog.Default(),
		amoSyncTTL:    defaultAmoSyncTTL,
		amoSyncStates: make(map[uuid.UUID]*amoSyncState),
	}
	for _, option := range options {
		option(service)
	}
	return service, nil
}

func (s *Service) acquirePasswordSlot(ctx context.Context) (func(), error) {
	select {
	case s.passwordSlots <- struct{}{}:
		return func() { <-s.passwordSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func requireAdministrator(actor Actor) error {
	if actor.Role != "owner" && actor.Role != "admin" {
		return forbidden("Недостаточно прав для изменения оргструктуры")
	}
	return nil
}

func normalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	at := strings.LastIndexByte(normalized, '@')
	if err != nil || address.Address != normalized || at <= 0 || !validEmailDomain(normalized[at+1:]) {
		return "", validation("Некорректный email")
	}
	return normalized, nil
}

func validEmailDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func normalizePhone(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	phone := strings.TrimSpace(*value)
	if phone == "" {
		return nil, nil
	}
	digits := 0
	for index, character := range phone {
		switch {
		case character >= '0' && character <= '9':
			digits++
		case character == '+' && index == 0:
		case character == ' ' || character == '-' || character == '(' || character == ')':
		default:
			return nil, validation("Некорректный номер телефона")
		}
	}
	if digits < 7 || digits > 15 {
		return nil, validation("Некорректный номер телефона")
	}
	return &phone, nil
}

func requiredText(value, message string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", validation(message)
	}
	return trimmed, nil
}

func pgText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func nullableUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func uuidPointer(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func datePointer(value pgtype.Date) *string {
	if !value.Valid {
		return nil
	}
	result := value.Time.Format(time.DateOnly)
	return &result
}

func int16Pointer(value pgtype.Int2) *int16 {
	if !value.Valid {
		return nil
	}
	result := value.Int16
	return &result
}

func parseIPAddress(value string) *netip.Addr {
	if value == "" {
		return nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return nil
	}
	return &address
}

func userFromDB(value db.User, positions []uuid.UUID) User {
	return User{
		ID:                value.ID,
		CompanyID:         value.CompanyID,
		Email:             value.Email,
		FirstName:         value.FirstName,
		LastName:          value.LastName,
		AvatarURL:         textPointer(value.AvatarUrl),
		Phone:             textPointer(value.Phone),
		Role:              value.Role,
		Status:            value.Status,
		PositionIDs:       append([]uuid.UUID(nil), positions...),
		BirthDate:         datePointer(value.BirthDate),
		HiredAt:           datePointer(value.HiredAt),
		VacationAllowance: int16Pointer(value.VacationAllowance),
		CreatedAt:         value.CreatedAt,
		Source:            value.Source,
	}
}

func userEventSnapshot(user User, departmentIDs []uuid.UUID) map[string]any {
	return map[string]any{
		"userId": user.ID.String(), "email": user.Email,
		"firstName": user.FirstName, "lastName": user.LastName,
		"role": orgRoleEventValue(user.Role), "status": orgStatusEventValue(user.Status),
		"positionIds": stringsFromUUIDs(user.PositionIDs), "departmentIds": stringsFromUUIDs(departmentIDs),
	}
}

func stringsFromUUIDs(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func orgRoleEventValue(role string) string {
	return "ORG_USER_ROLE_" + strings.ToUpper(role)
}

func orgStatusEventValue(status string) string {
	return "ORG_USER_STATUS_" + strings.ToUpper(status)
}

func companyFromDB(value db.Company) Company {
	var ownerID uuid.UUID
	if value.OwnerID.Valid {
		ownerID = value.OwnerID.UUID
	}
	return Company{
		ID: value.ID, Name: value.Name, LogoURL: textPointer(value.LogoUrl),
		OwnerID: ownerID, CreatedAt: value.CreatedAt, AmoAccountID: textPointer(value.AmoAccountID),
	}
}

func inviteFromDB(value db.Invite) Invite {
	return Invite{
		ID: value.ID, Email: textPointer(value.Email), Token: value.Token, Role: value.Role,
		PositionID: uuidPointer(value.PositionID), DepartmentID: uuidPointer(value.DepartmentID),
		InvitedByID: value.InvitedByID, Status: value.Status, CreatedAt: value.CreatedAt,
	}
}

func (s *Service) emit(
	ctx context.Context,
	queries *db.Queries,
	companyID, actorID uuid.UUID,
	subject string,
	payload any,
) error {
	id := uuid.New()
	occurredAt := s.now().UTC()
	body, err := json.Marshal(struct {
		EventID    string `json:"eventId"`
		OccurredAt string `json:"occurredAt"`
		CompanyID  string `json:"companyId"`
		ActorID    string `json:"actorId"`
		Payload    any    `json:"payload"`
	}{
		EventID: id.String(), OccurredAt: occurredAt.Format(time.RFC3339Nano),
		CompanyID: companyID.String(), ActorID: actorID.String(), Payload: payload,
	})
	if err != nil {
		return internal("Не удалось сформировать событие", err)
	}
	headers, _ := json.Marshal(map[string]string{"Nats-Msg-Id": id.String()})
	_, err = queries.CreateOutboxEvent(ctx, db.CreateOutboxEventParams{
		ID: id, CompanyID: companyID, Subject: subject,
		Payload: body, Headers: headers, OccurredAt: occurredAt,
	})
	if err != nil {
		return internal("Не удалось сохранить событие", err)
	}
	return nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
