// Package externalsession owns the Academy-only session used by external
// learners. It is intentionally isolated from TeamOS user authentication.
package externalsession

import (
	"crypto/subtle"
	"errors"
	"time"
)

// ID is an opaque Academy identifier.
type ID string

// RevocationReason distinguishes automatic cleanup, manual logout/revoke,
// and replacement by a rotated session.
type RevocationReason string

const (
	RevocationExpired RevocationReason = "expired"
	RevocationManual  RevocationReason = "manual"
	RevocationRotated RevocationReason = "rotated"
)

var (
	ErrSessionIDRequired    = errors.New("Для внешней сессии требуется идентификатор")
	ErrCompanyRequired      = errors.New("Для внешней сессии требуется компания")
	ErrLearnerRequired      = errors.New("Для внешней сессии требуется внешний ученик")
	ErrTokenHashRequired    = errors.New("Для внешней сессии требуется хеш токена")
	ErrCreatedAtRequired    = errors.New("Для внешней сессии требуется дата создания")
	ErrExpiryInvalid        = errors.New("Срок внешней сессии задан некорректно")
	ErrLastUsedInvalid      = errors.New("Дата использования внешней сессии задана некорректно")
	ErrRevokedAtInvalid     = errors.New("Дата отзыва внешней сессии задана некорректно")
	ErrRevocationInvalid    = errors.New("Причина отзыва внешней сессии задана некорректно")
	ErrSessionExpired       = errors.New("Срок действия внешней сессии истёк")
	ErrSessionRevoked       = errors.New("Внешняя сессия отозвана")
	ErrSessionScopeMismatch = errors.New("Внешняя сессия не даёт доступ к этому прохождению")
	ErrSessionTimeInvalid   = errors.New("Время использования внешней сессии задано некорректно")
)

// Snapshot contains only a token hash. It has no role, user ID, internal JWT,
// or any other field that could grant internal TeamOS access.
type Snapshot struct {
	ID                ID
	CompanyID         ID
	ExternalLearnerID ID
	TokenHash         []byte
	ExpiresAt         time.Time
	LastUsedAt        *time.Time
	RevokedAt         *time.Time
	RevocationReason  *RevocationReason
	CreatedAt         time.Time
}

// NewParams leaves session TTL selection to the application policy because
// the product plan does not prescribe a V1 duration.
type NewParams struct {
	ID                ID
	CompanyID         ID
	ExternalLearnerID ID
	TokenHash         []byte
	ExpiresAt         time.Time
	CreatedAt         time.Time
}

// Session is one tenant-scoped external authentication session.
type Session struct {
	snapshot Snapshot
}

// New creates a session from a pre-hashed token.
func New(params NewParams) (*Session, error) {
	return Rehydrate(Snapshot{
		ID: params.ID, CompanyID: params.CompanyID,
		ExternalLearnerID: params.ExternalLearnerID, TokenHash: cloneBytes(params.TokenHash),
		ExpiresAt: params.ExpiresAt.UTC(), CreatedAt: params.CreatedAt.UTC(),
	})
}

// Rehydrate validates and canonicalizes a stored session.
func Rehydrate(snapshot Snapshot) (*Session, error) {
	value := snapshot
	value.TokenHash = cloneBytes(snapshot.TokenHash)
	value.CreatedAt = value.CreatedAt.UTC()
	value.ExpiresAt = value.ExpiresAt.UTC()
	value.LastUsedAt = utcPointer(value.LastUsedAt)
	value.RevokedAt = utcPointer(value.RevokedAt)
	if err := validateSnapshot(value); err != nil {
		return nil, err
	}
	return &Session{snapshot: value}, nil
}

// Snapshot returns an isolated hash-only representation.
func (s *Session) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	result := s.snapshot
	result.TokenHash = cloneBytes(s.snapshot.TokenHash)
	result.LastUsedAt = timePointer(s.snapshot.LastUsedAt)
	result.RevokedAt = timePointer(s.snapshot.RevokedAt)
	if s.snapshot.RevocationReason != nil {
		copy := *s.snapshot.RevocationReason
		result.RevocationReason = &copy
	}
	return result
}

