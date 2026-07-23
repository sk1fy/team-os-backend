package enrollment

import (
	"errors"
	"testing"
	"time"
)

func TestSequentialCompleteUnlockAndServerResume(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	if got := enrollment.Resume(testNow); got.LessonVersionID == nil || *got.LessonVersionID != "lesson-1" {
		t.Fatalf("initial Resume() = %#v", got)
	}
	if status, _ := enrollment.LessonStatus("lesson-2"); status != LessonViewLocked {
		t.Fatalf("second lesson = %s, want locked", status)
	}
	if err := enrollment.CanViewLesson("lesson-2", testNow); !errors.Is(err, ErrFutureContentUnavailable) {
		t.Fatalf("CanViewLesson(locked) = %v", err)
	}

	next, completed, err := enrollment.CompleteLesson("lesson-1", testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if next != "lesson-2" || completed {
		t.Fatalf("first completion = next %q, completed %v", next, completed)
	}
	if got := enrollment.ProgressPercent(); got != 50 {
		t.Fatalf("ProgressPercent() = %d, want 50", got)
	}
	if got := enrollment.Resume(testNow.Add(time.Minute)); got.LessonVersionID == nil || *got.LessonVersionID != "lesson-2" {
		t.Fatalf("next Resume() = %#v", got)
	}

	next, completed, err = enrollment.CompleteLesson("lesson-2", testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || !completed || enrollment.State() != StateCompleted || enrollment.ProgressPercent() != 100 {
		t.Fatalf("course completion = next %q, completed %v, state %s, percent %d", next, completed, enrollment.State(), enrollment.ProgressPercent())
	}
	if got := enrollment.Resume(testNow.Add(2 * time.Minute)); got.LessonVersionID != nil || got.State != StateCompleted {
		t.Fatalf("completed Resume() = %#v", got)
	}
}

func TestSelfEnrollmentRehydratesResumesAndCompletes(t *testing.T) {
	t.Parallel()

	snapshot := validPendingSnapshot()
	snapshot.SourceType = SourceSelfEnrollment
	snapshot.SourceID = idPtr(snapshot.CourseID)
	enrollment, err := Rehydrate(snapshot)
	if err != nil {
		t.Fatalf("Rehydrate(self enrollment) = %v", err)
	}
	if err = enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatalf("Activate(self enrollment) = %v", err)
	}
	if got := enrollment.Resume(testNow); got.State != StateActive || got.LessonVersionID == nil || *got.LessonVersionID != "lesson-1" {
		t.Fatalf("Resume(self enrollment) = %#v", got)
	}
	if _, completed, completeErr := enrollment.CompleteLesson("lesson-1", testNow.Add(time.Minute)); completeErr != nil || completed {
		t.Fatalf("CompleteLesson(first) completed=%v error=%v", completed, completeErr)
	}
	if _, completed, completeErr := enrollment.CompleteLesson("lesson-2", testNow.Add(2*time.Minute)); completeErr != nil || !completed {
		t.Fatalf("CompleteLesson(last) completed=%v error=%v", completed, completeErr)
	}
	if got := enrollment.Resume(testNow.Add(2 * time.Minute)); got.State != StateCompleted || got.LessonVersionID != nil {
		t.Fatalf("Resume(completed self enrollment) = %#v", got)
	}
}

func TestNonSequentialUnlocksAllLessons(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, false, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		lesson ID
		want   LessonViewStatus
	}{
		{lesson: "lesson-1", want: LessonViewCurrent},
		{lesson: "lesson-2", want: LessonViewAvailable},
	}
	for _, test := range tests {
		if got, err := enrollment.LessonStatus(test.lesson); err != nil || got != test.want {
			t.Errorf("LessonStatus(%s) = %s, %v; want %s", test.lesson, got, err, test.want)
		}
		if err := enrollment.CanViewLesson(test.lesson, testNow); err != nil {
			t.Errorf("CanViewLesson(%s) = %v", test.lesson, err)
		}
	}
}

func TestRecordPositionIsServerResumeState(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	position := "paragraph:7"
	if err := enrollment.RecordPosition("lesson-1", 35, &position, testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	position = "mutated-by-caller"
	progress := enrollment.Snapshot().LessonProgress[0]
	if progress.ActiveSeconds != 35 || progress.LastPosition == nil || *progress.LastPosition != "paragraph:7" {
		t.Fatalf("stored progress = %#v", progress)
	}
	if err := enrollment.RecordPosition("lesson-1", -1, nil, testNow); !errors.Is(err, ErrActiveSecondsInvalid) {
		t.Fatalf("negative active seconds = %v", err)
	}
}

func TestRestrictedContentPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		transition  func(*Enrollment)
		completedOK bool
		wantState   LearnerState
	}{
		{name: "expired keeps completed", transition: func(e *Enrollment) {
			e.snapshot.AccessStatus = AccessExpired
		}, completedOK: true, wantState: StateExpired},
		{name: "frozen keeps completed", transition: func(e *Enrollment) {
			e.snapshot.AccessStatus = AccessFrozen
		}, completedOK: true, wantState: StateFrozen},
		{name: "suspended hides all", transition: func(e *Enrollment) {
			e.SuspendForBlock(testNow.Add(2 * time.Minute))
		}, wantState: StateSuspended},
		{name: "revoked hides all", transition: func(e *Enrollment) {
			e.Revoke(testNow.Add(2 * time.Minute))
		}, wantState: StateRevoked},
		{name: "closed hides all", transition: func(e *Enrollment) {
			e.Close(testNow.Add(2 * time.Minute))
		}, wantState: StateClosed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, LearnerUser, true, lessonsWithoutQuiz())
			if err := enrollment.Activate(Activation{At: testNow}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := enrollment.CompleteLesson("lesson-1", testNow.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			test.transition(enrollment)
			if enrollment.State() != test.wantState {
				t.Fatalf("State() = %s, want %s", enrollment.State(), test.wantState)
			}
			completedErr := enrollment.CanViewLesson("lesson-1", testNow.Add(3*time.Minute))
			if test.completedOK && completedErr != nil {
				t.Errorf("completed lesson should remain visible: %v", completedErr)
			}
			if !test.completedOK && completedErr == nil {
				t.Error("completed lesson unexpectedly visible")
			}
			if err := enrollment.CanViewLesson("lesson-2", testNow.Add(3*time.Minute)); err == nil {
				t.Error("future lesson unexpectedly visible")
			}
		})
	}
}

func TestExternalDeadlineUsesServerClock(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerExternal, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow, Duration: 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if enrollment.EvaluateDeadline(testNow.Add(24*time.Hour - time.Nanosecond)) {
		t.Fatal("deadline expired too early")
	}
	if !enrollment.EvaluateDeadline(testNow.Add(24 * time.Hour)) {
		t.Fatal("deadline did not expire at boundary")
	}
	if enrollment.State() != StateExpired {
		t.Fatalf("State() = %s, want expired", enrollment.State())
	}
	if got := enrollment.Resume(testNow.Add(25 * time.Hour)); got.LessonVersionID != nil || got.State != StateExpired {
		t.Fatalf("expired Resume() = %#v", got)
	}
}
