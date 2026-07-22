package grpc

import (
	"time"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func externalCampaignPurposeFromProto(value academyv1.ExternalCampaignPurpose) string {
	if value == academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_PARTNER_PROMO {
		return "partner_promo"
	}
	if value == academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_COMPANY_CANDIDATE {
		return "company_candidate"
	}
	return ""
}

func externalCampaignToProto(value application.ExternalCampaign) *academyv1.ExternalCampaign {
	result := &academyv1.ExternalCampaign{
		Id: value.ID.String(), CompanyId: value.CompanyID.String(), CourseId: value.CourseID.String(),
		CourseVersionId: value.CourseVersionID.String(), OwnerType: externalCampaignOwnerTypeToProto(value.OwnerType),
		OwnerUserId: optionalUUIDString(value.OwnerUserID), Purpose: externalCampaignPurposeToProto(value.Purpose),
		Name: value.Name, DeadlineDays: uint32(max(0, value.DeadlineDays)),
		Status: externalCampaignStatusToProto(value.Status), TokenPrefix: value.TokenPrefix,
		CreatedById: value.CreatedByID.String(), CreatedAt: timestamppb.New(value.CreatedAt),
	}
	result.PausedAt = optionalTimestamp(value.PausedAt)
	result.RevokedAt = optionalTimestamp(value.RevokedAt)
	return result
}

func externalCampaignsToProto(values []application.ExternalCampaign) []*academyv1.ExternalCampaign {
	result := make([]*academyv1.ExternalCampaign, len(values))
	for index := range values {
		result[index] = externalCampaignToProto(values[index])
	}
	return result
}

func externalCampaignCreatedToProto(value application.ExternalCampaignCreated) *academyv1.ExternalCampaignCreated {
	return &academyv1.ExternalCampaignCreated{Campaign: externalCampaignToProto(value.Campaign), Token: value.Token}
}

func externalCampaignOwnerTypeToProto(value string) academyv1.ExternalCampaignOwnerType {
	if value == "company" {
		return academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_COMPANY
	}
	if value == "partner" {
		return academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_PARTNER
	}
	return academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_UNSPECIFIED
}

func externalCampaignPurposeToProto(value string) academyv1.ExternalCampaignPurpose {
	if value == "company_candidate" {
		return academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_COMPANY_CANDIDATE
	}
	if value == "partner_promo" {
		return academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_PARTNER_PROMO
	}
	return academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_UNSPECIFIED
}

func externalCampaignStatusToProto(value string) academyv1.ExternalCampaignStatus {
	switch value {
	case "active":
		return academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_ACTIVE
	case "paused":
		return academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_PAUSED
	case "revoked":
		return academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_REVOKED
	case "closed":
		return academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_CLOSED
	default:
		return academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_UNSPECIFIED
	}
}

func externalCampaignReportToProto(value application.ExternalCampaignReport) *academyv1.ExternalCampaignReport {
	enrollments := make([]*academyv1.CourseEnrollment, len(value.Enrollments))
	for index := range value.Enrollments {
		enrollments[index] = enrollmentToProto(value.Enrollments[index])
	}
	return &academyv1.ExternalCampaignReport{
		Campaign: externalCampaignToProto(value.Campaign), Funnel: campaignFunnelToProto(value.Funnel),
		Enrollments: enrollments, Analytics: campaignAnalyticsToProto(value.Analytics),
	}
}

func campaignFunnelToProto(value application.CampaignFunnel) *academyv1.CampaignFunnel {
	return &academyv1.CampaignFunnel{
		Views: uint64(max(0, value.Views)), UniqueVisitors: uint64(max(0, value.UniqueVisitors)),
		FormSubmits: uint64(max(0, value.FormSubmits)), VerifiedEmails: uint64(max(0, value.VerifiedEmails)),
		Activations: uint64(max(0, value.Activations)), Completions: uint64(max(0, value.Completions)),
	}
}

func campaignAnalyticsToProto(value application.CampaignAnalytics) *academyv1.CampaignAnalytics {
	result := &academyv1.CampaignAnalytics{
		FirstLessonStarts:  uint64(max(0, value.FirstLessonStarts)),
		LessonCompletions:  uint64(max(0, value.LessonCompletions)),
		QuizSubmissions:    uint64(max(0, value.QuizSubmissions)),
		ExpiredEnrollments: uint64(max(0, value.ExpiredEnrollments)), ReturnVisits: uint64(max(0, value.ReturnVisits)),
		AverageProgressPercent: value.AverageProgressPercent, MedianProgressPercent: value.MedianProgressPercent,
		LessonDropOff: make([]*academyv1.CampaignLessonDropOff, len(value.LessonDropOff)),
		Attribution:   make([]*academyv1.CampaignAttribution, len(value.Attribution)),
		Versions:      make([]*academyv1.CampaignVersionAnalytics, len(value.Versions)),
	}
	if value.AverageCompletionSecs != nil && *value.AverageCompletionSecs >= 0 {
		converted := uint64(*value.AverageCompletionSecs)
		result.AverageCompletionSeconds = &converted
	}
	if value.MedianCompletionSecs != nil && *value.MedianCompletionSecs >= 0 {
		converted := uint64(*value.MedianCompletionSecs)
		result.MedianCompletionSeconds = &converted
	}
	for index, item := range value.LessonDropOff {
		result.LessonDropOff[index] = &academyv1.CampaignLessonDropOff{
			LessonVersionId: item.LessonVersionID.String(), Reached: uint64(max(0, item.Reached)),
			Completed: uint64(max(0, item.Completed)),
		}
	}
	for index, item := range value.Attribution {
		result.Attribution[index] = &academyv1.CampaignAttribution{
			UtmSource: item.UTMSource, UtmMedium: item.UTMMedium, UtmCampaign: item.UTMCampaign,
			UtmContent: item.UTMContent, UtmTerm: item.UTMTerm, Referrer: item.Referrer,
			Visits: uint64(max(0, item.Visits)), Activations: uint64(max(0, item.Activations)),
			Completions: uint64(max(0, item.Completions)),
		}
	}
	for index, item := range value.Versions {
		result.Versions[index] = &academyv1.CampaignVersionAnalytics{
			CourseVersionId: item.CourseVersionID.String(), VersionNumber: uint32(max(0, item.VersionNumber)),
			Activations: uint64(max(0, item.Activations)), Completions: uint64(max(0, item.Completions)),
			AverageProgressPercent: item.AverageProgressPercent,
		}
	}
	return result
}

func courseExternalReportToProto(value application.CourseExternalReport) *academyv1.CourseExternalReport {
	result := &academyv1.CourseExternalReport{CourseId: value.CourseID.String(), Enrollments: make([]*academyv1.CourseEnrollment, len(value.Enrollments))}
	for index := range value.Enrollments {
		result.Enrollments[index] = enrollmentToProto(value.Enrollments[index])
	}
	return result
}

func externalLearnerTimelineToProto(value application.ExternalLearnerTimeline) *academyv1.ExternalLearnerTimeline {
	result := &academyv1.ExternalLearnerTimeline{
		Learner: externalLearnerToProto(value.Learner), Events: make([]*academyv1.ExternalLearnerTimelineEvent, len(value.Events)),
	}
	for index, event := range value.Events {
		converted := &academyv1.ExternalLearnerTimelineEvent{
			Id: event.ID.String(), Type: externalTimelineEventTypeToProto(event.Type),
			OccurredAt: timestamppb.New(event.OccurredAt), CourseId: optionalUUIDString(event.CourseID),
			CourseVersionId: optionalUUIDString(event.CourseVersionID), EnrollmentId: optionalUUIDString(event.EnrollmentID),
			SourceId: optionalUUIDString(event.SourceID), CourseTitle: event.CourseTitle, DeletedCourse: event.DeletedCourse,
		}
		if event.SourceType != nil {
			value := enrollmentSourceTypeToProto(*event.SourceType)
			converted.SourceType = &value
		}
		if event.VersionNumber != nil && *event.VersionNumber >= 0 {
			value := uint32(*event.VersionNumber)
			converted.VersionNumber = &value
		}
		if event.ProgressPercent != nil && *event.ProgressPercent >= 0 {
			value := uint32(*event.ProgressPercent)
			converted.ProgressPercent = &value
		}
		if event.AccessStatus != nil {
			value := enrollmentAccessStatusToProto(*event.AccessStatus)
			converted.AccessStatus = &value
		}
		result.Events[index] = converted
	}
	return result
}

func externalTimelineEventTypeToProto(value string) academyv1.ExternalLearnerTimelineEventType {
	values := map[string]academyv1.ExternalLearnerTimelineEventType{
		"access_issued":          academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACCESS_ISSUED,
		"verification_requested": academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_VERIFICATION_REQUESTED,
		"email_verified":         academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_EMAIL_VERIFIED,
		"activated":              academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACTIVATED,
		"lesson_completed":       academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_LESSON_COMPLETED,
		"quiz_submitted":         academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_QUIZ_SUBMITTED,
		"course_completed":       academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_COURSE_COMPLETED,
		"deadline_expired":       academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_DEADLINE_EXPIRED,
		"access_extended":        academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACCESS_EXTENDED,
		"token_rotated":          academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_TOKEN_ROTATED,
		"access_revoked":         academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_ACCESS_REVOKED,
		"frozen":                 academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_FROZEN,
		"suspended":              academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_SUSPENDED,
		"repeat_created":         academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_REPEAT_CREATED,
	}
	if result, ok := values[value]; ok {
		return result
	}
	return academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_UNSPECIFIED
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}
