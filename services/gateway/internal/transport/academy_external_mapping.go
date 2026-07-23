package transport

import (
	"errors"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

var errEmptyAcademyResponse = errors.New("academy returned empty response")

func externalPersonalAccessFromProto(value *academyv1.ExternalPersonalAccess) (api.ExternalPersonalAccess, error) {
	if value == nil {
		return api.ExternalPersonalAccess{}, errors.New("academy returned empty personal access")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	versionID, err := uuid.Parse(value.GetCourseVersionId())
	if err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	partnerID, err := uuid.Parse(value.GetPartnerOwnerId())
	if err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	issuerID, err := uuid.Parse(value.GetIssuedById())
	if err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	result := api.ExternalPersonalAccess{
		Id: id, CompanyId: companyID, CourseId: courseID, CourseVersionId: versionID,
		PartnerOwnerId: partnerID, ExpectedEmail: openapi_types.Email(value.GetExpectedEmail()),
		RecipientFirstName: value.RecipientFirstName, RecipientLastName: value.RecipientLastName,
		DeadlineDays: int(value.GetDeadlineDays()), Status: externalAccessStatusFromProto(value.GetStatus()),
		TokenPrefix: value.GetTokenPrefix(), IssuedById: issuerID,
	}
	if value.GetIssuedAt() == nil || !value.GetIssuedAt().IsValid() {
		return api.ExternalPersonalAccess{}, errors.New("academy returned personal access without issuedAt")
	}
	result.IssuedAt = value.GetIssuedAt().AsTime()
	result.ActivatedAt = protoTimestampPointer(value.GetActivatedAt())
	result.RevokedAt = protoTimestampPointer(value.GetRevokedAt())
	if result.ExternalLearnerId, err = parseOptionalUUIDString(value.GetExternalLearnerId()); err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	if result.EnrollmentId, err = parseOptionalUUIDString(value.GetEnrollmentId()); err != nil {
		return api.ExternalPersonalAccess{}, err
	}
	return result, nil
}

func externalPersonalAccessesFromProto(values []*academyv1.ExternalPersonalAccess) ([]api.ExternalPersonalAccess, error) {
	result := make([]api.ExternalPersonalAccess, len(values))
	for index, value := range values {
		converted, err := externalPersonalAccessFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func externalPersonalAccessCreatedFromProto(value *academyv1.ExternalPersonalAccessCreated) (api.ExternalPersonalAccessCreated, error) {
	if value == nil || value.GetToken() == "" {
		return api.ExternalPersonalAccessCreated{}, errors.New("academy returned empty personal access token")
	}
	access, err := externalPersonalAccessFromProto(value.GetAccess())
	if err != nil {
		return api.ExternalPersonalAccessCreated{}, err
	}
	return api.ExternalPersonalAccessCreated{Access: access, Token: value.GetToken()}, nil
}

func externalLearnerFromProto(value *academyv1.ExternalLearner) (api.ExternalLearner, error) {
	if value == nil {
		return api.ExternalLearner{}, errors.New("academy returned empty external learner")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ExternalLearner{}, err
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.ExternalLearner{}, err
	}
	if value.GetCreatedAt() == nil || !value.GetCreatedAt().IsValid() || value.GetUpdatedAt() == nil || !value.GetUpdatedAt().IsValid() {
		return api.ExternalLearner{}, errors.New("academy returned external learner without timestamps")
	}
	return api.ExternalLearner{
		Id: id, CompanyId: companyID, Email: openapi_types.Email(value.GetEmail()),
		NormalizedEmail: openapi_types.Email(value.GetNormalizedEmail()), FirstName: value.FirstName,
		LastName: value.LastName, Phone: value.Phone, EmailVerifiedAt: protoTimestampPointer(value.GetEmailVerifiedAt()),
		CreatedAt: value.GetCreatedAt().AsTime(), UpdatedAt: value.GetUpdatedAt().AsTime(),
	}, nil
}

func externalLearnersFromProto(values []*academyv1.ExternalLearner) ([]api.ExternalLearner, error) {
	result := make([]api.ExternalLearner, len(values))
	for index, value := range values {
		converted, err := externalLearnerFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func publicAcademyAccessFromProto(value *academyv1.PublicAcademyAccess) (api.PublicAcademyAccess, error) {
	if value == nil {
		return api.PublicAcademyAccess{}, errors.New("academy returned empty public access")
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.PublicAcademyAccess{}, err
	}
	versionID, err := uuid.Parse(value.GetCourseVersionId())
	if err != nil {
		return api.PublicAcademyAccess{}, err
	}
	ownerType, err := courseOwnerTypeFromProto(value.GetOwnerType())
	if err != nil {
		return api.PublicAcademyAccess{}, err
	}
	result := api.PublicAcademyAccess{
		Kind: publicAcademyAccessKindFromProto(value.GetKind()), CourseId: courseID, CourseVersionId: versionID,
		Title: value.GetTitle(), Description: value.Description, CoverUrl: value.CoverUrl,
		OwnerType: ownerType, DeadlineDays: int(value.GetDeadlineDays()), Available: value.GetAvailable(),
		UnavailableReason: value.UnavailableReason, EmailVerificationRequired: value.GetEmailVerificationRequired(),
		Outline: make([]api.PublicAcademyOutlineSection, len(value.GetOutline())),
	}
	if result.OwnerUserId, err = parseOptionalUUIDString(value.GetOwnerUserId()); err != nil {
		return api.PublicAcademyAccess{}, err
	}
	for sectionIndex, section := range value.GetOutline() {
		sectionID, parseErr := uuid.Parse(section.GetId())
		if parseErr != nil {
			return api.PublicAcademyAccess{}, parseErr
		}
		lessons := make([]api.PublicAcademyOutlineLesson, len(section.GetLessons()))
		for lessonIndex, lesson := range section.GetLessons() {
			lessonID, lessonErr := uuid.Parse(lesson.GetId())
			if lessonErr != nil {
				return api.PublicAcademyAccess{}, lessonErr
			}
			converted := api.PublicAcademyOutlineLesson{Id: lessonID, Title: lesson.GetTitle(), Order: int(lesson.GetOrder())}
			if lesson.EstimatedMinutes != nil {
				minutes := int(lesson.GetEstimatedMinutes())
				converted.EstimatedMinutes = &minutes
			}
			lessons[lessonIndex] = converted
		}
		result.Outline[sectionIndex] = api.PublicAcademyOutlineSection{
			Id: sectionID, Title: section.GetTitle(), Order: int(section.GetOrder()), Lessons: lessons,
		}
	}
	return result, nil
}

func externalEnrollmentFromProto(value *academyv1.CourseEnrollment) (api.ExternalEnrollment, error) {
	converted, err := academyEnrollmentFromProto(value)
	if err != nil {
		return api.ExternalEnrollment{}, err
	}
	if converted.ExternalLearnerId == nil {
		return api.ExternalEnrollment{}, errors.New("academy returned external enrollment without learner")
	}
	return api.ExternalEnrollment{
		Id: converted.Id, CompanyId: converted.CompanyId, CourseId: converted.CourseId,
		CourseVersionId: converted.CourseVersionId, VersionNumber: int(value.GetVersionNumber()),
		LearnerType: converted.LearnerType, ExternalLearnerId: *converted.ExternalLearnerId,
		SourceType: converted.SourceType, SourceId: converted.SourceId, AttemptNumber: converted.AttemptNumber,
		ProgressStatus: converted.ProgressStatus, AccessStatus: converted.AccessStatus,
		CurrentLessonVersionId: converted.CurrentLessonVersionId, ProgressPercent: converted.ProgressPercent,
		ActivatedAt: converted.ActivatedAt, AccessUntil: converted.AccessUntil, StartedAt: converted.StartedAt,
		CompletedAt: converted.CompletedAt, LastActivityAt: converted.LastActivityAt, CreatedAt: converted.CreatedAt,
	}, nil
}

func externalEnrollmentsFromProto(values []*academyv1.CourseEnrollment) ([]api.ExternalEnrollment, error) {
	result := make([]api.ExternalEnrollment, len(values))
	for index, value := range values {
		converted, err := externalEnrollmentFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func externalOutlineFromProto(value *academyv1.EnrollmentOutline) (api.ExternalEnrollmentOutline, error) {
	internal, err := enrollmentOutlineFromProto(value)
	if err != nil {
		return api.ExternalEnrollmentOutline{}, err
	}
	enrollment, err := externalEnrollmentFromProto(value.GetEnrollment())
	if err != nil {
		return api.ExternalEnrollmentOutline{}, err
	}
	return api.ExternalEnrollmentOutline{Enrollment: enrollment, Sections: internal.Sections}, nil
}

func externalQuizAttemptResultFromProto(value *academyv1.ExternalQuizAttemptResult) (api.ExternalQuizAttemptResult, error) {
	if value == nil || value.GetCreatedAt() == nil || !value.GetCreatedAt().IsValid() {
		return api.ExternalQuizAttemptResult{}, errors.New("academy returned invalid external quiz result")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ExternalQuizAttemptResult{}, err
	}
	result := api.ExternalQuizAttemptResult{
		Id: id, Score: int(value.GetScore()), Passed: value.GetPassed(), PendingReview: value.GetPendingReview(),
		CreatedAt: value.GetCreatedAt().AsTime(),
	}
	if value.AttemptsRemaining != nil {
		remaining := int(value.GetAttemptsRemaining())
		result.AttemptsRemaining = &remaining
	}
	return result, nil
}

func externalEnrollmentResultsFromProto(value *academyv1.ExternalEnrollmentResults) (api.ExternalEnrollmentResults, error) {
	if value == nil {
		return api.ExternalEnrollmentResults{}, errors.New("academy returned empty external results")
	}
	enrollment, err := externalEnrollmentFromProto(value.GetEnrollment())
	if err != nil {
		return api.ExternalEnrollmentResults{}, err
	}
	completed, err := UUIDsFromStrings(value.GetCompletedLessonIds())
	if err != nil {
		return api.ExternalEnrollmentResults{}, err
	}
	attempts := make([]api.ExternalQuizAttemptResult, len(value.GetQuizAttempts()))
	for index, attempt := range value.GetQuizAttempts() {
		converted, convertErr := externalQuizAttemptResultFromProto(attempt)
		if convertErr != nil {
			return api.ExternalEnrollmentResults{}, convertErr
		}
		attempts[index] = converted
	}
	return api.ExternalEnrollmentResults{Enrollment: enrollment, CompletedLessonIds: completed, QuizAttempts: attempts}, nil
}

func externalAccessStatusFromProto(value academyv1.ExternalPersonalAccessStatus) api.ExternalAccessStatus {
	values := map[academyv1.ExternalPersonalAccessStatus]api.ExternalAccessStatus{
		academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_ISSUED:    "issued",
		academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_ACTIVATED: "activated",
		academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_REVOKED:   "revoked",
		academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_CLOSED:    "closed",
	}
	return values[value]
}

func publicAcademyAccessKindFromProto(value academyv1.PublicAcademyAccessKind) api.PublicAcademyAccessKind {
	values := map[academyv1.PublicAcademyAccessKind]api.PublicAcademyAccessKind{
		academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_PERSONAL_ACCESS:            "personal_access",
		academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_PARTNER_PROMO_CAMPAIGN:     "partner_promo_campaign",
		academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_COMPANY_CANDIDATE_CAMPAIGN: "company_candidate_campaign",
	}
	return values[value]
}

func externalAnswerToProto(value api.ExternalQuizAnswer) *academyv1.EnrollmentQuizAnswer {
	options := make([]string, 0)
	if value.OptionIds != nil {
		options = make([]string, len(*value.OptionIds))
		for index, id := range *value.OptionIds {
			options[index] = id.String()
		}
	}
	return &academyv1.EnrollmentQuizAnswer{
		QuestionId: value.QuestionId.String(), SelectedOptionIds: options, Text: value.Text,
	}
}
