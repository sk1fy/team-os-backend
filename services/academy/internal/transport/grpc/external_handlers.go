package grpc

import (
	"context"
	"strings"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (s *Server) CreateExternalPersonalAccess(ctx context.Context, request *academyv1.CreateExternalPersonalAccessRequest) (*academyv1.CreateExternalPersonalAccessResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetCourseVersionId())
	if err != nil {
		return nil, err
	}
	created, err := s.application.CreateExternalPersonalAccess(ctx, actor, application.CreateExternalPersonalAccessInput{
		CourseID: courseID, CourseVersionID: versionID, Email: request.GetEmail(),
		FirstName: request.FirstName, LastName: request.LastName, DeadlineDays: int32(request.GetDeadlineDays()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateExternalPersonalAccessResponse{Created: externalPersonalAccessCreatedToProto(created)}, nil
}

func (s *Server) GetExternalPersonalAccesses(ctx context.Context, request *academyv1.GetExternalPersonalAccessesRequest) (*academyv1.GetExternalPersonalAccessesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	values, err := s.application.GetExternalPersonalAccesses(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalPersonalAccessesResponse{Accesses: externalPersonalAccessesToProto(values)}, nil
}

func (s *Server) GetExternalPersonalAccess(ctx context.Context, request *academyv1.GetExternalPersonalAccessRequest) (*academyv1.GetExternalPersonalAccessResponse, error) {
	actor, accessID, err := s.externalInternalActorAndID(ctx, request.GetAccessId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetExternalPersonalAccess(ctx, actor, accessID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalPersonalAccessResponse{Access: externalPersonalAccessToProto(value)}, nil
}

func (s *Server) ExtendExternalPersonalAccess(ctx context.Context, request *academyv1.ExtendExternalPersonalAccessRequest) (*academyv1.ExtendExternalPersonalAccessResponse, error) {
	actor, accessID, err := s.externalInternalActorAndID(ctx, request.GetAccessId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.ExtendExternalPersonalAccess(ctx, actor, accessID, int32(request.GetDeadlineDays()))
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ExtendExternalPersonalAccessResponse{Access: externalPersonalAccessToProto(value)}, nil
}

func (s *Server) RotateExternalPersonalAccessToken(ctx context.Context, request *academyv1.RotateExternalPersonalAccessTokenRequest) (*academyv1.RotateExternalPersonalAccessTokenResponse, error) {
	actor, accessID, err := s.externalInternalActorAndID(ctx, request.GetAccessId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.RotateExternalPersonalAccessToken(ctx, actor, accessID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RotateExternalPersonalAccessTokenResponse{Created: externalPersonalAccessCreatedToProto(value)}, nil
}

func (s *Server) RevokeExternalPersonalAccess(ctx context.Context, request *academyv1.RevokeExternalPersonalAccessRequest) (*academyv1.RevokeExternalPersonalAccessResponse, error) {
	actor, accessID, err := s.externalInternalActorAndID(ctx, request.GetAccessId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.RevokeExternalPersonalAccess(ctx, actor, accessID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RevokeExternalPersonalAccessResponse{Access: externalPersonalAccessToProto(value)}, nil
}

func (s *Server) RepeatExternalPersonalAccess(ctx context.Context, request *academyv1.RepeatExternalPersonalAccessRequest) (*academyv1.RepeatExternalPersonalAccessResponse, error) {
	actor, accessID, err := s.externalInternalActorAndID(ctx, request.GetAccessId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.RepeatExternalPersonalAccess(ctx, actor, accessID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RepeatExternalPersonalAccessResponse{Created: externalPersonalAccessCreatedToProto(value)}, nil
}

func (s *Server) GetPublicAcademyAccess(ctx context.Context, request *academyv1.GetPublicAcademyAccessRequest) (*academyv1.GetPublicAcademyAccessResponse, error) {
	var principal *application.ExternalPrincipal
	if value, err := s.externalPrincipal(ctx, false); err == nil {
		principal = &value
	}
	access, err := s.application.GetPublicAcademyAccess(ctx, request.GetToken(), principal, application.CampaignAnalyticsContext{
		VisitorHash: request.GetVisitorHash(), UTMSource: request.UtmSource, UTMMedium: request.UtmMedium,
		UTMCampaign: request.UtmCampaign, UTMContent: request.UtmContent, UTMTerm: request.UtmTerm,
		Referrer: request.Referrer,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublicAcademyAccessResponse{Access: publicAcademyAccessToProto(access)}, nil
}

func (s *Server) RequestPublicAcademyVerification(ctx context.Context, request *academyv1.RequestPublicAcademyVerificationRequest) (*academyv1.RequestPublicAcademyVerificationResponse, error) {
	challenge, err := s.application.RequestPublicAcademyVerification(ctx, application.RequestExternalVerificationInput{
		AccessToken: request.GetAccessToken(), Email: request.GetEmail(), FirstName: request.FirstName,
		LastName: request.LastName, IPHash: s.application.ExternalIPHash(incomingMetadataValue(ctx, "x-client-ip")),
		Analytics: application.CampaignAnalyticsContext{VisitorHash: request.GetVisitorHash()},
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RequestPublicAcademyVerificationResponse{Challenge: externalVerificationChallengeToProto(challenge)}, nil
}

func (s *Server) ConfirmPublicAcademyVerification(ctx context.Context, request *academyv1.ConfirmPublicAcademyVerificationRequest) (*academyv1.ConfirmPublicAcademyVerificationResponse, error) {
	challengeID, err := parseUUID(request.GetChallengeId())
	if err != nil {
		return nil, err
	}
	confirmed, err := s.application.ConfirmPublicAcademyVerification(ctx, challengeID, request.GetCode())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ConfirmPublicAcademyVerificationResponse{Confirmed: externalVerificationConfirmedToProto(confirmed)}, nil
}

func (s *Server) ActivatePublicAcademyAccess(ctx context.Context, request *academyv1.ActivatePublicAcademyAccessRequest) (*academyv1.ActivatePublicAcademyAccessResponse, error) {
	principal, err := s.externalPrincipal(ctx, true)
	if err != nil {
		return nil, err
	}
	enrollment, err := s.application.ActivatePublicAcademyAccess(
		ctx, principal, request.GetAccessToken(), request.GetIdempotencyKey(),
		application.CampaignAnalyticsContext{VisitorHash: request.GetVisitorHash()},
	)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ActivatePublicAcademyAccessResponse{Enrollment: enrollmentToProto(enrollment)}, nil
}

func (s *Server) GetPublicAcademyEnrollment(ctx context.Context, request *academyv1.GetPublicAcademyEnrollmentRequest) (*academyv1.GetPublicAcademyEnrollmentResponse, error) {
	principal, enrollmentID, err := s.externalPrincipalAndID(ctx, request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetPublicAcademyEnrollment(ctx, principal, enrollmentID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublicAcademyEnrollmentResponse{Enrollment: enrollmentToProto(value)}, nil
}

func (s *Server) GetPublicAcademyEnrollmentOutline(ctx context.Context, request *academyv1.GetPublicAcademyEnrollmentOutlineRequest) (*academyv1.GetPublicAcademyEnrollmentOutlineResponse, error) {
	principal, enrollmentID, err := s.externalPrincipalAndID(ctx, request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetPublicAcademyEnrollmentOutline(ctx, principal, enrollmentID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublicAcademyEnrollmentOutlineResponse{Outline: enrollmentOutlineToProto(value)}, nil
}

func (s *Server) GetPublicAcademyEnrollmentLesson(ctx context.Context, request *academyv1.GetPublicAcademyEnrollmentLessonRequest) (*academyv1.GetPublicAcademyEnrollmentLessonResponse, error) {
	principal, enrollmentID, err := s.externalPrincipalAndID(ctx, request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonVersionId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetPublicAcademyEnrollmentLesson(ctx, principal, enrollmentID, lessonID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := enrollmentLessonToProto(value)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublicAcademyEnrollmentLessonResponse{Lesson: converted}, nil
}

func (s *Server) CompletePublicAcademyEnrollmentLesson(ctx context.Context, request *academyv1.CompletePublicAcademyEnrollmentLessonRequest) (*academyv1.CompletePublicAcademyEnrollmentLessonResponse, error) {
	principal, enrollmentID, err := s.externalPrincipalAndID(ctx, request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonVersionId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.CompletePublicAcademyEnrollmentLesson(ctx, principal, enrollmentID, lessonID, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CompletePublicAcademyEnrollmentLessonResponse{Enrollment: enrollmentToProto(value)}, nil
}

func (s *Server) SubmitPublicAcademyQuizAttempt(ctx context.Context, request *academyv1.SubmitPublicAcademyQuizAttemptRequest) (*academyv1.SubmitPublicAcademyQuizAttemptResponse, error) {
	principal, enrollmentID, err := s.externalPrincipalAndID(ctx, request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	quizID, err := parseUUID(request.GetQuizVersionId())
	if err != nil {
		return nil, err
	}
	answers := make([]application.EnrollmentQuizAnswer, len(request.GetAnswers()))
	for index, answer := range request.GetAnswers() {
		answers[index] = application.EnrollmentQuizAnswer{QuestionID: answer.GetQuestionId(),
			SelectedOptionIDs: append([]string(nil), answer.GetSelectedOptionIds()...), Text: answer.Text}
	}
	value, err := s.application.SubmitPublicAcademyQuizAttempt(ctx, principal, application.SubmitExternalQuizInput{
		EnrollmentID: enrollmentID, QuizID: quizID, IdempotencyKey: request.GetIdempotencyKey(), Answers: answers,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.SubmitPublicAcademyQuizAttemptResponse{Result: externalQuizAttemptResultToProto(value)}, nil
}

func (s *Server) GetPublicAcademyEnrollmentResults(ctx context.Context, request *academyv1.GetPublicAcademyEnrollmentResultsRequest) (*academyv1.GetPublicAcademyEnrollmentResultsResponse, error) {
	principal, enrollmentID, err := s.externalPrincipalAndID(ctx, request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetPublicAcademyEnrollmentResults(ctx, principal, enrollmentID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublicAcademyEnrollmentResultsResponse{Results: externalEnrollmentResultsToProto(value)}, nil
}

func (s *Server) GetExternalLearners(ctx context.Context, _ *academyv1.GetExternalLearnersRequest) (*academyv1.GetExternalLearnersResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	values, err := s.application.GetExternalLearners(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalLearnersResponse{Learners: externalLearnersToProto(values)}, nil
}

func (s *Server) GetExternalLearner(ctx context.Context, request *academyv1.GetExternalLearnerRequest) (*academyv1.GetExternalLearnerResponse, error) {
	actor, learnerID, err := s.externalInternalActorAndID(ctx, request.GetLearnerId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetExternalLearner(ctx, actor, learnerID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalLearnerResponse{Learner: externalLearnerToProto(value)}, nil
}

func (s *Server) GetExternalLearnerEnrollments(ctx context.Context, request *academyv1.GetExternalLearnerEnrollmentsRequest) (*academyv1.GetExternalLearnerEnrollmentsResponse, error) {
	actor, learnerID, err := s.externalInternalActorAndID(ctx, request.GetLearnerId())
	if err != nil {
		return nil, err
	}
	values, err := s.application.GetExternalLearnerEnrollments(ctx, actor, learnerID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalLearnerEnrollmentsResponse{Enrollments: enrollmentsToProto(values)}, nil
}

func (s *Server) externalInternalActorAndID(ctx context.Context, rawID string) (application.Actor, uuid.UUID, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return application.Actor{}, uuid.Nil, err
	}
	id, err := parseUUID(rawID)
	return actor, id, err
}

func (s *Server) externalPrincipalAndID(ctx context.Context, rawID string) (application.ExternalPrincipal, uuid.UUID, error) {
	principal, err := s.externalPrincipal(ctx, true)
	if err != nil {
		return application.ExternalPrincipal{}, uuid.Nil, err
	}
	id, err := parseUUID(rawID)
	return principal, id, err
}

func (s *Server) externalPrincipal(ctx context.Context, required bool) (application.ExternalPrincipal, error) {
	token := incomingMetadataValue(ctx, "x-external-session")
	if strings.TrimSpace(token) == "" && !required {
		return application.ExternalPrincipal{}, status.Error(codes.Unauthenticated, "Требуется внешняя сессия")
	}
	principal, err := s.application.AuthenticateExternalSession(ctx, token)
	if err != nil {
		return application.ExternalPrincipal{}, transportError(err)
	}
	return principal, nil
}

func incomingMetadataValue(ctx context.Context, key string) string {
	metadataValues, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := metadataValues.Get(key)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}
