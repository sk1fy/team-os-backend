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
	ownerType := courseOwnerTypeToProto(value.OwnerType)
	if ownerType != academyv1.CourseOwnerType_COURSE_OWNER_TYPE_UNSPECIFIED {
		course.OwnerType = &ownerType
	}
	course.OwnerUserId = optionalUUIDString(value.OwnerUserID)
	course.CurrentDraftVersionId = optionalUUIDString(value.CurrentDraftVersionID)
	course.LatestPublishedVersionId = optionalUUIDString(value.LatestPublishedVersionID)
	if value.CreatedByID != uuid.Nil {
		createdByID := value.CreatedByID.String()
		course.CreatedById = &createdByID
	}
	lifecycle := courseLifecycleToProto(value.LifecycleStatus)
	if lifecycle != academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_UNSPECIFIED {
		course.LifecycleStatus = &lifecycle
	}
	distribution := courseDistributionToProto(value.DistributionStatus)
	if distribution != academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_UNSPECIFIED {
		course.DistributionStatus = &distribution
	}
	if value.DeadlineDays != nil && *value.DeadlineDays > 0 {
		days := uint32(*value.DeadlineDays)
		course.DeadlineDays = &days
	}
	return course
}

func courseVersionToProto(value application.CourseVersion) *academyv1.CourseVersion {
	result := &academyv1.CourseVersion{
		Id: value.ID.String(), CourseId: value.CourseID.String(), Number: uint32(max(0, value.Number)),
		Status: courseVersionStatusToProto(value.Status), Title: value.Title, Description: value.Description,
		CoverFileId: optionalUUIDString(value.CoverFileID), CoverUrl: value.CoverURL,
		Sequential: value.Sequential, CreatedById: value.CreatedByID.String(),
		CreatedAt: timestamppb.New(value.CreatedAt.UTC()), PublishedById: optionalUUIDString(value.PublishedByID),
		ContentHash: value.ContentHash,
	}
	if value.DefaultInternalDeadlineDays != nil && *value.DefaultInternalDeadlineDays > 0 {
		days := uint32(*value.DefaultInternalDeadlineDays)
		result.DefaultInternalDeadlineDays = &days
	}
	if value.PublishedAt != nil {
		result.PublishedAt = timestamppb.New(value.PublishedAt.UTC())
	}
	return result
}

func courseVersionsToProto(values []application.CourseVersion) []*academyv1.CourseVersion {
	result := make([]*academyv1.CourseVersion, len(values))
	for index := range values {
		result[index] = courseVersionToProto(values[index])
	}
	return result
}

func courseVersionStatusToProto(value string) academyv1.CourseVersionStatus {
	switch value {
	case "draft":
		return academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_DRAFT
	case "published":
		return academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_PUBLISHED
	case "retired":
		return academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_RETIRED
	default:
		return academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_UNSPECIFIED
	}
}

func courseVersionSectionToProto(value application.CourseVersionSection) *academyv1.CourseVersionSection {
	return &academyv1.CourseVersionSection{
		Id: value.ID.String(), CourseVersionId: value.CourseVersionID.String(),
		Title: value.Title, Order: uint32(max(0, value.Order)),
	}
}

func courseVersionSectionsToProto(values []application.CourseVersionSection) []*academyv1.CourseVersionSection {
	result := make([]*academyv1.CourseVersionSection, len(values))
	for index := range values {
		result[index] = courseVersionSectionToProto(values[index])
	}
	return result
}

func courseLessonSourceTypeToProto(value string) academyv1.CourseLessonSourceType {
	switch value {
	case "manual":
		return academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_MANUAL
	case "kb_link":
		return academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_KB_LINK
	case "kb_snapshot":
		return academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_KB_SNAPSHOT
	case "template_snapshot":
		return academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_TEMPLATE_SNAPSHOT
	default:
		return academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_UNSPECIFIED
	}
}

func courseLessonSourceTypeFromProto(value academyv1.CourseLessonSourceType) (string, error) {
	switch value {
	case academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_MANUAL:
		return "manual", nil
	case academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_KB_LINK:
		return "kb_link", nil
	case academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_KB_SNAPSHOT:
		return "kb_snapshot", nil
	case academyv1.CourseLessonSourceType_COURSE_LESSON_SOURCE_TYPE_TEMPLATE_SNAPSHOT:
		return "template_snapshot", nil
	default:
		return "", invalidArgument("Некорректный источник урока")
	}
}

