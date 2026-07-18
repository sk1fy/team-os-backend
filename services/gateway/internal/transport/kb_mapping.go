package transport

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/structpb"
)

func sectionFromProto(value *kbv1.ArticleSection) (api.ArticleSection, error) {
	if value == nil {
		return api.ArticleSection{}, errors.New("kb returned an empty section")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ArticleSection{}, err
	}
	parent := nullable.NewNullNullable[string]()
	if value.ParentId != nil {
		if _, err = uuid.Parse(value.GetParentId()); err != nil {
			return api.ArticleSection{}, err
		}
		parent = nullable.NewNullableWithValue(value.GetParentId())
	}
	access, err := accessFromProto(value.GetAccess())
	if err != nil {
		return api.ArticleSection{}, err
	}
	visibility, err := sectionVisibilityFromProto(value.GetVisibility())
	if err != nil {
		return api.ArticleSection{}, err
	}
	return api.ArticleSection{
		Id: id, Name: value.GetName(), ParentId: parent,
		Order: int(value.GetOrder()), Access: access, Visibility: visibility,
	}, nil
}

func sectionVisibilityFromProto(value kbv1.SectionVisibility) (api.ArticleSectionVisibility, error) {
	switch value {
	case kbv1.SectionVisibility_SECTION_VISIBILITY_PUBLIC:
		return api.ArticleSectionVisibilityPublic, nil
	case kbv1.SectionVisibility_SECTION_VISIBILITY_COMPANY:
		return api.ArticleSectionVisibilityCompany, nil
	default:
		return "", fmt.Errorf("unknown section visibility %d", value)
	}
}

func sectionVisibilityToProto(value api.ArticleSectionVisibility) (kbv1.SectionVisibility, error) {
	switch value {
	case api.ArticleSectionVisibilityPublic:
		return kbv1.SectionVisibility_SECTION_VISIBILITY_PUBLIC, nil
	case api.ArticleSectionVisibilityCompany:
		return kbv1.SectionVisibility_SECTION_VISIBILITY_COMPANY, nil
	default:
		return kbv1.SectionVisibility_SECTION_VISIBILITY_UNSPECIFIED, fmt.Errorf("unknown section visibility %q", value)
	}
}

