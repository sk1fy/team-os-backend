package transport

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func courseVersionAuthorDetailFromProto(value *academyv1.GetCourseVersionResponse) (api.CourseVersionAuthorDetail, error) {
	if value == nil || value.GetVersion() == nil || value.GetVersion().GetCreatedAt() == nil {
		return api.CourseVersionAuthorDetail{}, errors.New("academy returned empty course version")
	}
	version := value.GetVersion()
	id, err := uuid.Parse(version.GetId())
	if err != nil {
		return api.CourseVersionAuthorDetail{}, err
	}
	courseID, err := uuid.Parse(version.GetCourseId())
	if err != nil {
		return api.CourseVersionAuthorDetail{}, err
	}
	var status api.CourseVersionAuthorDetailStatus
	switch version.GetStatus() {
	case academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_DRAFT:
		status = api.CourseVersionAuthorDetailStatusDraft
	case academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_PUBLISHED:
		status = api.CourseVersionAuthorDetailStatusPublished
	case academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_RETIRED:
		// Learner preview has no lifecycle status. Retired versions remain
		// immutable published snapshots and are represented as published when
		// this author-shaped intermediate is used by the learner adapter.
		status = api.CourseVersionAuthorDetailStatusPublished
	default:
		return api.CourseVersionAuthorDetail{}, fmt.Errorf("unsupported author version status %s", version.GetStatus())
	}
	createdAt := version.GetCreatedAt().AsTime()
	result := api.CourseVersionAuthorDetail{
		Id: id, CourseId: courseID, VersionNumber: int(version.GetNumber()), Status: status,
		Title: version.GetTitle(), Description: version.Description, Sequential: version.GetSequential(),
		CreatedAt: createdAt, UpdatedAt: createdAt, Sections: []api.SectionAuthor{},
	}
	if version.DefaultInternalDeadlineDays != nil {
		days := int(version.GetDefaultInternalDeadlineDays())
		result.DeadlineDays = &days
	}
	if version.GetPublishedAt() != nil {
		publishedAt := version.GetPublishedAt().AsTime()
		result.PublishedAt = &publishedAt
		// course_versions currently has no updated_at; publication is the last
		// durable mutation timestamp for an immutable published version.
		result.UpdatedAt = publishedAt
	}
	quizzes := make(map[string]*api.Quiz, len(value.GetQuizzes()))
	for _, quiz := range value.GetQuizzes() {
		quizID, parseErr := uuid.Parse(quiz.GetId())
		if parseErr != nil {
			return api.CourseVersionAuthorDetail{}, parseErr
		}
		lessonID, parseErr := uuid.Parse(quiz.GetLessonVersionId())
		if parseErr != nil {
			return api.CourseVersionAuthorDetail{}, parseErr
		}
		questions, convertErr := quizQuestionsFromProto(quiz.GetQuestions())
		if convertErr != nil {
			return api.CourseVersionAuthorDetail{}, convertErr
		}
		converted := &api.Quiz{Id: quizID, LessonId: lessonID, Questions: questions, PassingScore: int(quiz.GetPassingScore())}
		if quiz.MaxAttempts != nil {
			attempts := int(quiz.GetMaxAttempts())
			converted.MaxAttempts = &attempts
		}
		quizzes[quiz.GetLessonVersionId()] = converted
	}
	lessons := make(map[string][]api.LessonAuthor, len(value.GetSections()))
	for _, lesson := range value.GetLessons() {
		lessonID, parseErr := uuid.Parse(lesson.GetId())
		if parseErr != nil {
			return api.CourseVersionAuthorDetail{}, parseErr
		}
		sectionID, parseErr := uuid.Parse(lesson.GetSectionVersionId())
		if parseErr != nil {
			return api.CourseVersionAuthorDetail{}, parseErr
		}
		content, convertErr := richTextFromStruct(lesson.GetContent())
		if convertErr != nil {
			return api.CourseVersionAuthorDetail{}, convertErr
		}
		converted := api.LessonAuthor{
			Id: lessonID, CourseId: courseID, SectionId: sectionID, VersionId: id,
			Title: lesson.GetTitle(), Order: int(lesson.GetOrder()), Content: content,
			Quiz: quizzes[lesson.GetId()],
		}
		if converted.SourceArticleId, parseErr = parseOptionalProtoUUID(lesson.SourceArticleId); parseErr != nil {
			return api.CourseVersionAuthorDetail{}, parseErr
		}
		if lesson.EstimatedMinutes != nil {
			minutes := int(lesson.GetEstimatedMinutes())
			converted.EstimatedMinutes = &minutes
		}
		switch lesson.GetSourceType() {
		case academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_KB_LINK:
			mode := api.LessonAuthorSourceMode("link")
			converted.SourceMode = &mode
		case academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_KB_SNAPSHOT:
			mode := api.LessonAuthorSourceMode("copy")
			converted.SourceMode = &mode
		}
		lessons[lesson.GetSectionVersionId()] = append(lessons[lesson.GetSectionVersionId()], converted)
	}
	for _, section := range value.GetSections() {
		sectionID, parseErr := uuid.Parse(section.GetId())
		if parseErr != nil {
			return api.CourseVersionAuthorDetail{}, parseErr
		}
		result.Sections = append(result.Sections, api.SectionAuthor{
			Id: sectionID, CourseId: courseID, VersionId: id, Title: section.GetTitle(),
			Order: int(section.GetOrder()), Lessons: lessons[section.GetId()],
		})
	}
	return result, nil
}

