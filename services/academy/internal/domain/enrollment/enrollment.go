// Package enrollment contains the persistence-neutral state machine for one
// concrete Academy course run. An enrollment is permanently pinned to a
// published course version; all progress and quiz attempts belong to it.
package enrollment

import (
	"errors"
	"time"
)

// ID is an opaque Academy or company identifier.
type ID string

// LearnerType determines which identity field and deadline rules apply.
type LearnerType string

const (
	LearnerUser     LearnerType = "user"
	LearnerExternal LearnerType = "external"
)

// SourceType explains why this course run exists.
type SourceType string

const (
	SourceAssignment               SourceType = "assignment"
	SourcePersonalAccess           SourceType = "personal_access"
	SourcePartnerPromoCampaign     SourceType = "partner_promo_campaign"
	SourceCompanyCandidateCampaign SourceType = "company_candidate_campaign"
	SourceRepeatTraining           SourceType = "repeat_training"
	SourceLegacy                   SourceType = "legacy"
	SourceSelfEnrollment           SourceType = "self_enrollment"
)

// ProgressStatus is deliberately independent from access restrictions.
type ProgressStatus string

const (
	ProgressNotStarted ProgressStatus = "not_started"
	ProgressInProgress ProgressStatus = "in_progress"
	ProgressCompleted  ProgressStatus = "completed"
)

// AccessStatus governs whether the learner can move forward. Pending and
// completed are derived learner states, not access statuses.
type AccessStatus string

const (
	AccessInvited   AccessStatus = "invited"
	AccessReady     AccessStatus = "ready"
	AccessActive    AccessStatus = "active"
	AccessExpired   AccessStatus = "expired"
	AccessFrozen    AccessStatus = "frozen"
	AccessSuspended AccessStatus = "suspended"
	AccessRevoked   AccessStatus = "revoked"
	AccessClosed    AccessStatus = "closed"
)

// LearnerState is a stable projection for resume screens and reports.
type LearnerState string

const (
	StatePending   LearnerState = "pending"
	StateActive    LearnerState = "active"
	StateCompleted LearnerState = "completed"
	StateExpired   LearnerState = "expired"
	StateFrozen    LearnerState = "frozen"
	StateSuspended LearnerState = "suspended"
	StateRevoked   LearnerState = "revoked"
	StateClosed    LearnerState = "closed"
)

var (
	ErrEnrollmentIDRequired       = errors.New("Для прохождения требуется идентификатор")
	ErrCompanyRequired            = errors.New("Для прохождения требуется компания")
	ErrCourseRequired             = errors.New("Для прохождения требуется курс")
	ErrCourseVersionRequired      = errors.New("Для прохождения требуется версия курса")
	ErrCourseVersionMismatch      = errors.New("Прохождение закреплено за другой версией курса")
	ErrUnknownLearnerType         = errors.New("Неизвестный тип ученика")
	ErrLearnerIdentityRequired    = errors.New("Для прохождения требуется ученик")
	ErrLearnerIdentityConflict    = errors.New("У прохождения должна быть ровно одна учётная запись ученика")
	ErrUnknownSourceType          = errors.New("Неизвестный источник прохождения")
	ErrEnrollmentAttemptInvalid   = errors.New("Номер прохождения должен быть больше нуля")
	ErrUnknownProgressStatus      = errors.New("Неизвестное состояние прогресса")
	ErrUnknownAccessStatus        = errors.New("Неизвестное состояние доступа")
	ErrCreatedAtRequired          = errors.New("Для прохождения требуется дата создания")
	ErrExternalDeadlineRequired   = errors.New("Для активного внешнего прохождения требуется срок доступа")
	ErrInternalDeadlineForbidden  = errors.New("Внутреннее прохождение не использует жёсткий срок доступа")
	ErrInvalidDeadline            = errors.New("Срок доступа должен быть позже даты активации")
	ErrActivationTimeRequired     = errors.New("Для активации требуется время сервера")
	ErrActivationDurationInvalid  = errors.New("Срок внешнего доступа должен быть от одного до семи дней")
	ErrEnrollmentNotReady         = errors.New("Прохождение нельзя активировать в текущем состоянии")
	ErrEnrollmentNotSuspended     = errors.New("Прохождение не приостановлено")
	ErrEnrollmentNotFrozen        = errors.New("Прохождение не заморожено")
	ErrTransitionTimeInvalid      = errors.New("Время перехода не может быть раньше предыдущего события")
	ErrReactivationDeadline       = errors.New("Для возобновления внешнего прохождения требуется новый срок доступа")
	ErrEnrollmentRevoked          = errors.New("Доступ к прохождению отозван")
	ErrEnrollmentClosed           = errors.New("Прохождение закрыто")
	ErrExternalEnrollmentRequired = errors.New("Продление доступно только для внешнего прохождения")
	ErrEnrollmentCannotExtend     = errors.New("Прохождение нельзя продлить в текущем состоянии")
)

