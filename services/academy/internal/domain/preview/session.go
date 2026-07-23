// Package preview models a privileged, ephemeral course run. A preview
// session is deliberately not an enrollment and cannot produce a persistence
// command for real learner progress.
package preview

import (
	"errors"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

// ID is an opaque session, actor, course or lesson identifier.
type ID string

// Mode describes whether an administrator only inspects content or also
// exercises learner interactions. Neither mode persists real progress.
type Mode string

const (
	ModePreview Mode = "preview"
	ModeTest    Mode = "test"
)

var (
	ErrSessionIDRequired          = errors.New("Для предпросмотра требуется идентификатор сессии")
	ErrActorIDRequired            = errors.New("Для предпросмотра требуется пользователь")
	ErrStartedAtRequired          = errors.New("Для предпросмотра требуется дата начала")
	ErrUnknownMode                = errors.New("Неизвестный режим предпросмотра курса")
	ErrPartnerCourseRequired      = errors.New("Предпросмотр доступен только для партнёрского курса")
	ErrPublishedVersionRequired   = errors.New("Для предпросмотра требуется опубликованная версия")
	ErrVersionScopeMismatch       = errors.New("Версия предпросмотра принадлежит другому курсу или компании")
	ErrDeletedCourse              = errors.New("Удалённый курс нельзя предпросмотреть")
	ErrLessonNotFound             = errors.New("Урок не принадлежит версии предпросмотра")
	ErrActivityTimeRequired       = errors.New("Для действия предпросмотра требуется время сервера")
	ErrActivityBeforeSession      = errors.New("Действие предпросмотра не может быть раньше начала сессии")
	ErrRealProgressWriteForbidden = errors.New("Режим предпросмотра не сохраняет реальный прогресс")
)

// Params creates an ephemeral administrative session from a specific
// published version. A blocked course is intentionally accepted for the
// service preview described by the product rules.
type Params struct {
	SessionID ID
	ActorID   ID
	Mode      Mode
	StartedAt time.Time
	Course    course.Course
	Version   courseversion.Snapshot
}

// LessonActivity is session-local and is never mapped to enrollment progress.
type LessonActivity struct {
	LessonID  ID
	ViewedAt  time.Time
	Completed bool
}

// Snapshot contains only ephemeral preview state. In particular, it has no
// enrollment ID, learner identity, source assignment or deadline.
type Snapshot struct {
	SessionID ID
	ActorID   ID
	CompanyID ID
	CourseID  ID
	VersionID ID
	Mode      Mode
	StartedAt time.Time
	Activity  []LessonActivity
}

// Session owns ephemeral navigation state.
type Session struct {
	snapshot Snapshot
	lessons  map[ID]struct{}
}

// New validates the privileged preview target and creates isolated state.
// Authorization (owner/admin) remains an object-level policy decision.
func New(params Params) (*Session, error) {
	switch {
	case params.SessionID == "":
		return nil, ErrSessionIDRequired
	case params.ActorID == "":
		return nil, ErrActorIDRequired
	case params.StartedAt.IsZero():
		return nil, ErrStartedAtRequired
	case params.Mode != ModePreview && params.Mode != ModeTest:
		return nil, ErrUnknownMode
	case params.Course.OwnerType != course.CourseOwnerPartner:
		return nil, ErrPartnerCourseRequired
	case params.Course.LifecycleStatus == course.CourseDeleted:
		return nil, ErrDeletedCourse
	case params.Version.Status != courseversion.StatusPublished:
		return nil, ErrPublishedVersionRequired
	case course.ID(params.Version.CompanyID) != params.Course.CompanyID ||
		course.ID(params.Version.CourseID) != params.Course.ID:
		return nil, ErrVersionScopeMismatch
	}
	if err := params.Course.Validate(); err != nil {
		return nil, err
	}
	if _, err := courseversion.Rehydrate(params.Version); err != nil {
		return nil, err
	}

	lessons := make(map[ID]struct{}, len(params.Version.Definition.Lessons))
	for _, lesson := range params.Version.Definition.Lessons {
		lessons[ID(lesson.ID)] = struct{}{}
	}
	return &Session{
		snapshot: Snapshot{
			SessionID: params.SessionID, ActorID: params.ActorID,
			CompanyID: ID(params.Course.CompanyID), CourseID: ID(params.Course.ID),
			VersionID: ID(params.Version.ID), Mode: params.Mode,
			StartedAt: params.StartedAt.UTC(),
		},
		lessons: lessons,
	}, nil
}

// RecordLesson stores session-local activity only.
func (s *Session) RecordLesson(lessonID ID, completed bool, at time.Time) error {
	if s == nil {
		return ErrSessionIDRequired
	}
	if _, exists := s.lessons[lessonID]; !exists {
		return ErrLessonNotFound
	}
	if at.IsZero() {
		return ErrActivityTimeRequired
	}
	at = at.UTC()
	if at.Before(s.snapshot.StartedAt) {
		return ErrActivityBeforeSession
	}
	for index := range s.snapshot.Activity {
		if s.snapshot.Activity[index].LessonID == lessonID {
			s.snapshot.Activity[index].ViewedAt = at
			s.snapshot.Activity[index].Completed = completed
			return nil
		}
	}
	s.snapshot.Activity = append(s.snapshot.Activity, LessonActivity{
		LessonID: lessonID, ViewedAt: at, Completed: completed,
	})
	return nil
}

// Snapshot returns a defensive copy for the preview response.
func (s *Session) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	result := s.snapshot
	result.Activity = append([]LessonActivity(nil), s.snapshot.Activity...)
	return result
}

// GuardRealProgressWrite is a mandatory boundary before any enrollment write.
// It always rejects preview/test sessions by construction.
func (s *Session) GuardRealProgressWrite() error {
	return ErrRealProgressWriteForbidden
}
