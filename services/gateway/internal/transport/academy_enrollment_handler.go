package transport

import (
	"net/http"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/structpb"
)

func (h *Handler) GetAcademyEnrollments(w http.ResponseWriter, r *http.Request, params api.GetAcademyEnrollmentsParams) {
	request := &academyv1.GetEnrollmentsRequest{}
	if params.CourseId != nil {
		value := params.CourseId.String()
		request.CourseId = &value
	}
	if params.CourseVersionId != nil {
		value := params.CourseVersionId.String()
		request.CourseVersionId = &value
	}
	if params.UserId != nil {
		value := params.UserId.String()
		request.UserId = &value
	}
	if params.ProgressStatus != nil {
		value, err := enrollmentFilterProgressToProto(*params.ProgressStatus)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное состояние прогресса"))
			return
		}
		request.ProgressStatus = &value
	}
	if params.AccessStatus != nil {
		value, err := enrollmentFilterAccessToProto(*params.AccessStatus)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное состояние доступа"))
			return
		}
		request.AccessStatus = &value
	}
	response, err := h.academy.GetEnrollments(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := academyEnrollmentsFromProto(response.GetEnrollments())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetAcademyEnrollment(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	response, err := h.academy.GetEnrollment(outgoingContext(r), &academyv1.GetEnrollmentRequest{EnrollmentId: enrollmentID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := academyEnrollmentFromProto(response.GetEnrollment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetAcademyEnrollmentOutline(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	response, err := h.academy.GetEnrollmentOutline(outgoingContext(r), &academyv1.GetEnrollmentOutlineRequest{EnrollmentId: enrollmentID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := enrollmentOutlineFromProto(response.GetOutline())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetAcademyEnrollmentLesson(
	w http.ResponseWriter,
	r *http.Request,
	enrollmentID api.EnrollmentId,
	lessonVersionID api.LessonVersionId,
) {
	response, err := h.academy.GetEnrollmentLesson(outgoingContext(r), &academyv1.GetEnrollmentLessonRequest{
		EnrollmentId: enrollmentID.String(), LessonVersionId: lessonVersionID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := academyEnrollmentLessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) ResumeAcademyEnrollment(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	response, err := h.academy.ResumeEnrollment(outgoingContext(r), &academyv1.ResumeEnrollmentRequest{EnrollmentId: enrollmentID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	enrollment, err := academyEnrollmentFromProto(response.GetEnrollment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	result := api.AcademyEnrollmentResume{Enrollment: enrollment}
	if response.GetCurrentLesson() != nil {
		lesson, convertErr := academyEnrollmentLessonFromProto(response.GetCurrentLesson())
		if convertErr != nil {
			h.writeConversionError(w, r, convertErr)
			return
		}
		result.CurrentLesson = &lesson
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CompleteAcademyEnrollmentLesson(
	w http.ResponseWriter,
	r *http.Request,
	enrollmentID api.EnrollmentId,
	lessonVersionID api.LessonVersionId,
	params api.CompleteAcademyEnrollmentLessonParams,
) {
	var input api.CompleteEnrollmentLessonInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.CompleteEnrollmentLessonRequest{
		EnrollmentId: enrollmentID.String(), LessonVersionId: lessonVersionID.String(),
		IdempotencyKey: string(params.IdempotencyKey),
	}
	if input.ActiveSeconds != nil {
		if *input.ActiveSeconds < 0 {
			apierror.Write(w, apierror.BadRequest("Активное время не может быть отрицательным"))
			return
		}
		seconds := uint64(*input.ActiveSeconds)
		request.ActiveSeconds = &seconds
	}
	if input.LastPosition != nil {
		position, err := structpb.NewStruct(*input.LastPosition)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректная позиция в уроке"))
			return
		}
		request.LastPosition = position
	}
	response, err := h.academy.CompleteEnrollmentLesson(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := enrollmentProgressSnapshotFromProto(response.GetProgress())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) SubmitAcademyEnrollmentQuizAttempt(
	w http.ResponseWriter,
	r *http.Request,
	enrollmentID api.EnrollmentId,
	quizVersionID api.QuizVersionId,
	params api.SubmitAcademyEnrollmentQuizAttemptParams,
) {
	var input api.SubmitEnrollmentQuizAttemptInput
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
	request := &academyv1.SubmitEnrollmentQuizAttemptRequest{
		EnrollmentId: enrollmentID.String(), QuizVersionId: quizVersionID.String(), Answers: answers,
		IdempotencyKey: string(params.IdempotencyKey),
	}
	if input.ActiveSeconds != nil {
		if *input.ActiveSeconds < 0 {
			apierror.Write(w, apierror.BadRequest("Активное время не может быть отрицательным"))
			return
		}
		seconds := uint64(*input.ActiveSeconds)
		request.ActiveSeconds = &seconds
	}
	if input.LastPosition != nil {
		position, err := structpb.NewStruct(*input.LastPosition)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректная позиция в уроке"))
			return
		}
		request.LastPosition = position
	}
	response, err := h.academy.SubmitEnrollmentQuizAttempt(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	attempt, err := enrollmentQuizAttemptFromProto(response.GetAttempt())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	progress, err := enrollmentProgressSnapshotFromProto(response.GetProgress())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.EnrollmentQuizAttemptSubmitted{Attempt: attempt, Progress: progress})
}

func (h *Handler) GetAcademyEnrollmentReport(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	response, err := h.academy.GetEnrollmentReport(outgoingContext(r), &academyv1.GetEnrollmentReportRequest{EnrollmentId: enrollmentID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := enrollmentReportFromProto(response.GetReport())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}