// LessonStatus is stored only for unlocked lessons. A missing progress row is
// a locked lesson in a sequential course.
type LessonStatus string

const (
	LessonAvailable LessonStatus = "available"
	LessonCurrent   LessonStatus = "current"
	LessonCompleted LessonStatus = "completed"
)

// LessonSpec is ordered exactly as it appears in the pinned version.
type LessonSpec struct {
	ID       ID
	QuizID   *ID
	Optional bool
}

// LessonProgress belongs to this enrollment and one lesson of its pinned
// version. LastPosition is an opaque server-stored resume cursor.
type LessonProgress struct {
	LessonVersionID ID
	Status          LessonStatus
	FirstOpenedAt   *time.Time
	CompletedAt     *time.Time
	ActiveSeconds   int64
	LastPosition    *string
}

// QuizAttempt is an immutable decision recorded for one submitted quiz.
type QuizAttempt struct {
	ID            ID
	QuizVersionID ID
	LessonID      ID
	Number        int
	Score         int
	Passed        bool
	PendingReview bool
	CreatedAt     time.Time
}

// Snapshot is a persistence-neutral representation. Returned snapshots are
// defensively copied so callers cannot mutate the aggregate indirectly.
type Snapshot struct {
	ID              ID
	CompanyID       ID
	CourseID        ID
	CourseVersionID ID
	LearnerType     LearnerType
	UserID          *ID
	ExternalID      *ID
	SourceType      SourceType
	SourceID        *ID
	AttemptNumber   int
	ProgressStatus  ProgressStatus
	AccessStatus    AccessStatus
	PreviousAccess  *AccessStatus
	Sequential      bool
	Lessons         []LessonSpec
	LessonProgress  []LessonProgress
	QuizAttempts    []QuizAttempt
	ActivatedAt     *time.Time
	AccessUntil     *time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	LastActivityAt  *time.Time
	FrozenAt        *time.Time
	SuspendedAt     *time.Time
	CreatedAt       time.Time
}

// Params creates a new, not-yet-started enrollment.
type Params struct {
	ID              ID
	CompanyID       ID
	CourseID        ID
	CourseVersionID ID
	LearnerType     LearnerType
	UserID          *ID
	ExternalID      *ID
	SourceType      SourceType
	SourceID        *ID
	AttemptNumber   int
	AccessStatus    AccessStatus
	Sequential      bool
	Lessons         []LessonSpec
	CreatedAt       time.Time
}

// Activation defines the server-authoritative activation time. Duration is
// required only for external learners and is ignored for neither learner.
type Activation struct {
	At       time.Time
	Duration time.Duration
}

// Enrollment owns one course run.
type Enrollment struct {
	snapshot Snapshot
}

// New creates a pending enrollment. Invited or ready are the only permitted
// initial access states; use Rehydrate for migrations and stored records.
func New(params Params) (*Enrollment, error) {
	if params.AccessStatus != AccessInvited && params.AccessStatus != AccessReady {
		return nil, ErrEnrollmentNotReady
	}
	return Rehydrate(Snapshot{
		ID:              params.ID,
		CompanyID:       params.CompanyID,
		CourseID:        params.CourseID,
		CourseVersionID: params.CourseVersionID,
		LearnerType:     params.LearnerType,
		UserID:          params.UserID,
		ExternalID:      params.ExternalID,
		SourceType:      params.SourceType,
		SourceID:        params.SourceID,
		AttemptNumber:   params.AttemptNumber,
		ProgressStatus:  ProgressNotStarted,
		AccessStatus:    params.AccessStatus,
		Sequential:      params.Sequential,
		Lessons:         params.Lessons,
		CreatedAt:       params.CreatedAt,
	})
}

// Rehydrate validates and restores a stored enrollment.
func Rehydrate(snapshot Snapshot) (*Enrollment, error) {
	copy := cloneSnapshot(snapshot)
	if err := validateSnapshot(copy); err != nil {
		return nil, err
	}
	return &Enrollment{snapshot: copy}, nil
}

// Snapshot returns an isolated copy of the current aggregate state.
func (e *Enrollment) Snapshot() Snapshot {
	if e == nil {
		return Snapshot{}
	}
	return cloneSnapshot(e.snapshot)
}