func courseVersionLearnerDetailFromAuthor(value api.CourseVersionAuthorDetail) api.CourseVersionLearnerDetail {
	result := api.CourseVersionLearnerDetail{
		Id: value.Id, CourseId: value.CourseId, VersionNumber: value.VersionNumber,
		Title: value.Title, Description: value.Description, Sequential: value.Sequential,
		Sections: make([]api.SectionLearner, len(value.Sections)),
	}
	for sectionIndex, section := range value.Sections {
		converted := api.SectionLearner{Id: section.Id, Title: section.Title, Order: section.Order, Lessons: []api.LessonLearnerSummary{}}
		for _, lesson := range section.Lessons {
			item := api.LessonLearnerSummary{
				Id: lesson.Id, Title: lesson.Title, Order: lesson.Order, Locked: false, Completed: false,
				HasQuiz: lesson.Quiz != nil, Content: &lesson.Content, EstimatedMinutes: lesson.EstimatedMinutes,
			}
			if lesson.Quiz != nil {
				quiz := api.QuizLearnerDetail{
					Id: lesson.Quiz.Id, LessonId: lesson.Quiz.LessonId,
					PassingScore: lesson.Quiz.PassingScore, MaxAttempts: lesson.Quiz.MaxAttempts,
					Questions: make([]api.QuizLearnerQuestion, len(lesson.Quiz.Questions)),
				}
				for questionIndex, question := range lesson.Quiz.Questions {
					learnerQuestion := api.QuizLearnerQuestion{
						Id: question.Id, Type: question.Type, Text: question.Text,
						Options: make([]api.QuizLearnerOption, len(question.Options)),
					}
					for optionIndex, option := range question.Options {
						learnerQuestion.Options[optionIndex] = api.QuizLearnerOption{Id: option.Id, Text: option.Text}
					}
					quiz.Questions[questionIndex] = learnerQuestion
				}
				item.Quiz = &quiz
			}
			converted.Lessons = append(converted.Lessons, item)
		}
		result.Sections[sectionIndex] = converted
	}
	return result
}

func templateVersionAuthorDetail(value api.CourseTemplateVersion) (api.CourseVersionAuthorDetail, error) {
	if value.Content == nil {
		return api.CourseVersionAuthorDetail{}, errors.New("academy returned template version without content")
	}
	status := api.CourseVersionAuthorDetailStatusPublished
	if value.Status == api.CourseVersionStatusDraft {
		status = api.CourseVersionAuthorDetailStatusDraft
	}
	sequential := value.Sequential != nil && *value.Sequential
	result := api.CourseVersionAuthorDetail{
		Id: value.Id, CourseId: value.TemplateId, VersionNumber: value.Number, Status: status,
		Title: value.Title, Description: value.Description, Sequential: sequential,
		CreatedAt: value.CreatedAt, UpdatedAt: value.CreatedAt, PublishedAt: value.PublishedAt,
		Sections: []api.SectionAuthor{},
	}
	if value.PublishedAt != nil {
		result.UpdatedAt = *value.PublishedAt
	}
	quizzes := make(map[uuid.UUID]*api.Quiz, len(value.Content.Quizzes))
	for _, quiz := range value.Content.Quizzes {
		quizzes[quiz.LessonVersionId] = &api.Quiz{
			Id: quiz.Id, LessonId: quiz.LessonVersionId, Questions: quiz.Questions,
			PassingScore: quiz.PassingScore, MaxAttempts: quiz.MaxAttempts,
		}
	}
	lessons := make(map[uuid.UUID][]api.LessonAuthor, len(value.Content.Sections))
	for _, lesson := range value.Content.Lessons {
		lessons[lesson.SectionVersionId] = append(lessons[lesson.SectionVersionId], api.LessonAuthor{
			Id: lesson.Id, CourseId: value.TemplateId, VersionId: value.Id, SectionId: lesson.SectionVersionId,
			Title: lesson.Title, Order: lesson.Order, Content: lesson.Content, SourceArticleId: lesson.SourceArticleId,
			EstimatedMinutes: lesson.EstimatedMinutes, Quiz: quizzes[lesson.Id],
		})
	}
	for _, section := range value.Content.Sections {
		result.Sections = append(result.Sections, api.SectionAuthor{
			Id: section.Id, CourseId: value.TemplateId, VersionId: value.Id,
			Title: section.Title, Order: section.Order, Lessons: lessons[section.Id],
		})
	}
	return result, nil
}
