package grpc

import (
	"context"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

func (s *Server) CreateExternalCampaign(ctx context.Context, request *academyv1.CreateExternalCampaignRequest) (*academyv1.CreateExternalCampaignResponse, error) {
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
	created, err := s.application.CreateExternalCampaign(ctx, actor, application.CreateExternalCampaignInput{
		CourseID: courseID, CourseVersionID: versionID, Name: request.GetName(),
		Purpose: externalCampaignPurposeFromProto(request.GetPurpose()), DeadlineDays: int32(request.GetDeadlineDays()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateExternalCampaignResponse{Created: externalCampaignCreatedToProto(created)}, nil
}

func (s *Server) GetExternalCampaigns(ctx context.Context, request *academyv1.GetExternalCampaignsRequest) (*academyv1.GetExternalCampaignsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	values, err := s.application.GetExternalCampaigns(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalCampaignsResponse{Campaigns: externalCampaignsToProto(values)}, nil
}

func (s *Server) GetExternalCampaign(ctx context.Context, request *academyv1.GetExternalCampaignRequest) (*academyv1.GetExternalCampaignResponse, error) {
	actor, campaignID, err := s.externalInternalActorAndID(ctx, request.GetCampaignId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetExternalCampaign(ctx, actor, campaignID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalCampaignResponse{Campaign: externalCampaignToProto(value)}, nil
}

func (s *Server) PauseExternalCampaign(ctx context.Context, request *academyv1.PauseExternalCampaignRequest) (*academyv1.PauseExternalCampaignResponse, error) {
	actor, campaignID, err := s.externalInternalActorAndID(ctx, request.GetCampaignId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.PauseExternalCampaign(ctx, actor, campaignID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.PauseExternalCampaignResponse{Campaign: externalCampaignToProto(value)}, nil
}

func (s *Server) ResumeExternalCampaign(ctx context.Context, request *academyv1.ResumeExternalCampaignRequest) (*academyv1.ResumeExternalCampaignResponse, error) {
	actor, campaignID, err := s.externalInternalActorAndID(ctx, request.GetCampaignId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.ResumeExternalCampaign(ctx, actor, campaignID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ResumeExternalCampaignResponse{Campaign: externalCampaignToProto(value)}, nil
}

func (s *Server) RotateExternalCampaignToken(ctx context.Context, request *academyv1.RotateExternalCampaignTokenRequest) (*academyv1.RotateExternalCampaignTokenResponse, error) {
	actor, campaignID, err := s.externalInternalActorAndID(ctx, request.GetCampaignId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.RotateExternalCampaignToken(ctx, actor, campaignID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RotateExternalCampaignTokenResponse{Created: externalCampaignCreatedToProto(value)}, nil
}

func (s *Server) RevokeExternalCampaign(ctx context.Context, request *academyv1.RevokeExternalCampaignRequest) (*academyv1.RevokeExternalCampaignResponse, error) {
	actor, campaignID, err := s.externalInternalActorAndID(ctx, request.GetCampaignId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.RevokeExternalCampaign(ctx, actor, campaignID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RevokeExternalCampaignResponse{Campaign: externalCampaignToProto(value)}, nil
}

func (s *Server) GetExternalCampaignReport(ctx context.Context, request *academyv1.GetExternalCampaignReportRequest) (*academyv1.GetExternalCampaignReportResponse, error) {
	actor, campaignID, err := s.externalInternalActorAndID(ctx, request.GetCampaignId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetExternalCampaignReport(ctx, actor, campaignID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalCampaignReportResponse{Report: externalCampaignReportToProto(value)}, nil
}

func (s *Server) GetCourseExternalReport(ctx context.Context, request *academyv1.GetCourseExternalReportRequest) (*academyv1.GetCourseExternalReportResponse, error) {
	actor, courseID, err := s.externalInternalActorAndID(ctx, request.GetCourseId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetCourseExternalReport(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseExternalReportResponse{Report: courseExternalReportToProto(value)}, nil
}

func (s *Server) GetExternalLearnerTimeline(ctx context.Context, request *academyv1.GetExternalLearnerTimelineRequest) (*academyv1.GetExternalLearnerTimelineResponse, error) {
	actor, learnerID, err := s.externalInternalActorAndID(ctx, request.GetLearnerId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetExternalLearnerTimeline(ctx, actor, learnerID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetExternalLearnerTimelineResponse{Timeline: externalLearnerTimelineToProto(value)}, nil
}
