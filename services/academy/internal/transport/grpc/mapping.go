package grpc

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func courseToProto(value application.Course) *academyv1.Course {
	course := &academyv1.Course{
		Id: value.ID.String(), Title: value.Title,
		Description: value.Description, CoverUrl: value.CoverURL,
		Status:     courseStatusToProto(value.Status),
		Visibility: courseVisibilityToProto(value.Visibility),
		AuthorId:   value.AuthorID.String(),
		Sequential: value.Sequential,
		CreatedAt:  timestamppb.New(value.CreatedAt.UTC()),
		UpdatedAt:  timestamppb.New(value.UpdatedAt.UTC()),
	}
	if value.DeadlineDays != nil && *value.DeadlineDays > 0 {
		days := uint32(*value.DeadlineDays)
		course.DeadlineDays = &days
	}
	return course
}

func courseVisibilityToProto(value string) academyv1.CourseVisibility {
	switch value {
	case "public":
		return academyv1.CourseVisibility_COURSE_VISIBILITY_PUBLIC
	case "company":
		return academyv1.CourseVisibility_COURSE_VISIBILITY_COMPANY
	case "restricted":
		return academyv1.CourseVisibility_COURSE_VISIBILITY_RESTRICTED
	default:
		return academyv1.CourseVisibility_COURSE_VISIBILITY_UNSPECIFIED
	}
}

func courseVisibilityFromProto(value academyv1.CourseVisibility) (string, error) {
	switch value {
	case academyv1.CourseVisibility_COURSE_VISIBILITY_PUBLIC:
		return "public", nil
	case academyv1.CourseVisibility_COURSE_VISIBILITY_COMPANY:
		return "company", nil
	case academyv1.CourseVisibility_COURSE_VISIBILITY_RESTRICTED:
		return "restricted", nil
	default:
		return "", invalidArgument("Некорректная видимость курса")
	}
}

func coursesToProto(values []application.Course) []*academyv1.Course {
	result := make([]*academyv1.Course, len(values))
	for index := range values {
		result[index] = courseToProto(values[index])
	}
	return result
}

func sectionToProto(value application.CourseSection) *academyv1.CourseSection {
	return &academyv1.CourseSection{
		Id: value.ID.String(), CourseId: value.CourseID.String(),
		Title: value.Title, Order: uint32(max(0, value.Order)),
	}
}

func sectionsToProto(values []application.CourseSection) []*academyv1.CourseSection {
	result := make([]*academyv1.CourseSection, len(values))
	for index := range values {
		result[index] = sectionToProto(values[index])
	}
	return result
}

func lessonToProto(value application.Lesson) (*academyv1.Lesson, error) {
	content, err := contentToStruct(value.Content)
	if err != nil {
		return nil, err
	}
	lesson := &academyv1.Lesson{
		Id: value.ID.String(), CourseId: value.CourseID.String(),
		SectionId: value.SectionID.String(), Title: value.Title,
		Order: uint32(max(0, value.Order)), Content: content,
		SourceArticleId: optionalUUIDString(value.SourceArticleID),
		QuizId:          optionalUUIDString(value.QuizID),
	}
	if value.SourceMode != nil {
		mode, modeErr := sourceModeToProto(*value.SourceMode)
		if modeErr != nil {
			return nil, modeErr
		}
		lesson.SourceMode = &mode
	}
	return lesson, nil
}

