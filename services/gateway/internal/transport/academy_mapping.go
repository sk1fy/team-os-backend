package transport

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func courseFromProto(value *academyv1.Course) (api.Course, error) {
	if value == nil {
		return api.Course{}, errors.New("academy returned an empty course")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Course{}, err
	}
	authorID, err := uuid.Parse(value.GetAuthorId())
	if err != nil {
		return api.Course{}, err
	}
	status, err := courseStatusFromProto(value.GetStatus())
	if err != nil {
		return api.Course{}, err
	}
	result := api.Course{
		Id: id, Title: value.GetTitle(), Status: status, AuthorId: authorID,
		Sequential:  value.GetSequential(),
		Description: value.Description, CoverUrl: value.CoverUrl,
	}
	if value.GetCreatedAt() != nil {
		result.CreatedAt = value.GetCreatedAt().AsTime()
	}
	if value.GetUpdatedAt() != nil {
		result.UpdatedAt = value.GetUpdatedAt().AsTime()
	}
	if value.DeadlineDays != nil {
		days := int(value.GetDeadlineDays())
		result.DeadlineDays = &days
	}
	return result, nil
}

func coursesFromProto(values []*academyv1.Course) ([]api.Course, error) {
	result := make([]api.Course, len(values))
	for index, value := range values {
		converted, err := courseFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func courseSectionFromProto(value *academyv1.CourseSection) (api.CourseSection, error) {
	if value == nil {
		return api.CourseSection{}, errors.New("academy returned an empty course section")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.CourseSection{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.CourseSection{}, err
	}
	return api.CourseSection{
		Id: id, CourseId: courseID, Title: value.GetTitle(), Order: int(value.GetOrder()),
	}, nil
}

func courseSectionsFromProto(values []*academyv1.CourseSection) ([]api.CourseSection, error) {
	result := make([]api.CourseSection, len(values))
	for index, value := range values {
		converted, err := courseSectionFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func lessonFromProto(value *academyv1.Lesson) (api.Lesson, error) {
	if value == nil {
		return api.Lesson{}, errors.New("academy returned an empty lesson")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Lesson{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.Lesson{}, err
	}
	sectionID, err := uuid.Parse(value.GetSectionId())
	if err != nil {
		return api.Lesson{}, err
	}
	content, err := richTextFromStruct(value.GetContent())
	if err != nil {
		return api.Lesson{}, err
	}
	result := api.Lesson{
		Id: id, CourseId: courseID, SectionId: sectionID,
		Title: value.GetTitle(), Order: int(value.GetOrder()), Content: content,
	}
	if value.SourceArticleId != nil {
		articleID, parseErr := uuid.Parse(value.GetSourceArticleId())
		if parseErr != nil {
			return api.Lesson{}, parseErr
		}
		result.SourceArticleId = &articleID
	}
	if value.SourceMode != nil {
		mode, modeErr := lessonSourceModeFromProto(value.GetSourceMode())
		if modeErr != nil {
			return api.Lesson{}, modeErr
		}
		result.SourceMode = &mode
	}
	if value.QuizId != nil {
		quizID, parseErr := uuid.Parse(value.GetQuizId())
		if parseErr != nil {
			return api.Lesson{}, parseErr
		}
		result.QuizId = &quizID
	}
	return result, nil
}

func lessonsFromProto(values []*academyv1.Lesson) ([]api.Lesson, error) {
	result := make([]api.Lesson, len(values))
	for index, value := range values {
		converted, err := lessonFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func quizFromProto(value *academyv1.Quiz) (api.Quiz, error) {
	if value == nil {
		return api.Quiz{}, errors.New("academy returned an empty quiz")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.Quiz{}, err
	}
	lessonID, err := uuid.Parse(value.GetLessonId())
	if err != nil {
		return api.Quiz{}, err
	}
	questions, err := quizQuestionsFromProto(value.GetQuestions())
	if err != nil {
		return api.Quiz{}, err
	}
	result := api.Quiz{
		Id: id, LessonId: lessonID, Questions: questions,
		PassingScore: int(value.GetPassingScore()),
	}
	if value.MaxAttempts != nil {
		attempts := int(value.GetMaxAttempts())
		result.MaxAttempts = &attempts
	}
	return result, nil
}

func quizzesFromProto(values []*academyv1.Quiz) ([]api.Quiz, error) {
	result := make([]api.Quiz, len(values))
	for index, value := range values {
		converted, err := quizFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func quizQuestionsFromProto(values []*academyv1.QuizQuestion) ([]api.QuizQuestion, error) {
	result := make([]api.QuizQuestion, len(values))
	for index, value := range values {
		id, err := uuid.Parse(value.GetId())
		if err != nil {
			return nil, err
		}
		questionType, err := quizQuestionTypeFromProto(value.GetType())
		if err != nil {
			return nil, err
		}
		options := make([]api.QuizOption, len(value.GetOptions()))
		for optionIndex, option := range value.GetOptions() {
			optionID, parseErr := uuid.Parse(option.GetId())
			if parseErr != nil {
				return nil, parseErr
			}
			options[optionIndex] = api.QuizOption{
				Id: optionID, Text: option.GetText(), Correct: option.GetCorrect(),
			}
		}
		result[index] = api.QuizQuestion{
			Id: id, Type: questionType, Text: value.GetText(), Options: options,
		}
	}
	return result, nil
}

func quizQuestionsToProto(values []api.QuizQuestion) ([]*academyv1.QuizQuestion, error) {
	result := make([]*academyv1.QuizQuestion, len(values))
	for index, value := range values {
		questionType, err := quizQuestionTypeToProto(value.Type)
		if err != nil {
			return nil, err
		}
		options := make([]*academyv1.QuizOption, len(value.Options))
		for optionIndex, option := range value.Options {
			options[optionIndex] = &academyv1.QuizOption{
				Id: option.Id.String(), Text: option.Text, Correct: option.Correct,
			}
		}
		result[index] = &academyv1.QuizQuestion{
			Id: value.Id.String(), Type: questionType, Text: value.Text, Options: options,
		}
	}
	return result, nil
}

func assignmentFromProto(value *academyv1.CourseAssignment) (api.CourseAssignment, error) {
	if value == nil {
		return api.CourseAssignment{}, errors.New("academy returned an empty assignment")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.CourseAssignment{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.CourseAssignment{}, err
	}
	assignedByID, err := uuid.Parse(value.GetAssignedById())
	if err != nil {
		return api.CourseAssignment{}, err
	}
	assigneeType, err := assigneeTypeFromProto(value.GetAssigneeType())
	if err != nil {
		return api.CourseAssignment{}, err
	}
	result := api.CourseAssignment{
		Id: id, CourseId: courseID, AssigneeType: assigneeType,
		AssignedById: assignedByID, InviteToken: value.InviteToken,
	}
	if value.GetCreatedAt() != nil {
		result.CreatedAt = value.GetCreatedAt().AsTime()
	}
	if value.AssigneeId != nil {
		assigneeID, parseErr := uuid.Parse(value.GetAssigneeId())
		if parseErr != nil {
			return api.CourseAssignment{}, parseErr
		}
		result.AssigneeId = &assigneeID
	}
	if value.GetDueDate() != nil {
		dueDate := value.GetDueDate().AsTime()
		result.DueDate = &dueDate
	}
	return result, nil
}

func assignmentsFromProto(values []*academyv1.CourseAssignment) ([]api.CourseAssignment, error) {
	result := make([]api.CourseAssignment, len(values))
	for index, value := range values {
		converted, err := assignmentFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func courseProgressFromProto(value *academyv1.CourseProgress) (api.CourseProgress, error) {
	if value == nil {
		return api.CourseProgress{}, errors.New("academy returned empty progress")
	}
	userID, err := uuid.Parse(value.GetUserId())
	if err != nil {
		return api.CourseProgress{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.CourseProgress{}, err
	}
	status, err := progressStatusFromProto(value.GetStatus())
	if err != nil {
		return api.CourseProgress{}, err
	}
	completed := make([]api.ID, len(value.GetCompletedLessonIds()))
	for index, lessonID := range value.GetCompletedLessonIds() {
		parsed, parseErr := uuid.Parse(lessonID)
		if parseErr != nil {
			return api.CourseProgress{}, parseErr
		}
		completed[index] = parsed
	}
	attempts := make([]api.QuizAttempt, len(value.GetQuizAttempts()))
	for index, attempt := range value.GetQuizAttempts() {
		converted, convertErr := quizAttemptFromProto(attempt)
		if convertErr != nil {
			return api.CourseProgress{}, convertErr
		}
		attempts[index] = converted
	}
	result := api.CourseProgress{
		UserId: userID, CourseId: courseID, Status: status,
		CompletedLessonIds: completed, QuizAttempts: attempts,
	}
	if value.GetStartedAt() != nil {
		startedAt := value.GetStartedAt().AsTime()
		result.StartedAt = &startedAt
	}
	if value.GetCompletedAt() != nil {
		completedAt := value.GetCompletedAt().AsTime()
		result.CompletedAt = &completedAt
	}
	return result, nil
}

func courseProgressListFromProto(values []*academyv1.CourseProgress) ([]api.CourseProgress, error) {
	result := make([]api.CourseProgress, len(values))
	for index, value := range values {
		converted, err := courseProgressFromProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func quizAttemptFromProto(value *academyv1.QuizAttempt) (api.QuizAttempt, error) {
	if value == nil {
		return api.QuizAttempt{}, errors.New("academy returned an empty quiz attempt")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.QuizAttempt{}, err
	}
	quizID, err := uuid.Parse(value.GetQuizId())
	if err != nil {
		return api.QuizAttempt{}, err
	}
	userID, err := uuid.Parse(value.GetUserId())
	if err != nil {
		return api.QuizAttempt{}, err
	}
	result := api.QuizAttempt{
		Id: id, QuizId: quizID, UserId: userID, Score: int(value.GetScore()),
		Passed: value.GetPassed(), PendingReview: value.GetPendingReview(),
	}
	if value.GetCreatedAt() != nil {
		result.CreatedAt = value.GetCreatedAt().AsTime()
	}
	return result, nil
}

func courseStatusFromProto(value academyv1.CourseStatus) (api.CourseStatus, error) {
	switch value {
	case academyv1.CourseStatus_COURSE_STATUS_DRAFT:
		return api.CourseStatusDraft, nil
	case academyv1.CourseStatus_COURSE_STATUS_PUBLISHED:
		return api.CourseStatusPublished, nil
	default:
		return "", fmt.Errorf("unknown course status %d", value)
	}
}

func courseStatusToProto(value api.CourseStatus) (academyv1.CourseStatus, error) {
	switch value {
	case api.CourseStatusDraft:
		return academyv1.CourseStatus_COURSE_STATUS_DRAFT, nil
	case api.CourseStatusPublished:
		return academyv1.CourseStatus_COURSE_STATUS_PUBLISHED, nil
	default:
		return academyv1.CourseStatus_COURSE_STATUS_UNSPECIFIED,
			fmt.Errorf("unknown course status %q", value)
	}
}

func lessonSourceModeFromProto(value academyv1.LessonSourceMode) (api.LessonSourceMode, error) {
	switch value {
	case academyv1.LessonSourceMode_LESSON_SOURCE_MODE_LINK:
		return api.Link, nil
	case academyv1.LessonSourceMode_LESSON_SOURCE_MODE_COPY:
		return api.Copy, nil
	default:
		return "", fmt.Errorf("unknown lesson source mode %d", value)
	}
}

func lessonSourceModeToProto(value api.LessonSourceMode) (academyv1.LessonSourceMode, error) {
	switch value {
	case api.Link:
		return academyv1.LessonSourceMode_LESSON_SOURCE_MODE_LINK, nil
	case api.Copy:
		return academyv1.LessonSourceMode_LESSON_SOURCE_MODE_COPY, nil
	default:
		return academyv1.LessonSourceMode_LESSON_SOURCE_MODE_UNSPECIFIED,
			fmt.Errorf("unknown lesson source mode %q", value)
	}
}

func quizQuestionTypeFromProto(value academyv1.QuizQuestionType) (api.QuizQuestionType, error) {
	switch value {
	case academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_SINGLE:
		return api.Single, nil
	case academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_MULTIPLE:
		return api.Multiple, nil
	case academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_OPEN:
		return api.Open, nil
	default:
		return "", fmt.Errorf("unknown quiz question type %d", value)
	}
}

func quizQuestionTypeToProto(value api.QuizQuestionType) (academyv1.QuizQuestionType, error) {
	switch value {
	case api.Single:
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_SINGLE, nil
	case api.Multiple:
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_MULTIPLE, nil
	case api.Open:
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_OPEN, nil
	default:
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_UNSPECIFIED,
			fmt.Errorf("unknown quiz question type %q", value)
	}
}

func assigneeTypeFromProto(value academyv1.AssigneeType) (api.AssigneeType, error) {
	switch value {
	case academyv1.AssigneeType_ASSIGNEE_TYPE_USER:
		return api.AssigneeTypeUser, nil
	case academyv1.AssigneeType_ASSIGNEE_TYPE_POSITION:
		return api.AssigneeTypePosition, nil
	case academyv1.AssigneeType_ASSIGNEE_TYPE_DEPARTMENT:
		return api.AssigneeTypeDepartment, nil
	case academyv1.AssigneeType_ASSIGNEE_TYPE_EXTERNAL:
		return api.AssigneeTypeExternal, nil
	default:
		return "", fmt.Errorf("unknown assignee type %d", value)
	}
}

func assigneeTypeToProto(value api.AssigneeType) (academyv1.AssigneeType, error) {
	switch value {
	case api.AssigneeTypeUser:
		return academyv1.AssigneeType_ASSIGNEE_TYPE_USER, nil
	case api.AssigneeTypePosition:
		return academyv1.AssigneeType_ASSIGNEE_TYPE_POSITION, nil
	case api.AssigneeTypeDepartment:
		return academyv1.AssigneeType_ASSIGNEE_TYPE_DEPARTMENT, nil
	case api.AssigneeTypeExternal:
		return academyv1.AssigneeType_ASSIGNEE_TYPE_EXTERNAL, nil
	default:
		return academyv1.AssigneeType_ASSIGNEE_TYPE_UNSPECIFIED,
			fmt.Errorf("unknown assignee type %q", value)
	}
}

func progressStatusFromProto(value academyv1.CourseProgressStatus) (api.CourseProgressStatus, error) {
	switch value {
	case academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_NOT_STARTED:
		return api.CourseProgressStatusNotStarted, nil
	case academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_IN_PROGRESS:
		return api.CourseProgressStatusInProgress, nil
	case academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_COMPLETED:
		return api.CourseProgressStatusCompleted, nil
	case academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_OVERDUE:
		return api.CourseProgressStatusOverdue, nil
	default:
		return "", fmt.Errorf("unknown course progress status %d", value)
	}
}
