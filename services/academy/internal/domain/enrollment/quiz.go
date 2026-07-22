package enrollment

import (
	"errors"
	"time"
)

// QuizDecision is the state of one immutable attempt. A pending attempt must
// be reviewed before it can complete the lesson.
type QuizDecision string

const (
	QuizFailed        QuizDecision = "failed"
	QuizPassed        QuizDecision = "passed"
	QuizPendingReview QuizDecision = "pending_review"
)

var (
	ErrQuizAttemptIDRequired = errors.New("Для попытки требуется идентификатор")
	ErrQuizIDRequired        = errors.New("Для попытки требуется тест")
	ErrQuizMismatch          = errors.New("Тест не принадлежит уроку")
	ErrQuizAttemptDuplicate  = errors.New("Идентификатор попытки уже использован")
	ErrQuizAttemptLimit      = errors.New("Число попыток исчерпано")
	ErrQuizPendingReview     = errors.New("Предыдущая попытка ожидает проверки")
	ErrMaxAttemptsInvalid    = errors.New("Число попыток должно быть не меньше одной")
	ErrScoreInvalid          = errors.New("Результат теста должен быть от 0 до 100")
	ErrPassingScoreInvalid   = errors.New("Проходной балл должен быть от 0 до 100")
	ErrAttemptTimeRequired   = errors.New("Для попытки требуется время сервера")
	ErrAttemptNumberInvalid  = errors.New("Некорректный номер попытки теста")
	ErrAttemptDecision       = errors.New("Некорректное решение по попытке")
	ErrAttemptNotFound       = errors.New("Попытка не найдена")
	ErrAttemptNotPending     = errors.New("Попытка не ожидает проверки")
	ErrReviewerRequired      = errors.New("Для проверки попытки требуется пользователь")
)

// QuizSubmission contains the trusted result of answer evaluation. Scoring
// closed questions happens before this method; this state machine owns limits,
// passing threshold, pending review, lesson completion, and unlock.
type QuizSubmission struct {
	AttemptID     ID
	LessonID      ID
	QuizVersionID ID
	Score         int
	PassingScore  int
	MaxAttempts   *int
	PendingReview bool
	At            time.Time
}

// QuizOutcome is persisted together with the changed enrollment snapshot in
// one storage transaction by the application layer.
type QuizOutcome struct {
	AttemptNumber      int
	Decision           QuizDecision
	CompletedLessonID  *ID
	UnlockedLessonID   *ID
	EnrollmentComplete bool
}

// Review resolves a pending open-answer attempt.
type Review struct {
	AttemptID ID
	ActorID   ID
	Passed    bool
	Comment   *string
	At        time.Time
}

// SubmitQuiz makes the complete atomic domain decision on a cloned snapshot;
// every validation failure leaves the enrollment unchanged.
func (e *Enrollment) SubmitQuiz(params QuizSubmission) (QuizOutcome, error) {
	if params.AttemptID == "" {
		return QuizOutcome{}, ErrQuizAttemptIDRequired
	}
	if params.QuizVersionID == "" {
		return QuizOutcome{}, ErrQuizIDRequired
	}
	if params.At.IsZero() {
		return QuizOutcome{}, ErrAttemptTimeRequired
	}
	if params.Score < 0 || params.Score > 100 {
		return QuizOutcome{}, ErrScoreInvalid
	}
	if params.PassingScore < 0 || params.PassingScore > 100 {
		return QuizOutcome{}, ErrPassingScoreInvalid
	}
	if params.MaxAttempts != nil && *params.MaxAttempts < 1 {
		return QuizOutcome{}, ErrMaxAttemptsInvalid
	}
	lesson, ok := lessonByID(e.snapshot.Lessons, params.LessonID)
	if !ok {
		return QuizOutcome{}, ErrUnknownLesson
	}
	if lesson.QuizID == nil {
		return QuizOutcome{}, ErrLessonHasNoQuiz
	}
	if *lesson.QuizID != params.QuizVersionID {
		return QuizOutcome{}, ErrQuizMismatch
	}
	updated := cloneSnapshot(e.snapshot)
	temporary := &Enrollment{snapshot: updated}
	if err := temporary.CanViewLesson(params.LessonID, params.At); err != nil {
		return QuizOutcome{}, err
	}
	if e.snapshot.ProgressStatus == ProgressCompleted {
		return QuizOutcome{}, ErrEnrollmentAlreadyCompleted
	}
	for _, attempt := range e.snapshot.QuizAttempts {
		if attempt.ID == params.AttemptID {
			return QuizOutcome{}, ErrQuizAttemptDuplicate
		}
		if attempt.QuizVersionID == params.QuizVersionID && attempt.PendingReview {
			return QuizOutcome{}, ErrQuizPendingReview
		}
	}
	used := attemptsForQuiz(e.snapshot.QuizAttempts, params.QuizVersionID)
	if params.MaxAttempts != nil && used >= *params.MaxAttempts {
		return QuizOutcome{}, ErrQuizAttemptLimit
	}

	at := params.At.UTC()
	decision := QuizFailed
	passed := params.Score >= params.PassingScore
	if params.PendingReview {
		decision = QuizPendingReview
		passed = false
	} else if passed {
		decision = QuizPassed
	}
	updated.QuizAttempts = append(updated.QuizAttempts, QuizAttempt{
		ID:            params.AttemptID,
		QuizVersionID: params.QuizVersionID,
		LessonID:      params.LessonID,
		Number:        used + 1,
		Score:         params.Score,
		Passed:        passed,
		PendingReview: params.PendingReview,
		CreatedAt:     at,
	})
	updated.LastActivityAt = &at
	outcome := QuizOutcome{AttemptNumber: used + 1, Decision: decision}
	if passed {
		next, complete, err := completeLesson(&updated, params.LessonID, at)
		if err != nil {
			return QuizOutcome{}, err
		}
		lessonID := params.LessonID
		outcome.CompletedLessonID = &lessonID
		outcome.EnrollmentComplete = complete
		if next != "" {
			outcome.UnlockedLessonID = &next
		}
	}
	e.snapshot = updated
	return outcome, nil
}

