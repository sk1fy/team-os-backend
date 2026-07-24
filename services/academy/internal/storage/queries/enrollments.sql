-- name: CreateInternalEnrollmentForAssignment :one
WITH enrollment AS (
    INSERT INTO course_enrollments (
        id, company_id, course_id, course_version_id, learner_type, user_id,
        source_type, source_id, attempt_number, progress_status, access_status,
        current_lesson_version_id, created_at, updated_at
    )
    SELECT sqlc.arg(id), assignment.company_id, assignment.course_id,
           assignment.course_version_id, 'user', sqlc.arg(user_id),
           'assignment', assignment.id, sqlc.arg(attempt_number),
           'not_started', 'ready', first_lesson.id,
           sqlc.arg(created_at), sqlc.arg(created_at)
    FROM assignments AS assignment
    LEFT JOIN LATERAL (
        SELECT lesson.id
        FROM course_version_lessons AS lesson
        JOIN course_version_sections AS section
          ON section.id = lesson.section_version_id
        WHERE lesson.company_id = assignment.company_id
          AND lesson.course_version_id = assignment.course_version_id
        ORDER BY section."order", lesson."order", lesson.id
        LIMIT 1
    ) AS first_lesson ON true
    WHERE assignment.company_id = sqlc.arg(company_id)
      AND assignment.id = sqlc.arg(assignment_id)
      AND assignment.assignee_type IN ('user', 'position', 'department')
      AND sqlc.arg(user_id)::uuid = ANY(assignment.resolved_user_ids)
    ON CONFLICT (company_id, source_id, user_id)
        WHERE source_type = 'assignment' AND learner_type = 'user'
    DO UPDATE SET updated_at = course_enrollments.updated_at
    RETURNING id, company_id, course_id, course_version_id, learner_type,
        user_id, external_learner_id, source_type, source_id, attempt_number,
        progress_status, access_status, current_lesson_version_id,
        activated_at, access_until, started_at, completed_at,
        last_activity_at, frozen_at, suspended_at, created_at, updated_at
), seeded AS (
    INSERT INTO enrollment_lesson_progress (
        company_id, enrollment_id, lesson_version_id, status, active_seconds
    )
    SELECT enrollment.company_id, enrollment.id, lesson.id,
           CASE
               WHEN lesson.id = enrollment.current_lesson_version_id THEN 'current'
               ELSE 'available'
           END,
           0
    FROM enrollment
    JOIN course_versions AS version ON version.id = enrollment.course_version_id
    JOIN course_version_lessons AS lesson
      ON lesson.company_id = enrollment.company_id
     AND lesson.course_version_id = enrollment.course_version_id
    WHERE enrollment.progress_status <> 'not_started'
      AND (NOT version.sequential
           OR lesson.id = enrollment.current_lesson_version_id)
    ON CONFLICT (enrollment_id, lesson_version_id) DO NOTHING
    RETURNING enrollment_id
)
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, enrollment.learner_type, enrollment.user_id,
    enrollment.external_learner_id, enrollment.source_type,
    enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.frozen_at,
    enrollment.suspended_at, enrollment.created_at, enrollment.updated_at
FROM enrollment
LEFT JOIN (SELECT count(*) AS seeded_count FROM seeded) AS seed_result ON true;

-- name: CreateSelfEnrollment :one
WITH enrollment AS (
    INSERT INTO course_enrollments (
        id, company_id, course_id, course_version_id, learner_type, user_id,
        source_type, source_id, attempt_number, progress_status, access_status,
        current_lesson_version_id, created_at, updated_at
    )
    SELECT sqlc.arg(id), course.company_id, course.id,
           course.latest_published_version_id, 'user', sqlc.arg(user_id),
           'self_enrollment', course.id, sqlc.arg(attempt_number),
           'not_started', 'ready', first_lesson.id,
           sqlc.arg(created_at), sqlc.arg(created_at)
    FROM courses AS course
    LEFT JOIN LATERAL (
        SELECT lesson.id
        FROM course_version_lessons AS lesson
        JOIN course_version_sections AS section
          ON section.id = lesson.section_version_id
        WHERE lesson.company_id = course.company_id
          AND lesson.course_version_id = course.latest_published_version_id
        ORDER BY section."order", lesson."order", lesson.id
        LIMIT 1
    ) AS first_lesson ON true
    WHERE course.company_id = sqlc.arg(company_id)
      AND course.id = sqlc.arg(course_id)
      AND course.owner_type = 'company'
      AND course.lifecycle_status = 'active'
      AND course.distribution_status = 'active'
      AND course.status = 'published'
      AND course.visibility IN ('public', 'company')
      AND course.latest_published_version_id IS NOT NULL
    ON CONFLICT (company_id, user_id, course_id)
        WHERE source_type = 'self_enrollment' AND learner_type = 'user'
    DO UPDATE SET updated_at = course_enrollments.updated_at
    RETURNING id, company_id, course_id, course_version_id, learner_type,
        user_id, external_learner_id, source_type, source_id, attempt_number,
        progress_status, access_status, current_lesson_version_id,
        activated_at, access_until, started_at, completed_at,
        last_activity_at, frozen_at, suspended_at, created_at, updated_at
)
SELECT * FROM enrollment;

