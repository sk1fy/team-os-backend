package transport

import (
	"errors"
	"net/http"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) GetCourseTemplates(w http.ResponseWriter, r *http.Request, params api.GetCourseTemplatesParams) {
	request := &academyv1.GetCourseTemplatesRequest{}
	if params.Q != nil {
		request.Query = params.Q
	}
	if params.Page != nil {
		if *params.Page < 1 {
			apierror.Write(w, apierror.BadRequest("Номер страницы должен быть не меньше 1"))
			return
		}
		request.Page = uint32(*params.Page)
	}
	if params.PageSize != nil {
		if *params.PageSize < 1 || *params.PageSize > 100 {
			apierror.Write(w, apierror.BadRequest("Размер страницы должен быть от 1 до 100"))
			return
		}
		request.PageSize = uint32(*params.PageSize)
	}
	if params.Type != nil {
		value, err := courseTemplateTypeToProto(*params.Type)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный тип шаблона курса"))
			return
		}
		request.Type = &value
	}
	if params.OwnerType != nil {
		ownerType := api.CourseTemplateType(*params.OwnerType)
		if params.Type != nil && *params.Type != ownerType {
			apierror.Write(w, apierror.BadRequest("Параметры type и ownerType противоречат друг другу"))
			return
		}
		value, err := courseTemplateTypeToProto(ownerType)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный тип владельца шаблона"))
			return
		}
		request.Type = &value
	}
	if params.Lifecycle != nil {
		value, err := courseTemplateLifecycleToProto(*params.Lifecycle)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное состояние шаблона курса"))
			return
		}
		request.LifecycleStatus = &value
	}
	response, err := h.academy.GetCourseTemplates(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := academyTemplateSummariesFromProto(response.GetItems())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.AcademyTemplatePage{
		Items: converted, Page: int(response.GetPage()), PageSize: int(response.GetPageSize()),
		Total: int(response.GetTotal()), TotalPages: int(response.GetTotalPages()),
	})
}

func (h *Handler) GetCourseTemplate(w http.ResponseWriter, r *http.Request, templateID api.TemplateId, params api.GetCourseTemplateParams) {
	request := &academyv1.GetCourseTemplateRequest{TemplateId: templateID.String()}
	if params.VersionId != nil {
		value := params.VersionId.String()
		request.VersionId = &value
	}
	response, err := h.academy.GetCourseTemplate(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseTemplateDetailsFromProto(response)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateCourseTemplate(w http.ResponseWriter, r *http.Request) {
	var input api.CreateCourseTemplateInput
	if !decode(w, r, &input) {
		return
	}
	content, err := courseTemplateContentToProto(input.Content)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректное содержимое шаблона курса"))
		return
	}
	request := &academyv1.CreateCourseTemplateRequest{
		Title: input.Title, Description: input.Description, Sequential: input.Sequential, Content: content,
	}
	if input.CoverFileId != nil {
		value := input.CoverFileId.String()
		request.CoverFileId = &value
	}
	response, err := h.academy.CreateCourseTemplate(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	template, err := courseTemplateFromProto(response.GetTemplate())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	draft, err := courseTemplateVersionFromProto(response.GetDraft(), nil)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	template.SelectedVersion = &draft
	writeJSON(w, http.StatusCreated, template)
}

func (h *Handler) UpdateCourseTemplateDraft(w http.ResponseWriter, r *http.Request, templateID api.TemplateId) {
	var input api.UpdateCourseTemplateDraftInput
	if !decode(w, r, &input) {
		return
	}
	content, err := courseTemplateContentToProto(input.Content)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректное содержимое шаблона курса"))
		return
	}
	request := &academyv1.UpdateCourseTemplateDraftRequest{
		TemplateId: templateID.String(), Title: input.Title, Description: input.Description,
		Sequential: input.Sequential, Content: content,
	}
	if input.CoverFileId != nil {
		value := input.CoverFileId.String()
		request.CoverFileId = &value
	}
	response, err := h.academy.UpdateCourseTemplateDraft(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	draft := response.GetDraft()
	converted, err := courseTemplateVersionFromProto(draft.GetVersion(), draft.GetContent())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateCourseTemplateDraft(w http.ResponseWriter, r *http.Request, templateID api.TemplateId) {
	response, err := h.academy.CreateCourseTemplateDraft(outgoingContext(r), &academyv1.CreateCourseTemplateDraftRequest{TemplateId: templateID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	draft := response.GetDraft()
	converted, err := courseTemplateVersionFromProto(draft.GetVersion(), draft.GetContent())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) PublishCourseTemplateVersion(w http.ResponseWriter, r *http.Request, templateID api.TemplateId, params api.PublishCourseTemplateVersionParams) {
	response, err := h.academy.PublishCourseTemplateVersion(outgoingContext(r), &academyv1.PublishCourseTemplateVersionRequest{
		TemplateId: templateID.String(), IdempotencyKey: string(params.IdempotencyKey),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseTemplateVersionFromProto(response.GetVersion(), nil)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) ArchiveCourseTemplate(w http.ResponseWriter, r *http.Request, templateID api.TemplateId) {
	response, err := h.academy.ArchiveCourseTemplate(outgoingContext(r), &academyv1.ArchiveCourseTemplateRequest{TemplateId: templateID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseTemplateFromProto(response.GetTemplate())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) InstantiateCourseTemplateVersion(w http.ResponseWriter, r *http.Request, versionID api.VersionId, params api.InstantiateCourseTemplateVersionParams) {
	response, err := h.academy.InstantiateCourseTemplateVersion(outgoingContext(r), &academyv1.InstantiateCourseTemplateVersionRequest{
		VersionId: versionID.String(), IdempotencyKey: string(params.IdempotencyKey),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	value := response.GetResult()
	if value == nil {
		h.writeConversionError(w, r, errors.New("Academy вернул пустой результат применения шаблона"))
		return
	}
	course, err := courseFromProto(value.GetCourse())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	draft, err := courseVersionFromProto(value.GetDraft())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	origin, err := courseOriginFromProto(value.GetOrigin())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.TemplateInstantiationResult{Course: course, Draft: draft, Origin: origin})
}
