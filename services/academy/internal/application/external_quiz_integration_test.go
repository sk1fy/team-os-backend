//go:build integration

package application

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestExternalProgressMutationsAreAtomicAndIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := externalQuizTestPool(t, ctx)
	fixedNow := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	fixture := seedExternalQuizFixture(t, ctx, pool, fixedNow)
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("создание Academy service: %v", err)
	}
	service.now = func() time.Time { return fixedNow }
	principal := ExternalPrincipal{
		CompanyID: fixture.companyID,
		LearnerID: fixture.learnerID,
		SessionID: fixture.sessionID,
		ExpiresAt: fixedNow.Add(24 * time.Hour),
	}

	t.Run("complete lesson serializes one idempotency key", func(t *testing.T) {
		results := runConcurrently(2, func() (Enrollment, error) {
			return service.CompletePublicAcademyEnrollmentLesson(
				ctx, principal, fixture.enrollmentID, fixture.firstLessonID, "complete-key-1",
			)
		})
		for index, result := range results {
			if result.err != nil {
				t.Fatalf("complete result %d: %v", index, result.err)
			}
			if result.value.ProgressStatus != "in_progress" || result.value.ProgressPercent != 50 {
				t.Fatalf("complete result %d: %+v", index, result.value)
			}
		}
		assertExternalMutationCount(t, ctx, pool, fixture.companyID,
			externalOperationCompleteLesson, "complete-key-1", 1)
		assertCount(t, ctx, pool, `
			SELECT count(*) FROM outbox
			WHERE company_id = $1 AND aggregate_id = $2`, 1,
			fixture.companyID, fixture.enrollmentID)
	})

	t.Run("submit quiz serializes one idempotency key", func(t *testing.T) {
		input := SubmitExternalQuizInput{
			EnrollmentID:   fixture.enrollmentID,
			QuizID:         fixture.quizID,
			IdempotencyKey: "quiz-key-0001",
			Answers: []EnrollmentQuizAnswer{{
				QuestionID: "q1", SelectedOptionIDs: []string{"correct"},
			}},
		}
		results := runConcurrently(2, func() (ExternalQuizAttemptResult, error) {
			return service.SubmitPublicAcademyQuizAttempt(ctx, principal, input)
		})
		for index, result := range results {
			if result.err != nil {
				t.Fatalf("quiz result %d: %v", index, result.err)
			}
			if result.value.ID == uuid.Nil || result.value.Score != 100 || !result.value.Passed {
				t.Fatalf("quiz result %d: %+v", index, result.value)
			}
		}
		if results[0].value.ID != results[1].value.ID {
			t.Fatalf("повтор создал разные attempts: %s и %s",
				results[0].value.ID, results[1].value.ID)
		}
		assertExternalMutationCount(t, ctx, pool, fixture.companyID,
			externalOperationSubmitQuiz, input.IdempotencyKey, 1)
		assertCount(t, ctx, pool, `
			SELECT count(*) FROM quiz_attempts
			WHERE company_id = $1 AND enrollment_id = $2`, 1,
			fixture.companyID, fixture.enrollmentID)
		var legacyQuizID, legacyUserID uuid.NullUUID
		if err = pool.QueryRow(ctx, `
			SELECT quiz_id, user_id FROM quiz_attempts
			WHERE company_id = $1 AND enrollment_id = $2`,
			fixture.companyID, fixture.enrollmentID,
		).Scan(&legacyQuizID, &legacyUserID); err != nil {
			t.Fatalf("legacy-поля external attempt: %v", err)
		}
		if legacyQuizID.Valid || legacyUserID.Valid {
			t.Fatalf("external attempt записан в legacy identity: quiz=%v user=%v",
				legacyQuizID, legacyUserID)
		}

		conflicting := input
		conflicting.Answers = []EnrollmentQuizAnswer{{QuestionID: "q1"}}
		if _, conflictErr := service.SubmitPublicAcademyQuizAttempt(ctx, principal, conflicting); !isApplicationError(conflictErr, ErrorConflict) {
			t.Fatalf("другой request с тем же ключом: %v", conflictErr)
		}
		assertCount(t, ctx, pool, `
			SELECT count(*) FROM quiz_attempts
			WHERE company_id = $1 AND enrollment_id = $2`, 1,
			fixture.companyID, fixture.enrollmentID)
	})

	t.Run("failed mutation rolls reservation back", func(t *testing.T) {
		_, submitErr := service.SubmitPublicAcademyQuizAttempt(ctx, principal, SubmitExternalQuizInput{
			EnrollmentID: fixture.enrollmentID, QuizID: fixture.quizID,
			IdempotencyKey: "quiz-invalid-1",
			Answers: []EnrollmentQuizAnswer{{
				QuestionID: "unknown", SelectedOptionIDs: []string{"correct"},
			}},
		})
		if submitErr == nil {
			t.Fatal("некорректный ответ принят")
		}
		assertExternalMutationCount(t, ctx, pool, fixture.companyID,
			externalOperationSubmitQuiz, "quiz-invalid-1", 0)
	})

	t.Run("modern and legacy employee projections remain compatible", func(t *testing.T) {
		queries := db.New(pool)
		modernEnrollmentID := insertEmployeeEnrollment(t, ctx, pool, fixture, uuid.New())
		modernAttemptID := uuid.New()
		if _, createErr := queries.CreateEnrollmentQuizAttempt(ctx, db.CreateEnrollmentQuizAttemptParams{
			ID: modernAttemptID, CompanyID: fixture.companyID, EnrollmentID: modernEnrollmentID,
			QuizVersionID: fixture.quizID, Answers: []byte(`[]`), Score: 0,
			CreatedAt: fixedNow,
		}); createErr != nil {
			t.Fatalf("modern employee attempt: %v", createErr)
		}
		assertLegacyAttemptIdentity(t, ctx, pool, modernAttemptID, false)

		insertLegacyQuizProjection(t, ctx, pool, fixture)
		legacyUserID := uuid.New()
		legacyEnrollmentID := insertEmployeeEnrollment(t, ctx, pool, fixture, legacyUserID)
		legacyAttemptID := uuid.New()
		if _, createErr := queries.CreateEnrollmentQuizAttempt(ctx, db.CreateEnrollmentQuizAttemptParams{
			ID: legacyAttemptID, CompanyID: fixture.companyID, EnrollmentID: legacyEnrollmentID,
			QuizVersionID: fixture.quizID, Answers: []byte(`[]`), Score: 0,
			CreatedAt: fixedNow,
		}); createErr != nil {
			t.Fatalf("legacy employee attempt: %v", createErr)
		}
		assertLegacyAttemptIdentity(t, ctx, pool, legacyAttemptID, true)

		_, wrongTenantErr := queries.CreateEnrollmentQuizAttempt(ctx, db.CreateEnrollmentQuizAttemptParams{
			ID: uuid.New(), CompanyID: uuid.New(), EnrollmentID: legacyEnrollmentID,
			QuizVersionID: fixture.quizID, Answers: []byte(`[]`), CreatedAt: fixedNow,
		})
		if !errors.Is(wrongTenantErr, pgx.ErrNoRows) {
			t.Fatalf("cross-tenant attempt error = %v, want pgx.ErrNoRows", wrongTenantErr)
		}
	})
}

