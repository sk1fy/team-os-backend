package transport

import (
	"net/http"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handler) GetCourses(w http.ResponseWriter, r *http.Request) {
	response, err := h.academy.GetCourses(outgoingContext(r), &academyv1.GetCoursesRequest{})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := coursesFromProto(response.GetCourses())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCourse(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.academy.GetCourse(outgoingContext(r), &academyv1.GetCourseRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseFromProto(response.GetCourse())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetPublicCourse(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.academy.GetPublicCourse(outgoingContext(r), &academyv1.GetPublicCourseRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	course, err := courseFromProto(response.GetCourse())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	sections, err := courseSectionsFromProto(response.GetSections())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	lessons, err := lessonsFromProto(response.GetLessons())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.PublicCourse{Course: course, Sections: sections, Lessons: lessons})
}

func (h *Handler) CreateCourse(w http.ResponseWriter, r *http.Request) {
	var input api.CreateCourseInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.CreateCourseRequest{
		Title: input.Title, Description: input.Description, Sequential: input.Sequential,
	}
	if input.Status != nil {
		status, err := courseStatusToProto(*input.Status)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный статус курса"))
			return
		}
		request.Status = &status
	}
	if input.DeadlineDays != nil {
		if *input.DeadlineDays < 0 {
			apierror.Write(w, apierror.BadRequest("Дедлайн курса должен быть положительным числом дней"))
			return
		}
		days := uint32(*input.DeadlineDays)
		request.DeadlineDays = &days
	}
	if input.Visibility != nil {
		visibility, err := courseVisibilityToProto(*input.Visibility)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректная видимость курса"))
			return
		}
		request.Visibility = &visibility
	}
	response, err := h.academy.CreateCourse(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseFromProto(response.GetCourse())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) CreateCourseFromKb(w http.ResponseWriter, r *http.Request) {
	var input api.CreateCourseFromKbInput
	if !decode(w, r, &input) {
		return
	}
	mode, err := lessonSourceModeToProto(input.Mode)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректный режим импорта статей"))
		return
	}
	request := &academyv1.CreateCourseFromKbRequest{
		Title: input.Title, Description: input.Description, Sequential: input.Sequential,
		Mode: mode, SectionIds: idStrings(input.SectionIds), ArticleIds: idStrings(input.ArticleIds),
	}
	if input.DeadlineDays != nil {
		if *input.DeadlineDays < 0 {
			apierror.Write(w, apierror.BadRequest("Дедлайн курса должен быть положительным числом дней"))
			return
		}
		days := uint32(*input.DeadlineDays)
		request.DeadlineDays = &days
	}
	if input.Visibility != nil {
		visibility, err := courseVisibilityToProto(*input.Visibility)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректная видимость курса"))
			return
		}
		request.Visibility = &visibility
	}
	response, err := h.academy.CreateCourseFromKb(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseFromProto(response.GetCourse())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateCourse(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateCourseInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.UpdateCourseRequest{
		Id: id.String(), Title: input.Title, Description: input.Description,
		Sequential: input.Sequential,
	}
	if input.Status != nil {
		status, err := courseStatusToProto(*input.Status)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный статус курса"))
			return
		}
		request.Status = &status
	}
	if input.DeadlineDays != nil {
		if *input.DeadlineDays < 0 {
			apierror.Write(w, apierror.BadRequest("Дедлайн курса должен быть положительным числом дней"))
			return
		}
		days := uint32(*input.DeadlineDays)
		request.DeadlineDays = &days
	}
	if input.Visibility != nil {
		visibility, err := courseVisibilityToProto(*input.Visibility)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректная видимость курса"))
			return
		}
		request.Visibility = &visibility
	}
	response, err := h.academy.UpdateCourse(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseFromProto(response.GetCourse())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) DeleteCourse(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.academy.DeleteCourse(outgoingContext(r), &academyv1.DeleteCourseRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetCourseSections(w http.ResponseWriter, r *http.Request, courseID api.ID) {
	response, err := h.academy.GetCourseSections(outgoingContext(r), &academyv1.GetCourseSectionsRequest{
		CourseId: courseID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseSectionsFromProto(response.GetSections())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateCourseSection(w http.ResponseWriter, r *http.Request, courseID api.ID) {
	var input api.CreateCourseSectionInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.academy.CreateCourseSection(outgoingContext(r), &academyv1.CreateCourseSectionRequest{
		CourseId: courseID.String(), Title: input.Title,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseSectionFromProto(response.GetSection())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateCourseSection(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateCourseSectionInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.academy.UpdateCourseSection(outgoingContext(r), &academyv1.UpdateCourseSectionRequest{
		Id: id.String(), Title: input.Title,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseSectionFromProto(response.GetSection())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) DeleteCourseSection(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.academy.DeleteCourseSection(outgoingContext(r), &academyv1.DeleteCourseSectionRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetLessons(w http.ResponseWriter, r *http.Request, params api.GetLessonsParams) {
	request := &academyv1.GetLessonsRequest{}
	if params.CourseId != nil {
		courseID := params.CourseId.String()
		request.CourseId = &courseID
	}
	response, err := h.academy.GetLessons(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := lessonsFromProto(response.GetLessons())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateLesson(w http.ResponseWriter, r *http.Request) {
	var input api.CreateLessonInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.CreateLessonRequest{
		CourseId: input.CourseId.String(), SectionId: input.SectionId.String(), Title: input.Title,
	}
	if input.Content != nil {
		content, err := richTextToStruct(*input.Content)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Content = content
	}
	if input.SourceArticleId != nil {
		articleID := input.SourceArticleId.String()
		request.SourceArticleId = &articleID
	}
	if input.SourceMode != nil {
		mode, err := lessonSourceModeToProto(*input.SourceMode)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный режим импорта статьи"))
			return
		}
		request.SourceMode = &mode
	}
	response, err := h.academy.CreateLesson(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := lessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateLesson(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateLessonInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.UpdateLessonRequest{Id: id.String(), Title: input.Title}
	if input.Content != nil {
		content, err := richTextToStruct(*input.Content)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Content = content
	}
	if input.SourceArticleId != nil {
		articleID := input.SourceArticleId.String()
		request.SourceArticleId = &articleID
	}
	if input.SourceMode != nil {
		mode, err := lessonSourceModeToProto(*input.SourceMode)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный режим импорта статьи"))
			return
		}
		request.SourceMode = &mode
	}
	response, err := h.academy.UpdateLesson(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := lessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) DeleteLesson(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.academy.DeleteLesson(outgoingContext(r), &academyv1.DeleteLessonRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveLesson(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.MoveLessonInput
	if !decode(w, r, &input) {
		return
	}
	if input.Order < 0 {
		apierror.Write(w, apierror.BadRequest("Порядок урока не может быть отрицательным"))
		return
	}
	response, err := h.academy.MoveLesson(outgoingContext(r), &academyv1.MoveLessonRequest{
		Id: id.String(), SectionId: input.SectionId.String(), Order: uint32(input.Order),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := lessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetQuizzes(w http.ResponseWriter, r *http.Request, params api.GetQuizzesParams) {
	request := &academyv1.GetQuizzesRequest{}
	if params.LessonId != nil {
		lessonID := params.LessonId.String()
		request.LessonId = &lessonID
	}
	response, err := h.academy.GetQuizzes(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := quizzesFromProto(response.GetQuizzes())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) UpsertQuiz(w http.ResponseWriter, r *http.Request) {
	var input api.UpsertQuizInput
	if !decode(w, r, &input) {
		return
	}
	if input.PassingScore < 0 || input.PassingScore > 100 {
		apierror.Write(w, apierror.BadRequest("Проходной балл должен быть от 0 до 100"))
		return
	}
	questions, err := quizQuestionsToProto(input.Questions)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректные вопросы теста"))
		return
	}
	request := &academyv1.UpsertQuizRequest{
		LessonId: input.LessonId.String(), Questions: questions,
		PassingScore: uint32(input.PassingScore),
	}
	if input.Id != nil {
		id := input.Id.String()
		request.Id = &id
	}
	if input.MaxAttempts != nil {
		if *input.MaxAttempts < 1 {
			apierror.Write(w, apierror.BadRequest("Число попыток должно быть не меньше одной"))
			return
		}
		attempts := uint32(*input.MaxAttempts)
		request.MaxAttempts = &attempts
	}
	response, err := h.academy.UpsertQuiz(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := quizFromProto(response.GetQuiz())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetAssignments(w http.ResponseWriter, r *http.Request) {
	response, err := h.academy.GetAssignments(outgoingContext(r), &academyv1.GetAssignmentsRequest{})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := assignmentsFromProto(response.GetAssignments())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) AssignCourse(w http.ResponseWriter, r *http.Request) {
	var input api.AssignCourseInput
	if !decode(w, r, &input) {
		return
	}
	assigneeType, err := assigneeTypeToProto(input.AssigneeType)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректный тип назначения"))
		return
	}
	request := &academyv1.AssignCourseRequest{
		CourseId: input.CourseId.String(), AssigneeType: assigneeType,
	}
	if input.AssigneeId != nil {
		assigneeID := input.AssigneeId.String()
		request.AssigneeId = &assigneeID
	}
	if input.DueDate != nil {
		if datetime, dateErr := input.DueDate.AsISODateTime(); dateErr == nil {
			request.DueDate = timestamppb.New(datetime.UTC())
		}
	}
	response, err := h.academy.AssignCourse(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := assignmentFromProto(response.GetAssignment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) GetProgress(w http.ResponseWriter, r *http.Request, params api.GetProgressParams) {
	request := &academyv1.GetProgressRequest{}
	if params.CourseId != nil {
		courseID := params.CourseId.String()
		request.CourseId = &courseID
	}
	response, err := h.academy.GetProgress(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseProgressListFromProto(response.GetProgress())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) MarkLessonComplete(w http.ResponseWriter, r *http.Request, lessonID api.ID) {
	var input api.MarkLessonCompleteInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.MarkLessonCompleteRequest{
		LessonId: lessonID.String(), CourseId: input.CourseId.String(),
	}
	if input.UserId != nil {
		userID := input.UserId.String()
		request.UserId = &userID
	}
	response, err := h.academy.MarkLessonComplete(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseProgressFromProto(response.GetProgress())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) writeAcademyRPCError(w http.ResponseWriter, r *http.Request, err error) {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		h.logger.ErrorContext(r.Context(), "academy RPC failed", "error", err)
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
		h.logger.WarnContext(r.Context(), "academy RPC unavailable", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис временно недоступен"))
	default:
		h.logger.ErrorContext(r.Context(), "academy RPC failed", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.Internal(err))
	}
}
