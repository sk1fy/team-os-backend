package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type cleanupStatement struct {
	entity string
	query  string
}

type commandExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// The seed command is a development reset for one tenant. Replica mode is
// transaction-local and is used only while removing immutable history and
// published snapshots; every DELETE remains company-scoped. Normal trigger
// enforcement is restored before any replacement aggregate is inserted.
var tenantCleanupStatements = []cleanupStatement{
	{"агрегаты воронки кампаний", `DELETE FROM external_campaign_funnel_daily WHERE company_id = $1`},
	{"аналитические события кампаний", `DELETE FROM analytics_events WHERE company_id = $1`},
	{"история кампаний", `DELETE FROM external_campaign_history WHERE company_id = $1`},
	{"история персональных доступов", `DELETE FROM external_personal_access_history WHERE company_id = $1`},
	{"история состояний прохождений", `DELETE FROM course_enrollment_access_history WHERE company_id = $1`},
	{"идемпотентность внешних команд", `DELETE FROM external_mutation_idempotency WHERE company_id = $1`},
	{"попытки тестов", `DELETE FROM quiz_attempts WHERE company_id = $1`},
	{"прогресс уроков", `DELETE FROM enrollment_lesson_progress WHERE company_id = $1`},
	{"персональные доступы", `DELETE FROM external_personal_accesses WHERE company_id = $1`},
	{"внешние кампании", `DELETE FROM external_campaigns WHERE company_id = $1`},
	{"проверки внешнего email", `DELETE FROM external_verification_challenges WHERE company_id = $1`},
	{"внешние сессии", `DELETE FROM external_sessions WHERE company_id = $1`},
	{"прохождения", `DELETE FROM course_enrollments WHERE company_id = $1`},
	{"внешние ученики", `DELETE FROM external_learners WHERE company_id = $1`},
	{"назначения", `DELETE FROM assignments WHERE company_id = $1`},
	{"идемпотентность создания из шаблона", `DELETE FROM course_template_instantiation_idempotency WHERE company_id = $1`},
	{"идемпотентность копирования курса", `DELETE FROM partner_course_copy_idempotency WHERE company_id = $1`},
	{"происхождение курсов", `DELETE FROM course_origins WHERE company_id = $1`},
	{"ограничения курсов", `DELETE FROM course_restrictions WHERE company_id = $1`},
	{"идемпотентность публикаций", `DELETE FROM course_version_publish_idempotency WHERE company_id = $1`},
	{"элементы заданий копирования файлов", `DELETE FROM academy_file_clone_job_items WHERE company_id = $1`},
	{"задания копирования файлов", `DELETE FROM academy_file_clone_jobs WHERE company_id = $1`},
	{"legacy-прогресс", `DELETE FROM progress WHERE company_id = $1`},
	{"аудит", `DELETE FROM audit_log WHERE company_id = $1`},
	{"outbox", `DELETE FROM outbox WHERE company_id = $1`},
	{"обработанные события", `DELETE FROM processed_events WHERE company_id = $1`},
	{"указатели версий курсов", `
		UPDATE courses
		SET current_draft_version_id = NULL, latest_published_version_id = NULL
		WHERE company_id = $1`},
	{"тесты версий", `DELETE FROM course_version_quizzes WHERE company_id = $1`},
	{"уроки версий", `DELETE FROM course_version_lessons WHERE company_id = $1`},
	{"разделы версий", `DELETE FROM course_version_sections WHERE company_id = $1`},
	{"версии курсов", `DELETE FROM course_versions WHERE company_id = $1`},
	{"legacy-тесты", `DELETE FROM quizzes WHERE company_id = $1`},
	{"legacy-уроки", `DELETE FROM lessons WHERE company_id = $1`},
	{"legacy-разделы", `DELETE FROM course_sections WHERE company_id = $1`},
	{"курсы", `DELETE FROM courses WHERE company_id = $1`},
	{"осиротевшие legacy-снимки БЗ", `
		DELETE FROM kb_article_snapshots AS snapshot
		WHERE snapshot.company_id = $1
		  AND snapshot.request_key LIKE 'legacy:%'
		  AND NOT EXISTS (
		      SELECT 1 FROM course_template_version_lessons AS lesson
		      WHERE lesson.company_id = snapshot.company_id
		        AND lesson.kb_snapshot_id = snapshot.id
		  )`},
}

