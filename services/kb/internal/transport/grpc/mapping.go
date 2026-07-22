package grpc

import (
	"encoding/json"

	"github.com/google/uuid"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	"github.com/sk1fy/team-os-backend/services/kb/internal/application"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sectionToProto(value application.Section) *kbv1.ArticleSection {
	return &kbv1.ArticleSection{
		Id: value.ID.String(), Name: value.Name,
		ParentId:   optionalUUIDString(value.ParentID),
		Order:      uint32(max(0, value.Order)),
		Access:     accessToProto(value.Access),
		Visibility: sectionVisibilityToProto(value.Visibility),
	}
}

func sectionVisibilityToProto(value string) kbv1.SectionVisibility {
	if value == "public" {
		return kbv1.SectionVisibility_SECTION_VISIBILITY_PUBLIC
	}
	if value == "company" {
		return kbv1.SectionVisibility_SECTION_VISIBILITY_COMPANY
	}
	return kbv1.SectionVisibility_SECTION_VISIBILITY_UNSPECIFIED
}

func sectionVisibilityFromProto(value kbv1.SectionVisibility) (string, error) {
	switch value {
	case kbv1.SectionVisibility_SECTION_VISIBILITY_PUBLIC:
		return "public", nil
	case kbv1.SectionVisibility_SECTION_VISIBILITY_COMPANY:
		return "company", nil
	default:
		return "", invalidArgument("Некорректная видимость раздела")
	}
}

func sectionsToProto(values []application.Section) []*kbv1.ArticleSection {
	result := make([]*kbv1.ArticleSection, len(values))
	for index := range values {
		result[index] = sectionToProto(values[index])
	}
	return result
}

func articleToProto(value application.Article) (*kbv1.Article, error) {
	content, err := contentToStruct(value.Content)
	if err != nil {
		return nil, err
	}
	result := &kbv1.Article{
		Id: value.ID.String(), SectionId: value.SectionID.String(),
		Title: value.Title, Content: content, Status: articleStatusToProto(value.Status),
		AuthorId: value.AuthorID.String(), Version: uint32(value.Version),
		RequiresAcknowledgement: value.RequiresAcknowledgement,
		CreatedAt:               timestamppb.New(value.CreatedAt.UTC()),
		UpdatedAt:               timestamppb.New(value.UpdatedAt.UTC()),
	}
	if value.PartnerAccess.Mode != "" {
		result.PartnerAccess = partnerAccessToProto(value.PartnerAccess)
	}
	if value.PartnerReusePolicy != "" {
		policy, mapErr := partnerReusePolicyToProto(value.PartnerReusePolicy)
		if mapErr != nil {
			return nil, mapErr
		}
		result.PartnerReusePolicy = &policy
	}
	return result, nil
}

func partnerAccessToProto(value application.PartnerAccessSettings) *kbv1.PartnerAccessSettings {
	mode := kbv1.PartnerAccessMode_PARTNER_ACCESS_MODE_NONE
	switch value.Mode {
	case "all":
		mode = kbv1.PartnerAccessMode_PARTNER_ACCESS_MODE_ALL
	case "selected":
		mode = kbv1.PartnerAccessMode_PARTNER_ACCESS_MODE_SELECTED
	}
	return &kbv1.PartnerAccessSettings{Mode: mode, PartnerIds: uuidStrings(value.PartnerIDs)}
}

func partnerAccessFromProto(value *kbv1.PartnerAccessSettings) (application.PartnerAccessSettings, error) {
	if value == nil {
		return application.PartnerAccessSettings{}, invalidArgument("Укажите настройки доступа партнёров")
	}
	mode := ""
	switch value.GetMode() {
	case kbv1.PartnerAccessMode_PARTNER_ACCESS_MODE_NONE:
		mode = "none"
	case kbv1.PartnerAccessMode_PARTNER_ACCESS_MODE_ALL:
		mode = "all"
	case kbv1.PartnerAccessMode_PARTNER_ACCESS_MODE_SELECTED:
		mode = "selected"
	default:
		return application.PartnerAccessSettings{}, invalidArgument("Некорректный режим доступа партнёров")
	}
	partnerIDs, err := parseUUIDStrings(value.GetPartnerIds())
	if err != nil {
		return application.PartnerAccessSettings{}, invalidArgument("Некорректный идентификатор партнёра")
	}
	return application.PartnerAccessSettings{Mode: mode, PartnerIDs: partnerIDs}, nil
}

func partnerReusePolicyFromProto(value kbv1.PartnerReusePolicy) (string, error) {
	switch value {
	case kbv1.PartnerReusePolicy_PARTNER_REUSE_POLICY_NOT_ALLOWED:
		return "not_allowed", nil
	case kbv1.PartnerReusePolicy_PARTNER_REUSE_POLICY_COPY_ALLOWED:
		return "copy_allowed", nil
	default:
		return "", invalidArgument("Некорректная политика повторного использования статьи")
	}
}

func partnerReusePolicyToProto(value string) (kbv1.PartnerReusePolicy, error) {
	switch value {
	case "not_allowed":
		return kbv1.PartnerReusePolicy_PARTNER_REUSE_POLICY_NOT_ALLOWED, nil
	case "copy_allowed":
		return kbv1.PartnerReusePolicy_PARTNER_REUSE_POLICY_COPY_ALLOWED, nil
	default:
		return kbv1.PartnerReusePolicy_PARTNER_REUSE_POLICY_UNSPECIFIED, invalidArgument("Некорректная политика повторного использования статьи")
	}
}

func articlePartnerPolicyToProto(value application.ArticlePartnerPolicy) (*kbv1.ArticlePartnerPolicy, error) {
	reusePolicy, err := partnerReusePolicyToProto(value.ReusePolicy)
	if err != nil {
		return nil, err
	}
	result := &kbv1.ArticlePartnerPolicy{
		ArticleId: value.ArticleID.String(), Access: partnerAccessToProto(value.Access),
		ReusePolicy: reusePolicy, UpdatedAt: timestamppb.New(value.UpdatedAt.UTC()),
	}
	if value.UpdatedByID != nil {
		updatedBy := value.UpdatedByID.String()
		result.UpdatedById = &updatedBy
	}
	return result, nil
}

func courseCopyPermissionToProto(value application.ArticleCourseCopyPermission) (*kbv1.CheckArticleCourseCopyPermissionResponse, error) {
	reusePolicy, err := partnerReusePolicyToProto(value.ReusePolicy)
	if err != nil {
		return nil, err
	}
	result := &kbv1.CheckArticleCourseCopyPermissionResponse{
		CanRead: value.CanRead, CanCopy: value.CanCopy, ReusePolicy: reusePolicy,
	}
	if value.DenialReason != "" {
		result.DenialReason = &value.DenialReason
	}
	if value.ResolvedArticleVersionID != nil {
		resolvedID := value.ResolvedArticleVersionID.String()
		result.ResolvedArticleVersionId = &resolvedID
	}
	return result, nil
}

func articleSnapshotToProto(value application.ArticleSnapshotForCourseCopy) (*kbv1.ArticleSnapshotForCourseCopy, error) {
	content, err := contentToStruct(value.Content)
	if err != nil {
		return nil, err
	}
	attachments := make([]*kbv1.ArticleSnapshotAttachment, 0, len(value.Attachments))
	for _, attachment := range value.Attachments {
		attachments = append(attachments, &kbv1.ArticleSnapshotAttachment{FileId: attachment.FileID.String()})
	}
	return &kbv1.ArticleSnapshotForCourseCopy{
		ArticleId: value.ArticleID.String(), ArticleVersionId: value.ArticleVersionID.String(),
		Version: uint32(value.Version), Title: value.Title, Content: content,
		Attachments: attachments, ContentHash: value.ContentHash,
		CapturedAt: timestamppb.New(value.CapturedAt.UTC()),
	}, nil
}

func articlesToProto(values []application.Article) ([]*kbv1.Article, error) {
	result := make([]*kbv1.Article, len(values))
	for index, value := range values {
		converted, err := articleToProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func versionToProto(value application.ArticleVersion) (*kbv1.ArticleVersion, error) {
	content, err := contentToStruct(value.Content)
	if err != nil {
		return nil, err
	}
	return &kbv1.ArticleVersion{
		Id: value.ID.String(), ArticleId: value.ArticleID.String(),
		Version: uint32(value.Version), Title: value.Title, Content: content,
		AuthorId: value.AuthorID.String(), CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
	}, nil
}

func versionsToProto(values []application.ArticleVersion) ([]*kbv1.ArticleVersion, error) {
	result := make([]*kbv1.ArticleVersion, len(values))
	for index, value := range values {
		converted, err := versionToProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func acknowledgementToProto(value application.Acknowledgement) *kbv1.Acknowledgement {
	return &kbv1.Acknowledgement{
		ArticleId: value.ArticleID.String(), UserId: value.UserID.String(),
		AcknowledgedAt: timestamppb.New(value.AcknowledgedAt.UTC()),
	}
}

func acknowledgementsToProto(values []application.Acknowledgement) []*kbv1.Acknowledgement {
	result := make([]*kbv1.Acknowledgement, len(values))
	for index := range values {
		result[index] = acknowledgementToProto(values[index])
	}
	return result
}

func accessToProto(value application.AccessSettings) *kbv1.AccessSettings {
	return &kbv1.AccessSettings{
		Scope:         accessScopeToProto(value.Scope),
		DepartmentIds: uuidStrings(value.DepartmentIDs),
		PositionIds:   uuidStrings(value.PositionIDs),
		UserIds:       uuidStrings(value.UserIDs),
	}
}

func accessFromProto(value *kbv1.AccessSettings) (application.AccessSettings, error) {
	if value == nil {
		return application.AccessSettings{}, invalidArgument("Некорректные настройки доступа")
	}
	scope, err := accessScopeFromProto(value.GetScope())
	if err != nil {
		return application.AccessSettings{}, err
	}
	departmentIDs, err := parseUUIDStrings(value.GetDepartmentIds())
	if err != nil {
		return application.AccessSettings{}, invalidArgument("Некорректные departmentIds")
	}
	positionIDs, err := parseUUIDStrings(value.GetPositionIds())
	if err != nil {
		return application.AccessSettings{}, invalidArgument("Некорректные positionIds")
	}
	userIDs, err := parseUUIDStrings(value.GetUserIds())
	if err != nil {
		return application.AccessSettings{}, invalidArgument("Некорректные userIds")
	}
	return application.AccessSettings{
		Scope: scope, DepartmentIDs: departmentIDs,
		PositionIDs: positionIDs, UserIDs: userIDs,
	}, nil
}

func contentFromStruct(value *structpb.Struct) (json.RawMessage, error) {
	if value == nil {
		return nil, invalidArgument("Некорректное содержимое статьи")
	}
	raw, err := value.MarshalJSON()
	if err != nil {
		return nil, invalidArgument("Некорректное содержимое статьи")
	}
	return raw, nil
}

func contentToStruct(raw json.RawMessage) (*structpb.Struct, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return structpb.NewStruct(payload)
}

func articleStatusToProto(status string) kbv1.ArticleStatus {
	switch status {
	case "published":
		return kbv1.ArticleStatus_ARTICLE_STATUS_PUBLISHED
	default:
		return kbv1.ArticleStatus_ARTICLE_STATUS_DRAFT
	}
}

func articleStatusFromProto(status kbv1.ArticleStatus) (string, error) {
	switch status {
	case kbv1.ArticleStatus_ARTICLE_STATUS_DRAFT:
		return "draft", nil
	case kbv1.ArticleStatus_ARTICLE_STATUS_PUBLISHED:
		return "published", nil
	default:
		return "", invalidArgument("Некорректный статус статьи")
	}
}

func accessScopeToProto(scope domainaccess.Scope) kbv1.AccessScope {
	if scope == domainaccess.ScopeCustom {
		return kbv1.AccessScope_ACCESS_SCOPE_CUSTOM
	}
	return kbv1.AccessScope_ACCESS_SCOPE_COMPANY
}

func accessScopeFromProto(scope kbv1.AccessScope) (domainaccess.Scope, error) {
	switch scope {
	case kbv1.AccessScope_ACCESS_SCOPE_COMPANY:
		return domainaccess.ScopeCompany, nil
	case kbv1.AccessScope_ACCESS_SCOPE_CUSTOM:
		return domainaccess.ScopeCustom, nil
	default:
		return "", invalidArgument("Некорректные настройки доступа")
	}
}

func parseUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalidArgument("Некорректный идентификатор")
	}
	return parsed, nil
}

func parseOptionalUUID(value *string) (*uuid.UUID, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed, err := parseUUID(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseUUIDStrings(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func optionalUUIDString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
