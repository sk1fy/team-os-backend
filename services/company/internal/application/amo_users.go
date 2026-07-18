package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/mail"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) syncAmoUsers(ctx context.Context, actor Actor) error {
	if s.externalUsers == nil {
		return nil
	}
	unlock, ok := s.tryStartAmoSync(actor.CompanyID)
	if !ok {
		return nil
	}
	defer unlock()

	if err := s.syncAmoUsersNow(ctx, actor); err != nil {
		s.logger.WarnContext(ctx, "amoCRM user import failed; serving TeamOS users", "company_id", actor.CompanyID, "error", err)
	}
	return nil
}

func (s *Service) tryStartAmoSync(companyID uuid.UUID) (func(), bool) {
	s.amoSyncMu.Lock()
	state := s.amoSyncStates[companyID]
	if state == nil {
		state = &amoSyncState{}
		s.amoSyncStates[companyID] = state
	}
	s.amoSyncMu.Unlock()

	if !state.mu.TryLock() {
		return nil, false
	}
	now := s.now().UTC()
	if !state.lastAttempt.IsZero() && now.Sub(state.lastAttempt) < s.amoSyncTTL {
		state.mu.Unlock()
		return nil, false
	}
	// Throttle both successful attempts and failures so an unavailable upstream
	// cannot turn every org-tree read into a slow external request.
	state.lastAttempt = now
	return state.mu.Unlock, true
}

func (s *Service) syncAmoUsersNow(ctx context.Context, actor Actor) error {
	if s.externalUsers == nil {
		return nil
	}
	company, err := db.New(s.pool).GetCompany(ctx, actor.CompanyID)
	if err != nil {
		return internal("Не удалось получить настройки amoCRM", err)
	}
	amoAccountID := textPointer(company.AmoAccountID)
	if amoAccountID == nil {
		return nil
	}
	employees, err := s.externalUsers.FetchAll(ctx, *amoAccountID)
	if err != nil {
		return upstream("Не удалось получить сотрудников из amoCRM", err)
	}
	normalized, err := normalizeExternalEmployees(actor.CompanyID, employees)
	if err != nil {
		return upstream("Внешний API вернул некорректных сотрудников", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return internal("Не удалось начать импорт сотрудников", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoUserSync(ctx, actor.CompanyID); err != nil {
		return internal("Не удалось заблокировать импорт сотрудников", err)
	}

	for _, employee := range normalized {
		_, findErr := queries.FindUserForAmoSync(ctx, db.FindUserForAmoSyncParams{
			CompanyID: actor.CompanyID, ExternalID: pgtype.Text{String: employee.ID, Valid: true}, Email: employee.Email,
		})
		if findErr == nil {
			// Импорт не перезаписывает профиль, статус или доступ уже известного TeamOS-пользователя.
			continue
		}
		if !isNoRows(findErr) {
			return internal("Не удалось сопоставить сотрудника amoCRM", findErr)
		}
		row, err := queries.CreateAmoUser(ctx, db.CreateAmoUserParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, Email: employee.Email,
			FirstName: employee.FirstName, LastName: pgText(&employee.LastName),
			AvatarUrl: pgText(employee.AvatarURL), ExternalID: pgtype.Text{String: employee.ID, Valid: true},
			AvatarSource:      avatarSource(employee.AvatarURL),
			ExternalGroupID:   pgText(trimmedStringPointer(employee.GroupID)),
			ExternalGroupName: pgText(trimmedStringPointer(employee.GroupName)),
		})
		if isUniqueViolation(err) {
			return conflict("Не удалось сопоставить сотрудников amoCRM: email уже занят")
		}
		if err != nil {
			return internal("Не удалось сохранить сотрудника amoCRM", err)
		}
		if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(userFromDB(row, nil), nil),
		}); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось завершить импорт сотрудников", err)
	}
	return nil
}

type normalizedExternalEmployee struct {
	ID, Email, FirstName, LastName, GroupID, GroupName string
	AvatarURL                                          *string
}

func normalizeExternalEmployees(companyID uuid.UUID, values []ExternalEmployee) ([]normalizedExternalEmployee, error) {
	result := make([]normalizedExternalEmployee, 0, len(values))
	ids := make(map[string]struct{}, len(values))
	emails := make(map[string]struct{}, len(values))
	for index, value := range values {
		id := strings.TrimSpace(value.ID)
		if id == "" {
			return nil, fmt.Errorf("сотрудник %d: отсутствует id", index+1)
		}
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("сотрудник %d: повторяется id %q", index+1, id)
		}
		ids[id] = struct{}{}
		firstName, lastName, _ := splitEmployeeName(value.Name)
		email := fallbackAmoEmail(companyID, id)
		if value.Email != nil && strings.TrimSpace(*value.Email) != "" {
			candidate := strings.ToLower(strings.TrimSpace(*value.Email))
			address, parseErr := mail.ParseAddress(candidate)
			if parseErr != nil || address.Address != candidate {
				return nil, fmt.Errorf("сотрудник %s: некорректный email", id)
			}
			email = candidate
		}
		if _, exists := emails[email]; exists {
			return nil, fmt.Errorf("сотрудник %s: повторяется email %q", id, email)
		}
		emails[email] = struct{}{}
		result = append(result, normalizedExternalEmployee{
			ID: id, Email: email, FirstName: firstName, LastName: lastName,
			AvatarURL: trimmedStringPointerValue(value.AvatarURL),
			GroupID:   strings.TrimSpace(value.GroupID), GroupName: strings.TrimSpace(value.GroupName),
		})
	}
	return result, nil
}

func splitEmployeeName(value string) (string, string, bool) {
	parts := strings.FieldsFunc(strings.TrimSpace(value), unicode.IsSpace)
	if len(parts) == 0 {
		return "Сотрудник", "", false
	}
	if len(parts) == 1 {
		return parts[0], "", false
	}
	return parts[0], strings.Join(parts[1:], " "), true
}

func fallbackAmoEmail(companyID uuid.UUID, externalID string) string {
	digest := sha256.Sum256([]byte(externalID))
	return fmt.Sprintf("amo-%s-%x@users.invalid", companyID.String(), digest[:8])
}

func trimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func trimmedStringPointerValue(value *string) *string {
	if value == nil {
		return nil
	}
	return trimmedStringPointer(*value)
}

func avatarSource(avatarURL *string) pgtype.Text {
	if avatarURL == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: "amo", Valid: true}
}
