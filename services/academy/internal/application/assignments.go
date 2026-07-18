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
	if actor.Role == "partner" {
		rows, err := queries.GetUserAssignments(ctx, db.GetUserAssignmentsParams{
			CompanyID: actor.CompanyID, UserID: actor.UserID,
		})
		if err != nil {
			return nil, internal("Не удалось получить назначения", err)
		}
		return assignmentsFromRows(rows), nil
	}
	rows, err := queries.GetAssignments(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить назначения", err)
	}
	return assignmentsFromRows(rows), nil
}

type AssignCourseInput struct {
	CourseID     uuid.UUID
	AssigneeType string
	AssigneeID   *uuid.UUID
	DueDate      *time.Time
}

func (s *Service) AssignCourse(ctx context.Context, actor Actor, input AssignCourseInput) (Assignment, error) {
	if !actor.canManage() {
		return Assignment{}, forbidden("Недостаточно прав для изменения академии")
	}
	switch input.AssigneeType {
	case "user", "position", "department":
		if input.AssigneeID == nil {
			return Assignment{}, validation("Укажите, кому назначается курс")
		}
	case "external":
		if input.AssigneeID != nil {
			return Assignment{}, validation("Для внешнего назначения не указывайте получателя")
		}
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
	if input.AssigneeType == "external" {
		token := uuid.NewString()
		inviteToken = &token
	}
	row, err := queries.CreateAssignment(ctx, db.CreateAssignmentParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, CourseID: input.CourseID,
		AssigneeType: input.AssigneeType, AssigneeID: nullUUID(input.AssigneeID),
		InviteToken: nullText(inviteToken), DueDate: nullTimestamptz(input.DueDate),
		ResolvedUserIds: resolvedUserIDs, AssignedByID: actor.UserID,
		CreatedAt: s.now().UTC(),
	})
	created := true
	if isNoRows(err) {
		created = false
		row, err = queries.GetAssignmentByTarget(ctx, db.GetAssignmentByTargetParams{
			CompanyID: actor.CompanyID, CourseID: input.CourseID,
			AssigneeType: input.AssigneeType, AssigneeID: nullUUID(input.AssigneeID),
		})
	}
	if err != nil {
		return Assignment{}, internal("Не удалось создать назначение", err)
	}
	assignment := assignmentFromRow(row)
	if !created {
		if err = tx.Commit(ctx); err != nil {
			return Assignment{}, internal("Не удалось получить назначение", err)
		}
		return assignment, nil
	}

	payload := &eventsv1.AcademyCourseAssignedPayload{
		AssignmentId: assignment.ID.String(), CourseId: assignment.CourseID.String(),
		CourseTitle: course.Title, AssigneeType: assigneeTypeToEvent(assignment.AssigneeType),
		AssignedById: assignment.AssignedByID.String(), Link: academyLink,
		RecipientUserIds: uuidStrings(resolvedUserIDs),
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
