package transport

import (
	"net/http"
	"strconv"

	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *Handler) GetSections(w http.ResponseWriter, r *http.Request) {
	response, err := h.kb.GetSections(outgoingContext(r), &kbv1.GetSectionsRequest{})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := sectionsFromProto(response.GetSections())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateSection(w http.ResponseWriter, r *http.Request) {
	var input api.CreateArticleSectionInput
	if !decode(w, r, &input) {
		return
	}
	if !input.ParentId.IsSpecified() {
		apierror.Write(w, apierror.BadRequest("Поле parentId обязательно"))
		return
	}
	request := &kbv1.CreateSectionRequest{Name: input.Name}
	if !input.ParentId.IsNull() {
		value, err := input.ParentId.Get()
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный parentId"))
			return
		}
		request.ParentId = &value
	}
	if input.Access != nil {
		access, err := accessToProto(*input.Access)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Access = access
	}
	response, err := h.kb.CreateSection(outgoingContext(r), request)
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := sectionFromProto(response.GetSection())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateSection(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateArticleSectionInput
	if !decode(w, r, &input) {
		return
	}
	request := &kbv1.UpdateSectionRequest{Id: id.String(), Name: input.Name}
	if input.Access != nil {
		access, err := accessToProto(*input.Access)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Access = access
	}
	response, err := h.kb.UpdateSection(outgoingContext(r), request)
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := sectionFromProto(response.GetSection())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) DeleteSection(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.kb.DeleteSection(outgoingContext(r), &kbv1.DeleteSectionRequest{Id: id.String()})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetArticles(w http.ResponseWriter, r *http.Request, params api.GetArticlesParams) {
	request := &kbv1.GetArticlesRequest{}
	if params.SectionId != nil {
		sectionID := params.SectionId.String()
		request.SectionId = &sectionID
	}
	response, err := h.kb.GetArticles(outgoingContext(r), request)
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articlesFromProto(response.GetArticles())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetArticle(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.kb.GetArticle(outgoingContext(r), &kbv1.GetArticleRequest{Id: id.String()})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articleFromProto(response.GetArticle())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateArticle(w http.ResponseWriter, r *http.Request) {
	var input api.CreateArticleInput
	if !decode(w, r, &input) {
		return
	}
	content, err := richTextToStruct(input.Content)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	status, err := articleStatusToProto(input.Status)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	response, err := h.kb.CreateArticle(outgoingContext(r), &kbv1.CreateArticleRequest{
		SectionId: input.SectionId.String(), Title: input.Title, Content: content,
		Status: status, RequiresAcknowledgement: input.RequiresAcknowledgement,
	})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articleFromProto(response.GetArticle())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateArticle(w http.ResponseWriter, r *http.Request, id api.Id, params api.UpdateArticleParams) {
	var input api.UpdateArticleInput
	if !decode(w, r, &input) {
		return
	}
	request := &kbv1.UpdateArticleRequest{Id: id.String()}
	if input.SectionId != nil {
		sectionID := input.SectionId.String()
		request.SectionId = &sectionID
	}
	if input.Title != nil {
		request.Title = input.Title
	}
	if input.Content != nil {
		content, err := richTextToStruct(*input.Content)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Content = content
	}
	if input.Status != nil {
		status, err := articleStatusToProto(*input.Status)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Status = &status
	}
	if input.RequiresAcknowledgement != nil {
		request.RequiresAcknowledgement = input.RequiresAcknowledgement
	}
	if params.IfMatch != nil {
		version := uint32(*params.IfMatch)
		request.ExpectedVersion = &version
	}
	response, err := h.kb.UpdateArticle(outgoingContext(r), request)
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articleFromProto(response.GetArticle())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RollbackArticle(w http.ResponseWriter, r *http.Request, articleId api.Id) {
	var input api.RollbackArticleInput
	if !decode(w, r, &input) {
		return
	}
	request := &kbv1.RollbackArticleRequest{
		ArticleId: articleId.String(), VersionId: input.VersionId.String(),
	}
	if header := r.Header.Get("If-Match"); header != "" {
		parsed, err := strconv.Atoi(header)
		if err != nil || parsed < 1 {
			apierror.Write(w, apierror.BadRequest("Некорректный заголовок If-Match"))
			return
		}
		version := uint32(parsed)
		request.ExpectedVersion = &version
	}
	response, err := h.kb.RollbackArticle(outgoingContext(r), request)
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articleFromProto(response.GetArticle())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetArticleVersions(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.kb.GetArticleVersions(outgoingContext(r), &kbv1.GetArticleVersionsRequest{ArticleId: id.String()})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articleVersionsFromProto(response.GetVersions())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetAcknowledgements(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.kb.GetAcknowledgements(outgoingContext(r), &kbv1.GetAcknowledgementsRequest{ArticleId: id.String()})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := acknowledgementsFromProto(response.GetAcknowledgements())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) AcknowledgeArticle(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.kb.AcknowledgeArticle(outgoingContext(r), &kbv1.AcknowledgeArticleRequest{ArticleId: id.String()})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SearchArticles(w http.ResponseWriter, r *http.Request, params api.SearchArticlesParams) {
	response, err := h.kb.SearchArticles(outgoingContext(r), &kbv1.SearchArticlesRequest{Query: params.Q})
	if err != nil {
		h.writeKbRPCError(w, r, err)
		return
	}
	converted, err := articlesFromProto(response.GetArticles())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) writeKbRPCError(w http.ResponseWriter, r *http.Request, err error) {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		h.logger.ErrorContext(r.Context(), "kb RPC failed", "error", err)
		apierror.Write(w, apierror.Internal(err))
		return
	}
	message := grpcStatus.Message()
	switch grpcStatus.Code() {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		apierror.Write(w, apierror.BadRequest(message))
	case codes.Unauthenticated:
		apierror.Write(w, apierror.Unauthorized(message))
	case codes.PermissionDenied:
		apierror.Write(w, apierror.Forbidden(message))
	case codes.NotFound:
		apierror.Write(w, apierror.New(http.StatusNotFound, message))
	case codes.AlreadyExists, codes.Aborted:
		apierror.Write(w, apierror.Conflict(message))
	case codes.Unavailable, codes.DeadlineExceeded:
		h.logger.WarnContext(r.Context(), "kb RPC unavailable", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис временно недоступен"))
	default:
		h.logger.ErrorContext(r.Context(), "kb RPC failed", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.Internal(err))
	}
}