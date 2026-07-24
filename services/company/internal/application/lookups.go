package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) ResolveReportUserScope(
	ctx context.Context,
	actor Actor,
	input ResolveReportUserScopeInput,
) (ReportUserScope, error) {
	if err := requireAdministrator(actor); err != nil {
		return ReportUserScope{}, err
	}
	var search *string
	if input.Search != nil {
		if trimmed := strings.TrimSpace(*input.Search); trimmed != "" {
			search = &trimmed
		}
	}
	rows, err := db.New(s.pool).ResolveReportUserScope(ctx, db.ResolveReportUserScopeParams{
		Search: pgText(search), CompanyID: actor.CompanyID,
		PositionID: nullableUUID(input.PositionID), DepartmentID: nullableUUID(input.DepartmentID),
	})
	if err != nil {
		return ReportUserScope{}, internal("Не удалось определить сотрудников для отчёта", err)
	}
	result := ReportUserScope{
		UserIDs: make([]uuid.UUID, 0, len(rows)),
	}
	for _, row := range rows {
		result.UserIDs = append(result.UserIDs, row.ID)
		if matches, ok := row.MatchesSearch.(bool); ok && matches {
			result.SearchUserIDs = append(result.SearchUserIDs, row.ID)
		}
	}
	return result, nil
}

func (s *Service) GetReportUserProfiles(
	ctx context.Context,
	actor Actor,
	input GetReportUserProfilesInput,
) ([]ReportUserProfile, error) {
	if err := requireAdministrator(actor); err != nil {
		return nil, err
	}
	if len(input.UserIDs) == 0 {
		return []ReportUserProfile{}, nil
	}
	rows, err := db.New(s.pool).GetReportUserProfiles(ctx, db.GetReportUserProfilesParams{
		PreferredPositionID:   nullableUUID(input.PreferredPositionID),
		PreferredDepartmentID: nullableUUID(input.PreferredDepartmentID),
		CompanyID:             actor.CompanyID, UserIds: append([]uuid.UUID(nil), input.UserIDs...),
	})
	if err != nil {
		return nil, internal("Не удалось получить сотрудников страницы отчёта", err)
	}
	result := make([]ReportUserProfile, len(rows))
	for index, row := range rows {
		result[index] = ReportUserProfile{
			UserID: row.UserID, Email: row.Email, FirstName: row.FirstName,
			LastName: textValue(row.LastName), DepartmentName: textPointer(row.DepartmentName),
		}
		if name := strings.TrimSpace(row.PositionName); name != "" {
			result[index].PositionName = &name
		}
	}
	return result, nil
}

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
