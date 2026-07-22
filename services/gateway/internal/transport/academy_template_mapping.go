package transport

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func courseTemplateFromProto(value *academyv1.CourseTemplate) (api.CourseTemplate, error) {
	if value == nil || value.GetCreatedAt() == nil {
		return api.CourseTemplate{}, errors.New("academy returned an empty course template")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.CourseTemplate{}, err
	}
	createdByID, err := uuid.Parse(value.GetCreatedById())
	if err != nil {
		return api.CourseTemplate{}, err
	}
	templateType, err := courseTemplateTypeFromProto(value.GetType())
	if err != nil {
		return api.CourseTemplate{}, err
	}
	lifecycle, err := courseTemplateLifecycleFromProto(value.GetLifecycleStatus())
	if err != nil {
		return api.CourseTemplate{}, err
	}
	result := api.CourseTemplate{
		Id: id, Type: templateType, LifecycleStatus: lifecycle,
		SystemTemplateKey: value.SystemTemplateKey, CreatedById: createdByID,
		CreatedAt: value.GetCreatedAt().AsTime(),
	}
	if value.GetUpdatedAt() != nil {
		updatedAt := value.GetUpdatedAt().AsTime()
		result.UpdatedAt = &updatedAt
	}
	if result.CurrentDraftVersionId, err = parseOptionalProtoUUID(value.CurrentDraftVersionId); err != nil {
		return api.CourseTemplate{}, err
	}
	if result.LatestPublishedVersionId, err = parseOptionalProtoUUID(value.LatestPublishedVersionId); err != nil {
		return api.CourseTemplate{}, err
	}
	return result, nil
}