-- name: GetEnrollment :one
SELECT id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at,
    restriction_previous_access_status
FROM course_enrollments
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id);

-- name: GetEnrollmentForUpdate :one
SELECT id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at,
    restriction_previous_access_status
FROM course_enrollments
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
FOR UPDATE;

-- name: GetLatestUserCourseEnrollment :one
SELECT id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at
FROM course_enrollments
WHERE company_id = sqlc.arg(company_id)
  AND learner_type = 'user'
  AND user_id = sqlc.arg(user_id)
  AND course_id = sqlc.arg(course_id)
ORDER BY attempt_number DESC, created_at DESC, id DESC
LIMIT 1;

-- name: GetCurrentUserCourseEnrollmentForUpdate :one
SELECT id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at
FROM course_enrollments
WHERE company_id = sqlc.arg(company_id)
  AND learner_type = 'user'
  AND user_id = sqlc.arg(user_id)
  AND course_id = sqlc.arg(course_id)
  AND access_status NOT IN ('revoked', 'closed', 'expired')
ORDER BY attempt_number DESC, created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: GetNextUserCourseAttemptNumber :one
SELECT COALESCE(max(attempt_number), 0)::integer + 1
FROM course_enrollments
WHERE company_id = sqlc.arg(company_id)
  AND learner_type = 'user'
  AND user_id = sqlc.arg(user_id)
  AND course_id = sqlc.arg(course_id);

-- name: ListInternalEnrollments :many
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, version.number AS version_number,
    course.title AS course_title, course.cover_url AS course_cover_url,
    enrollment.learner_type, enrollment.user_id,
    enrollment.external_learner_id, enrollment.source_type,
    enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id,
    COALESCE(progress.completed_count * 100 / NULLIF(progress.lesson_count, 0), 0)::integer
        AS progress_percent,
    progress.completed_count AS completed_lesson_count,
    progress.lesson_count AS total_lesson_count,
    COALESCE(
        assignment.due_date,
        assignment.created_at
            + make_interval(days => version.default_internal_deadline_days)
    ) AS due_date,
    COALESCE((COALESCE(
        assignment.due_date,
        assignment.created_at
            + make_interval(days => version.default_internal_deadline_days)
     ) < sqlc.arg(now)::timestamptz
        AND enrollment.progress_status <> 'completed'), false) AS overdue,
    enrollment.activated_at, enrollment.access_until, enrollment.started_at,
    enrollment.completed_at, enrollment.last_activity_at,
    enrollment.frozen_at, enrollment.suspended_at,
    enrollment.created_at, enrollment.updated_at
FROM course_enrollments AS enrollment
JOIN course_versions AS version
  ON version.company_id = enrollment.company_id
 AND version.id = enrollment.course_version_id
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
LEFT JOIN assignments AS assignment
  ON enrollment.source_type = 'assignment'
 AND assignment.company_id = enrollment.company_id
 AND assignment.id = enrollment.source_id
