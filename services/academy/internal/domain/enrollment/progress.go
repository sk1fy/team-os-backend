package enrollment

import (
	"errors"
	"time"
)

var (
	ErrLessonOutlineRequired      = errors.New("В версии курса нет уроков")
	ErrLessonIDRequired           = errors.New("Для урока требуется идентификатор")
	ErrDuplicateLessonID          = errors.New("Урок повторяется в версии курса")
	ErrDuplicateQuizID            = errors.New("Тест повторяется в версии курса")
	ErrUnknownLesson              = errors.New("Урок не принадлежит версии прохождения")
	ErrUnknownLessonStatus        = errors.New("Неизвестное состояние урока")
	ErrDuplicateLessonProgress    = errors.New("Прогресс урока повторяется")
	ErrLessonProgressLocked       = errors.New("Урок ещё не открыт")
	ErrLessonAlreadyCompleted     = errors.New("Урок уже завершён")
	ErrQuizRequired               = errors.New("Урок можно завершить только после успешного теста")
	ErrLessonHasNoQuiz            = errors.New("У урока нет теста")
	ErrContentUnavailable         = errors.New("Содержимое урока недоступно")
	ErrFutureContentUnavailable   = errors.New("Будущий урок ещё не открыт")
	ErrProgressTimeRequired       = errors.New("Для изменения прогресса требуется время сервера")
	ErrActiveSecondsInvalid       = errors.New("Активное время урока не может быть отрицательным")
	ErrProgressStatusMismatch     = errors.New("Состояние прохождения не соответствует прогрессу уроков")
	ErrCurrentLessonConflict      = errors.New("У прохождения может быть только один текущий урок")
	ErrSequentialProgressInvalid  = errors.New("Последовательный прогресс не может пропускать уроки")
	ErrCompletedAtRequired        = errors.New("Для завершённого урока требуется дата")
	ErrCompletedAtForbidden       = errors.New("У незавершённого урока не может быть даты завершения")
	ErrEnrollmentAlreadyCompleted = errors.New("Прохождение уже завершено")
)

// ResumeDecision is calculated by the server from stored enrollment state.
// A nil lesson means no forward navigation is allowed or the course is done.
type ResumeDecision struct {
	State           LearnerState
	LessonVersionID *ID
	ProgressPercent int
}

// LessonViewStatus includes the derived locked state used in outline DTOs.
type LessonViewStatus string

const (
	LessonViewLocked    LessonViewStatus = "locked"
	LessonViewAvailable LessonViewStatus = "available"
	LessonViewCurrent   LessonViewStatus = "current"
	LessonViewCompleted LessonViewStatus = "completed"
)

// Resume resolves both deadline and current lesson from server state. The
// client never chooses the next lesson itself.
func (e *Enrollment) Resume(now time.Time) ResumeDecision {
	if e == nil {
		return ResumeDecision{State: StateClosed}
	}
	e.EvaluateDeadline(now)
	result := ResumeDecision{State: e.State(), ProgressPercent: e.ProgressPercent()}
	if result.State != StateActive {
		return result
	}
	for _, lesson := range e.snapshot.Lessons {
		progress, ok := progressByLesson(e.snapshot.LessonProgress, lesson.ID)
		if ok && progress.Status == LessonCurrent {
			id := lesson.ID
			result.LessonVersionID = &id
			return result
		}
	}
	for _, lesson := range e.snapshot.Lessons {
		progress, ok := progressByLesson(e.snapshot.LessonProgress, lesson.ID)
		if ok && progress.Status != LessonCompleted {
			id := lesson.ID
			result.LessonVersionID = &id
			return result
		}
	}
	return result
}

