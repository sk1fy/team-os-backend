package application

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func normalizedEmployeeSections(values []string) []string {
	result := append([]string(nil), values...)
	slices.Sort(result)
	return result
}

func employeeSectionsEqual(left, right []string) bool {
	return slices.Equal(normalizedEmployeeSections(left), normalizedEmployeeSections(right))
}

func replaceEmployeeSections(
	ctx context.Context,
	queries *db.Queries,
	companyID, userID uuid.UUID,
	sections []string,
	grantedBy *uuid.UUID,
) error {
	if err := queries.DeleteEmployeeSectionAccess(ctx, db.DeleteEmployeeSectionAccessParams{
		CompanyID: companyID, UserID: userID,
	}); err != nil {
		return internal("Не удалось обновить доступ к разделам", err)
	}
	actorID := uuid.NullUUID{}
	if grantedBy != nil {
		actorID = uuid.NullUUID{UUID: *grantedBy, Valid: true}
	}
	for _, section := range normalizedEmployeeSections(sections) {
		if err := queries.GrantEmployeeSectionAccess(ctx, db.GrantEmployeeSectionAccessParams{
			CompanyID: companyID, UserID: userID, Section: section, GrantedBy: actorID,
		}); err != nil {
			return internal("Не удалось обновить доступ к разделам", err)
		}
	}
	return nil
}

func createUserAdminAudit(
	ctx context.Context,
	queries *db.Queries,
	companyID uuid.UUID,
	targetUserID *uuid.UUID,
	actorUserID *uuid.UUID,
	actorKind, action string,
	beforeState, afterState any,
	requestID string,
	createdAt time.Time,
) error {
	beforeJSON, err := json.Marshal(beforeState)
	if err != nil {
		return internal("Не удалось подготовить аудит пользователя", err)
	}
	afterJSON, err := json.Marshal(afterState)
	if err != nil {
		return internal("Не удалось подготовить аудит пользователя", err)
	}
	target := uuid.NullUUID{}
	if targetUserID != nil {
		target = uuid.NullUUID{UUID: *targetUserID, Valid: true}
	}
	actor := uuid.NullUUID{}
	if actorUserID != nil {
		actor = uuid.NullUUID{UUID: *actorUserID, Valid: true}
	}
	requestID = strings.TrimSpace(requestID)
	if err = queries.CreateUserAdminAudit(ctx, db.CreateUserAdminAuditParams{
		ID: uuid.New(), CompanyID: companyID, TargetUserID: target, ActorUserID: actor,
		ActorKind: actorKind, Action: action, BeforeState: beforeJSON, AfterState: afterJSON,
		RequestID: pgtype.Text{String: requestID, Valid: requestID != ""}, CreatedAt: createdAt.UTC(),
	}); err != nil {
		return internal("Не удалось записать аудит пользователя", err)
	}
	return nil
}

func userAuditState(user User) map[string]any {
	return map[string]any{
		"id": user.ID.String(), "role": user.Role, "status": user.Status,
		"source": user.Source, "sectionAccess": append([]string(nil), user.SectionAccess...),
	}
}
