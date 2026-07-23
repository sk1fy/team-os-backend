// Package externallearner owns the external Academy identity. An external
// learner is deliberately not a TeamOS user and is scoped to one company.
package externallearner

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

// ID is an opaque Academy identifier.
type ID string

var (
	ErrLearnerIDRequired        = errors.New("Для внешнего ученика требуется идентификатор")
	ErrCompanyRequired          = errors.New("Для внешнего ученика требуется компания")
	ErrInvalidEmail             = errors.New("Некорректный email")
	ErrCreatedAtRequired        = errors.New("Для внешнего ученика требуется дата создания")
	ErrUpdatedAtRequired        = errors.New("Для внешнего ученика требуется дата изменения")
	ErrInvalidTimeline          = errors.New("Дата изменения внешнего ученика не может быть раньше даты создания")
	ErrVerificationBeforeCreate = errors.New("Email нельзя подтвердить раньше создания внешнего ученика")
	ErrDeletionBeforeCreate     = errors.New("Внешнего ученика нельзя удалить раньше его создания")
	ErrLearnerDeleted           = errors.New("Внешний ученик удалён")
	ErrTransitionTimeInvalid    = errors.New("Время изменения не может быть раньше предыдущего события")
	ErrCompanyMismatch          = errors.New("Внешний ученик принадлежит другой компании")
	ErrEmailMismatch            = errors.New("Email не соответствует внешнему ученику")
)

// Snapshot is persistence-neutral and intentionally has no linked user ID.
type Snapshot struct {
	ID              ID
	CompanyID       ID
	Email           string
	NormalizedEmail string
	FirstName       *string
	LastName        *string
	Phone           *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// NewParams contains user-provided contact data and server-owned identity data.
type NewParams struct {
	ID        ID
	CompanyID ID
	Email     string
	FirstName *string
	LastName  *string
	Phone     *string
	CreatedAt time.Time
}

// ContactCorrection is applied atomically. Changing the normalized email
// invalidates the old verification; casing-only changes do not.
type ContactCorrection struct {
	Email     string
	FirstName *string
	LastName  *string
	Phone     *string
	At        time.Time
}

// Learner is one external identity within one company.
type Learner struct {
	snapshot Snapshot
}

// NormalizeEmail applies only the product-approved normalization. In
// particular, dots and +suffixes are preserved.
func NormalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ValidEmail validates a plain mailbox address without provider-specific
// canonicalization.
func ValidEmail(value string) bool {
	normalized := NormalizeEmail(value)
	address, err := mail.ParseAddress(normalized)
	return err == nil && address.Address == normalized
}

// New creates an unverified profile. Uniqueness of (company, normalized
// email) is enforced transactionally by storage.
func New(params NewParams) (*Learner, error) {
	normalized := NormalizeEmail(params.Email)
	createdAt := params.CreatedAt.UTC()
	return Rehydrate(Snapshot{
		ID:              params.ID,
		CompanyID:       params.CompanyID,
		Email:           strings.TrimSpace(params.Email),
		NormalizedEmail: normalized,
		FirstName:       cleanOptional(params.FirstName),
		LastName:        cleanOptional(params.LastName),
		Phone:           cleanOptional(params.Phone),
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	})
}

// Rehydrate validates and canonicalizes a stored snapshot to UTC.
func Rehydrate(snapshot Snapshot) (*Learner, error) {
	value := cloneSnapshot(snapshot)
	canonicalizeTimes(&value)
	if err := validateSnapshot(value); err != nil {
		return nil, err
	}
	return &Learner{snapshot: value}, nil
}

// Snapshot returns a defensive copy.
func (l *Learner) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{}
	}
	return cloneSnapshot(l.snapshot)
}

// MatchesIdentity prevents a tenant-scoped external session from being used
// for another company or email.
func (l *Learner) MatchesIdentity(companyID ID, email string) error {
	if l == nil || l.snapshot.DeletedAt != nil {
		return ErrLearnerDeleted
	}
	if l.snapshot.CompanyID != companyID {
		return ErrCompanyMismatch
	}
	if l.snapshot.NormalizedEmail != NormalizeEmail(email) {
		return ErrEmailMismatch
	}
	return nil
}