// EnsureVersion prevents content from another version from being applied to
// an existing enrollment.
func (e *Enrollment) EnsureVersion(courseVersionID ID) error {
	if e == nil || e.snapshot.CourseVersionID == "" {
		return ErrCourseVersionRequired
	}
	if courseVersionID == "" || courseVersionID != e.snapshot.CourseVersionID {
		return ErrCourseVersionMismatch
	}
	return nil
}

// State returns the restriction-first learner state used by resume and
// reporting. Access restrictions intentionally override completion.
func (e *Enrollment) State() LearnerState {
	if e == nil {
		return StateClosed
	}
	switch e.snapshot.AccessStatus {
	case AccessExpired:
		return StateExpired
	case AccessFrozen:
		return StateFrozen
	case AccessSuspended:
		return StateSuspended
	case AccessRevoked:
		return StateRevoked
	case AccessClosed:
		return StateClosed
	}
	if e.snapshot.ProgressStatus == ProgressCompleted {
		return StateCompleted
	}
	if e.snapshot.AccessStatus == AccessActive {
		return StateActive
	}
	return StatePending
}

// Activate is idempotent after the first successful activation. The original
// server deadline is never extended by a repeated click.
func (e *Enrollment) Activate(params Activation) error {
	if params.At.IsZero() {
		return ErrActivationTimeRequired
	}
	if e.snapshot.AccessStatus == AccessActive && e.snapshot.ActivatedAt != nil {
		return nil
	}
	if e.snapshot.AccessStatus != AccessInvited && e.snapshot.AccessStatus != AccessReady {
		return accessError(e.snapshot.AccessStatus, ErrEnrollmentNotReady)
	}
	if len(e.snapshot.Lessons) == 0 {
		return ErrLessonOutlineRequired
	}

	updated := cloneSnapshot(e.snapshot)
	at := params.At.UTC()
	if updated.LearnerType == LearnerExternal {
		if params.Duration < 24*time.Hour || params.Duration > 7*24*time.Hour || params.Duration%(24*time.Hour) != 0 {
			return ErrActivationDurationInvalid
		}
		until := at.Add(params.Duration)
		updated.AccessUntil = &until
	} else if params.Duration != 0 {
		return ErrInternalDeadlineForbidden
	}
	updated.AccessStatus = AccessActive
	updated.ProgressStatus = ProgressInProgress
	updated.ActivatedAt = &at
	updated.StartedAt = &at
	updated.LastActivityAt = &at
	seedProgress(&updated, at)
	e.snapshot = updated
	return nil
}

// EvaluateDeadline expires an active external enrollment according to server
// time. Internal due dates never enter this state machine.
func (e *Enrollment) EvaluateDeadline(now time.Time) bool {
	if e == nil || now.IsZero() || e.snapshot.LearnerType != LearnerExternal ||
		e.snapshot.AccessStatus != AccessActive || e.snapshot.AccessUntil == nil ||
		now.Before(*e.snapshot.AccessUntil) {
		return false
	}
	e.snapshot.AccessStatus = AccessExpired
	e.snapshot.LastActivityAt = timePtr(now.UTC())
	return true
}

// ExtendExternalAccess preserves the same enrollment, pinned version, lesson
// progress, and quiz attempts. Active access is extended from its current
// deadline; expired access receives a fresh period from server time.
func (e *Enrollment) ExtendExternalAccess(at time.Time, duration time.Duration) error {
	if e == nil || e.snapshot.LearnerType != LearnerExternal {
		return ErrExternalEnrollmentRequired
	}
	if at.IsZero() || at.Before(e.snapshot.CreatedAt) ||
		(e.snapshot.LastActivityAt != nil && at.Before(*e.snapshot.LastActivityAt)) {
		return ErrTransitionTimeInvalid
	}
	if duration < 24*time.Hour || duration > 7*24*time.Hour || duration%(24*time.Hour) != 0 {
		return ErrActivationDurationInvalid
	}
	if e.snapshot.AccessStatus != AccessActive && e.snapshot.AccessStatus != AccessExpired {
		return accessError(e.snapshot.AccessStatus, ErrEnrollmentCannotExtend)
	}
	at = at.UTC()
	base := at
	if e.snapshot.AccessStatus == AccessActive && e.snapshot.AccessUntil != nil && e.snapshot.AccessUntil.After(base) {
		base = *e.snapshot.AccessUntil
	}
	until := base.Add(duration).UTC()
	e.snapshot.AccessStatus = AccessActive
	e.snapshot.AccessUntil = &until
	e.snapshot.LastActivityAt = &at
	return nil
}

