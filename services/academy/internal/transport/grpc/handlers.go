package grpc

import (
	"context"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

func (s *Server) GetCourses(ctx context.Context, request *academyv1.GetCoursesRequest) (*academyv1.GetCoursesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	filters := application.CourseFilters{HasDraft: request.HasDraft, LatestVersion: request.LatestVersion}
	if request.OwnerType != nil {
		value, convertErr := courseOwnerTypeFromProto(request.GetOwnerType())
		if convertErr != nil {
			return nil, convertErr
		}
		filters.OwnerType = &value
	}
	if request.PartnerId != nil {
		partnerID, parseErr := parseUUID(request.GetPartnerId())
		if parseErr != nil {
			return nil, parseErr
		}
		filters.PartnerID = &partnerID
	}
	if request.Lifecycle != nil {
		value, convertErr := courseLifecycleFromProto(request.GetLifecycle())
		if convertErr != nil {
			return nil, convertErr
		}
		filters.LifecycleStatus = &value
	}
	if request.Distribution != nil {
		value, convertErr := courseDistributionFromProto(request.GetDistribution())
		if convertErr != nil {
			return nil, convertErr
		}
		filters.DistributionStatus = &value
	}
	if request.OriginType != nil {
		value, convertErr := courseOriginTypeFromProto(request.GetOriginType())
		if convertErr != nil {
			return nil, convertErr
		}
		filters.OriginType = &value
	}
	courses, err := s.application.GetCourses(ctx, actor, filters)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCoursesResponse{Courses: coursesToProto(courses)}, nil
}

func (s *Server) ArchiveCourse(ctx context.Context, request *academyv1.ArchiveCourseRequest) (*academyv1.ArchiveCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.ArchiveCourse(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.ArchiveCourseResponse{Course: courseToProto(value)}, nil
}

func (s *Server) RestoreCourse(ctx context.Context, request *academyv1.RestoreCourseRequest) (*academyv1.RestoreCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.RestoreCourse(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RestoreCourseResponse{Course: courseToProto(value)}, nil
}

func (s *Server) GetCourseVersions(ctx context.Context, request *academyv1.GetCourseVersionsRequest) (*academyv1.GetCourseVersionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versions, err := s.application.GetCourseVersions(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseVersionsResponse{Versions: courseVersionsToProto(versions)}, nil
}

func (s *Server) GetCourseVersion(ctx context.Context, request *academyv1.GetCourseVersionRequest) (*academyv1.GetCourseVersionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	content, err := s.application.GetCourseVersion(ctx, actor, courseID, versionID)
	if err != nil {
		return nil, transportError(err)
	}
	lessons, err := courseVersionLessonsToProto(content.Lessons)
	if err != nil {
		return nil, transportError(err)
	}
	quizzes, err := courseVersionQuizzesToProto(content.Quizzes)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseVersionResponse{
		Version: courseVersionToProto(content.Version), Sections: courseVersionSectionsToProto(content.Sections),
		Lessons: lessons, Quizzes: quizzes,
	}, nil
}

func (s *Server) CreateCourseDraft(ctx context.Context, request *academyv1.CreateCourseDraftRequest) (*academyv1.CreateCourseDraftResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	version, err := s.application.CreateCourseDraft(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseDraftResponse{Version: courseVersionToProto(version)}, nil
}

func (s *Server) UpdateCourseDraft(ctx context.Context, request *academyv1.UpdateCourseDraftRequest) (*academyv1.UpdateCourseDraftResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	coverFileID, err := parseOptionalUUID(request.CoverFileId)
	if err != nil {
		return nil, err
	}
	input := application.UpdateCourseDraftInput{
		CourseID: courseID, Title: request.Title, Description: request.Description,
		CoverFileID: coverFileID, Sequential: request.Sequential,
		DefaultInternalDeadlineDays: uint32Pointer(request.DefaultInternalDeadlineDays),
	}
	version, err := s.application.UpdateCourseDraft(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateCourseDraftResponse{Version: courseVersionToProto(version)}, nil
}

func (s *Server) PublishCourseVersion(ctx context.Context, request *academyv1.PublishCourseVersionRequest) (*academyv1.PublishCourseVersionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	version, err := s.application.PublishCourseVersion(ctx, actor, courseID, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.PublishCourseVersionResponse{Version: courseVersionToProto(version)}, nil
}

func (s *Server) GetPublishedCourseVersion(ctx context.Context, request *academyv1.GetPublishedCourseVersionRequest) (*academyv1.GetPublishedCourseVersionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	versionID, err := parseOptionalUUID(request.VersionId)
	if err != nil {
		return nil, err
	}
	content, err := s.application.GetPublishedCourseVersion(ctx, actor, courseID, versionID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := learnerPublishedCourseVersionToProto(content)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublishedCourseVersionResponse{Version: converted}, nil
}

func (s *Server) CreateCourseVersionSection(ctx context.Context, request *academyv1.CreateCourseVersionSectionRequest) (*academyv1.CreateCourseVersionSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	section, err := s.application.CreateCourseVersionSection(ctx, actor, versionID, request.GetTitle())
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseVersionSectionResponse{Section: courseVersionSectionToProto(section)}, nil
}

func (s *Server) UpdateCourseVersionSection(ctx context.Context, request *academyv1.UpdateCourseVersionSectionRequest) (*academyv1.UpdateCourseVersionSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	section, err := s.application.UpdateCourseVersionSection(ctx, actor, application.UpdateCourseVersionSectionInput{
		ID: id, Title: request.Title, Order: uint32Pointer(request.Order),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateCourseVersionSectionResponse{Section: courseVersionSectionToProto(section)}, nil
}

func (s *Server) DeleteCourseVersionSection(ctx context.Context, request *academyv1.DeleteCourseVersionSectionRequest) (*academyv1.DeleteCourseVersionSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteCourseVersionSection(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.DeleteCourseVersionSectionResponse{}, nil
}

func (s *Server) CreateCourseVersionLesson(ctx context.Context, request *academyv1.CreateCourseVersionLessonRequest) (*academyv1.CreateCourseVersionLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	versionID, err := parseUUID(request.GetVersionId())
	if err != nil {
		return nil, err
	}
	sectionID, err := parseUUID(request.GetSectionVersionId())
	if err != nil {
		return nil, err
	}
	articleID, err := parseOptionalUUID(request.SourceArticleId)
	if err != nil {
		return nil, err
	}
	content, err := structToContent(request.Content)
	if err != nil {
		return nil, invalidArgument("Некорректное содержимое урока")
	}
	input := application.CreateCourseVersionLessonInput{
		VersionID: versionID, SectionVersionID: sectionID, Title: request.GetTitle(), Content: content,
		SourceArticleID: articleID, SourceArticleVersion: uint32Pointer(request.SourceArticleVersion),
		EstimatedMinutes: uint32Pointer(request.EstimatedMinutes),
	}
	if request.SourceType != nil {
		sourceType, convertErr := courseLessonSourceTypeFromProto(request.GetSourceType())
		if convertErr != nil {
			return nil, convertErr
		}
		input.SourceType = &sourceType
	}
	lesson, err := s.application.CreateCourseVersionLesson(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseVersionLessonToProto(lesson)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseVersionLessonResponse{Lesson: converted}, nil
}

func (s *Server) UpdateCourseVersionLesson(ctx context.Context, request *academyv1.UpdateCourseVersionLessonRequest) (*academyv1.UpdateCourseVersionLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	articleID, err := parseOptionalUUID(request.SourceArticleId)
	if err != nil {
		return nil, err
	}
	content, err := structToContent(request.Content)
	if err != nil {
		return nil, invalidArgument("Некорректное содержимое урока")
	}
	input := application.UpdateCourseVersionLessonInput{
		ID: id, Title: request.Title, Content: content, SetContent: request.Content != nil,
		SourceArticleID: articleID, SourceArticleVersion: uint32Pointer(request.SourceArticleVersion),
		EstimatedMinutes: uint32Pointer(request.EstimatedMinutes),
	}
	if request.SourceType != nil {
		sourceType, convertErr := courseLessonSourceTypeFromProto(request.GetSourceType())
		if convertErr != nil {
			return nil, convertErr
		}
		input.SourceType = &sourceType
	}
	lesson, err := s.application.UpdateCourseVersionLesson(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseVersionLessonToProto(lesson)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateCourseVersionLessonResponse{Lesson: converted}, nil
}

func (s *Server) DeleteCourseVersionLesson(ctx context.Context, request *academyv1.DeleteCourseVersionLessonRequest) (*academyv1.DeleteCourseVersionLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteCourseVersionLesson(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.DeleteCourseVersionLessonResponse{}, nil
}

func (s *Server) MoveCourseVersionLesson(ctx context.Context, request *academyv1.MoveCourseVersionLessonRequest) (*academyv1.MoveCourseVersionLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	sectionID, err := parseUUID(request.GetSectionVersionId())
	if err != nil {
		return nil, err
	}
	lesson, err := s.application.MoveCourseVersionLesson(ctx, actor, application.MoveCourseVersionLessonInput{
		ID: id, SectionVersionID: sectionID, Order: int32(request.GetOrder()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseVersionLessonToProto(lesson)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.MoveCourseVersionLessonResponse{Lesson: converted}, nil
}

func (s *Server) UpsertCourseVersionQuiz(ctx context.Context, request *academyv1.UpsertCourseVersionQuizRequest) (*academyv1.UpsertCourseVersionQuizResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonVersionId())
	if err != nil {
		return nil, err
	}
	questions, err := questionsFromProto(request.GetQuestions())
	if err != nil {
		return nil, invalidArgument("Некорректные вопросы теста")
	}
	quiz, err := s.application.UpsertCourseVersionQuiz(ctx, actor, application.UpsertCourseVersionQuizInput{
		LessonVersionID: lessonID, Questions: questions, PassingScore: int32(request.GetPassingScore()),
		MaxAttempts: uint32Pointer(request.MaxAttempts),
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := courseVersionQuizToProto(quiz)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpsertCourseVersionQuizResponse{Quiz: converted}, nil
}

func (s *Server) DeleteCourseVersionQuiz(ctx context.Context, request *academyv1.DeleteCourseVersionQuizRequest) (*academyv1.DeleteCourseVersionQuizResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonVersionId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteCourseVersionQuiz(ctx, actor, lessonID); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.DeleteCourseVersionQuizResponse{}, nil
}

func (s *Server) GetCourse(ctx context.Context, request *academyv1.GetCourseRequest) (*academyv1.GetCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	course, err := s.application.GetCourse(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseResponse{Course: courseToProto(course)}, nil
}

func (s *Server) GetPublicCourse(ctx context.Context, request *academyv1.GetPublicCourseRequest) (*academyv1.GetPublicCourseResponse, error) {
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	publicCourse, err := s.application.GetPublicCourse(ctx, id)
	if err != nil {
		return nil, transportError(err)
	}
	lessons, err := lessonsToProto(publicCourse.Lessons)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetPublicCourseResponse{
		Course: courseToProto(publicCourse.Course), Sections: sectionsToProto(publicCourse.Sections), Lessons: lessons,
	}, nil
}

func (s *Server) CreateCourse(ctx context.Context, request *academyv1.CreateCourseRequest) (*academyv1.CreateCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	input := application.CreateCourseInput{
		Title: request.GetTitle(), Description: request.Description,
		Sequential: request.Sequential, DeadlineDays: uint32Pointer(request.DeadlineDays),
	}
	if request.Status != nil {
		status, statusErr := courseStatusFromProto(request.GetStatus())
		if statusErr != nil {
			return nil, statusErr
		}
		input.Status = &status
	}
	if request.Visibility != nil {
		visibility, visibilityErr := courseVisibilityFromProto(request.GetVisibility())
		if visibilityErr != nil {
			return nil, visibilityErr
		}
		input.Visibility = &visibility
	}
	course, err := s.application.CreateCourse(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseResponse{Course: courseToProto(course)}, nil
}

func (s *Server) CreateCourseFromKb(ctx context.Context, request *academyv1.CreateCourseFromKbRequest) (*academyv1.CreateCourseFromKbResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	mode, err := sourceModeFromProto(request.GetMode())
	if err != nil {
		return nil, err
	}
	sectionIDs, err := parseUUIDList(request.GetSectionIds())
	if err != nil {
		return nil, err
	}
	articleIDs, err := parseUUIDList(request.GetArticleIds())
	if err != nil {
		return nil, err
	}
	input := application.CreateCourseFromKbInput{
		Title: request.GetTitle(), Description: request.Description,
		Sequential: request.Sequential, DeadlineDays: uint32Pointer(request.DeadlineDays),
		Mode: mode, SectionIDs: sectionIDs, ArticleIDs: articleIDs,
	}
	if request.Visibility != nil {
		visibility, visibilityErr := courseVisibilityFromProto(request.GetVisibility())
		if visibilityErr != nil {
			return nil, visibilityErr
		}
		input.Visibility = &visibility
	}
	course, err := s.application.CreateCourseFromKb(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseFromKbResponse{Course: courseToProto(course)}, nil
}

func (s *Server) UpdateCourse(ctx context.Context, request *academyv1.UpdateCourseRequest) (*academyv1.UpdateCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	input := application.UpdateCourseInput{
		ID: id, Title: request.Title, Description: request.Description,
		Sequential: request.Sequential, DeadlineDays: uint32Pointer(request.DeadlineDays),
	}
	if request.Status != nil {
		status, statusErr := courseStatusFromProto(request.GetStatus())
		if statusErr != nil {
			return nil, statusErr
		}
		input.Status = &status
	}
	if request.Visibility != nil {
		visibility, visibilityErr := courseVisibilityFromProto(request.GetVisibility())
		if visibilityErr != nil {
			return nil, visibilityErr
		}
		input.Visibility = &visibility
	}
	course, err := s.application.UpdateCourse(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateCourseResponse{Course: courseToProto(course)}, nil
}

func (s *Server) DeleteCourse(ctx context.Context, request *academyv1.DeleteCourseRequest) (*academyv1.DeleteCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteCourse(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.DeleteCourseResponse{}, nil
}

func (s *Server) GetCourseSections(ctx context.Context, request *academyv1.GetCourseSectionsRequest) (*academyv1.GetCourseSectionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	sections, err := s.application.GetCourseSections(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCourseSectionsResponse{Sections: sectionsToProto(sections)}, nil
}

func (s *Server) CreateCourseSection(ctx context.Context, request *academyv1.CreateCourseSectionRequest) (*academyv1.CreateCourseSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	section, err := s.application.CreateCourseSection(ctx, actor, application.CreateCourseSectionInput{
		CourseID: courseID, Title: request.GetTitle(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateCourseSectionResponse{Section: sectionToProto(section)}, nil
}

func (s *Server) UpdateCourseSection(ctx context.Context, request *academyv1.UpdateCourseSectionRequest) (*academyv1.UpdateCourseSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	section, err := s.application.UpdateCourseSection(ctx, actor, application.UpdateCourseSectionInput{
		ID: id, Title: request.GetTitle(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateCourseSectionResponse{Section: sectionToProto(section)}, nil
}

func (s *Server) DeleteCourseSection(ctx context.Context, request *academyv1.DeleteCourseSectionRequest) (*academyv1.DeleteCourseSectionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteCourseSection(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.DeleteCourseSectionResponse{}, nil
}

func (s *Server) GetLessons(ctx context.Context, request *academyv1.GetLessonsRequest) (*academyv1.GetLessonsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseOptionalUUID(request.CourseId)
	if err != nil {
		return nil, err
	}
	lessons, err := s.application.GetLessons(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := lessonsToProto(lessons)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetLessonsResponse{Lessons: converted}, nil
}

func (s *Server) CreateLesson(ctx context.Context, request *academyv1.CreateLessonRequest) (*academyv1.CreateLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	sectionID, err := parseUUID(request.GetSectionId())
	if err != nil {
		return nil, err
	}
	sourceArticleID, err := parseOptionalUUID(request.SourceArticleId)
	if err != nil {
		return nil, err
	}
	content, err := structToContent(request.GetContent())
	if err != nil {
		return nil, invalidArgument("Некорректное содержимое урока")
	}
	input := application.CreateLessonInput{
		CourseID: courseID, SectionID: sectionID, Title: request.GetTitle(),
		Content: content, SourceArticleID: sourceArticleID,
	}
	if request.SourceMode != nil {
		mode, modeErr := sourceModeFromProto(request.GetSourceMode())
		if modeErr != nil {
			return nil, modeErr
		}
		input.SourceMode = &mode
	}
	lesson, err := s.application.CreateLesson(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := lessonToProto(lesson)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.CreateLessonResponse{Lesson: converted}, nil
}

func (s *Server) UpdateLesson(ctx context.Context, request *academyv1.UpdateLessonRequest) (*academyv1.UpdateLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	sourceArticleID, err := parseOptionalUUID(request.SourceArticleId)
	if err != nil {
		return nil, err
	}
	content, err := structToContent(request.GetContent())
	if err != nil {
		return nil, invalidArgument("Некорректное содержимое урока")
	}
	input := application.UpdateLessonInput{
		ID: id, Title: request.Title, Content: content, SourceArticleID: sourceArticleID,
	}
	if request.SourceMode != nil {
		mode, modeErr := sourceModeFromProto(request.GetSourceMode())
		if modeErr != nil {
			return nil, modeErr
		}
		input.SourceMode = &mode
	}
	lesson, err := s.application.UpdateLesson(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := lessonToProto(lesson)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpdateLessonResponse{Lesson: converted}, nil
}

func (s *Server) DeleteLesson(ctx context.Context, request *academyv1.DeleteLessonRequest) (*academyv1.DeleteLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteLesson(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.DeleteLessonResponse{}, nil
}

func (s *Server) MoveLesson(ctx context.Context, request *academyv1.MoveLessonRequest) (*academyv1.MoveLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetId())
	if err != nil {
		return nil, err
	}
	sectionID, err := parseUUID(request.GetSectionId())
	if err != nil {
		return nil, err
	}
	lesson, err := s.application.MoveLesson(ctx, actor, application.MoveLessonInput{
		ID: id, SectionID: sectionID, Order: int32(min(request.GetOrder(), uint32(1)<<30)),
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := lessonToProto(lesson)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.MoveLessonResponse{Lesson: converted}, nil
}

func (s *Server) GetQuizzes(ctx context.Context, request *academyv1.GetQuizzesRequest) (*academyv1.GetQuizzesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	lessonID, err := parseOptionalUUID(request.LessonId)
	if err != nil {
		return nil, err
	}
	quizzes, err := s.application.GetQuizzes(ctx, actor, lessonID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := quizzesToProto(quizzes)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetQuizzesResponse{Quizzes: converted}, nil
}

func (s *Server) UpsertQuiz(ctx context.Context, request *academyv1.UpsertQuizRequest) (*academyv1.UpsertQuizResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseOptionalUUID(request.Id)
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonId())
	if err != nil {
		return nil, err
	}
	questions, err := questionsFromProto(request.GetQuestions())
	if err != nil {
		return nil, err
	}
	quiz, err := s.application.UpsertQuiz(ctx, actor, application.UpsertQuizInput{
		ID: id, LessonID: lessonID, Questions: questions,
		PassingScore: int32(min(request.GetPassingScore(), uint32(101))),
		MaxAttempts:  uint32Pointer(request.MaxAttempts),
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := quizToProto(quiz)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.UpsertQuizResponse{Quiz: converted}, nil
}

func (s *Server) GetAssignments(ctx context.Context, _ *academyv1.GetAssignmentsRequest) (*academyv1.GetAssignmentsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	assignments, err := s.application.GetAssignments(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetAssignmentsResponse{Assignments: assignmentsToProto(assignments)}, nil
}

func (s *Server) AssignCourse(ctx context.Context, request *academyv1.AssignCourseRequest) (*academyv1.AssignCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	assigneeType, err := assigneeTypeFromProto(request.GetAssigneeType())
	if err != nil {
		return nil, err
	}
	assigneeID, err := parseOptionalUUID(request.AssigneeId)
	if err != nil {
		return nil, err
	}
	versionID, err := parseOptionalUUID(request.CourseVersionId)
	if err != nil {
		return nil, err
	}
	input := application.AssignCourseInput{
		CourseID: courseID, CourseVersionID: versionID, AssigneeType: assigneeType, AssigneeID: assigneeID,
	}
	if request.GetDueDate().IsValid() {
		dueDate := request.GetDueDate().AsTime()
		input.DueDate = &dueDate
	}
	assignment, err := s.application.AssignCourse(ctx, actor, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.AssignCourseResponse{Assignment: assignmentToProto(assignment)}, nil
}

func (s *Server) RevokeAssignment(ctx context.Context, request *academyv1.RevokeAssignmentRequest) (*academyv1.RevokeAssignmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	assignmentID, err := parseUUID(request.GetAssignmentId())
	if err != nil {
		return nil, err
	}
	if err = s.application.RevokeAssignment(ctx, actor, assignmentID); err != nil {
		return nil, transportError(err)
	}
	return &academyv1.RevokeAssignmentResponse{}, nil
}

func (s *Server) GetProgress(ctx context.Context, request *academyv1.GetProgressRequest) (*academyv1.GetProgressResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseOptionalUUID(request.CourseId)
	if err != nil {
		return nil, err
	}
	progress, err := s.application.GetProgress(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetProgressResponse{Progress: progressListToProto(progress)}, nil
}

func (s *Server) MarkLessonComplete(ctx context.Context, request *academyv1.MarkLessonCompleteRequest) (*academyv1.MarkLessonCompleteResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonId())
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	userID, err := parseOptionalUUID(request.UserId)
	if err != nil {
		return nil, err
	}
	progress, err := s.application.MarkLessonComplete(ctx, actor, application.MarkLessonCompleteInput{
		CourseID: courseID, LessonID: lessonID, UserID: userID,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.MarkLessonCompleteResponse{Progress: progressToProto(progress)}, nil
}

func parseUUIDList(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := parseUUID(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func uint32Pointer(value *uint32) *int32 {
	if value == nil {
		return nil
	}
	converted := int32(min(*value, uint32(1)<<30))
	return &converted
}