// Apply replaces one company's exported Academy fixtures and creates both the
// legacy compatibility rows and their version/enrollment source of truth.
func Apply(ctx context.Context, tx pgx.Tx, dataset Dataset) error {
	if err := cleanupTenant(ctx, tx, dataset.CompanyID); err != nil {
		return err
	}
	versionIDs, err := insertLegacyAndVersionContent(ctx, tx, dataset)
	if err != nil {
		return err
	}
	if err = finalizeSeedVersions(ctx, tx, dataset.CompanyID); err != nil {
		return err
	}
	if err = insertAssignments(ctx, tx, dataset.Assignments, versionIDs); err != nil {
		return err
	}
	if err = insertLegacyProgress(ctx, tx, dataset.Progress); err != nil {
		return err
	}
	if err = projectEmployeeEnrollments(ctx, tx, dataset.CompanyID); err != nil {
		return err
	}
	if err = projectLessonProgress(ctx, tx, dataset.CompanyID); err != nil {
		return err
	}
	if err = seedSystemTemplates(ctx, tx, dataset.CompanyID); err != nil {
		return err
	}
	return nil
}

func cleanupTenant(ctx context.Context, tx commandExecutor, companyID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = 'replica'`); err != nil {
		return fmt.Errorf("включить dev-only режим сброса Academy (требуется право SET session_replication_role): %w", err)
	}
	for _, statement := range tenantCleanupStatements {
		if _, err := tx.Exec(ctx, statement.query, companyID); err != nil {
			return fmt.Errorf("очистить %s компании: %w", statement.entity, err)
		}
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = 'origin'`); err != nil {
		return fmt.Errorf("восстановить проверки Academy после очистки: %w", err)
	}
	return nil
}