LEFT JOIN LATERAL (
    SELECT count(*)::integer AS lesson_count,
           count(*) FILTER (WHERE lesson_progress.status = 'completed')::integer
               AS completed_count
    FROM course_version_lessons AS lesson
    LEFT JOIN enrollment_lesson_progress AS lesson_progress
      ON lesson_progress.company_id = enrollment.company_id
     AND lesson_progress.enrollment_id = enrollment.id
     AND lesson_progress.lesson_version_id = lesson.id
    WHERE lesson.company_id = enrollment.company_id
      AND lesson.course_version_id = enrollment.course_version_id
) AS progress ON true
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.learner_type = 'user'
  AND (sqlc.narg(user_id)::uuid IS NULL
       OR enrollment.user_id = sqlc.narg(user_id)::uuid)
  AND (sqlc.narg(course_id)::uuid IS NULL
       OR enrollment.course_id = sqlc.narg(course_id)::uuid)
  AND (sqlc.narg(course_version_id)::uuid IS NULL
       OR enrollment.course_version_id = sqlc.narg(course_version_id)::uuid)
  AND (sqlc.narg(progress_status)::text IS NULL
       OR enrollment.progress_status = sqlc.narg(progress_status)::text)
  AND (sqlc.narg(access_status)::text IS NULL
       OR enrollment.access_status = sqlc.narg(access_status)::text)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR enrollment.user_id = sqlc.narg(partner_owner_id)::uuid
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
ORDER BY enrollment.created_at DESC, enrollment.id DESC;

-- name: CountInternalEnrollmentReportRows :one
WITH report_rows AS (
    SELECT enrollment.id,
        CASE
            WHEN enrollment.access_status = 'frozen' THEN 'frozen'
            WHEN COALESCE(
                COALESCE(
                    assignment.due_date,
                    assignment.created_at
                        + make_interval(days => version.default_internal_deadline_days)
                ) < sqlc.arg(now)::timestamptz
                AND enrollment.progress_status <> 'completed',
                false
            ) THEN 'overdue'
            ELSE enrollment.progress_status
        END AS report_status
    FROM course_enrollments AS enrollment
    JOIN course_versions AS version
      ON version.company_id = enrollment.company_id
     AND version.id = enrollment.course_version_id
    JOIN courses AS course
      ON course.company_id = enrollment.company_id
     AND course.id = enrollment.course_id
    LEFT JOIN assignments AS assignment
      ON enrollment.source_type = 'assignment'
     AND assignment.company_id = enrollment.company_id
     AND assignment.id = enrollment.source_id
    WHERE enrollment.company_id = sqlc.arg(company_id)
      AND enrollment.learner_type = 'user'
      AND enrollment.user_id = ANY(sqlc.arg(user_ids)::uuid[])
      AND (sqlc.narg(course_id)::uuid IS NULL
           OR enrollment.course_id = sqlc.narg(course_id)::uuid)
      AND (sqlc.narg(search)::text IS NULL
           OR course.title ILIKE '%' || sqlc.narg(search)::text || '%'
           OR enrollment.user_id = ANY(sqlc.arg(search_user_ids)::uuid[]))
)
SELECT count(*)::bigint
FROM report_rows
WHERE sqlc.narg(status)::text IS NULL
   OR report_status = sqlc.narg(status)::text;

