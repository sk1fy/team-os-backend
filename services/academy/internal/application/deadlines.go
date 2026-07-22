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
// External expirations are materialized in the new enrollment model. Internal
// overdue state is derived from assignment deadlines by reports and is no
// longer written to the legacy progress projection.
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

	companyIDs, err := queries.ListExternalMaintenanceCompanyIDs(ctx)
	if err != nil {
		return internal("Не удалось получить компании для обработки внешних сроков", err)
	}
	for _, companyID := range companyIDs {
		for batch := 0; batch < 10; batch++ {
			expired, expireErr := queries.MaterializeExpiredExternalEnrollments(ctx, db.MaterializeExpiredExternalEnrollmentsParams{
				Now: now, CompanyID: companyID, BatchSize: 100,
			})
			if expireErr != nil {
				return internal("Не удалось обновить истёкшие внешние прохождения", expireErr)
			}
			if len(expired) < 100 {
				break
			}
		}
		for batch := 0; batch < 10; batch++ {
			expired, expireErr := queries.MaterializeExpiredExternalChallenges(ctx, db.MaterializeExpiredExternalChallengesParams{
				CompanyID: companyID, Now: now, BatchSize: 100,
			})
			if expireErr != nil {
				return internal("Не удалось очистить истёкшие подтверждения", expireErr)
			}
			if len(expired) < 100 {
				break
			}
		}
	}
	campaigns, err := queries.ListExternalCampaignIDsForAnalyticsMaintenance(ctx)
	if err != nil {
		return internal("Не удалось получить кампании для агрегации", err)
	}
	dayStart := now.Truncate(24 * time.Hour)
	for _, campaign := range campaigns {
		if _, err = queries.RebuildExternalCampaignFunnelDaily(ctx, db.RebuildExternalCampaignFunnelDailyParams{
			CompanyID: campaign.CompanyID, CampaignID: campaign.CampaignID,
			FromTime: dayStart.Add(-24 * time.Hour), ToTime: dayStart.Add(24 * time.Hour), AggregatedAt: now,
		}); err != nil {
			return internal("Не удалось обновить воронку кампании", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить обработку дедлайнов", err)
	}
	return nil
}
