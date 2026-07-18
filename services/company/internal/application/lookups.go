package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// GetUsersByIDs returns only users belonging to the actor's company. Missing
// IDs are omitted, which keeps the lookup useful for denormalization callers.
func (s *Service) GetUsersByIDs(ctx context.Context, actor Actor, userIDs []uuid.UUID) ([]User, error) {
	if len(userIDs) == 0 {
		return []User{}, nil
	}
	rows, err := db.New(s.pool).GetUsersByIDs(ctx, db.GetUsersByIDsParams{
		CompanyID: actor.CompanyID,
		UserIds:   userIDs,
	})
	if err != nil {
		return nil, internal("Не удалось получить сотрудников", err)
	}
	result := make([]User, len(rows))
	for index, row := range rows {
		result[index] = User{
			ID:                row.ID,
			CompanyID:         row.CompanyID,
			Email:             row.Email,
			FirstName:         row.FirstName,
			LastName:          textValue(row.LastName),
			AvatarURL:         textPointer(row.AvatarUrl),
			Phone:             textPointer(row.Phone),
			Role:              row.Role,
			Status:            row.Status,
			PositionIDs:       append([]uuid.UUID(nil), row.PositionIds...),
			BirthDate:         datePointer(row.BirthDate),
			HiredAt:           datePointer(row.HiredAt),
			VacationAllowance: int16Pointer(row.VacationAllowance),
			CreatedAt:         row.CreatedAt,
			Source:            row.Source,
			AccessMode:        row.AccessMode,
		}
	}
	return result, nil
}

// ResolvePositionUsers returns active users assigned to a position inside the
// actor's company.
func (s *Service) ResolvePositionUsers(ctx context.Context, actor Actor, positionID uuid.UUID) ([]uuid.UUID, error) {
	queries := db.New(s.pool)
	if _, err := queries.GetPosition(ctx, db.GetPositionParams{
		CompanyID: actor.CompanyID,
		ID:        positionID,
	}); isNoRows(err) {
		return nil, notFound("Должность")
	} else if err != nil {
		return nil, internal("Не удалось проверить должность", err)
	}
	userIDs, err := queries.ResolvePositionUserIDs(ctx, db.ResolvePositionUserIDsParams{
		CompanyID:  actor.CompanyID,
		PositionID: positionID,
	})
	if err != nil {
		return nil, internal("Не удалось определить сотрудников должности", err)
	}
	return userIDs, nil
}

// ResolveDepartmentUsers returns active users assigned to positions in the
// selected department, optionally including all descendants.
func (s *Service) ResolveDepartmentUsers(
	ctx context.Context,
	actor Actor,
	departmentID uuid.UUID,
	includeDescendants bool,
) ([]uuid.UUID, error) {
	queries := db.New(s.pool)
	if _, err := queries.GetDepartment(ctx, db.GetDepartmentParams{
		CompanyID: actor.CompanyID,
		ID:        departmentID,
	}); isNoRows(err) {
		return nil, notFound("Отдел")
	} else if err != nil {
		return nil, internal("Не удалось проверить отдел", err)
	}
	userIDs, err := queries.ResolveDepartmentUserIDs(ctx, db.ResolveDepartmentUserIDsParams{
		CompanyID:          actor.CompanyID,
		DepartmentID:       departmentID,
		IncludeDescendants: includeDescendants,
	})
	if err != nil {
		return nil, internal("Не удалось определить сотрудников отдела", err)
	}
	return userIDs, nil
}
