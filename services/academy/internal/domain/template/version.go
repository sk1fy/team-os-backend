package template

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

// VersionStatus is the authoring state of one template snapshot.
type VersionStatus string

const (
	VersionDraft     VersionStatus = "draft"
	VersionPublished VersionStatus = "published"
)

var (
	ErrVersionIDRequired         = errors.New("Для версии шаблона требуется идентификатор")
	ErrTemplateVersionRequired   = errors.New("Для версии шаблона требуется шаблон")
	ErrVersionNumberInvalid      = errors.New("Номер версии шаблона должен быть больше нуля")
	ErrUnknownVersionStatus      = errors.New("Неизвестное состояние версии шаблона")
	ErrPublishedMetadataRequired = errors.New("Для опубликованной версии шаблона требуются данные публикации")
	ErrDraftPublishedMetadata    = errors.New("У черновика шаблона не может быть данных публикации")
	ErrPublishedVersionImmutable = errors.New("Опубликованную версию шаблона нельзя редактировать")
	ErrOnlyDraftCanBeEdited      = errors.New("Редактировать можно только черновик шаблона")
	ErrOnlyDraftCanBePublished   = errors.New("Опубликовать можно только черновик шаблона")
	ErrPublisherRequired         = errors.New("Для публикации шаблона требуется пользователь")
	ErrPublishedAtRequired       = errors.New("Для публикации шаблона требуется дата")
	ErrSystemVersionRequiresRoot = errors.New("Системную версию можно создать только для системного шаблона")
	ErrCompanyDraftRequiresRoot  = errors.New("Черновик можно создать только для шаблона компании")
)

// Definition deliberately reuses the full versioned course authoring shape.
// It remains owned by this separate aggregate and is always cloned at its
// boundaries.
type Definition = courseversion.Definition

// PublicationValidators are pure validators for TipTap and file availability.
type PublicationValidators = courseversion.PublicationValidators

// VersionSnapshot is a complete immutable observation of one template
// version.
type VersionSnapshot struct {
	ID            ID
	CompanyID     ID
	TemplateID    ID
	Number        int
	Status        VersionStatus
	Definition    Definition
	CreatedByID   ID
	CreatedAt     time.Time
	PublishedByID *ID
	PublishedAt   *time.Time
	ContentHash   string
}

// NewDraftParams contains externally allocated identity and version number.
type NewDraftParams struct {
	ID          ID
	Number      int
	Definition  Definition
	CreatedByID ID
	CreatedAt   time.Time
}

// PublishParams identifies the publication audit principal and moment.
type PublishParams struct {
	ActorID ID
	At      time.Time
}

// Version owns mutable draft content and freezes it on publish.
type Version struct {
	snapshot VersionSnapshot
}

// NewDraft creates a company-template draft. The application must allocate
// the number while holding the template lock via PlanNextDraft.
func NewDraft(root Snapshot, params NewDraftParams) (*Version, error) {
	if err := root.Validate(); err != nil {
		return nil, err
	}
	if root.Type != TypeCompany {
		return nil, ErrCompanyDraftRequiresRoot
	}
	if root.LifecycleStatus != LifecycleActive {
		return nil, ErrTemplateArchived
	}
	return RehydrateVersion(VersionSnapshot{
		ID: params.ID, CompanyID: root.CompanyID, TemplateID: root.ID,
		Number: params.Number, Status: VersionDraft,
		Definition: params.Definition, CreatedByID: params.CreatedByID,
		CreatedAt: params.CreatedAt.UTC(),
	})
}

// NewSystemPublished creates a directly frozen seed version. It is the only
// authoring path for immutable system-template copies.
func NewSystemPublished(
	root Snapshot,
	params NewDraftParams,
	publish PublishParams,
	validators PublicationValidators,
) (*Version, error) {
	if err := root.Validate(); err != nil {
		return nil, err
	}
	if root.Type != TypeSystem {
		return nil, ErrSystemVersionRequiresRoot
	}
	if publish.ActorID == "" {
		return nil, ErrPublisherRequired
	}
	if publish.At.IsZero() {
		return nil, ErrPublishedAtRequired
	}
	if err := courseversion.ValidateDefinitionForPublication(params.Definition, validators); err != nil {
		return nil, err
	}
	publisher := publish.ActorID
	publishedAt := publish.At.UTC()
	return RehydrateVersion(VersionSnapshot{
		ID: params.ID, CompanyID: root.CompanyID, TemplateID: root.ID,
		Number: params.Number, Status: VersionPublished,
		Definition: params.Definition, CreatedByID: params.CreatedByID,
		CreatedAt: params.CreatedAt.UTC(), PublishedByID: &publisher,
		PublishedAt: &publishedAt, ContentHash: definitionHash(params.Definition),
	})
}

// RehydrateVersion restores a version from persistence.
func RehydrateVersion(snapshot VersionSnapshot) (*Version, error) {
	if err := validateVersionSnapshot(snapshot); err != nil {
		return nil, err
	}
	return &Version{snapshot: cloneVersionSnapshot(snapshot)}, nil
}

// Snapshot returns a deep defensive copy.
func (v *Version) Snapshot() VersionSnapshot {
	if v == nil {
		return VersionSnapshot{}
	}
	return cloneVersionSnapshot(v.snapshot)
}

