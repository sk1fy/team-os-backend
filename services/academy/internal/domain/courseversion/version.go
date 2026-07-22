// Package courseversion contains the immutable versioning and publication
// rules for Academy course content. It has no persistence or transport
// dependencies.
package courseversion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ID is an opaque identifier of an Academy or company entity.
type ID string

// Status is the lifecycle of one immutable course version.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusRetired   Status = "retired"
)

// QuestionType identifies how a quiz question is assessed.
type QuestionType string

const (
	QuestionSingle   QuestionType = "single"
	QuestionMultiple QuestionType = "multiple"
	QuestionOpen     QuestionType = "open"
)

var (
	ErrVersionIDRequired         = errors.New("Для версии требуется идентификатор")
	ErrCompanyRequired           = errors.New("Для версии требуется компания")
	ErrCourseRequired            = errors.New("Для версии требуется курс")
	ErrCreatorRequired           = errors.New("Для версии требуется автор")
	ErrCreatedAtRequired         = errors.New("Для версии требуется дата создания")
	ErrVersionNumberInvalid      = errors.New("Номер версии должен быть больше нуля")
	ErrUnknownStatus             = errors.New("Неизвестное состояние версии курса")
	ErrPublishedMetadataRequired = errors.New("Для опубликованной версии требуются данные публикации")
	ErrDraftPublishedMetadata    = errors.New("У черновика не может быть данных публикации")
	ErrPublishedVersionImmutable = errors.New("Опубликованную версию нельзя редактировать")
	ErrRetiredVersionImmutable   = errors.New("Архивную версию нельзя редактировать")
	ErrOnlyDraftCanBePublished   = errors.New("Опубликовать можно только черновик версии")
	ErrOnlyPublishedCanBeRetired = errors.New("Архивировать можно только опубликованную версию")
	ErrPublisherRequired         = errors.New("Для публикации требуется пользователь")
	ErrPublishedAtRequired       = errors.New("Для публикации требуется дата")
	ErrDraftOwnerMismatch        = errors.New("Черновик принадлежит другому пользователю")
)

// Definition is the complete editable snapshot frozen by publication.
// Callers replace it atomically instead of mutating published nested content.
type Definition struct {
	Title                       string
	Description                 *string
	CoverFileID                 *ID
	CoverURL                    *string
	Sequential                  bool
	DefaultInternalDeadlineDays *int
	Sections                    []Section
	Lessons                     []Lesson
}

// Section is a section inside one course version.
type Section struct {
	ID        ID
	StableKey string
	Title     string
	Order     int
}

// Lesson is a lesson inside one course version. Content must be TipTap JSON;
// FileIDs contains all file references that have to be available at publish.
type Lesson struct {
	ID                      ID
	SectionID               ID
	StableKey               string
	Title                   string
	Order                   int
	Content                 json.RawMessage
	SourceType              string
	SourceArticleID         *ID
	SourceArticleVersion    *int
	SourceTemplateID        *ID
	SourceTemplateVersionID *ID
	EstimatedMinutes        *int
	FileIDs                 []ID
	Quiz                    *Quiz
}

// Quiz is the authoring representation. Correct answers are intentionally
// kept here and are stripped by LearnerView.
type Quiz struct {
	ID           ID
	Questions    []Question
	PassingScore int
	MaxAttempts  *int
}

// Question is an authoring question.
type Question struct {
	ID      ID
	Type    QuestionType
	Text    string
	Options []Option
}

// Option is an authoring answer option.
type Option struct {
	ID      ID
	Text    string
	Correct bool
}

// Snapshot is the persistence-neutral representation used to rehydrate a
// version. Definition and byte slices are defensively copied at the boundary.
type Snapshot struct {
	ID            ID
	CompanyID     ID
	CourseID      ID
	Number        int
	Status        Status
	Definition    Definition
	CreatedByID   ID
	CreatedAt     time.Time
	PublishedByID *ID
	PublishedAt   *time.Time
	ContentHash   string
}

// NewDraftParams contains data for a newly numbered draft.
type NewDraftParams struct {
	ID          ID
	CompanyID   ID
	CourseID    ID
	Number      int
	Definition  Definition
	CreatedByID ID
	CreatedAt   time.Time
}

// PublishParams identifies the actor and moment of publication.
type PublishParams struct {
	ActorID ID
	At      time.Time
}

// Version owns a course-version state. Its fields are deliberately private so
// published content can only be observed through a defensive Snapshot.
type Version struct {
	snapshot Snapshot
}

// NewDraft creates a mutable draft with an already allocated version number.
// Use PlanNextDraft while holding the course lock to allocate that number.
func NewDraft(params NewDraftParams) (*Version, error) {
	return Rehydrate(Snapshot{
		ID:          params.ID,
		CompanyID:   params.CompanyID,
		CourseID:    params.CourseID,
		Number:      params.Number,
		Status:      StatusDraft,
		Definition:  params.Definition,
		CreatedByID: params.CreatedByID,
		CreatedAt:   params.CreatedAt,
	})
}

