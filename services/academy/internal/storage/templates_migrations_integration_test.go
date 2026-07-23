//go:build integration

package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestCourseTemplatesMigrationSeedIsolationAndSagas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
		),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("запуск Postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("DSN Postgres: %v", err)
	}
	pool, err := newMigrationTestPool(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к Postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	companyID, actorID := uuid.New(), uuid.New()
	seedMarkerCourseID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, created_by_id
		) VALUES ($1, $2, 'Маркер компании', 'draft', $3, true,
			'company', 'company', $3)`, seedMarkerCourseID, companyID, actorID)
	if err != nil {
		t.Fatalf("подготовка tenant: %v", err)
	}

	migration, err := os.ReadFile(filepath.Join(
		migrationsDir, "000008_course_templates_and_kb_snapshots.up.sql",
	))
	if err != nil {
		t.Fatalf("чтение template-миграции: %v", err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("применение template-миграции: %v", err)
	}

	var templates, versions, sections, lessons, quizzes, checkpoints int
	err = pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM course_templates
			 WHERE company_id = $1 AND template_type = 'system'),
			(SELECT count(*) FROM course_template_versions
			 WHERE company_id = $1 AND status = 'published'),
			(SELECT count(*) FROM course_template_version_sections
			 WHERE company_id = $1),
			(SELECT count(*) FROM course_template_version_lessons
			 WHERE company_id = $1 AND content ->> 'type' = 'doc'),
			(SELECT count(*) FROM course_template_version_quizzes
			 WHERE company_id = $1 AND jsonb_array_length(questions) > 0),
			(SELECT count(*) FROM system_template_seed_checkpoints
			 WHERE company_id = $1 AND seed_version = 1)`, companyID).
		Scan(&templates, &versions, &sections, &lessons, &quizzes, &checkpoints)
	if err != nil || templates != 10 || versions != 10 || sections != 20 ||
		lessons != 30 || quizzes != 10 || checkpoints != 10 {
		t.Fatalf("неполный system seed: templates=%d versions=%d sections=%d lessons=%d quizzes=%d checkpoints=%d err=%v",
			templates, versions, sections, lessons, quizzes, checkpoints, err)
	}

	var insertedAgain int
	if err = pool.QueryRow(ctx, `SELECT academy_seed_system_templates($1)`, companyID).
		Scan(&insertedAgain); err != nil || insertedAgain != 0 {
		t.Fatalf("system seed не идемпотентен: inserted=%d err=%v", insertedAgain, err)
	}

	var systemTemplateID, systemVersionID, systemSectionID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT template.id, version.id, section.id
		FROM course_templates AS template
		JOIN course_template_versions AS version
		  ON version.id = template.latest_published_version_id
		JOIN course_template_version_sections AS section
		  ON section.template_version_id = version.id
		WHERE template.company_id = $1
		  AND template.system_template_key = 'employee-onboarding'
		ORDER BY section."order" LIMIT 1`, companyID).
		Scan(&systemTemplateID, &systemVersionID, &systemSectionID)
	if err != nil {
		t.Fatalf("чтение системного шаблона: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE course_templates SET lifecycle_status = 'archived' WHERE id = $1`,
		systemTemplateID); err == nil || !strings.Contains(err.Error(), "неизмен") {
		t.Fatalf("системный шаблон оказался изменяемым: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE course_template_version_sections SET title = 'Подмена' WHERE id = $1`,
		systemSectionID); err == nil || !strings.Contains(err.Error(), "неизмен") {
		t.Fatalf("published content шаблона оказался изменяемым: %v", err)
	}

	// A reusable KB snapshot is self-contained and remains immutable after
	// source policy changes in the KB service.
	snapshotID, sourceArticleID, sourceArticleVersionID := uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO kb_article_snapshots (
			id, company_id, source_article_id, source_article_version_id,
			source_article_version_number, reuse_grant_id, requested_by_id,
			requested_by_partner_id, request_key, title, content,
			source_file_ids, content_hash
		) VALUES (
			$1, $2, $3, $4, 2, $5, $6, $7, 'snapshot-key',
			'Разрешённая статья', '{"type":"doc","content":[]}'::jsonb,
			ARRAY[$8]::uuid[], repeat('a', 64)
		)`, snapshotID, companyID, sourceArticleID, sourceArticleVersionID,
		uuid.New(), actorID, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("сохранение KB snapshot provenance: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE kb_article_snapshots SET title = 'Подмена' WHERE id = $1`,
		snapshotID); err == nil || !strings.Contains(err.Error(), "неизмен") {
		t.Fatalf("KB snapshot оказался изменяемым: %v", err)
	}

	// Instantiate the selected published version into a partner-owned draft.
	targetCourseID, targetVersionID := uuid.New(), uuid.New()
	partnerID, originID, idempotencyID := uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, owner_user_id, created_by_id
		) VALUES ($1, $2, 'Курс из шаблона', 'draft', $3, true,
			'restricted', 'partner', $3, $3);
		INSERT INTO course_versions (
			id, company_id, course_id, number, status, title,
			sequential, created_by_id
		) VALUES ($4, $2, $1, 1, 'draft', 'Курс из шаблона', true, $3);
		UPDATE courses SET current_draft_version_id = $4 WHERE id = $1`,
		targetCourseID, companyID, partnerID, targetVersionID)
	if err != nil {
		t.Fatalf("создание target draft: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO course_version_sections (
			id, company_id, course_version_id, stable_key, title, "order"
		)
		SELECT gen_random_uuid(), $1, $2, stable_key, title, "order"
		FROM course_template_version_sections
		WHERE template_version_id = $3;
		INSERT INTO course_version_lessons (
			id, company_id, course_version_id, section_version_id, stable_key,
			title, "order", content, source_type, source_template_id,
			source_template_version_id, estimated_minutes
		)
		SELECT gen_random_uuid(), $1, $2, target_section.id,
			source_lesson.stable_key, source_lesson.title, source_lesson."order",
			source_lesson.content, 'template_snapshot', $4, $3,
			source_lesson.estimated_minutes
		FROM course_template_version_lessons AS source_lesson
		JOIN course_template_version_sections AS source_section
		  ON source_section.id = source_lesson.section_version_id
		JOIN course_version_sections AS target_section
		  ON target_section.course_version_id = $2
		 AND target_section.stable_key = source_section.stable_key
		WHERE source_lesson.template_version_id = $3;
		WITH inserted AS (
			INSERT INTO course_version_quizzes (
				id, company_id, course_version_id, lesson_version_id,
				questions, passing_score, max_attempts
			)
			SELECT gen_random_uuid(), $1, $2, target_lesson.id,
				source_quiz.questions, source_quiz.passing_score,
				source_quiz.max_attempts
			FROM course_template_version_quizzes AS source_quiz
			JOIN course_template_version_lessons AS source_lesson
			  ON source_lesson.id = source_quiz.lesson_version_id
			JOIN course_version_lessons AS target_lesson
			  ON target_lesson.course_version_id = $2
			 AND target_lesson.stable_key = source_lesson.stable_key
			WHERE source_quiz.template_version_id = $3
			RETURNING id, lesson_version_id
		)
		UPDATE course_version_lessons AS lesson
		SET quiz_version_id = inserted.id
		FROM inserted WHERE lesson.id = inserted.lesson_version_id`,
		companyID, targetVersionID, systemVersionID, systemTemplateID)
	if err != nil {
		t.Fatalf("глубокое инстанцирование шаблона: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO course_origins (
			id, company_id, target_course_id, origin_type,
			source_template_id, source_template_version_id,
			instantiated_by_id, acquisition_type
		) VALUES ($1, $2, $3, 'system_template', $4, $5, $6, 'free_copy');
		INSERT INTO course_template_instantiation_idempotency (
			id, company_id, source_template_id, source_template_version_id,
			target_owner_type, target_owner_user_id, idempotency_key,
			target_course_id, target_course_version_id, origin_id,
			instantiated_by_id
		) VALUES ($7, $2, $4, $5, 'partner', $6, 'instantiate-key',
			$3, $8, $1, $6)`, originID, companyID, targetCourseID,
		systemTemplateID, systemVersionID, partnerID, idempotencyID, targetVersionID)
	if err != nil {
		t.Fatalf("provenance инстанцирования: %v", err)
	}

	var copiedSections, copiedLessons, copiedQuizzes, operationalRows int
	err = pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM course_version_sections WHERE course_version_id = $1),
			(SELECT count(*) FROM course_version_lessons WHERE course_version_id = $1),
			(SELECT count(*) FROM course_version_quizzes WHERE course_version_id = $1),
			(SELECT count(*) FROM course_enrollments WHERE course_id = $2) +
			(SELECT count(*) FROM assignments WHERE course_id = $2)`,
		targetVersionID, targetCourseID).
		Scan(&copiedSections, &copiedLessons, &copiedQuizzes, &operationalRows)
	if err != nil || copiedSections != 2 || copiedLessons != 3 ||
		copiedQuizzes != 1 || operationalRows != 0 {
		t.Fatalf("инстанцирование не независимо: sections=%d lessons=%d quizzes=%d operational=%d err=%v",
			copiedSections, copiedLessons, copiedQuizzes, operationalRows, err)
	}

	otherCompanyID, otherTargetCourseID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, created_by_id
		) VALUES ($1, $2, 'Чужой tenant', 'draft', $3, true,
			'company', 'company', $3)`, otherTargetCourseID, otherCompanyID, actorID)
	if err != nil {
		t.Fatalf("подготовка другого tenant: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO course_origins (
			company_id, target_course_id, origin_type,
			source_template_id, source_template_version_id, instantiated_by_id
		) VALUES ($1, $2, 'system_template', $3, $4, $5)`,
		otherCompanyID, otherTargetCourseID, systemTemplateID,
		systemVersionID, actorID); err == nil {
		t.Fatal("разрешён cross-tenant template origin")
	}

	// Saga rows are idempotent and can finish only after every item is cloned.
	jobID, sourceFileID, targetFileID := uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO academy_file_clone_jobs (
			id, company_id, operation_type, aggregate_id, idempotency_key,
			source_owner_type, source_owner_id, target_owner_type,
			target_owner_id, status
		) VALUES ($1, $2, 'template_instantiate', $3, 'files-key',
			'template_version', $4, 'course_version', $3, 'running');
		INSERT INTO academy_file_clone_job_items (
			company_id, job_id, source_file_id, status
		) VALUES ($2, $1, $5, 'pending')`, jobID, companyID,
		targetVersionID, systemVersionID, sourceFileID)
	if err != nil {
		t.Fatalf("создание file clone saga: %v", err)
	}
	var completedRows int64
	command, err := pool.Exec(ctx, `
		UPDATE academy_file_clone_jobs AS job
		SET status = 'completed', completed_at = now(), updated_at = now()
		WHERE id = $1 AND NOT EXISTS (
			SELECT 1 FROM academy_file_clone_job_items AS item
			WHERE item.job_id = job.id AND item.status <> 'completed'
		)`, jobID)
	if err != nil {
		t.Fatalf("проверка незавершённой saga: %v", err)
	}
	completedRows = command.RowsAffected()
	if completedRows != 0 {
		t.Fatal("file clone job завершился до клонирования items")
	}
	_, err = pool.Exec(ctx, `
		UPDATE academy_file_clone_job_items
		SET status = 'completed', target_file_id = $2, updated_at = now()
		WHERE job_id = $1;
		UPDATE academy_file_clone_jobs
		SET status = 'completed', completed_at = now(), updated_at = now()
		WHERE id = $1`, jobID, targetFileID)
	if err != nil {
		t.Fatalf("завершение file clone saga: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO academy_file_clone_jobs (
			company_id, operation_type, aggregate_id, idempotency_key,
			source_owner_type, source_owner_id, target_owner_type,
			target_owner_id
		) VALUES ($1, 'template_instantiate', $2, 'files-key',
			'template_version', $3, 'course_version', $2)`,
		companyID, targetVersionID, systemVersionID); err == nil {
		t.Fatal("file clone idempotency допускает дубликат")
	}
}