// MatchesTokenHash compares a derived candidate hash in constant time.
func (s *Session) MatchesTokenHash(candidateHash []byte) bool {
	return s != nil && subtle.ConstantTimeCompare(s.snapshot.TokenHash, candidateHash) == 1
}

// Authorize checks tenant, learner ownership, revocation, and expiration.
// Supplying a User ID is impossible by construction.
func (s *Session) Authorize(companyID, externalLearnerID ID, at time.Time) error {
	if s == nil || s.snapshot.CompanyID != companyID || s.snapshot.ExternalLearnerID != externalLearnerID ||
		companyID == "" || externalLearnerID == "" {
		return ErrSessionScopeMismatch
	}
	return s.ensureActive(at)
}

// Touch records server-side use while keeping expiry fixed.
func (s *Session) Touch(at time.Time) error {
	if s == nil {
		return ErrSessionIDRequired
	}
	if err := s.ensureActive(at); err != nil {
		return err
	}
	at = at.UTC()
	if s.snapshot.LastUsedAt != nil && at.Before(*s.snapshot.LastUsedAt) {
		return ErrSessionTimeInvalid
	}
	s.snapshot.LastUsedAt = &at
	return nil
}

// Revoke is irreversible and idempotent.
func (s *Session) Revoke(at time.Time, reason RevocationReason) error {
	if s == nil {
		return ErrSessionIDRequired
	}
	if s.snapshot.RevokedAt != nil {
		return nil
	}
	if !validRevocationReason(reason) {
		return ErrRevocationInvalid
	}
	if at.IsZero() || at.Before(s.snapshot.CreatedAt) ||
		(s.snapshot.LastUsedAt != nil && at.Before(*s.snapshot.LastUsedAt)) {
		return ErrSessionTimeInvalid
	}
	at = at.UTC()
	s.snapshot.RevokedAt = &at
	s.snapshot.RevocationReason = &reason
	return nil
}

// Expire marks an elapsed session for cleanup. It cannot revoke early.
func (s *Session) Expire(at time.Time) (bool, error) {
	if s == nil || s.snapshot.RevokedAt != nil || at.IsZero() || at.Before(s.snapshot.ExpiresAt) {
		return false, nil
	}
	return true, s.Revoke(at, RevocationExpired)
}

// Expired materializes no state: storage may delete/revoke it separately.
func (s *Session) Expired(at time.Time) bool {
	return s == nil || at.IsZero() || !at.Before(s.snapshot.ExpiresAt)
}

func (s *Session) ensureActive(at time.Time) error {
	if s.snapshot.RevokedAt != nil {
		return ErrSessionRevoked
	}
	if at.IsZero() || at.Before(s.snapshot.CreatedAt) {
		return ErrSessionTimeInvalid
	}
	if !at.Before(s.snapshot.ExpiresAt) {
		return ErrSessionExpired
	}
	return nil
}

func validateSnapshot(value Snapshot) error {
	switch {
	case value.ID == "":
		return ErrSessionIDRequired
	case value.CompanyID == "":
		return ErrCompanyRequired
	case value.ExternalLearnerID == "":
		return ErrLearnerRequired
	case len(value.TokenHash) != 32:
		return ErrTokenHashRequired
	case value.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	case !value.ExpiresAt.After(value.CreatedAt):
		return ErrExpiryInvalid
	case value.LastUsedAt != nil && (value.LastUsedAt.Before(value.CreatedAt) || !value.LastUsedAt.Before(value.ExpiresAt)):
		return ErrLastUsedInvalid
	case value.RevokedAt != nil && value.RevokedAt.Before(value.CreatedAt):
		return ErrRevokedAtInvalid
	case (value.RevokedAt == nil) != (value.RevocationReason == nil):
		return ErrRevocationInvalid
	case value.RevocationReason != nil && !validRevocationReason(*value.RevocationReason):
		return ErrRevocationInvalid
	default:
		return nil
	}
}

func validRevocationReason(value RevocationReason) bool {
	return value == RevocationExpired || value == RevocationManual || value == RevocationRotated
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
