package grpc

import (
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func uuidsToStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}

func coursePartnerAudienceToProto(value string) academyv1.CoursePartnerAudience {
	switch value {
	case "all_partners":
		return academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_ALL_PARTNERS
	case "selected_partners":
		return academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_SELECTED_PARTNERS
	case "none":
		return academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_NONE
	default:
		return academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_UNSPECIFIED
	}
}

func coursePartnerAudienceFromProto(value academyv1.CoursePartnerAudience) string {
	switch value {
	case academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_ALL_PARTNERS:
		return "all_partners"
	case academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_SELECTED_PARTNERS:
		return "selected_partners"
	case academyv1.CoursePartnerAudience_COURSE_PARTNER_AUDIENCE_NONE:
		return "none"
	default:
		return ""
	}
}

func optionalProtoUUID(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	converted := value.String()
	return &converted
}

func courseOriginTypeToProto(value string) academyv1.CourseOriginType {
	switch value {
	case "partner_course":
		return academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_PARTNER_COURSE
	case "system_template":
		return academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_SYSTEM_TEMPLATE
	case "company_template":
		return academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_COMPANY_TEMPLATE
	default:
		return academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_UNSPECIFIED
	}
}

func courseRestrictionToProto(value application.CourseRestriction) *academyv1.CourseRestriction {
	result := &academyv1.CourseRestriction{
		Id: value.ID.String(), CompanyId: value.CompanyID.String(), CourseId: value.CourseID.String(),
		Type: courseRestrictionTypeToProto(value.Type), Reason: value.Reason,
		CreatedById: value.CreatedByID.String(), CreatedAt: timestamppb.New(value.CreatedAt),
		ResolutionReason: value.ResolutionReason,
	}
	if value.ResolvedByID != nil {
		converted := value.ResolvedByID.String()
		result.ResolvedById = &converted
	}
	if value.ResolvedAt != nil {
		result.ResolvedAt = timestamppb.New(*value.ResolvedAt)
	}
	return result
}

func courseRestrictionsToProto(values []application.CourseRestriction) []*academyv1.CourseRestriction {
	result := make([]*academyv1.CourseRestriction, len(values))
	for index := range values {
		result[index] = courseRestrictionToProto(values[index])
	}
	return result
}

func courseRestrictionTypeToProto(value string) academyv1.CourseRestrictionType {
	switch value {
	case "pause":
		return academyv1.CourseRestrictionType_COURSE_RESTRICTION_TYPE_PAUSE
	case "block":
		return academyv1.CourseRestrictionType_COURSE_RESTRICTION_TYPE_BLOCK
	default:
		return academyv1.CourseRestrictionType_COURSE_RESTRICTION_TYPE_UNSPECIFIED
	}
}

func courseOriginToProto(value application.CourseOrigin) *academyv1.CourseOrigin {
	result := &academyv1.CourseOrigin{
		Type: courseOriginTypeToProto(value.Type), InstantiatedById: value.InstantiatedByID.String(),
		InstantiatedAt: timestamppb.New(value.InstantiatedAt), AcquisitionType: value.AcquisitionType,
	}
	result.SourceCourseId = optionalProtoUUID(value.SourceCourseID)
	result.SourceCourseVersionId = optionalProtoUUID(value.SourceCourseVersionID)
	result.SourcePartnerId = optionalProtoUUID(value.SourcePartnerID)
	result.SourceTemplateId = optionalProtoUUID(value.SourceTemplateID)
	result.SourceTemplateVersionId = optionalProtoUUID(value.SourceTemplateVersionID)
	result.EntitlementId = optionalProtoUUID(value.EntitlementID)
	return result
}

func partnerCourseCopyResultToProto(value application.PartnerCourseCopyResult) *academyv1.PartnerCourseCopyResult {
	return &academyv1.PartnerCourseCopyResult{
		Course: courseToProto(value.Course), Draft: courseVersionToProto(value.Draft), Origin: courseOriginToProto(value.Origin),
	}
}

func partnerCourseGroupsToProto(values []application.PartnerCourseGroup) []*academyv1.PartnerCourseGroup {
	result := make([]*academyv1.PartnerCourseGroup, len(values))
	for index := range values {
		result[index] = &academyv1.PartnerCourseGroup{
			PartnerId: values[index].PartnerID.String(), Courses: coursesToProto(values[index].Courses),
		}
	}
	return result
}

func partnerCoursesReportToProto(value application.PartnerCoursesReport) *academyv1.PartnerCoursesReport {
	result := &academyv1.PartnerCoursesReport{
		PartnerId: value.PartnerID.String(),
		Summary: &academyv1.PartnerCourseReportSummary{
			TotalCourses:    uint32(max(0, value.Summary.TotalCourses)),
			ActiveCourses:   uint32(max(0, value.Summary.ActiveCourses)),
			ArchivedCourses: uint32(max(0, value.Summary.ArchivedCourses)),
			DeletedCourses:  uint32(max(0, value.Summary.DeletedCourses)),
			PausedCourses:   uint32(max(0, value.Summary.PausedCourses)),
			BlockedCourses:  uint32(max(0, value.Summary.BlockedCourses)),
		},
		OperationalCourses: make([]*academyv1.PartnerCourseOperationalReport, len(value.Courses)),
		Courses:            make([]*academyv1.CourseExternalReport, len(value.ExternalCourses)),
	}
	for index, item := range value.Courses {
		result.OperationalCourses[index] = &academyv1.PartnerCourseOperationalReport{
			Course: courseToProto(item.Course), VersionCount: uint32(max(0, item.VersionCount)),
			EnrollmentCount:          uint32(max(0, item.EnrollmentCount)),
			ActiveEnrollmentCount:    uint32(max(0, item.ActiveEnrollmentCount)),
			CompletedEnrollmentCount: uint32(max(0, item.CompletedEnrollmentCount)),
			AverageProgressPercent:   uint32(max(0, item.AverageProgressPercent)),
		}
	}
	for index, item := range value.ExternalCourses {
		result.Courses[index] = courseExternalReportToProto(item)
	}
	return result
}

func courseVersionPreviewToProto(value application.CourseVersionPreview) (*academyv1.CourseVersionPreview, error) {
	version, err := learnerPublishedCourseVersionToProto(value.Version)
	if err != nil {
		return nil, fmt.Errorf("preview version: %w", err)
	}
	return &academyv1.CourseVersionPreview{
		Course: courseToProto(value.Course), Version: version, PersistsProgress: value.PersistsProgress,
	}, nil
}

func coursePreviewQuizAttemptResultToProto(value application.CoursePreviewQuizAttemptResult) *academyv1.CoursePreviewQuizAttemptResult {
	return &academyv1.CoursePreviewQuizAttemptResult{
		QuizVersionId: value.QuizVersionID.String(), Score: uint32(max(0, value.Score)),
		Passed: value.Passed, PendingReview: value.PendingReview,
	}
}
