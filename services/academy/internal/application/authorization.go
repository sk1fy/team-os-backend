package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func canReadAcademy(actor Actor) bool {
	switch actor.Role {
	case "owner", "admin", "employee", "partner":
		return true
	default:
		return false
	}
}

func (s *Service) assignedCourseIDs(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
) ([]uuid.UUID, error) {
	rows, err := queries.GetUserAssignments(ctx, db.GetUserAssignmentsParams{
		CompanyID: actor.CompanyID,
		UserID:    actor.UserID,
	})
	if err != nil {
		return nil, internal("Не удалось проверить назначения курса", err)
	}
	result := make([]uuid.UUID, 0, len(rows))
	seen := make(map[uuid.UUID]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.CourseID]; exists {
			continue
		}
		seen[row.CourseID] = struct{}{}
		result = append(result, row.CourseID)
	}
	return result, nil
}

func (s *Service) requireCourseAccess(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courseID uuid.UUID,
) error {
	if !canReadAcademy(actor) {
		return forbidden("Недостаточно прав для просмотра академии")
	}
	course, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return notFound("Курс")
		}
		return internal("Не удалось проверить доступ к курсу", err)
	}
	if actor.canManage() {
		return nil
	}
	if course.Status != "published" {
		return forbidden("Черновик курса доступен только управляющим ролям")
	}
	if course.Visibility == "public" || course.Visibility == "company" {
		return nil
	}
	courseIDs, err := s.assignedCourseIDs(ctx, queries, actor)
	if err != nil {
		return err
	}
	for _, assignedID := range courseIDs {
		if assignedID == courseID {
			return nil
		}
	}
	return forbidden("Курс не назначен пользователю")
}

func visibleCourse(actor Actor, course Course, assigned map[uuid.UUID]struct{}) bool {
	if actor.canManage() {
		return true
	}
	if course.Status != "published" {
		return false
	}
	if course.Visibility == "public" || course.Visibility == "company" {
		return true
	}
	_, ok := assigned[course.ID]
	return ok
}