func courseTemplatesFromProto(values []*academyv1.CourseTemplate) ([]api.CourseTemplate, error) {
	result := make([]api.CourseTemplate, len(values))
	for index := range values {
		converted, err := courseTemplateFromProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func courseTemplateDetailsFromProto(value *academyv1.GetCourseTemplateResponse) (api.CourseTemplate, error) {
	if value == nil {
		return api.CourseTemplate{}, errors.New("academy returned an empty course template response")
	}
	result, err := courseTemplateFromProto(value.GetTemplate())
	if err != nil {
		return api.CourseTemplate{}, err
	}
	versions := make([]api.CourseTemplateVersion, len(value.GetVersions()))
	for index, version := range value.GetVersions() {
		versions[index], err = courseTemplateVersionFromProto(version, nil)
		if err != nil {
			return api.CourseTemplate{}, err
		}
	}
	result.Versions = &versions
	if selected := value.GetSelectedVersion(); selected != nil {
		converted, convertErr := courseTemplateVersionFromProto(selected.GetVersion(), selected.GetContent())
		if convertErr != nil {
			return api.CourseTemplate{}, convertErr
		}
		result.SelectedVersion = &converted
	}
	return result, nil
}

func courseTemplateVersionFromProto(
	value *academyv1.CourseTemplateVersion,
	content *academyv1.CourseTemplateVersionContent,
) (api.CourseTemplateVersion, error) {
	if value == nil || value.GetCreatedAt() == nil {
		return api.CourseTemplateVersion{}, errors.New("academy returned an empty course template version")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.CourseTemplateVersion{}, err
	}
	templateID, err := uuid.Parse(value.GetTemplateId())
	if err != nil {
		return api.CourseTemplateVersion{}, err
	}
	createdByID, err := uuid.Parse(value.GetCreatedById())
	if err != nil {
		return api.CourseTemplateVersion{}, err
	}
	statusValue, err := courseVersionStatusFromProto(value.GetStatus())
	if err != nil {
		return api.CourseTemplateVersion{}, err
	}
	sequential := value.GetSequential()
	result := api.CourseTemplateVersion{
		Id: id, TemplateId: templateID, Number: int(value.GetNumber()), Status: statusValue,
		Title: value.GetTitle(), Description: value.Description, Sequential: &sequential,
		CreatedById: createdByID, CreatedAt: value.GetCreatedAt().AsTime(), ContentHash: value.ContentHash,
	}
	if result.CoverFileId, err = parseOptionalProtoUUID(value.CoverFileId); err != nil {
		return api.CourseTemplateVersion{}, err
	}
	if result.PublishedById, err = parseOptionalProtoUUID(value.PublishedById); err != nil {
		return api.CourseTemplateVersion{}, err
	}
	if value.GetPublishedAt() != nil {
		publishedAt := value.GetPublishedAt().AsTime()
		result.PublishedAt = &publishedAt
	}
	if content != nil {
		converted, contentErr := courseTemplateVersionContentFromProto(content)
		if contentErr != nil {
			return api.CourseTemplateVersion{}, contentErr
		}
		result.Content = &converted
	}
	return result, nil
}

func courseTemplateVersionContentFromProto(value *academyv1.CourseTemplateVersionContent) (api.CourseTemplateVersionContent, error) {
	result := api.CourseTemplateVersionContent{
		Sections: make([]api.CourseTemplateVersionSection, len(value.GetSections())),
		Lessons:  make([]api.CourseTemplateVersionLesson, len(value.GetLessons())),
		Quizzes:  make([]api.CourseTemplateVersionQuiz, len(value.GetQuizzes())),
	}
	for index, section := range value.GetSections() {
		id, err := uuid.Parse(section.GetId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		versionID, err := uuid.Parse(section.GetTemplateVersionId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		result.Sections[index] = api.CourseTemplateVersionSection{
			Id: id, TemplateVersionId: versionID, StableKey: section.GetStableKey(),
			Title: section.GetTitle(), Order: int(section.GetOrder()),
		}
	}
	for index, lesson := range value.GetLessons() {
		id, err := uuid.Parse(lesson.GetId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		versionID, err := uuid.Parse(lesson.GetTemplateVersionId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		sectionID, err := uuid.Parse(lesson.GetSectionVersionId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		content, err := richTextFromStruct(lesson.GetContent())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		sourceType, err := courseLessonSourceTypeFromProto(lesson.GetSourceType())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		converted := api.CourseTemplateVersionLesson{
			Id: id, TemplateVersionId: versionID, SectionVersionId: sectionID,
			StableKey: lesson.GetStableKey(), Title: lesson.GetTitle(), Order: int(lesson.GetOrder()),
			Content: content, SourceType: sourceType,
		}
		if converted.SourceArticleId, err = parseOptionalProtoUUID(lesson.SourceArticleId); err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		if converted.QuizVersionId, err = parseOptionalProtoUUID(lesson.QuizVersionId); err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		if lesson.SourceArticleVersion != nil {
			version := int(lesson.GetSourceArticleVersion())
			converted.SourceArticleVersion = &version
		}
		if lesson.EstimatedMinutes != nil {
			minutes := int(lesson.GetEstimatedMinutes())
			converted.EstimatedMinutes = &minutes
		}
		result.Lessons[index] = converted
	}
	for index, quiz := range value.GetQuizzes() {
		id, err := uuid.Parse(quiz.GetId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		versionID, err := uuid.Parse(quiz.GetTemplateVersionId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		lessonID, err := uuid.Parse(quiz.GetLessonVersionId())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		questions, err := quizQuestionsFromProto(quiz.GetQuestions())
		if err != nil {
			return api.CourseTemplateVersionContent{}, err
		}
		converted := api.CourseTemplateVersionQuiz{
			Id: id, TemplateVersionId: versionID, LessonVersionId: lessonID,
			Questions: questions, PassingScore: int(quiz.GetPassingScore()),
		}
		if quiz.MaxAttempts != nil {
			attempts := int(quiz.GetMaxAttempts())
			converted.MaxAttempts = &attempts
		}
		result.Quizzes[index] = converted
	}
	return result, nil
}

func courseTemplateContentToProto(value *api.CourseTemplateDraftContentInput) (*academyv1.CourseTemplateDraftContentInput, error) {
	if value == nil {
		return nil, nil
	}
	result := &academyv1.CourseTemplateDraftContentInput{Sections: make([]*academyv1.CourseTemplateDraftSectionInput, len(value.Sections))}
	for sectionIndex, section := range value.Sections {
		converted := &academyv1.CourseTemplateDraftSectionInput{
			StableKey: section.StableKey, Title: section.Title, Order: uint32(max(0, section.Order)),
			Lessons: make([]*academyv1.CourseTemplateDraftLessonInput, len(section.Lessons)),
		}
		for lessonIndex, lesson := range section.Lessons {
			content, err := richTextToStruct(lesson.Content)
			if err != nil {
				return nil, err
			}
			protoLesson := &academyv1.CourseTemplateDraftLessonInput{
				StableKey: lesson.StableKey, Title: lesson.Title, Order: uint32(max(0, lesson.Order)), Content: content,
			}
			if lesson.SourceType != nil {
				sourceType, sourceErr := courseLessonSourceTypeToProto(*lesson.SourceType)
				if sourceErr != nil {
					return nil, sourceErr
				}
				protoLesson.SourceType = &sourceType
			}
			if lesson.SourceArticleId != nil {
				value := lesson.SourceArticleId.String()
				protoLesson.SourceArticleId = &value
			}
			if lesson.SourceArticleVersion != nil {
				if *lesson.SourceArticleVersion < 0 {
					return nil, fmt.Errorf("negative article version")
				}
				value := uint32(*lesson.SourceArticleVersion)
				protoLesson.SourceArticleVersion = &value
			}
			if lesson.EstimatedMinutes != nil {
				if *lesson.EstimatedMinutes < 0 {
					return nil, fmt.Errorf("negative estimated minutes")
				}
				value := uint32(*lesson.EstimatedMinutes)
				protoLesson.EstimatedMinutes = &value
			}
			if lesson.Quiz != nil {
				questions, questionsErr := quizQuestionsToProto(lesson.Quiz.Questions)
				if questionsErr != nil {
					return nil, questionsErr
				}
				protoLesson.Quiz = &academyv1.CourseTemplateDraftQuizInput{
					Questions: questions, PassingScore: uint32(max(0, lesson.Quiz.PassingScore)),
				}
				if lesson.Quiz.MaxAttempts != nil {
					if *lesson.Quiz.MaxAttempts < 0 {
						return nil, fmt.Errorf("negative max attempts")
					}
					value := uint32(*lesson.Quiz.MaxAttempts)
					protoLesson.Quiz.MaxAttempts = &value
				}
			}
			converted.Lessons[lessonIndex] = protoLesson
		}
		result.Sections[sectionIndex] = converted
	}
	return result, nil
}

func courseTemplateTypeFromProto(value academyv1.CourseTemplateType) (api.CourseTemplateType, error) {
	switch value {
	case academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_SYSTEM:
		return api.CourseTemplateTypeSystem, nil
	case academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_COMPANY:
		return api.CourseTemplateTypeCompany, nil
	default:
		return "", fmt.Errorf("unknown course template type %d", value)
	}
}

func courseTemplateTypeToProto(value api.CourseTemplateType) (academyv1.CourseTemplateType, error) {
	switch value {
	case api.CourseTemplateTypeSystem:
		return academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_SYSTEM, nil
	case api.CourseTemplateTypeCompany:
		return academyv1.CourseTemplateType_COURSE_TEMPLATE_TYPE_COMPANY, nil
	default:
		return 0, fmt.Errorf("unknown course template type %q", value)
	}
}

func courseTemplateLifecycleFromProto(value academyv1.CourseTemplateLifecycleStatus) (api.CourseTemplateLifecycleStatus, error) {
	switch value {
	case academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ACTIVE:
		return api.CourseTemplateLifecycleStatusActive, nil
	case academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ARCHIVED:
		return api.CourseTemplateLifecycleStatusArchived, nil
	default:
		return "", fmt.Errorf("unknown course template lifecycle %d", value)
	}
}

func courseTemplateLifecycleToProto(value api.CourseTemplateLifecycleStatus) (academyv1.CourseTemplateLifecycleStatus, error) {
	switch value {
	case api.CourseTemplateLifecycleStatusActive:
		return academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ACTIVE, nil
	case api.CourseTemplateLifecycleStatusArchived:
		return academyv1.CourseTemplateLifecycleStatus_COURSE_TEMPLATE_LIFECYCLE_STATUS_ARCHIVED, nil
	default:
		return 0, fmt.Errorf("unknown course template lifecycle %q", value)
	}
}
