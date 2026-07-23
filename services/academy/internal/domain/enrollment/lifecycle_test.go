package enrollment

import (
	"errors"
	"testing"
	"time"
)

func TestArchiveFreezesEveryUnfinishedEnrollment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		learner  LearnerType
		complete bool
		want     bool
	}{
		{name: "external unfinished", learner: LearnerExternal, want: true},
		{name: "internal unfinished", learner: LearnerUser, want: true},
		{name: "completed external stays accessible", learner: LearnerExternal, complete: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, test.learner, true, []LessonSpec{{ID: "lesson-1"}})
			activation := Activation{At: testNow}
			if test.learner == LearnerExternal {
				activation.Duration = 24 * time.Hour
			}
			if err := enrollment.Activate(activation); err != nil {
				t.Fatal(err)
			}
			if test.complete {
				if _, _, err := enrollment.CompleteLesson("lesson-1", testNow.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			if got := enrollment.FreezeForArchive(testNow.Add(2 * time.Minute)); got != test.want {
				t.Fatalf("FreezeForArchive() = %v, want %v", got, test.want)
			}
			if test.want && enrollment.State() != StateFrozen {
				t.Fatalf("State() = %s, want frozen", enrollment.State())
			}
		})
	}
}

func TestCourseRestoreDoesNotReactivateFrozenEnrollment(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerExternal, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow, Duration: 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	enrollment.FreezeForArchive(testNow.Add(time.Hour))

	// There is intentionally no automatic Restore transition on Enrollment.
	if enrollment.State() != StateFrozen {
		t.Fatalf("state changed without explicit decision: %s", enrollment.State())
	}
	newDeadline := testNow.Add(3 * 24 * time.Hour)
	if err := enrollment.ReactivateFrozen(testNow.Add(2*time.Hour), newDeadline); err != nil {
		t.Fatal(err)
	}
	if enrollment.State() != StateActive || !enrollment.Snapshot().AccessUntil.Equal(newDeadline) {
		t.Fatalf("explicit reactivation = %#v", enrollment.Snapshot())
	}
}

func TestBlockPreservesProgressAndPausesExternalDeadline(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerExternal, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow, Duration: 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := enrollment.RecordPosition("lesson-1", 42, nil, testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := enrollment.Snapshot()
	blockedAt := testNow.Add(2 * time.Hour)
	if !enrollment.SuspendForBlock(blockedAt) {
		t.Fatal("SuspendForBlock() = false")
	}
	if err := enrollment.CanViewLesson("lesson-1", blockedAt); err == nil {
		t.Fatal("blocked course exposed content")
	}
	if enrollment.Snapshot().LessonProgress[0].ActiveSeconds != 42 {
		t.Fatal("block discarded progress")
	}

	unblockedAt := blockedAt.Add(6 * time.Hour)
	if err := enrollment.ResumeAfterBlock(unblockedAt); err != nil {
		t.Fatal(err)
	}
	snapshot := enrollment.Snapshot()
	wantDeadline := before.AccessUntil.Add(6 * time.Hour)
	if snapshot.AccessStatus != AccessActive || snapshot.AccessUntil == nil || !snapshot.AccessUntil.Equal(wantDeadline) {
		t.Fatalf("unblocked state/deadline = %s/%v, want active/%v", snapshot.AccessStatus, snapshot.AccessUntil, wantDeadline)
	}
	if snapshot.LessonProgress[0].ActiveSeconds != 42 {
		t.Fatal("unblock discarded progress")
	}
}

func TestBlockRestoresPreviousRestrictedStatusWithoutDeadlineShift(t *testing.T) {
	t.Parallel()

	tests := []AccessStatus{AccessExpired, AccessFrozen}
	for _, status := range tests {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, LearnerExternal, true, lessonsWithoutQuiz())
			if err := enrollment.Activate(Activation{At: testNow, Duration: 24 * time.Hour}); err != nil {
				t.Fatal(err)
			}
			enrollment.snapshot.AccessStatus = status
			deadline := *enrollment.snapshot.AccessUntil
			enrollment.SuspendForBlock(testNow.Add(time.Hour))
			if err := enrollment.ResumeAfterBlock(testNow.Add(5 * time.Hour)); err != nil {
				t.Fatal(err)
			}
			if enrollment.snapshot.AccessStatus != status || !enrollment.snapshot.AccessUntil.Equal(deadline) {
				t.Fatalf("restored %s/%v", enrollment.snapshot.AccessStatus, enrollment.snapshot.AccessUntil)
			}
		})
	}
}

