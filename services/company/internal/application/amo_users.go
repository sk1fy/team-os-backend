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
		return internal("Не удалось начать синхронизацию сотрудников", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoUserSync(ctx, actor.CompanyID); err != nil {
		return internal("Не удалось заблокировать синхронизацию сотрудников", err)
	}

	externalIDs := make([]string, 0, len(normalized))
	for _, employee := range normalized {
		externalIDs = append(externalIDs, employee.ID)
		current, findErr := queries.FindUserForAmoSync(ctx, db.FindUserForAmoSyncParams{
			CompanyID: actor.CompanyID, ExternalID: pgtype.Text{String: employee.ID, Valid: true}, Email: employee.Email,
		})
		created := false
		changedFields := []string(nil)
		var row db.User
		switch {
		case findErr == nil:
			if !employee.HasLastName {
				employee.LastName = preservedAmoLastName(current.LastName)
			}
			// Эти поля принадлежат amoCRM. Локальные значения намеренно заменяются при синхронизации.
			changedFields = amoChangedFields(current, employee)
			row, err = queries.UpdateAmoUser(ctx, db.UpdateAmoUserParams{
				Email: employee.Email, FirstName: employee.FirstName, LastName: employee.LastName,
				AvatarUrl: pgText(employee.AvatarURL), ExternalID: pgtype.Text{String: employee.ID, Valid: true},
				ExternalGroupID:   pgText(trimmedStringPointer(employee.GroupID)),
				ExternalGroupName: pgText(trimmedStringPointer(employee.GroupName)),
				CompanyID:         actor.CompanyID, ID: current.ID,
			})
		case isNoRows(findErr):
			row, err = queries.CreateAmoUser(ctx, db.CreateAmoUserParams{
				ID: uuid.New(), CompanyID: actor.CompanyID, Email: employee.Email,
				FirstName: employee.FirstName, LastName: employee.LastName,
				AvatarUrl: pgText(employee.AvatarURL), ExternalID: pgtype.Text{String: employee.ID, Valid: true},
				ExternalGroupID:   pgText(trimmedStringPointer(employee.GroupID)),
				ExternalGroupName: pgText(trimmedStringPointer(employee.GroupName)),
			})
			created = true
		default:
			return internal("Не удалось сопоставить сотрудника amoCRM", findErr)
		}
		if isUniqueViolation(err) {
			return conflict("Не удалось сопоставить сотрудников amoCRM: email уже занят")
		}
		if err != nil {
			return internal("Не удалось сохранить сотрудника amoCRM", err)
		}
		if created {
			if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.created.v1", map[string]any{
				"user": userEventSnapshot(userFromDB(row, nil), nil),
			}); err != nil {
				return err
			}
		} else if len(changedFields) > 0 {
			joined, joinErr := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: actor.CompanyID, ID: row.ID})
			if joinErr != nil {
				return internal("Не удалось получить синхронизированного сотрудника", joinErr)
			}
			departments, claimsErr := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{CompanyID: actor.CompanyID, UserID: row.ID})
			if claimsErr != nil {
				return internal("Не удалось получить отделы синхронизированного сотрудника", claimsErr)
			}
			if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.updated.v1", map[string]any{
				"user": userEventSnapshot(userFromJoinedRow(joined), departments), "changedFields": changedFields,
			}); err != nil {
				return err
			}
		}
	}

	deactivatedIDs, err := queries.DeactivateMissingAmoUsers(ctx, db.DeactivateMissingAmoUsersParams{
		CompanyID: actor.CompanyID, ExternalIds: externalIDs,
	})
	if err != nil {
		return internal("Не удалось деактивировать уволенных сотрудников amoCRM", err)
	}
	for _, userID := range deactivatedIDs {
		if err = queries.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{
			UserID: userID, RevokedAt: pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
		}); err != nil {
			return internal("Не удалось отозвать сессии уволенного сотрудника", err)
		}
		if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.deactivated.v1", map[string]any{
			"userId": userID.String(),
		}); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось завершить синхронизацию сотрудников", err)
	}
	return nil
}

type normalizedExternalEmployee struct {
	ID, Email, FirstName, LastName, GroupID, GroupName string
	AvatarURL                                          *string
	HasLastName                                        bool
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
		firstName, lastName, hasLastName := splitEmployeeName(value.Name)
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
			HasLastName: hasLastName,
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

func preservedAmoLastName(value string) string {
	if value == "amoCRM" {
		return ""
	}
	return value
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

func amoChangedFields(current db.User, next normalizedExternalEmployee) []string {
	result := make([]string, 0, 5)
	if current.Email != next.Email {
		result = append(result, "email")
	}
	if current.FirstName != next.FirstName {
		result = append(result, "firstName")
	}
	if current.LastName != next.LastName {
		result = append(result, "lastName")
	}
	if !equalText(current.AvatarUrl, next.AvatarURL) {
		result = append(result, "avatarUrl")
	}
	if current.Status != "active" {
		result = append(result, "status")
	}
	return result
}

func equalText(current pgtype.Text, next *string) bool {
	if !current.Valid {
		return next == nil
	}
	return next != nil && current.String == *next
}
