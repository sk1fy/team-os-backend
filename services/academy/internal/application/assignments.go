package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetAssignments(ctx context.Context, actor Actor) ([]Assignment, error) {
	queries := db.New(s.pool)
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
	default:
		return Assignment{}, validation("Некорректный тип назначения")
	}

	// The user list is resolved once, at assignment time (§9). A company outage
	// degrades to an empty snapshot instead of failing the assignment.
	resolvedUserIDs := s.resolveAssignees(ctx, actor, input.AssigneeType, input.AssigneeID)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Assignment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	course, err := queries.GetCourse(ctx, db.GetCourseParams{
		CompanyID: actor.CompanyID, ID: input.CourseID,
	})
	if err != nil {
		if isNoRows(err) {
			return Assignment{}, notFound("Курс")
		}
		return Assignment{}, internal("Не удалось проверить курс", err)
	}

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
	if err != nil {
		return Assignment{}, internal("Не удалось создать назначение", err)
	}
	assignment := assignmentFromRow(row)

	payload := map[string]any{
		"assignmentId": assignment.ID.String(),
		"courseId":     assignment.CourseID.String(),
		"courseTitle":  course.Title,
		"assigneeType": assignment.AssigneeType,
		"assignedById": assignment.AssignedByID.String(),
		"link":         academyLink,
		// Snapshot for consumers that must not call company back (§10.1).
		"recipientUserIds": uuidStrings(resolvedUserIDs),
	}
	if assignment.AssigneeID != nil {
		payload["assigneeId"] = assignment.AssigneeID.String()
	}
	if assignment.InviteToken != nil {
		payload["inviteToken"] = *assignment.InviteToken
	}
	if assignment.DueDate != nil {
		payload["dueDate"] = assignment.DueDate.UTC().Format(time.RFC3339Nano)
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.academy.course.assigned.v1", payload); err != nil {
		return Assignment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Assignment{}, internal("Не удалось сохранить назначение", err)
	}
	return assignment, nil
}

func (s *Service) resolveAssignees(ctx context.Context, actor Actor, assigneeType string, assigneeID *uuid.UUID) []uuid.UUID {
	switch assigneeType {
	case "user":
		if assigneeID != nil {
			return []uuid.UUID{*assigneeID}
		}
	case "position":
		if assigneeID != nil && s.company != nil {
			userIDs, err := s.company.ResolvePositionUsers(ctx, actor.Token, *assigneeID)
			if err == nil {
				return userIDs
			}
			s.logger.WarnContext(ctx, "position resolve degraded to empty snapshot", "error", err)
		}
	case "department":
		if assigneeID != nil && s.company != nil {
			userIDs, err := s.company.ResolveDepartmentUsers(ctx, actor.Token, *assigneeID)
			if err == nil {
				return userIDs
			}
			s.logger.WarnContext(ctx, "department resolve degraded to empty snapshot", "error", err)
		}
	}
	return []uuid.UUID{}
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}