func insertLegacyAndVersionContent(
	ctx context.Context,
	tx pgx.Tx,
	dataset Dataset,
) (map[uuid.UUID]uuid.UUID, error) {
	versionIDs := make(map[uuid.UUID]uuid.UUID, len(dataset.Courses))
	courses := make(map[uuid.UUID]courseRow, len(dataset.Courses))
	for _, value := range dataset.Courses {
		versionIDs[value.ID] = value.VersionID
		courses[value.ID] = value
		if _, err := tx.Exec(ctx, `
			INSERT INTO courses (
				id, company_id, title, description, cover_url, status, author_id,
				sequential, deadline_days, created_at, updated_at,
				owner_type, owner_user_id, created_by_id,
				lifecycle_status, distribution_status
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
				'company',NULL,$7,'active','active')`,
			value.ID, value.CompanyID, value.Title, value.Description, value.CoverURL,
			value.Status, value.AuthorID, value.Sequential, value.DeadlineDays,
			value.CreatedAt, value.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("вставить course %s: %w", value.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_versions (
				id, company_id, course_id, number, status, title, description,
				cover_url, sequential, default_internal_deadline_days,
				created_by_id, created_at
			) VALUES ($1,$2,$3,1,'draft',$4,$5,$6,$7,$8,$9,$10)`,
			value.VersionID, value.CompanyID, value.ID, value.Title,
			value.Description, value.CoverURL, value.Sequential,
			value.DeadlineDays, value.AuthorID, value.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("вставить version 1 курса %s: %w", value.ID, err)
		}
	}

	for _, value := range dataset.Sections {
		versionID, ok := versionIDs[value.CourseID]
		if !ok {
			return nil, fmt.Errorf("courseSection %s: курс не найден", value.ID)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_sections (id, company_id, course_id, title, "order")
			VALUES ($1,$2,$3,$4,$5)`,
			value.ID, value.CompanyID, value.CourseID, value.Title, value.Order,
		); err != nil {
			return nil, fmt.Errorf("вставить courseSection %s: %w", value.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_version_sections (
				id, company_id, course_version_id, stable_key, title, "order"
			) VALUES ($1,$2,$3,$1,$4,$5)`,
			value.ID, value.CompanyID, versionID, value.Title, value.Order,
		); err != nil {
			return nil, fmt.Errorf("вставить version section %s: %w", value.ID, err)
		}
	}

	lessonVersionIDs := make(map[uuid.UUID]uuid.UUID, len(dataset.Lessons))
	for _, value := range dataset.Lessons {
		versionID, ok := versionIDs[value.CourseID]
		if !ok {
			return nil, fmt.Errorf("lesson %s: курс не найден", value.ID)
		}
		course := courses[value.CourseID]
		lessonVersionIDs[value.ID] = versionID
		if _, err := tx.Exec(ctx, `
			INSERT INTO lessons (
				id, company_id, course_id, section_id, title, "order", content,
				source_article_id, source_article_title, source_mode, quiz_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			value.ID, value.CompanyID, value.CourseID, value.SectionID,
			value.Title, value.Order, value.Content, value.SourceArticleID,
			value.SourceArticleTitle, value.SourceMode, value.QuizID,
		); err != nil {
			return nil, fmt.Errorf("вставить lesson %s: %w", value.ID, err)
		}
		var kbSnapshotID *uuid.UUID
		if versionLessonSourceType(value) == "kb_snapshot" {
			snapshotID := seedEntityID("kb-article-snapshot", value.ID)
			kbSnapshotID = &snapshotID
			if _, err := tx.Exec(ctx, `
				INSERT INTO kb_article_snapshots (
					id, company_id, source_article_id, requested_by_id,
					request_key, title, content, content_hash, created_at
				) VALUES (
					$1,$2,$3,$4,$5,$6,$7,
					encode(digest($7::jsonb::text, 'sha256'), 'hex'),$8
				)`,
				snapshotID, value.CompanyID, value.SourceArticleID, course.AuthorID,
				"legacy:"+value.ID.String(), value.Title, value.Content, course.CreatedAt,
			); err != nil {
				return nil, fmt.Errorf("вставить legacy KB snapshot урока %s: %w", value.ID, err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_version_lessons (
				id, company_id, course_version_id, section_version_id,
				stable_key, title, "order", content, source_type, source_article_id,
				kb_snapshot_id
			) VALUES ($1,$2,$3,$4,$1,$5,$6,$7,$8,$9,$10)`,
			value.ID, value.CompanyID, versionID, value.SectionID, value.Title,
			value.Order, value.Content, versionLessonSourceType(value),
			value.SourceArticleID, kbSnapshotID,
		); err != nil {
			return nil, fmt.Errorf("вставить version lesson %s: %w", value.ID, err)
		}
	}

	for _, value := range dataset.Quizzes {
		versionID, ok := lessonVersionIDs[value.LessonID]
		if !ok {
			return nil, fmt.Errorf("quiz %s: урок не найден", value.ID)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO quizzes (
				id, company_id, lesson_id, questions, passing_score, max_attempts
			) VALUES ($1,$2,$3,$4,$5,$6)`,
			value.ID, value.CompanyID, value.LessonID, value.Questions,
			value.PassingScore, value.MaxAttempts,
		); err != nil {
			return nil, fmt.Errorf("вставить quiz %s: %w", value.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_version_quizzes (
				id, company_id, course_version_id, lesson_version_id,
				questions, passing_score, max_attempts
			) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			value.ID, value.CompanyID, versionID, value.LessonID,
			value.Questions, value.PassingScore, value.MaxAttempts,
		); err != nil {
			return nil, fmt.Errorf("вставить version quiz %s: %w", value.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE course_version_lessons
			SET quiz_version_id = $1
			WHERE company_id = $2 AND course_version_id = $3 AND id = $4`,
			value.ID, value.CompanyID, versionID, value.LessonID,
		); err != nil {
			return nil, fmt.Errorf("связать version quiz %s: %w", value.ID, err)
		}
	}
	return versionIDs, nil
}

func versionLessonSourceType(value lessonRow) string {
	if value.SourceArticleID == nil {
		return "manual"
	}
	if value.SourceMode != nil && *value.SourceMode == "link" {
		return "kb_link"
	}
	return "kb_snapshot"
}

func finalizeSeedVersions(ctx context.Context, tx pgx.Tx, companyID uuid.UUID) error {
	if _, err := tx.Exec(ctx, publishSeedVersionsSQL, companyID); err != nil {
		return fmt.Errorf("опубликовать version 1 из фикстур: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE courses AS course
		SET current_draft_version_id = CASE WHEN version.status = 'draft' THEN version.id END,
		    latest_published_version_id = CASE WHEN version.status = 'published' THEN version.id END
		FROM course_versions AS version
		WHERE course.company_id = $1
		  AND version.company_id = course.company_id
		  AND version.course_id = course.id
		  AND version.number = 1`, companyID); err != nil {
		return fmt.Errorf("связать курсы с version 1: %w", err)
	}
	return nil
}

func insertAssignments(
	ctx context.Context,
	tx pgx.Tx,
	assignments []assignmentRow,
	versionIDs map[uuid.UUID]uuid.UUID,
) error {
	for _, value := range assignments {
		if value.AssigneeType == "external" {
			continue
		}
		if err := insertAssignment(ctx, tx, value, versionIDs); err != nil {
			return err
		}
	}

	hasExternal := false
	for _, value := range assignments {
		if value.AssigneeType == "external" {
			hasExternal = true
			break
		}
	}
	if !hasExternal {
		return nil
	}
	// Post-migration writes reject the old universal external assignment. Seed
	// imports it only as read-only legacy history and never expands it to users.
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = 'replica'`); err != nil {
		return fmt.Errorf("разрешить импорт legacy external assignment: %w", err)
	}
	for _, value := range assignments {
		if value.AssigneeType != "external" {
			continue
		}
		if err := insertAssignment(ctx, tx, value, versionIDs); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = 'origin'`); err != nil {
		return fmt.Errorf("восстановить запрет legacy external assignment: %w", err)
	}
	return nil
}

func insertAssignment(
	ctx context.Context,
	tx pgx.Tx,
	value assignmentRow,
	versionIDs map[uuid.UUID]uuid.UUID,
) error {
	versionID, ok := versionIDs[value.CourseID]
	if !ok {
		return fmt.Errorf("assignment %s: курс не найден", value.ID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO assignments (
			id, company_id, course_id, course_version_id, assignee_type,
			assignee_id, invite_token, due_date, resolved_user_ids,
			assigned_by_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		value.ID, value.CompanyID, value.CourseID, versionID, value.AssigneeType,
		value.AssigneeID, value.InviteToken, value.DueDate,
		value.ResolvedUserIDs, value.AssignedByID, value.CreatedAt,
	); err != nil {
		return fmt.Errorf("вставить assignment %s: %w", value.ID, err)
	}
	return nil
}

func insertLegacyProgress(ctx context.Context, tx pgx.Tx, progressRows []progressRow) error {
	for _, value := range progressRows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO progress (
				company_id, user_id, course_id, status,
				completed_lesson_ids, started_at, completed_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			value.CompanyID, value.UserID, value.CourseID, value.Status,
			value.CompletedLessonIDs, value.StartedAt, value.CompletedAt,
		); err != nil {
			return fmt.Errorf("вставить progress %s/%s: %w", value.UserID, value.CourseID, err)
		}
	}
	return nil
}

func projectEmployeeEnrollments(ctx context.Context, tx pgx.Tx, companyID uuid.UUID) error {
	if _, err := tx.Exec(ctx, progressEnrollmentsSQL, companyID); err != nil {
		return fmt.Errorf("создать прохождения из legacy progress: %w", err)
	}
	if _, err := tx.Exec(ctx, assignmentEnrollmentsSQL, companyID); err != nil {
		return fmt.Errorf("создать прохождения из назначений: %w", err)
	}
	return nil
}

func projectLessonProgress(ctx context.Context, tx pgx.Tx, companyID uuid.UUID) error {
	if _, err := tx.Exec(ctx, lessonProgressSQL, companyID); err != nil {
		return fmt.Errorf("создать прогресс уроков прохождений: %w", err)
	}
	return nil
}

func seedSystemTemplates(ctx context.Context, tx pgx.Tx, companyID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT academy_seed_system_templates($1)`, companyID); err != nil {
		return fmt.Errorf("создать системные шаблоны Academy для компании: %w", err)
	}
	return nil
}

const publishSeedVersionsSQL = `
UPDATE course_versions AS version
SET status = 'published',
    published_by_id = course.author_id,
    published_at = course.updated_at,
    content_hash = encode(digest(jsonb_build_object(
        'title', version.title,
        'description', version.description,
        'coverFileId', version.cover_file_id,
        'coverUrl', version.cover_url,
        'sequential', version.sequential,
        'defaultInternalDeadlineDays', version.default_internal_deadline_days,
        'sections', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'stableKey', section.stable_key,
                'title', section.title,
                'order', section."order"
            ) ORDER BY section."order", section.stable_key)
            FROM course_version_sections AS section
            WHERE section.course_version_id = version.id
        ), '[]'::jsonb),
        'lessons', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'stableKey', lesson.stable_key,
                'sectionStableKey', section.stable_key,
                'title', lesson.title,
                'order', lesson."order",
                'content', lesson.content,
                'sourceType', lesson.source_type,
                'sourceArticleId', lesson.source_article_id,
                'sourceArticleVersion', lesson.source_article_version,
                'sourceTemplateId', lesson.source_template_id,
                'sourceTemplateVersionId', lesson.source_template_version_id,
                'estimatedMinutes', lesson.estimated_minutes,
                'fileIds', lesson.file_ids
            ) ORDER BY section."order", lesson."order", lesson.stable_key)
            FROM course_version_lessons AS lesson
            JOIN course_version_sections AS section ON section.id = lesson.section_version_id
            WHERE lesson.course_version_id = version.id
        ), '[]'::jsonb),
        'quizzes', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'lessonStableKey', lesson.stable_key,
                'questions', quiz.questions,
                'passingScore', quiz.passing_score,
                'maxAttempts', quiz.max_attempts
            ) ORDER BY lesson.stable_key)
            FROM course_version_quizzes AS quiz
            JOIN course_version_lessons AS lesson ON lesson.id = quiz.lesson_version_id
            WHERE quiz.course_version_id = version.id
        ), '[]'::jsonb)
    )::text, 'sha256'), 'hex')
