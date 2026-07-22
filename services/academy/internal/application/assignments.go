package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) GetAssignments(ctx context.Context, actor Actor) ([]Assignment, error) {
	queries := db.New(s.pool)
	if !canReadAcademy(actor) {
		return nil, forbidden("Недостаточно прав для просмотра академии")
	}
	if !actor.canManage() {
		rows, err := queries.GetUserAssignments(ctx, db.GetUserAssignmentsParams{
			CompanyID: actor.CompanyID, UserID: actor.UserID,
		})
		if err != nil {
			return nil, internal("Не удалось получить назначения", err)
		}
		result := make([]Assignment, len(rows))
		for index := range rows {
			result[index] = assignmentFromGetUserAssignmentsRow(rows[index])
		}
		return result, nil
	}
	rows, err := queries.GetAssignments(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить назначения", err)
	}
	result := make([]Assignment, len(rows))
	for index := range rows {
		result[index] = assignmentFromGetAssignmentsRow(rows[index])
	}
	return result, nil
}

type AssignCourseInput struct {
	CourseID        uuid.UUID
	CourseVersionID *uuid.UUID
	AssigneeType    string
	AssigneeID      *uuid.UUID
	DueDate         *time.Time
}

func (s *Service) AssignCourse(ctx context.Context, actor Actor, input AssignCourseInput) (Assignment, error) {
	switch input.AssigneeType {
	case "user", "position", "department":
		if input.AssigneeID == nil {
			return Assignment{}, validation("Укажите, кому назначается курс")
		}
	case "external":
		return Assignment{}, validation("Внешние назначения создаются через персональный доступ или кампанию")
	default:
		return Assignment{}, validation("Некорректный тип назначения")
	}

	course, err := db.New(s.pool).GetCourse(ctx, db.GetCourseParams{
		CompanyID: actor.CompanyID, ID: input.CourseID,
	})
	if err != nil {
		if isNoRows(err) {
			return Assignment{}, notFound("Курс")
		}
		return Assignment{}, internal("Не удалось проверить курс", err)
	}
	courseValue := courseFromRow(course)
	if !canAssignCourse(actor, courseValue) {
		return Assignment{}, forbidden("Недостаточно прав для назначения этого курса")
	}
	if courseValue.LatestPublishedVersionID == nil {
		return Assignment{}, validation("Назначить можно только опубликованную версию курса")
	}
	pinnedVersionID := courseValue.LatestPublishedVersionID
	if input.CourseVersionID != nil {
		version, versionErr := db.New(s.pool).GetCourseVersion(ctx, db.GetCourseVersionParams{
			CompanyID: actor.CompanyID, ID: *input.CourseVersionID,
		})
		if versionErr != nil || version.CourseID != input.CourseID || version.Status != "published" {
			if isNoRows(versionErr) || versionErr == nil {
				return Assignment{}, validation("Назначить можно только опубликованную версию этого курса")
			}
			return Assignment{}, internal("Не удалось проверить версию назначения", versionErr)
		}
		pinnedVersionID = input.CourseVersionID
	}
	resolvedUserIDs, err := s.resolveAssignees(ctx, actor, input.AssigneeType, input.AssigneeID)
	if err != nil {
		return Assignment{}, unavailable("Не удалось определить получателей курса", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Assignment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	var inviteToken *string
	row, err := queries.CreateAssignment(ctx, db.CreateAssignmentParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: input.CourseID,
		CourseVersionID: *pinnedVersionID,
		AssigneeType:    input.AssigneeType, AssigneeID: nullUUID(input.AssigneeID),
		InviteToken: nullText(inviteToken), DueDate: nullTimestamptz(input.DueDate),
		ResolvedUserIds: resolvedUserIDs, AssignedByID: actor.UserID,
		CreatedAt: s.now().UTC(),
	})
	created := true
	var assignment Assignment
	if isNoRows(err) {
		created = false
		existing, existingErr := queries.GetAssignmentByTarget(ctx, db.GetAssignmentByTargetParams{
			CompanyID: actor.CompanyID, CourseID: input.CourseID,
			AssigneeType: input.AssigneeType, AssigneeID: nullUUID(input.AssigneeID),
		})
		err = existingErr
		if err == nil {
			assignment = assignmentFromTargetRow(existing)
		}
	} else if err == nil {
		assignment = assignmentFromCreateRow(row)
	}
	if err != nil {
		return Assignment{}, internal("Не удалось создать назначение", err)
	}
	enrollmentIDs := make([]string, 0, len(resolvedUserIDs))
	for _, userID := range resolvedUserIDs {
		attemptNumber, numberErr := queries.GetNextUserCourseAttemptNumber(ctx, db.GetNextUserCourseAttemptNumberParams{
			CompanyID: actor.CompanyID, UserID: nullUUID(&userID), CourseID: input.CourseID,
		})
		if numberErr != nil {
			return Assignment{}, internal("Не удалось определить номер прохождения", numberErr)
		}
		enrollment, enrollmentErr := queries.CreateInternalEnrollmentForAssignment(ctx, db.CreateInternalEnrollmentForAssignmentParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, AssignmentID: assignment.ID,
			UserID: nullUUID(&userID), AttemptNumber: attemptNumber, CreatedAt: s.now().UTC(),
		})
		if enrollmentErr != nil {
			return Assignment{}, internal("Не удалось создать прохождение по назначению", enrollmentErr)
		}
		enrollmentIDs = append(enrollmentIDs, enrollment.ID.String())
	}
	if !created {
		if assignment.CourseVersionID == nil || *assignment.CourseVersionID != *pinnedVersionID ||
			!equalOptionalTime(assignment.DueDate, input.DueDate) {
			return Assignment{}, conflict("Назначение для этой цели уже существует с другими параметрами")
		}
		if err = tx.Commit(ctx); err != nil {
			return Assignment{}, internal("Не удалось получить назначение", err)
		}
		return assignment, nil
	}

	versionIDString := pinnedVersionID.String()
	payload := &eventsv1.AcademyCourseAssignedPayload{
		AssignmentId: assignment.ID.String(), CourseId: assignment.CourseID.String(),
		CourseTitle: course.Title, AssigneeType: assigneeTypeToEvent(assignment.AssigneeType),
		AssignedById: assignment.AssignedByID.String(), Link: academyLink,
		RecipientUserIds: uuidStrings(resolvedUserIDs),
		CourseVersionId:  &versionIDString, EnrollmentIds: enrollmentIDs,
	}
	if assignment.AssigneeID != nil {
		value := assignment.AssigneeID.String()
		payload.AssigneeId = &value
	}
	if assignment.InviteToken != nil {
		payload.InviteToken = assignment.InviteToken
	}
	if assignment.DueDate != nil {
		payload.DueDate = timestamppb.New(assignment.DueDate.UTC())
	}
	if err = s.emit(ctx, queries, actor.CompanyID, assignment.CourseID, actor.UserID, "teamos.academy.course.assigned.v1", payload); err != nil {
		return Assignment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Assignment{}, internal("Не удалось сохранить назначение", err)
	}
	return assignment, nil
}

func equalOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}

func (s *Service) RevokeAssignment(ctx context.Context, actor Actor, assignmentID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetAssignmentForUpdate(ctx, db.GetAssignmentForUpdateParams{
		CompanyID: actor.CompanyID, ID: assignmentID,
	})
	if err != nil {
		if isNoRows(err) {
			return notFound("Назначение")
		}
		return internal("Не удалось получить назначение", err)
	}
	courseRow, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: row.CourseID})
	if err != nil {
		return internal("Не удалось проверить курс назначения", err)
	}
	if !canAssignCourse(actor, courseFromRow(courseRow)) {
		return forbidden("Недостаточно прав для отзыва назначения")
	}
	now := s.now().UTC()
	if _, err = queries.RevokeAssignmentEnrollments(ctx, db.RevokeAssignmentEnrollmentsParams{
		RevokedAt: now, CompanyID: actor.CompanyID,
		AssignmentID: uuid.NullUUID{UUID: assignmentID, Valid: true},
	}); err != nil {
		return internal("Не удалось отозвать доступ по назначению", err)
	}
	affected, err := queries.RevokeAssignment(ctx, db.RevokeAssignmentParams{
		RevokedAt: nullTimestamptz(&now), RevokedByID: uuid.NullUUID{UUID: actor.UserID, Valid: true},
		CompanyID: actor.CompanyID, ID: assignmentID,
	})
	if err != nil || affected != 1 {
		return internal("Не удалось отозвать назначение", err)
	}
	if err = s.auditMutation(ctx, queries, actor, "assignment_revoked", "assignment", assignmentID,
		map[string]any{"accessStatus": "active", "courseId": row.CourseID},
		map[string]any{"accessStatus": "revoked", "courseId": row.CourseID, "revokedAt": now}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить отзыв назначения", err)
	}
	return nil
}

func (s *Service) resolveAssignees(
	ctx context.Context,
	actor Actor,
	assigneeType string,
	assigneeID *uuid.UUID,
) ([]uuid.UUID, error) {
	switch assigneeType {
	case "user":
		if assigneeID == nil || s.company == nil {
			return nil, errors.New("company client is unavailable")
		}
		if err := s.company.ValidateUser(ctx, actor.Token, *assigneeID); err != nil {
			return nil, err
		}
		return []uuid.UUID{*assigneeID}, nil
	case "position":
		if assigneeID == nil || s.company == nil {
			return nil, errors.New("company client is unavailable")
		}
		return s.company.ResolvePositionUsers(ctx, actor.Token, *assigneeID)
	case "department":
		if assigneeID == nil || s.company == nil {
			return nil, errors.New("company client is unavailable")
		}
		return s.company.ResolveDepartmentUsers(ctx, actor.Token, *assigneeID)
	}
	return []uuid.UUID{}, nil
}

func assigneeTypeToEvent(value string) eventsv1.CourseAssigneeType {
	switch value {
	case "user":
		return eventsv1.CourseAssigneeType_COURSE_ASSIGNEE_TYPE_USER
	case "position":
		return eventsv1.CourseAssigneeType_COURSE_ASSIGNEE_TYPE_POSITION
	case "department":
		return eventsv1.CourseAssigneeType_COURSE_ASSIGNEE_TYPE_DEPARTMENT
	case "external":
		return eventsv1.CourseAssigneeType_COURSE_ASSIGNEE_TYPE_EXTERNAL
	default:
		return eventsv1.CourseAssigneeType_COURSE_ASSIGNEE_TYPE_UNSPECIFIED
	}
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}
