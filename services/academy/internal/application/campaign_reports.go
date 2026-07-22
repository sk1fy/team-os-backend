package application

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/academy/internal/domain/authorization"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) GetExternalCampaignReport(
	ctx context.Context,
	actor Actor,
	campaignID uuid.UUID,
) (ExternalCampaignReport, error) {
	campaign, err := s.GetExternalCampaign(ctx, actor, campaignID)
	if err != nil {
		return ExternalCampaignReport{}, err
	}
	if !domainauth.CanViewExternalCampaignReport(authorizationActor(actor), campaignDomainSnapshot(campaign, nil)) {
		return ExternalCampaignReport{}, notFound("Кампания")
	}
	partnerID, err := campaignPartnerScope(actor)
	if err != nil {
		return ExternalCampaignReport{}, err
	}
	queries := db.New(s.pool)
	from, to := time.Unix(0, 0).UTC(), s.now().UTC().Add(time.Nanosecond)
	row, err := queries.GetExternalCampaignAnalyticsReport(ctx, db.GetExternalCampaignAnalyticsReportParams{
		FromTime: from, ToTime: to, CompanyID: actor.CompanyID, CampaignID: campaignID,
		PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		if isNoRows(err) {
			return ExternalCampaignReport{}, notFound("Кампания")
		}
		return ExternalCampaignReport{}, internal("Не удалось построить отчёт кампании", err)
	}
	enrollmentRows, err := queries.ListScopedExternalEnrollmentsForReport(ctx, db.ListScopedExternalEnrollmentsForReportParams{
		CompanyID: actor.CompanyID, CampaignID: nullUUID(&campaignID), PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return ExternalCampaignReport{}, internal("Не удалось получить прохождения кампании", err)
	}
	attributionRows, err := queries.ListExternalCampaignUTMReport(ctx, db.ListExternalCampaignUTMReportParams{
		CompanyID: actor.CompanyID, CampaignID: campaignID, FromTime: from, ToTime: to,
		PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return ExternalCampaignReport{}, internal("Не удалось получить источники кампании", err)
	}
	dropOffRows, err := queries.ListExternalCampaignLessonDropOffReport(ctx, db.ListExternalCampaignLessonDropOffReportParams{
		CompanyID: actor.CompanyID, CampaignID: campaignID, PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return ExternalCampaignReport{}, internal("Не удалось получить отток по урокам", err)
	}
	averageProgress := numericFloat64(row.AverageEnrollmentProgress)
	medianProgress := numericFloat64(row.MedianEnrollmentProgress)
	averageCompletion := numericInt64Pointer(row.AverageCompletionSeconds)
	medianCompletion := numericInt64Pointer(row.MedianCompletionSeconds)
	report := ExternalCampaignReport{
		Campaign: campaign,
		Funnel: CampaignFunnel{
			Views: row.Views, UniqueVisitors: row.UniqueVisitors, FormSubmits: row.FormSubmits,
			VerifiedEmails: row.VerifiedEmails, Activations: row.Activations,
			Completions: row.CompletedEnrollmentCount,
		},
		Enrollments: scopedEnrollmentsFromRows(enrollmentRows),
		Analytics: CampaignAnalytics{
			FirstLessonStarts: row.FirstLessonStarts, LessonCompletions: row.LessonCompletions,
			QuizSubmissions: row.QuizSubmissions, ExpiredEnrollments: row.ExpiredEnrollmentCount,
			ReturnVisits: row.ReturnVisits, AverageProgressPercent: averageProgress,
			MedianProgressPercent: medianProgress, AverageCompletionSecs: averageCompletion,
			MedianCompletionSecs: medianCompletion, LessonDropOff: campaignDropOffFromRows(dropOffRows),
			Attribution: campaignAttributionFromRows(attributionRows),
			Versions: []CampaignVersionAnalytics{{
				CourseVersionID: campaign.CourseVersionID, VersionNumber: row.CourseVersionNumber,
				Activations: row.Activations, Completions: row.CompletedEnrollmentCount,
				AverageProgressPercent: averageProgress,
			}},
		},
	}
	return report, nil
}

func (s *Service) GetCourseExternalReport(
	ctx context.Context,
	actor Actor,
	courseID uuid.UUID,
) (CourseExternalReport, error) {
	queries := db.New(s.pool)
	row, err := queries.GetCourse(ctx, db.GetCourseParams{CompanyID: actor.CompanyID, ID: courseID})
	if err != nil {
		if isNoRows(err) {
			return CourseExternalReport{}, notFound("Курс")
		}
		return CourseExternalReport{}, internal("Не удалось проверить курс", err)
	}
	course := courseFromRow(row)
	if !domainauth.CanViewEnrollmentReport(authorizationActor(actor), authorizationCourse(course)) {
		return CourseExternalReport{}, notFound("Курс")
	}
	partnerID, err := campaignPartnerScope(actor)
	if err != nil {
		return CourseExternalReport{}, err
	}
	rows, err := queries.ListScopedExternalEnrollmentsForReport(ctx, db.ListScopedExternalEnrollmentsForReportParams{
		CompanyID: actor.CompanyID, CourseID: nullUUID(&courseID), PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return CourseExternalReport{}, internal("Не удалось построить внешний отчёт курса", err)
	}
	return CourseExternalReport{CourseID: courseID, Enrollments: scopedEnrollmentsFromRows(rows)}, nil
}

func (s *Service) GetExternalLearnerTimeline(
	ctx context.Context,
	actor Actor,
	learnerID uuid.UUID,
) (ExternalLearnerTimeline, error) {
	learner, err := s.GetExternalLearner(ctx, actor, learnerID)
	if err != nil {
		return ExternalLearnerTimeline{}, err
	}
	partnerID, err := externalReportPartnerFilter(actor)
	if err != nil {
		return ExternalLearnerTimeline{}, err
	}
	rows, err := db.New(s.pool).ListExternalLearnerTimeline(ctx, db.ListExternalLearnerTimelineParams{
		CompanyID: actor.CompanyID, ExternalLearnerID: nullUUID(&learnerID), PartnerOwnerID: nullUUID(partnerID),
	})
	if err != nil {
		return ExternalLearnerTimeline{}, internal("Не удалось получить хронологию внешнего ученика", err)
	}
	events := make([]ExternalLearnerTimelineEvent, 0, len(rows)*3)
	for _, row := range rows {
		base := ExternalLearnerTimelineEvent{
			CourseID: &row.CourseID, CourseVersionID: &row.CourseVersionID, EnrollmentID: &row.EnrollmentID,
			SourceType: &row.SourceType, SourceID: nullUUIDPointer(row.SourceID),
			CourseTitle: interfaceTextPointer(row.CourseTitle), VersionNumber: &row.CourseVersionNumber,
			ProgressPercent: &row.ProgressPercent, AccessStatus: &row.AccessStatus,
			DeletedCourse: row.CourseLifecycleStatus == "deleted",
		}
		events = append(events, timelineEvent(base, row.EnrollmentID, "access_issued", row.CreatedAt))
		if row.ActivatedAt.Valid {
			events = append(events, timelineEvent(base, row.EnrollmentID, "activated", row.ActivatedAt.Time))
		}
		if row.CompletedAt.Valid {
			events = append(events, timelineEvent(base, row.EnrollmentID, "course_completed", row.CompletedAt.Time))
		}
		if row.AccessStatus == "expired" && row.AccessUntil.Valid {
			events = append(events, timelineEvent(base, row.EnrollmentID, "deadline_expired", row.AccessUntil.Time))
		}
		if row.AccessStatus == "frozen" && row.CourseArchivedAt.Valid {
			events = append(events, timelineEvent(base, row.EnrollmentID, "frozen", row.CourseArchivedAt.Time))
		}
	}
	sort.Slice(events, func(left, right int) bool { return events[left].OccurredAt.After(events[right].OccurredAt) })
	return ExternalLearnerTimeline{Learner: learner, Events: events}, nil
}

func scopedEnrollmentsFromRows(rows []db.ListScopedExternalEnrollmentsForReportRow) []Enrollment {
	result := make([]Enrollment, len(rows))
	for index, row := range rows {
		result[index] = Enrollment{
			ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
			VersionNumber: row.CourseVersionNumber, LearnerType: "external",
			ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID), SourceType: row.SourceType,
			SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
			ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
			CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ProgressPercent: row.ProgressPercent,
			ActivatedAt: timestamptzPointer(row.ActivatedAt), AccessUntil: timestamptzPointer(row.AccessUntil),
			StartedAt: timestamptzPointer(row.StartedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
			LastActivityAt: timestamptzPointer(row.LastActivityAt), FrozenAt: timestamptzPointer(row.FrozenAt),
			SuspendedAt: timestamptzPointer(row.SuspendedAt), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	return result
}

func campaignAttributionFromRows(rows []db.ListExternalCampaignUTMReportRow) []CampaignAttribution {
	result := make([]CampaignAttribution, len(rows))
	for index, row := range rows {
		result[index] = CampaignAttribution{
			UTMSource: nonEmptyStringPointer(row.UtmSource), UTMMedium: nonEmptyStringPointer(row.UtmMedium),
			UTMCampaign: nonEmptyStringPointer(row.UtmCampaign), UTMContent: nonEmptyStringPointer(row.UtmContent),
			UTMTerm: nonEmptyStringPointer(row.UtmTerm), Referrer: nonEmptyStringPointer(row.Referrer),
			Visits: row.Views, Activations: row.Activations, Completions: row.Completions,
		}
	}
	return result
}

func campaignDropOffFromRows(rows []db.ListExternalCampaignLessonDropOffReportRow) []CampaignLessonDropOff {
	result := make([]CampaignLessonDropOff, len(rows))
	for index, row := range rows {
		result[index] = CampaignLessonDropOff{
			LessonVersionID: row.LessonVersionID, Reached: row.Reached, Completed: row.Completed,
		}
	}
	return result
}

func timelineEvent(base ExternalLearnerTimelineEvent, enrollmentID uuid.UUID, eventType string, occurredAt time.Time) ExternalLearnerTimelineEvent {
	base.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(enrollmentID.String()+"\x00"+eventType+"\x00"+occurredAt.UTC().Format(time.RFC3339Nano)))
	base.Type = eventType
	base.OccurredAt = occurredAt.UTC()
	return base
}

func numericFloat64(value pgtype.Numeric) float64 {
	converted, err := value.Float64Value()
	if err != nil || !converted.Valid {
		return 0
	}
	return converted.Float64
}

func numericInt64Pointer(value pgtype.Numeric) *int64 {
	converted := numericFloat64(value)
	if converted <= 0 {
		return nil
	}
	result := int64(converted)
	return &result
}

func nonEmptyStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