FROM courses AS course
WHERE version.company_id = $1
  AND course.company_id = version.company_id
  AND course.id = version.course_id
  AND course.status = 'published'
  AND version.number = 1
  AND version.status = 'draft'`

// These projections intentionally mirror migration 000006. External legacy
// assignments are excluded; only resolved employees become enrollments.
const progressEnrollmentsSQL = `
INSERT INTO course_enrollments (
    company_id, course_id, course_version_id, learner_type, user_id,
    source_type, source_id, attempt_number, progress_status, access_status,
    current_lesson_version_id, started_at, completed_at, last_activity_at,
    created_at, updated_at
)
SELECT legacy_progress.company_id,
       legacy_progress.course_id,
       version.id,
       'user',
       legacy_progress.user_id,
       CASE WHEN source_assignment.id IS NULL THEN 'legacy' ELSE 'assignment' END,
       source_assignment.id,
       1,
       CASE
           WHEN lesson_stats.lesson_count > 0 AND next_lesson.id IS NULL THEN 'completed'
           WHEN legacy_progress.status IN ('in_progress', 'completed')
                OR legacy_progress.started_at IS NOT NULL
                OR cardinality(legacy_progress.completed_lesson_ids) > 0 THEN 'in_progress'
           ELSE 'not_started'
       END,
       CASE
           WHEN legacy_progress.status = 'not_started'
                AND legacy_progress.started_at IS NULL
                AND cardinality(legacy_progress.completed_lesson_ids) = 0 THEN 'ready'
           ELSE 'active'
       END,
       next_lesson.id,
       legacy_progress.started_at,
       CASE
           WHEN lesson_stats.lesson_count > 0 AND next_lesson.id IS NULL
               THEN COALESCE(legacy_progress.completed_at,
                             legacy_progress.started_at, version.created_at)
       END,
       COALESCE(legacy_progress.completed_at, legacy_progress.started_at, version.created_at),
       COALESCE(legacy_progress.started_at, version.created_at),
       COALESCE(legacy_progress.completed_at, legacy_progress.started_at, version.created_at)
