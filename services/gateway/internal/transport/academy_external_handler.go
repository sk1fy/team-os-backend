package transport

import (
	"net/http"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) CreateExternalPersonalAccess(w http.ResponseWriter, r *http.Request, courseID api.CourseId, versionID api.VersionId) {
	var input api.CreateExternalPersonalAccessInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.academy.CreateExternalPersonalAccess(outgoingContext(r), &academyv1.CreateExternalPersonalAccessRequest{
		CourseId: courseID.String(), CourseVersionId: versionID.String(), Email: string(input.Email),
		FirstName: input.FirstName, LastName: input.LastName, DeadlineDays: uint32(input.DeadlineDays),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessCreatedFromProto(response.GetCreated())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) GetExternalPersonalAccesses(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.GetExternalPersonalAccesses(outgoingContext(r), &academyv1.GetExternalPersonalAccessesRequest{
		CourseId: courseID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessesFromProto(response.GetAccesses())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetExternalPersonalAccess(w http.ResponseWriter, r *http.Request, accessID api.AccessId) {
	response, err := h.academy.GetExternalPersonalAccess(outgoingContext(r), &academyv1.GetExternalPersonalAccessRequest{
		AccessId: accessID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessFromProto(response.GetAccess())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) ExtendExternalPersonalAccess(w http.ResponseWriter, r *http.Request, accessID api.AccessId) {
	var input api.ExtendExternalPersonalAccessInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.academy.ExtendExternalPersonalAccess(outgoingContext(r), &academyv1.ExtendExternalPersonalAccessRequest{
		AccessId: accessID.String(), DeadlineDays: uint32(input.DeadlineDays),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessFromProto(response.GetAccess())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RotateExternalPersonalAccessToken(w http.ResponseWriter, r *http.Request, accessID api.AccessId) {
	response, err := h.academy.RotateExternalPersonalAccessToken(outgoingContext(r), &academyv1.RotateExternalPersonalAccessTokenRequest{
		AccessId: accessID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessCreatedFromProto(response.GetCreated())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RevokeExternalPersonalAccess(w http.ResponseWriter, r *http.Request, accessID api.AccessId) {
	response, err := h.academy.RevokeExternalPersonalAccess(outgoingContext(r), &academyv1.RevokeExternalPersonalAccessRequest{
		AccessId: accessID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessFromProto(response.GetAccess())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RepeatExternalPersonalAccess(w http.ResponseWriter, r *http.Request, accessID api.AccessId) {
	response, err := h.academy.RepeatExternalPersonalAccess(outgoingContext(r), &academyv1.RepeatExternalPersonalAccessRequest{
		AccessId: accessID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalPersonalAccessCreatedFromProto(response.GetCreated())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) GetPublicAcademyAccess(w http.ResponseWriter, r *http.Request, token api.PublicAcademyToken) {
	setPublicAcademyHeaders(w)
	visitorHash, err := h.academyVisitorHash(w, r)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	utmSource, utmMedium, utmCampaign, utmContent, utmTerm, referrer := academyAttribution(r)
	response, err := h.academy.GetPublicAcademyAccess(h.externalOutgoingContext(r), &academyv1.GetPublicAcademyAccessRequest{
		Token: token, VisitorHash: visitorHash, UtmSource: utmSource, UtmMedium: utmMedium,
		UtmCampaign: utmCampaign, UtmContent: utmContent, UtmTerm: utmTerm, Referrer: referrer,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := publicAcademyAccessFromProto(response.GetAccess())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RequestPublicAcademyVerification(w http.ResponseWriter, r *http.Request, token api.PublicAcademyToken) {
	setPublicAcademyHeaders(w)
	var input api.RequestExternalVerificationInput
	if !decode(w, r, &input) {
		return
	}
	visitorHash, err := h.academyVisitorHash(w, r)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	response, err := h.academy.RequestPublicAcademyVerification(outgoingContext(r), &academyv1.RequestPublicAcademyVerificationRequest{
		AccessToken: token, Email: string(input.Email), FirstName: input.FirstName, LastName: input.LastName,
		VisitorHash: visitorHash,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	challenge := response.GetChallenge()
	if challenge == nil || challenge.GetExpiresAt() == nil || !challenge.GetExpiresAt().IsValid() {
		h.writeConversionError(w, r, errEmptyAcademyResponse)
		return
	}
	challengeID, err := uuid.Parse(challenge.GetChallengeId())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, api.ExternalVerificationChallenge{
		ChallengeId: challengeID, ExpiresAt: challenge.GetExpiresAt().AsTime(),
	})
}

func (h *Handler) ConfirmPublicAcademyVerification(w http.ResponseWriter, r *http.Request, challengeID api.ChallengeId) {
	setPublicAcademyHeaders(w)
	var input api.ConfirmExternalVerificationInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.academy.ConfirmPublicAcademyVerification(outgoingContext(r), &academyv1.ConfirmPublicAcademyVerificationRequest{
		ChallengeId: challengeID.String(), Code: input.Code,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	confirmed := response.GetConfirmed()
	if confirmed == nil || confirmed.GetVerifiedAt() == nil || !confirmed.GetVerifiedAt().IsValid() ||
		confirmed.GetSessionExpiresAt() == nil || !confirmed.GetSessionExpiresAt().IsValid() || confirmed.GetSessionToken() == "" {
		h.writeConversionError(w, r, errEmptyAcademyResponse)
		return
	}
	learnerID, err := uuid.Parse(confirmed.GetLearnerId())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	h.setExternalSessionCookie(w, confirmed.GetSessionToken(), confirmed.GetSessionExpiresAt().AsTime())
	writeJSON(w, http.StatusOK, api.ExternalVerificationConfirmed{
		LearnerId: learnerID, VerifiedAt: confirmed.GetVerifiedAt().AsTime(),
	})
}

func (h *Handler) ActivatePublicAcademyAccess(w http.ResponseWriter, r *http.Request, token api.PublicAcademyToken, params api.ActivatePublicAcademyAccessParams) {
	setPublicAcademyHeaders(w)
	if !h.requireExternalCSRF(w, r) {
		return
	}
	visitorHash, err := h.academyVisitorHash(w, r)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	response, err := h.academy.ActivatePublicAcademyAccess(h.externalOutgoingContext(r), &academyv1.ActivatePublicAcademyAccessRequest{
		AccessToken: token, IdempotencyKey: string(params.IdempotencyKey), VisitorHash: visitorHash,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalEnrollmentFromProto(response.GetEnrollment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetPublicAcademyEnrollment(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	setPublicAcademyHeaders(w)
	response, err := h.academy.GetPublicAcademyEnrollment(h.externalOutgoingContext(r), &academyv1.GetPublicAcademyEnrollmentRequest{
		EnrollmentId: enrollmentID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalEnrollmentFromProto(response.GetEnrollment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetPublicAcademyEnrollmentOutline(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	setPublicAcademyHeaders(w)
	response, err := h.academy.GetPublicAcademyEnrollmentOutline(h.externalOutgoingContext(r), &academyv1.GetPublicAcademyEnrollmentOutlineRequest{
		EnrollmentId: enrollmentID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalOutlineFromProto(response.GetOutline())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetPublicAcademyEnrollmentLesson(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId, lessonID api.LessonVersionId) {
	setPublicAcademyHeaders(w)
	response, err := h.academy.GetPublicAcademyEnrollmentLesson(h.externalOutgoingContext(r), &academyv1.GetPublicAcademyEnrollmentLessonRequest{
		EnrollmentId: enrollmentID.String(), LessonVersionId: lessonID.String(),
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
	writeJSON(w, http.StatusOK, converted.Lesson)
}

func (h *Handler) CompletePublicAcademyEnrollmentLesson(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId, lessonID api.LessonVersionId, params api.CompletePublicAcademyEnrollmentLessonParams) {
	setPublicAcademyHeaders(w)
	if !h.requireExternalCSRF(w, r) {
		return
	}
	response, err := h.academy.CompletePublicAcademyEnrollmentLesson(h.externalOutgoingContext(r), &academyv1.CompletePublicAcademyEnrollmentLessonRequest{
		EnrollmentId: enrollmentID.String(), LessonVersionId: lessonID.String(), IdempotencyKey: string(params.IdempotencyKey),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalEnrollmentFromProto(response.GetEnrollment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) SubmitPublicAcademyQuizAttempt(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId, quizID api.QuizVersionId, params api.SubmitPublicAcademyQuizAttemptParams) {
	setPublicAcademyHeaders(w)
	if !h.requireExternalCSRF(w, r) {
		return
	}
	var input api.SubmitExternalQuizAttemptInput
	if !decode(w, r, &input) {
		return
	}
	answers := make([]*academyv1.EnrollmentQuizAnswer, len(input.Answers))
	for index, answer := range input.Answers {
		answers[index] = externalAnswerToProto(answer)
	}
	response, err := h.academy.SubmitPublicAcademyQuizAttempt(h.externalOutgoingContext(r), &academyv1.SubmitPublicAcademyQuizAttemptRequest{
		EnrollmentId: enrollmentID.String(), QuizVersionId: quizID.String(),
		IdempotencyKey: string(params.IdempotencyKey), Answers: answers,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	attempt, err := externalQuizAttemptResultFromProto(response.GetResult())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	enrollment, err := externalEnrollmentFromProto(response.GetEnrollment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.ExternalQuizAttemptSubmitted{
		Id: attempt.Id, Score: attempt.Score, Passed: attempt.Passed, PendingReview: attempt.PendingReview,
		AttemptsRemaining: attempt.AttemptsRemaining, CreatedAt: attempt.CreatedAt,
		Attempt: attempt, Enrollment: enrollment,
	})
}

func (h *Handler) GetPublicAcademyEnrollmentResults(w http.ResponseWriter, r *http.Request, enrollmentID api.EnrollmentId) {
	setPublicAcademyHeaders(w)
	response, err := h.academy.GetPublicAcademyEnrollmentResults(h.externalOutgoingContext(r), &academyv1.GetPublicAcademyEnrollmentResultsRequest{
		EnrollmentId: enrollmentID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalEnrollmentResultsFromProto(response.GetResults())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetExternalLearners(w http.ResponseWriter, r *http.Request) {
	response, err := h.academy.GetExternalLearners(outgoingContext(r), &academyv1.GetExternalLearnersRequest{})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalLearnersFromProto(response.GetLearners())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetExternalLearner(w http.ResponseWriter, r *http.Request, learnerID api.LearnerId) {
	response, err := h.academy.GetExternalLearner(outgoingContext(r), &academyv1.GetExternalLearnerRequest{LearnerId: learnerID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalLearnerFromProto(response.GetLearner())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetExternalLearnerEnrollments(w http.ResponseWriter, r *http.Request, learnerID api.LearnerId) {
	response, err := h.academy.GetExternalLearnerEnrollments(outgoingContext(r), &academyv1.GetExternalLearnerEnrollmentsRequest{LearnerId: learnerID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := externalEnrollmentsFromProto(response.GetEnrollments())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func setPublicAcademyHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "private, no-store")
}