// ReviewAttempt resolves an open-answer attempt. A successful review performs
// the same lesson/course transition as an immediately passing closed quiz.
func (e *Enrollment) ReviewAttempt(params Review) (QuizOutcome, error) {
	if params.ActorID == "" {
		return QuizOutcome{}, ErrReviewerRequired
	}
	if params.At.IsZero() {
		return QuizOutcome{}, ErrAttemptTimeRequired
	}
	updated := cloneSnapshot(e.snapshot)
	temporary := &Enrollment{snapshot: updated}
	if err := requireForwardAccess(temporary, params.At); err != nil {
		return QuizOutcome{}, err
	}
	index := attemptIndex(updated.QuizAttempts, params.AttemptID)
	if index < 0 {
		return QuizOutcome{}, ErrAttemptNotFound
	}
	if !updated.QuizAttempts[index].PendingReview {
		return QuizOutcome{}, ErrAttemptNotPending
	}
	attempt := &updated.QuizAttempts[index]
	attempt.PendingReview = false
	attempt.Passed = params.Passed
	at := params.At.UTC()
	updated.LastActivityAt = &at
	outcome := QuizOutcome{AttemptNumber: attempt.Number, Decision: QuizFailed}
	if params.Passed {
		outcome.Decision = QuizPassed
		next, complete, err := completeLesson(&updated, attempt.LessonID, at)
		if err != nil {
			return QuizOutcome{}, err
		}
		lessonID := attempt.LessonID
		outcome.CompletedLessonID = &lessonID
		outcome.EnrollmentComplete = complete
		if next != "" {
			outcome.UnlockedLessonID = &next
		}
	}
	e.snapshot = updated
	return outcome, nil
}

func requireForwardAccess(e *Enrollment, at time.Time) error {
	if e == nil {
		return ErrContentUnavailable
	}
	e.EvaluateDeadline(at)
	if e.snapshot.AccessStatus != AccessActive {
		return accessError(e.snapshot.AccessStatus, ErrContentUnavailable)
	}
	return nil
}

func validateAttempts(snapshot Snapshot) error {
	ids := make(map[ID]struct{}, len(snapshot.QuizAttempts))
	numbers := make(map[ID]map[int]struct{})
	for _, attempt := range snapshot.QuizAttempts {
		if attempt.ID == "" {
			return ErrQuizAttemptIDRequired
		}
		if _, duplicate := ids[attempt.ID]; duplicate {
			return ErrQuizAttemptDuplicate
		}
		ids[attempt.ID] = struct{}{}
		lesson, ok := lessonByID(snapshot.Lessons, attempt.LessonID)
		if !ok {
			return ErrUnknownLesson
		}
		if lesson.QuizID == nil || *lesson.QuizID != attempt.QuizVersionID {
			return ErrQuizMismatch
		}
		if attempt.Number < 1 {
			return ErrAttemptNumberInvalid
		}
		if attempt.Score < 0 || attempt.Score > 100 || attempt.CreatedAt.IsZero() {
			return ErrAttemptDecision
		}
		if attempt.Passed && attempt.PendingReview {
			return ErrAttemptDecision
		}
		if numbers[attempt.QuizVersionID] == nil {
			numbers[attempt.QuizVersionID] = make(map[int]struct{})
		}
		if _, duplicate := numbers[attempt.QuizVersionID][attempt.Number]; duplicate {
			return ErrAttemptNumberInvalid
		}
		numbers[attempt.QuizVersionID][attempt.Number] = struct{}{}
	}
	for _, quizNumbers := range numbers {
		for number := 1; number <= len(quizNumbers); number++ {
			if _, ok := quizNumbers[number]; !ok {
				return ErrAttemptNumberInvalid
			}
		}
	}
	return nil
}

func attemptsForQuiz(attempts []QuizAttempt, quizID ID) int {
	count := 0
	for _, attempt := range attempts {
		if attempt.QuizVersionID == quizID {
			count++
		}
	}
	return count
}

func attemptIndex(attempts []QuizAttempt, attemptID ID) int {
	for index := range attempts {
		if attempts[index].ID == attemptID {
			return index
		}
	}
	return -1
}
