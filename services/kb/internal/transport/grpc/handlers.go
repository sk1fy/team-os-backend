package grpc

import (
	"context"

	"github.com/google/uuid"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	"github.com/sk1fy/team-os-backend/services/kb/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) GetSections(ctx context.Context, _ *kbv1.GetSectionsRequest) (*kbv1.GetSectionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	sections, err := s.application.GetSections(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetSectionsResponse{Sections: sectionsToProto(sections)}, nil
}

func (s *Server) CreateSection(ctx context.Context, request *kbv1.CreateSectionRequest) (*kbv1.CreateSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	parentID, err := parseOptionalUUID(request.ParentId)
	if err != nil {
		return nil, err
	}
	var access *application.AccessSettings
	if request.Access != nil {
		converted, mapErr := accessFromProto(request.Access)
		if mapErr != nil {
			return nil, mapErr
		}
		access = &converted
	}
	var visibility *string
	if request.Visibility != nil {
		converted, mapErr := sectionVisibilityFromProto(request.GetVisibility())
		if mapErr != nil {
			return nil, mapErr
		}
		visibility = &converted
	}
	section, err := s.application.CreateSection(ctx, actor, application.CreateSectionInput{
		Name: request.GetName(), ParentID: parentID, Access: access, Visibility: visibility,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.CreateSectionResponse{Section: sectionToProto(section)}, nil
}

func (s *Server) UpdateSection(ctx context.Context, request *kbv1.UpdateSectionRequest) (*kbv1.UpdateSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	input := application.UpdateSectionInput{ID: id, Name: request.Name}
	if request.Access != nil {
		converted, mapErr := accessFromProto(request.Access)
		if mapErr != nil {
			return nil, mapErr
		}
		input.Access = &converted
	}
	if request.Visibility != nil {
		converted, mapErr := sectionVisibilityFromProto(request.GetVisibility())
		if mapErr != nil {
			return nil, mapErr
		}
		input.Visibility = &converted
	}
	section, err := s.application.UpdateSection(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.UpdateSectionResponse{Section: sectionToProto(section)}, nil
}

func (s *Server) DeleteSection(ctx context.Context, request *kbv1.DeleteSectionRequest) (*kbv1.DeleteSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteSection(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &kbv1.DeleteSectionResponse{}, nil
}

func (s *Server) GetArticles(ctx context.Context, request *kbv1.GetArticlesRequest) (*kbv1.GetArticlesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	var sectionID *uuid.UUID
	if request.SectionId != nil {
		parsed, parseErr := parseUUID(request.GetSectionId())
		if parseErr != nil {
			return nil, parseErr
		}
		sectionID = &parsed
	}
	articles, err := s.application.GetArticles(ctx, actor, sectionID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articlesToProto(articles)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetArticlesResponse{Articles: converted}, nil
}

func (s *Server) GetArticle(ctx context.Context, request *kbv1.GetArticleRequest) (*kbv1.GetArticleResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	article, err := s.application.GetArticle(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articleToProto(article)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetArticleResponse{Article: converted}, nil
}

func (s *Server) GetPublicArticle(ctx context.Context, request *kbv1.GetPublicArticleRequest) (*kbv1.GetPublicArticleResponse, error) {
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	article, err := s.application.GetPublicArticle(ctx, id)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articleToProto(article)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetPublicArticleResponse{Article: converted}, nil
}

func (s *Server) CreateArticle(ctx context.Context, request *kbv1.CreateArticleRequest) (*kbv1.CreateArticleResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	sectionID, err := parseUUID(request.GetSectionId())
	if err != nil {
		return nil, err
	}
	content, err := contentFromStruct(request.GetContent())
	if err != nil {
		return nil, err
	}
	status, err := articleStatusFromProto(request.GetStatus())
	if err != nil {
		return nil, err
	}
	input := application.CreateArticleInput{
		SectionID: sectionID, Title: request.GetTitle(), Content: content,
		Status: status, RequiresAcknowledgement: request.GetRequiresAcknowledgement(),
	}
	if request.PartnerAccess != nil {
		partnerAccess, mapErr := partnerAccessFromProto(request.PartnerAccess)
		if mapErr != nil {
			return nil, mapErr
		}
		input.PartnerAccess = &partnerAccess
	}
	if request.PartnerReusePolicy != nil {
		reusePolicy, mapErr := partnerReusePolicyFromProto(request.GetPartnerReusePolicy())
		if mapErr != nil {
			return nil, mapErr
		}
		input.PartnerReusePolicy = &reusePolicy
	}
	article, err := s.application.CreateArticle(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articleToProto(article)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.CreateArticleResponse{Article: converted}, nil
}

func (s *Server) UpdateArticle(ctx context.Context, request *kbv1.UpdateArticleRequest) (*kbv1.UpdateArticleResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	input := application.UpdateArticleInput{ID: id}
	if request.SectionId != nil {
		sectionID, parseErr := parseUUID(request.GetSectionId())
		if parseErr != nil {
			return nil, parseErr
		}
		input.SectionID = &sectionID
	}
	if request.Title != nil {
		input.Title = request.Title
	}
	if request.Content != nil {
		content, mapErr := contentFromStruct(request.Content)
		if mapErr != nil {
			return nil, mapErr
		}
		input.Content = content
	}
	if request.Status != nil {
		status, mapErr := articleStatusFromProto(request.GetStatus())
		if mapErr != nil {
			return nil, mapErr
		}
		input.Status = &status
	}
	if request.RequiresAcknowledgement != nil {
		input.RequiresAcknowledgement = request.RequiresAcknowledgement
	}
	if request.ExpectedVersion != nil {
		if request.GetExpectedVersion() > uint32(1<<31-1) {
			return nil, status.Error(codes.InvalidArgument, "Некорректная ожидаемая версия")
		}
		version := int32(request.GetExpectedVersion())
		input.ExpectedVersion = &version
	}
	if request.PartnerAccess != nil {
		partnerAccess, mapErr := partnerAccessFromProto(request.PartnerAccess)
		if mapErr != nil {
			return nil, mapErr
		}
		input.PartnerAccess = &partnerAccess
	}
	if request.PartnerReusePolicy != nil {
		reusePolicy, mapErr := partnerReusePolicyFromProto(request.GetPartnerReusePolicy())
		if mapErr != nil {
			return nil, mapErr
		}
		input.PartnerReusePolicy = &reusePolicy
	}
	article, err := s.application.UpdateArticle(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articleToProto(article)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.UpdateArticleResponse{Article: converted}, nil
}

func (s *Server) GetArticlePartnerPolicy(ctx context.Context, request *kbv1.GetArticlePartnerPolicyRequest) (*kbv1.GetArticlePartnerPolicyResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	policy, err := s.application.GetArticlePartnerPolicy(ctx, actor, articleID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articlePartnerPolicyToProto(policy)
	if err != nil {
		return nil, err
	}
	return &kbv1.GetArticlePartnerPolicyResponse{Policy: converted}, nil
}

func (s *Server) UpdateArticlePartnerPolicy(ctx context.Context, request *kbv1.UpdateArticlePartnerPolicyRequest) (*kbv1.UpdateArticlePartnerPolicyResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	access, err := partnerAccessFromProto(request.GetAccess())
	if err != nil {
		return nil, err
	}
	reusePolicy, err := partnerReusePolicyFromProto(request.GetReusePolicy())
	if err != nil {
		return nil, err
	}
	policy, err := s.application.UpdateArticlePartnerPolicy(ctx, actor, articleID, access, reusePolicy)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articlePartnerPolicyToProto(policy)
	if err != nil {
		return nil, err
	}
	return &kbv1.UpdateArticlePartnerPolicyResponse{Policy: converted}, nil
}

func (s *Server) CheckArticleCourseCopyPermission(ctx context.Context, request *kbv1.CheckArticleCourseCopyPermissionRequest) (*kbv1.CheckArticleCourseCopyPermissionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	articleVersionID, err := parseOptionalUUID(request.ArticleVersionId)
	if err != nil {
		return nil, err
	}
	targetPartnerID, err := parseOptionalUUID(request.TargetPartnerId)
	if err != nil {
		return nil, err
	}
	permission, err := s.application.CheckArticleCourseCopyPermission(
		ctx, actor, articleID, articleVersionID, targetPartnerID,
	)
	if err != nil {
		return nil, transportError(err)
	}
	return courseCopyPermissionToProto(permission)
}

func (s *Server) GetArticleSnapshotForCourseCopy(ctx context.Context, request *kbv1.GetArticleSnapshotForCourseCopyRequest) (*kbv1.GetArticleSnapshotForCourseCopyResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	articleVersionID, err := parseOptionalUUID(request.ArticleVersionId)
	if err != nil {
		return nil, err
	}
	targetPartnerID, err := parseOptionalUUID(request.TargetPartnerId)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.application.GetArticleSnapshotForCourseCopy(
		ctx, actor, articleID, articleVersionID, targetPartnerID,
	)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articleSnapshotToProto(snapshot)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetArticleSnapshotForCourseCopyResponse{Snapshot: converted}, nil
}

func (s *Server) RollbackArticle(ctx context.Context, request *kbv1.RollbackArticleRequest) (*kbv1.RollbackArticleResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	input := application.RollbackArticleInput{ArticleID: articleID, VersionID: versionID}
	if request.ExpectedVersion != nil {
		if request.GetExpectedVersion() > uint32(1<<31-1) {
			return nil, status.Error(codes.InvalidArgument, "Некорректная ожидаемая версия")
		}
		version := int32(request.GetExpectedVersion())
		input.ExpectedVersion = &version
	}
	article, err := s.application.RollbackArticle(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articleToProto(article)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.RollbackArticleResponse{Article: converted}, nil
}

func (s *Server) GetArticleVersions(ctx context.Context, request *kbv1.GetArticleVersionsRequest) (*kbv1.GetArticleVersionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	versions, err := s.application.GetArticleVersions(ctx, actor, articleID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := versionsToProto(versions)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetArticleVersionsResponse{Versions: converted}, nil
}

func (s *Server) GetAcknowledgements(ctx context.Context, request *kbv1.GetAcknowledgementsRequest) (*kbv1.GetAcknowledgementsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	acknowledgements, err := s.application.GetAcknowledgements(ctx, actor, articleID)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetAcknowledgementsResponse{
		Acknowledgements: acknowledgementsToProto(acknowledgements),
	}, nil
}

func (s *Server) AcknowledgeArticle(ctx context.Context, request *kbv1.AcknowledgeArticleRequest) (*kbv1.AcknowledgeArticleResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articleID, err := parseUUID(request.GetArticleId())
	if err != nil {
		return nil, err
	}
	if err = s.application.AcknowledgeArticle(ctx, actor, articleID); err != nil {
		return nil, transportError(err)
	}
	return &kbv1.AcknowledgeArticleResponse{}, nil
}

func (s *Server) SearchArticles(ctx context.Context, request *kbv1.SearchArticlesRequest) (*kbv1.SearchArticlesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	articles, err := s.application.SearchArticles(ctx, actor, request.GetQuery())
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articlesToProto(articles)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.SearchArticlesResponse{Articles: converted}, nil
}

func (s *Server) GetArticlesByIds(ctx context.Context, request *kbv1.GetArticlesByIdsRequest) (*kbv1.GetArticlesByIdsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := parseUUIDStrings(request.GetIds())
	if err != nil {
		return nil, invalidArgument("Некорректные идентификаторы статей")
	}
	articles, err := s.application.GetArticlesByIDs(ctx, actor, ids)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := articlesToProto(articles)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.GetArticlesByIdsResponse{Articles: converted}, nil
}

func (s *Server) ArticleExists(ctx context.Context, request *kbv1.ArticleExistsRequest) (*kbv1.ArticleExistsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	exists, err := s.application.ArticleExists(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &kbv1.ArticleExistsResponse{Exists: exists}, nil
}
