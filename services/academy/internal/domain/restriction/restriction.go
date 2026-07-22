// Package restriction contains the lifecycle of administrative restrictions
// placed on partner-course distribution. Persistence and authorization are
// intentionally handled outside this package.
package restriction

import (
	"errors"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
)

// ID is an opaque restriction or user identifier.
type ID string

// Kind identifies the distribution state caused by a restriction.
type Kind string

const (
	KindPause Kind = "pause"
	KindBlock Kind = "block"
)

var (
	ErrRestrictionIDRequired = errors.New("Для ограничения требуется идентификатор")
	ErrCourseIDRequired      = errors.New("Для ограничения требуется курс")
	ErrCompanyIDRequired     = errors.New("Для ограничения требуется компания")
	ErrUnknownKind           = errors.New("Неизвестный тип ограничения курса")
	ErrReasonRequired        = errors.New("Для ограничения требуется причина")
	ErrActorRequired         = errors.New("Для ограничения требуется пользователь")
	ErrAppliedAtRequired     = errors.New("Для ограничения требуется дата применения")
	ErrResolveActorRequired  = errors.New("Для снятия ограничения требуется пользователь")
	ErrResolvedAtRequired    = errors.New("Для снятия ограничения требуется дата")
	ErrResolvedBeforeApplied = errors.New("Ограничение нельзя снять раньше его применения")
	ErrResolutionIncomplete  = errors.New("Данные снятия ограничения должны быть заполнены полностью")
	ErrDuplicateActiveKind   = errors.New("Ограничение этого типа уже действует")
	ErrRestrictionNotFound   = errors.New("Ограничение не найдено")
	ErrRestrictionResolved   = errors.New("Ограничение уже снято")
	ErrRestrictionScope      = errors.New("Ограничение относится к другому курсу или компании")
)

// Restriction is one immutable application event plus optional resolution.
// Rows remain in history after resolution.
type Restriction struct {
	ID           ID
	CompanyID    course.ID
	CourseID     course.ID
	Kind         Kind
	Reason       string
	AppliedByID  ID
	AppliedAt    time.Time
	ResolvedByID *ID
	ResolvedAt   *time.Time
}

// Active reports whether the restriction has not been resolved.
func (r Restriction) Active() bool {
	return r.ResolvedAt == nil
}

// Validate checks a stored restriction independently from a set.
func (r Restriction) Validate() error {
	switch {
	case r.ID == "":
		return ErrRestrictionIDRequired
	case r.CompanyID == "":
		return ErrCompanyIDRequired
	case r.CourseID == "":
		return ErrCourseIDRequired
	case strings.TrimSpace(r.Reason) == "":
		return ErrReasonRequired
	case r.AppliedByID == "":
		return ErrActorRequired
	case r.AppliedAt.IsZero():
		return ErrAppliedAtRequired
	}
	if r.Kind != KindPause && r.Kind != KindBlock {
		return ErrUnknownKind
	}
	if (r.ResolvedByID == nil) != (r.ResolvedAt == nil) {
		return ErrResolutionIncomplete
	}
	if r.ResolvedByID != nil && *r.ResolvedByID == "" {
		return ErrResolveActorRequired
	}
	if r.ResolvedAt != nil {
		if r.ResolvedAt.IsZero() {
			return ErrResolvedAtRequired
		}
		if r.ResolvedAt.Before(r.AppliedAt) {
			return ErrResolvedBeforeApplied
		}
	}
	return nil
}

// ApplyParams describes a new administrative restriction. IDs and time are
// allocated by the application layer so retries can be idempotent.
type ApplyParams struct {
	ID          ID
	Kind        Kind
	Reason      string
	AppliedByID ID
	AppliedAt   time.Time
}

// Set owns the active restrictions and their audit history for one course.
type Set struct {
	companyID    course.ID
	courseID     course.ID
	restrictions []Restriction
}

