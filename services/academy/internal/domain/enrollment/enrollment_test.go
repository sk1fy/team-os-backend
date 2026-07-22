package enrollment

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)

func TestRehydrateRejectsBrokenInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "version is mandatory", mutate: func(s *Snapshot) { s.CourseVersionID = "" }, want: ErrCourseVersionRequired},
		{name: "one learner identity", mutate: func(s *Snapshot) { s.ExternalID = idPtr("external") }, want: ErrLearnerIdentityConflict},
		{name: "matching learner identity", mutate: func(s *Snapshot) { s.UserID = nil }, want: ErrLearnerIdentityRequired},
		{name: "known source", mutate: func(s *Snapshot) { s.SourceType = "mystery" }, want: ErrUnknownSourceType},
		{name: "positive run number", mutate: func(s *Snapshot) { s.AttemptNumber = 0 }, want: ErrEnrollmentAttemptInvalid},
		{name: "unique lesson", mutate: func(s *Snapshot) { s.Lessons = append(s.Lessons, s.Lessons[0]) }, want: ErrDuplicateLessonID},
		{name: "internal hard deadline forbidden", mutate: func(s *Snapshot) { s.AccessUntil = timePtr(testNow.Add(time.Hour)) }, want: ErrInternalDeadlineForbidden},
		{name: "not started has no rows", mutate: func(s *Snapshot) {
			s.LessonProgress = []LessonProgress{{LessonVersionID: "lesson-1", Status: LessonCurrent}}
		}, want: ErrProgressStatusMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := validPendingSnapshot()
			test.mutate(&snapshot)
			_, err := Rehydrate(snapshot)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rehydrate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnrollmentPinnedVersionAndDefensiveSnapshot(t *testing.T) {
	t.Parallel()

	enrollment, err := Rehydrate(validPendingSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := enrollment.EnsureVersion("version-1"); err != nil {
		t.Fatalf("EnsureVersion(correct) = %v", err)
	}
	if err := enrollment.EnsureVersion("version-2"); !errors.Is(err, ErrCourseVersionMismatch) {
		t.Fatalf("EnsureVersion(other) = %v, want mismatch", err)
	}

	snapshot := enrollment.Snapshot()
	snapshot.Lessons[0].ID = "mutated"
	if got := enrollment.Snapshot(); got.CourseVersionID != "version-1" || got.Lessons[0].ID != "lesson-1" {
		t.Fatalf("aggregate mutated through snapshot: %#v", got)
	}
}

func TestActivationRulesAndIdempotency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		learner   LearnerType
		duration  time.Duration
		wantErr   error
		wantUntil *time.Time
	}{
		{name: "internal has no hard deadline", learner: LearnerUser},
		{name: "internal rejects hard deadline", learner: LearnerUser, duration: 24 * time.Hour, wantErr: ErrInternalDeadlineForbidden},
		{name: "external one day", learner: LearnerExternal, duration: 24 * time.Hour, wantUntil: timePtr(testNow.Add(24 * time.Hour))},
		{name: "external seven days", learner: LearnerExternal, duration: 7 * 24 * time.Hour, wantUntil: timePtr(testNow.Add(7 * 24 * time.Hour))},
		{name: "external rejects partial day", learner: LearnerExternal, duration: 36 * time.Hour, wantErr: ErrActivationDurationInvalid},
		{name: "external rejects more than seven days", learner: LearnerExternal, duration: 8 * 24 * time.Hour, wantErr: ErrActivationDurationInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := mustNew(t, test.learner, true, lessonsWithoutQuiz())
			before := enrollment.Snapshot()
			err := enrollment.Activate(Activation{At: testNow, Duration: test.duration})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Activate() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				if !reflect.DeepEqual(enrollment.Snapshot(), before) {
					t.Fatal("failed activation changed enrollment")
				}
				return
			}
			snapshot := enrollment.Snapshot()
			if snapshot.AccessStatus != AccessActive || snapshot.ProgressStatus != ProgressInProgress {
				t.Fatalf("activation state = %s/%s", snapshot.AccessStatus, snapshot.ProgressStatus)
			}
			if !timesEqual(snapshot.AccessUntil, test.wantUntil) {
				t.Fatalf("accessUntil = %v, want %v", snapshot.AccessUntil, test.wantUntil)
			}
			if got, _ := enrollment.LessonStatus("lesson-1"); got != LessonViewCurrent {
				t.Fatalf("first lesson = %s, want current", got)
			}

			firstDeadline := clonePtr(snapshot.AccessUntil)
			if err := enrollment.Activate(Activation{At: testNow.Add(time.Hour), Duration: test.duration}); err != nil {
				t.Fatalf("idempotent Activate() = %v", err)
			}
			if !timesEqual(enrollment.Snapshot().AccessUntil, firstDeadline) {
				t.Fatal("repeated activation changed deadline")
			}
		})
	}
}

func TestLearnerStateProjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		access   AccessStatus
		progress ProgressStatus
		want     LearnerState
	}{
		{name: "pending", access: AccessReady, progress: ProgressNotStarted, want: StatePending},
		{name: "active", access: AccessActive, progress: ProgressInProgress, want: StateActive},
		{name: "completed", access: AccessActive, progress: ProgressCompleted, want: StateCompleted},
		{name: "expired overrides complete", access: AccessExpired, progress: ProgressCompleted, want: StateExpired},
		{name: "frozen", access: AccessFrozen, progress: ProgressInProgress, want: StateFrozen},
		{name: "suspended", access: AccessSuspended, progress: ProgressInProgress, want: StateSuspended},
		{name: "revoked", access: AccessRevoked, progress: ProgressInProgress, want: StateRevoked},
		{name: "closed", access: AccessClosed, progress: ProgressInProgress, want: StateClosed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enrollment := &Enrollment{snapshot: Snapshot{AccessStatus: test.access, ProgressStatus: test.progress}}
			if got := enrollment.State(); got != test.want {
				t.Fatalf("State() = %s, want %s", got, test.want)
			}
		})
	}
}

func validPendingSnapshot() Snapshot {
	userID := ID("user-1")
	return Snapshot{
		ID:              "enrollment-1",
		CompanyID:       "company-1",
		CourseID:        "course-1",
		CourseVersionID: "version-1",
		LearnerType:     LearnerUser,
		UserID:          &userID,
		SourceType:      SourceAssignment,
		AttemptNumber:   1,
		ProgressStatus:  ProgressNotStarted,
		AccessStatus:    AccessReady,
		Sequential:      true,
		Lessons:         lessonsWithoutQuiz(),
		CreatedAt:       testNow,
	}
}

func mustNew(t *testing.T, learner LearnerType, sequential bool, lessons []LessonSpec) *Enrollment {
	t.Helper()
	params := Params{
		ID:              "enrollment-1",
		CompanyID:       "company-1",
		CourseID:        "course-1",
		CourseVersionID: "version-1",
		LearnerType:     learner,
		SourceType:      SourceAssignment,
		AttemptNumber:   1,
		AccessStatus:    AccessReady,
		Sequential:      sequential,
		Lessons:         lessons,
		CreatedAt:       testNow,
	}
	if learner == LearnerUser {
		params.UserID = idPtr("user-1")
	} else {
		params.ExternalID = idPtr("external-1")
		params.SourceType = SourceCompanyCandidateCampaign
	}
	enrollment, err := New(params)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return enrollment
}

func lessonsWithoutQuiz() []LessonSpec {
	return []LessonSpec{{ID: "lesson-1"}, {ID: "lesson-2"}}
}

func lessonsWithQuiz() []LessonSpec {
	return []LessonSpec{{ID: "lesson-1", QuizID: idPtr("quiz-1")}, {ID: "lesson-2"}}
}

func idPtr(value ID) *ID { return &value }

func timesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
