package transport

import (
	"net/http"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) CreateExternalCampaign(w http.ResponseWriter, r *http.Request, courseID api.CourseId, versionID api.VersionId) {
	var input api.CreateExternalCampaignInput
	if !decode(w, r, &input) {
		return
	}
	purpose, err := externalCampaignPurposeToProto(input.Purpose)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректная цель внешней кампании"))
		return
	}
	response, err := h.academy.CreateExternalCampaign(outgoingContext(r), &academyv1.CreateExternalCampaignRequest{
		CourseId: courseID.String(), CourseVersionId: versionID.String(), Name: input.Name,
		Purpose: purpose, DeadlineDays: uint32(input.DeadlineDays),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalCampaignCreatedFromProto(response.GetCreated())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) GetExternalCampaigns(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.GetExternalCampaigns(outgoingContext(r), &academyv1.GetExternalCampaignsRequest{
		CourseId: courseID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalCampaignsFromProto(response.GetCampaigns())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetExternalCampaign(w http.ResponseWriter, r *http.Request, campaignID api.CampaignId) {
	response, err := h.academy.GetExternalCampaign(outgoingContext(r), &academyv1.GetExternalCampaignRequest{
		CampaignId: campaignID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	h.writeExternalCampaign(w, r, response.GetCampaign())
}

func (h *Handler) PauseExternalCampaign(w http.ResponseWriter, r *http.Request, campaignID api.CampaignId) {
	response, err := h.academy.PauseExternalCampaign(outgoingContext(r), &academyv1.PauseExternalCampaignRequest{
		CampaignId: campaignID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	h.writeExternalCampaign(w, r, response.GetCampaign())
}

func (h *Handler) ResumeExternalCampaign(w http.ResponseWriter, r *http.Request, campaignID api.CampaignId) {
	response, err := h.academy.ResumeExternalCampaign(outgoingContext(r), &academyv1.ResumeExternalCampaignRequest{
		CampaignId: campaignID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	h.writeExternalCampaign(w, r, response.GetCampaign())
}

func (h *Handler) RotateExternalCampaignToken(w http.ResponseWriter, r *http.Request, campaignID api.CampaignId) {
	response, err := h.academy.RotateExternalCampaignToken(outgoingContext(r), &academyv1.RotateExternalCampaignTokenRequest{
		CampaignId: campaignID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalCampaignCreatedFromProto(response.GetCreated())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RevokeExternalCampaign(w http.ResponseWriter, r *http.Request, campaignID api.CampaignId) {
	response, err := h.academy.RevokeExternalCampaign(outgoingContext(r), &academyv1.RevokeExternalCampaignRequest{
		CampaignId: campaignID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	h.writeExternalCampaign(w, r, response.GetCampaign())
}

func (h *Handler) GetExternalCampaignReport(w http.ResponseWriter, r *http.Request, campaignID api.CampaignId) {
	response, err := h.academy.GetExternalCampaignReport(outgoingContext(r), &academyv1.GetExternalCampaignReportRequest{
		CampaignId: campaignID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := campaignReportFromProto(response.GetReport())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCourseExternalReport(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.GetCourseExternalReport(outgoingContext(r), &academyv1.GetCourseExternalReportRequest{
		CourseId: courseID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseExternalReportFromProto(response.GetReport())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetExternalLearnerTimeline(w http.ResponseWriter, r *http.Request, learnerID api.LearnerId) {
	response, err := h.academy.GetExternalLearnerTimeline(outgoingContext(r), &academyv1.GetExternalLearnerTimelineRequest{
		LearnerId: learnerID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalLearnerTimelineFromProto(response.GetTimeline())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) writeExternalCampaign(w http.ResponseWriter, r *http.Request, value *academyv1.ExternalCampaign) {
	converted, err := externalCampaignFromProto(value)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}
