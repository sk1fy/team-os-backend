// Package externalcampaign owns public Academy campaigns and their token
// lifecycle. Raw tokens and persistence concerns never enter the aggregate.
package externalcampaign

import (
	"crypto/subtle"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

// ID is an opaque Academy or company identifier.
type ID string

// OwnerType identifies who owns and manages a campaign.
type OwnerType string

const (
	OwnerCompany OwnerType = "company"
	OwnerPartner OwnerType = "partner"
)

// Purpose fixes the product flow represented by a campaign.
type Purpose string

const (
	PurposeCompanyCandidate Purpose = "company_candidate"
	PurposePartnerPromo     Purpose = "partner_promo"
)

// Status is the campaign lifecycle. Revoked and closed are terminal.
type Status string

const (
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusRevoked Status = "revoked"
	StatusClosed  Status = "closed"

	MinDeadlineDays = 1
	MaxDeadlineDays = 7
	Day             = 24 * time.Hour
)

// Availability is the effective reason a campaign can or cannot accept a new
// learner. Course-wide restrictions take precedence over reversible campaign
// state; terminal campaign states remain explicit.
type Availability string

const (
	AvailabilityAvailable          Availability = "available"
	AvailabilityCampaignPaused     Availability = "campaign_paused"
	AvailabilityCampaignRevoked    Availability = "campaign_revoked"
	AvailabilityCampaignClosed     Availability = "campaign_closed"
	AvailabilityCoursePaused       Availability = "course_paused"
	AvailabilityCourseBlocked      Availability = "course_blocked"
	AvailabilityCourseArchived     Availability = "course_archived"
	AvailabilityCourseDeleted      Availability = "course_deleted"
	AvailabilityVersionUnavailable Availability = "version_unavailable"
)

var tokenPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,24}$`)

var (
	ErrCampaignIDRequired      = errors.New("Для кампании требуется идентификатор")
	ErrCompanyRequired         = errors.New("Для кампании требуется компания")
	ErrCourseRequired          = errors.New("Для кампании требуется курс")
	ErrCourseVersionRequired   = errors.New("Для кампании требуется версия курса")
	ErrUnknownOwnerType        = errors.New("Неизвестный тип владельца кампании")
	ErrCompanyOwnerForbidden   = errors.New("У кампании компании не может быть владельца-пользователя")
	ErrPartnerOwnerRequired    = errors.New("Для партнёрской кампании требуется владелец-партнёр")
	ErrUnknownPurpose          = errors.New("Неизвестное назначение кампании")
	ErrOwnerPurposeMismatch    = errors.New("Владелец кампании не соответствует её назначению")
	ErrCreatorRequired         = errors.New("Для кампании требуется автор")
	ErrPartnerCreatorMismatch  = errors.New("Партнёрскую кампанию может создать только владелец курса")
	ErrCampaignNameRequired    = errors.New("Для кампании требуется название")
	ErrDeadlineDaysInvalid     = errors.New("Срок кампании должен быть от одного до семи дней")
	ErrUnknownStatus           = errors.New("Неизвестное состояние кампании")
	ErrTokenHashRequired       = errors.New("Для кампании требуется хеш токена")
	ErrTokenPrefixRequired     = errors.New("Префикс токена кампании задан некорректно")
	ErrCreatedAtRequired       = errors.New("Для кампании требуется дата создания")
	ErrUpdatedAtInvalid        = errors.New("Дата изменения кампании задана некорректно")
	ErrPausedStateInvalid      = errors.New("Данные приостановки кампании заданы некорректно")
	ErrRevokedStateInvalid     = errors.New("Данные отзыва кампании заданы некорректно")
	ErrClosedStateInvalid      = errors.New("Данные закрытия кампании заданы некорректно")
	ErrTokenRotationInvalid    = errors.New("Новый токен кампании задан некорректно")
	ErrCampaignAlreadyPaused   = errors.New("Кампания уже приостановлена")
	ErrCampaignNotPaused       = errors.New("Кампания не приостановлена")
	ErrCampaignRevoked         = errors.New("Кампания отозвана")
	ErrCampaignClosed          = errors.New("Кампания закрыта")
	ErrCampaignScopeMismatch   = errors.New("Кампания относится к другому курсу или компании")
	ErrCampaignOwnerMismatch   = errors.New("Владелец кампании не соответствует владельцу курса")
	ErrCampaignVersionMismatch = errors.New("Кампания относится к другой версии курса")
	ErrCampaignResumeDenied    = errors.New("Кампанию нельзя возобновить для недоступного курса или версии")
)

// Snapshot is the persistence-neutral, hash-only campaign representation.
type Snapshot struct {
	ID              ID
	CompanyID       ID
	CourseID        ID
	CourseVersionID ID
	OwnerType       OwnerType
	OwnerUserID     *ID
	Purpose         Purpose
	Name            string
	DeadlineDays    int
	Status          Status
	TokenHash       []byte
	TokenPrefix     string
	CreatedByID     ID
	CreatedAt       time.Time
	PausedAt        *time.Time
	TokenRotatedAt  *time.Time
	RevokedAt       *time.Time
	ClosedAt        *time.Time
	UpdatedAt       time.Time
}

// NewParams contains values allocated by the application before persistence.
type NewParams struct {
	ID              ID
	CompanyID       ID
	CourseID        ID
	CourseVersionID ID
	OwnerType       OwnerType
	OwnerUserID     *ID
	Purpose         Purpose
	Name            string
	DeadlineDays    int
	TokenHash       []byte
	TokenPrefix     string
	CreatedByID     ID
	CreatedAt       time.Time
}

// Campaign owns one multi-learner external access token. Enrollment identity
// and campaign/email uniqueness belong to the enrollment aggregate/storage.
type Campaign struct {
	snapshot Snapshot
}

// Validate checks persistent invariants without retaining the aggregate.
func (s Snapshot) Validate() error {
	_, err := Rehydrate(s)
	return err
}

// New creates an active campaign pinned to one immutable course version.
func New(params NewParams) (*Campaign, error) {
	createdAt := params.CreatedAt.UTC()
	return Rehydrate(Snapshot{
		ID: params.ID, CompanyID: params.CompanyID, CourseID: params.CourseID,
		CourseVersionID: params.CourseVersionID, OwnerType: params.OwnerType,
		OwnerUserID: cloneID(params.OwnerUserID), Purpose: params.Purpose,
		Name: strings.TrimSpace(params.Name), DeadlineDays: params.DeadlineDays,
		Status: StatusActive, TokenHash: cloneBytes(params.TokenHash),
		TokenPrefix: params.TokenPrefix, CreatedByID: params.CreatedByID,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	})
}

// Rehydrate validates and defensively copies stored state.
func Rehydrate(snapshot Snapshot) (*Campaign, error) {
	value := cloneSnapshot(snapshot)
	value.Name = strings.TrimSpace(value.Name)
	canonicalizeTimes(&value)
	if err := validateSnapshot(value); err != nil {
		return nil, err
	}
	return &Campaign{snapshot: value}, nil
}

// Snapshot returns an isolated copy suitable for persistence or reporting.
func (c *Campaign) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return cloneSnapshot(c.snapshot)
}

// MatchesTokenHash compares a derived candidate hash in constant time.
func (c *Campaign) MatchesTokenHash(candidateHash []byte) bool {
	return c != nil && constantTimeEqual(c.snapshot.TokenHash, candidateHash)
}

// TokenUsable reports only local campaign state. EffectiveAvailability must
// also be checked before returning a landing page or activating an enrollment.
func (c *Campaign) TokenUsable() bool {
	return c != nil && c.snapshot.Status == StatusActive
}

// Pause prevents new learners without affecting existing enrollments.
func (c *Campaign) Pause(at time.Time) error {
	if c == nil {
		return ErrCampaignIDRequired
	}
	switch c.snapshot.Status {
	case StatusPaused:
		return ErrCampaignAlreadyPaused
	case StatusRevoked:
		return ErrCampaignRevoked
	case StatusClosed:
		return ErrCampaignClosed
	case StatusActive:
	default:
		return ErrUnknownStatus
	}
	if err := c.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	c.snapshot.Status = StatusPaused
	c.snapshot.PausedAt = &at
	c.snapshot.UpdatedAt = at
	return nil
}

// Resume returns a paused campaign to active state. An administrative course
// pause may coexist with the active campaign, but archive, delete, block, or a
// non-published version prevent the transition.
func (c *Campaign) Resume(root course.Course, version courseversion.Snapshot, at time.Time) error {
	if c == nil {
		return ErrCampaignIDRequired
	}
	switch c.snapshot.Status {
	case StatusActive:
		return ErrCampaignNotPaused
	case StatusRevoked:
		return ErrCampaignRevoked
	case StatusClosed:
		return ErrCampaignClosed
	case StatusPaused:
	default:
		return ErrUnknownStatus
	}
	if err := c.resumeAllowed(root, version); err != nil {
		return err
	}
	if err := c.validateTransition(at); err != nil {
		return err
	}
	c.snapshot.Status = StatusActive
	c.snapshot.PausedAt = nil
	c.snapshot.UpdatedAt = at.UTC()
	return nil
}

// RotateToken atomically invalidates the old campaign token without changing
// its status, pinned version, deadline policy, or existing enrollments.
func (c *Campaign) RotateToken(tokenHash []byte, tokenPrefix string, at time.Time) error {
	if err := c.ensureMutable(); err != nil {
		return err
	}
	if len(tokenHash) != 32 || !tokenPrefixPattern.MatchString(tokenPrefix) ||
		constantTimeEqual(c.snapshot.TokenHash, tokenHash) {
		return ErrTokenRotationInvalid
	}
	if err := c.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	c.snapshot.TokenHash = cloneBytes(tokenHash)
	c.snapshot.TokenPrefix = tokenPrefix
	c.snapshot.TokenRotatedAt = &at
	c.snapshot.UpdatedAt = at
	return nil
}

// Revoke irreversibly invalidates a campaign while preserving report links.
func (c *Campaign) Revoke(at time.Time) error {
	if c == nil {
		return ErrCampaignIDRequired
	}
	if c.snapshot.Status == StatusRevoked {
		return nil
	}
	if c.snapshot.Status == StatusClosed {
		return ErrCampaignClosed
	}
	if err := c.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	c.snapshot.Status = StatusRevoked
	c.snapshot.PausedAt = nil
	c.snapshot.RevokedAt = &at
	c.snapshot.UpdatedAt = at
	return nil
}

// Close applies irreversible course deletion to the campaign. Prior
// revocation metadata is retained for reporting.
func (c *Campaign) Close(at time.Time) error {
	if c == nil {
		return ErrCampaignIDRequired
	}
	if c.snapshot.Status == StatusClosed {
		return nil
	}
	if err := c.validateTransition(at); err != nil {
		return err
	}
	at = at.UTC()
	c.snapshot.Status = StatusClosed
	c.snapshot.PausedAt = nil
	c.snapshot.ClosedAt = &at
	c.snapshot.UpdatedAt = at
	return nil
}

// EffectiveAvailability combines campaign, course, and pinned-version state.
// It is the mandatory domain guard before accepting or activating a learner.
func (c *Campaign) EffectiveAvailability(
	root course.Course,
	version courseversion.Snapshot,
) (Availability, error) {
	if c == nil {
		return "", ErrCampaignIDRequired
	}
	if err := c.validateContext(root, version); err != nil {
		return "", err
	}
	if root.LifecycleStatus == course.CourseDeleted {
		return AvailabilityCourseDeleted, nil
	}
	if c.snapshot.Status == StatusClosed {
		return AvailabilityCampaignClosed, nil
	}
	if root.DistributionStatus == course.DistributionBlocked {
		return AvailabilityCourseBlocked, nil
	}
	if root.LifecycleStatus == course.CourseArchived {
		return AvailabilityCourseArchived, nil
	}
	if c.snapshot.Status == StatusRevoked {
		return AvailabilityCampaignRevoked, nil
	}
	if root.DistributionStatus == course.DistributionPaused {
		return AvailabilityCoursePaused, nil
	}
	if c.snapshot.Status == StatusPaused {
		return AvailabilityCampaignPaused, nil
	}
	if version.Status != courseversion.StatusPublished {
		return AvailabilityVersionUnavailable, nil
	}
	return AvailabilityAvailable, nil
}

// CanAcceptLearner applies the complete effective-state check.
func (c *Campaign) CanAcceptLearner(root course.Course, version courseversion.Snapshot) bool {
	availability, err := c.EffectiveAvailability(root, version)
	return err == nil && availability == AvailabilityAvailable
}

func (c *Campaign) resumeAllowed(root course.Course, version courseversion.Snapshot) error {
	availability, err := c.EffectiveAvailability(root, version)
	if err != nil {
		return err
	}
	switch availability {
	case AvailabilityCampaignPaused, AvailabilityCoursePaused:
		if version.Status == courseversion.StatusPublished &&
			root.LifecycleStatus == course.CourseActive &&
			root.DistributionStatus != course.DistributionBlocked {
			return nil
		}
	}
	return ErrCampaignResumeDenied
}

func (c *Campaign) validateContext(root course.Course, version courseversion.Snapshot) error {
	if err := root.Validate(); err != nil {
		return err
	}
	if _, err := courseversion.Rehydrate(version); err != nil {
		return err
	}
	if ID(root.CompanyID) != c.snapshot.CompanyID || ID(root.ID) != c.snapshot.CourseID {
		return ErrCampaignScopeMismatch
	}
	switch c.snapshot.OwnerType {
	case OwnerCompany:
		if root.OwnerType != course.CourseOwnerCompany {
			return ErrCampaignOwnerMismatch
		}
	case OwnerPartner:
		if root.OwnerType != course.CourseOwnerPartner || root.OwnerUserID == nil ||
			ID(*root.OwnerUserID) != *c.snapshot.OwnerUserID {
			return ErrCampaignOwnerMismatch
		}
	default:
		return ErrUnknownOwnerType
	}
	if ID(version.CompanyID) != c.snapshot.CompanyID || ID(version.CourseID) != c.snapshot.CourseID ||
		ID(version.ID) != c.snapshot.CourseVersionID {
		return ErrCampaignVersionMismatch
	}
	return nil
}

func (c *Campaign) ensureMutable() error {
	if c == nil {
		return ErrCampaignIDRequired
	}
	switch c.snapshot.Status {
	case StatusActive, StatusPaused:
		return nil
	case StatusRevoked:
		return ErrCampaignRevoked
	case StatusClosed:
		return ErrCampaignClosed
	default:
		return ErrUnknownStatus
	}
}

func (c *Campaign) validateTransition(at time.Time) error {
	if at.IsZero() || at.Before(c.snapshot.UpdatedAt) {
		return ErrUpdatedAtInvalid
	}
	return nil
}

func validateSnapshot(value Snapshot) error {
	switch {
	case value.ID == "":
		return ErrCampaignIDRequired
	case value.CompanyID == "":
		return ErrCompanyRequired
	case value.CourseID == "":
		return ErrCourseRequired
	case value.CourseVersionID == "":
		return ErrCourseVersionRequired
	case strings.TrimSpace(value.Name) == "":
		return ErrCampaignNameRequired
	case value.DeadlineDays < MinDeadlineDays || value.DeadlineDays > MaxDeadlineDays:
		return ErrDeadlineDaysInvalid
	case len(value.TokenHash) != 32:
		return ErrTokenHashRequired
	case !tokenPrefixPattern.MatchString(value.TokenPrefix):
		return ErrTokenPrefixRequired
	case value.CreatedByID == "":
		return ErrCreatorRequired
	case value.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	case value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt):
		return ErrUpdatedAtInvalid
	}
	if err := validateOwner(value); err != nil {
		return err
	}
	if err := validateTimes(value); err != nil {
		return err
	}
	return validateStatus(value)
}

func validateOwner(value Snapshot) error {
	if value.Purpose != PurposeCompanyCandidate && value.Purpose != PurposePartnerPromo {
		return ErrUnknownPurpose
	}
	switch value.OwnerType {
	case OwnerCompany:
		if value.OwnerUserID != nil {
			return ErrCompanyOwnerForbidden
		}
		if value.Purpose != PurposeCompanyCandidate {
			return ErrOwnerPurposeMismatch
		}
	case OwnerPartner:
		if value.OwnerUserID == nil || *value.OwnerUserID == "" {
			return ErrPartnerOwnerRequired
		}
		if value.Purpose != PurposePartnerPromo {
			return ErrOwnerPurposeMismatch
		}
		if value.CreatedByID != *value.OwnerUserID {
			return ErrPartnerCreatorMismatch
		}
	default:
		return ErrUnknownOwnerType
	}
	return nil
}

func validateTimes(value Snapshot) error {
	for _, timestamp := range []*time.Time{
		value.PausedAt, value.TokenRotatedAt, value.RevokedAt, value.ClosedAt,
	} {
		if timestamp != nil && (timestamp.IsZero() || timestamp.Before(value.CreatedAt) || timestamp.After(value.UpdatedAt)) {
			return ErrUpdatedAtInvalid
		}
	}
	if value.RevokedAt != nil && value.ClosedAt != nil && value.ClosedAt.Before(*value.RevokedAt) {
		return ErrClosedStateInvalid
	}
	return nil
}

func validateStatus(value Snapshot) error {
	switch value.Status {
	case StatusActive:
		if value.PausedAt != nil || value.RevokedAt != nil || value.ClosedAt != nil {
			return ErrPausedStateInvalid
		}
	case StatusPaused:
		if value.PausedAt == nil || value.RevokedAt != nil || value.ClosedAt != nil {
			return ErrPausedStateInvalid
		}
	case StatusRevoked:
		if value.PausedAt != nil || value.RevokedAt == nil || value.ClosedAt != nil {
			return ErrRevokedStateInvalid
		}
	case StatusClosed:
		if value.PausedAt != nil || value.ClosedAt == nil {
			return ErrClosedStateInvalid
		}
	default:
		return ErrUnknownStatus
	}
	return nil
}

func canonicalizeTimes(value *Snapshot) {
	value.CreatedAt = value.CreatedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	value.PausedAt = utcTime(value.PausedAt)
	value.TokenRotatedAt = utcTime(value.TokenRotatedAt)
	value.RevokedAt = utcTime(value.RevokedAt)
	value.ClosedAt = utcTime(value.ClosedAt)
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	result.OwnerUserID = cloneID(value.OwnerUserID)
	result.TokenHash = cloneBytes(value.TokenHash)
	result.PausedAt = cloneTime(value.PausedAt)
	result.TokenRotatedAt = cloneTime(value.TokenRotatedAt)
	result.RevokedAt = cloneTime(value.RevokedAt)
	result.ClosedAt = cloneTime(value.ClosedAt)
	return result
}

func cloneID(value *ID) *ID {
	if value == nil {
		return nil
	}
	copyID := *value
	return &copyID
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyTime := *value
	return &copyTime
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func constantTimeEqual(left, right []byte) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(left, right) == 1
}