func TestIrreversibleAccessTransitions(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	if !enrollment.Revoke(testNow.Add(time.Minute)) || enrollment.State() != StateRevoked {
		t.Fatalf("revoke = %s", enrollment.State())
	}
	if err := enrollment.Activate(Activation{At: testNow.Add(2 * time.Minute)}); !errors.Is(err, ErrEnrollmentRevoked) {
		t.Fatalf("reactivate revoked = %v", err)
	}
	if !enrollment.Close(testNow.Add(3*time.Minute)) || enrollment.State() != StateClosed {
		t.Fatalf("close = %s", enrollment.State())
	}
	if enrollment.Revoke(testNow.Add(4 * time.Minute)) {
		t.Fatal("closed enrollment changed back to revoked")
	}
}

func TestExtendExternalAccessPreservesProgressAndVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    AccessStatus
		at        time.Time
		duration  time.Duration
		wantUntil time.Time
		wantErr   error
	}{
		{
			name: "active adds to current deadline", status: AccessActive,
			at: testNow.Add(12 * time.Hour), duration: 2 * 24 * time.Hour,
			wantUntil: testNow.Add(3 * 24 * time.Hour),
		},
		{
			name: "expired starts from extension time", status: AccessExpired,
			at: testNow.Add(2 * 24 * time.Hour), duration: 3 * 24 * time.Hour,
			wantUntil: testNow.Add(5 * 24 * time.Hour),
		},
		{name: "partial day denied", status: AccessActive, at: testNow.Add(time.Hour), duration: 36 * time.Hour, wantErr: ErrActivationDurationInvalid},
		{name: "ready denied", status: AccessReady, at: testNow.Add(time.Hour), duration: 24 * time.Hour, wantErr: ErrEnrollmentCannotExtend},
		{name: "revoked denied", status: AccessRevoked, at: testNow.Add(time.Hour), duration: 24 * time.Hour, wantErr: ErrEnrollmentRevoked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, LearnerExternal, true, lessonsWithoutQuiz())
			if err := enrollment.Activate(Activation{At: testNow, Duration: 24 * time.Hour}); err != nil {
				t.Fatal(err)
			}
			if err := enrollment.RecordPosition("lesson-1", 37, nil, testNow.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			enrollment.snapshot.AccessStatus = test.status
			before := enrollment.Snapshot()
			err := enrollment.ExtendExternalAccess(test.at, test.duration)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ExtendExternalAccess() = %v, want %v", err, test.wantErr)
			}
			after := enrollment.Snapshot()
			if test.wantErr != nil {
				if after.AccessStatus != before.AccessStatus || !timesEqual(after.AccessUntil, before.AccessUntil) ||
					after.LessonProgress[0].ActiveSeconds != before.LessonProgress[0].ActiveSeconds {
					t.Fatal("failed extension mutated enrollment")
				}
				return
			}
			if after.AccessStatus != AccessActive || after.AccessUntil == nil || !after.AccessUntil.Equal(test.wantUntil) ||
				after.CourseVersionID != before.CourseVersionID || after.LessonProgress[0].ActiveSeconds != 37 {
				t.Fatalf("extended snapshot = %#v", after)
			}
		})
	}
}

func TestInternalEnrollmentCannotBeExtendedAsExternal(t *testing.T) {
	t.Parallel()

	enrollment := mustNew(t, LearnerUser, true, lessonsWithoutQuiz())
	if err := enrollment.Activate(Activation{At: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := enrollment.ExtendExternalAccess(testNow.Add(time.Hour), 24*time.Hour); !errors.Is(err, ErrExternalEnrollmentRequired) {
		t.Fatalf("ExtendExternalAccess(internal) = %v", err)
	}
}