func courseVersionLessonToProto(value application.CourseVersionLesson) (*academyv1.CourseVersionLesson, error) {
	content, err := contentToStruct(value.Content)
	if err != nil {
		return nil, err
	}
	result := &academyv1.CourseVersionLesson{
		Id: value.ID.String(), CourseVersionId: value.CourseVersionID.String(),
		SectionVersionId: value.SectionVersionID.String(), StableKey: value.StableKey,
		Title: value.Title, Order: uint32(max(0, value.Order)), Content: content,
		SourceType:              courseLessonSourceTypeToProto(value.SourceType),
		SourceArticleId:         optionalUUIDString(value.SourceArticleID),
		SourceTemplateId:        optionalUUIDString(value.SourceTemplateID),
		SourceTemplateVersionId: optionalUUIDString(value.SourceTemplateVersionID),
		QuizVersionId:           optionalUUIDString(value.QuizVersionID),
	}
	if value.SourceArticleVersion != nil && *value.SourceArticleVersion > 0 {
		converted := uint32(*value.SourceArticleVersion)
		result.SourceArticleVersion = &converted
	}
	if value.EstimatedMinutes != nil && *value.EstimatedMinutes > 0 {
		converted := uint32(*value.EstimatedMinutes)
		result.EstimatedMinutes = &converted
	}
	return result, nil
}

