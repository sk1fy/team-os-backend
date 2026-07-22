// Package course contains the lifecycle and ownership invariants of an
// Academy course. It intentionally has no persistence or transport concerns.
package course

import "errors"

// ID is an opaque identifier of an Academy or company entity.
type ID string

// OwnerType identifies the party that controls course content.
type OwnerType string

const (
	CourseOwnerCompany OwnerType = "company"
	CourseOwnerPartner OwnerType = "partner"
)

// LifecycleStatus describes the product lifecycle of a course.
type LifecycleStatus string

const (
	CourseActive   LifecycleStatus = "active"
	CourseArchived LifecycleStatus = "archived"
	CourseDeleted  LifecycleStatus = "deleted"
)

// DistributionStatus describes administrative distribution restrictions.
type DistributionStatus string

const (
	DistributionActive  DistributionStatus = "active"
	DistributionPaused  DistributionStatus = "paused"
	DistributionBlocked DistributionStatus = "blocked"
)

// EffectiveState is the state that governs user-facing course availability.
type EffectiveState string

const (
	EffectiveActive   EffectiveState = "active"
	EffectivePaused   EffectiveState = "paused"
	EffectiveArchived EffectiveState = "archived"
	EffectiveBlocked  EffectiveState = "blocked"
	EffectiveDeleted  EffectiveState = "deleted"
)

var (
	ErrCompanyRequired           = errors.New("Для курса требуется компания")
	ErrUnknownOwnerType          = errors.New("Неизвестный тип владельца курса")
	ErrCompanyOwnerUserForbidden = errors.New("У курса компании не может быть владельца-пользователя")
	ErrPartnerOwnerUserRequired  = errors.New("Для партнёрского курса требуется владелец-партнёр")
	ErrUnknownLifecycleStatus    = errors.New("Неизвестное состояние жизненного цикла курса")
	ErrUnknownDistributionStatus = errors.New("Неизвестное состояние распространения курса")
	ErrCourseAlreadyArchived     = errors.New("Курс уже находится в архиве")
	ErrCourseNotArchived         = errors.New("Курс не находится в архиве")
	ErrCourseDeleted             = errors.New("Курс удалён")
)

// Course is the subset of a course root needed by pure domain rules.
type Course struct {
	ID                 ID
	CompanyID          ID
	OwnerType          OwnerType
	OwnerUserID        *ID
	LifecycleStatus    LifecycleStatus
	DistributionStatus DistributionStatus
}

// ValidateOwnership enforces the relationship between owner_type and
// owner_user_id. An empty partner ID is treated as absent.
func ValidateOwnership(ownerType OwnerType, ownerUserID *ID) error {
	switch ownerType {
	case CourseOwnerCompany:
		if ownerUserID != nil {
			return ErrCompanyOwnerUserForbidden
		}
	case CourseOwnerPartner:
		if ownerUserID == nil || *ownerUserID == "" {
			return ErrPartnerOwnerUserRequired
		}
	default:
		return ErrUnknownOwnerType
	}

	return nil
}

// Validate checks the invariants represented by the course root.
func (c Course) Validate() error {
	if c.CompanyID == "" {
		return ErrCompanyRequired
	}
	if err := ValidateOwnership(c.OwnerType, c.OwnerUserID); err != nil {
		return err
	}

	switch c.LifecycleStatus {
	case CourseActive, CourseArchived, CourseDeleted:
	default:
		return ErrUnknownLifecycleStatus
	}

	switch c.DistributionStatus {
	case DistributionActive, DistributionPaused, DistributionBlocked:
	default:
		return ErrUnknownDistributionStatus
	}

	return nil
}

// EffectiveState applies the required priority:
// deleted > blocked > archived > paused > active.
func (c Course) EffectiveState() (EffectiveState, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}

	switch {
	case c.LifecycleStatus == CourseDeleted:
		return EffectiveDeleted, nil
	case c.DistributionStatus == DistributionBlocked:
		return EffectiveBlocked, nil
	case c.LifecycleStatus == CourseArchived:
		return EffectiveArchived, nil
	case c.DistributionStatus == DistributionPaused:
		return EffectivePaused, nil
	default:
		return EffectiveActive, nil
	}
}

// Archive moves an active course to the archive. A deleted course can never
// transition again.
func (c *Course) Archive() error {
	if err := c.Validate(); err != nil {
		return err
	}

	switch c.LifecycleStatus {
	case CourseDeleted:
		return ErrCourseDeleted
	case CourseArchived:
		return ErrCourseAlreadyArchived
	case CourseActive:
		c.LifecycleStatus = CourseArchived
		return nil
	default:
		return ErrUnknownLifecycleStatus
	}
}

// Restore returns an archived course to the active lifecycle. It does not
// imply restoring enrollments, accesses, or campaigns.
func (c *Course) Restore() error {
	if err := c.Validate(); err != nil {
		return err
	}

	switch c.LifecycleStatus {
	case CourseDeleted:
		return ErrCourseDeleted
	case CourseActive:
		return ErrCourseNotArchived
	case CourseArchived:
		c.LifecycleStatus = CourseActive
		return nil
	default:
		return ErrUnknownLifecycleStatus
	}
}

// Delete irreversibly marks an active or archived course as deleted.
func (c *Course) Delete() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.LifecycleStatus == CourseDeleted {
		return ErrCourseDeleted
	}

	c.LifecycleStatus = CourseDeleted
	return nil
}
