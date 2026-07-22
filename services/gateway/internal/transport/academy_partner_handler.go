package transport

import (
	"net/http"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) GetPartnerCourseGroups(w http.ResponseWriter, r *http.Request, params api.GetPartnerCourseGroupsParams) {
	request := &academyv1.GetPartnerCourseGroupsRequest{}
	if params.Lifecycle != nil {
		value, err := courseLifecycleToProto(*params.Lifecycle)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное состояние курса"))
			return
		}
		request.Lifecycle = &value
	}
	if params.Distribution != nil {
		value, err := courseDistributionToProto(*params.Distribution)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное состояние распространения курса"))
			return
		}
		request.Distribution = &value
	}
	response, err := h.academy.GetPartnerCourseGroups(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := partnerCourseGroupsFromProto(response.GetGroups())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetPartnerCoursesReport(w http.ResponseWriter, r *http.Request, partnerID api.PartnerId) {
	response, err := h.academy.GetPartnerCoursesReport(outgoingContext(r), &academyv1.GetPartnerCoursesReportRequest{
		PartnerId: partnerID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := partnerCoursesReportFromProto(response.GetReport())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCourseVersionPreview(w http.ResponseWriter, r *http.Request, courseID api.CourseId, versionID api.VersionId) {
	response, err := h.academy.GetCourseVersionPreview(outgoingContext(r), &academyv1.GetCourseVersionPreviewRequest{
		CourseId: courseID.String(), VersionId: versionID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionPreviewFromProto(response.GetPreview())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) SubmitCoursePreviewQuizAttempt(w http.ResponseWriter, r *http.Request, courseID api.CourseId, versionID api.VersionId, quizVersionID api.ID) {
	var input api.SubmitCoursePreviewQuizAttemptInput
	if !decode(w, r, &input) {
		return
	}
	answers := make([]*academyv1.EnrollmentQuizAnswer, len(input.Answers))
	for index, answer := range input.Answers {
		converted := &academyv1.EnrollmentQuizAnswer{QuestionId: answer.QuestionId.String(), Text: answer.Text}
		if answer.OptionIds != nil {
			converted.SelectedOptionIds = make([]string, len(*answer.OptionIds))
			for optionIndex, optionID := range *answer.OptionIds {
				converted.SelectedOptionIds[optionIndex] = optionID.String()
			}
		}
		answers[index] = converted
	}
	response, err := h.academy.SubmitCoursePreviewQuizAttempt(outgoingContext(r), &academyv1.SubmitCoursePreviewQuizAttemptRequest{
		CourseId: courseID.String(), VersionId: versionID.String(), QuizVersionId: quizVersionID.String(), Answers: answers,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := coursePreviewQuizAttemptResultFromProto(response.GetResult())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) PausePartnerCourseDistribution(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	h.applyPartnerRestriction(w, r, courseID, "pause")
}

func (h *Handler) BlockPartnerCourse(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	h.applyPartnerRestriction(w, r, courseID, "block")
}

func (h *Handler) ResolvePartnerCourseRestriction(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	h.applyPartnerRestriction(w, r, courseID, "resolve")
}

func (h *Handler) applyPartnerRestriction(w http.ResponseWriter, r *http.Request, courseID api.CourseId, command string) {
	var input api.CourseRestrictionInput
	if !decode(w, r, &input) {
		return
	}
	var responseRestriction *academyv1.CourseRestriction
	var err error
	switch command {
	case "pause":
		response, callErr := h.academy.PausePartnerCourseDistribution(outgoingContext(r), &academyv1.PausePartnerCourseDistributionRequest{
			CourseId: courseID.String(), Reason: input.Reason,
		})
		err = callErr
		if response != nil {
			responseRestriction = response.GetRestriction()
		}
	case "block":
		response, callErr := h.academy.BlockPartnerCourse(outgoingContext(r), &academyv1.BlockPartnerCourseRequest{
			CourseId: courseID.String(), Reason: input.Reason,
		})
		err = callErr
		if response != nil {
			responseRestriction = response.GetRestriction()
		}
	default:
		response, callErr := h.academy.ResolvePartnerCourseRestriction(outgoingContext(r), &academyv1.ResolvePartnerCourseRestrictionRequest{
			CourseId: courseID.String(), Reason: input.Reason,
		})
		err = callErr
		if response != nil {
			responseRestriction = response.GetRestriction()
		}
	}
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseRestrictionFromProto(responseRestriction)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	status := http.StatusCreated
	if command == "resolve" {
		status = http.StatusOK
	}
	writeJSON(w, status, converted)
}

func (h *Handler) GetCourseRestrictions(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.GetCourseRestrictions(outgoingContext(r), &academyv1.GetCourseRestrictionsRequest{
		CourseId: courseID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseRestrictionsFromProto(response.GetRestrictions())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CopyPartnerCourseVersionToCompany(w http.ResponseWriter, r *http.Request, courseID api.CourseId, versionID api.VersionId, params api.CopyPartnerCourseVersionToCompanyParams) {
	response, err := h.academy.CopyPartnerCourseVersionToCompany(outgoingContext(r), &academyv1.CopyPartnerCourseVersionToCompanyRequest{
		CourseId: courseID.String(), VersionId: versionID.String(), IdempotencyKey: string(params.IdempotencyKey),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := partnerCourseCopyResultFromProto(response.GetResult())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}
