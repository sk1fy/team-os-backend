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

func (h *Handler) GetCourses(w http.ResponseWriter, r *http.Request, params api.GetCoursesParams) {
	request := &academyv1.GetCoursesRequest{HasDraft: params.HasDraft}
	if params.OwnerType != nil {
		value, err := courseOwnerTypeToProto(*params.OwnerType)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный тип владельца курса"))
			return
		}
		request.OwnerType = &value
	}
	if params.PartnerId != nil {
		value := params.PartnerId.String()
		request.PartnerId = &value
	}
	if params.Lifecycle != nil {
		value, err := courseLifecycleToProto(*params.Lifecycle)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный статус жизненного цикла курса"))
			return
		}
		request.Lifecycle = &value
	}
	if params.Distribution != nil {
		value, err := courseDistributionToProto(*params.Distribution)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный статус распространения курса"))
			return
		}
		request.Distribution = &value
	}
	if params.LatestVersion != nil {
		if *params.LatestVersion < 0 {
			apierror.Write(w, apierror.BadRequest("Номер версии не может быть отрицательным"))
			return
		}
		value := uint32(*params.LatestVersion)
		request.LatestVersion = &value
	}
	if params.OriginType != nil {
		value, err := courseOriginTypeToProto(*params.OriginType)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное происхождение курса"))
			return
		}
		request.OriginType = &value
	}
	response, err := h.academy.GetCourses(outgoingContext(r), request)
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
	markLegacyAcademyEndpoint(w, publicAcademyAccessSuccessor)
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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

