package courseversion

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
)

func TestPublishCourseGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutateRoot func(*course.Course)
		mutate     func(*PublishParams)
		wantErr    error
		wantStatus Status
	}{
		{name: "active course", wantStatus: StatusPublished},
		{
			name:       "archived course can publish",
			mutateRoot: func(value *course.Course) { value.LifecycleStatus = course.CourseArchived },
			wantStatus: StatusPublished,
		},
		{
			name:       "paused course can publish",
			mutateRoot: func(value *course.Course) { value.DistributionStatus = course.DistributionPaused },
			wantStatus: StatusPublished,
		},
		{
			name:       "deleted course",
			mutateRoot: func(value *course.Course) { value.LifecycleStatus = course.CourseDeleted },
			wantErr:    ErrCourseDeleted, wantStatus: StatusDraft,
		},
		{
			name:       "blocked course",
			mutateRoot: func(value *course.Course) { value.DistributionStatus = course.DistributionBlocked },
			wantErr:    ErrCourseBlocked, wantStatus: StatusDraft,
		},
		{
			name:       "course id mismatch",
			mutateRoot: func(value *course.Course) { value.ID = "course-2" },
			wantErr:    ErrCourseMismatch, wantStatus: StatusDraft,
		},
		{
			name:       "company mismatch",
			mutateRoot: func(value *course.Course) { value.CompanyID = "company-2" },
			wantErr:    ErrCourseMismatch, wantStatus: StatusDraft,
		},
		{
			name:       "unknown course state",
			mutateRoot: func(value *course.Course) { value.LifecycleStatus = "unknown" },
			wantErr:    course.ErrUnknownLifecycleStatus, wantStatus: StatusDraft,
		},
		{
			name:    "publisher required",
			mutate:  func(value *PublishParams) { value.ActorID = "" },
			wantErr: ErrPublisherRequired, wantStatus: StatusDraft,
		},
		{
			name:    "publication date required",
			mutate:  func(value *PublishParams) { value.At = time.Time{} },
			wantErr: ErrPublishedAtRequired, wantStatus: StatusDraft,
		},
		{
			name:    "draft owner mismatch",
			mutate:  func(value *PublishParams) { value.ActorID = "actor-2" },
			wantErr: ErrDraftOwnerMismatch, wantStatus: StatusDraft,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			version := mustDraft(t)
			root := validCourseRoot()
			params := PublishParams{ActorID: "actor-1", At: testNow().Add(time.Hour)}
			if test.mutateRoot != nil {
				test.mutateRoot(&root)
			}
			if test.mutate != nil {
				test.mutate(&params)
			}
			before := version.Snapshot()
			err := version.Publish(params, root, validPublicationValidators())
			assertErrorIs(t, err, test.wantErr)
			after := version.Snapshot()
			if after.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q", after.Status, test.wantStatus)
			}
			if test.wantErr != nil && !reflect.DeepEqual(after, before) {
				t.Fatalf("failed publication changed draft\nafter: %#v\nbefore: %#v", after, before)
			}
		})
	}
}

func TestPublishFreezesVersionAndMetadata(t *testing.T) {
	t.Parallel()

	version := mustDraft(t)
	publishedAt := time.Date(2026, time.July, 22, 18, 30, 0, 0, time.FixedZone("MSK", 3*60*60))
	if err := version.Publish(
		PublishParams{ActorID: "actor-1", At: publishedAt},
		validCourseRoot(),
		validPublicationValidators(),
	); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	snapshot := version.Snapshot()
	if snapshot.Status != StatusPublished {
		t.Fatalf("status = %q", snapshot.Status)
	}
	if snapshot.PublishedByID == nil || *snapshot.PublishedByID != "actor-1" {
		t.Fatalf("published by = %v", snapshot.PublishedByID)
	}
	if snapshot.PublishedAt == nil || !snapshot.PublishedAt.Equal(publishedAt.UTC()) || snapshot.PublishedAt.Location() != time.UTC {
		t.Fatalf("published at = %v, want UTC %v", snapshot.PublishedAt, publishedAt.UTC())
	}
	if len(snapshot.ContentHash) != sha256HexLength || strings.Trim(snapshot.ContentHash, "0123456789abcdef") != "" {
		t.Fatalf("content hash = %q, want lowercase SHA-256", snapshot.ContentHash)
	}
	assertErrorIs(t, version.Publish(
		PublishParams{ActorID: "actor-1", At: publishedAt.Add(time.Hour)},
		validCourseRoot(), validPublicationValidators(),
	), ErrPublishedVersionImmutable)
	assertErrorIs(t, version.ReplaceDraft(validDefinition()), ErrPublishedVersionImmutable)
}