// LessonStatus returns an outline-safe derived state. Sequential lessons with
// no stored progress row are locked.
func (e *Enrollment) LessonStatus(lessonID ID) (LessonViewStatus, error) {
	if e == nil || !hasLesson(e.snapshot.Lessons, lessonID) {
		return "", ErrUnknownLesson
	}
	progress, ok := progressByLesson(e.snapshot.LessonProgress, lessonID)
	if !ok {
		return LessonViewLocked, nil
	}
	switch progress.Status {
	case LessonAvailable:
		return LessonViewAvailable, nil
	case LessonCurrent:
		return LessonViewCurrent, nil
	case LessonCompleted:
		return LessonViewCompleted, nil
	default:
		return "", ErrUnknownLessonStatus
	}
}

// CanViewLesson enforces the absence of future content in restricted states.
// Frozen and expired learners retain only already completed lessons; a block
// (suspended) exposes no lesson content at all.
func (e *Enrollment) CanViewLesson(lessonID ID, now time.Time) error {
	if e == nil || !hasLesson(e.snapshot.Lessons, lessonID) {
		return ErrUnknownLesson
	}
	e.EvaluateDeadline(now)
	progress, unlocked := progressByLesson(e.snapshot.LessonProgress, lessonID)
	switch e.snapshot.AccessStatus {
	case AccessActive:
		if !unlocked {
			return ErrFutureContentUnavailable
		}
		return nil
	case AccessExpired, AccessFrozen:
		if unlocked && progress.Status == LessonCompleted {
			return nil
		}
		return ErrFutureContentUnavailable
	default:
		return accessError(e.snapshot.AccessStatus, ErrContentUnavailable)
	}
}

// OpenLesson stores the first-open time and makes an available lesson current
// when no other current lesson exists.
func (e *Enrollment) OpenLesson(lessonID ID, at time.Time) error {
	if at.IsZero() {
		return ErrProgressTimeRequired
	}
	if err := e.CanViewLesson(lessonID, at); err != nil {
		return err
	}
	updated := cloneSnapshot(e.snapshot)
	index := progressIndex(updated.LessonProgress, lessonID)
	if index < 0 {
		return ErrLessonProgressLocked
	}
	at = at.UTC()
	if updated.LessonProgress[index].FirstOpenedAt == nil {
		updated.LessonProgress[index].FirstOpenedAt = &at
	}
	if updated.LessonProgress[index].Status == LessonAvailable && currentProgressIndex(updated.LessonProgress) < 0 {
		updated.LessonProgress[index].Status = LessonCurrent
	}
	updated.LastActivityAt = &at
	e.snapshot = updated
	return nil
}

// RecordPosition persists the server-side resume cursor and accumulates an
// already measured duration. It never accepts negative corrections.
func (e *Enrollment) RecordPosition(lessonID ID, activeSeconds int64, position *string, at time.Time) error {
	if at.IsZero() {
		return ErrProgressTimeRequired
	}
	if activeSeconds < 0 {
		return ErrActiveSecondsInvalid
	}
	if err := e.CanViewLesson(lessonID, at); err != nil {
		return err
	}
	updated := cloneSnapshot(e.snapshot)
	index := progressIndex(updated.LessonProgress, lessonID)
	if index < 0 {
		return ErrLessonProgressLocked
	}
	updated.LessonProgress[index].ActiveSeconds += activeSeconds
	updated.LessonProgress[index].LastPosition = clonePtr(position)
	at = at.UTC()
	updated.LastActivityAt = &at
	e.snapshot = updated
	return nil
}

// CompleteLesson completes a lesson without a quiz. A quiz-backed lesson must
// go through SubmitQuiz so attempt, completion, unlock, and course completion
// form one domain decision.
func (e *Enrollment) CompleteLesson(lessonID ID, at time.Time) (ID, bool, error) {
	if at.IsZero() {
		return "", false, ErrProgressTimeRequired
	}
	lesson, ok := lessonByID(e.snapshot.Lessons, lessonID)
	if !ok {
		return "", false, ErrUnknownLesson
	}
	if lesson.QuizID != nil {
		return "", false, ErrQuizRequired
	}
	if err := e.CanViewLesson(lessonID, at); err != nil {
		return "", false, err
	}
	updated := cloneSnapshot(e.snapshot)
	next, completed, err := completeLesson(&updated, lessonID, at.UTC())
	if err != nil {
		return "", false, err
	}
	e.snapshot = updated
	return next, completed, nil
}

