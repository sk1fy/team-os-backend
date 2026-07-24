//go:build integration

package seed

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestApplyAgainstCurrentMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := seedTestPool(t, ctx)

	otherCompanyID := uuid.New()
	otherCourseID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO courses (id, company_id, title, status, author_id, created_by_id)
		VALUES ($1, $2, 'Чужой курс', 'draft', $3, $3)`,
		otherCourseID, otherCompanyID, uuid.New()); err != nil {
		t.Fatalf("подготовка курса другой компании: %v", err)
	}

	fixtures := currentSeedFixtures()
	dataset, err := Normalize(fixtures)
	if err != nil {
		t.Fatalf("Normalize(): %v", err)
	}
	applySeedDataset(t, ctx, pool, dataset)
	assertCurrentSeedModel(t, ctx, pool, dataset, otherCompanyID, otherCourseID)

	// Published versions and their content are immutable. A second reset proves
	// cleanup handles that history and stays idempotent for one company.
	applySeedDataset(t, ctx, pool, dataset)
	assertCurrentSeedModel(t, ctx, pool, dataset, otherCompanyID, otherCourseID)
}

func seedTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("academy"),
		postgres.WithUsername("academy"),
		postgres.WithPassword("academy"),
		postgres.WithInitScripts(
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
			filepath.Join(migrationsDir, "000014_normalize_quiz_question_ids.up.sql"),
			filepath.Join(migrationsDir, "000015_course_partner_audience.up.sql"),
		),
		postgres.BasicWaitStrategies(),
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

func applySeedDataset(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dataset Dataset) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("начать seed-транзакцию: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Apply(ctx, tx, dataset); err != nil {
		t.Fatalf("Apply(): %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatalf("зафиксировать seed-транзакцию: %v", err)
	}
}

func currentSeedFixtures() Fixtures {
	description := "Курс для проверки seed"
	userOne := "employee-one"
	userTwo := "employee-two"
	inviteToken := "legacy-external-invite"
	startedAt := "2026-01-02T04:00:00Z"
	sourceArticleID := "kb-article"
	sourceMode := "copy"
	return Fixtures{
		CompanyID: "seed-company",
		Courses: []CourseFixture{{
			ID: "seed-course", Title: "Курс", Description: &description,
			Status: "published", AuthorID: "seed-author", Sequential: true,
			CreatedAt: "2026-01-02T03:04:05Z", UpdatedAt: "2026-01-03T03:04:05Z",
		}},
		CourseSections: []CourseSectionFixture{{
			ID: "seed-section", CourseID: "seed-course", Title: "Раздел", Order: 0,
		}},
		Lessons: []LessonFixture{
			{
				ID: "seed-lesson-one", CourseID: "seed-course", SectionID: "seed-section",
				Title: "Первый урок", Order: 0, Content: []byte(`{"type":"doc"}`),
			},
			{
				ID: "seed-lesson-two", CourseID: "seed-course", SectionID: "seed-section",
				Title: "Второй урок", Order: 1, Content: []byte(`{"type":"doc"}`),
				SourceArticleID: &sourceArticleID, SourceMode: &sourceMode,
				QuizID: stringPointer("seed-quiz"),
			},
		},
		Quizzes: []QuizFixture{{
			ID: "seed-quiz", LessonID: "seed-lesson-two", Questions: []byte(`[]`),
			PassingScore: 80,
		}},
		Assignments: []AssignmentFixture{
			{
				ID: "assignment-one", CourseID: "seed-course", AssigneeType: "user",
				AssigneeID: &userOne, AssignedByID: "seed-author", CreatedAt: "2026-01-02T05:00:00Z",
			},
			{
				ID: "assignment-two", CourseID: "seed-course", AssigneeType: "user",
				AssigneeID: &userTwo, AssignedByID: "seed-author", CreatedAt: "2026-01-02T06:00:00Z",
			},
			{
				ID: "assignment-external", CourseID: "seed-course", AssigneeType: "external",
				InviteToken: &inviteToken, AssignedByID: "seed-author", CreatedAt: "2026-01-02T07:00:00Z",
			},
		},
		Progress: []ProgressFixture{{
			UserID: userOne, CourseID: "seed-course", Status: "in_progress",
			CompletedLessonIDs: []string{"seed-lesson-one"}, StartedAt: &startedAt,
		}},
	}
}

func assertCurrentSeedModel(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	dataset Dataset,
	otherCompanyID uuid.UUID,
	otherCourseID uuid.UUID,
) {
	t.Helper()
	course := dataset.Courses[0]
	var versionID, latestPublishedID uuid.UUID
	var versionStatus, contentHash string
	if err := pool.QueryRow(ctx, `
		SELECT version.id, version.status, version.content_hash,
		       course.latest_published_version_id
		FROM courses AS course
		JOIN course_versions AS version
		  ON version.company_id = course.company_id AND version.course_id = course.id
		WHERE course.company_id = $1 AND course.id = $2 AND version.number = 1`,
		dataset.CompanyID, course.ID,
	).Scan(&versionID, &versionStatus, &contentHash, &latestPublishedID); err != nil {
		t.Fatalf("прочитать version 1: %v", err)
	}
	if versionID != course.VersionID || latestPublishedID != course.VersionID ||
		versionStatus != "published" || len(contentHash) != 64 {
		t.Fatalf("некорректная version 1: id=%s latest=%s status=%s hash=%q",
			versionID, latestPublishedID, versionStatus, contentHash)
	}

	assertCompanyCount(t, ctx, pool, "course_sections", dataset.CompanyID, len(dataset.Sections))
	assertCompanyCount(t, ctx, pool, "lessons", dataset.CompanyID, len(dataset.Lessons))
	assertCompanyCount(t, ctx, pool, "quizzes", dataset.CompanyID, len(dataset.Quizzes))
	assertCompanyCount(t, ctx, pool, "course_version_sections", dataset.CompanyID, len(dataset.Sections))
	assertCompanyCount(t, ctx, pool, "course_version_lessons", dataset.CompanyID, len(dataset.Lessons))
	assertCompanyCount(t, ctx, pool, "course_version_quizzes", dataset.CompanyID, len(dataset.Quizzes))
	assertCompanyCount(t, ctx, pool, "kb_article_snapshots", dataset.CompanyID, 1)
	assertCompanyCount(t, ctx, pool, "assignments", dataset.CompanyID, len(dataset.Assignments))
	assertCompanyCount(t, ctx, pool, "course_templates", dataset.CompanyID, 10)
	assertCompanyCount(t, ctx, pool, "system_template_seed_checkpoints", dataset.CompanyID, 10)

	var unpinnedAssignments int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM assignments
		WHERE company_id = $1 AND course_version_id <> $2`,
		dataset.CompanyID, course.VersionID).Scan(&unpinnedAssignments); err != nil {
		t.Fatalf("проверить pinned assignments: %v", err)
	}
	if unpinnedAssignments != 0 {
		t.Fatalf("найдены неприкреплённые назначения: %d", unpinnedAssignments)
	}
	var snapshotAttached bool
	if err := pool.QueryRow(ctx, `
		SELECT kb_snapshot_id IS NOT NULL
		FROM course_version_lessons
		WHERE company_id = $1 AND id = $2`, dataset.CompanyID, dataset.Lessons[1].ID,
	).Scan(&snapshotAttached); err != nil {
		t.Fatalf("проверить KB snapshot урока: %v", err)
	}
	if !snapshotAttached {
		t.Fatal("копия урока из БЗ не ссылается на immutable snapshot")
	}

	var enrollmentCount, externalEnrollmentCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE learner_type = 'external')
		FROM course_enrollments WHERE company_id = $1`, dataset.CompanyID,
	).Scan(&enrollmentCount, &externalEnrollmentCount); err != nil {
		t.Fatalf("прочитать прохождения: %v", err)
	}
	if enrollmentCount != 2 || externalEnrollmentCount != 0 {
		t.Fatalf("неожиданные прохождения: всего=%d external=%d",
			enrollmentCount, externalEnrollmentCount)
	}

	userOne, _ := MapID("employee-one")
	userTwo, _ := MapID("employee-two")
	lessonOne, _ := MapID("seed-lesson-one")
	lessonTwo, _ := MapID("seed-lesson-two")
	assertEnrollment(t, ctx, pool, dataset.CompanyID, userOne,
		"assignment", "in_progress", "active", lessonTwo)
	assertEnrollment(t, ctx, pool, dataset.CompanyID, userTwo,
		"assignment", "not_started", "ready", lessonOne)

	var completed, current int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE progress.status = 'completed'),
		       count(*) FILTER (WHERE progress.status = 'current')
		FROM enrollment_lesson_progress AS progress
		JOIN course_enrollments AS enrollment ON enrollment.id = progress.enrollment_id
		WHERE progress.company_id = $1 AND enrollment.user_id = $2`,
		dataset.CompanyID, userOne).Scan(&completed, &current); err != nil {
		t.Fatalf("прочитать прогресс уроков: %v", err)
	}
	if completed != 1 || current != 1 {
		t.Fatalf("неожиданный прогресс уроков: completed=%d current=%d", completed, current)
	}

	var otherCourseExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM courses WHERE company_id = $1 AND id = $2)`,
		otherCompanyID, otherCourseID).Scan(&otherCourseExists); err != nil {
		t.Fatalf("проверить курс другой компании: %v", err)
	}
	if !otherCourseExists {
		t.Fatal("tenant cleanup удалил курс другой компании")
	}
}

func assertCompanyCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	companyID uuid.UUID,
	want int,
) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM "+table+" WHERE company_id = $1", companyID,
	).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", table, got, want)
	}
}

func assertEnrollment(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	userID uuid.UUID,
	wantSource string,
	wantProgress string,
	wantAccess string,
	wantCurrent uuid.UUID,
) {
	t.Helper()
	var source, progress, access string
	var current uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT source_type, progress_status, access_status, current_lesson_version_id
		FROM course_enrollments WHERE company_id = $1 AND user_id = $2`,
		companyID, userID,
	).Scan(&source, &progress, &access, &current); err != nil {
		t.Fatalf("прочитать прохождение %s: %v", userID, err)
	}
	if source != wantSource || progress != wantProgress || access != wantAccess || current != wantCurrent {
		t.Fatalf("прохождение %s: source=%s progress=%s access=%s current=%s",
			userID, source, progress, access, current)
	}
}
