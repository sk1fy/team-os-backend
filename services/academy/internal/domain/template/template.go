// Package template contains Academy course-template aggregates. Templates are
// authoring snapshots and can never be assigned, enrolled in, or completed.
package template

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// ID is an opaque identifier of a template, version, company, or user.
type ID string

// Type distinguishes immutable system copies from company-owned templates.
type Type string

const (
	TypeSystem  Type = "system"
	TypeCompany Type = "company"
)

// LifecycleStatus controls whether new courses may be instantiated.
type LifecycleStatus string

const (
	LifecycleActive   LifecycleStatus = "active"
	LifecycleArchived LifecycleStatus = "archived"
)

// systemTemplateKeys is the stable, versioned initial system catalogue.
var systemTemplateKeys = []string{
	"employee-onboarding",
	"sales-manager-onboarding",
	"manager-onboarding",
	"company-and-product-intro",
	"information-security",
	"customer-service-standards",
	"crm-basics",
	"regulations-knowledge-check",
	"intern-preparation",
	"external-partner-course",
}

var (
	ErrTemplateIDRequired        = errors.New("Для шаблона требуется идентификатор")
	ErrCompanyIDRequired         = errors.New("Для шаблона требуется компания")
	ErrCreatorRequired           = errors.New("Для шаблона требуется автор")
	ErrCreatedAtRequired         = errors.New("Для шаблона требуется дата создания")
	ErrUnknownTemplateType       = errors.New("Неизвестный тип шаблона курса")
	ErrUnknownLifecycle          = errors.New("Неизвестное состояние шаблона курса")
	ErrSystemTemplateKeyRequired = errors.New("Для системного шаблона требуется стабильный ключ")
	ErrUnknownSystemTemplateKey  = errors.New("Неизвестный ключ системного шаблона")
	ErrSystemPublishedRequired   = errors.New("Для системного шаблона требуется опубликованная версия")
	ErrCompanyTemplateHasKey     = errors.New("У шаблона компании не может быть системного ключа")
	ErrSystemTemplateHasDraft    = errors.New("У системного шаблона не может быть черновика")
	ErrSystemTemplateArchived    = errors.New("Системный шаблон не может быть архивирован")
	ErrVersionPointersEqual      = errors.New("Черновик и опубликованная версия шаблона должны различаться")
	ErrSystemTemplateImmutable   = errors.New("Системный шаблон нельзя изменить или архивировать")
	ErrTemplateAlreadyArchived   = errors.New("Шаблон уже архивирован")
	ErrTemplateArchived          = errors.New("Архивный шаблон нельзя изменить или применить")
	ErrVersionTemplateMismatch   = errors.New("Версия принадлежит другому шаблону или компании")
	ErrDraftPointerMismatch      = errors.New("Черновик не совпадает с текущей версией шаблона")
)

// Snapshot is the persistence-neutral root state.
type Snapshot struct {
	ID                       ID
	CompanyID                ID
	Type                     Type
	SystemTemplateKey        *string
	LifecycleStatus          LifecycleStatus
	CurrentDraftVersionID    *ID
	LatestPublishedVersionID *ID
	CreatedByID              ID
	CreatedAt                time.Time
}

// Template is the aggregate root. Version content is owned by Version.
type Template struct {
	snapshot Snapshot
}

// NewCompany creates a mutable company template root. The initial draft is
// attached separately in the same application transaction.
func NewCompany(id, companyID, creatorID ID, createdAt time.Time) (*Template, error) {
	return RehydrateTemplate(Snapshot{
		ID: id, CompanyID: companyID, Type: TypeCompany,
		LifecycleStatus: LifecycleActive, CreatedByID: creatorID,
		CreatedAt: createdAt.UTC(),
	})
}

// NewSystem creates one immutable, tenant-local copy of a system template.
func NewSystem(id, companyID, creatorID ID, key string, publishedVersionID ID, createdAt time.Time) (*Template, error) {
	return RehydrateTemplate(Snapshot{
		ID: id, CompanyID: companyID, Type: TypeSystem,
		SystemTemplateKey: stringPointer(key), LifecycleStatus: LifecycleActive,
		LatestPublishedVersionID: idPointer(publishedVersionID),
		CreatedByID:              creatorID, CreatedAt: createdAt.UTC(),
	})
}

// RehydrateTemplate restores and validates a root snapshot.
func RehydrateTemplate(snapshot Snapshot) (*Template, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return &Template{snapshot: cloneTemplateSnapshot(snapshot)}, nil
}