// ProgressPercent uses required lessons of the pinned version only. Lessons
// are required by default and become optional explicitly.
func (e *Enrollment) ProgressPercent() int {
	if e == nil {
		return 0
	}
	required := 0
	completed := 0
	for _, lesson := range e.snapshot.Lessons {
		if lesson.Optional {
			continue
		}
		required++
		progress, ok := progressByLesson(e.snapshot.LessonProgress, lesson.ID)
		if ok && progress.Status == LessonCompleted {
			completed++
		}
	}
	if required == 0 {
		return 100
	}
	return completed * 100 / required
}

func seedProgress(snapshot *Snapshot, at time.Time) {
	if snapshot.Sequential {
		snapshot.LessonProgress = []LessonProgress{{
			LessonVersionID: snapshot.Lessons[0].ID,
			Status:          LessonCurrent,
			FirstOpenedAt:   timePtr(at),
		}}
		return
	}
	snapshot.LessonProgress = make([]LessonProgress, 0, len(snapshot.Lessons))
	for index, lesson := range snapshot.Lessons {
		status := LessonAvailable
		var openedAt *time.Time
		if index == 0 {
			status = LessonCurrent
			openedAt = timePtr(at)
		}
		snapshot.LessonProgress = append(snapshot.LessonProgress, LessonProgress{
			LessonVersionID: lesson.ID,
			Status:          status,
			FirstOpenedAt:   openedAt,
		})
	}
}

func completeLesson(snapshot *Snapshot, lessonID ID, at time.Time) (ID, bool, error) {
	if snapshot.ProgressStatus == ProgressCompleted {
		return "", false, ErrEnrollmentAlreadyCompleted
	}
	index := progressIndex(snapshot.LessonProgress, lessonID)
	if index < 0 {
		return "", false, ErrLessonProgressLocked
	}
	if snapshot.LessonProgress[index].Status == LessonCompleted {
		return "", false, ErrLessonAlreadyCompleted
	}
	snapshot.LessonProgress[index].Status = LessonCompleted
	snapshot.LessonProgress[index].CompletedAt = timePtr(at)
	if snapshot.LessonProgress[index].FirstOpenedAt == nil {
		snapshot.LessonProgress[index].FirstOpenedAt = timePtr(at)
	}
	snapshot.LastActivityAt = timePtr(at)

	if requiredLessonsComplete(*snapshot) {
		snapshot.ProgressStatus = ProgressCompleted
		snapshot.CompletedAt = timePtr(at)
		return "", true, nil
	}

	currentIndex := currentProgressIndex(snapshot.LessonProgress)
	if currentIndex < 0 {
		for lessonIndex, lesson := range snapshot.Lessons {
			progressIndex := progressIndex(snapshot.LessonProgress, lesson.ID)
			if progressIndex >= 0 && snapshot.LessonProgress[progressIndex].Status != LessonCompleted {
				snapshot.LessonProgress[progressIndex].Status = LessonCurrent
				return lesson.ID, false, nil
			}
			if snapshot.Sequential && progressIndex < 0 {
				snapshot.LessonProgress = append(snapshot.LessonProgress, LessonProgress{
					LessonVersionID: snapshot.Lessons[lessonIndex].ID,
					Status:          LessonCurrent,
				})
				return snapshot.Lessons[lessonIndex].ID, false, nil
			}
		}
	}
	return "", false, nil
}

func requiredLessonsComplete(snapshot Snapshot) bool {
	for _, lesson := range snapshot.Lessons {
		if lesson.Optional {
			continue
		}
		progress, ok := progressByLesson(snapshot.LessonProgress, lesson.ID)
		if !ok || progress.Status != LessonCompleted {
			return false
		}
	}
	return true
}

