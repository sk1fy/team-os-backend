//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInternalEnrollmentMutationsAreAtomicAndIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := externalQuizTestPool(t, ctx)
	now := time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC)
	fixture := seedExternalQuizFixture(t, ctx, pool, now)
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("создание Academy service: %v", err)
	}
	service.now = func() time.Time { return now }

	userID, enrollmentID := uuid.New(), uuid.New()
	if _, err = pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id, learner_type,
			user_id, source_type, attempt_number, progress_status,
			access_status, current_lesson_version_id, started_at,
			last_activity_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'user',$5,'legacy',1,'in_progress',
			'active',$6,$7,$7,$7,$7);
		INSERT INTO enrollment_lesson_progress (
			company_id, enrollment_id, lesson_version_id, status,
			first_opened_at
		) VALUES ($2,$1,$6,'current',$7)`,
		enrollmentID, fixture.companyID, fixture.courseID, fixture.versionID,
		userID, fixture.firstLessonID, now); err != nil {
		t.Fatalf("подготовка прохождения сотрудника: %v", err)
	}
	actor := Actor{CompanyID: fixture.companyID, UserID: userID, Role: "employee"}

	completeInput := CompleteEnrollmentLessonInput{
		EnrollmentID: enrollmentID, LessonID: fixture.firstLessonID,
		IdempotencyKey: "internal-complete-1",
	}
	completeResults := runConcurrently(2, func() (EnrollmentProgressSnapshot, error) {
		return service.CompleteEnrollmentLesson(ctx, actor, completeInput)
	})
	for index, result := range completeResults {
		if result.err != nil {
			t.Fatalf("complete result %d: %v", index, result.err)
		}
		if result.value.Enrollment.ProgressPercent != 50 {
			t.Fatalf("complete result %d: %+v", index, result.value.Enrollment)
		}
	}
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM enrollment_mutation_idempotency
		WHERE company_id=$1 AND actor_user_id=$2 AND operation='complete_lesson'
		  AND idempotency_key=$3 AND completed_at IS NOT NULL`, 1,
		fixture.companyID, userID, completeInput.IdempotencyKey)
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM outbox
		WHERE company_id=$1 AND aggregate_id=$2`, 1,
		fixture.companyID, enrollmentID)

	quizInput := SubmitEnrollmentQuizInput{
		EnrollmentID: enrollmentID, QuizID: fixture.quizID,
		IdempotencyKey: "internal-quiz-0001",
		Answers: []EnrollmentQuizAnswer{{
			QuestionID: "q1", SelectedOptionIDs: []string{"correct"},
		}},
	}
	quizResults := runConcurrently(2, func() (EnrollmentQuizAttempt, error) {
		attempt, _, submitErr := service.SubmitEnrollmentQuizAttempt(ctx, actor, quizInput)
		return attempt, submitErr
	})
	for index, result := range quizResults {
		if result.err != nil {
			t.Fatalf("quiz result %d: %v", index, result.err)
		}
		if result.value.ID == uuid.Nil || result.value.Score != 100 || !result.value.Passed {
			t.Fatalf("quiz result %d: %+v", index, result.value)
		}
	}
	if quizResults[0].value.ID != quizResults[1].value.ID {
		t.Fatalf("повтор создал разные attempts: %s и %s",
			quizResults[0].value.ID, quizResults[1].value.ID)
	}
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM quiz_attempts
		WHERE company_id=$1 AND enrollment_id=$2`, 1,
		fixture.companyID, enrollmentID)
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM enrollment_mutation_idempotency
		WHERE company_id=$1 AND actor_user_id=$2 AND operation='submit_quiz'
		  AND idempotency_key=$3 AND completed_at IS NOT NULL`, 1,
		fixture.companyID, userID, quizInput.IdempotencyKey)

	conflicting := quizInput
	conflicting.Answers = []EnrollmentQuizAnswer{{QuestionID: "q1"}}
	if _, _, conflictErr := service.SubmitEnrollmentQuizAttempt(ctx, actor, conflicting); !isApplicationError(conflictErr, ErrorConflict) {
		t.Fatalf("другой request с тем же ключом: %v", conflictErr)
	}
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM quiz_attempts
		WHERE company_id=$1 AND enrollment_id=$2`, 1,
		fixture.companyID, enrollmentID)
}
