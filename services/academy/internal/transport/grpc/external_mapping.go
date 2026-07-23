package grpc

import (
	"time"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func externalPersonalAccessToProto(value application.ExternalPersonalAccess) *academyv1.ExternalPersonalAccess {
	result := &academyv1.ExternalPersonalAccess{Id: value.ID.String(), CompanyId: value.CompanyID.String(),
		CourseId: value.CourseID.String(), CourseVersionId: value.CourseVersionID.String(),
		PartnerOwnerId: value.PartnerOwnerID.String(), ExpectedEmail: value.ExpectedEmail,
		RecipientFirstName: value.RecipientFirstName, RecipientLastName: value.RecipientLastName,
		DeadlineDays: uint32(max(0, value.DeadlineDays)), Status: externalAccessStatusToProto(value.Status),
		TokenPrefix: value.TokenPrefix, IssuedById: value.IssuedByID.String(), IssuedAt: timestamppb.New(value.IssuedAt)}
	result.ExternalLearnerId = optionalUUIDString(value.ExternalLearnerID)
	result.EnrollmentId = optionalUUIDString(value.EnrollmentID)
	result.ActivatedAt = externalOptionalTimestamp(value.ActivatedAt)
	result.RevokedAt = externalOptionalTimestamp(value.RevokedAt)
	return result
}

func externalPersonalAccessesToProto(values []application.ExternalPersonalAccess) []*academyv1.ExternalPersonalAccess {
	result := make([]*academyv1.ExternalPersonalAccess, len(values))
	for index, value := range values {
		result[index] = externalPersonalAccessToProto(value)
	}
	return result
}

func externalPersonalAccessCreatedToProto(value application.ExternalPersonalAccessCreated) *academyv1.ExternalPersonalAccessCreated {
	return &academyv1.ExternalPersonalAccessCreated{Access: externalPersonalAccessToProto(value.Access), Token: value.Token}
}

func externalLearnerToProto(value application.ExternalLearner) *academyv1.ExternalLearner {
	return &academyv1.ExternalLearner{Id: value.ID.String(), CompanyId: value.CompanyID.String(), Email: value.Email,
		NormalizedEmail: value.NormalizedEmail, FirstName: value.FirstName, LastName: value.LastName, Phone: value.Phone,
		EmailVerifiedAt: externalOptionalTimestamp(value.EmailVerifiedAt), CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt)}
}

func externalLearnersToProto(values []application.ExternalLearner) []*academyv1.ExternalLearner {
	result := make([]*academyv1.ExternalLearner, len(values))
	for index, value := range values {
		result[index] = externalLearnerToProto(value)
	}
	return result
}

func publicAcademyAccessToProto(value application.PublicAcademyAccess) *academyv1.PublicAcademyAccess {
	result := &academyv1.PublicAcademyAccess{Kind: publicAccessKindToProto(value.Kind), CourseId: value.CourseID.String(),
		CourseVersionId: value.CourseVersionID.String(), Title: value.Title, Description: value.Description,
		CoverUrl: value.CoverURL, OwnerType: courseOwnerTypeToProto(value.OwnerType), OwnerUserId: optionalUUIDString(value.OwnerUserID),
		DeadlineDays: uint32(max(0, value.DeadlineDays)), Available: value.Available,
		UnavailableReason: value.UnavailableReason, EmailVerificationRequired: value.EmailVerificationRequired,
		Outline: make([]*academyv1.PublicAcademyOutlineSection, len(value.Outline))}
	for sectionIndex, section := range value.Outline {
		lessons := make([]*academyv1.PublicAcademyOutlineLesson, len(section.Lessons))
		for lessonIndex, lesson := range section.Lessons {
			converted := &academyv1.PublicAcademyOutlineLesson{Id: lesson.ID.String(), Title: lesson.Title, Order: uint32(max(0, lesson.Order))}
			if lesson.EstimatedMinutes != nil && *lesson.EstimatedMinutes >= 0 {
				minutes := uint32(*lesson.EstimatedMinutes)
				converted.EstimatedMinutes = &minutes
			}
			lessons[lessonIndex] = converted
		}
		result.Outline[sectionIndex] = &academyv1.PublicAcademyOutlineSection{Id: section.ID.String(), Title: section.Title,
			Order: uint32(max(0, section.Order)), Lessons: lessons}
	}
	return result
}

func externalVerificationChallengeToProto(value application.ExternalVerificationChallenge) *academyv1.ExternalVerificationChallenge {
	return &academyv1.ExternalVerificationChallenge{ChallengeId: value.ID.String(), ExpiresAt: timestamppb.New(value.ExpiresAt)}
}

func externalVerificationConfirmedToProto(value application.ExternalVerificationConfirmed) *academyv1.ExternalVerificationConfirmed {
	return &academyv1.ExternalVerificationConfirmed{LearnerId: value.LearnerID.String(), VerifiedAt: timestamppb.New(value.VerifiedAt),
		SessionToken: value.SessionToken, SessionExpiresAt: timestamppb.New(value.SessionExpiresAt)}
}

func externalQuizAttemptResultToProto(value application.ExternalQuizAttemptResult) *academyv1.ExternalQuizAttemptResult {
	result := &academyv1.ExternalQuizAttemptResult{Id: value.ID.String(), Score: uint32(max(0, value.Score)),
		Passed: value.Passed, PendingReview: value.PendingReview, CreatedAt: timestamppb.New(value.CreatedAt)}
	if value.AttemptsRemaining != nil && *value.AttemptsRemaining >= 0 {
		remaining := uint32(*value.AttemptsRemaining)
		result.AttemptsRemaining = &remaining
	}
	return result
}

func externalEnrollmentResultsToProto(value application.ExternalEnrollmentResults) *academyv1.ExternalEnrollmentResults {
	attempts := make([]*academyv1.ExternalQuizAttemptResult, len(value.QuizAttempts))
	for index, attempt := range value.QuizAttempts {
		attempts[index] = externalQuizAttemptResultToProto(attempt)
	}
	return &academyv1.ExternalEnrollmentResults{Enrollment: enrollmentToProto(value.Enrollment),
		CompletedLessonIds: externalUUIDStrings(value.CompletedLessonIDs), QuizAttempts: attempts}
}

func externalAccessStatusToProto(value string) academyv1.ExternalPersonalAccessStatus {
	switch value {
	case "issued":
		return academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_ISSUED
	case "activated":
		return academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_ACTIVATED
	case "revoked":
		return academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_REVOKED
	case "closed":
		return academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_CLOSED
	default:
		return academyv1.ExternalPersonalAccessStatus_EXTERNAL_PERSONAL_ACCESS_STATUS_UNSPECIFIED
	}
}

func publicAccessKindToProto(value string) academyv1.PublicAcademyAccessKind {
	if value == "personal" {
		return academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_PERSONAL_ACCESS
	}
	if value == "partner_promo_campaign" {
		return academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_PARTNER_PROMO_CAMPAIGN
	}
	if value == "company_candidate_campaign" {
		return academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_COMPANY_CANDIDATE_CAMPAIGN
	}
	return academyv1.PublicAcademyAccessKind_PUBLIC_ACADEMY_ACCESS_KIND_UNSPECIFIED
}

func externalOptionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}

func externalUUIDStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