func validateOutlineAndProgress(snapshot Snapshot) error {
	if len(snapshot.Lessons) == 0 {
		return ErrLessonOutlineRequired
	}
	lessons := make(map[ID]struct{}, len(snapshot.Lessons))
	quizzes := make(map[ID]struct{})
	for _, lesson := range snapshot.Lessons {
		if lesson.ID == "" {
			return ErrLessonIDRequired
		}
		if _, duplicate := lessons[lesson.ID]; duplicate {
			return ErrDuplicateLessonID
		}
		lessons[lesson.ID] = struct{}{}
		if lesson.QuizID != nil {
			if *lesson.QuizID == "" {
				return ErrLessonHasNoQuiz
			}
			if _, duplicate := quizzes[*lesson.QuizID]; duplicate {
				return ErrDuplicateQuizID
			}
			quizzes[*lesson.QuizID] = struct{}{}
		}
	}

	progressLessons := make(map[ID]struct{}, len(snapshot.LessonProgress))
	current := 0
	lastOutlineIndex := -1
	for _, progress := range snapshot.LessonProgress {
		if _, ok := lessons[progress.LessonVersionID]; !ok {
			return ErrUnknownLesson
		}
		if _, duplicate := progressLessons[progress.LessonVersionID]; duplicate {
			return ErrDuplicateLessonProgress
		}
		progressLessons[progress.LessonVersionID] = struct{}{}
		switch progress.Status {
		case LessonAvailable, LessonCurrent:
			if progress.CompletedAt != nil {
				return ErrCompletedAtForbidden
			}
		case LessonCompleted:
			if progress.CompletedAt == nil {
				return ErrCompletedAtRequired
			}
		default:
			return ErrUnknownLessonStatus
		}
		if progress.Status == LessonCurrent {
			current++
		}
		if progress.ActiveSeconds < 0 {
			return ErrActiveSecondsInvalid
		}
		outlineIndex := lessonIndex(snapshot.Lessons, progress.LessonVersionID)
		if snapshot.Sequential && outlineIndex != lastOutlineIndex+1 {
			return ErrSequentialProgressInvalid
		}
		lastOutlineIndex = outlineIndex
	}
	if current > 1 {
		return ErrCurrentLessonConflict
	}
	if snapshot.ProgressStatus == ProgressNotStarted && len(snapshot.LessonProgress) != 0 {
		return ErrProgressStatusMismatch
	}
	if snapshot.ProgressStatus == ProgressCompleted && !requiredLessonsComplete(snapshot) {
		return ErrProgressStatusMismatch
	}
	return nil
}

func hasLesson(lessons []LessonSpec, lessonID ID) bool {
	_, ok := lessonByID(lessons, lessonID)
	return ok
}

func lessonByID(lessons []LessonSpec, lessonID ID) (LessonSpec, bool) {
	for _, lesson := range lessons {
		if lesson.ID == lessonID {
			return lesson, true
		}
	}
	return LessonSpec{}, false
}

func lessonIndex(lessons []LessonSpec, lessonID ID) int {
	for index, lesson := range lessons {
		if lesson.ID == lessonID {
			return index
		}
	}
	return -1
}

func progressByLesson(progress []LessonProgress, lessonID ID) (LessonProgress, bool) {
	index := progressIndex(progress, lessonID)
	if index < 0 {
		return LessonProgress{}, false
	}
	return progress[index], true
}

func progressIndex(progress []LessonProgress, lessonID ID) int {
	for index := range progress {
		if progress[index].LessonVersionID == lessonID {
			return index
		}
	}
	return -1
}

func currentProgressIndex(progress []LessonProgress) int {
	for index := range progress {
		if progress[index].Status == LessonCurrent {
			return index
		}
	}
	return -1
}
