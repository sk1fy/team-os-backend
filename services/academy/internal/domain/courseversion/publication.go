package courseversion

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
)

var (
	ErrCourseMismatch                   = errors.New("Версия принадлежит другому курсу")
	ErrCourseBlocked                    = errors.New("Курс временно заблокирован")
	ErrCourseDeleted                    = course.ErrCourseDeleted
	ErrCourseTitleRequired              = errors.New("Для публикации требуется название курса")
	ErrDefaultDeadlineInvalid           = errors.New("Срок прохождения должен быть не меньше одного дня")
	ErrSectionRequired                  = errors.New("Для публикации требуется хотя бы один раздел")
	ErrSectionIDRequired                = errors.New("Для раздела требуется идентификатор")
	ErrSectionTitleRequired             = errors.New("Для раздела требуется название")
	ErrDuplicateSectionID               = errors.New("Идентификатор раздела повторяется")
	ErrSectionOrderInvalid              = errors.New("Порядок разделов должен начинаться с нуля и не содержать пропусков")
	ErrLessonRequired                   = errors.New("Для публикации требуется хотя бы один урок")
	ErrSectionHasNoLessons              = errors.New("В каждом разделе должен быть хотя бы один урок")
	ErrLessonIDRequired                 = errors.New("Для урока требуется идентификатор")
	ErrLessonTitleRequired              = errors.New("Для урока требуется название")
	ErrLessonStableKeyRequired          = errors.New("Для урока требуется стабильный ключ")
	ErrDuplicateLessonID                = errors.New("Идентификатор урока повторяется")
	ErrDuplicateLessonStableKey         = errors.New("Стабильный ключ урока повторяется")
	ErrLessonSectionMissing             = errors.New("Урок ссылается на отсутствующий раздел")
	ErrLessonOrderInvalid               = errors.New("Порядок уроков в разделе должен начинаться с нуля и не содержать пропусков")
	ErrRichTextValidatorRequired        = errors.New("Не настроена проверка TipTap JSON")
	ErrInvalidLessonContent             = errors.New("Некорректный TipTap-документ урока")
	ErrFileAvailabilityCheckerRequired  = errors.New("Не настроена проверка доступности файлов")
	ErrFileUnavailable                  = errors.New("Файл курса недоступен")
	ErrQuizIDRequired                   = errors.New("Для теста требуется идентификатор")
	ErrDuplicateQuizID                  = errors.New("Идентификатор теста повторяется")
	ErrQuizQuestionsRequired            = errors.New("Для теста требуется хотя бы один вопрос")
	ErrPassingScoreInvalid              = errors.New("Проходной балл должен быть от 0 до 100")
	ErrMaxAttemptsInvalid               = errors.New("Число попыток должно быть не меньше одной")
	ErrQuestionIDRequired               = errors.New("Для вопроса требуется идентификатор")
	ErrQuestionTextRequired             = errors.New("Для вопроса требуется текст")
	ErrUnknownQuestionType              = errors.New("Неизвестный тип вопроса")
	ErrDuplicateQuestionID              = errors.New("Идентификатор вопроса повторяется")
	ErrClosedQuestionOptionsRequired    = errors.New("Для закрытого вопроса требуется не меньше двух вариантов")
	ErrClosedQuestionWithoutCorrect     = errors.New("Закрытый вопрос должен иметь правильный вариант")
	ErrSingleQuestionCorrectAnswerCount = errors.New("В вопросе с одним ответом должен быть ровно один правильный вариант")
	ErrOpenQuestionOptionsForbidden     = errors.New("Открытый вопрос не должен содержать варианты ответа")
	ErrOptionIDRequired                 = errors.New("Для варианта ответа требуется идентификатор")
	ErrOptionTextRequired               = errors.New("Для варианта ответа требуется текст")
	ErrDuplicateOptionID                = errors.New("Идентификатор варианта ответа повторяется")
)

// RichTextValidator is the domain boundary for the shared TipTap validator.
// Application code can pass richtext.Validate without coupling this package to
// a particular parser implementation.
type RichTextValidator func(json.RawMessage) error

// FileAvailability is a pre-resolved, side-effect-free file access check.
// Network calls must happen before entering the domain operation.
type FileAvailability func(ID) bool

// PublicationValidators contains pure validation boundaries required to
// freeze a draft.
type PublicationValidators struct {
	RichText      RichTextValidator
	FileAvailable FileAvailability
}