-- name: ListInternalEnrollmentReportRows :many
WITH report_rows AS (
    SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
        enrollment.course_version_id, version.number AS version_number,
        course.title AS course_title, course.cover_url AS course_cover_url,
        enrollment.learner_type, enrollment.user_id,
        enrollment.external_learner_id, enrollment.source_type,
        enrollment.source_id, enrollment.attempt_number,
        enrollment.progress_status, enrollment.access_status,
        enrollment.current_lesson_version_id,
        COALESCE(progress.completed_count * 100 / NULLIF(progress.lesson_count, 0), 0)::integer
            AS progress_percent,
        progress.completed_count AS completed_lesson_count,
        progress.lesson_count AS total_lesson_count,
        COALESCE(
            assignment.due_date,
            assignment.created_at
                + make_interval(days => version.default_internal_deadline_days)
        ) AS due_date,
        COALESCE(
            COALESCE(
                assignment.due_date,
                assignment.created_at
                    + make_interval(days => version.default_internal_deadline_days)
            ) < sqlc.arg(now)::timestamptz
            AND enrollment.progress_status <> 'completed',
            false
        ) AS overdue,
        CASE
            WHEN enrollment.access_status = 'frozen' THEN 'frozen'
            WHEN COALESCE(
                COALESCE(
                    assignment.due_date,
                    assignment.created_at
                        + make_interval(days => version.default_internal_deadline_days)
                ) < sqlc.arg(now)::timestamptz
                AND enrollment.progress_status <> 'completed',
                false
            ) THEN 'overdue'
            ELSE enrollment.progress_status
        END AS report_status,
        enrollment.activated_at, enrollment.access_until,
        enrollment.started_at, enrollment.completed_at,
        enrollment.last_activity_at, enrollment.frozen_at,
        enrollment.suspended_at, enrollment.created_at,
        enrollment.updated_at
    FROM course_enrollments AS enrollment
    JOIN course_versions AS version
      ON version.company_id = enrollment.company_id
     AND version.id = enrollment.course_version_id
    JOIN courses AS course
      ON course.company_id = enrollment.company_id
     AND course.id = enrollment.course_id
    LEFT JOIN assignments AS assignment
      ON enrollment.source_type = 'assignment'
     AND assignment.company_id = enrollment.company_id
     AND assignment.id = enrollment.source_id
    LEFT JOIN LATERAL (
        SELECT count(*)::integer AS lesson_count,
               count(*) FILTER (
                   WHERE lesson_progress.status = 'completed'
               )::integer AS completed_count
        FROM course_version_lessons AS lesson
        LEFT JOIN enrollment_lesson_progress AS lesson_progress
          ON lesson_progress.company_id = enrollment.company_id
         AND lesson_progress.enrollment_id = enrollment.id
         AND lesson_progress.lesson_version_id = lesson.id
        WHERE lesson.company_id = enrollment.company_id
          AND lesson.course_version_id = enrollment.course_version_id
    ) AS progress ON true
    WHERE enrollment.company_id = sqlc.arg(company_id)
      AND enrollment.learner_type = 'user'
      AND enrollment.user_id = ANY(sqlc.arg(user_ids)::uuid[])
      AND (sqlc.narg(course_id)::uuid IS NULL
           OR enrollment.course_id = sqlc.narg(course_id)::uuid)
      AND (sqlc.narg(search)::text IS NULL
           OR course.title ILIKE '%' || sqlc.narg(search)::text || '%'
           OR enrollment.user_id = ANY(sqlc.arg(search_user_ids)::uuid[]))
)
SELECT id, company_id, course_id, course_version_id, version_number,
    course_title, course_cover_url, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    progress_percent, completed_lesson_count, total_lesson_count,
    due_date, overdue, activated_at, access_until, started_at,
    completed_at, last_activity_at, frozen_at, suspended_at,
    created_at, updated_at
FROM report_rows
WHERE sqlc.narg(status)::text IS NULL
   OR report_status = sqlc.narg(status)::text
ORDER BY
    CASE WHEN sqlc.arg(sort)::text = 'title_asc'
         THEN lower(course_title) END ASC,
    CASE WHEN sqlc.arg(sort)::text = 'title_desc'
         THEN lower(course_title) END DESC,
    CASE WHEN sqlc.arg(sort)::text = 'deadline_asc'
         THEN due_date END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort)::text = 'status'
         THEN report_status END ASC,
    CASE WHEN sqlc.arg(sort)::text = 'updated_asc'
         THEN last_activity_at END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort)::text = 'updated_desc'
         THEN last_activity_at END DESC NULLS LAST,
    created_at DESC,
    id DESC
LIMIT sqlc.arg(page_limit)::integer
OFFSET sqlc.arg(page_offset)::integer;

-- name: GetEnrollmentResume :one
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, version.number AS version_number,
    enrollment.learner_type, enrollment.user_id,
    enrollment.external_learner_id, enrollment.source_type,
    enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id,
    COALESCE(progress.completed_count * 100 / NULLIF(progress.lesson_count, 0), 0)::integer
        AS progress_percent,
    enrollment.activated_at, enrollment.access_until, enrollment.started_at,
    enrollment.completed_at, enrollment.last_activity_at,
    enrollment.frozen_at, enrollment.suspended_at,
    enrollment.created_at, enrollment.updated_at
FROM course_enrollments AS enrollment
JOIN course_versions AS version
  ON version.company_id = enrollment.company_id
 AND version.id = enrollment.course_version_id
LEFT JOIN LATERAL (
    SELECT count(*)::integer AS lesson_count,
           count(*) FILTER (WHERE lesson_progress.status = 'completed')::integer
               AS completed_count
    FROM course_version_lessons AS lesson
    LEFT JOIN enrollment_lesson_progress AS lesson_progress
      ON lesson_progress.company_id = enrollment.company_id
     AND lesson_progress.enrollment_id = enrollment.id
     AND lesson_progress.lesson_version_id = lesson.id
    WHERE lesson.company_id = enrollment.company_id
      AND lesson.course_version_id = enrollment.course_version_id
) AS progress ON true
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(id);