type concurrentResult[T any] struct {
	value T
	err   error
}

func runConcurrently[T any](count int, operation func() (T, error)) []concurrentResult[T] {
	start := make(chan struct{})
	results := make([]concurrentResult[T], count)
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index].value, results[index].err = operation()
		}()
	}
	close(start)
	wait.Wait()
	return results
}

type externalQuizFixture struct {
	companyID, courseID, versionID, sectionID uuid.UUID
	firstLessonID, quizLessonID, quizID       uuid.UUID
	learnerID, sessionID, enrollmentID        uuid.UUID
}

func seedExternalQuizFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) externalQuizFixture {
	t.Helper()
	fixture := externalQuizFixture{
		companyID: uuid.New(), courseID: uuid.New(), versionID: uuid.New(), sectionID: uuid.New(),
		firstLessonID: uuid.New(), quizLessonID: uuid.New(), quizID: uuid.New(),
		learnerID: uuid.New(), sessionID: uuid.New(), enrollmentID: uuid.New(),
	}
	authorID := uuid.New()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (
			id, company_id, title, status, author_id, created_by_id
		) VALUES ($1,$2,'Внешний курс','published',$3,$3)`,
			[]any{fixture.courseID, fixture.companyID, authorID}},
		{`INSERT INTO course_versions (
			id, company_id, course_id, number, status, title, sequential, created_by_id, created_at
		) VALUES ($1,$2,$3,1,'draft','Внешний курс',true,$4,$5)`,
			[]any{fixture.versionID, fixture.companyID, fixture.courseID, authorID, now}},
		{`INSERT INTO course_version_sections (
			id, company_id, course_version_id, stable_key, title, "order"
		) VALUES ($1,$2,$3,$1,'Раздел',0)`,
			[]any{fixture.sectionID, fixture.companyID, fixture.versionID}},
		{`INSERT INTO course_version_lessons (
			id, company_id, course_version_id, section_version_id,
			stable_key, title, "order", content
		) VALUES
			($1,$2,$3,$4,$1,'Первый урок',0,'{"type":"doc"}'),
			($5,$2,$3,$4,$5,'Урок с тестом',1,'{"type":"doc"}')`,
			[]any{fixture.firstLessonID, fixture.companyID, fixture.versionID,
				fixture.sectionID, fixture.quizLessonID}},
		{`INSERT INTO course_version_quizzes (
			id, company_id, course_version_id, lesson_version_id,
			questions, passing_score, max_attempts
		) VALUES ($1,$2,$3,$4,$5,80,3)`,
			[]any{fixture.quizID, fixture.companyID, fixture.versionID, fixture.quizLessonID,
				[]byte(`[{"id":"q1","type":"single","text":"Вопрос","options":[{"id":"correct","text":"Да","correct":true}]}]`)}},
		{`UPDATE course_version_lessons SET quiz_version_id=$1 WHERE id=$2`,
			[]any{fixture.quizID, fixture.quizLessonID}},
		{`UPDATE course_versions SET status='published', published_by_id=$2,
			published_at=$3, content_hash=repeat('a',64) WHERE id=$1`,
			[]any{fixture.versionID, authorID, now}},
		{`UPDATE courses SET latest_published_version_id=$1 WHERE id=$2`,
			[]any{fixture.versionID, fixture.courseID}},
		{`INSERT INTO external_learners (
			id, company_id, email, normalized_email, email_verified_at, created_at, updated_at
		) VALUES ($1,$2,'learner@example.test','learner@example.test',$3,$3,$3)`,
			[]any{fixture.learnerID, fixture.companyID, now}},
		{`INSERT INTO external_sessions (
			id, company_id, external_learner_id, token_hash, expires_at, created_at
		) VALUES ($1,$2,$3,decode(repeat('11',32),'hex'),$4,$5)`,
			[]any{fixture.sessionID, fixture.companyID, fixture.learnerID,
				now.Add(24 * time.Hour), now}},
		{`INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id, learner_type,
			external_learner_id, source_type, progress_status, access_status,
			current_lesson_version_id, activated_at, access_until, started_at,
			last_activity_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'external',$5,'legacy','in_progress','active',
			$6,$7,$8,$7,$7,$7,$7)`,
			[]any{fixture.enrollmentID, fixture.companyID, fixture.courseID,
				fixture.versionID, fixture.learnerID, fixture.firstLessonID,
				now, now.Add(7 * 24 * time.Hour)}},
		{`INSERT INTO enrollment_lesson_progress (
			company_id, enrollment_id, lesson_version_id, status, first_opened_at
		) VALUES ($1,$2,$3,'current',$4)`,
			[]any{fixture.companyID, fixture.enrollmentID, fixture.firstLessonID, now}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed external quiz fixture: %v\n%s", err, statement.query)
		}
	}
	return fixture
}

func insertEmployeeEnrollment(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture externalQuizFixture,
	userID uuid.UUID,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id, learner_type,
			user_id, source_type, progress_status, access_status,
			current_lesson_version_id
		) VALUES ($1,$2,$3,$4,'user',$5,'legacy','in_progress','active',$6)`,
		id, fixture.companyID, fixture.courseID, fixture.versionID,
		userID, fixture.quizLessonID); err != nil {
		t.Fatalf("employee enrollment: %v", err)
	}
	return id
}

func insertLegacyQuizProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture externalQuizFixture,
) {
	t.Helper()
	legacySectionID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_sections (id, company_id, course_id, title, "order")
		VALUES ($1,$2,$3,'Legacy',0)`,
		legacySectionID, fixture.companyID, fixture.courseID); err != nil {
		t.Fatalf("legacy course section: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO lessons (
			id, company_id, course_id, section_id, title, "order", content, quiz_id
		) VALUES ($1,$2,$3,$4,'Legacy quiz lesson',0,'{"type":"doc"}',$5)`,
		fixture.quizLessonID, fixture.companyID, fixture.courseID,
		legacySectionID, fixture.quizID); err != nil {
		t.Fatalf("legacy lesson: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO quizzes (id, company_id, lesson_id, questions, passing_score)
		VALUES ($1,$2,$3,'[]',0)`,
		fixture.quizID, fixture.companyID, fixture.quizLessonID); err != nil {
		t.Fatalf("legacy quiz: %v", err)
	}
}

func assertLegacyAttemptIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	attemptID uuid.UUID,
	wantValid bool,
) {
	t.Helper()
	var quizID, userID uuid.NullUUID
	if err := pool.QueryRow(ctx, `
		SELECT quiz_id, user_id FROM quiz_attempts WHERE id=$1`, attemptID,
	).Scan(&quizID, &userID); err != nil {
		t.Fatalf("attempt identity %s: %v", attemptID, err)
	}
	if quizID.Valid != wantValid || userID.Valid != wantValid {
		t.Fatalf("attempt identity %s: quiz=%v user=%v, want valid=%v",
			attemptID, quizID, userID, wantValid)
	}
}

func assertExternalMutationCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	operation, key string,
	want int,
) {
	t.Helper()
	assertCount(t, ctx, pool, `
		SELECT count(*) FROM external_mutation_idempotency
		WHERE company_id=$1 AND operation=$2 AND idempotency_key=$3
		  AND completed_at IS NOT NULL`, want, companyID, operation, key)
}

func assertCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	query string,
	want int,
	args ...any,
) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, query, args...).Scan(&got); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func externalQuizTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("academy"), postgres.WithUsername("academy"),
		postgres.WithPassword("academy"), postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_assignment_events_and_outbox.up.sql"),
			filepath.Join(migrationsDir, "000003_course_visibility_assignment_idempotency.up.sql"),
			filepath.Join(migrationsDir, "000004_course_ownership_lifecycle_audit.up.sql"),
			filepath.Join(migrationsDir, "000005_immutable_course_versions.up.sql"),
			filepath.Join(migrationsDir, "000006_version_pinned_enrollments.up.sql"),
			filepath.Join(migrationsDir, "000007_partner_courses_and_restrictions.up.sql"),
			filepath.Join(migrationsDir, "000008_course_templates_and_kb_snapshots.up.sql"),
			filepath.Join(migrationsDir, "000009_external_learners_and_personal_accesses.up.sql"),
			filepath.Join(migrationsDir, "000010_external_campaigns_and_analytics.up.sql"),
			filepath.Join(migrationsDir, "000011_self_enrollment.up.sql"),
			filepath.Join(migrationsDir, "000012_external_quiz_attempts.up.sql"),
			filepath.Join(migrationsDir, "000013_enrollment_mutation_idempotency.up.sql"),
		), postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("запуск PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("DSN PostgreSQL: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