func TestValidateDefinitionForPublication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*Definition)
		validators PublicationValidators
		wantErr    error
	}{
		{name: "valid", validators: validPublicationValidators()},
		{
			name: "available cover and lesson file",
			mutate: func(value *Definition) {
				cover := ID("cover-1")
				value.CoverFileID = &cover
				value.Lessons[0].FileIDs = []ID{"file-1"}
			},
			validators: validPublicationValidators(),
		},
		{name: "title", mutate: func(value *Definition) { value.Title = "  " }, validators: validPublicationValidators(), wantErr: ErrCourseTitleRequired},
		{
			name:       "default deadline",
			mutate:     func(value *Definition) { zero := 0; value.DefaultInternalDeadlineDays = &zero },
			validators: validPublicationValidators(), wantErr: ErrDefaultDeadlineInvalid,
		},
		{
			name:       "cover checker missing",
			mutate:     func(value *Definition) { cover := ID("cover-1"); value.CoverFileID = &cover },
			validators: PublicationValidators{RichText: richtext.Validate}, wantErr: ErrFileAvailabilityCheckerRequired,
		},
		{
			name:       "cover unavailable",
			mutate:     func(value *Definition) { cover := ID("cover-1"); value.CoverFileID = &cover },
			validators: PublicationValidators{RichText: richtext.Validate, FileAvailable: func(ID) bool { return false }},
			wantErr:    ErrFileUnavailable,
		},
		{name: "section required", mutate: func(value *Definition) { value.Sections = nil }, validators: validPublicationValidators(), wantErr: ErrSectionRequired},
		{name: "lesson required", mutate: func(value *Definition) { value.Lessons = nil }, validators: validPublicationValidators(), wantErr: ErrLessonRequired},
		{name: "section id", mutate: func(value *Definition) { value.Sections[0].ID = "" }, validators: validPublicationValidators(), wantErr: ErrSectionIDRequired},
		{name: "section title", mutate: func(value *Definition) { value.Sections[0].Title = "" }, validators: validPublicationValidators(), wantErr: ErrSectionTitleRequired},
		{
			name: "duplicate section id",
			mutate: func(value *Definition) {
				value.Sections = append(value.Sections, Section{ID: "section-1", Title: "Дубль", Order: 1})
			},
			validators: validPublicationValidators(), wantErr: ErrDuplicateSectionID,
		},
		{name: "negative section order", mutate: func(value *Definition) { value.Sections[0].Order = -1 }, validators: validPublicationValidators(), wantErr: ErrSectionOrderInvalid},
		{
			name: "section order gap",
			mutate: func(value *Definition) {
				value.Sections = append(value.Sections, Section{ID: "section-2", Title: "Второй", Order: 2})
			},
			validators: validPublicationValidators(), wantErr: ErrSectionOrderInvalid,
		},
		{name: "lesson id", mutate: func(value *Definition) { value.Lessons[0].ID = "" }, validators: validPublicationValidators(), wantErr: ErrLessonIDRequired},
		{name: "lesson title", mutate: func(value *Definition) { value.Lessons[0].Title = "" }, validators: validPublicationValidators(), wantErr: ErrLessonTitleRequired},
		{name: "lesson stable key", mutate: func(value *Definition) { value.Lessons[0].StableKey = "" }, validators: validPublicationValidators(), wantErr: ErrLessonStableKeyRequired},
		{
			name: "duplicate lesson id",
			mutate: func(value *Definition) {
				value.Lessons = append(value.Lessons, secondLesson(0))
				value.Lessons[1].ID = "lesson-1"
			},
			validators: validPublicationValidators(), wantErr: ErrDuplicateLessonID,
		},
		{
			name: "duplicate stable key",
			mutate: func(value *Definition) {
				value.Lessons = append(value.Lessons, secondLesson(1))
				value.Lessons[1].StableKey = "stable-1"
			},
			validators: validPublicationValidators(), wantErr: ErrDuplicateLessonStableKey,
		},
		{name: "missing lesson section", mutate: func(value *Definition) { value.Lessons[0].SectionID = "missing" }, validators: validPublicationValidators(), wantErr: ErrLessonSectionMissing},
		{name: "TipTap validator missing", validators: PublicationValidators{}, wantErr: ErrRichTextValidatorRequired},
		{name: "invalid TipTap", mutate: func(value *Definition) { value.Lessons[0].Content = json.RawMessage(`<p>HTML</p>`) }, validators: validPublicationValidators(), wantErr: ErrInvalidLessonContent},
		{
			name:       "lesson file checker missing",
			mutate:     func(value *Definition) { value.Lessons[0].FileIDs = []ID{"file-1"} },
			validators: PublicationValidators{RichText: richtext.Validate}, wantErr: ErrFileAvailabilityCheckerRequired,
		},
		{
			name:       "lesson file unavailable",
			mutate:     func(value *Definition) { value.Lessons[0].FileIDs = []ID{"file-1"} },
			validators: PublicationValidators{RichText: richtext.Validate, FileAvailable: func(ID) bool { return false }},
			wantErr:    ErrFileUnavailable,
		},
		{
			name: "empty section",
			mutate: func(value *Definition) {
				value.Sections = append(value.Sections, Section{ID: "section-2", Title: "Пустой", Order: 1})
			},
			validators: validPublicationValidators(), wantErr: ErrSectionHasNoLessons,
		},
		{
			name:       "duplicate lesson order",
			mutate:     func(value *Definition) { value.Lessons = append(value.Lessons, secondLesson(0)) },
			validators: validPublicationValidators(), wantErr: ErrLessonOrderInvalid,
		},
		{
			name: "duplicate quiz id",
			mutate: func(value *Definition) {
				value.Lessons = append(value.Lessons, secondLesson(1))
				value.Lessons[1].Quiz.ID = "quiz-1"
			},
			validators: validPublicationValidators(), wantErr: ErrDuplicateQuizID,
		},
		{
			name:       "invalid quiz",
			mutate:     func(value *Definition) { value.Lessons[0].Quiz.Questions[0].Options[0].Correct = false },
			validators: validPublicationValidators(), wantErr: ErrClosedQuestionWithoutCorrect,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			definition := validDefinition()
			if test.mutate != nil {
				test.mutate(&definition)
			}
			assertErrorIs(t, ValidateDefinitionForPublication(definition, test.validators), test.wantErr)
		})
	}
}

