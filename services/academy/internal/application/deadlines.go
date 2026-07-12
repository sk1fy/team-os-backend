package application

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const dueSoonWindow = 72 * time.Hour

// ProcessDeadlines emits course.due_soon once per assignment three days before
// the effective deadline (assignment dueDate or course deadlineDays, §10.4)
// and flags progress of expired assignments as overdue.
func (s *Service) ProcessDeadlines(ctx context.Context) error {
	now := s.now().UTC()

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	dueSoon, err := queries.GetDueSoonAssignments(ctx, now.Add(dueSoonWindow))
	if err != nil {
		return internal("Не удалось получить назначения с дедлайном", err)
	}
	for _, assignment := range dueSoon {
		if err = queries.MarkAssignmentDueSoonSent(ctx, db.MarkAssignmentDueSoonSentParams{
			ID: assignment.ID, DueSoonSentAt: nullTimestamptz(&now),
		}); err != nil {
			return internal("Не удалось отметить напоминание", err)
		}
		dueDate := assignment.EffectiveDueDate
		if !dueDate.Valid || dueDate.Time.Before(now) {
			// Already overdue: the overdue pass below covers it, a reminder
			// about the near deadline would be misleading.
			continue
		}
		payload := &eventsv1.AcademyCourseDueSoonPayload{
			AssignmentId: assignment.ID.String(), CourseId: assignment.CourseID.String(),
			CourseTitle: assignment.CourseTitle, DueDate: timestamppb.New(dueDate.Time.UTC()),
			RecipientUserIds: uuidStrings(assignment.ResolvedUserIds), Link: academyLink,
			AssigneeType: assigneeTypeToEvent(assignment.AssigneeType),
		}
		if assignment.AssigneeID.Valid {
			value := assignment.AssigneeID.UUID.String()
			payload.AssigneeId = &value
		}
		if err = s.emit(ctx, queries, assignment.CompanyID, assignment.CourseID, assignment.AssignedByID,
			"teamos.academy.course.due_soon.v1", payload); err != nil {
			return err
		}
	}

	overdue, err := queries.GetOverdueAssignments(ctx, now)
	if err != nil {
		return internal("Не удалось получить просроченные назначения", err)
	}
	for _, assignment := range overdue {
		if len(assignment.ResolvedUserIds) == 0 {
			continue
		}
		if err = queries.InsertOverdueProgress(ctx, db.InsertOverdueProgressParams{
			CompanyID: assignment.CompanyID, CourseID: assignment.CourseID,
			UserIds: assignment.ResolvedUserIds,
		}); err != nil {
			return internal("Не удалось создать просроченный прогресс", err)
		}
		if _, err = queries.MarkProgressOverdue(ctx, db.MarkProgressOverdueParams{
			CompanyID: assignment.CompanyID, CourseID: assignment.CourseID,
			UserIds: assignment.ResolvedUserIds,
		}); err != nil {
			return internal("Не удалось отметить просроченный прогресс", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить обработку дедлайнов", err)
	}
	return nil
}
