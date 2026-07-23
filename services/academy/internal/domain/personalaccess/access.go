// Package personalaccess owns a partner-issued, email-bound external Academy
// link. Raw access tokens never become part of the aggregate snapshot.
package personalaccess

import (
	"crypto/subtle"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// ID is an opaque Academy or company identifier.
type ID string

// Status is the irreversible lifecycle of a personal access link.
type Status string

const (
	StatusIssued    Status = "issued"
	StatusActivated Status = "activated"
	StatusRevoked   Status = "revoked"
	StatusClosed    Status = "closed"

	MinDeadlineDays = 1
	MaxDeadlineDays = 7
	Day             = 24 * time.Hour
)

var tokenPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,24}$`)

var (
	ErrAccessIDRequired       = errors.New("Для персонального доступа требуется идентификатор")
	ErrCompanyRequired        = errors.New("Для персонального доступа требуется компания")
	ErrCourseRequired         = errors.New("Для персонального доступа требуется курс")
	ErrCourseVersionRequired  = errors.New("Для персонального доступа требуется версия курса")
	ErrPartnerOwnerRequired   = errors.New("Для персонального доступа требуется владелец-партнёр")
	ErrIssuerRequired         = errors.New("Для персонального доступа требуется автор")
	ErrIssuerNotPartnerOwner  = errors.New("Персональный доступ может создать только владелец партнёрского курса")
	ErrInvalidExpectedEmail   = errors.New("Некорректный email персонального доступа")
	ErrDeadlineDaysInvalid    = errors.New("Срок персонального доступа должен быть от одного до семи дней")
	ErrUnknownStatus          = errors.New("Неизвестное состояние персонального доступа")
	ErrTokenHashRequired      = errors.New("Для персонального доступа требуется хеш токена")
	ErrTokenPrefixRequired    = errors.New("Префикс токена персонального доступа задан некорректно")
	ErrIdempotencyKeyRequired = errors.New("Для персонального доступа требуется ключ идемпотентности")
	ErrIssuedAtRequired       = errors.New("Для персонального доступа требуется дата создания")
	ErrUpdatedAtInvalid       = errors.New("Дата изменения персонального доступа задана некорректно")
	ErrRepeatShapeInvalid     = errors.New("Связь повторного персонального доступа задана некорректно")
	ErrActivationStateInvalid = errors.New("Данные активации персонального доступа заданы некорректно")
	ErrRevocationStateInvalid = errors.New("Данные отзыва персонального доступа заданы некорректно")
	ErrClosedStateInvalid     = errors.New("Данные закрытия персонального доступа заданы некорректно")
	ErrAccessEmailMismatch    = errors.New("Этот email не соответствует персональному доступу")
	ErrAccessCompanyMismatch  = errors.New("Персональный доступ принадлежит другой компании")
	ErrLearnerRequired        = errors.New("Для активации требуется внешний ученик")
	ErrEnrollmentRequired     = errors.New("Для активации требуется прохождение")
	ErrActivationTimeInvalid  = errors.New("Время активации персонального доступа задано некорректно")
	ErrAccessAlreadyActivated = errors.New("Персональный доступ уже активирован другим прохождением")
	ErrAccessNotActivated     = errors.New("Персональный доступ ещё не активирован")
	ErrAccessRevoked          = errors.New("Персональный доступ отозван")
	ErrAccessClosed           = errors.New("Персональный доступ закрыт")
	ErrTokenRotationInvalid   = errors.New("Новый токен персонального доступа задан некорректно")
	ErrRepeatNotAvailable     = errors.New("Повторное прохождение для этого доступа недоступно")
	ErrRepeatAccessInvalid    = errors.New("Новый повторный персональный доступ задан некорректно")
)

// Snapshot is persistence-neutral. TokenHash is derived; the full token is
// returned once by the application and never stored here.
type Snapshot struct {
	ID                      ID
	CompanyID               ID
	CourseID                ID
	CourseVersionID         ID
	PartnerOwnerID          ID
	ExternalLearnerID       *ID
	ExpectedEmail           string
	NormalizedExpectedEmail string
	RecipientFirstName      *string
	RecipientLastName       *string
	DeadlineDays            int
	Status                  Status
	TokenHash               []byte
	TokenPrefix             string
	EnrollmentID            *ID
	RootAccessID            ID
	RepeatOfAccessID        *ID
	AttemptNumber           int
	IssuanceIdempotencyKey  string
	IssuedByID              ID
	IssuedAt                time.Time
	ActivatedAt             *time.Time
	TokenRotatedAt          *time.Time
	RevokedAt               *time.Time
	ClosedAt                *time.Time
	UpdatedAt               time.Time
}

// NewParams contains the data for a first personal link.
type NewParams struct {
	ID                     ID
	CompanyID              ID
	CourseID               ID
	CourseVersionID        ID
	PartnerOwnerID         ID
	ExpectedEmail          string
	RecipientFirstName     *string
	RecipientLastName      *string
	DeadlineDays           int
	TokenHash              []byte
	TokenPrefix            string
	IssuanceIdempotencyKey string
	IssuedByID             ID
	IssuedAt               time.Time
}

// Activation binds an issued access to a verified external identity and one
// enrollment. Email/company come from the external session.
type Activation struct {
	CompanyID         ID
	ExternalLearnerID ID
	NormalizedEmail   string
	EnrollmentID      ID
	At                time.Time
}

// RepeatParams creates a separate access/token and, after explicit activation,
// a separate enrollment. The prior enrollment must already be complete.
type RepeatParams struct {
	ID                     ID
	TokenHash              []byte
	TokenPrefix            string
	IssuanceIdempotencyKey string
	IssuedAt               time.Time
	PreviousCompleted      bool
}

// Access is one email-bound token lifecycle.
type Access struct {
	snapshot Snapshot
}

// Validate checks all persistent invariants without retaining the aggregate.
func (s Snapshot) Validate() error {
	_, err := Rehydrate(s)
	return err
}

// NormalizeEmail applies only TrimSpace and lowercase; dots and +suffixes are
// deliberately preserved.
func NormalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// New issues the first link in a repeat lineage.
func New(params NewParams) (*Access, error) {
	issuedAt := params.IssuedAt.UTC()
	return Rehydrate(Snapshot{
		ID: params.ID, CompanyID: params.CompanyID, CourseID: params.CourseID,
		CourseVersionID: params.CourseVersionID, PartnerOwnerID: params.PartnerOwnerID,
		ExpectedEmail:           strings.TrimSpace(params.ExpectedEmail),
		NormalizedExpectedEmail: NormalizeEmail(params.ExpectedEmail),
		RecipientFirstName:      cleanOptional(params.RecipientFirstName),
		RecipientLastName:       cleanOptional(params.RecipientLastName),
		DeadlineDays:            params.DeadlineDays, Status: StatusIssued,
		TokenHash: cloneBytes(params.TokenHash), TokenPrefix: params.TokenPrefix,
		RootAccessID: params.ID, AttemptNumber: 1,
		IssuanceIdempotencyKey: params.IssuanceIdempotencyKey,
		IssuedByID:             params.IssuedByID, IssuedAt: issuedAt, UpdatedAt: issuedAt,
	})
}

// Rehydrate validates and canonicalizes stored state.
func Rehydrate(snapshot Snapshot) (*Access, error) {
	value := cloneSnapshot(snapshot)
	canonicalizeTimes(&value)
	if err := validateSnapshot(value); err != nil {
		return nil, err
	}
	return &Access{snapshot: value}, nil
}

// Snapshot returns an isolated, hash-only representation.
func (a *Access) Snapshot() Snapshot {
	if a == nil {
		return Snapshot{}
	}
	return cloneSnapshot(a.snapshot)
}

// MatchesTokenHash compares a derived candidate hash in constant time.
func (a *Access) MatchesTokenHash(candidateHash []byte) bool {
	return a != nil && constantTimeEqual(a.snapshot.TokenHash, candidateHash)
}

// TokenUsable reports whether the current token may resolve a landing page.
func (a *Access) TokenUsable() bool {
	return a != nil && (a.snapshot.Status == StatusIssued || a.snapshot.Status == StatusActivated)
}

// Activate is idempotent for the same learner/enrollment and never resets a
// deadline itself; Enrollment starts it after the explicit user action.
func (a *Access) Activate(params Activation) error {
	if a == nil {
		return ErrAccessIDRequired
	}
	switch a.snapshot.Status {
	case StatusRevoked:
		return ErrAccessRevoked
	case StatusClosed:
		return ErrAccessClosed
	case StatusActivated:
		if idsEqual(a.snapshot.ExternalLearnerID, params.ExternalLearnerID) && idsEqual(a.snapshot.EnrollmentID, params.EnrollmentID) {
			return nil
		}
		return ErrAccessAlreadyActivated
	case StatusIssued:
	default:
		return ErrUnknownStatus
	}
	if params.CompanyID != a.snapshot.CompanyID {
		return ErrAccessCompanyMismatch
	}
	if NormalizeEmail(params.NormalizedEmail) != a.snapshot.NormalizedExpectedEmail {
		return ErrAccessEmailMismatch
	}
	if params.ExternalLearnerID == "" {
		return ErrLearnerRequired
	}
	if a.snapshot.ExternalLearnerID != nil && *a.snapshot.ExternalLearnerID != params.ExternalLearnerID {
		return ErrAccessAlreadyActivated
	}
	if params.EnrollmentID == "" {
		return ErrEnrollmentRequired
	}
	if err := a.validateTransition(params.At); err != nil {
		return ErrActivationTimeInvalid
	}
	at := params.At.UTC()
	learnerID := params.ExternalLearnerID
	enrollmentID := params.EnrollmentID
	a.snapshot.ExternalLearnerID = &learnerID
	a.snapshot.EnrollmentID = &enrollmentID
	a.snapshot.Status = StatusActivated
	a.snapshot.ActivatedAt = &at
	a.snapshot.UpdatedAt = at
	return nil
}

// RotateToken atomically replaces the token hash. Version, enrollment,
// progress, and deadline policy remain unchanged.
func (a *Access) RotateToken(tokenHash []byte, tokenPrefix string, at time.Time) error {
	if err := a.ensureMutable(); err != nil {
		return err
	}
	if len(tokenHash) != 32 || !tokenPrefixPattern.MatchString(tokenPrefix) ||
		constantTimeEqual(a.snapshot.TokenHash, tokenHash) {
		return ErrTokenRotationInvalid
	}
	if err := a.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	a.snapshot.TokenHash = cloneBytes(tokenHash)
	a.snapshot.TokenPrefix = tokenPrefix
	a.snapshot.TokenRotatedAt = &at
	a.snapshot.UpdatedAt = at
	return nil
}

// SetDeadlineDays updates the period used by an extension/repeat. The
// application extends the same enrollment deadline in the same transaction.
func (a *Access) SetDeadlineDays(days int, at time.Time) error {
	if err := a.ensureMutable(); err != nil {
		return err
	}
	if a.snapshot.Status != StatusActivated {
		return ErrAccessNotActivated
	}
	if days < MinDeadlineDays || days > MaxDeadlineDays {
		return ErrDeadlineDaysInvalid
	}
	if err := a.validateTransition(at); err != nil {
		return err
	}
	a.snapshot.DeadlineDays = days
	a.snapshot.UpdatedAt = at.UTC()
	return nil
}

// Revoke invalidates the token while preserving all report references.
func (a *Access) Revoke(at time.Time) error {
	if a == nil {
		return ErrAccessIDRequired
	}
	if a.snapshot.Status == StatusRevoked {
		return nil
	}
	if a.snapshot.Status == StatusClosed {
		return ErrAccessClosed
	}
	if err := a.validateTransition(at); err != nil {
		return ErrRevocationStateInvalid
	}
	at = at.UTC()
	a.snapshot.Status = StatusRevoked
	a.snapshot.RevokedAt = &at
	a.snapshot.UpdatedAt = at
	return nil
}

// Close is the irreversible link effect of course deletion.
func (a *Access) Close(at time.Time) error {
	if a == nil {
		return ErrAccessIDRequired
	}
	if a.snapshot.Status == StatusClosed {
		return nil
	}
	if err := a.validateTransition(at); err != nil {
		return ErrClosedStateInvalid
	}
	at = at.UTC()
	a.snapshot.Status = StatusClosed
	a.snapshot.ClosedAt = &at
	a.snapshot.UpdatedAt = at
	return nil
}

// PlanRepeat creates a new issued access with a new token, the same immutable
// course version, and explicit root/previous lineage. The current access and
// enrollment remain unchanged.
func (a *Access) PlanRepeat(params RepeatParams) (*Access, error) {
	if a == nil || a.snapshot.Status != StatusActivated || a.snapshot.ExternalLearnerID == nil ||
		a.snapshot.EnrollmentID == nil || !params.PreviousCompleted {
		return nil, ErrRepeatNotAvailable
	}
	if params.ID == "" || params.ID == a.snapshot.ID || len(params.TokenHash) != 32 ||
		!tokenPrefixPattern.MatchString(params.TokenPrefix) ||
		strings.TrimSpace(params.IssuanceIdempotencyKey) == "" || params.IssuedAt.Before(a.snapshot.UpdatedAt) {
		return nil, ErrRepeatAccessInvalid
	}
	issuedAt := params.IssuedAt.UTC()
	learnerID := *a.snapshot.ExternalLearnerID
	previousID := a.snapshot.ID
	return Rehydrate(Snapshot{
		ID: params.ID, CompanyID: a.snapshot.CompanyID, CourseID: a.snapshot.CourseID,
		CourseVersionID: a.snapshot.CourseVersionID, PartnerOwnerID: a.snapshot.PartnerOwnerID,
		ExternalLearnerID: &learnerID,
		ExpectedEmail:     a.snapshot.ExpectedEmail, NormalizedExpectedEmail: a.snapshot.NormalizedExpectedEmail,
		RecipientFirstName: stringPointer(a.snapshot.RecipientFirstName),
		RecipientLastName:  stringPointer(a.snapshot.RecipientLastName),
		DeadlineDays:       a.snapshot.DeadlineDays, Status: StatusIssued,
		TokenHash: cloneBytes(params.TokenHash), TokenPrefix: params.TokenPrefix,
		RootAccessID: a.snapshot.RootAccessID, RepeatOfAccessID: &previousID,
		AttemptNumber:          a.snapshot.AttemptNumber + 1,
		IssuanceIdempotencyKey: params.IssuanceIdempotencyKey,
		IssuedByID:             a.snapshot.PartnerOwnerID, IssuedAt: issuedAt, UpdatedAt: issuedAt,
	})
}

func (a *Access) ensureMutable() error {
	if a == nil {
		return ErrAccessIDRequired
	}
	if a.snapshot.Status == StatusRevoked {
		return ErrAccessRevoked
	}
	if a.snapshot.Status == StatusClosed {
		return ErrAccessClosed
	}
	return nil
}

func (a *Access) validateTransition(at time.Time) error {
	if at.IsZero() || at.Before(a.snapshot.UpdatedAt) {
		return ErrUpdatedAtInvalid
	}
	return nil
}

func validateSnapshot(value Snapshot) error {
	switch {
	case value.ID == "":
		return ErrAccessIDRequired
	case value.CompanyID == "":
		return ErrCompanyRequired
	case value.CourseID == "":
		return ErrCourseRequired
	case value.CourseVersionID == "":
		return ErrCourseVersionRequired
	case value.PartnerOwnerID == "":
		return ErrPartnerOwnerRequired
	case value.IssuedByID == "":
		return ErrIssuerRequired
	case value.IssuedByID != value.PartnerOwnerID:
		return ErrIssuerNotPartnerOwner
	case !validEmail(value.ExpectedEmail) || value.NormalizedExpectedEmail != NormalizeEmail(value.ExpectedEmail):
		return ErrInvalidExpectedEmail
	case value.DeadlineDays < MinDeadlineDays || value.DeadlineDays > MaxDeadlineDays:
		return ErrDeadlineDaysInvalid
	case len(value.TokenHash) != 32:
		return ErrTokenHashRequired
	case !tokenPrefixPattern.MatchString(value.TokenPrefix):
		return ErrTokenPrefixRequired
	case strings.TrimSpace(value.IssuanceIdempotencyKey) == "" || len(value.IssuanceIdempotencyKey) > 512:
		return ErrIdempotencyKeyRequired
	case value.IssuedAt.IsZero():
		return ErrIssuedAtRequired
	case value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.IssuedAt):
		return ErrUpdatedAtInvalid
	}
	if err := validateRepeatShape(value); err != nil {
		return err
	}
	if err := validateTimes(value); err != nil {
		return err
	}

	hasLearner := value.ExternalLearnerID != nil && *value.ExternalLearnerID != ""
	hasEnrollment := value.EnrollmentID != nil && *value.EnrollmentID != ""
	switch value.Status {
	case StatusIssued:
		if hasEnrollment || value.ActivatedAt != nil || value.RevokedAt != nil || value.ClosedAt != nil {
			return ErrActivationStateInvalid
		}
		if value.AttemptNumber == 1 && hasLearner {
			return ErrActivationStateInvalid
		}
	case StatusActivated:
		if !hasLearner || !hasEnrollment || value.ActivatedAt == nil || value.RevokedAt != nil || value.ClosedAt != nil {
			return ErrActivationStateInvalid
		}
	case StatusRevoked:
		if value.RevokedAt == nil || value.ClosedAt != nil || hasEnrollment != (value.ActivatedAt != nil) || hasEnrollment && !hasLearner {
			return ErrRevocationStateInvalid
		}
	case StatusClosed:
		if value.ClosedAt == nil || hasEnrollment != (value.ActivatedAt != nil) || hasEnrollment && !hasLearner {
			return ErrClosedStateInvalid
		}
	default:
		return ErrUnknownStatus
	}
	return nil
}

func validateRepeatShape(value Snapshot) error {
	if value.RootAccessID == "" || value.AttemptNumber < 1 {
		return ErrRepeatShapeInvalid
	}
	if value.AttemptNumber == 1 {
		if value.RootAccessID != value.ID || value.RepeatOfAccessID != nil {
			return ErrRepeatShapeInvalid
		}
		return nil
	}
	if value.RootAccessID == value.ID || value.RepeatOfAccessID == nil || *value.RepeatOfAccessID == "" {
		return ErrRepeatShapeInvalid
	}
	return nil
}

func validateTimes(value Snapshot) error {
	for _, event := range []*time.Time{value.ActivatedAt, value.TokenRotatedAt, value.RevokedAt, value.ClosedAt} {
		if event != nil && (event.Before(value.IssuedAt) || event.After(value.UpdatedAt)) {
			return ErrUpdatedAtInvalid
		}
	}
	if value.RevokedAt != nil && value.ActivatedAt != nil && value.RevokedAt.Before(*value.ActivatedAt) {
		return ErrRevocationStateInvalid
	}
	if value.ClosedAt != nil && value.ActivatedAt != nil && value.ClosedAt.Before(*value.ActivatedAt) {
		return ErrClosedStateInvalid
	}
	return nil
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && address.Address == strings.TrimSpace(value)
}

func constantTimeEqual(left, right []byte) bool {
	return subtle.ConstantTimeCompare(left, right) == 1
}

func idsEqual(value *ID, expected ID) bool {
	return value != nil && *value == expected
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

func canonicalizeTimes(value *Snapshot) {
	value.IssuedAt = value.IssuedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	value.ActivatedAt = utcPointer(value.ActivatedAt)
	value.TokenRotatedAt = utcPointer(value.TokenRotatedAt)
	value.RevokedAt = utcPointer(value.RevokedAt)
	value.ClosedAt = utcPointer(value.ClosedAt)
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	result.TokenHash = cloneBytes(value.TokenHash)
	result.ExternalLearnerID = idPointer(value.ExternalLearnerID)
	result.RecipientFirstName = stringPointer(value.RecipientFirstName)
	result.RecipientLastName = stringPointer(value.RecipientLastName)
	result.EnrollmentID = idPointer(value.EnrollmentID)
	result.RepeatOfAccessID = idPointer(value.RepeatOfAccessID)
	result.ActivatedAt = timePointer(value.ActivatedAt)
	result.TokenRotatedAt = timePointer(value.TokenRotatedAt)
	result.RevokedAt = timePointer(value.RevokedAt)
	result.ClosedAt = timePointer(value.ClosedAt)
	return result
}

func idPointer(value *ID) *ID {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
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

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