func sectionsFromProto(values []*kbv1.ArticleSection) ([]api.ArticleSection, error) {
	result := make([]api.ArticleSection, len(values))
	for index, value := range values {
		converted, err := sectionFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func articleFromProto(value *kbv1.Article) (api.Article, error) {
	if value == nil {
		return api.Article{}, errors.New("kb returned an empty article")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Article{}, err
	}
	sectionID, err := uuid.Parse(value.GetSectionId())
	if err != nil {
		return api.Article{}, err
	}
	authorID, err := uuid.Parse(value.GetAuthorId())
	if err != nil {
		return api.Article{}, err
	}
	content, err := richTextFromStruct(value.GetContent())
	if err != nil {
		return api.Article{}, err
	}
	status, err := articleStatusFromProto(value.GetStatus())
	if err != nil {
		return api.Article{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	updatedAt := time.Time{}
	if value.GetUpdatedAt() != nil {
		updatedAt = value.GetUpdatedAt().AsTime()
	}
	return api.Article{
		Id: id, SectionId: sectionID, Title: value.GetTitle(), Content: content,
		Status: status, AuthorId: authorID, Version: int(value.GetVersion()),
		RequiresAcknowledgement: value.GetRequiresAcknowledgement(),
		CreatedAt:               createdAt, UpdatedAt: updatedAt,
	}, nil
}

func articlesFromProto(values []*kbv1.Article) ([]api.Article, error) {
	result := make([]api.Article, len(values))
	for index, value := range values {
		converted, err := articleFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func articleVersionFromProto(value *kbv1.ArticleVersion) (api.ArticleVersion, error) {
	if value == nil {
		return api.ArticleVersion{}, errors.New("kb returned an empty article version")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.ArticleVersion{}, err
	}
	articleID, err := uuid.Parse(value.GetArticleId())
	if err != nil {
		return api.ArticleVersion{}, err
	}
	authorID, err := uuid.Parse(value.GetAuthorId())
	if err != nil {
		return api.ArticleVersion{}, err
	}
	content, err := richTextFromStruct(value.GetContent())
	if err != nil {
		return api.ArticleVersion{}, err
	}
	createdAt := time.Time{}
	if value.GetCreatedAt() != nil {
		createdAt = value.GetCreatedAt().AsTime()
	}
	return api.ArticleVersion{
		Id: id, ArticleId: articleID, Version: int(value.GetVersion()),
		Title: value.GetTitle(), Content: content, AuthorId: authorID, CreatedAt: createdAt,
	}, nil
}

func articleVersionsFromProto(values []*kbv1.ArticleVersion) ([]api.ArticleVersion, error) {
	result := make([]api.ArticleVersion, len(values))
	for index, value := range values {
		converted, err := articleVersionFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func acknowledgementFromProto(value *kbv1.Acknowledgement) (api.Acknowledgement, error) {
	if value == nil {
		return api.Acknowledgement{}, errors.New("kb returned an empty acknowledgement")
	}
	articleID, err := uuid.Parse(value.GetArticleId())
	if err != nil {
		return api.Acknowledgement{}, err
	}
	userID, err := uuid.Parse(value.GetUserId())
	if err != nil {
		return api.Acknowledgement{}, err
	}
	acknowledgedAt := time.Time{}
	if value.GetAcknowledgedAt() != nil {
		acknowledgedAt = value.GetAcknowledgedAt().AsTime()
	}
	return api.Acknowledgement{
		ArticleId: articleID, UserId: userID, AcknowledgedAt: acknowledgedAt,
	}, nil
}

func acknowledgementsFromProto(values []*kbv1.Acknowledgement) ([]api.Acknowledgement, error) {
	result := make([]api.Acknowledgement, len(values))
	for index, value := range values {
		converted, err := acknowledgementFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func accessFromProto(value *kbv1.AccessSettings) (api.AccessSettings, error) {
	if value == nil {
		return api.AccessSettings{}, errors.New("kb returned empty access settings")
	}
	scope, err := accessScopeFromProto(value.GetScope())
	if err != nil {
		return api.AccessSettings{}, err
	}
	departmentIDs, err := UUIDsFromStrings(value.GetDepartmentIds())
	if err != nil {
		return api.AccessSettings{}, err
	}
	positionIDs, err := UUIDsFromStrings(value.GetPositionIds())
	if err != nil {
		return api.AccessSettings{}, err
	}
	userIDs, err := UUIDsFromStrings(value.GetUserIds())
	if err != nil {
		return api.AccessSettings{}, err
	}
	return api.AccessSettings{
		Scope: scope, DepartmentIds: departmentIDs,
		PositionIds: positionIDs, UserIds: userIDs,
	}, nil
}

func accessToProto(value api.AccessSettings) (*kbv1.AccessSettings, error) {
	scope, err := accessScopeToProto(value.Scope)
	if err != nil {
		return nil, err
	}
	return &kbv1.AccessSettings{
		Scope:         scope,
		DepartmentIds: idStrings(value.DepartmentIds),
		PositionIds:   idStrings(value.PositionIds),
		UserIds:       idStrings(value.UserIds),
	}, nil
}

func richTextToStruct(value api.RichTextContent) (*structpb.Struct, error) {
	payload := map[string]any{"type": value.Type}
	if value.Content != nil {
		payload["content"] = *value.Content
	}
	return structpb.NewStruct(payload)
}

func richTextFromStruct(value *structpb.Struct) (api.RichTextContent, error) {
	if value == nil {
		return api.RichTextContent{}, errors.New("kb returned empty rich text")
	}
	raw := value.AsMap()
	result := api.RichTextContent{Type: "doc"}
	if typeName, ok := raw["type"].(string); ok && typeName != "" {
		result.Type = typeName
	}
	if content, ok := raw["content"].([]any); ok {
		result.Content = &content
	}
	return result, nil
}

func articleStatusFromProto(status kbv1.ArticleStatus) (api.ArticleStatus, error) {
	switch status {
	case kbv1.ArticleStatus_ARTICLE_STATUS_DRAFT:
		return api.ArticleStatusDraft, nil
	case kbv1.ArticleStatus_ARTICLE_STATUS_PUBLISHED:
		return api.ArticleStatusPublished, nil
	default:
		return "", fmt.Errorf("unsupported article status: %v", status)
	}
}

func articleStatusToProto(status api.ArticleStatus) (kbv1.ArticleStatus, error) {
	switch status {
	case api.ArticleStatusDraft:
		return kbv1.ArticleStatus_ARTICLE_STATUS_DRAFT, nil
	case api.ArticleStatusPublished:
		return kbv1.ArticleStatus_ARTICLE_STATUS_PUBLISHED, nil
	default:
		return kbv1.ArticleStatus_ARTICLE_STATUS_UNSPECIFIED, fmt.Errorf("unsupported article status: %q", status)
	}
}

func accessScopeFromProto(scope kbv1.AccessScope) (api.AccessScope, error) {
	switch scope {
	case kbv1.AccessScope_ACCESS_SCOPE_COMPANY:
		return api.AccessScopeCompany, nil
	case kbv1.AccessScope_ACCESS_SCOPE_CUSTOM:
		return api.AccessScopeCustom, nil
	default:
		return "", fmt.Errorf("unsupported access scope: %v", scope)
	}
}

func accessScopeToProto(scope api.AccessScope) (kbv1.AccessScope, error) {
	switch scope {
	case api.AccessScopeCompany:
		return kbv1.AccessScope_ACCESS_SCOPE_COMPANY, nil
	case api.AccessScopeCustom:
		return kbv1.AccessScope_ACCESS_SCOPE_CUSTOM, nil
	default:
		return kbv1.AccessScope_ACCESS_SCOPE_UNSPECIFIED, fmt.Errorf("unsupported access scope: %q", scope)
	}
}

func idStrings(values []api.ID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
