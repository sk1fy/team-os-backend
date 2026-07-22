package transport

import (
	"testing"
	"time"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExternalCampaignMapping(t *testing.T) {
	t.Parallel()

	value := validExternalCampaignProto()
	converted, err := externalCampaignFromProto(value)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Name != "Кандидаты — август" || converted.OwnerType != "partner" ||
		converted.Purpose != "partner_promo" || converted.Status != "active" || converted.DeadlineDays != 3 {
		t.Fatalf("converted=%#v", converted)
	}
	value.OwnerType = academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_UNSPECIFIED
	if _, err := externalCampaignFromProto(value); err == nil {
		t.Fatal("unspecified owner type accepted")
	}
}

func TestCampaignAnalyticsMapping(t *testing.T) {
	t.Parallel()

	averageSeconds := uint64(3600)
	analytics, err := campaignAnalyticsFromProto(&academyv1.CampaignAnalytics{
		FirstLessonStarts: 8, LessonCompletions: 12, QuizSubmissions: 7,
		ExpiredEnrollments: 2, ReturnVisits: 5, AverageProgressPercent: 61.5,
		MedianProgressPercent: 70, AverageCompletionSeconds: &averageSeconds,
		LessonDropOff: []*academyv1.CampaignLessonDropOff{{
			LessonVersionId: "77777777-7777-4777-8777-777777777777", Reached: 8, Completed: 6,
		}},
		Attribution: []*academyv1.CampaignAttribution{{UtmSource: stringPointer("newsletter"), Visits: 10, Activations: 4}},
		Versions: []*academyv1.CampaignVersionAnalytics{{
			CourseVersionId: "33333333-3333-4333-8333-333333333333", VersionNumber: 2,
			Activations: 4, Completions: 2, AverageProgressPercent: 61.5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analytics.FirstLessonStarts != 8 || analytics.AverageCompletionSeconds == nil ||
		*analytics.AverageCompletionSeconds != 3600 || len(analytics.LessonDropOff) != 1 ||
		len(analytics.Attribution) != 1 || len(analytics.Versions) != 1 {
		t.Fatalf("analytics=%#v", analytics)
	}
}

func TestExternalLearnerTimelineMapping(t *testing.T) {
	t.Parallel()

	now := timestamppb.New(time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC))
	progress := uint32(75)
	event, err := externalLearnerTimelineEventFromProto(&academyv1.ExternalLearnerTimelineEvent{
		Id:         "88888888-8888-4888-8888-888888888888",
		Type:       academyv1.ExternalLearnerTimelineEventType_EXTERNAL_LEARNER_TIMELINE_EVENT_TYPE_LESSON_COMPLETED,
		OccurredAt: now, CourseId: stringPointer("22222222-2222-4222-8222-222222222222"),
		ProgressPercent: &progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "lesson_completed" || event.CourseId == nil || event.ProgressPercent == nil || *event.ProgressPercent != 75 {
		t.Fatalf("event=%#v", event)
	}
}

func validExternalCampaignProto() *academyv1.ExternalCampaign {
	return &academyv1.ExternalCampaign{
		Id: "11111111-1111-4111-8111-111111111111", CompanyId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		CourseId: "22222222-2222-4222-8222-222222222222", CourseVersionId: "33333333-3333-4333-8333-333333333333",
		OwnerType:   academyv1.ExternalCampaignOwnerType_EXTERNAL_CAMPAIGN_OWNER_TYPE_PARTNER,
		OwnerUserId: stringPointer("44444444-4444-4444-8444-444444444444"),
		Purpose:     academyv1.ExternalCampaignPurpose_EXTERNAL_CAMPAIGN_PURPOSE_PARTNER_PROMO,
		Name:        "Кандидаты — август", DeadlineDays: 3,
		Status:      academyv1.ExternalCampaignStatus_EXTERNAL_CAMPAIGN_STATUS_ACTIVE,
		TokenPrefix: "visitor123", CreatedById: "44444444-4444-4444-8444-444444444444",
		CreatedAt: timestamppb.New(time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC)),
	}
}
