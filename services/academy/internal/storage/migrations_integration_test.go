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

func TestImmutableCourseVersionsMigrationBackfillAndGuards(t *testing.T) {
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
	publishedCourseID, draftCourseID := uuid.New(), uuid.New()
	publishedSectionID, draftSectionID := uuid.New(), uuid.New()
	publishedLessonID, draftLessonID := uuid.New(), uuid.New()
	publishedQuizID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			deadline_days, visibility, created_by_id
		) VALUES
			($1, $3, 'Опубликованный', 'published', $4, true, 5, 'company', $4),
			($2, $3, 'Черновик', 'draft', $4, false, NULL, 'restricted', $4)`,
		publishedCourseID, draftCourseID, companyID, actorID)
	if err != nil {
		t.Fatalf("подготовка legacy-курсов: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO course_sections (id, company_id, course_id, title, "order")
		VALUES ($1, $3, $4, 'Раздел', 0), ($2, $3, $5, 'Черновой раздел', 0)`,
		publishedSectionID, draftSectionID, companyID, publishedCourseID, draftCourseID)
	if err != nil {
		t.Fatalf("подготовка legacy-разделов: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO lessons (
			id, company_id, course_id, section_id, title, "order", content,
			source_article_id, source_mode, quiz_id
		) VALUES
			($1, $3, $4, $6, 'Урок', 0, '{"type":"doc"}'::jsonb, NULL, NULL, $8),
			($2, $3, $5, $7, 'Черновой урок', 0, '{"type":"doc"}'::jsonb, NULL, NULL, NULL)`,
		publishedLessonID, draftLessonID, companyID, publishedCourseID, draftCourseID,
		publishedSectionID, draftSectionID, publishedQuizID)
	if err != nil {
		t.Fatalf("подготовка legacy-уроков: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO quizzes (id, company_id, lesson_id, questions, passing_score)
		VALUES ($1, $2, $3, '[]'::jsonb, 80)`,
		publishedQuizID, companyID, publishedLessonID)
	if err != nil {
		t.Fatalf("подготовка legacy-данных: %v", err)
	}

	migration, err := os.ReadFile(filepath.Join(migrationsDir, "000005_immutable_course_versions.up.sql"))
	if err != nil {
		t.Fatalf("чтение миграции: %v", err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("применение миграции: %v", err)
	}

	var publishedVersionID, publishedPointer, draftVersionID, draftPointer uuid.UUID
	var publishedHash, draftHash *string
	err = pool.QueryRow(ctx, `
		SELECT published_version.id, course.latest_published_version_id,
		       published_version.content_hash
		FROM courses AS course
		JOIN course_versions AS published_version
		  ON published_version.course_id = course.id AND published_version.number = 1
		WHERE course.id = $1`, publishedCourseID).
		Scan(&publishedVersionID, &publishedPointer, &publishedHash)
	if err != nil {
		t.Fatalf("чтение published version 1: %v", err)
	}
	if publishedVersionID != publishedPointer || publishedHash == nil || len(*publishedHash) != 64 {
		t.Fatalf("некорректный published backfill: version=%s pointer=%s hash=%v",
			publishedVersionID, publishedPointer, publishedHash)
	}

	err = pool.QueryRow(ctx, `
		SELECT version.id, course.current_draft_version_id, version.content_hash
		FROM courses AS course
		JOIN course_versions AS version
		  ON version.course_id = course.id AND version.number = 1
		WHERE course.id = $1`, draftCourseID).Scan(&draftVersionID, &draftPointer, &draftHash)
	if err != nil {
		t.Fatalf("чтение draft version 1: %v", err)
	}
	if draftVersionID != draftPointer {
		t.Fatalf("draft pointer не указывает на version 1: version=%s pointer=%s",
			draftVersionID, draftPointer)
	}
	if draftHash != nil {
		t.Fatalf("черновик получил content_hash: %q", *draftHash)
	}

	var sectionCount, lessonCount, quizCount int
	err = pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM course_version_sections WHERE id IN ($1, $2)),
			(SELECT count(*) FROM course_version_lessons WHERE id IN ($3, $4)),
			(SELECT count(*) FROM course_version_quizzes WHERE id = $5)`,
		publishedSectionID, draftSectionID, publishedLessonID, draftLessonID,
		publishedQuizID).Scan(&sectionCount, &lessonCount, &quizCount)
	if err != nil || sectionCount != 2 || lessonCount != 2 || quizCount != 1 {
		t.Fatalf("контент version 1 потерян: sections=%d lessons=%d quizzes=%d err=%v",
			sectionCount, lessonCount, quizCount, err)
	}

	if _, err = pool.Exec(ctx,
		`UPDATE course_version_sections SET title = 'Нельзя' WHERE id = $1`,
		publishedSectionID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("опубликованный раздел изменяем: %v", err)
	}
	if _, err = pool.Exec(ctx,
		`UPDATE course_version_sections SET title = 'Можно' WHERE id = $1`,
		draftSectionID); err != nil {
		t.Fatalf("черновой раздел нельзя изменить: %v", err)
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO course_versions (
			company_id, course_id, number, status, title, created_by_id
		) VALUES ($1, $2, 2, 'draft', 'Второй черновик', $3)`,
		companyID, draftCourseID, actorID); err == nil {
		t.Fatal("ограничение одного черновика не сработало")
	}

	// Phase 3 expands the same legacy dataset without losing assignment,
	// completion or attempt identity.
	userID, assignmentID, attemptID := uuid.New(), uuid.New(), uuid.New()
	completedAt := time.Now().UTC().Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `
		INSERT INTO assignments (
			id, company_id, course_id, assignee_type, assignee_id,
			resolved_user_ids, assigned_by_id
		) VALUES ($1, $2, $3, 'user', $4, ARRAY[$4]::uuid[], $5);
		INSERT INTO progress (
			company_id, user_id, course_id, status, completed_lesson_ids,
			started_at, completed_at
		) VALUES ($2, $4, $3, 'completed', ARRAY[$6]::uuid[], $7, $7);
		INSERT INTO quiz_attempts (
			id, company_id, quiz_id, user_id, score, passed, pending_review,
			created_at
		) VALUES ($8, $2, $9, $4, 100, true, false, $7)`,
		assignmentID, companyID, publishedCourseID, userID, actorID,
		publishedLessonID, completedAt, attemptID, publishedQuizID)
	if err != nil {
		t.Fatalf("подготовка legacy progress: %v", err)
	}

	enrollmentMigration, err := os.ReadFile(filepath.Join(
		migrationsDir, "000006_version_pinned_enrollments.up.sql",
	))
	if err != nil {
		t.Fatalf("чтение enrollment-миграции: %v", err)
	}
	if _, err = pool.Exec(ctx, string(enrollmentMigration)); err != nil {
		t.Fatalf("применение enrollment-миграции: %v", err)
	}

	var enrollmentID, assignmentVersionID, enrollmentVersionID uuid.UUID
	var progressStatus, accessStatus string
	var currentLessonID *uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT enrollment.id, assignment.course_version_id,
		       enrollment.course_version_id, enrollment.progress_status,
		       enrollment.access_status, enrollment.current_lesson_version_id
		FROM course_enrollments AS enrollment
		JOIN assignments AS assignment ON assignment.id = enrollment.source_id
		WHERE enrollment.company_id = $1
		  AND enrollment.user_id = $2
		  AND enrollment.course_id = $3`,
		companyID, userID, publishedCourseID).Scan(
		&enrollmentID, &assignmentVersionID, &enrollmentVersionID,
		&progressStatus, &accessStatus, &currentLessonID,
	)
	if err != nil {
		t.Fatalf("чтение backfill enrollment: %v", err)
	}
	if assignmentVersionID != publishedVersionID || enrollmentVersionID != publishedVersionID {
		t.Fatalf("assignment/enrollment не закреплены на version 1: assignment=%s enrollment=%s want=%s",
			assignmentVersionID, enrollmentVersionID, publishedVersionID)
	}
	if progressStatus != "completed" || accessStatus != "active" || currentLessonID != nil {
		t.Fatalf("неверное состояние enrollment: progress=%s access=%s current=%v",
			progressStatus, accessStatus, currentLessonID)
	}

	var lessonStatus string
	var lessonCompletedAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT status, completed_at
		FROM enrollment_lesson_progress
		WHERE enrollment_id = $1 AND lesson_version_id = $2`,
		enrollmentID, publishedLessonID).Scan(&lessonStatus, &lessonCompletedAt)
	if err != nil || lessonStatus != "completed" || !lessonCompletedAt.Equal(completedAt) {
		t.Fatalf("lesson progress потерян: status=%s completedAt=%s err=%v",
			lessonStatus, lessonCompletedAt, err)
	}

	var attemptEnrollmentID, attemptVersionQuizID uuid.UUID
	var answers string
	err = pool.QueryRow(ctx, `
		SELECT enrollment_id, quiz_version_id, answers::text
		FROM quiz_attempts WHERE id = $1`, attemptID).
		Scan(&attemptEnrollmentID, &attemptVersionQuizID, &answers)
	if err != nil || attemptEnrollmentID != enrollmentID ||
		attemptVersionQuizID != publishedQuizID || answers != "[]" {
		t.Fatalf("quiz attempt потерян: enrollment=%s quiz=%s answers=%q err=%v",
			attemptEnrollmentID, attemptVersionQuizID, answers, err)
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO enrollment_lesson_progress (
			company_id, enrollment_id, lesson_version_id, status
		) VALUES ($1, $2, $3, 'available')`,
		companyID, enrollmentID, draftLessonID); err == nil ||
		!strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("урок другой версии принят в progress: %v", err)
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO assignments (
			company_id, course_id, course_version_id, assignee_type,
			resolved_user_ids, assigned_by_id
		) VALUES ($1, $2, $3, 'external', '{}'::uuid[], $4)`,
		companyID, publishedCourseID, publishedVersionID, actorID); err == nil ||
		!strings.Contains(err.Error(), "external assignments") {
		t.Fatalf("новое external assignment не отклонено: %v", err)
	}

	partnerID := uuid.New()
	_, err = pool.Exec(ctx, `
		UPDATE courses
		SET owner_type = 'partner', owner_user_id = $2
		WHERE id = $1`, publishedCourseID, partnerID)
	if err != nil {
		t.Fatalf("подготовка партнёрского курса: %v", err)
	}

	partnerMigration, err := os.ReadFile(filepath.Join(
		migrationsDir, "000007_partner_courses_and_restrictions.up.sql",
	))
	if err != nil {
		t.Fatalf("чтение partner-миграции: %v", err)
	}
	if _, err = pool.Exec(ctx, string(partnerMigration)); err != nil {
		t.Fatalf("применение partner-миграции: %v", err)
	}

	// Pause and block can coexist in history; block has the effective priority.
	// Resolving it exposes the still-active pause rather than losing it.
	pauseID, blockID := uuid.New(), uuid.New()
	pauseAt := completedAt.Add(time.Hour)
	blockAt := pauseAt.Add(time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO course_restrictions (
			id, company_id, course_id, restriction_type, reason,
			created_by_id, created_at
		) VALUES
			($1, $3, $4, 'pause', 'Проверка материалов', $5, $6),
			($2, $3, $4, 'block', 'Экстренная блокировка', $5, $7)`,
		pauseID, blockID, companyID, publishedCourseID, actorID, pauseAt, blockAt)
	if err != nil {
		t.Fatalf("создание ограничений: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO course_restrictions (
			company_id, course_id, restriction_type, reason, created_by_id
		) VALUES ($1, $2, 'block', 'Дубликат', $3)`,
		companyID, publishedCourseID, actorID); err == nil {
		t.Fatal("разрешена вторая активная блокировка одного курса")
	}
	_, err = pool.Exec(ctx, `
		UPDATE courses AS course
		SET distribution_status = CASE
			WHEN EXISTS (
				SELECT 1 FROM course_restrictions
				WHERE company_id = course.company_id AND course_id = course.id
				  AND restriction_type = 'block' AND resolved_at IS NULL
			) THEN 'blocked'
			WHEN EXISTS (
				SELECT 1 FROM course_restrictions
				WHERE company_id = course.company_id AND course_id = course.id
				  AND restriction_type = 'pause' AND resolved_at IS NULL
			) THEN 'paused'
			ELSE 'active'
		END
		WHERE company_id = $1 AND id = $2`, companyID, publishedCourseID)
	if err != nil {
		t.Fatalf("пересчёт статуса распространения: %v", err)
	}
	var distributionStatus string
	if err = pool.QueryRow(ctx, `SELECT distribution_status FROM courses WHERE id = $1`,
		publishedCourseID).Scan(&distributionStatus); err != nil || distributionStatus != "blocked" {
		t.Fatalf("block не получил приоритет: status=%s err=%v", distributionStatus, err)
	}

	// A temporary block keeps both progress and the previous access state. The
	// external deadline is shifted by exactly the duration of the block.
	externalEnrollmentID := uuid.New()
	externalLearnerID := uuid.New()
	personalAccessID := uuid.New()
	activatedAt := blockAt.Add(-24 * time.Hour)
	accessUntil := activatedAt.Add(5 * 24 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO course_enrollments (
			id, company_id, course_id, course_version_id,
			learner_type, external_learner_id, source_type, source_id,
			progress_status, access_status, current_lesson_version_id,
			activated_at, access_until, started_at
		) VALUES (
			$1, $2, $3, $4, 'external', $5, 'personal_access', $6,
			'in_progress', 'active', $7, $8, $9, $8
		)`, externalEnrollmentID, companyID, publishedCourseID,
		publishedVersionID, externalLearnerID, personalAccessID,
		publishedLessonID, activatedAt, accessUntil)
	if err != nil {
		t.Fatalf("создание внешнего enrollment: %v", err)
	}
	_, err = pool.Exec(ctx, `
		UPDATE course_enrollments
		SET restriction_previous_access_status = access_status,
			access_status = 'suspended', suspended_at = $3, updated_at = $3
		WHERE company_id = $1 AND course_id = $2
		  AND access_status NOT IN ('suspended', 'closed')`,
		companyID, publishedCourseID, blockAt)
	if err != nil {
		t.Fatalf("suspend enrollment: %v", err)
	}
	resolveBlockAt := blockAt.Add(2 * time.Hour)
	_, err = pool.Exec(ctx, `
		UPDATE course_restrictions
		SET resolved_by_id = $2, resolved_at = $3,
			resolution_reason = 'Причина устранена'
		WHERE id = $1`, blockID, actorID, resolveBlockAt)
	if err != nil {
		t.Fatalf("снятие блокировки: %v", err)
	}
	_, err = pool.Exec(ctx, `
		UPDATE course_enrollments AS enrollment
		SET access_status = CASE course.lifecycle_status
				WHEN 'deleted' THEN 'closed'
				WHEN 'archived' THEN 'frozen'
				ELSE enrollment.restriction_previous_access_status
			END,
			access_until = CASE
				WHEN enrollment.learner_type = 'external'
				 AND enrollment.restriction_previous_access_status = 'active'
				 AND enrollment.access_until IS NOT NULL
				THEN enrollment.access_until + ($3::timestamptz - enrollment.suspended_at)
				ELSE enrollment.access_until
			END,
			suspended_at = NULL, restriction_previous_access_status = NULL,
			updated_at = $3
		FROM courses AS course
		WHERE enrollment.company_id = $1 AND enrollment.course_id = $2
		  AND enrollment.access_status = 'suspended'
		  AND enrollment.restriction_previous_access_status IS NOT NULL
		  AND course.company_id = enrollment.company_id
		  AND course.id = enrollment.course_id`,
		companyID, publishedCourseID, resolveBlockAt)
	if err != nil {
		t.Fatalf("восстановление enrollment после block: %v", err)
	}
	var restoredAccessStatus string
	var shiftedAccessUntil time.Time
	var previousAccessStatus *string
	err = pool.QueryRow(ctx, `
		SELECT access_status, access_until, restriction_previous_access_status
		FROM course_enrollments WHERE id = $1`, externalEnrollmentID).
		Scan(&restoredAccessStatus, &shiftedAccessUntil, &previousAccessStatus)
	if err != nil || restoredAccessStatus != "active" || previousAccessStatus != nil ||
		!shiftedAccessUntil.Equal(accessUntil.Add(2*time.Hour)) {
		t.Fatalf("неверное восстановление deadline: status=%s until=%s previous=%v err=%v",
			restoredAccessStatus, shiftedAccessUntil, previousAccessStatus, err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE course_restrictions
		SET resolution_reason = 'Переписанная история'
		WHERE id = $1`, blockID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("разрешена правка истории ограничения: %v", err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE courses AS course
		SET distribution_status = CASE
			WHEN EXISTS (
				SELECT 1 FROM course_restrictions
				WHERE company_id = course.company_id AND course_id = course.id
				  AND restriction_type = 'block' AND resolved_at IS NULL
			) THEN 'blocked'
			WHEN EXISTS (
				SELECT 1 FROM course_restrictions
				WHERE company_id = course.company_id AND course_id = course.id
				  AND restriction_type = 'pause' AND resolved_at IS NULL
			) THEN 'paused'
			ELSE 'active'
		END
		WHERE company_id = $1 AND id = $2`, companyID, publishedCourseID)
	if err != nil {
		t.Fatalf("пересчёт статуса после unblock: %v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT distribution_status FROM courses WHERE id = $1`,
		publishedCourseID).Scan(&distributionStatus); err != nil || distributionStatus != "paused" {
		t.Fatalf("активный pause потерян после unblock: status=%s err=%v", distributionStatus, err)
	}

	// A company copy has new row IDs and no operational aggregates, while the
	// immutable snapshot and its provenance survive deletion of the source.
	targetCourseID, targetVersionID, originID := uuid.New(), uuid.New(), uuid.New()
	copyKeyID := uuid.New()
	copyAt := resolveBlockAt.Add(time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, created_by_id, created_at, updated_at
		) VALUES ($1, $2, 'Независимая копия', 'draft', $3, true,
			'company', 'company', $3, $4, $4);
		INSERT INTO course_versions (
			id, company_id, course_id, number, status, title,
			sequential, created_by_id, created_at
		) VALUES ($5, $2, $1, 1, 'draft', 'Независимая копия', true, $3, $4);
		UPDATE courses SET current_draft_version_id = $5 WHERE id = $1`,
		targetCourseID, companyID, actorID, copyAt, targetVersionID)
	if err != nil {
		t.Fatalf("создание target draft: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO course_version_sections (
			id, company_id, course_version_id, stable_key, title, "order"
		)
		SELECT gen_random_uuid(), $1, $2, stable_key, title, "order"
		FROM course_version_sections WHERE course_version_id = $3;
		INSERT INTO course_version_lessons (
			id, company_id, course_version_id, section_version_id, stable_key,
			title, "order", content, source_type, source_article_id,
			source_article_version, source_template_id,
			source_template_version_id, estimated_minutes
		)
		SELECT gen_random_uuid(), $1, $2, target_section.id, source_lesson.stable_key,
			source_lesson.title, source_lesson."order", source_lesson.content,
			CASE source_lesson.source_type WHEN 'kb_link' THEN 'kb_snapshot'
				ELSE source_lesson.source_type END,
			source_lesson.source_article_id, source_lesson.source_article_version,
			source_lesson.source_template_id,
			source_lesson.source_template_version_id,
			source_lesson.estimated_minutes
		FROM course_version_lessons AS source_lesson
		JOIN course_version_sections AS source_section
		  ON source_section.id = source_lesson.section_version_id
		JOIN course_version_sections AS target_section
		  ON target_section.course_version_id = $2
		 AND target_section.stable_key = source_section.stable_key
		WHERE source_lesson.course_version_id = $3;
		WITH inserted AS (
			INSERT INTO course_version_quizzes (
				id, company_id, course_version_id, lesson_version_id,
				questions, passing_score, max_attempts
			)
			SELECT gen_random_uuid(), $1, $2, target_lesson.id,
				source_quiz.questions, source_quiz.passing_score,
				source_quiz.max_attempts
			FROM course_version_quizzes AS source_quiz
			JOIN course_version_lessons AS source_lesson
			  ON source_lesson.id = source_quiz.lesson_version_id
			JOIN course_version_lessons AS target_lesson
			  ON target_lesson.course_version_id = $2
			 AND target_lesson.stable_key = source_lesson.stable_key
			WHERE source_quiz.course_version_id = $3
			RETURNING id, lesson_version_id
		)
		UPDATE course_version_lessons AS lesson
		SET quiz_version_id = inserted.id
		FROM inserted WHERE lesson.id = inserted.lesson_version_id`,
		companyID, targetVersionID, publishedVersionID)
	if err != nil {
		t.Fatalf("глубокое копирование версии: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO course_origins (
			id, company_id, target_course_id, origin_type,
			source_course_id, source_course_version_id, source_partner_id,
			instantiated_by_id, instantiated_at, acquisition_type
		) VALUES ($1, $2, $3, 'partner_course', $4, $5, $6, $7, $8, 'free_copy');
		INSERT INTO partner_course_copy_idempotency (
			id, company_id, source_course_id, source_course_version_id,
			idempotency_key, target_course_id, target_course_version_id,
			origin_id, created_by_id, created_at
		) VALUES ($9, $2, $4, $5, 'copy-key', $3, $10, $1, $7, $8)`,
		originID, companyID, targetCourseID, publishedCourseID,
		publishedVersionID, partnerID, actorID, copyAt, copyKeyID,
		targetVersionID)
	if err != nil {
		t.Fatalf("сохранение provenance/idempotency: %v", err)
	}

	var copiedSectionID, copiedLessonID, copiedQuizID uuid.UUID
	var copiedContent, copiedQuestions string
	err = pool.QueryRow(ctx, `
		SELECT section.id, lesson.id, quiz.id,
		       lesson.content::text, quiz.questions::text
		FROM course_version_sections AS section
		JOIN course_version_lessons AS lesson
		  ON lesson.section_version_id = section.id
		JOIN course_version_quizzes AS quiz
		  ON quiz.lesson_version_id = lesson.id
		WHERE section.course_version_id = $1`, targetVersionID).
		Scan(&copiedSectionID, &copiedLessonID, &copiedQuizID,
			&copiedContent, &copiedQuestions)
	if err != nil || copiedSectionID == publishedSectionID ||
		copiedLessonID == publishedLessonID || copiedQuizID == publishedQuizID ||
		copiedContent != `{"type": "doc"}` || copiedQuestions != "[]" {
		t.Fatalf("копия не является независимым snapshot: section=%s lesson=%s quiz=%s content=%s questions=%s err=%v",
			copiedSectionID, copiedLessonID, copiedQuizID,
			copiedContent, copiedQuestions, err)
	}
	var targetAssignments, targetEnrollments, targetProgress int
	err = pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM assignments WHERE course_id = $1),
			(SELECT count(*) FROM course_enrollments WHERE course_id = $1),
			(SELECT count(*) FROM enrollment_lesson_progress AS progress
			 JOIN course_enrollments AS enrollment ON enrollment.id = progress.enrollment_id
			 WHERE enrollment.course_id = $1)`, targetCourseID).
		Scan(&targetAssignments, &targetEnrollments, &targetProgress)
	if err != nil || targetAssignments != 0 || targetEnrollments != 0 || targetProgress != 0 {
		t.Fatalf("операционные данные попали в копию: assignments=%d enrollments=%d progress=%d err=%v",
			targetAssignments, targetEnrollments, targetProgress, err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO partner_course_copy_idempotency (
			company_id, source_course_id, source_course_version_id,
			idempotency_key, target_course_id, target_course_version_id,
			origin_id, created_by_id
		) VALUES ($1, $2, $3, 'copy-key', $4, $5, $6, $7)
		ON CONFLICT (
			company_id, source_course_id, source_course_version_id, idempotency_key
		) DO NOTHING`, companyID, publishedCourseID, publishedVersionID,
		targetCourseID, targetVersionID, originID, actorID); err != nil {
		t.Fatalf("идемпотентный повтор copy завершился ошибкой: %v", err)
	}
	var idempotencyCount int
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM partner_course_copy_idempotency
		WHERE company_id = $1 AND source_course_version_id = $2
		  AND idempotency_key = 'copy-key'`, companyID, publishedVersionID).
		Scan(&idempotencyCount); err != nil || idempotencyCount != 1 {
		t.Fatalf("copy idempotency не удержала один результат: count=%d err=%v",
			idempotencyCount, err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE course_origins SET source_partner_id = $2 WHERE id = $1`,
		originID, uuid.New()); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("разрешена подмена provenance: %v", err)
	}
	_, err = pool.Exec(ctx, `
		UPDATE courses
		SET lifecycle_status = 'deleted', deleted_at = $2, deleted_by_id = $3
		WHERE id = $1`, publishedCourseID, copyAt.Add(time.Hour), partnerID)
	if err != nil {
		t.Fatalf("soft delete источника: %v", err)
	}
	var copiedLessonCount int
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM course_version_lessons WHERE course_version_id = $1`,
		targetVersionID).Scan(&copiedLessonCount); err != nil || copiedLessonCount != 1 {
		t.Fatalf("soft delete источника изменил копию: count=%d err=%v",
			copiedLessonCount, err)
	}

	otherCompanyID, otherTargetCourseID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, sequential,
			visibility, owner_type, created_by_id
		) VALUES ($1, $2, 'Чужая компания', 'draft', $3, true,
			'company', 'company', $3)`, otherTargetCourseID, otherCompanyID, actorID)
	if err != nil {
		t.Fatalf("подготовка другого tenant: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO course_origins (
			company_id, target_course_id, origin_type, source_course_id,
			source_course_version_id, source_partner_id, instantiated_by_id
		) VALUES ($1, $2, 'partner_course', $3, $4, $5, $6)`,
		otherCompanyID, otherTargetCourseID, publishedCourseID,
		publishedVersionID, partnerID, actorID); err == nil {
		t.Fatal("разрешён cross-tenant origin")
	}
}
