package application

import (
	"context"

	"github.com/google/uuid"
	domainauth "github.com/sk1fy/team-os-backend/services/academy/internal/domain/authorization"
	domaincourse "github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
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
	converted := courseFromRow(course)
	if actor.canManage() {
		return nil
	}
	if converted.LifecycleStatus == "deleted" {
		return notFound("Курс")
	}
	if converted.DistributionStatus == "blocked" {
		return forbidden("Курс временно заблокирован")
	}
	if converted.LifecycleStatus == "archived" {
		return forbidden("Курс находится в архиве")
	}
	if converted.OwnerType == "partner" {
		if actor.Role == "partner" && converted.OwnerUserID != nil && *converted.OwnerUserID == actor.UserID {
			return nil
		}
		return notFound("Курс")
	}
	if converted.Status != "published" {
		return forbidden("Черновик курса доступен только управляющим ролям")
	}
	if actor.Role != "partner" && (converted.Visibility == "public" || converted.Visibility == "company") {
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
	if course.LifecycleStatus == "deleted" || course.LifecycleStatus == "archived" || course.DistributionStatus == "blocked" {
		return false
	}
	if course.OwnerType == "partner" {
		return actor.Role == "partner" && course.OwnerUserID != nil && *course.OwnerUserID == actor.UserID
	}
	if course.Status != "published" {
		return false
	}
	if actor.Role != "partner" && (course.Visibility == "public" || course.Visibility == "company") {
		return true
	}
	_, ok := assigned[course.ID]
	return ok
}

func authorizationActor(actor Actor) domainauth.Actor {
	return domainauth.Actor{
		UserID: domaincourse.ID(actor.UserID.String()), CompanyID: domaincourse.ID(actor.CompanyID.String()),
		Role: domainauth.Role(actor.Role),
	}
}

func authorizationCourse(value Course) domaincourse.Course {
	var ownerUserID *domaincourse.ID
	if value.OwnerUserID != nil {
		converted := domaincourse.ID(value.OwnerUserID.String())
		ownerUserID = &converted
	}
	return domaincourse.Course{
		ID: domaincourse.ID(value.ID.String()), CompanyID: domaincourse.ID(value.CompanyID.String()),
		OwnerType: domaincourse.OwnerType(value.OwnerType), OwnerUserID: ownerUserID,
		LifecycleStatus:    domaincourse.LifecycleStatus(value.LifecycleStatus),
		DistributionStatus: domaincourse.DistributionStatus(value.DistributionStatus),
	}
}

func canCreateCourse(actor Actor) bool {
	converted := authorizationActor(actor)
	return domainauth.CanCreateCompanyCourse(converted) || domainauth.CanCreatePartnerCourse(converted)
}

func canEditCourse(actor Actor, value Course) bool {
	convertedActor, convertedCourse := authorizationActor(actor), authorizationCourse(value)
	return domainauth.CanEditCompanyCourse(convertedActor, convertedCourse) ||
		domainauth.CanEditPartnerCourse(convertedActor, convertedCourse)
}

func canPublishCourse(actor Actor, value Course) bool {
	convertedActor, convertedCourse := authorizationActor(actor), authorizationCourse(value)
	return domainauth.CanPublishCompanyCourse(convertedActor, convertedCourse) ||
		domainauth.CanPublishPartnerCourse(convertedActor, convertedCourse)
}

func canAssignCourse(actor Actor, value Course) bool {
	return domainauth.CanAssignCompanyCourse(authorizationActor(actor), authorizationCourse(value))
}

func canArchiveCourse(actor Actor, value Course) bool {
	return domainauth.CanArchiveCourse(authorizationActor(actor), authorizationCourse(value))
}

func canDeleteCourse(actor Actor, value Course) bool {
	return domainauth.CanDeleteCourse(authorizationActor(actor), authorizationCourse(value))
}

func canViewEnrollment(actor Actor, enrollment Enrollment, course Course) bool {
	if actor.CompanyID != enrollment.CompanyID || course.CompanyID != enrollment.CompanyID {
		return false
	}
	if actor.canManage() {
		return true
	}
	if enrollment.LearnerType == "user" && enrollment.UserID != nil && *enrollment.UserID == actor.UserID {
		return true
	}
	return actor.Role == "partner" && course.OwnerType == "partner" &&
		course.OwnerUserID != nil && *course.OwnerUserID == actor.UserID
}

func canMutateEnrollment(actor Actor, enrollment Enrollment) bool {
	return actor.CompanyID == enrollment.CompanyID && enrollment.LearnerType == "user" &&
		enrollment.UserID != nil && *enrollment.UserID == actor.UserID
}

func (s *Service) requireCourseEditAccess(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	courseID uuid.UUID,
) (Course, error) {
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return Course{}, notFound("Курс")
		}
		return Course{}, internal("Не удалось проверить курс", err)
	}
	value := courseFromRow(row)
	if value.LifecycleStatus == "deleted" {
		return Course{}, conflict("Курс удалён")
	}
	if !canEditCourse(actor, value) {
		return Course{}, forbidden("Недостаточно прав для изменения этого курса")
	}
	return value, nil
}