-- name: UpdateEnrollmentResumeStatus :one
UPDATE course_enrollments
SET progress_status = sqlc.arg(progress_status),
    access_status = sqlc.arg(access_status),
    current_lesson_version_id = sqlc.narg(current_lesson_version_id),
    activated_at = sqlc.narg(activated_at),
    started_at = sqlc.narg(started_at),
    completed_at = sqlc.narg(completed_at),
    last_activity_at = sqlc.narg(last_activity_at),
    updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
RETURNING id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at,
    restriction_previous_access_status;

-- name: ListEnrollmentVersionLessonsWithQuiz :many
SELECT lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, lesson.stable_key, lesson.title,
    lesson."order", section."order" AS section_order, lesson.content,
    lesson.source_type, lesson.source_article_id, lesson.source_article_version,
    lesson.source_template_id, lesson.source_template_version_id,
    lesson.estimated_minutes, lesson.quiz_version_id,
    progress.status AS progress_status,
    progress.first_opened_at, progress.completed_at,
    COALESCE(progress.active_seconds, 0)::bigint AS active_seconds,
    progress.last_position,
    (quiz.id IS NOT NULL) AS has_quiz,
    COALESCE(quiz.questions, '[]'::jsonb) AS quiz_questions,
    COALESCE(quiz.passing_score, 0)::integer AS quiz_passing_score,
    quiz.max_attempts AS quiz_max_attempts
FROM course_enrollments AS enrollment
JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
JOIN course_version_sections AS section ON section.id = lesson.section_version_id
LEFT JOIN enrollment_lesson_progress AS progress
  ON progress.enrollment_id = enrollment.id
 AND progress.lesson_version_id = lesson.id
LEFT JOIN course_version_quizzes AS quiz ON quiz.id = lesson.quiz_version_id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
ORDER BY section."order", lesson."order", lesson.id;

-- name: ListInternalEnrollmentReports :many
SELECT enrollment.id AS enrollment_id, enrollment.company_id,
    enrollment.course_id, enrollment.course_version_id,
    version.number AS version_number, version.title AS course_title,
    enrollment.user_id, enrollment.source_type, enrollment.source_id,
    enrollment.attempt_number, enrollment.progress_status,
    enrollment.access_status, enrollment.current_lesson_version_id,
    count(lesson.id)::integer AS lesson_count,
    count(lesson.id) FILTER (WHERE lesson_progress.status = 'completed')::integer
        AS completed_lesson_count,
    COALESCE(sum(lesson_progress.active_seconds), 0)::bigint AS active_seconds,
    CASE
        WHEN count(lesson.id) = 0 THEN 0
        ELSE (count(lesson.id) FILTER (WHERE lesson_progress.status = 'completed')
              * 100 / count(lesson.id))::integer
    END AS progress_percent,
    COALESCE(
        assignment.due_date,
        assignment.created_at
            + make_interval(days => version.default_internal_deadline_days)
    ) AS due_date,
    COALESCE((COALESCE(
        assignment.due_date,
        assignment.created_at
            + make_interval(days => version.default_internal_deadline_days)
     ) < sqlc.arg(now)::timestamptz
        AND enrollment.progress_status <> 'completed'), false) AS overdue,
    enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.created_at
FROM course_enrollments AS enrollment
JOIN course_versions AS version
  ON version.company_id = enrollment.company_id
 AND version.id = enrollment.course_version_id
LEFT JOIN assignments AS assignment
  ON enrollment.source_type = 'assignment'
 AND assignment.company_id = enrollment.company_id
 AND assignment.id = enrollment.source_id
LEFT JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
LEFT JOIN enrollment_lesson_progress AS lesson_progress
  ON lesson_progress.company_id = enrollment.company_id
 AND lesson_progress.enrollment_id = enrollment.id
 AND lesson_progress.lesson_version_id = lesson.id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.learner_type = 'user'
  AND (sqlc.narg(user_id)::uuid IS NULL
       OR enrollment.user_id = sqlc.narg(user_id)::uuid)
  AND (sqlc.narg(course_id)::uuid IS NULL
       OR enrollment.course_id = sqlc.narg(course_id)::uuid)
GROUP BY enrollment.id, version.number, version.title,
    version.default_internal_deadline_days,
    assignment.due_date, assignment.created_at
ORDER BY enrollment.created_at DESC, enrollment.id DESC;
