package grpc

import (
	"context"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

func (s *Server) GetCoursePartnerAudience(ctx context.Context, request *academyv1.GetCoursePartnerAudienceRequest) (*academyv1.GetCoursePartnerAudienceResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	audience, err := s.application.GetCoursePartnerAudience(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCoursePartnerAudienceResponse{
		Audience: coursePartnerAudienceToProto(audience.Audience), PartnerUserIds: uuidsToStrings(audience.PartnerUserIDs),
	}, nil
}

func (s *Server) SetCoursePartnerAudience(ctx context.Context, request *academyv1.SetCoursePartnerAudienceRequest) (*academyv1.SetCoursePartnerAudienceResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	partnerIDs, err := parseUUIDList(request.GetPartnerUserIds())
	if err != nil {
		return nil, err
	}
	audience, err := s.application.SetCoursePartnerAudience(ctx, actor, application.SetCoursePartnerAudienceInput{
		CourseID: courseID, Audience: coursePartnerAudienceFromProto(request.GetAudience()), PartnerUserIDs: partnerIDs,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.SetCoursePartnerAudienceResponse{
		Audience: coursePartnerAudienceToProto(audience.Audience), PartnerUserIds: uuidsToStrings(audience.PartnerUserIDs),
	}, nil
}

func (s *Server) GetPartnerCourseGroups(ctx context.Context, request *academyv1.GetPartnerCourseGroupsRequest) (*academyv1.GetPartnerCourseGroupsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	var lifecycle, distribution *string
	if request.Lifecycle != nil {
		value, convertErr := courseLifecycleFromProto(request.GetLifecycle())
		if convertErr != nil {
			return nil, convertErr
		}
		lifecycle = &value
	}
	if request.Distribution != nil {
		value, convertErr := courseDistributionFromProto(request.GetDistribution())
		if convertErr != nil {
			return nil, convertErr
		}
		distribution = &value
	}
	groups, err := s.application.GetPartnerCourseGroups(ctx, actor, lifecycle, distribution)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPartnerCourseGroupsResponse{Groups: partnerCourseGroupsToProto(groups)}, nil
}

func (s *Server) GetPartnerCoursesReport(ctx context.Context, request *academyv1.GetPartnerCoursesReportRequest) (*academyv1.GetPartnerCoursesReportResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	partnerID, err := parseUUID(request.GetPartnerId())
	if err != nil {
		return nil, err
	}
	report, err := s.application.GetPartnerCoursesReport(ctx, actor, partnerID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPartnerCoursesReportResponse{Report: partnerCoursesReportToProto(report)}, nil
}

func (s *Server) GetCourseVersionPreview(ctx context.Context, request *academyv1.GetCourseVersionPreviewRequest) (*academyv1.GetCourseVersionPreviewResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	preview, err := s.application.GetCourseVersionPreview(ctx, actor, courseID, versionID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseVersionPreviewToProto(preview)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	return &academyv1.GetCourseVersionPreviewResponse{Preview: converted}, nil
}

func (s *Server) SubmitCoursePreviewQuizAttempt(ctx context.Context, request *academyv1.SubmitCoursePreviewQuizAttemptRequest) (*academyv1.SubmitCoursePreviewQuizAttemptResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	quizID, err := parseUUID(request.GetQuizVersionId())
	if err != nil {
		return nil, err
	}
	answers := make([]application.EnrollmentQuizAnswer, len(request.GetAnswers()))
	for index, answer := range request.GetAnswers() {
		answers[index] = application.EnrollmentQuizAnswer{
			QuestionID: answer.GetQuestionId(), SelectedOptionIDs: append([]string(nil), answer.GetSelectedOptionIds()...), Text: answer.Text,
		}
	}
	result, err := s.application.SubmitCoursePreviewQuizAttempt(ctx, actor, courseID, versionID, quizID, answers)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.SubmitCoursePreviewQuizAttemptResponse{Result: coursePreviewQuizAttemptResultToProto(result)}, nil
}

func (s *Server) PausePartnerCourseDistribution(ctx context.Context, request *academyv1.PausePartnerCourseDistributionRequest) (*academyv1.PausePartnerCourseDistributionResponse, error) {
	actor, courseID, err := s.partnerCourseCommand(ctx, request.GetCourseId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.PausePartnerCourseDistribution(ctx, actor, courseID, request.GetReason())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.PausePartnerCourseDistributionResponse{Restriction: courseRestrictionToProto(value)}, nil
}

func (s *Server) BlockPartnerCourse(ctx context.Context, request *academyv1.BlockPartnerCourseRequest) (*academyv1.BlockPartnerCourseResponse, error) {
	actor, courseID, err := s.partnerCourseCommand(ctx, request.GetCourseId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.BlockPartnerCourse(ctx, actor, courseID, request.GetReason())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.BlockPartnerCourseResponse{Restriction: courseRestrictionToProto(value)}, nil
}

func (s *Server) ResolvePartnerCourseRestriction(ctx context.Context, request *academyv1.ResolvePartnerCourseRestrictionRequest) (*academyv1.ResolvePartnerCourseRestrictionResponse, error) {
	actor, courseID, err := s.partnerCourseCommand(ctx, request.GetCourseId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.ResolvePartnerCourseRestriction(ctx, actor, courseID, request.GetReason())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ResolvePartnerCourseRestrictionResponse{Restriction: courseRestrictionToProto(value)}, nil
}

func (s *Server) GetCourseRestrictions(ctx context.Context, request *academyv1.GetCourseRestrictionsRequest) (*academyv1.GetCourseRestrictionsResponse, error) {
	actor, courseID, err := s.partnerCourseCommand(ctx, request.GetCourseId())
	if err != nil {
		return nil, err
	}
	values, err := s.application.GetCourseRestrictions(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseRestrictionsResponse{Restrictions: courseRestrictionsToProto(values)}, nil
}

func (s *Server) CopyPartnerCourseVersionToCompany(ctx context.Context, request *academyv1.CopyPartnerCourseVersionToCompanyRequest) (*academyv1.CopyPartnerCourseVersionToCompanyResponse, error) {
	actor, courseID, err := s.partnerCourseCommand(ctx, request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.CopyPartnerCourseVersionToCompany(ctx, actor, courseID, versionID, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CopyPartnerCourseVersionToCompanyResponse{Result: partnerCourseCopyResultToProto(value)}, nil
}

func (s *Server) partnerCourseCommand(ctx context.Context, rawCourseID string) (application.Actor, uuid.UUID, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return application.Actor{}, uuid.Nil, err
	}
	courseID, err := parseUUID(rawCourseID)
	if err != nil {
		return application.Actor{}, uuid.Nil, err
	}
	return actor, courseID, nil
}
