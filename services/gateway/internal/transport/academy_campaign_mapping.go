package transport

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func externalCampaignFromProto(value *academyv1.ExternalCampaign) (api.ExternalCampaign, error) {
	if value == nil || value.GetCreatedAt() == nil || !value.GetCreatedAt().IsValid() {
		return api.ExternalCampaign{}, errors.New("academy returned invalid external campaign")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	versionID, err := uuid.Parse(value.GetCourseVersionId())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	creatorID, err := uuid.Parse(value.GetCreatedById())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	ownerType, err := externalCampaignOwnerTypeFromProto(value.GetOwnerType())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	purpose, err := externalCampaignPurposeFromProto(value.GetPurpose())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	status, err := externalCampaignStatusFromProto(value.GetStatus())
	if err != nil {
		return api.ExternalCampaign{}, err
	}
	result := api.ExternalCampaign{
		Id: id, CompanyId: companyID, CourseId: courseID, CourseVersionId: versionID,
		OwnerType: ownerType, Purpose: purpose, Name: value.GetName(), DeadlineDays: int(value.GetDeadlineDays()),
		Status: status, TokenPrefix: value.GetTokenPrefix(), CreatedById: creatorID,
		CreatedAt: value.GetCreatedAt().AsTime(), PausedAt: protoTimestampPointer(value.GetPausedAt()),
		RevokedAt: protoTimestampPointer(value.GetRevokedAt()),
	}
	if result.OwnerUserId, err = parseOptionalUUIDString(value.GetOwnerUserId()); err != nil {
		return api.ExternalCampaign{}, err
	}
	return result, nil
}

func externalCampaignsFromProto(values []*academyv1.ExternalCampaign) ([]api.ExternalCampaign, error) {
	result := make([]api.ExternalCampaign, len(values))
	for index, value := range values {
		converted, err := externalCampaignFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func externalCampaignCreatedFromProto(value *academyv1.ExternalCampaignCreated) (api.ExternalCampaignCreated, error) {
	if value == nil || value.GetToken() == "" {
		return api.ExternalCampaignCreated{}, errors.New("academy returned empty external campaign token")
	}
	campaign, err := externalCampaignFromProto(value.GetCampaign())
	if err != nil {
		return api.ExternalCampaignCreated{}, err
	}
	return api.ExternalCampaignCreated{Campaign: campaign, Token: value.GetToken()}, nil
}

func campaignReportFromProto(value *academyv1.ExternalCampaignReport) (api.CampaignReport, error) {
	if value == nil || value.GetFunnel() == nil {
		return api.CampaignReport{}, errors.New("academy returned invalid campaign report")
	}
	campaign, err := externalCampaignFromProto(value.GetCampaign())
	if err != nil {
		return api.CampaignReport{}, err
	}
	enrollments, err := externalEnrollmentsFromProto(value.GetEnrollments())
	if err != nil {
		return api.CampaignReport{}, err
	}
	funnel := value.GetFunnel()
	result := api.CampaignReport{
		Campaign: campaign, Enrollments: enrollments,
		Funnel: api.CampaignFunnel{
			Views: int(funnel.GetViews()), UniqueVisitors: int(funnel.GetUniqueVisitors()),
			FormSubmits: int(funnel.GetFormSubmits()), VerifiedEmails: int(funnel.GetVerifiedEmails()),
			Activations: int(funnel.GetActivations()), Completions: int(funnel.GetCompletions()),
		},
	}
	if value.GetAnalytics() != nil {
		analytics, convertErr := campaignAnalyticsFromProto(value.GetAnalytics())
		if convertErr != nil {
			return api.CampaignReport{}, convertErr
		}
		result.Analytics = &analytics
	}
	return result, nil
}

func campaignAnalyticsFromProto(value *academyv1.CampaignAnalytics) (api.CampaignAnalytics, error) {
	result := api.CampaignAnalytics{
		FirstLessonStarts: int(value.GetFirstLessonStarts()), LessonCompletions: int(value.GetLessonCompletions()),
		QuizSubmissions: int(value.GetQuizSubmissions()), ExpiredEnrollments: int(value.GetExpiredEnrollments()),
		ReturnVisits: int(value.GetReturnVisits()), AverageProgressPercent: value.GetAverageProgressPercent(),
		MedianProgressPercent: value.GetMedianProgressPercent(),
		LessonDropOff:         make([]api.CampaignLessonDropOff, len(value.GetLessonDropOff())),
		Attribution:           make([]api.CampaignAttribution, len(value.GetAttribution())),
		Versions:              make([]api.CampaignVersionAnalytics, len(value.GetVersions())),
	}
	if value.AverageCompletionSeconds != nil {
		seconds := int(value.GetAverageCompletionSeconds())
		result.AverageCompletionSeconds = &seconds
	}
	if value.MedianCompletionSeconds != nil {
		seconds := int(value.GetMedianCompletionSeconds())
		result.MedianCompletionSeconds = &seconds
	}
	for index, item := range value.GetLessonDropOff() {
		lessonID, err := uuid.Parse(item.GetLessonVersionId())
		if err != nil {
			return api.CampaignAnalytics{}, err
		}
		result.LessonDropOff[index] = api.CampaignLessonDropOff{
			LessonVersionId: lessonID, Reached: int(item.GetReached()), Completed: int(item.GetCompleted()),
		}
	}
	for index, item := range value.GetAttribution() {
		result.Attribution[index] = api.CampaignAttribution{
			UtmSource: item.UtmSource, UtmMedium: item.UtmMedium, UtmCampaign: item.UtmCampaign,
			UtmContent: item.UtmContent, UtmTerm: item.UtmTerm, Referrer: item.Referrer,
			Visits: int(item.GetVisits()), Activations: int(item.GetActivations()),
			Completions: int(item.GetCompletions()),
		}
	}
	for index, item := range value.GetVersions() {
		versionID, err := uuid.Parse(item.GetCourseVersionId())
		if err != nil {
			return api.CampaignAnalytics{}, err
		}
		result.Versions[index] = api.CampaignVersionAnalytics{
			CourseVersionId: versionID, VersionNumber: int(item.GetVersionNumber()),
			Activations: int(item.GetActivations()), Completions: int(item.GetCompletions()),
			AverageProgressPercent: item.GetAverageProgressPercent(),
		}
	}
	return result, nil
}

func courseExternalReportFromProto(value *academyv1.CourseExternalReport) (api.CourseExternalReport, error) {
	if value == nil {
		return api.CourseExternalReport{}, errors.New("academy returned empty course external report")
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.CourseExternalReport{}, err
	}
	enrollments, err := externalEnrollmentsFromProto(value.GetEnrollments())
	if err != nil {
		return api.CourseExternalReport{}, err
	}
	return api.CourseExternalReport{CourseId: courseID, Enrollments: enrollments}, nil
}

func courseExternalReportsFromProto(values []*academyv1.CourseExternalReport) ([]api.CourseExternalReport, error) {
	result := make([]api.CourseExternalReport, len(values))
	for index, value := range values {
		converted, err := courseExternalReportFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func externalLearnerTimelineFromProto(value *academyv1.ExternalLearnerTimeline) (api.ExternalLearnerTimeline, error) {
	if value == nil {
		return api.ExternalLearnerTimeline{}, errors.New("academy returned empty external learner timeline")
	}
	learner, err := externalLearnerFromProto(value.GetLearner())
	if err != nil {
		return api.ExternalLearnerTimeline{}, err
	}
	events := make([]api.ExternalLearnerTimelineEvent, len(value.GetEvents()))
	for index, event := range value.GetEvents() {
		converted, convertErr := externalLearnerTimelineEventFromProto(event)
		if convertErr != nil {
			return api.ExternalLearnerTimeline{}, convertErr
		}
		events[index] = converted
	}
	return api.ExternalLearnerTimeline{Learner: learner, Events: events}, nil
}

func externalLearnerTimelineEventFromProto(value *academyv1.ExternalLearnerTimelineEvent) (api.ExternalLearnerTimelineEvent, error) {
	if value == nil || value.GetOccurredAt() == nil || !value.GetOccurredAt().IsValid() {
		return api.ExternalLearnerTimelineEvent{}, errors.New("academy returned invalid external learner timeline event")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ExternalLearnerTimelineEvent{}, err
	}
	eventType, err := externalLearnerTimelineEventTypeFromProto(value.GetType())
	if err != nil {
		return api.ExternalLearnerTimelineEvent{}, err
	}
	result := api.ExternalLearnerTimelineEvent{
		Id: id, Type: eventType, OccurredAt: value.GetOccurredAt().AsTime(), DeletedCourse: value.GetDeletedCourse(),
		CourseTitle: value.CourseTitle,
	}
	if result.CourseId, err = parseOptionalUUIDString(value.GetCourseId()); err != nil {
		return api.ExternalLearnerTimelineEvent{}, err
	}
	if result.CourseVersionId, err = parseOptionalUUIDString(value.GetCourseVersionId()); err != nil {
		return api.ExternalLearnerTimelineEvent{}, err
	}
	if result.EnrollmentId, err = parseOptionalUUIDString(value.GetEnrollmentId()); err != nil {
		return api.ExternalLearnerTimelineEvent{}, err
	}
	if result.SourceId, err = parseOptionalUUIDString(value.GetSourceId()); err != nil {
		return api.ExternalLearnerTimelineEvent{}, err
	}
	if value.SourceType != nil {
		sourceType := enrollmentSourceTypeFromProto(value.GetSourceType())
		result.SourceType = &sourceType
	}
	if value.VersionNumber != nil {
		number := int(value.GetVersionNumber())
		result.VersionNumber = &number
	}
	if value.ProgressPercent != nil {
		progress := int(value.GetProgressPercent())
		result.ProgressPercent = &progress
	}
	if value.AccessStatus != nil {
		status := enrollmentAccessStatusFromProto(value.GetAccessStatus())
		result.AccessStatus = &status
	}
	return result, nil
}

func externalCampaignPurposeToProto(value api.ExternalCampaignPurpose) (academyv1.ExternalCampaignPurpose, error) {
	switch value {
	case "company_candidate":
		return academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_COMPANY_CANDIDATE, nil
	case "partner_promo":
		return academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_PARTNER_PROMO, nil
	default:
		return academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_UNSPECIFIED, fmt.Errorf("unknown campaign purpose %q", value)
	}
}

func externalCampaignOwnerTypeFromProto(value academyv1.ExternalCampaignOwnerType) (api.ExternalCampaignOwnerType, error) {
	switch value {
	case academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_COMPANY:
		return "company", nil
	case academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_PARTNER:
		return "partner", nil
	default:
		return "", fmt.Errorf("unknown campaign owner type %d", value)
	}
}

func externalCampaignPurposeFromProto(value academyv1.ExternalCampaignPurpose) (api.ExternalCampaignPurpose, error) {
	switch value {
	case academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_COMPANY_CANDIDATE:
		return "company_candidate", nil
	case academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_PARTNER_PROMO:
		return "partner_promo", nil
	default:
		return "", fmt.Errorf("unknown campaign purpose %d", value)
	}
}

func externalCampaignStatusFromProto(value academyv1.ExternalCampaignStatus) (api.ExternalCampaignStatus, error) {
	switch value {
	case academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_ACTIVE:
		return "active", nil
	case academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_PAUSED:
		return "paused", nil
	case academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_REVOKED:
		return "revoked", nil
	case academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_CLOSED:
		return "closed", nil
	default:
		return "", fmt.Errorf("unknown campaign status %d", value)
	}
}

func externalLearnerTimelineEventTypeFromProto(value academyv1.ExternalLearnerTimelineEventType) (api.ExternalLearnerTimelineEventType, error) {
	values := map[academyv1.ExternalLearnerTimelineEventType]api.ExternalLearnerTimelineEventType{
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACCESS_ISSUED:          "access_issued",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_VERIFICATION_REQUESTED: "verification_requested",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_EMAIL_VERIFIED:         "email_verified",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACTIVATED:              "activated",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_LESSON_COMPLETED:       "lesson_completed",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_QUIZ_SUBMITTED:         "quiz_submitted",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_COURSE_COMPLETED:       "course_completed",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_DEADLINE_EXPIRED:       "deadline_expired",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACCESS_EXTENDED:        "access_extended",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_TOKEN_ROTATED:          "token_rotated",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACCESS_REVOKED:         "access_revoked",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_FROZEN:                 "frozen",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_SUSPENDED:              "suspended",
		academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_REPEAT_CREATED:         "repeat_created",
	}
	result, ok := values[value]
	if !ok {
		return "", fmt.Errorf("unknown external timeline event type %d", value)
	}
	return result, nil
}