// Rehydrate restores and validates restriction history. At most one active
// pause and one active block may exist at a time.
func Rehydrate(companyID, courseID course.ID, restrictions []Restriction) (*Set, error) {
	if companyID == "" {
		return nil, ErrCompanyIDRequired
	}
	if courseID == "" {
		return nil, ErrCourseIDRequired
	}
	result := &Set{companyID: companyID, courseID: courseID}
	active := make(map[Kind]bool, 2)
	ids := make(map[ID]struct{}, len(restrictions))
	for _, value := range restrictions {
		if err := value.Validate(); err != nil {
			return nil, err
		}
		if value.CompanyID != companyID || value.CourseID != courseID {
			return nil, ErrRestrictionScope
		}
		if _, exists := ids[value.ID]; exists {
			return nil, ErrRestrictionIDRequired
		}
		ids[value.ID] = struct{}{}
		if value.Active() {
			if active[value.Kind] {
				return nil, ErrDuplicateActiveKind
			}
			active[value.Kind] = true
		}
		result.restrictions = append(result.restrictions, cloneRestriction(value))
	}
	return result, nil
}

// Apply adds a restriction without discarding a lower-priority active one.
// This is what lets resolving a block reveal a previously active pause.
func (s *Set) Apply(params ApplyParams) (course.DistributionStatus, error) {
	if s == nil {
		return "", ErrRestrictionScope
	}
	value := Restriction{
		ID: params.ID, CompanyID: s.companyID, CourseID: s.courseID,
		Kind: params.Kind, Reason: params.Reason, AppliedByID: params.AppliedByID,
		AppliedAt: params.AppliedAt.UTC(),
	}
	if err := value.Validate(); err != nil {
		return "", err
	}
	for _, existing := range s.restrictions {
		if existing.ID == value.ID {
			return "", ErrRestrictionIDRequired
		}
		if existing.Active() && existing.Kind == value.Kind {
			return "", ErrDuplicateActiveKind
		}
	}
	s.restrictions = append(s.restrictions, value)
	return s.EffectiveStatus(), nil
}

// Resolve closes one active restriction and returns the newly effective
// distribution status. Resolving a block never silently resolves a pause.
func (s *Set) Resolve(id, actorID ID, at time.Time) (course.DistributionStatus, error) {
	if s == nil {
		return "", ErrRestrictionScope
	}
	if actorID == "" {
		return "", ErrResolveActorRequired
	}
	if at.IsZero() {
		return "", ErrResolvedAtRequired
	}
	for index := range s.restrictions {
		value := &s.restrictions[index]
		if value.ID != id {
			continue
		}
		if !value.Active() {
			return "", ErrRestrictionResolved
		}
		at = at.UTC()
		if at.Before(value.AppliedAt) {
			return "", ErrResolvedBeforeApplied
		}
		resolver := actorID
		value.ResolvedByID = &resolver
		value.ResolvedAt = &at
		return s.EffectiveStatus(), nil
	}
	return "", ErrRestrictionNotFound
}

// EffectiveStatus applies block > pause > active. Course lifecycle remains a
// separate higher-level concern in course.EffectiveState.
func (s *Set) EffectiveStatus() course.DistributionStatus {
	if s == nil {
		return course.DistributionActive
	}
	paused := false
	for _, value := range s.restrictions {
		if !value.Active() {
			continue
		}
		if value.Kind == KindBlock {
			return course.DistributionBlocked
		}
		if value.Kind == KindPause {
			paused = true
		}
	}
	if paused {
		return course.DistributionPaused
	}
	return course.DistributionActive
}

// Snapshot returns a defensive copy suitable for persistence or reporting.
func (s *Set) Snapshot() []Restriction {
	if s == nil {
		return nil
	}
	result := make([]Restriction, len(s.restrictions))
	for index, value := range s.restrictions {
		result[index] = cloneRestriction(value)
	}
	return result
}

func cloneRestriction(value Restriction) Restriction {
	result := value
	if value.ResolvedByID != nil {
		copyID := *value.ResolvedByID
		result.ResolvedByID = &copyID
	}
	if value.ResolvedAt != nil {
		copyTime := *value.ResolvedAt
		result.ResolvedAt = &copyTime
	}
	return result
}