FROM progress AS legacy_progress
JOIN course_versions AS version
  ON version.company_id = legacy_progress.company_id
 AND version.course_id = legacy_progress.course_id
 AND version.number = 1
LEFT JOIN LATERAL (
    SELECT assignment.id
    FROM assignments AS assignment
    WHERE assignment.company_id = legacy_progress.company_id
      AND assignment.course_id = legacy_progress.course_id
      AND legacy_progress.user_id = ANY(assignment.resolved_user_ids)
      AND assignment.assignee_type IN ('user', 'position', 'department')
    ORDER BY assignment.created_at, assignment.id
    LIMIT 1
) AS source_assignment ON true
LEFT JOIN LATERAL (
    SELECT lesson.id
    FROM course_version_lessons AS lesson
    JOIN course_version_sections AS section ON section.id = lesson.section_version_id
    WHERE lesson.company_id = legacy_progress.company_id
      AND lesson.course_version_id = version.id
      AND NOT (lesson.id = ANY(legacy_progress.completed_lesson_ids))
    ORDER BY section."order", lesson."order", lesson.id
    LIMIT 1
) AS next_lesson ON true
CROSS JOIN LATERAL (
    SELECT count(*)::integer AS lesson_count
    FROM course_version_lessons AS lesson
    WHERE lesson.company_id = legacy_progress.company_id
      AND lesson.course_version_id = version.id
) AS lesson_stats
WHERE legacy_progress.company_id = $1`

const assignmentEnrollmentsSQL = `
WITH assignment_users AS (
    SELECT DISTINCT ON (assignment.company_id, assignment.course_id, member.user_id)
           assignment.id AS assignment_id,
           assignment.company_id,
           assignment.course_id,
           assignment.course_version_id,
           member.user_id,
           assignment.created_at
    FROM assignments AS assignment
    CROSS JOIN LATERAL unnest(assignment.resolved_user_ids) AS member(user_id)
    WHERE assignment.company_id = $1
      AND assignment.assignee_type IN ('user', 'position', 'department')
    ORDER BY assignment.company_id, assignment.course_id, member.user_id,
             assignment.created_at, assignment.id
)
INSERT INTO course_enrollments (
    company_id, course_id, course_version_id, learner_type, user_id,
    source_type, source_id, attempt_number, progress_status, access_status,
    current_lesson_version_id, created_at, updated_at
)
SELECT member.company_id,
       member.course_id,
       member.course_version_id,
       'user',
       member.user_id,
       'assignment',
       member.assignment_id,
       1,
       'not_started',
       'ready',
       first_lesson.id,
       member.created_at,
       member.created_at