// Validate checks all root-level invariants.
func (s Snapshot) Validate() error {
	switch {
	case s.ID == "":
		return ErrTemplateIDRequired
	case s.CompanyID == "":
		return ErrCompanyIDRequired
	case s.CreatedByID == "":
		return ErrCreatorRequired
	case s.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	}
	if s.LifecycleStatus != LifecycleActive && s.LifecycleStatus != LifecycleArchived {
		return ErrUnknownLifecycle
	}
	if s.CurrentDraftVersionID != nil && s.LatestPublishedVersionID != nil &&
		*s.CurrentDraftVersionID == *s.LatestPublishedVersionID {
		return ErrVersionPointersEqual
	}

	switch s.Type {
	case TypeSystem:
		if s.SystemTemplateKey == nil || strings.TrimSpace(*s.SystemTemplateKey) == "" {
			return ErrSystemTemplateKeyRequired
		}
		if !IsSystemTemplateKey(*s.SystemTemplateKey) {
			return ErrUnknownSystemTemplateKey
		}
		if s.CurrentDraftVersionID != nil {
			return ErrSystemTemplateHasDraft
		}
		if s.LatestPublishedVersionID == nil || *s.LatestPublishedVersionID == "" {
			return ErrSystemPublishedRequired
		}
		if s.LifecycleStatus == LifecycleArchived {
			return ErrSystemTemplateArchived
		}
	case TypeCompany:
		if s.SystemTemplateKey != nil {
			return ErrCompanyTemplateHasKey
		}
	default:
		return ErrUnknownTemplateType
	}
	return nil
}

// Snapshot returns a defensive root copy.
func (t *Template) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return cloneTemplateSnapshot(t.snapshot)
}

// AttachDraft records the one mutable draft after the application has locked
// the template and allocated its version number.
func (t *Template) AttachDraft(version VersionSnapshot) error {
	if err := t.assertCompanyEditable(); err != nil {
		return err
	}
	if err := validateVersionScope(t.snapshot, version); err != nil {
		return err
	}
	if version.Status != VersionDraft {
		return ErrOnlyDraftCanBeEdited
	}
	if t.snapshot.CurrentDraftVersionID != nil && *t.snapshot.CurrentDraftVersionID != version.ID {
		return ErrDraftAlreadyExists
	}
	id := version.ID
	t.snapshot.CurrentDraftVersionID = &id
	return nil
}

// RecordPublication advances the published pointer and clears the matching
// draft pointer. The version must already have been frozen successfully.
func (t *Template) RecordPublication(version VersionSnapshot) error {
	if err := t.assertCompanyEditable(); err != nil {
		return err
	}
	if err := validateVersionScope(t.snapshot, version); err != nil {
		return err
	}
	if version.Status != VersionPublished {
		return ErrOnlyDraftCanBePublished
	}
	if t.snapshot.CurrentDraftVersionID == nil || *t.snapshot.CurrentDraftVersionID != version.ID {
		return ErrDraftPointerMismatch
	}
	id := version.ID
	t.snapshot.LatestPublishedVersionID = &id
	t.snapshot.CurrentDraftVersionID = nil
	return nil
}

// RecordSystemPublication advances a system seed pointer without opening an
// editable draft. Only the trusted seed/backfill flow should call it.
func (t *Template) RecordSystemPublication(version VersionSnapshot) error {
	if t == nil {
		return ErrTemplateIDRequired
	}
	if t.snapshot.Type != TypeSystem {
		return ErrSystemVersionRequiresRoot
	}
	if t.snapshot.LifecycleStatus != LifecycleActive {
		return ErrTemplateArchived
	}
	if err := validateVersionScope(t.snapshot, version); err != nil {
		return err
	}
	if version.Status != VersionPublished {
		return ErrOnlyDraftCanBePublished
	}
	id := version.ID
	t.snapshot.LatestPublishedVersionID = &id
	return nil
}

// Archive prevents future instantiation without affecting courses already
// created from any published version.
func (t *Template) Archive() error {
	if t == nil {
		return ErrTemplateIDRequired
	}
	if t.snapshot.Type == TypeSystem {
		return ErrSystemTemplateImmutable
	}
	if t.snapshot.LifecycleStatus == LifecycleArchived {
		return ErrTemplateAlreadyArchived
	}
	t.snapshot.LifecycleStatus = LifecycleArchived
	return nil
}

func (t *Template) assertCompanyEditable() error {
	if t == nil {
		return ErrTemplateIDRequired
	}
	if t.snapshot.Type == TypeSystem {
		return ErrSystemTemplateImmutable
	}
	if t.snapshot.LifecycleStatus == LifecycleArchived {
		return ErrTemplateArchived
	}
	return nil
}

func validateVersionScope(root Snapshot, version VersionSnapshot) error {
	if version.TemplateID != root.ID || version.CompanyID != root.CompanyID {
		return ErrVersionTemplateMismatch
	}
	return nil
}

// IsSystemTemplateKey reports whether key belongs to the supported seed set.
func IsSystemTemplateKey(key string) bool {
	return slices.Contains(systemTemplateKeys, key)
}

// SystemTemplateKeys returns a defensive copy of the supported seed keys.
func SystemTemplateKeys() []string { return append([]string(nil), systemTemplateKeys...) }

func cloneTemplateSnapshot(value Snapshot) Snapshot {
	result := value
	result.SystemTemplateKey = cloneString(value.SystemTemplateKey)
	result.CurrentDraftVersionID = cloneID(value.CurrentDraftVersionID)
	result.LatestPublishedVersionID = cloneID(value.LatestPublishedVersionID)
	return result
}

func idPointer(value ID) *ID {
	if value == "" {
		return nil
	}
	return &value
}

func stringPointer(value string) *string { return &value }

func cloneID(value *ID) *ID {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
