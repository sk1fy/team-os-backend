package grpc

import (
	"context"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

func (s *Server) GetCourseTemplates(ctx context.Context, request *academyv1.GetCourseTemplatesRequest) (*academyv1.GetCourseTemplatesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	var templateType, lifecycle *string
	if request.Type != nil {
		value, convertErr := courseTemplateTypeFromProto(request.GetType())
		if convertErr != nil {
			return nil, convertErr
		}
		templateType = &value
	}
	if request.LifecycleStatus != nil {
		value, convertErr := courseTemplateLifecycleFromProto(request.GetLifecycleStatus())
		if convertErr != nil {
			return nil, convertErr
		}
		lifecycle = &value
	}
	values, err := s.application.GetCourseTemplates(ctx, actor, application.GetCourseTemplatesInput{
		Query: request.Query, TemplateType: templateType, Lifecycle: lifecycle,
		Page: int32(request.GetPage()), PageSize: int32(request.GetPageSize()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseTemplatesResponse{
		Items: academyTemplateSummariesToProto(values.Items),
		Page:  uint32(values.Page), PageSize: uint32(values.PageSize),
		Total: uint64(values.Total), TotalPages: uint32(values.TotalPages),
	}, nil
}

func (s *Server) GetCourseTemplate(ctx context.Context, request *academyv1.GetCourseTemplateRequest) (*academyv1.GetCourseTemplateResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	templateID, err := parseUUID(request.GetTemplateId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseOptionalUUID(request.VersionId)
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetCourseTemplate(ctx, actor, templateID, versionID)
	if err != nil {
		return nil, transportError(err)
	}
	response, err := courseTemplateDetailsToProto(value)
	if err != nil {
		return nil, transportError(err)
	}
	return response, nil
}

func (s *Server) CreateCourseTemplate(ctx context.Context, request *academyv1.CreateCourseTemplateRequest) (*academyv1.CreateCourseTemplateResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	coverFileID, err := parseOptionalUUID(request.CoverFileId)
	if err != nil {
		return nil, err
	}
	content, err := courseTemplateContentFromProto(request.Content)
	if err != nil {
		return nil, err
	}
	sequential := false
	if request.Sequential != nil {
		sequential = request.GetSequential()
	}
	template, draft, err := s.application.CreateCourseTemplate(ctx, actor, application.CreateCourseTemplateInput{
		Title: request.GetTitle(), Description: request.Description, CoverFileID: coverFileID,
		Sequential: sequential, Content: content,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseTemplateResponse{Template: courseTemplateToProto(template), Draft: courseTemplateVersionToProto(draft)}, nil
}

func (s *Server) UpdateCourseTemplateDraft(ctx context.Context, request *academyv1.UpdateCourseTemplateDraftRequest) (*academyv1.UpdateCourseTemplateDraftResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	templateID, err := parseUUID(request.GetTemplateId())
	if err != nil {
		return nil, err
	}
	coverFileID, err := parseOptionalUUID(request.CoverFileId)
	if err != nil {
		return nil, err
	}
	content, err := courseTemplateContentFromProto(request.Content)
	if err != nil {
		return nil, err
	}
	value, err := s.application.UpdateCourseTemplateDraft(ctx, actor, application.UpdateCourseTemplateDraftInput{
		TemplateID: templateID, Title: request.Title, Description: request.Description,
		CoverFileID: coverFileID, Sequential: request.Sequential, Content: content,
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseTemplateVersionDetailsToProto(value)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateCourseTemplateDraftResponse{Draft: converted}, nil
}

func (s *Server) CreateCourseTemplateDraft(ctx context.Context, request *academyv1.CreateCourseTemplateDraftRequest) (*academyv1.CreateCourseTemplateDraftResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	templateID, err := parseUUID(request.GetTemplateId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.CreateCourseTemplateDraft(ctx, actor, templateID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseTemplateVersionDetailsToProto(value)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseTemplateDraftResponse{Draft: converted}, nil
}

func (s *Server) PublishCourseTemplateVersion(ctx context.Context, request *academyv1.PublishCourseTemplateVersionRequest) (*academyv1.PublishCourseTemplateVersionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	templateID, err := parseUUID(request.GetTemplateId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.PublishCourseTemplateVersion(ctx, actor, templateID, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.PublishCourseTemplateVersionResponse{Version: courseTemplateVersionToProto(value)}, nil
}

func (s *Server) ArchiveCourseTemplate(ctx context.Context, request *academyv1.ArchiveCourseTemplateRequest) (*academyv1.ArchiveCourseTemplateResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	templateID, err := parseUUID(request.GetTemplateId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.ArchiveCourseTemplate(ctx, actor, templateID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ArchiveCourseTemplateResponse{Template: courseTemplateToProto(value)}, nil
}

func (s *Server) InstantiateCourseTemplateVersion(ctx context.Context, request *academyv1.InstantiateCourseTemplateVersionRequest) (*academyv1.InstantiateCourseTemplateVersionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.InstantiateCourseTemplateVersion(ctx, actor, versionID, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.InstantiateCourseTemplateVersionResponse{Result: courseTemplateInstantiationToProto(value)}, nil
}