func TestValidateQuiz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Quiz)
		wantErr error
	}{
		{name: "single"},
		{name: "multiple with several correct", mutate: func(value *Quiz) {
			value.Questions[0].Type = QuestionMultiple
			value.Questions[0].Options[1].Correct = true
		}},
		{name: "open", mutate: func(value *Quiz) { value.Questions[0].Type = QuestionOpen; value.Questions[0].Options = []Option{} }},
		{name: "quiz id", mutate: func(value *Quiz) { value.ID = "" }, wantErr: ErrQuizIDRequired},
		{name: "questions", mutate: func(value *Quiz) { value.Questions = nil }, wantErr: ErrQuizQuestionsRequired},
		{name: "passing score below zero", mutate: func(value *Quiz) { value.PassingScore = -1 }, wantErr: ErrPassingScoreInvalid},
		{name: "passing score above hundred", mutate: func(value *Quiz) { value.PassingScore = 101 }, wantErr: ErrPassingScoreInvalid},
		{name: "max attempts", mutate: func(value *Quiz) { zero := 0; value.MaxAttempts = &zero }, wantErr: ErrMaxAttemptsInvalid},
		{name: "question id", mutate: func(value *Quiz) { value.Questions[0].ID = "" }, wantErr: ErrQuestionIDRequired},
		{
			name:    "duplicate question id",
			mutate:  func(value *Quiz) { value.Questions = append(value.Questions, value.Questions[0]) },
			wantErr: ErrDuplicateQuestionID,
		},
		{name: "question text", mutate: func(value *Quiz) { value.Questions[0].Text = " " }, wantErr: ErrQuestionTextRequired},
		{name: "unknown question type", mutate: func(value *Quiz) { value.Questions[0].Type = "unknown" }, wantErr: ErrUnknownQuestionType},
		{name: "closed options", mutate: func(value *Quiz) { value.Questions[0].Options = value.Questions[0].Options[:1] }, wantErr: ErrClosedQuestionOptionsRequired},
		{
			name:    "single without correct answer",
			mutate:  func(value *Quiz) { value.Questions[0].Options[0].Correct = false },
			wantErr: ErrClosedQuestionWithoutCorrect,
		},
		{
			name: "multiple without correct answer",
			mutate: func(value *Quiz) {
				value.Questions[0].Type = QuestionMultiple
				value.Questions[0].Options[0].Correct = false
			},
			wantErr: ErrClosedQuestionWithoutCorrect,
		},
		{
			name:    "single has two correct answers",
			mutate:  func(value *Quiz) { value.Questions[0].Options[1].Correct = true },
			wantErr: ErrSingleQuestionCorrectAnswerCount,
		},
		{name: "open has options", mutate: func(value *Quiz) { value.Questions[0].Type = QuestionOpen }, wantErr: ErrOpenQuestionOptionsForbidden},
		{name: "option id", mutate: func(value *Quiz) { value.Questions[0].Options[0].ID = "" }, wantErr: ErrOptionIDRequired},
		{name: "option text", mutate: func(value *Quiz) { value.Questions[0].Options[0].Text = "" }, wantErr: ErrOptionTextRequired},
		{
			name:    "duplicate option id",
			mutate:  func(value *Quiz) { value.Questions[0].Options[1].ID = value.Questions[0].Options[0].ID },
			wantErr: ErrDuplicateOptionID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			quiz := validQuiz("quiz-1")
			if test.mutate != nil {
				test.mutate(&quiz)
			}
			assertErrorIs(t, ValidateQuiz(quiz), test.wantErr)
		})
	}
}

