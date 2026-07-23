package grpc

import (
	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func courseTemplateToProto(value application.CourseTemplate) *academyv1.CourseTemplate {
	return &academyv1.CourseTemplate{
		Id: value.ID.String(), CompanyId: value.CompanyID.String(), Type: courseTemplateTypeToProto(value.Type),
		SystemTemplateKey: value.SystemTemplateKey, LifecycleStatus: courseTemplateLifecycleToProto(value.LifecycleStatus),
		CurrentDraftVersionId:    optionalUUIDString(value.CurrentDraftVersionID),
		LatestPublishedVersionId: optionalUUIDString(value.LatestPublishedVersionID),
		CreatedById:              value.CreatedByID.String(), CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
		UpdatedAt: timestamppb.New(value.UpdatedAt.UTC()),
	}
}

func academyTemplateSummaryToProto(value application.AcademyTemplateSummary) *academyv1.AcademyTemplateSummary {
	result := &academyv1.AcademyTemplateSummary{
		Id: value.ID.String(), OwnerType: value.OwnerType, Title: value.Title,
		Description: value.Description, CoverUrl: value.CoverURL, Category: value.Category,
		LessonCount: uint32(max(0, value.LessonCount)), Archived: value.Archived,
		LatestVersionId:   optionalUUIDString(value.LatestVersionID),
		DraftVersionId:    optionalUUIDString(value.DraftVersionID),
		SystemTemplateKey: value.SystemTemplateKey,
		Capabilities: &academyv1.AcademyTemplateCapabilities{
			CanInstantiate: value.Capabilities.CanInstantiate,
			CanEdit:        value.Capabilities.CanEdit,
			CanArchive:     value.Capabilities.CanArchive,
			CanPreview:     value.Capabilities.CanPreview,
		},
	}
	if value.LatestVersionNumber != nil {
		number := uint32(max(0, *value.LatestVersionNumber))
		result.LatestVersionNumber = &number
	}
	return result
}

func academyTemplateSummariesToProto(values []application.AcademyTemplateSummary) []*academyv1.AcademyTemplateSummary {
	result := make([]*academyv1.AcademyTemplateSummary, len(values))
	for index := range values {
		result[index] = academyTemplateSummaryToProto(values[index])
	}
	return result
}

func courseTemplateVersionToProto(value application.CourseTemplateVersion) *academyv1.CourseTemplateVersion {
	result := &academyv1.CourseTemplateVersion{
		Id: value.ID.String(), TemplateId: value.TemplateID.String(), Number: uint32(max(0, value.Number)),
		Status: courseVersionStatusToProto(value.Status), Title: value.Title, Description: value.Description,
		CoverFileId: optionalUUIDString(value.CoverFileID), Sequential: value.Sequential,
		CreatedById: value.CreatedByID.String(), CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
		PublishedById: optionalUUIDString(value.PublishedByID), ContentHash: value.ContentHash,
	}
	if value.PublishedAt != nil {
		result.PublishedAt = timestamppb.New(value.PublishedAt.UTC())
	}
	return result
}

func courseTemplateVersionsToProto(values []application.CourseTemplateVersion) []*academyv1.CourseTemplateVersion {
	result := make([]*academyv1.CourseTemplateVersion, len(values))
	for index := range values {
		result[index] = courseTemplateVersionToProto(values[index])
	}
	return result
}

func courseTemplateDetailsToProto(value application.CourseTemplateDetails) (*academyv1.GetCourseTemplateResponse, error) {
	result := &academyv1.GetCourseTemplateResponse{
		Template: courseTemplateToProto(value.Template), Versions: courseTemplateVersionsToProto(value.Versions),
	}
	if value.SelectedVersion != nil {
		selected, err := courseTemplateVersionDetailsToProto(*value.SelectedVersion)
		if err != nil {
			return nil, err
		}
		result.SelectedVersion = selected
	}
	return result, nil
}

func courseTemplateVersionDetailsToProto(value application.CourseTemplateVersionDetails) (*academyv1.CourseTemplateVersionDetails, error) {
	sections := make([]*academyv1.CourseTemplateVersionSection, len(value.Content.Sections))
	for index, section := range value.Content.Sections {
		sections[index] = &academyv1.CourseTemplateVersionSection{
			Id: section.ID.String(), TemplateVersionId: section.TemplateVersionID.String(), StableKey: section.StableKey,
			Title: section.Title, Order: uint32(max(0, section.Order)),
		}
	}
	lessons := make([]*academyv1.CourseTemplateVersionLesson, len(value.Content.Lessons))
	for index, lesson := range value.Content.Lessons {
		content, err := contentToStruct(lesson.Content)
		if err != nil {
			return nil, err
		}
		converted := &academyv1.CourseTemplateVersionLesson{
			Id: lesson.ID.String(), TemplateVersionId: lesson.TemplateVersionID.String(),
			SectionVersionId: lesson.SectionVersionID.String(), StableKey: lesson.StableKey,
			Title: lesson.Title, Order: uint32(max(0, lesson.Order)), Content: content,
			SourceType:      courseLessonSourceTypeToProto(lesson.SourceType),
			SourceArticleId: optionalUUIDString(lesson.SourceArticleID), QuizVersionId: optionalUUIDString(lesson.QuizVersionID),
		}
		if lesson.SourceArticleVersion != nil && *lesson.SourceArticleVersion > 0 {
			version := uint32(*lesson.SourceArticleVersion)
			converted.SourceArticleVersion = &version
		}
		if lesson.EstimatedMinutes != nil && *lesson.EstimatedMinutes > 0 {
			minutes := uint32(*lesson.EstimatedMinutes)
			converted.EstimatedMinutes = &minutes
		}
		lessons[index] = converted
	}
	quizzes := make([]*academyv1.CourseTemplateVersionQuiz, len(value.Content.Quizzes))
	for index, quiz := range value.Content.Quizzes {
		questions, err := questionsToProto(quiz.Questions)
		if err != nil {
			return nil, err
		}
		converted := &academyv1.CourseTemplateVersionQuiz{
			Id: quiz.ID.String(), TemplateVersionId: quiz.TemplateVersionID.String(),
			LessonVersionId: quiz.LessonVersionID.String(), Questions: questions,
			PassingScore: uint32(max(0, quiz.PassingScore)),
		}
		if quiz.MaxAttempts != nil && *quiz.MaxAttempts > 0 {
			attempts := uint32(*quiz.MaxAttempts)
			converted.MaxAttempts = &attempts
		}
		quizzes[index] = converted
	}
	return &academyv1.CourseTemplateVersionDetails{
		Version: courseTemplateVersionToProto(value.Version),
		Content: &academyv1.CourseTemplateVersionContent{Sections: sections, Lessons: lessons, Quizzes: quizzes},
	}, nil
}

func courseTemplateContentFromProto(value *academyv1.CourseTemplateDraftContentInput) (*application.CourseTemplateDraftContentInput, error) {
	if value == nil {
		return nil, nil
	}
	result := &application.CourseTemplateDraftContentInput{Sections: make([]application.CourseTemplateDraftSectionInput, len(value.GetSections()))}
	for sectionIndex, section := range value.GetSections() {
		convertedSection := application.CourseTemplateDraftSectionInput{
			StableKey: section.GetStableKey(), Title: section.GetTitle(), Order: int32(section.GetOrder()),
			Lessons: make([]application.CourseTemplateDraftLessonInput, len(section.GetLessons())),
		}
		for lessonIndex, lesson := range section.GetLessons() {
			content, err := structToContent(lesson.GetContent())
			if err != nil {
				return nil, err
			}
			sourceType := "manual"
			if lesson.SourceType != nil {
				sourceType, err = courseLessonSourceTypeFromProto(lesson.GetSourceType())
				if err != nil {
					return nil, err
				}
			}
			sourceArticleID, err := parseOptionalUUID(lesson.SourceArticleId)
			if err != nil {
				return nil, err
			}
			convertedLesson := application.CourseTemplateDraftLessonInput{
				StableKey: lesson.GetStableKey(), Title: lesson.GetTitle(), Order: int32(lesson.GetOrder()),
				Content: content, SourceType: sourceType, SourceArticleID: sourceArticleID,
				SourceArticleVersion: uint32Pointer(lesson.SourceArticleVersion),
				EstimatedMinutes:     uint32Pointer(lesson.EstimatedMinutes),
			}
			if lesson.GetQuiz() != nil {
				questions, questionsErr := questionsFromProto(lesson.GetQuiz().GetQuestions())
				if questionsErr != nil {
					return nil, questionsErr
				}
				convertedLesson.Quiz = &application.CourseTemplateDraftQuizInput{
					Questions: questions, PassingScore: int32(lesson.GetQuiz().GetPassingScore()),
					MaxAttempts: uint32Pointer(lesson.GetQuiz().MaxAttempts),
				}
			}
			convertedSection.Lessons[lessonIndex] = convertedLesson
		}
		result.Sections[sectionIndex] = convertedSection
	}
	return result, nil
}

func courseTemplateInstantiationToProto(value application.CourseTemplateInstantiationResult) *academyv1.CourseTemplateInstantiationResult {
	return &academyv1.CourseTemplateInstantiationResult{
		Course: courseToProto(value.Course), Draft: courseVersionToProto(value.Draft), Origin: courseOriginToProto(value.Origin),
	}
}

func courseTemplateTypeToProto(value string) academyv1.CourseTemplateType {
	switch value {
	case "system":
		return academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_SYSTEM
	case "company":
		return academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_COMPANY
	default:
		return academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_UNSPECIFIED
	}
}

func courseTemplateTypeFromProto(value academyv1.CourseTemplateType) (string, error) {
	switch value {
	case academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_SYSTEM:
		return "system", nil
	case academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_COMPANY:
		return "company", nil
	default:
		return "", invalidArgument("Некорректный тип шаблона курса")
	}
}

func courseTemplateLifecycleToProto(value string) academyv1.CourseTemplateLifecycleStatus {
	switch value {
	case "active":
		return academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ACTIVE
	case "archived":
		return academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ARCHIVED
	default:
		return academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_UNSPECIFIED
	}
}

func courseTemplateLifecycleFromProto(value academyv1.CourseTemplateLifecycleStatus) (string, error) {
	switch value {
	case academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ACTIVE:
		return "active", nil
	case academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ARCHIVED:
		return "archived", nil
	default:
		return "", invalidArgument("Некорректное состояние шаблона курса")
	}
}

var _ = uuid.Nil