// Publish validates and atomically freezes the draft in memory. The caller is
// responsible for persisting the resulting snapshot in the same transaction
// that updates course pointers, audit, and outbox.
func (v *Version) Publish(params PublishParams, root course.Course, validators PublicationValidators) error {
	if v == nil {
		return ErrVersionIDRequired
	}
	switch v.snapshot.Status {
	case StatusDraft:
	case StatusPublished:
		return ErrPublishedVersionImmutable
	case StatusRetired:
		return ErrRetiredVersionImmutable
	default:
		return ErrUnknownStatus
	}
	if params.ActorID == "" {
		return ErrPublisherRequired
	}
	if params.At.IsZero() {
		return ErrPublishedAtRequired
	}
	if params.ActorID != v.snapshot.CreatedByID {
		return ErrDraftOwnerMismatch
	}
	if err := root.Validate(); err != nil {
		return err
	}
	if ID(root.ID) != v.snapshot.CourseID || ID(root.CompanyID) != v.snapshot.CompanyID {
		return ErrCourseMismatch
	}
	if root.LifecycleStatus == course.CourseDeleted {
		return ErrCourseDeleted
	}
	if root.DistributionStatus == course.DistributionBlocked {
		return ErrCourseBlocked
	}
	if err := ValidateDefinitionForPublication(v.snapshot.Definition, validators); err != nil {
		return err
	}

	publisher := params.ActorID
	publishedAt := params.At.UTC()
	v.snapshot.Status = StatusPublished
	v.snapshot.PublishedByID = &publisher
	v.snapshot.PublishedAt = &publishedAt
	v.snapshot.ContentHash = definitionHash(v.snapshot.Definition)
	return nil
}

// ValidateDefinitionForPublication validates a complete course snapshot. It is
// exported so previews can show publication errors without changing status.
func ValidateDefinitionForPublication(definition Definition, validators PublicationValidators) error {
	if strings.TrimSpace(definition.Title) == "" {
		return ErrCourseTitleRequired
	}
	if definition.DefaultInternalDeadlineDays != nil && *definition.DefaultInternalDeadlineDays < 1 {
		return ErrDefaultDeadlineInvalid
	}
	if definition.CoverFileID != nil {
		if err := validateFile(*definition.CoverFileID, validators.FileAvailable); err != nil {
			return fmt.Errorf("обложка: %w", err)
		}
	}
	if len(definition.Sections) == 0 {
		return ErrSectionRequired
	}
	if len(definition.Lessons) == 0 {
		return ErrLessonRequired
	}

	sections := make(map[ID]Section, len(definition.Sections))
	sectionOrders := make([]bool, len(definition.Sections))
	for _, section := range definition.Sections {
		switch {
		case section.ID == "":
			return ErrSectionIDRequired
		case strings.TrimSpace(section.Title) == "":
			return fmt.Errorf("раздел %q: %w", section.ID, ErrSectionTitleRequired)
		}
		if _, exists := sections[section.ID]; exists {
			return fmt.Errorf("раздел %q: %w", section.ID, ErrDuplicateSectionID)
		}
		if section.Order < 0 || section.Order >= len(sectionOrders) || sectionOrders[section.Order] {
			return ErrSectionOrderInvalid
		}
		sectionOrders[section.Order] = true
		sections[section.ID] = section
	}

	lessonIDs := make(map[ID]struct{}, len(definition.Lessons))
	stableKeys := make(map[string]struct{}, len(definition.Lessons))
	lessonsBySection := make(map[ID][]Lesson, len(definition.Sections))
	quizIDs := make(map[ID]struct{})
	for _, lesson := range definition.Lessons {
		switch {
		case lesson.ID == "":
			return ErrLessonIDRequired
		case strings.TrimSpace(lesson.Title) == "":
			return fmt.Errorf("урок %q: %w", lesson.ID, ErrLessonTitleRequired)
		case strings.TrimSpace(lesson.StableKey) == "":
			return fmt.Errorf("урок %q: %w", lesson.ID, ErrLessonStableKeyRequired)
		}
		if _, exists := lessonIDs[lesson.ID]; exists {
			return fmt.Errorf("урок %q: %w", lesson.ID, ErrDuplicateLessonID)
		}
		lessonIDs[lesson.ID] = struct{}{}
		if _, exists := stableKeys[lesson.StableKey]; exists {
			return fmt.Errorf("урок %q: %w", lesson.ID, ErrDuplicateLessonStableKey)
		}
		stableKeys[lesson.StableKey] = struct{}{}
		if _, exists := sections[lesson.SectionID]; !exists {
			return fmt.Errorf("урок %q: %w", lesson.ID, ErrLessonSectionMissing)
		}
		if validators.RichText == nil {
			return ErrRichTextValidatorRequired
		}
		if err := validators.RichText(lesson.Content); err != nil {
			return fmt.Errorf("урок %q: %w: %w", lesson.ID, ErrInvalidLessonContent, err)
		}
		for _, fileID := range lesson.FileIDs {
			if err := validateFile(fileID, validators.FileAvailable); err != nil {
				return fmt.Errorf("урок %q: %w", lesson.ID, err)
			}
		}
		if lesson.Quiz != nil {
			if _, exists := quizIDs[lesson.Quiz.ID]; exists && lesson.Quiz.ID != "" {
				return fmt.Errorf("урок %q: %w", lesson.ID, ErrDuplicateQuizID)
			}
			quizIDs[lesson.Quiz.ID] = struct{}{}
			if err := ValidateQuiz(*lesson.Quiz); err != nil {
				return fmt.Errorf("урок %q: %w", lesson.ID, err)
			}
		}
		lessonsBySection[lesson.SectionID] = append(lessonsBySection[lesson.SectionID], lesson)
	}

	for sectionID := range sections {
		lessons := lessonsBySection[sectionID]
		if len(lessons) == 0 {
			return fmt.Errorf("раздел %q: %w", sectionID, ErrSectionHasNoLessons)
		}
		orders := make([]bool, len(lessons))
		for _, lesson := range lessons {
			if lesson.Order < 0 || lesson.Order >= len(orders) || orders[lesson.Order] {
				return fmt.Errorf("раздел %q: %w", sectionID, ErrLessonOrderInvalid)
			}
			orders[lesson.Order] = true
		}
	}
	return nil
}

