package transport

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func courseRestrictionFromProto(value *academyv1.CourseRestriction) (api.CourseRestriction, error) {
	if value == nil || value.GetCreatedAt() == nil {
		return api.CourseRestriction{}, errors.New("academy returned an empty course restriction")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.CourseRestriction{}, err
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.CourseRestriction{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.CourseRestriction{}, err
	}
	createdByID, err := uuid.Parse(value.GetCreatedById())
	if err != nil {
		return api.CourseRestriction{}, err
	}
	typeValue, err := courseRestrictionTypeFromProto(value.GetType())
	if err != nil {
		return api.CourseRestriction{}, err
	}
	result := api.CourseRestriction{
		Id: id, CompanyId: &companyID, CourseId: courseID, Type: typeValue, Reason: value.GetReason(),
		CreatedById: createdByID, CreatedAt: value.GetCreatedAt().AsTime(), ResolutionReason: value.ResolutionReason,
		ResolvedAt: protoTimestampPointer(value.GetResolvedAt()),
	}
	if value.ResolvedById != nil {
		parsed, parseErr := uuid.Parse(value.GetResolvedById())
		if parseErr != nil {
			return api.CourseRestriction{}, parseErr
		}
		result.ResolvedById = &parsed
	}
	return result, nil
}

func courseRestrictionsFromProto(values []*academyv1.CourseRestriction) ([]api.CourseRestriction, error) {
	result := make([]api.CourseRestriction, len(values))
	for index := range values {
		converted, err := courseRestrictionFromProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func courseRestrictionTypeFromProto(value academyv1.CourseRestrictionType) (api.CourseRestrictionType, error) {
	switch value {
	case academyv1.CourseRestrictionType_COURSE_RESTRICTION_TYPE_PAUSE:
		return api.Pause, nil
	case academyv1.CourseRestrictionType_COURSE_RESTRICTION_TYPE_BLOCK:
		return api.Block, nil
	default:
		return "", fmt.Errorf("unknown course restriction type %d", value)
	}
}

func courseOriginFromProto(value *academyv1.CourseOrigin) (api.CourseOrigin, error) {
	if value == nil || value.GetInstantiatedAt() == nil {
		return api.CourseOrigin{}, errors.New("academy returned an empty course origin")
	}
	instantiatedByID, err := uuid.Parse(value.GetInstantiatedById())
	if err != nil {
		return api.CourseOrigin{}, err
	}
	typeValue, err := courseOriginTypeFromProtoValue(value.GetType())
	if err != nil {
		return api.CourseOrigin{}, err
	}
	result := api.CourseOrigin{
		Type: typeValue, AcquisitionType: api.FreeCopy, InstantiatedById: instantiatedByID,
		InstantiatedAt: value.GetInstantiatedAt().AsTime(),
	}
	if result.SourceCourseId, err = parseOptionalProtoUUID(value.SourceCourseId); err != nil {
		return api.CourseOrigin{}, err
	}
	if result.SourceCourseVersionId, err = parseOptionalProtoUUID(value.SourceCourseVersionId); err != nil {
		return api.CourseOrigin{}, err
	}
	if result.SourcePartnerId, err = parseOptionalProtoUUID(value.SourcePartnerId); err != nil {
		return api.CourseOrigin{}, err
	}
	if result.SourceTemplateId, err = parseOptionalProtoUUID(value.SourceTemplateId); err != nil {
		return api.CourseOrigin{}, err
	}
	if result.SourceTemplateVersionId, err = parseOptionalProtoUUID(value.SourceTemplateVersionId); err != nil {
		return api.CourseOrigin{}, err
	}
	if result.EntitlementId, err = parseOptionalProtoUUID(value.EntitlementId); err != nil {
		return api.CourseOrigin{}, err
	}
	return result, nil
}

func courseOriginTypeFromProtoValue(value academyv1.CourseOriginType) (api.CourseOriginType, error) {
	switch value {
	case academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_PARTNER_COURSE:
		return api.PartnerCourse, nil
	case academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_SYSTEM_TEMPLATE:
		return api.SystemTemplate, nil
	case academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_COMPANY_TEMPLATE:
		return api.CompanyTemplate, nil
	default:
		return "", fmt.Errorf("unknown course origin type %d", value)
	}
}

func parseOptionalProtoUUID(value *string) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := uuid.Parse(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func partnerCourseCopyResultFromProto(value *academyv1.PartnerCourseCopyResult) (api.PartnerCourseCopyResult, error) {
	if value == nil {
		return api.PartnerCourseCopyResult{}, errors.New("academy returned an empty partner course copy")
	}
	course, err := courseFromProto(value.GetCourse())
	if err != nil {
		return api.PartnerCourseCopyResult{}, err
	}
	draft, err := courseVersionFromProto(value.GetDraft())
	if err != nil {
		return api.PartnerCourseCopyResult{}, err
	}
	origin, err := courseOriginFromProto(value.GetOrigin())
	if err != nil {
		return api.PartnerCourseCopyResult{}, err
	}
	return api.PartnerCourseCopyResult{Course: course, Draft: draft, Origin: origin}, nil
}

func partnerCourseGroupsFromProto(values []*academyv1.PartnerCourseGroup) ([]api.PartnerCourseGroup, error) {
	result := make([]api.PartnerCourseGroup, len(values))
	for index, value := range values {
		partnerID, err := uuid.Parse(value.GetPartnerId())
		if err != nil {
			return nil, err
		}
		courses, err := coursesFromProto(value.GetCourses())
		if err != nil {
			return nil, err
		}
		result[index] = api.PartnerCourseGroup{PartnerId: partnerID, Courses: courses}
	}
	return result, nil
}

func partnerCoursesReportFromProto(value *academyv1.PartnerCoursesReport) (api.PartnerCoursesReport, error) {
	if value == nil {
		return api.PartnerCoursesReport{}, errors.New("academy returned an empty partner report")
	}
	partnerID, err := uuid.Parse(value.GetPartnerId())
	if err != nil {
		return api.PartnerCoursesReport{}, err
	}
	operational := make([]api.PartnerCourseOperationalReport, len(value.GetOperationalCourses()))
	for index, item := range value.GetOperationalCourses() {
		course, convertErr := courseFromProto(item.GetCourse())
		if convertErr != nil {
			return api.PartnerCoursesReport{}, convertErr
		}
		operational[index] = api.PartnerCourseOperationalReport{
			Course: course, VersionCount: int(item.GetVersionCount()), EnrollmentCount: int(item.GetEnrollmentCount()),
			ActiveEnrollmentCount:    int(item.GetActiveEnrollmentCount()),
			CompletedEnrollmentCount: int(item.GetCompletedEnrollmentCount()),
			AverageProgressPercent:   int(item.GetAverageProgressPercent()),
		}
	}
	courses, err := courseExternalReportsFromProto(value.GetCourses())
	if err != nil {
		return api.PartnerCoursesReport{}, err
	}
	result := api.PartnerCoursesReport{
		PartnerId: partnerID, Courses: courses, OperationalCourses: &operational,
	}
	if summary := value.GetSummary(); summary != nil {
		result.Summary = &api.PartnerCourseReportSummary{
			TotalCourses: int(summary.GetTotalCourses()), ActiveCourses: int(summary.GetActiveCourses()),
			ArchivedCourses: int(summary.GetArchivedCourses()), DeletedCourses: int(summary.GetDeletedCourses()),
			PausedCourses: int(summary.GetPausedCourses()), BlockedCourses: int(summary.GetBlockedCourses()),
		}
	}
	return result, nil
}

func courseVersionPreviewFromProto(value *academyv1.CourseVersionPreview) (api.CourseVersionPreview, error) {
	if value == nil || value.GetVersion() == nil {
		return api.CourseVersionPreview{}, errors.New("academy returned an empty course preview")
	}
	course, err := courseFromProto(value.GetCourse())
	if err != nil {
		return api.CourseVersionPreview{}, err
	}
	version := value.GetVersion()
	versionID, err := uuid.Parse(version.GetId())
	if err != nil {
		return api.CourseVersionPreview{}, err
	}
	courseID, err := uuid.Parse(version.GetCourseId())
	if err != nil {
		return api.CourseVersionPreview{}, err
	}
	sections := make([]api.CoursePreviewSection, len(version.GetSections()))
	for sectionIndex, section := range version.GetSections() {
		sectionID, parseErr := uuid.Parse(section.GetId())
		if parseErr != nil {
			return api.CourseVersionPreview{}, parseErr
		}
		lessons := make([]api.CoursePreviewLesson, len(section.GetLessons()))
		for lessonIndex, lesson := range section.GetLessons() {
			converted, convertErr := coursePreviewLessonFromProto(lesson)
			if convertErr != nil {
				return api.CourseVersionPreview{}, convertErr
			}
			lessons[lessonIndex] = converted
		}
		sections[sectionIndex] = api.CoursePreviewSection{
			Id: sectionID, Title: section.GetTitle(), Order: int(section.GetOrder()), Lessons: lessons,
		}
	}
	return api.CourseVersionPreview{
		Course: course, PersistsProgress: api.CourseVersionPreviewPersistsProgress(value.GetPersistsProgress()),
		Version: api.LearnerCourseVersionPreview{
			Id: versionID, CourseId: courseID, Number: int(version.GetNumber()), Title: version.GetTitle(),
			Description: version.Description, CoverUrl: version.CoverUrl, Sequential: version.GetSequential(), Sections: sections,
		},
	}, nil
}

func coursePreviewLessonFromProto(value *academyv1.LearnerCourseVersionLesson) (api.CoursePreviewLesson, error) {
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.CoursePreviewLesson{}, err
	}
	versionID, err := uuid.Parse(value.GetCourseVersionId())
	if err != nil {
		return api.CoursePreviewLesson{}, err
	}
	sectionID, err := uuid.Parse(value.GetSectionVersionId())
	if err != nil {
		return api.CoursePreviewLesson{}, err
	}
	content, err := richTextFromStruct(value.GetContent())
	if err != nil {
		return api.CoursePreviewLesson{}, err
	}
	result := api.CoursePreviewLesson{
		Id: id, CourseVersionId: versionID, SectionVersionId: sectionID, StableKey: value.GetStableKey(),
		Title: value.GetTitle(), Order: int(value.GetOrder()), Content: content,
	}
	if value.EstimatedMinutes != nil {
		minutes := int(value.GetEstimatedMinutes())
		result.EstimatedMinutes = &minutes
	}
	if value.GetQuiz() != nil {
		quiz, convertErr := learnerQuizFromProto(value.GetQuiz())
		if convertErr != nil {
			return api.CoursePreviewLesson{}, convertErr
		}
		result.Quiz = &quiz
	}
	return result, nil
}

func coursePreviewQuizAttemptResultFromProto(value *academyv1.CoursePreviewQuizAttemptResult) (api.CoursePreviewQuizAttemptResult, error) {
	if value == nil {
		return api.CoursePreviewQuizAttemptResult{}, errors.New("academy returned an empty preview quiz result")
	}
	id, err := uuid.Parse(value.GetQuizVersionId())
	if err != nil {
		return api.CoursePreviewQuizAttemptResult{}, err
	}
	return api.CoursePreviewQuizAttemptResult{
		QuizVersionId: id, Score: int(value.GetScore()), Passed: value.GetPassed(), PendingReview: value.GetPendingReview(),
	}, nil
}