FROM assignment_users AS member
LEFT JOIN LATERAL (
    SELECT lesson.id
    FROM course_version_lessons AS lesson
    JOIN course_version_sections AS section ON section.id = lesson.section_version_id
    WHERE lesson.company_id = member.company_id
      AND lesson.course_version_id = member.course_version_id
    ORDER BY section."order", lesson."order", lesson.id
    LIMIT 1
) AS first_lesson ON true
WHERE NOT EXISTS (
    SELECT 1
    FROM course_enrollments AS existing
    WHERE existing.company_id = member.company_id
      AND existing.course_id = member.course_id
      AND existing.user_id = member.user_id
)`

const lessonProgressSQL = `
INSERT INTO enrollment_lesson_progress (
    company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds
)
SELECT enrollment.company_id,
       enrollment.id,
       lesson.id,
       CASE
           WHEN lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[])) THEN 'completed'
           WHEN lesson.id = enrollment.current_lesson_version_id THEN 'current'
           ELSE 'available'
       END,
       CASE
           WHEN lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
                OR lesson.id = enrollment.current_lesson_version_id THEN enrollment.started_at
       END,
       CASE
           WHEN lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
               THEN COALESCE(enrollment.completed_at, enrollment.last_activity_at,
                             enrollment.started_at, enrollment.created_at)
       END,
       0
FROM course_enrollments AS enrollment
JOIN course_versions AS version ON version.id = enrollment.course_version_id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
LEFT JOIN progress AS legacy_progress
  ON legacy_progress.company_id = enrollment.company_id
 AND legacy_progress.course_id = enrollment.course_id
 AND legacy_progress.user_id = enrollment.user_id
WHERE enrollment.company_id = $1
  AND enrollment.progress_status <> 'not_started'
  AND (NOT version.sequential
       OR lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
       OR lesson.id = enrollment.current_lesson_version_id)`