// Rehydrate restores a version from a persistence-neutral snapshot.
func Rehydrate(snapshot Snapshot) (*Version, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	return &Version{snapshot: cloneSnapshot(snapshot)}, nil
}

// Snapshot returns a deep copy, preventing mutation of the aggregate through
// nested slices, pointers, or JSON byte buffers.
func (v *Version) Snapshot() Snapshot {
	if v == nil {
		return Snapshot{}
	}
	return cloneSnapshot(v.snapshot)
}

// ReplaceDraft replaces the complete editable definition. Published and
// retired versions reject the operation without changing their state.
func (v *Version) ReplaceDraft(definition Definition) error {
	if v == nil {
		return ErrVersionIDRequired
	}
	switch v.snapshot.Status {
	case StatusDraft:
		v.snapshot.Definition = cloneDefinition(definition)
		return nil
	case StatusPublished:
		return ErrPublishedVersionImmutable
	case StatusRetired:
		return ErrRetiredVersionImmutable
	default:
		return ErrUnknownStatus
	}
}

// Retire removes a published version from future selection while preserving
// its immutable content and historical references.
func (v *Version) Retire() error {
	if v == nil {
		return ErrVersionIDRequired
	}
	switch v.snapshot.Status {
	case StatusPublished:
		v.snapshot.Status = StatusRetired
		return nil
	case StatusDraft:
		return ErrOnlyPublishedCanBeRetired
	case StatusRetired:
		return ErrRetiredVersionImmutable
	default:
		return ErrUnknownStatus
	}
}

func validateSnapshot(snapshot Snapshot) error {
	switch {
	case snapshot.ID == "":
		return ErrVersionIDRequired
	case snapshot.CompanyID == "":
		return ErrCompanyRequired
	case snapshot.CourseID == "":
		return ErrCourseRequired
	case snapshot.CreatedByID == "":
		return ErrCreatorRequired
	case snapshot.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	case snapshot.Number < 1:
		return ErrVersionNumberInvalid
	}

	switch snapshot.Status {
	case StatusDraft:
		if snapshot.PublishedByID != nil || snapshot.PublishedAt != nil || snapshot.ContentHash != "" {
			return ErrDraftPublishedMetadata
		}
	case StatusPublished, StatusRetired:
		if snapshot.PublishedByID == nil || *snapshot.PublishedByID == "" ||
			snapshot.PublishedAt == nil || snapshot.PublishedAt.IsZero() || snapshot.ContentHash == "" {
			return ErrPublishedMetadataRequired
		}
	default:
		return ErrUnknownStatus
	}
	return nil
}

func definitionHash(definition Definition) string {
	encoded, err := json.Marshal(definition)
	if err != nil {
		// Definition contains no values that json.Marshal can reject. Keeping the
		// fallback deterministic makes the domain function total.
		encoded = []byte("null")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	result := snapshot
	result.Definition = cloneDefinition(snapshot.Definition)
	result.PublishedByID = cloneID(snapshot.PublishedByID)
	result.PublishedAt = cloneTime(snapshot.PublishedAt)
	return result
}

func cloneDefinition(definition Definition) Definition {
	result := definition
	result.Description = cloneString(definition.Description)
	result.CoverFileID = cloneID(definition.CoverFileID)
	result.CoverURL = cloneString(definition.CoverURL)
	result.DefaultInternalDeadlineDays = cloneInt(definition.DefaultInternalDeadlineDays)
	result.Sections = append([]Section(nil), definition.Sections...)
	result.Lessons = make([]Lesson, len(definition.Lessons))
	for index, lesson := range definition.Lessons {
		result.Lessons[index] = lesson
		result.Lessons[index].Content = append(json.RawMessage(nil), lesson.Content...)
		result.Lessons[index].SourceArticleID = cloneID(lesson.SourceArticleID)
		result.Lessons[index].SourceArticleVersion = cloneInt(lesson.SourceArticleVersion)
		result.Lessons[index].SourceTemplateID = cloneID(lesson.SourceTemplateID)
		result.Lessons[index].SourceTemplateVersionID = cloneID(lesson.SourceTemplateVersionID)
		result.Lessons[index].EstimatedMinutes = cloneInt(lesson.EstimatedMinutes)
		result.Lessons[index].FileIDs = append([]ID(nil), lesson.FileIDs...)
		if lesson.Quiz != nil {
			quiz := cloneQuiz(*lesson.Quiz)
			result.Lessons[index].Quiz = &quiz
		}
	}
	return result
}

func cloneQuiz(quiz Quiz) Quiz {
	result := quiz
	result.MaxAttempts = cloneInt(quiz.MaxAttempts)
	result.Questions = make([]Question, len(quiz.Questions))
	for index, question := range quiz.Questions {
		result.Questions[index] = question
		result.Questions[index].Options = append([]Option(nil), question.Options...)
	}
	return result
}

func cloneID(value *ID) *ID {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