func (h *Handler) ArchiveAcademyCourse(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.ArchiveCourse(outgoingContext(r), &academyv1.ArchiveCourseRequest{Id: courseID.String()})
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

func (h *Handler) RestoreAcademyCourse(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.RestoreCourse(outgoingContext(r), &academyv1.RestoreCourseRequest{Id: courseID.String()})
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

func (h *Handler) GetCourseVersions(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.GetCourseVersions(outgoingContext(r), &academyv1.GetCourseVersionsRequest{CourseId: courseID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionsFromProto(response.GetVersions())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCourseVersion(w http.ResponseWriter, r *http.Request, courseID api.CourseId, versionID api.VersionId) {
	response, err := h.academy.GetCourseVersion(outgoingContext(r), &academyv1.GetCourseVersionRequest{
		CourseId: courseID.String(), VersionId: versionID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionDetailFromProto(response)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateCourseDraft(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.CreateCourseDraft(outgoingContext(r), &academyv1.CreateCourseDraftRequest{CourseId: courseID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionFromProto(response.GetVersion())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateCourseDraft(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	var input api.UpdateCourseDraftInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.UpdateCourseDraftRequest{
		CourseId: courseID.String(), Title: input.Title, Description: input.Description,
		Sequential: input.Sequential,
	}
	if input.CoverFileId != nil {
		value := input.CoverFileId.String()
		request.CoverFileId = &value
	}
	if input.DefaultInternalDeadlineDays != nil {
		if *input.DefaultInternalDeadlineDays < 1 {
			apierror.Write(w, apierror.BadRequest("Срок выполнения должен быть не меньше одного дня"))
			return
		}
		value := uint32(*input.DefaultInternalDeadlineDays)
		request.DefaultInternalDeadlineDays = &value
	}
	response, err := h.academy.UpdateCourseDraft(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionFromProto(response.GetVersion())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) PublishCourseVersion(w http.ResponseWriter, r *http.Request, courseID api.CourseId, params api.PublishCourseVersionParams) {
	response, err := h.academy.PublishCourseVersion(outgoingContext(r), &academyv1.PublishCourseVersionRequest{
		CourseId: courseID.String(), IdempotencyKey: string(params.IdempotencyKey),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionFromProto(response.GetVersion())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateCourseVersionSection(w http.ResponseWriter, r *http.Request, versionID api.VersionId) {
	var input api.CreateCourseVersionSectionInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.academy.CreateCourseVersionSection(outgoingContext(r), &academyv1.CreateCourseVersionSectionRequest{
		VersionId: versionID.String(), Title: input.Title,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionSectionFromProto(response.GetSection())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateCourseVersionSection(w http.ResponseWriter, r *http.Request, sectionID api.SectionId) {
	var input api.UpdateCourseVersionSectionInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.UpdateCourseVersionSectionRequest{Id: sectionID.String(), Title: input.Title}
	if input.Order != nil {
		if *input.Order < 0 {
			apierror.Write(w, apierror.BadRequest("Некорректный порядок раздела"))
			return
		}
		value := uint32(*input.Order)
		request.Order = &value
	}
	response, err := h.academy.UpdateCourseVersionSection(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionSectionFromProto(response.GetSection())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) DeleteCourseVersionSection(w http.ResponseWriter, r *http.Request, sectionID api.SectionId) {
	_, err := h.academy.DeleteCourseVersionSection(outgoingContext(r), &academyv1.DeleteCourseVersionSectionRequest{Id: sectionID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateCourseVersionLesson(w http.ResponseWriter, r *http.Request, versionID api.VersionId) {
	var input api.CreateCourseVersionLessonInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.CreateCourseVersionLessonRequest{
		VersionId: versionID.String(), SectionVersionId: input.SectionVersionId.String(), Title: input.Title,
	}
	if input.Content != nil {
		content, err := richTextToStruct(*input.Content)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное содержимое урока"))
			return
		}
		request.Content = content
	}
	if input.SourceType != nil {
		value, err := courseLessonSourceTypeToProto(*input.SourceType)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный источник урока"))
			return
		}
		request.SourceType = &value
	}
	if input.SourceArticleId != nil {
		value := input.SourceArticleId.String()
		request.SourceArticleId = &value
	}
	if !setPositiveUint32(w, input.SourceArticleVersion, "Версия статьи должна быть больше нуля", func(value *uint32) { request.SourceArticleVersion = value }) ||
		!setPositiveUint32(w, input.EstimatedMinutes, "Продолжительность урока должна быть не меньше одной минуты", func(value *uint32) { request.EstimatedMinutes = value }) {
		return
	}
	response, err := h.academy.CreateCourseVersionLesson(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionLessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateCourseVersionLesson(w http.ResponseWriter, r *http.Request, lessonID api.LessonId) {
	var input api.UpdateCourseVersionLessonInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.UpdateCourseVersionLessonRequest{Id: lessonID.String(), Title: input.Title}
	if input.Content != nil {
		content, err := richTextToStruct(*input.Content)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректное содержимое урока"))
			return
		}
		request.Content = content
	}
	if input.SourceType != nil {
		value, err := courseLessonSourceTypeToProto(*input.SourceType)
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный источник урока"))
			return
		}
		request.SourceType = &value
	}
	if input.SourceArticleId != nil {
		value := input.SourceArticleId.String()
		request.SourceArticleId = &value
	}
	if !setPositiveUint32(w, input.SourceArticleVersion, "Версия статьи должна быть больше нуля", func(value *uint32) { request.SourceArticleVersion = value }) ||
		!setPositiveUint32(w, input.EstimatedMinutes, "Продолжительность урока должна быть не меньше одной минуты", func(value *uint32) { request.EstimatedMinutes = value }) {
		return
	}
	response, err := h.academy.UpdateCourseVersionLesson(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionLessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) DeleteCourseVersionLesson(w http.ResponseWriter, r *http.Request, lessonID api.LessonId) {
	_, err := h.academy.DeleteCourseVersionLesson(outgoingContext(r), &academyv1.DeleteCourseVersionLessonRequest{Id: lessonID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveCourseVersionLesson(w http.ResponseWriter, r *http.Request, lessonID api.LessonId) {
	var input api.MoveCourseVersionLessonInput
	if !decode(w, r, &input) {
		return
	}
	if input.Order < 0 {
		apierror.Write(w, apierror.BadRequest("Некорректный порядок урока"))
		return
	}
	response, err := h.academy.MoveCourseVersionLesson(outgoingContext(r), &academyv1.MoveCourseVersionLessonRequest{
		Id: lessonID.String(), SectionVersionId: input.SectionVersionId.String(), Order: uint32(input.Order),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionLessonFromProto(response.GetLesson())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) UpsertCourseVersionQuiz(w http.ResponseWriter, r *http.Request, lessonID api.LessonId) {
	var input api.UpsertCourseVersionQuizInput
	if !decode(w, r, &input) {
		return
	}
	questions, err := quizQuestionsToProto(input.Questions)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректные вопросы теста"))
		return
	}
	if input.PassingScore < 0 || input.PassingScore > 100 {
		apierror.Write(w, apierror.BadRequest("Проходной балл должен быть от 0 до 100"))
		return
	}
	request := &academyv1.UpsertCourseVersionQuizRequest{
		LessonVersionId: lessonID.String(), Questions: questions, PassingScore: uint32(input.PassingScore),
	}
	if !setPositiveUint32(w, input.MaxAttempts, "Число попыток должно быть не меньше одной", func(value *uint32) { request.MaxAttempts = value }) {
		return
	}
	response, err := h.academy.UpsertCourseVersionQuiz(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionQuizFromProto(response.GetQuiz())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func setPositiveUint32(w http.ResponseWriter, value *int, message string, set func(*uint32)) bool {
	if value == nil {
		return true
	}
	if *value < 1 {
		apierror.Write(w, apierror.BadRequest(message))
		return false
	}
	converted := uint32(*value)
	set(&converted)
	return true
}

func (h *Handler) GetCourseSections(w http.ResponseWriter, r *http.Request, courseID api.ID) {
	markLegacyAcademyEndpoint(w, academyCoursesSuccessor)
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
	_, err := h.academy.DeleteCourseSection(outgoingContext(r), &academyv1.DeleteCourseSectionRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetLessons(w http.ResponseWriter, r *http.Request, params api.GetLessonsParams) {
	markLegacyAcademyEndpoint(w, academyCoursesSuccessor)
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
	_, err := h.academy.DeleteLesson(outgoingContext(r), &academyv1.DeleteLessonRequest{Id: id.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveLesson(w http.ResponseWriter, r *http.Request, id api.Id) {
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	markLegacyAcademyEndpoint(w, academyCoursesSuccessor)
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
	if guardLegacyAcademyWrite(w, academyCoursesSuccessor) {
		return
	}
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
	if input.CourseVersionId != nil {
		versionID := input.CourseVersionId.String()
		request.CourseVersionId = &versionID
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
	markLegacyAcademyEndpoint(w, academyEnrollmentsSuccessor)
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
	if guardLegacyAcademyWrite(w, academyEnrollmentsSuccessor) {
		return
	}
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
