package transport

import (
	"testing"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestCourseVisibilityToProtoAcceptsContractValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value api.CourseVisibility
		want  academyv1.CourseVisibility
	}{
		{value: api.CourseVisibilityRestricted, want: academyv1.CourseVisibility_COURSE_VISIBILITY_RESTRICTED},
		{value: api.CourseVisibilityCompany, want: academyv1.CourseVisibility_COURSE_VISIBILITY_COMPANY},
		{value: api.CourseVisibilityPublic, want: academyv1.CourseVisibility_COURSE_VISIBILITY_PUBLIC},
	}
	for _, tt := range tests {
		t.Run(string(tt.value), func(t *testing.T) {
			t.Parallel()
			got, err := courseVisibilityToProto(tt.value)
			if err != nil {
				t.Fatalf("courseVisibilityToProto(%q): %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("courseVisibilityToProto(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestCourseFromProtoPreservesOwnershipAndLifecycle(t *testing.T) {
	ownerUserID := uuid.New()
	createdByID := uuid.New()
	ownerType := academyv1.CourseOwnerType_COURSE_OWNER_TYPE_PARTNER
	lifecycle := academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_ARCHIVED
	distribution := academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_PAUSED

	converted, err := courseFromProto(&academyv1.Course{
		Id: uuid.NewString(), AuthorId: uuid.NewString(), Title: "Курс партнёра",
		Status:     academyv1.CourseStatus_COURSE_STATUS_DRAFT,
		Visibility: academyv1.CourseVisibility_COURSE_VISIBILITY_RESTRICTED,
		OwnerType:  &ownerType, OwnerUserId: stringPointer(ownerUserID.String()),
		CreatedById: stringPointer(createdByID.String()), LifecycleStatus: &lifecycle,
		DistributionStatus: &distribution,
	})
	if err != nil {
		t.Fatal(err)
	}
	if converted.OwnerType == nil || *converted.OwnerType != api.CourseOwnerTypePartner {
		t.Fatalf("ownerType = %v", converted.OwnerType)
	}
	if converted.OwnerUserId == nil || *converted.OwnerUserId != ownerUserID {
		t.Fatalf("ownerUserId = %v", converted.OwnerUserId)
	}
	if converted.CreatedById == nil || *converted.CreatedById != createdByID {
		t.Fatalf("createdById = %v", converted.CreatedById)
	}
	if converted.LifecycleStatus == nil || *converted.LifecycleStatus != api.CourseLifecycleStatusArchived {
		t.Fatalf("lifecycleStatus = %v", converted.LifecycleStatus)
	}
	if converted.DistributionStatus == nil || *converted.DistributionStatus != api.CourseDistributionStatusPaused {
		t.Fatalf("distributionStatus = %v", converted.DistributionStatus)
	}
}

func TestCourseFromProtoPreservesVersionPointers(t *testing.T) {
	draftID := uuid.New()
	publishedID := uuid.New()
	converted, err := courseFromProto(&academyv1.Course{
		Id: uuid.NewString(), AuthorId: uuid.NewString(), Title: "Версионный курс",
		Status:                   academyv1.CourseStatus_COURSE_STATUS_DRAFT,
		Visibility:               academyv1.CourseVisibility_COURSE_VISIBILITY_RESTRICTED,
		CurrentDraftVersionId:    stringPointer(draftID.String()),
		LatestPublishedVersionId: stringPointer(publishedID.String()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if converted.CurrentDraftVersionId == nil || *converted.CurrentDraftVersionId != draftID {
		t.Fatalf("currentDraftVersionId = %v", converted.CurrentDraftVersionId)
	}
	if converted.LatestPublishedVersionId == nil || *converted.LatestPublishedVersionId != publishedID {
		t.Fatalf("latestPublishedVersionId = %v", converted.LatestPublishedVersionId)
	}
}

func TestPublicAcademyAccessFromProtoPreservesUnavailableContract(t *testing.T) {
	enrollmentID := uuid.New()
	reason := "distribution_paused"
	message := "Распространение курса временно приостановлено"

	converted, err := publicAcademyAccessFromProto(&academyv1.PublicAcademyAccess{
		Kind:                 academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_PERSONAL_ACCESS,
		CourseId:             uuid.NewString(),
		CourseVersionId:      uuid.NewString(),
		Title:                "Курс",
		OwnerType:            academyv1.CourseOwnerType_COURSE_OWNER_TYPE_PARTNER,
		DeadlineDays:         3,
		Available:            false,
		UnavailableReason:    &reason,
		Message:              &message,
		ExistingEnrollmentId: stringPointer(enrollmentID.String()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if converted.UnavailableReason == nil ||
		*converted.UnavailableReason != api.PublicAcademyUnavailableReason("distribution_paused") {
		t.Fatalf("unavailableReason = %v", converted.UnavailableReason)
	}
	if converted.Message == nil || *converted.Message != message {
		t.Fatalf("message = %v", converted.Message)
	}
	if converted.ExistingEnrollmentId == nil || *converted.ExistingEnrollmentId != enrollmentID {
		t.Fatalf("existingEnrollmentId = %v", converted.ExistingEnrollmentId)
	}
}

func TestLessonBlockRichTextSurvivesGatewayRoundTrip(t *testing.T) {
	t.Parallel()

	nodes := []any{
		map[string]any{
			"type": "lessonBlock",
			"attrs": map[string]any{
				"id": "checklist-1", "kind": "checklist",
				"data": map[string]any{
					"style": "card", "title": "Проверьте себя",
					"items": []any{map[string]any{"id": "item-1", "text": "Первый пункт"}},
				},
			},
		},
	}
	encoded, err := richTextToStruct(api.RichTextContent{Type: "doc", Content: &nodes})
	if err != nil {
		t.Fatalf("encode lesson blocks: %v", err)
	}
	decoded, err := richTextFromStruct(encoded)
	if err != nil {
		t.Fatalf("decode lesson blocks: %v", err)
	}
	if decoded.Content == nil || len(*decoded.Content) != 1 {
		t.Fatalf("decoded content = %#v", decoded.Content)
	}
	block, ok := (*decoded.Content)[0].(map[string]any)
	if !ok || block["type"] != "lessonBlock" {
		t.Fatalf("decoded block = %#v", (*decoded.Content)[0])
	}
	attrs, ok := block["attrs"].(map[string]any)
	if !ok || attrs["kind"] != "checklist" {
		t.Fatalf("decoded attrs = %#v", block["attrs"])
	}
}

func stringPointer(value string) *string { return &value }