// FreezeForArchive freezes an unfinished enrollment that could otherwise
// start or continue. Restoring a course does not reverse this transition.
func (e *Enrollment) FreezeForArchive(at time.Time) bool {
	if e == nil || at.IsZero() || e.snapshot.ProgressStatus == ProgressCompleted ||
		(e.snapshot.AccessStatus != AccessInvited && e.snapshot.AccessStatus != AccessReady && e.snapshot.AccessStatus != AccessActive) {
		return false
	}
	at = at.UTC()
	e.snapshot.AccessStatus = AccessFrozen
	e.snapshot.FrozenAt = &at
	e.snapshot.LastActivityAt = &at
	return true
}

// ReactivateFrozen explicitly resumes a frozen run. It is not used by course
// restore. The same enrollment and progress are preserved.
func (e *Enrollment) ReactivateFrozen(at, accessUntil time.Time) error {
	if e == nil || e.snapshot.AccessStatus != AccessFrozen {
		return ErrEnrollmentNotFrozen
	}
	if at.IsZero() || !accessUntil.After(at) {
		return ErrReactivationDeadline
	}
	at = at.UTC()
	accessUntil = accessUntil.UTC()
	e.snapshot.AccessStatus = AccessActive
	e.snapshot.AccessUntil = &accessUntil
	e.snapshot.FrozenAt = nil
	e.snapshot.LastActivityAt = &at
	return nil
}

// SuspendForBlock temporarily removes all content access while retaining the
// status that must be restored after unblock.
func (e *Enrollment) SuspendForBlock(at time.Time) bool {
	if e == nil || at.IsZero() || e.snapshot.AccessStatus == AccessSuspended ||
		e.snapshot.AccessStatus == AccessRevoked || e.snapshot.AccessStatus == AccessClosed {
		return false
	}
	at = at.UTC()
	previous := e.snapshot.AccessStatus
	e.snapshot.PreviousAccess = &previous
	e.snapshot.AccessStatus = AccessSuspended
	e.snapshot.SuspendedAt = &at
	e.snapshot.LastActivityAt = &at
	return true
}

// ResumeAfterBlock restores the previous status. For an external active run,
// the deadline is shifted by the exact suspension duration.
func (e *Enrollment) ResumeAfterBlock(at time.Time) error {
	if e == nil || e.snapshot.AccessStatus != AccessSuspended || e.snapshot.SuspendedAt == nil || e.snapshot.PreviousAccess == nil {
		return ErrEnrollmentNotSuspended
	}
	if at.IsZero() || at.Before(*e.snapshot.SuspendedAt) {
		return ErrTransitionTimeInvalid
	}
	at = at.UTC()
	previous := *e.snapshot.PreviousAccess
	if e.snapshot.LearnerType == LearnerExternal && previous == AccessActive && e.snapshot.AccessUntil != nil {
		until := e.snapshot.AccessUntil.Add(at.Sub(*e.snapshot.SuspendedAt))
		e.snapshot.AccessUntil = &until
	}
	e.snapshot.AccessStatus = previous
	e.snapshot.PreviousAccess = nil
	e.snapshot.SuspendedAt = nil
	e.snapshot.LastActivityAt = &at
	return nil
}

// Revoke permanently removes learner access while preserving progress.
func (e *Enrollment) Revoke(at time.Time) bool {
	if e == nil || at.IsZero() || e.snapshot.AccessStatus == AccessRevoked || e.snapshot.AccessStatus == AccessClosed {
		return false
	}
	at = at.UTC()
	e.snapshot.AccessStatus = AccessRevoked
	e.snapshot.PreviousAccess = nil
	e.snapshot.SuspendedAt = nil
	e.snapshot.LastActivityAt = &at
	return true
}

// Close is the irreversible enrollment effect of course deletion.
func (e *Enrollment) Close(at time.Time) bool {
	if e == nil || at.IsZero() || e.snapshot.AccessStatus == AccessClosed {
		return false
	}
	at = at.UTC()
	e.snapshot.AccessStatus = AccessClosed
	e.snapshot.PreviousAccess = nil
	e.snapshot.SuspendedAt = nil
	e.snapshot.LastActivityAt = &at
	return true
}