// VerifyEmail marks the current email as verified. Repeated confirmation is
// idempotent and keeps the original verification timestamp.
func (l *Learner) VerifyEmail(at time.Time) error {
	if l == nil || l.snapshot.DeletedAt != nil {
		return ErrLearnerDeleted
	}
	if l.snapshot.EmailVerifiedAt != nil {
		return nil
	}
	if err := l.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	l.snapshot.EmailVerifiedAt = &at
	l.snapshot.UpdatedAt = at
	return nil
}

// CorrectContact updates contact details without ever linking the learner to
// an internal user. It reports whether re-verification is required.
func (l *Learner) CorrectContact(params ContactCorrection) (bool, error) {
	if l == nil || l.snapshot.DeletedAt != nil {
		return false, ErrLearnerDeleted
	}
	if !ValidEmail(params.Email) {
		return false, ErrInvalidEmail
	}
	if err := l.validateTransition(params.At); err != nil {
		return false, err
	}
	normalized := NormalizeEmail(params.Email)
	changed := normalized != l.snapshot.NormalizedEmail
	l.snapshot.Email = strings.TrimSpace(params.Email)
	l.snapshot.NormalizedEmail = normalized
	l.snapshot.FirstName = cleanOptional(params.FirstName)
	l.snapshot.LastName = cleanOptional(params.LastName)
	l.snapshot.Phone = cleanOptional(params.Phone)
	l.snapshot.UpdatedAt = params.At.UTC()
	if changed {
		l.snapshot.EmailVerifiedAt = nil
	}
	return changed, nil
}

// Delete irreversibly hides the profile while preserving historical
// enrollments and reports.
func (l *Learner) Delete(at time.Time) error {
	if l == nil {
		return ErrLearnerIDRequired
	}
	if l.snapshot.DeletedAt != nil {
		return nil
	}
	if err := l.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	l.snapshot.DeletedAt = &at
	l.snapshot.UpdatedAt = at
	return nil
}

func (l *Learner) validateTransition(at time.Time) error {
	if at.IsZero() || at.Before(l.snapshot.UpdatedAt) {
		return ErrTransitionTimeInvalid
	}
	return nil
}

func validateSnapshot(value Snapshot) error {
	switch {
	case value.ID == "":
		return ErrLearnerIDRequired
	case value.CompanyID == "":
		return ErrCompanyRequired
	case !ValidEmail(value.Email):
		return ErrInvalidEmail
	case value.NormalizedEmail != NormalizeEmail(value.Email):
		return ErrInvalidEmail
	case value.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	case value.UpdatedAt.IsZero():
		return ErrUpdatedAtRequired
	case value.UpdatedAt.Before(value.CreatedAt):
		return ErrInvalidTimeline
	case value.EmailVerifiedAt != nil && value.EmailVerifiedAt.Before(value.CreatedAt):
		return ErrVerificationBeforeCreate
	case value.EmailVerifiedAt != nil && value.EmailVerifiedAt.After(value.UpdatedAt):
		return ErrInvalidTimeline
	case value.DeletedAt != nil && value.DeletedAt.Before(value.CreatedAt):
		return ErrDeletionBeforeCreate
	case value.DeletedAt != nil && !value.DeletedAt.Equal(value.UpdatedAt):
		return ErrInvalidTimeline
	default:
		return nil
	}
}

func canonicalizeTimes(value *Snapshot) {
	value.CreatedAt = value.CreatedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	value.EmailVerifiedAt = utcPointer(value.EmailVerifiedAt)
	value.DeletedAt = utcPointer(value.DeletedAt)
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	result.FirstName = stringPointer(value.FirstName)
	result.LastName = stringPointer(value.LastName)
	result.Phone = stringPointer(value.Phone)
	result.EmailVerifiedAt = timePointer(value.EmailVerifiedAt)
	result.DeletedAt = timePointer(value.DeletedAt)
	return result
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func timePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}