func courseVersionLessonsToProto(values []application.CourseVersionLesson) ([]*academyv1.CourseVersionLesson, error) {
	result := make([]*academyv1.CourseVersionLesson, len(values))
	for index := range values {
		converted, err := courseVersionLessonToProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func courseVersionQuizToProto(value application.CourseVersionQuiz) (*academyv1.CourseVersionQuiz, error) {
	questions, err := questionsToProto(value.Questions)
	if err != nil {
		return nil, err
	}
	result := &academyv1.CourseVersionQuiz{
		Id: value.ID.String(), CourseVersionId: value.CourseVersionID.String(),
		LessonVersionId: value.LessonVersionID.String(), Questions: questions,
		PassingScore: uint32(max(0, value.PassingScore)),
	}
	if value.MaxAttempts != nil && *value.MaxAttempts > 0 {
		converted := uint32(*value.MaxAttempts)
		result.MaxAttempts = &converted
	}
	return result, nil
}

func courseVersionQuizzesToProto(values []application.CourseVersionQuiz) ([]*academyv1.CourseVersionQuiz, error) {
	result := make([]*academyv1.CourseVersionQuiz, len(values))
	for index := range values {
		converted, err := courseVersionQuizToProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func learnerPublishedCourseVersionToProto(value application.CourseVersionContent) (*academyv1.LearnerPublishedCourseVersion, error) {
	result := &academyv1.LearnerPublishedCourseVersion{
		Id: value.Version.ID.String(), CourseId: value.Version.CourseID.String(),
		Number: uint32(max(0, value.Version.Number)), Title: value.Version.Title,
		Description: value.Version.Description, CoverUrl: value.Version.CoverURL,
		Sequential: value.Version.Sequential,
	}
	quizzes := make(map[uuid.UUID]application.CourseVersionQuiz, len(value.Quizzes))
	for _, quiz := range value.Quizzes {
		quizzes[quiz.ID] = quiz
	}
	sections := make(map[uuid.UUID]*academyv1.LearnerCourseVersionSection, len(value.Sections))
	for _, section := range value.Sections {
		converted := &academyv1.LearnerCourseVersionSection{
			Id: section.ID.String(), Title: section.Title, Order: uint32(max(0, section.Order)),
		}
		sections[section.ID] = converted
		result.Sections = append(result.Sections, converted)
	}
	for _, lesson := range value.Lessons {
		section := sections[lesson.SectionVersionID]
		if section == nil {
			return nil, fmt.Errorf("lesson %s references missing section", lesson.ID)
		}
		content, err := contentToStruct(lesson.Content)
		if err != nil {
			return nil, err
		}
		converted := &academyv1.LearnerCourseVersionLesson{
			Id: lesson.ID.String(), CourseVersionId: lesson.CourseVersionID.String(),
			SectionVersionId: lesson.SectionVersionID.String(), StableKey: lesson.StableKey,
			Title: lesson.Title, Order: uint32(max(0, lesson.Order)), Content: content,
		}
		if lesson.EstimatedMinutes != nil && *lesson.EstimatedMinutes > 0 {
			minutes := uint32(*lesson.EstimatedMinutes)
			converted.EstimatedMinutes = &minutes
		}
		if lesson.QuizVersionID != nil {
			quiz, ok := quizzes[*lesson.QuizVersionID]
			if ok {
				learnerQuiz, quizErr := learnerCourseVersionQuizToProto(quiz)
				if quizErr != nil {
					return nil, quizErr
				}
				converted.Quiz = learnerQuiz
			}
		}
		section.Lessons = append(section.Lessons, converted)
	}
	return result, nil
}

func learnerCourseVersionQuizToProto(value application.CourseVersionQuiz) (*academyv1.LearnerCourseVersionQuiz, error) {
	questions, err := questionsToProto(value.Questions)
	if err != nil {
		return nil, err
	}
	result := &academyv1.LearnerCourseVersionQuiz{
		Id: value.ID.String(), PassingScore: uint32(max(0, value.PassingScore)),
		Questions: make([]*academyv1.LearnerQuizQuestion, len(questions)),
	}
	if value.MaxAttempts != nil && *value.MaxAttempts > 0 {
		attempts := uint32(*value.MaxAttempts)
		result.MaxAttempts = &attempts
	}
	for index, question := range questions {
		converted := &academyv1.LearnerQuizQuestion{
			Id: question.GetId(), Type: question.GetType(), Text: question.GetText(),
			Options: make([]*academyv1.LearnerQuizOption, len(question.GetOptions())),
		}
		for optionIndex, option := range question.GetOptions() {
			converted.Options[optionIndex] = &academyv1.LearnerQuizOption{Id: option.GetId(), Text: option.GetText()}
		}
		result.Questions[index] = converted
	}
	return result, nil
}

func courseOwnerTypeToProto(value string) academyv1.CourseOwnerType {
	switch value {
	case "company":
		return academyv1.CourseOwnerType_COURSE_OWNER_TYPE_COMPANY
	case "partner":
		return academyv1.CourseOwnerType_COURSE_OWNER_TYPE_PARTNER
	default:
		return academyv1.CourseOwnerType_COURSE_OWNER_TYPE_UNSPECIFIED
	}
}

func courseOwnerTypeFromProto(value academyv1.CourseOwnerType) (string, error) {
	switch value {
	case academyv1.CourseOwnerType_COURSE_OWNER_TYPE_COMPANY:
		return "company", nil
	case academyv1.CourseOwnerType_COURSE_OWNER_TYPE_PARTNER:
		return "partner", nil
	default:
		return "", invalidArgument("Некорректный тип владельца курса")
	}
}

func courseLifecycleToProto(value string) academyv1.CourseLifecycleStatus {
	switch value {
	case "active":
		return academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_ACTIVE
	case "archived":
		return academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_ARCHIVED
	case "deleted":
		return academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_DELETED
	default:
		return academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_UNSPECIFIED
	}
}

func courseLifecycleFromProto(value academyv1.CourseLifecycleStatus) (string, error) {
	switch value {
	case academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_ACTIVE:
		return "active", nil
	case academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_ARCHIVED:
		return "archived", nil
	case academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_DELETED:
		return "deleted", nil
	default:
		return "", invalidArgument("Некорректное состояние курса")
	}
}

func courseDistributionToProto(value string) academyv1.CourseDistributionStatus {
	switch value {
	case "active":
		return academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_ACTIVE
	case "paused":
		return academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_PAUSED
	case "blocked":
		return academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_BLOCKED
	default:
		return academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_UNSPECIFIED
	}
}

func courseDistributionFromProto(value academyv1.CourseDistributionStatus) (string, error) {
	switch value {
	case academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_ACTIVE:
		return "active", nil
	case academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_PAUSED:
		return "paused", nil
	case academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_BLOCKED:
		return "blocked", nil
	default:
		return "", invalidArgument("Некорректное состояние распространения курса")
	}
}

func courseOriginTypeFromProto(value academyv1.CourseOriginType) (string, error) {
	switch value {
	case academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_PARTNER_COURSE:
		return "partner_course", nil
	case academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_SYSTEM_TEMPLATE:
		return "system_template", nil
	case academyv1.CourseOriginType_COURSE_ORIGIN_TYPE_COMPANY_TEMPLATE:
		return "company_template", nil
	default:
		return "", invalidArgument("Некорректный тип происхождения курса")
	}
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
		CourseVersionId: optionalUUIDString(value.CourseVersionID),
		AssigneeType:    assigneeTypeToProto(value.AssigneeType),
		AssigneeId:      optionalUUIDString(value.AssigneeID),
		InviteToken:     value.InviteToken,
		AssignedById:    value.AssignedByID.String(),
		CreatedAt:       timestamppb.New(value.CreatedAt.UTC()),
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
	progress.EnrollmentId = optionalUUIDString(value.EnrollmentID)
	progress.CourseVersionId = optionalUUIDString(value.CourseVersionID)
	progress.CurrentLessonVersionId = optionalUUIDString(value.CurrentLessonVersionID)
	if value.ProgressPercent != nil && *value.ProgressPercent >= 0 {
		percent := uint32(*value.ProgressPercent)
		progress.ProgressPercent = &percent
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