// ValidateQuiz checks authoring correctness before the version is frozen.
func ValidateQuiz(quiz Quiz) error {
	if quiz.ID == "" {
		return ErrQuizIDRequired
	}
	if len(quiz.Questions) == 0 {
		return ErrQuizQuestionsRequired
	}
	if quiz.PassingScore < 0 || quiz.PassingScore > 100 {
		return ErrPassingScoreInvalid
	}
	if quiz.MaxAttempts != nil && *quiz.MaxAttempts < 1 {
		return ErrMaxAttemptsInvalid
	}

	questionIDs := make(map[ID]struct{}, len(quiz.Questions))
	for _, question := range quiz.Questions {
		if question.ID == "" {
			return ErrQuestionIDRequired
		}
		if _, exists := questionIDs[question.ID]; exists {
			return fmt.Errorf("вопрос %q: %w", question.ID, ErrDuplicateQuestionID)
		}
		questionIDs[question.ID] = struct{}{}
		if strings.TrimSpace(question.Text) == "" {
			return fmt.Errorf("вопрос %q: %w", question.ID, ErrQuestionTextRequired)
		}

		switch question.Type {
		case QuestionOpen:
			if len(question.Options) != 0 {
				return fmt.Errorf("вопрос %q: %w", question.ID, ErrOpenQuestionOptionsForbidden)
			}
		case QuestionSingle, QuestionMultiple:
			if len(question.Options) < 2 {
				return fmt.Errorf("вопрос %q: %w", question.ID, ErrClosedQuestionOptionsRequired)
			}
			correct, err := validateOptions(question)
			if err != nil {
				return fmt.Errorf("вопрос %q: %w", question.ID, err)
			}
			if correct == 0 {
				return fmt.Errorf("вопрос %q: %w", question.ID, ErrClosedQuestionWithoutCorrect)
			}
			if question.Type == QuestionSingle && correct != 1 {
				return fmt.Errorf("вопрос %q: %w", question.ID, ErrSingleQuestionCorrectAnswerCount)
			}
		default:
			return fmt.Errorf("вопрос %q: %w", question.ID, ErrUnknownQuestionType)
		}
	}
	return nil
}

func validateOptions(question Question) (int, error) {
	optionIDs := make(map[ID]struct{}, len(question.Options))
	correct := 0
	for _, option := range question.Options {
		if option.ID == "" {
			return 0, ErrOptionIDRequired
		}
		if _, exists := optionIDs[option.ID]; exists {
			return 0, ErrDuplicateOptionID
		}
		optionIDs[option.ID] = struct{}{}
		if strings.TrimSpace(option.Text) == "" {
			return 0, ErrOptionTextRequired
		}
		if option.Correct {
			correct++
		}
	}
	return correct, nil
}

func validateFile(fileID ID, checker FileAvailability) error {
	if fileID == "" {
		return ErrFileUnavailable
	}
	if checker == nil {
		return ErrFileAvailabilityCheckerRequired
	}
	if !checker(fileID) {
		return ErrFileUnavailable
	}
	return nil
}