// ReplaceDraft replaces the complete mutable snapshot. Published content and
// all system-template versions reject edits.
func (v *Version) ReplaceDraft(root Snapshot, definition Definition) error {
	if v == nil {
		return ErrVersionIDRequired
	}
	if root.Type == TypeSystem {
		return ErrSystemTemplateImmutable
	}
	if root.LifecycleStatus == LifecycleArchived {
		return ErrTemplateArchived
	}
	if err := validateVersionScope(root, v.snapshot); err != nil {
		return err
	}
	if v.snapshot.Status == VersionPublished {
		return ErrPublishedVersionImmutable
	}
	if v.snapshot.Status != VersionDraft {
		return ErrOnlyDraftCanBeEdited
	}
	v.snapshot.Definition = cloneDefinition(definition)
	return nil
}

// Publish validates and freezes a company-template draft. Authorization is an
// object-level concern in domain/authorization.
func (v *Version) Publish(root Snapshot, params PublishParams, validators PublicationValidators) error {
	if v == nil {
		return ErrVersionIDRequired
	}
	if root.Type == TypeSystem {
		return ErrSystemTemplateImmutable
	}
	if root.LifecycleStatus == LifecycleArchived {
		return ErrTemplateArchived
	}
	if err := validateVersionScope(root, v.snapshot); err != nil {
		return err
	}
	if v.snapshot.Status == VersionPublished {
		return ErrPublishedVersionImmutable
	}
	if v.snapshot.Status != VersionDraft {
		return ErrOnlyDraftCanBePublished
	}
	if params.ActorID == "" {
		return ErrPublisherRequired
	}
	if params.At.IsZero() {
		return ErrPublishedAtRequired
	}
	if err := courseversion.ValidateDefinitionForPublication(v.snapshot.Definition, validators); err != nil {
		return err
	}
	publisher := params.ActorID
	publishedAt := params.At.UTC()
	v.snapshot.Status = VersionPublished
	v.snapshot.PublishedByID = &publisher
	v.snapshot.PublishedAt = &publishedAt
	v.snapshot.ContentHash = definitionHash(v.snapshot.Definition)
	return nil
}

func validateVersionSnapshot(snapshot VersionSnapshot) error {
	switch {
	case snapshot.ID == "":
		return ErrVersionIDRequired
	case snapshot.CompanyID == "":
		return ErrCompanyIDRequired
	case snapshot.TemplateID == "":
		return ErrTemplateVersionRequired
	case snapshot.Number < 1:
		return ErrVersionNumberInvalid
	case snapshot.CreatedByID == "":
		return ErrCreatorRequired
	case snapshot.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	}
	switch snapshot.Status {
	case VersionDraft:
		if snapshot.PublishedByID != nil || snapshot.PublishedAt != nil || snapshot.ContentHash != "" {
			return ErrDraftPublishedMetadata
		}
	case VersionPublished:
		if snapshot.PublishedByID == nil || *snapshot.PublishedByID == "" ||
			snapshot.PublishedAt == nil || snapshot.PublishedAt.IsZero() || snapshot.ContentHash == "" {
			return ErrPublishedMetadataRequired
		}
	default:
		return ErrUnknownVersionStatus
	}
	return nil
}

func definitionHash(definition Definition) string {
	encoded, err := json.Marshal(definition)
	if err != nil {
		encoded = []byte("null")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func cloneVersionSnapshot(value VersionSnapshot) VersionSnapshot {
	result := value
	result.Definition = cloneDefinition(value.Definition)
	result.PublishedByID = cloneID(value.PublishedByID)
	if value.PublishedAt != nil {
		publishedAt := *value.PublishedAt
		result.PublishedAt = &publishedAt
	}
	return result
}

func cloneDefinition(value Definition) Definition {
	result := value
	if value.Description != nil {
		description := *value.Description
		result.Description = &description
	}
	if value.CoverFileID != nil {
		coverFileID := *value.CoverFileID
		result.CoverFileID = &coverFileID
	}
	if value.CoverURL != nil {
		coverURL := *value.CoverURL
		result.CoverURL = &coverURL
	}
	if value.DefaultInternalDeadlineDays != nil {
		deadline := *value.DefaultInternalDeadlineDays
		result.DefaultInternalDeadlineDays = &deadline
	}
	result.Sections = append([]courseversion.Section(nil), value.Sections...)
	result.Lessons = make([]courseversion.Lesson, len(value.Lessons))
	for index, lesson := range value.Lessons {
		result.Lessons[index] = lesson
		result.Lessons[index].Content = append(json.RawMessage(nil), lesson.Content...)
		result.Lessons[index].SourceArticleID = cloneCourseVersionID(lesson.SourceArticleID)
		result.Lessons[index].SourceArticleVersion = cloneInt(lesson.SourceArticleVersion)
		result.Lessons[index].SourceTemplateID = cloneCourseVersionID(lesson.SourceTemplateID)
		result.Lessons[index].SourceTemplateVersionID = cloneCourseVersionID(lesson.SourceTemplateVersionID)
		result.Lessons[index].EstimatedMinutes = cloneInt(lesson.EstimatedMinutes)
		result.Lessons[index].FileIDs = append([]courseversion.ID(nil), lesson.FileIDs...)
		if lesson.Quiz != nil {
			quiz := *lesson.Quiz
			quiz.MaxAttempts = cloneInt(lesson.Quiz.MaxAttempts)
			quiz.Questions = make([]courseversion.Question, len(lesson.Quiz.Questions))
			for questionIndex, question := range lesson.Quiz.Questions {
				quiz.Questions[questionIndex] = question
				quiz.Questions[questionIndex].Options = append([]courseversion.Option(nil), question.Options...)
			}
			result.Lessons[index].Quiz = &quiz
		}
	}
	return result
}

func cloneCourseVersionID(value *courseversion.ID) *courseversion.ID {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
