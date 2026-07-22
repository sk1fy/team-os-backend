// Package externalverification contains the short-lived email verification
// challenge. Raw codes never become part of the aggregate snapshot.
package externalverification

import (
	"crypto/subtle"
	"errors"
	"net/mail"
	"strings"
	"time"
)

// ID is an opaque Academy identifier.
type ID string

// Purpose identifies why an external email is being confirmed.
type Purpose string

const (
	PurposePersonalAccess   Purpose = "personal_access"
	PurposeCampaignAccess   Purpose = "campaign_access"
	PurposeSessionBootstrap Purpose = "session_bootstrap"

	ChallengeTTL       = 10 * time.Minute
	DefaultMaxAttempts = 5
)

// InvalidationReason explains why an unconsumed challenge can no longer be
// used.
type InvalidationReason string

const (
	InvalidationExpired           InvalidationReason = "expired"
	InvalidationReplaced          InvalidationReason = "replaced"
	InvalidationRateLimited       InvalidationReason = "rate_limited"
	InvalidationAttemptsExhausted InvalidationReason = "attempts_exhausted"
)

var (
	ErrChallengeIDRequired   = errors.New("Для подтверждения email требуется идентификатор")
	ErrCompanyRequired       = errors.New("Для подтверждения email требуется компания")
	ErrEmailRequired         = errors.New("Для подтверждения email требуется адрес")
	ErrUnknownPurpose        = errors.New("Неизвестная цель подтверждения email")
	ErrSourceShapeInvalid    = errors.New("Источник подтверждения email задан некорректно")
	ErrCodeHashRequired      = errors.New("Для подтверждения email требуется хеш кода")
	ErrRequestIPHashInvalid  = errors.New("Хеш IP-адреса подтверждения задан некорректно")
	ErrCreatedAtRequired     = errors.New("Для подтверждения email требуется дата создания")
	ErrExpiryInvalid         = errors.New("Срок подтверждения email задан некорректно")
	ErrMaxAttemptsInvalid    = errors.New("Число попыток подтверждения должно быть от одной до пяти")
	ErrAttemptsInvalid       = errors.New("Число использованных попыток подтверждения задано некорректно")
	ErrConsumedAtInvalid     = errors.New("Дата использования подтверждения задана некорректно")
	ErrInvalidationInvalid   = errors.New("Данные отмены подтверждения email заданы некорректно")
	ErrChallengeConsumed     = errors.New("Код подтверждения уже использован")
	ErrChallengeInvalidated  = errors.New("Код подтверждения больше недействителен")
	ErrChallengeExpired      = errors.New("Срок действия кода подтверждения истёк")
	ErrChallengeAttemptsUsed = errors.New("Исчерпано число попыток подтверждения")
	ErrCodeMismatch          = errors.New("Неверный код подтверждения")
	ErrConfirmationTime      = errors.New("Время подтверждения задано некорректно")
)

// Snapshot persists only derived code/IP hashes, never the six-digit code or
// raw address of the requester.
type Snapshot struct {
	ID                 ID
	CompanyID          ID
	NormalizedEmail    string
	Purpose            Purpose
	SourceID           *ID
	ClaimedFirstName   *string
	ClaimedLastName    *string
	CodeHash           []byte
	RequestIPHash      []byte
	ExpiresAt          time.Time
	Attempts           int
	MaxAttempts        int
	ConsumedAt         *time.Time
	InvalidatedAt      *time.Time
	InvalidationReason *InvalidationReason
	CreatedAt          time.Time
}

// NewParams creates the fixed V1 challenge policy: ten minutes and five
// attempts. CodeHash and RequestIPHash must already be derived.
type NewParams struct {
	ID               ID
	CompanyID        ID
	NormalizedEmail  string
	Purpose          Purpose
	SourceID         *ID
	ClaimedFirstName *string
	ClaimedLastName  *string
	CodeHash         []byte
	RequestIPHash    []byte
	CreatedAt        time.Time
}

// Challenge is a one-time state machine.
type Challenge struct {
	snapshot Snapshot
}

// New creates a V1 challenge.
func New(params NewParams) (*Challenge, error) {
	createdAt := params.CreatedAt.UTC()
	return Rehydrate(Snapshot{
		ID: params.ID, CompanyID: params.CompanyID,
		NormalizedEmail: strings.ToLower(strings.TrimSpace(params.NormalizedEmail)),
		Purpose:         params.Purpose, SourceID: idPointer(params.SourceID),
		ClaimedFirstName: cleanOptional(params.ClaimedFirstName),
		ClaimedLastName:  cleanOptional(params.ClaimedLastName),
		CodeHash:         cloneBytes(params.CodeHash), RequestIPHash: cloneBytes(params.RequestIPHash),
		ExpiresAt: createdAt.Add(ChallengeTTL), MaxAttempts: DefaultMaxAttempts,
		CreatedAt: createdAt,
	})
}

// Rehydrate validates and canonicalizes stored timestamps.
func Rehydrate(snapshot Snapshot) (*Challenge, error) {
	value := cloneSnapshot(snapshot)
	value.CreatedAt = value.CreatedAt.UTC()
	value.ExpiresAt = value.ExpiresAt.UTC()
	value.ConsumedAt = utcPointer(value.ConsumedAt)
	value.InvalidatedAt = utcPointer(value.InvalidatedAt)
	if err := validateSnapshot(value); err != nil {
		return nil, err
	}
	return &Challenge{snapshot: value}, nil
}

// Snapshot returns a defensive copy containing hashes only.
func (c *Challenge) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return cloneSnapshot(c.snapshot)
}