func lessonsToProto(values []application.Lesson) ([]*academyv1.Lesson, error) {
	result := make([]*academyv1.Lesson, len(values))
	for index, value := range values {
		converted, err := lessonToProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func quizToProto(value application.Quiz) (*academyv1.Quiz, error) {
	questions, err := questionsToProto(value.Questions)
	if err != nil {
		return nil, err
	}
	quiz := &academyv1.Quiz{
		Id: value.ID.String(), LessonId: value.LessonID.String(),
		Questions: questions, PassingScore: uint32(max(0, value.PassingScore)),
	}
	if value.MaxAttempts != nil && *value.MaxAttempts > 0 {
		attempts := uint32(*value.MaxAttempts)
		quiz.MaxAttempts = &attempts
	}
	return quiz, nil
}

func quizzesToProto(values []application.Quiz) ([]*academyv1.Quiz, error) {
	result := make([]*academyv1.Quiz, len(values))
	for index, value := range values {
		converted, err := quizToProto(value)
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

// quizQuestionJSON mirrors the QuizQuestion frontend type stored as jsonb.
type quizQuestionJSON struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Options []struct {
		ID      string `json:"id"`
		Text    string `json:"text"`
		Correct bool   `json:"correct"`
	} `json:"options"`
}

func questionsToProto(raw json.RawMessage) ([]*academyv1.QuizQuestion, error) {
	if len(raw) == 0 {
		return []*academyv1.QuizQuestion{}, nil
	}
	var questions []quizQuestionJSON
	if err := json.Unmarshal(raw, &questions); err != nil {
		return nil, fmt.Errorf("decode quiz questions: %w", err)
	}
	result := make([]*academyv1.QuizQuestion, len(questions))
	for index, question := range questions {
		questionType, err := questionTypeToProto(question.Type)
		if err != nil {
			return nil, err
		}
		options := make([]*academyv1.QuizOption, len(question.Options))
		for optionIndex, option := range question.Options {
			options[optionIndex] = &academyv1.QuizOption{
				Id: option.ID, Text: option.Text, Correct: option.Correct,
			}
		}
		result[index] = &academyv1.QuizQuestion{
			Id: question.ID, Type: questionType, Text: question.Text, Options: options,
		}
	}
	return result, nil
}

func questionsFromProto(values []*academyv1.QuizQuestion) (json.RawMessage, error) {
	questions := make([]quizQuestionJSON, len(values))
	for index, value := range values {
		questionType, err := questionTypeFromProto(value.GetType())
		if err != nil {
			return nil, err
		}
		question := quizQuestionJSON{
			ID: value.GetId(), Type: questionType, Text: value.GetText(),
		}
		for _, option := range value.GetOptions() {
			question.Options = append(question.Options, struct {
				ID      string `json:"id"`
				Text    string `json:"text"`
				Correct bool   `json:"correct"`
			}{ID: option.GetId(), Text: option.GetText(), Correct: option.GetCorrect()})
		}
		questions[index] = question
	}
	encoded, err := json.Marshal(questions)
	if err != nil {
		return nil, fmt.Errorf("encode quiz questions: %w", err)
	}
	return encoded, nil
}

func assignmentToProto(value application.Assignment) *academyv1.CourseAssignment {
	assignment := &academyv1.CourseAssignment{
		Id: value.ID.String(), CourseId: value.CourseID.String(),
		AssigneeType: assigneeTypeToProto(value.AssigneeType),
		AssigneeId:   optionalUUIDString(value.AssigneeID),
		InviteToken:  value.InviteToken,
		AssignedById: value.AssignedByID.String(),
		CreatedAt:    timestamppb.New(value.CreatedAt.UTC()),
	}
	if value.DueDate != nil {
		assignment.DueDate = timestamppb.New(value.DueDate.UTC())
	}
	return assignment
}

func assignmentsToProto(values []application.Assignment) []*academyv1.CourseAssignment {
	result := make([]*academyv1.CourseAssignment, len(values))
	for index := range values {
		result[index] = assignmentToProto(values[index])
	}
	return result
}

func progressToProto(value application.Progress) *academyv1.CourseProgress {
	attempts := make([]*academyv1.QuizAttempt, len(value.QuizAttempts))
	for index, attempt := range value.QuizAttempts {
		attempts[index] = &academyv1.QuizAttempt{
			Id: attempt.ID.String(), QuizId: attempt.QuizID.String(),
			UserId: attempt.UserID.String(), Score: uint32(max(0, attempt.Score)),
			Passed: attempt.Passed, PendingReview: attempt.PendingReview,
			CreatedAt: timestamppb.New(attempt.CreatedAt.UTC()),
		}
	}
	progress := &academyv1.CourseProgress{
		UserId: value.UserID.String(), CourseId: value.CourseID.String(),
		Status:             progressStatusToProto(value.Status),
		CompletedLessonIds: uuidStrings(value.CompletedLessonIDs),
		QuizAttempts:       attempts,
	}
	if value.StartedAt != nil {
		progress.StartedAt = timestamppb.New(value.StartedAt.UTC())
	}
	if value.CompletedAt != nil {
		progress.CompletedAt = timestamppb.New(value.CompletedAt.UTC())
	}
	return progress
}

func progressListToProto(values []application.Progress) []*academyv1.CourseProgress {
	result := make([]*academyv1.CourseProgress, len(values))
	for index := range values {
		result[index] = progressToProto(values[index])
	}
	return result
}

func courseStatusToProto(status string) academyv1.CourseStatus {
	switch status {
	case "draft":
		return academyv1.CourseStatus_COURSE_STATUS_DRAFT
	case "published":
		return academyv1.CourseStatus_COURSE_STATUS_PUBLISHED
	default:
		return academyv1.CourseStatus_COURSE_STATUS_UNSPECIFIED
	}
}

func courseStatusFromProto(status academyv1.CourseStatus) (string, error) {
	switch status {
	case academyv1.CourseStatus_COURSE_STATUS_DRAFT:
		return "draft", nil
	case academyv1.CourseStatus_COURSE_STATUS_PUBLISHED:
		return "published", nil
	default:
		return "", invalidArgument("Некорректный статус курса")
	}
}

func sourceModeToProto(mode string) (academyv1.LessonSourceMode, error) {
	switch mode {
	case "link":
		return academyv1.LessonSourceMode_LESSON_SOURCE_MODE_LINK, nil
	case "copy":
		return academyv1.LessonSourceMode_LESSON_SOURCE_MODE_COPY, nil
	default:
		return academyv1.LessonSourceMode_LESSON_SOURCE_MODE_UNSPECIFIED,
			fmt.Errorf("unknown lesson source mode %q", mode)
	}
}

func sourceModeFromProto(mode academyv1.LessonSourceMode) (string, error) {
	switch mode {
	case academyv1.LessonSourceMode_LESSON_SOURCE_MODE_LINK:
		return "link", nil
	case academyv1.LessonSourceMode_LESSON_SOURCE_MODE_COPY:
		return "copy", nil
	default:
		return "", invalidArgument("Некорректный режим импорта статьи")
	}
}

func questionTypeToProto(value string) (academyv1.QuizQuestionType, error) {
	switch value {
	case "single":
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_SINGLE, nil
	case "multiple":
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_MULTIPLE, nil
	case "open":
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_OPEN, nil
	default:
		return academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_UNSPECIFIED,
			fmt.Errorf("unknown quiz question type %q", value)
	}
}

func questionTypeFromProto(value academyv1.QuizQuestionType) (string, error) {
	switch value {
	case academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_SINGLE:
		return "single", nil
	case academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_MULTIPLE:
		return "multiple", nil
	case academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_OPEN:
		return "open", nil
	default:
		return "", invalidArgument("Некорректный тип вопроса")
	}
}

func assigneeTypeToProto(value string) academyv1.AssigneeType {
	switch value {
	case "user":
		return academyv1.AssigneeType_ASSIGNEE_TYPE_USER
	case "position":
		return academyv1.AssigneeType_ASSIGNEE_TYPE_POSITION
	case "department":
		return academyv1.AssigneeType_ASSIGNEE_TYPE_DEPARTMENT
	case "external":
		return academyv1.AssigneeType_ASSIGNEE_TYPE_EXTERNAL
	default:
		return academyv1.AssigneeType_ASSIGNEE_TYPE_UNSPECIFIED
	}
}

func assigneeTypeFromProto(value academyv1.AssigneeType) (string, error) {
	switch value {
	case academyv1.AssigneeType_ASSIGNEE_TYPE_USER:
		return "user", nil
	case academyv1.AssigneeType_ASSIGNEE_TYPE_POSITION:
		return "position", nil
	case academyv1.AssigneeType_ASSIGNEE_TYPE_DEPARTMENT:
		return "department", nil
	case academyv1.AssigneeType_ASSIGNEE_TYPE_EXTERNAL:
		return "external", nil
	default:
		return "", invalidArgument("Некорректный тип назначения")
	}
}

func progressStatusToProto(value string) academyv1.CourseProgressStatus {
	switch value {
	case "not_started":
		return academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_NOT_STARTED
	case "in_progress":
		return academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_IN_PROGRESS
	case "completed":
		return academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_COMPLETED
	case "overdue":
		return academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_OVERDUE
	default:
		return academyv1.CourseProgressStatus_COURSE_PROGRESS_STATUS_UNSPECIFIED
	}
}

func contentToStruct(raw json.RawMessage) (*structpb.Struct, error) {
	if len(raw) == 0 {
		return structpb.NewStruct(map[string]any{"type": "doc"})
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode rich text content: %w", err)
	}
	return structpb.NewStruct(decoded)
}

func structToContent(value *structpb.Struct) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value.AsMap())
	if err != nil {
		return nil, fmt.Errorf("encode rich text content: %w", err)
	}
	return encoded, nil
}

func optionalUUIDString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].String()
	}
	return result
}

func parseUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalidArgument("Некорректный идентификатор")
	}
	return parsed, nil
}

func parseOptionalUUID(value *string) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseUUID(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