func validateSnapshot(snapshot Snapshot) error {
	switch {
	case snapshot.ID == "":
		return ErrEnrollmentIDRequired
	case snapshot.CompanyID == "":
		return ErrCompanyRequired
	case snapshot.CourseID == "":
		return ErrCourseRequired
	case snapshot.CourseVersionID == "":
		return ErrCourseVersionRequired
	case snapshot.AttemptNumber < 1:
		return ErrEnrollmentAttemptInvalid
	case snapshot.CreatedAt.IsZero():
		return ErrCreatedAtRequired
	}
	if err := validateIdentity(snapshot); err != nil {
		return err
	}
	if !validSource(snapshot.SourceType) {
		return ErrUnknownSourceType
	}
	if !validProgress(snapshot.ProgressStatus) {
		return ErrUnknownProgressStatus
	}
	if !validAccess(snapshot.AccessStatus) {
		return ErrUnknownAccessStatus
	}
	if snapshot.LearnerType == LearnerUser && snapshot.AccessUntil != nil {
		return ErrInternalDeadlineForbidden
	}
	if snapshot.LearnerType == LearnerExternal && snapshot.AccessStatus == AccessActive {
		if snapshot.ActivatedAt == nil || snapshot.AccessUntil == nil {
			return ErrExternalDeadlineRequired
		}
		if !snapshot.AccessUntil.After(*snapshot.ActivatedAt) {
			return ErrInvalidDeadline
		}
	}
	if err := validateOutlineAndProgress(snapshot); err != nil {
		return err
	}
	return validateAttempts(snapshot)
}

func validateIdentity(snapshot Snapshot) error {
	switch snapshot.LearnerType {
	case LearnerUser:
		if snapshot.UserID == nil || *snapshot.UserID == "" {
			return ErrLearnerIdentityRequired
		}
		if snapshot.ExternalID != nil {
			return ErrLearnerIdentityConflict
		}
	case LearnerExternal:
		if snapshot.ExternalID == nil || *snapshot.ExternalID == "" {
			return ErrLearnerIdentityRequired
		}
		if snapshot.UserID != nil {
			return ErrLearnerIdentityConflict
		}
	default:
		return ErrUnknownLearnerType
	}
	return nil
}

func validSource(value SourceType) bool {
	switch value {
	case SourceAssignment, SourcePersonalAccess, SourcePartnerPromoCampaign,
		SourceCompanyCandidateCampaign, SourceRepeatTraining, SourceLegacy,
		SourceSelfEnrollment:
		return true
	default:
		return false
	}
}

func validProgress(value ProgressStatus) bool {
	return value == ProgressNotStarted || value == ProgressInProgress || value == ProgressCompleted
}

func validAccess(value AccessStatus) bool {
	switch value {
	case AccessInvited, AccessReady, AccessActive, AccessExpired, AccessFrozen,
		AccessSuspended, AccessRevoked, AccessClosed:
		return true
	default:
		return false
	}
}

func accessError(status AccessStatus, fallback error) error {
	switch status {
	case AccessRevoked:
		return ErrEnrollmentRevoked
	case AccessClosed:
		return ErrEnrollmentClosed
	default:
		return fallback
	}
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	result.UserID = clonePtr(value.UserID)
	result.ExternalID = clonePtr(value.ExternalID)
	result.SourceID = clonePtr(value.SourceID)
	result.PreviousAccess = clonePtr(value.PreviousAccess)
	result.Lessons = append([]LessonSpec(nil), value.Lessons...)
	for i := range result.Lessons {
		result.Lessons[i].QuizID = clonePtr(value.Lessons[i].QuizID)
	}
	result.LessonProgress = append([]LessonProgress(nil), value.LessonProgress...)
	for i := range result.LessonProgress {
		result.LessonProgress[i].FirstOpenedAt = clonePtr(value.LessonProgress[i].FirstOpenedAt)
		result.LessonProgress[i].CompletedAt = clonePtr(value.LessonProgress[i].CompletedAt)
		result.LessonProgress[i].LastPosition = clonePtr(value.LessonProgress[i].LastPosition)
	}
	result.QuizAttempts = append([]QuizAttempt(nil), value.QuizAttempts...)
	result.ActivatedAt = clonePtr(value.ActivatedAt)
	result.AccessUntil = clonePtr(value.AccessUntil)
	result.StartedAt = clonePtr(value.StartedAt)
	result.CompletedAt = clonePtr(value.CompletedAt)
	result.LastActivityAt = clonePtr(value.LastActivityAt)
	result.FrozenAt = clonePtr(value.FrozenAt)
	result.SuspendedAt = clonePtr(value.SuspendedAt)
	return result
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func timePtr(value time.Time) *time.Time {
	return &value
}