func TestTipTapValidationBoundaryIsUsedForEveryLesson(t *testing.T) {
	t.Parallel()

	definition := validDefinition()
	definition.Lessons = append(definition.Lessons, secondLesson(1))
	calls := 0
	validators := validPublicationValidators()
	validators.RichText = func(raw json.RawMessage) error {
		calls++
		return richtext.Validate(raw)
	}
	if err := ValidateDefinitionForPublication(definition, validators); err != nil {
		t.Fatalf("ValidateDefinitionForPublication() error = %v", err)
	}
	if calls != len(definition.Lessons) {
		t.Fatalf("validator calls = %d, want %d", calls, len(definition.Lessons))
	}
}

func validDefinition() Definition {
	return Definition{
		Title:      "Курс",
		Sequential: true,
		Sections: []Section{
			{ID: "section-1", Title: "Раздел", Order: 0},
		},
		Lessons: []Lesson{
			{
				ID: "lesson-1", SectionID: "section-1", StableKey: "stable-1",
				Title: "Урок", Order: 0, Content: validTipTap(), Quiz: quizPointer(validQuiz("quiz-1")),
			},
		},
	}
}

func validQuiz(id ID) Quiz {
	maxAttempts := 3
	return Quiz{
		ID: id, PassingScore: 80, MaxAttempts: &maxAttempts,
		Questions: []Question{
			{
				ID: "question-1", Type: QuestionSingle, Text: "Выберите ответ",
				Options: []Option{
					{ID: "option-1", Text: "Верно", Correct: true},
					{ID: "option-2", Text: "Неверно", Correct: false},
				},
			},
		},
	}
}

func secondLesson(order int) Lesson {
	return Lesson{
		ID: "lesson-2", SectionID: "section-1", StableKey: "stable-2",
		Title: "Второй урок", Order: order, Content: validTipTap(), Quiz: quizPointer(validQuiz("quiz-2")),
	}
}

func validTipTap() json.RawMessage {
	return json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Текст"}]}]}`)
}

func quizPointer(value Quiz) *Quiz { return &value }

func validCourseRoot() course.Course {
	return course.Course{
		ID: "course-1", CompanyID: "company-1", OwnerType: course.CourseOwnerCompany,
		LifecycleStatus: course.CourseActive, DistributionStatus: course.DistributionActive,
	}
}

func validPublicationValidators() PublicationValidators {
	return PublicationValidators{
		RichText: richtext.Validate,
		FileAvailable: func(fileID ID) bool {
			return fileID != ""
		},
	}
}

func publishValid(t *testing.T, version *Version) {
	t.Helper()
	if err := version.Publish(
		PublishParams{ActorID: "actor-1", At: testNow().Add(time.Hour)},
		validCourseRoot(), validPublicationValidators(),
	); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

const sha256HexLength = 64

func TestPublicationErrorsAreRussian(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		ErrPublishedVersionImmutable, ErrCourseBlocked, ErrCourseDeleted,
		ErrClosedQuestionWithoutCorrect, ErrInvalidLessonContent,
	} {
		if errors.Is(err, nil) || !containsCyrillic(err.Error()) {
			t.Errorf("error %q must be a Russian user-facing message", err)
		}
	}
}

func containsCyrillic(value string) bool {
	for _, symbol := range value {
		if symbol >= 'А' && symbol <= 'я' {
			return true
		}
	}
	return false
}
