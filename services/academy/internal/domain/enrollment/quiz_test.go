package enrollment

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSubmitQuizAtomicDecision(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, true, lessonsWithQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	maxAttempts := 2

	failed, err := enrollment.SubmitQuiz(QuizSubmission{
		AttemptID: "attempt-1", LessonID: "lesson-1", QuizVersionID: "quiz-1",
		Score: 59, PassingScore: 60, MaxAttempts: &maxAttempts, At: testNow.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Decision != QuizFailed || failed.AttemptNumber != 1 || failed.CompletedLessonID != nil || failed.UnlockedLessonID != nil {
		t.Fatalf("failed outcome = %#v", failed)
	}
	if status, _ := enrollment.LessonStatus("lesson-2"); status != LessonViewLocked {
		t.Fatalf("failed quiz unlocked next lesson: %s", status)
	}

	passed, err := enrollment.SubmitQuiz(QuizSubmission{
		AttemptID: "attempt-2", LessonID: "lesson-1", QuizVersionID: "quiz-1",
		Score: 60, PassingScore: 60, MaxAttempts: &maxAttempts, At: testNow.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if passed.Decision != QuizPassed || passed.AttemptNumber != 2 || passed.CompletedLessonID == nil ||
		*passed.CompletedLessonID != "lesson-1" || passed.UnlockedLessonID == nil || *passed.UnlockedLessonID != "lesson-2" ||
		passed.EnrollmentComplete {
		t.Fatalf("passed outcome = %#v", passed)
	}
	snapshot := enrollment.Snapshot()
	if len(snapshot.QuizAttempts) != 2 || snapshot.QuizAttempts[0].Passed || !snapshot.QuizAttempts[1].Passed {
		t.Fatalf("stored attempts = %#v", snapshot.QuizAttempts)
	}
}

func TestSubmitQuizValidationsAreAtomic(t *testing.T) {
	t.Parallel()

	valid := QuizSubmission{
		AttemptID: "attempt-1", LessonID: "lesson-1", QuizVersionID: "quiz-1",
		Score: 80, PassingScore: 60, At: testNow.Add(time.Minute),
	}
	tests := []struct {
		name   string
		mutate func(*QuizSubmission)
		want   error
	}{
		{name: "attempt id", mutate: func(v *QuizSubmission) { v.AttemptID = "" }, want: ErrQuizAttemptIDRequired},
		{name: "quiz belongs to lesson", mutate: func(v *QuizSubmission) { v.QuizVersionID = "other" }, want: ErrQuizMismatch},
		{name: "valid score", mutate: func(v *QuizSubmission) { v.Score = 101 }, want: ErrScoreInvalid},
		{name: "valid passing score", mutate: func(v *QuizSubmission) { v.PassingScore = -1 }, want: ErrPassingScoreInvalid},
		{name: "positive max attempts", mutate: func(v *QuizSubmission) { v.MaxAttempts = intPtr(0) }, want: ErrMaxAttemptsInvalid},
		{name: "locked future lesson", mutate: func(v *QuizSubmission) {
			v.LessonID = "lesson-2"
			v.QuizVersionID = "quiz-2"
		}, want: ErrLessonHasNoQuiz},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, LearnerUser, true, lessonsWithQuiz())
			if err := enrollment.Activate(Activation{At: testNow}); err != nil {
				t.Fatal(err)
			}
			before := enrollment.Snapshot()
			params := valid
			test.mutate(&params)
			_, err := enrollment.SubmitQuiz(params)
			if !errors.Is(err, test.want) {
				t.Fatalf("SubmitQuiz() = %v, want %v", err, test.want)
			}
			if !reflect.DeepEqual(enrollment.Snapshot(), before) {
				t.Fatal("failed submission changed aggregate")
			}
		})
	}
}

func TestQuizAttemptLimitAndIdempotencyConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attemptID ID
		max       int
		want      error
	}{
		{name: "same command id is duplicate", attemptID: "attempt-1", max: 2, want: ErrQuizAttemptDuplicate},
		{name: "new command exceeds limit", attemptID: "attempt-2", max: 1, want: ErrQuizAttemptLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, LearnerUser, true, lessonsWithQuiz())
			if err := enrollment.Activate(Activation{At: testNow}); err != nil {
				t.Fatal(err)
			}
			max := 1
			if _, err := enrollment.SubmitQuiz(QuizSubmission{
				AttemptID: "attempt-1", LessonID: "lesson-1", QuizVersionID: "quiz-1",
				Score: 10, PassingScore: 60, MaxAttempts: &max, At: testNow.Add(time.Minute),
			}); err != nil {
				t.Fatal(err)
			}
			before := enrollment.Snapshot()
			_, err := enrollment.SubmitQuiz(QuizSubmission{
				AttemptID: test.attemptID, LessonID: "lesson-1", QuizVersionID: "quiz-1",
				Score: 10, PassingScore: 60, MaxAttempts: &test.max, At: testNow.Add(2 * time.Minute),
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("SubmitQuiz() = %v, want %v", err, test.want)
			}
			if !reflect.DeepEqual(enrollment.Snapshot(), before) {
				t.Fatal("rejected attempt changed aggregate")
			}
		})
	}
}

func TestPendingReviewBlocksNextAttemptAndReviewUnlocks(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, true, lessonsWithQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	pending, err := enrollment.SubmitQuiz(QuizSubmission{
		AttemptID: "attempt-1", LessonID: "lesson-1", QuizVersionID: "quiz-1",
		Score: 100, PassingScore: 60, PendingReview: true, At: testNow.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Decision != QuizPendingReview || pending.CompletedLessonID != nil {
		t.Fatalf("pending outcome = %#v", pending)
	}
	if _, err := enrollment.SubmitQuiz(QuizSubmission{
		AttemptID: "attempt-2", LessonID: "lesson-1", QuizVersionID: "quiz-1",
		Score: 100, PassingScore: 60, At: testNow.Add(2 * time.Minute),
	}); !errors.Is(err, ErrQuizPendingReview) {
		t.Fatalf("second attempt while pending = %v", err)
	}

	reviewed, err := enrollment.ReviewAttempt(Review{
		AttemptID: "attempt-1", ActorID: "reviewer-1", Passed: true, At: testNow.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Decision != QuizPassed || reviewed.UnlockedLessonID == nil || *reviewed.UnlockedLessonID != "lesson-2" {
		t.Fatalf("review outcome = %#v", reviewed)
	}
	attempt := enrollment.Snapshot().QuizAttempts[0]
	if attempt.PendingReview || !attempt.Passed {
		t.Fatalf("reviewed attempt = %#v", attempt)
	}
}

func TestQuizRejectedAfterExternalDeadlineWithoutPartialMutation(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerExternal, true, lessonsWithQuiz())
	if err := enrollment.Activate(Activation{At: testNow, Duration: 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	before := enrollment.Snapshot()
	_, err := enrollment.SubmitQuiz(QuizSubmission{
		AttemptID: "attempt-1", LessonID: "lesson-1", QuizVersionID: "quiz-1",
		Score: 100, PassingScore: 60, At: testNow.Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrFutureContentUnavailable) {
		t.Fatalf("deadline SubmitQuiz() = %v, want future content unavailable", err)
	}
	if !reflect.DeepEqual(enrollment.Snapshot(), before) {
		t.Fatal("rejected deadline submission partially mutated aggregate")
	}
}

func intPtr(value int) *int { return &value }