// ValidSixDigitCode checks the V1 transport-independent code policy.
func ValidSixDigitCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// Confirm compares already-derived hashes in constant time. A wrong code
// consumes one attempt; expiry and repeated use consume none.
func (c *Challenge) Confirm(candidateHash []byte, at time.Time) error {
	if c == nil {
		return ErrChallengeIDRequired
	}
	if c.snapshot.ConsumedAt != nil {
		return ErrChallengeConsumed
	}
	if c.snapshot.InvalidatedAt != nil {
		return ErrChallengeInvalidated
	}
	if at.IsZero() || at.Before(c.snapshot.CreatedAt) {
		return ErrConfirmationTime
	}
	at = at.UTC()
	if !at.Before(c.snapshot.ExpiresAt) {
		return ErrChallengeExpired
	}
	if c.snapshot.Attempts >= c.snapshot.MaxAttempts {
		return ErrChallengeAttemptsUsed
	}
	if !constantTimeEqual(c.snapshot.CodeHash, candidateHash) {
		c.snapshot.Attempts++
		if c.snapshot.Attempts >= c.snapshot.MaxAttempts {
			reason := InvalidationAttemptsExhausted
			c.snapshot.InvalidatedAt = &at
			c.snapshot.InvalidationReason = &reason
			return ErrChallengeAttemptsUsed
		}
		return ErrCodeMismatch
	}
	c.snapshot.ConsumedAt = &at
	return nil
}

// Available reports whether a challenge may still be attempted at server
// time. The expiry boundary itself is unavailable.
func (c *Challenge) Available(at time.Time) bool {
	return c != nil && !at.IsZero() && !at.Before(c.snapshot.CreatedAt) &&
		c.snapshot.ConsumedAt == nil && c.snapshot.InvalidatedAt == nil &&
		c.snapshot.Attempts < c.snapshot.MaxAttempts && at.Before(c.snapshot.ExpiresAt)
}

// Invalidate irreversibly cancels an unconsumed challenge.
func (c *Challenge) Invalidate(at time.Time, reason InvalidationReason) error {
	if c == nil {
		return ErrChallengeIDRequired
	}
	if c.snapshot.ConsumedAt != nil {
		return ErrChallengeConsumed
	}
	if c.snapshot.InvalidatedAt != nil {
		return nil
	}
	if at.IsZero() || at.Before(c.snapshot.CreatedAt) || !validInvalidationReason(reason) {
		return ErrInvalidationInvalid
	}
	at = at.UTC()
	c.snapshot.InvalidatedAt = &at
	c.snapshot.InvalidationReason = &reason
	return nil
}

func validateSnapshot(value Snapshot) error {
	switch {
	case value.ID == "":
		return ErrChallengeIDRequired
	case value.CompanyID == "":
		return ErrCompanyRequired
	case !validEmail(value.NormalizedEmail) || value.NormalizedEmail != strings.ToLower(strings.TrimSpace(value.NormalizedEmail)):
		return ErrEmailRequired
	case !validPurpose(value.Purpose):
		return ErrUnknownPurpose
	case (value.Purpose == PurposeSessionBootstrap && value.SourceID != nil) ||
		(value.Purpose != PurposeSessionBootstrap && (value.SourceID == nil || *value.SourceID == "")):
		return ErrSourceShapeInvalid
	case len(value.CodeHash) != 32:
		return ErrCodeHashRequired
	case value.RequestIPHash != nil && len(value.RequestIPHash) != 32:
		return ErrRequestIPHashInvalid
	case value.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	case !value.ExpiresAt.After(value.CreatedAt) || value.ExpiresAt.Sub(value.CreatedAt) > ChallengeTTL:
		return ErrExpiryInvalid
	case value.MaxAttempts < 1 || value.MaxAttempts > DefaultMaxAttempts:
		return ErrMaxAttemptsInvalid
	case value.Attempts < 0 || value.Attempts > value.MaxAttempts:
		return ErrAttemptsInvalid
	case value.ConsumedAt != nil && (value.ConsumedAt.Before(value.CreatedAt) || !value.ConsumedAt.Before(value.ExpiresAt)):
		return ErrConsumedAtInvalid
	case value.ConsumedAt != nil && value.InvalidatedAt != nil:
		return ErrInvalidationInvalid
	case (value.InvalidatedAt == nil) != (value.InvalidationReason == nil):
		return ErrInvalidationInvalid
	case value.InvalidatedAt != nil && (value.InvalidatedAt.Before(value.CreatedAt) || !validInvalidationReason(*value.InvalidationReason)):
		return ErrInvalidationInvalid
	default:
		return nil
	}
}

func validPurpose(value Purpose) bool {
	return value == PurposePersonalAccess || value == PurposeCampaignAccess || value == PurposeSessionBootstrap
}

func validInvalidationReason(value InvalidationReason) bool {
	switch value {
	case InvalidationExpired, InvalidationReplaced, InvalidationRateLimited, InvalidationAttemptsExhausted:
		return true
	default:
		return false
	}
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

func constantTimeEqual(left, right []byte) bool {
	return subtle.ConstantTimeCompare(left, right) == 1
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	result.SourceID = idPointer(value.SourceID)
	result.ClaimedFirstName = stringPointer(value.ClaimedFirstName)
	result.ClaimedLastName = stringPointer(value.ClaimedLastName)
	result.CodeHash = cloneBytes(value.CodeHash)
	result.RequestIPHash = cloneBytes(value.RequestIPHash)
	result.ConsumedAt = timePointer(value.ConsumedAt)
	result.InvalidatedAt = timePointer(value.InvalidatedAt)
	if value.InvalidationReason != nil {
		copy := *value.InvalidationReason
		result.InvalidationReason = &copy
	}
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

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
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
