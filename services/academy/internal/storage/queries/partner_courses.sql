-- name: CreateCourseRestriction :one
INSERT INTO course_restrictions (
    id, company_id, course_id, restriction_type, reason,
    created_by_id, created_at
)
SELECT sqlc.arg(id), course.company_id, course.id,
       sqlc.arg(restriction_type), sqlc.arg(reason),
       sqlc.arg(created_by_id), sqlc.arg(created_at)
FROM courses AS course
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id)
  AND course.owner_type = 'partner'
  AND course.lifecycle_status <> 'deleted'
ON CONFLICT (company_id, course_id, restriction_type)
    WHERE resolved_at IS NULL
DO NOTHING
RETURNING id, company_id, course_id, restriction_type, reason,
    created_by_id, created_at, resolved_by_id, resolved_at,
    resolution_reason;

-- name: GetActiveCourseRestrictionForUpdate :one
SELECT id, company_id, course_id, restriction_type, reason,
    created_by_id, created_at, resolved_by_id, resolved_at,
    resolution_reason
FROM course_restrictions
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND resolved_at IS NULL
ORDER BY CASE restriction_type WHEN 'block' THEN 0 ELSE 1 END,
    created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: GetActiveCourseRestrictionByTypeForUpdate :one
SELECT id, company_id, course_id, restriction_type, reason,
    created_by_id, created_at, resolved_by_id, resolved_at,
    resolution_reason
FROM course_restrictions
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND restriction_type = sqlc.arg(restriction_type)
  AND resolved_at IS NULL
FOR UPDATE;

-- name: ResolveCourseRestriction :one
UPDATE course_restrictions
SET resolved_by_id = sqlc.arg(resolved_by_id),
    resolved_at = sqlc.arg(resolved_at),
    resolution_reason = sqlc.arg(resolution_reason)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND id = sqlc.arg(id)
  AND resolved_at IS NULL
RETURNING id, company_id, course_id, restriction_type, reason,
    created_by_id, created_at, resolved_by_id, resolved_at,
    resolution_reason;

-- name: ListCourseRestrictions :many
SELECT id, company_id, course_id, restriction_type, reason,
    created_by_id, created_at, resolved_by_id, resolved_at,
    resolution_reason
FROM course_restrictions
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
ORDER BY created_at DESC, id DESC;

-- name: RefreshPartnerCourseDistributionStatus :one
UPDATE courses AS course
SET distribution_status = CASE
        WHEN EXISTS (
            SELECT 1
            FROM course_restrictions AS restriction
            WHERE restriction.company_id = course.company_id
              AND restriction.course_id = course.id
              AND restriction.restriction_type = 'block'
              AND restriction.resolved_at IS NULL
        ) THEN 'blocked'
        WHEN EXISTS (
            SELECT 1
            FROM course_restrictions AS restriction
            WHERE restriction.company_id = course.company_id
              AND restriction.course_id = course.id
              AND restriction.restriction_type = 'pause'
              AND restriction.resolved_at IS NULL
        ) THEN 'paused'
        ELSE 'active'
    END,
    updated_at = sqlc.arg(updated_at)
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id)
  AND course.owner_type = 'partner'
  AND course.lifecycle_status <> 'deleted'
RETURNING id, company_id, title, description, cover_url, status, author_id,
    sequential, deadline_days, created_at, updated_at, visibility,
    owner_type, owner_user_id, created_by_id, lifecycle_status,
    distribution_status, archived_at, archived_by_id, deleted_at, deleted_by_id,
    current_draft_version_id, latest_published_version_id;

-- name: SuspendCourseEnrollmentsForBlock :many
UPDATE course_enrollments
SET restriction_previous_access_status = access_status,
    access_status = 'suspended',
    suspended_at = sqlc.arg(suspended_at),
    updated_at = sqlc.arg(suspended_at)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND access_status NOT IN ('suspended', 'closed')
RETURNING id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at,
    restriction_previous_access_status;

-- name: RestoreCourseEnrollmentsAfterBlock :many
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
        THEN enrollment.access_until
             + (sqlc.arg(resolved_at)::timestamptz - enrollment.suspended_at)
        ELSE enrollment.access_until
    END,
    frozen_at = CASE
        WHEN course.lifecycle_status = 'archived'
        THEN COALESCE(enrollment.frozen_at, sqlc.arg(resolved_at))
        ELSE enrollment.frozen_at
    END,
    suspended_at = NULL,
    restriction_previous_access_status = NULL,
    updated_at = sqlc.arg(resolved_at)
FROM courses AS course
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.course_id = sqlc.arg(course_id)
  AND enrollment.access_status = 'suspended'
  AND enrollment.restriction_previous_access_status IS NOT NULL
  AND course.company_id = enrollment.company_id
  AND course.id = enrollment.course_id
RETURNING enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, enrollment.learner_type,
    enrollment.user_id, enrollment.external_learner_id,
    enrollment.source_type, enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.frozen_at,
    enrollment.suspended_at, enrollment.created_at, enrollment.updated_at,
    enrollment.restriction_previous_access_status;

-- name: FreezeCourseEnrollmentsForArchive :many
UPDATE course_enrollments
SET access_status = 'frozen',
    frozen_at = sqlc.arg(frozen_at),
    updated_at = sqlc.arg(frozen_at)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND progress_status <> 'completed'
  AND access_status NOT IN ('frozen', 'suspended', 'revoked', 'closed')
RETURNING id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at,
    restriction_previous_access_status;

-- name: CloseCourseEnrollmentsForDelete :many
UPDATE course_enrollments
SET access_status = 'closed',
    suspended_at = NULL,
    restriction_previous_access_status = NULL,
    updated_at = sqlc.arg(closed_at)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND access_status <> 'closed'
RETURNING id, company_id, course_id, course_version_id, learner_type, user_id,
    external_learner_id, source_type, source_id, attempt_number,
    progress_status, access_status, current_lesson_version_id,
    activated_at, access_until, started_at, completed_at, last_activity_at,
    frozen_at, suspended_at, created_at, updated_at,
    restriction_previous_access_status;

-- name: LockPublishedPartnerCourseVersionForCopy :one
SELECT course.id AS course_id, course.company_id, course.owner_user_id,
    course.lifecycle_status, course.distribution_status,
    version.id AS version_id, version.number AS version_number,
    version.title, version.description, version.cover_file_id,
    version.cover_url, version.sequential,
    version.default_internal_deadline_days, version.content_hash
FROM courses AS course
JOIN course_versions AS version
  ON version.company_id = course.company_id
 AND version.course_id = course.id
WHERE course.company_id = sqlc.arg(company_id)
  AND course.id = sqlc.arg(course_id)
  AND course.owner_type = 'partner'
  AND course.lifecycle_status = 'active'
  AND course.distribution_status <> 'blocked'
  AND version.id = sqlc.arg(version_id)
  AND version.status = 'published'
FOR SHARE OF course, version;

-- name: CreateCourseOrigin :one
INSERT INTO course_origins (
    id, company_id, target_course_id, origin_type,
    source_course_id, source_course_version_id, source_partner_id,
    source_template_id, source_template_version_id,
    instantiated_by_id, instantiated_at, acquisition_type, entitlement_id
)
SELECT sqlc.arg(id), target.company_id, target.id, sqlc.arg(origin_type),
       sqlc.narg(source_course_id), sqlc.narg(source_course_version_id),
       sqlc.narg(source_partner_id), sqlc.narg(source_template_id),
       sqlc.narg(source_template_version_id), sqlc.arg(instantiated_by_id),
       sqlc.arg(instantiated_at), sqlc.arg(acquisition_type),
       sqlc.narg(entitlement_id)
FROM courses AS target
WHERE target.company_id = sqlc.arg(company_id)
  AND target.id = sqlc.arg(target_course_id)
  AND target.owner_type = 'company'
RETURNING id, company_id, target_course_id, origin_type,
    source_course_id, source_course_version_id, source_partner_id,
    source_template_id, source_template_version_id,
    instantiated_by_id, instantiated_at, acquisition_type, entitlement_id;

-- name: GetCourseOrigin :one
SELECT id, company_id, target_course_id, origin_type,
    source_course_id, source_course_version_id, source_partner_id,
    source_template_id, source_template_version_id,
    instantiated_by_id, instantiated_at, acquisition_type, entitlement_id
FROM course_origins
WHERE company_id = sqlc.arg(company_id)
  AND target_course_id = sqlc.arg(target_course_id);

-- name: ListPartnerCourseCopies :many
SELECT id, company_id, target_course_id, origin_type,
    source_course_id, source_course_version_id, source_partner_id,
    source_template_id, source_template_version_id,
    instantiated_by_id, instantiated_at, acquisition_type, entitlement_id
FROM course_origins
WHERE company_id = sqlc.arg(company_id)
  AND origin_type = 'partner_course'
  AND source_course_id = sqlc.arg(source_course_id)
  AND (sqlc.narg(source_course_version_id)::uuid IS NULL
       OR source_course_version_id = sqlc.narg(source_course_version_id)::uuid)
ORDER BY instantiated_at DESC, id DESC;

-- name: GetPartnerCourseCopyIdempotency :one
SELECT id, company_id, source_course_id, source_course_version_id,
    idempotency_key, target_course_id, target_course_version_id,
    origin_id, created_by_id, created_at
FROM partner_course_copy_idempotency
WHERE company_id = sqlc.arg(company_id)
  AND source_course_id = sqlc.arg(source_course_id)
  AND source_course_version_id = sqlc.arg(source_course_version_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreatePartnerCourseCopyIdempotency :one
INSERT INTO partner_course_copy_idempotency (
    id, company_id, source_course_id, source_course_version_id,
    idempotency_key, target_course_id, target_course_version_id,
    origin_id, created_by_id, created_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(source_course_id),
    sqlc.arg(source_course_version_id), sqlc.arg(idempotency_key),
    sqlc.arg(target_course_id), sqlc.arg(target_course_version_id),
    sqlc.arg(origin_id), sqlc.arg(created_by_id), sqlc.arg(created_at)
)
ON CONFLICT (
    company_id, source_course_id, source_course_version_id, idempotency_key
) DO NOTHING
RETURNING id, company_id, source_course_id, source_course_version_id,
    idempotency_key, target_course_id, target_course_version_id,
    origin_id, created_by_id, created_at;

-- name: ClonePartnerCourseVersionSections :execrows
INSERT INTO course_version_sections (
    id, company_id, course_version_id, stable_key, title, "order"
)
SELECT gen_random_uuid(), target_version.company_id, target_version.id,
       source_section.stable_key, source_section.title, source_section."order"
FROM course_version_sections AS source_section
JOIN course_versions AS source_version
  ON source_version.company_id = source_section.company_id
 AND source_version.id = source_section.course_version_id
JOIN courses AS source_course
  ON source_course.company_id = source_version.company_id
 AND source_course.id = source_version.course_id
JOIN course_versions AS target_version
  ON target_version.company_id = source_version.company_id
 AND target_version.id = sqlc.arg(target_version_id)
JOIN courses AS target_course
  ON target_course.company_id = target_version.company_id
 AND target_course.id = target_version.course_id
WHERE source_version.company_id = sqlc.arg(company_id)
  AND source_version.id = sqlc.arg(source_version_id)
  AND source_version.status = 'published'
  AND source_course.owner_type = 'partner'
  AND target_version.status = 'draft'
  AND target_course.owner_type = 'company'
ORDER BY source_section."order", source_section.id;

-- name: ClonePartnerCourseVersionLessons :execrows
INSERT INTO course_version_lessons (
    id, company_id, course_version_id, section_version_id, stable_key,
    title, "order", content, source_type, source_article_id,
    source_article_version, source_template_id, source_template_version_id,
    estimated_minutes, file_ids, kb_snapshot_id
)
SELECT gen_random_uuid(), target_version.company_id, target_version.id,
       target_section.id, source_lesson.stable_key, source_lesson.title,
       source_lesson."order", source_lesson.content,
       CASE source_lesson.source_type
           WHEN 'kb_link' THEN 'kb_snapshot'
           ELSE source_lesson.source_type
       END,
       source_lesson.source_article_id, source_lesson.source_article_version,
       source_lesson.source_template_id,
       source_lesson.source_template_version_id,
       source_lesson.estimated_minutes, source_lesson.file_ids,
       source_lesson.kb_snapshot_id
FROM course_version_lessons AS source_lesson
JOIN course_version_sections AS source_section
  ON source_section.id = source_lesson.section_version_id
JOIN course_versions AS source_version
  ON source_version.company_id = source_lesson.company_id
 AND source_version.id = source_lesson.course_version_id
JOIN courses AS source_course
  ON source_course.company_id = source_version.company_id
 AND source_course.id = source_version.course_id
JOIN course_versions AS target_version
  ON target_version.company_id = source_version.company_id
 AND target_version.id = sqlc.arg(target_version_id)
JOIN courses AS target_course
  ON target_course.company_id = target_version.company_id
 AND target_course.id = target_version.course_id
JOIN course_version_sections AS target_section
  ON target_section.course_version_id = target_version.id
 AND target_section.stable_key = source_section.stable_key
WHERE source_version.company_id = sqlc.arg(company_id)
  AND source_version.id = sqlc.arg(source_version_id)
  AND source_version.status = 'published'
  AND source_course.owner_type = 'partner'
  AND target_version.status = 'draft'
  AND target_course.owner_type = 'company'
ORDER BY target_section."order", source_lesson."order", source_lesson.id;

-- name: ClonePartnerCourseVersionQuizzes :execrows
WITH inserted AS (
    INSERT INTO course_version_quizzes (
        id, company_id, course_version_id, lesson_version_id,
        questions, passing_score, max_attempts
    )
    SELECT gen_random_uuid(), target_version.company_id, target_version.id,
           target_lesson.id, source_quiz.questions,
           source_quiz.passing_score, source_quiz.max_attempts
    FROM course_version_quizzes AS source_quiz
    JOIN course_version_lessons AS source_lesson
      ON source_lesson.id = source_quiz.lesson_version_id
    JOIN course_versions AS source_version
      ON source_version.company_id = source_quiz.company_id
     AND source_version.id = source_quiz.course_version_id
    JOIN courses AS source_course
      ON source_course.company_id = source_version.company_id
     AND source_course.id = source_version.course_id
    JOIN course_versions AS target_version
      ON target_version.company_id = source_version.company_id
     AND target_version.id = sqlc.arg(target_version_id)
    JOIN courses AS target_course
      ON target_course.company_id = target_version.company_id
     AND target_course.id = target_version.course_id
    JOIN course_version_lessons AS target_lesson
      ON target_lesson.course_version_id = target_version.id
     AND target_lesson.stable_key = source_lesson.stable_key
    WHERE source_version.company_id = sqlc.arg(company_id)
      AND source_version.id = sqlc.arg(source_version_id)
      AND source_version.status = 'published'
      AND source_course.owner_type = 'partner'
      AND target_version.status = 'draft'
      AND target_course.owner_type = 'company'
    RETURNING id, lesson_version_id
)
UPDATE course_version_lessons AS lesson
SET quiz_version_id = inserted.id
FROM inserted
WHERE lesson.id = inserted.lesson_version_id;

-- name: ListPartnerCourseGroups :many
SELECT course.owner_user_id,
    count(*)::integer AS course_count,
    count(*) FILTER (WHERE course.lifecycle_status = 'active')::integer
        AS active_course_count,
    count(*) FILTER (WHERE course.lifecycle_status = 'archived')::integer
        AS archived_course_count,
    count(*) FILTER (WHERE course.lifecycle_status = 'deleted')::integer
        AS deleted_course_count,
    count(*) FILTER (WHERE course.distribution_status = 'paused')::integer
        AS paused_course_count,
    count(*) FILTER (WHERE course.distribution_status = 'blocked')::integer
        AS blocked_course_count,
    COALESCE(sum(enrollment.enrollment_count), 0)::bigint AS enrollment_count
FROM courses AS course
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS enrollment_count
    FROM course_enrollments AS enrollment
    WHERE enrollment.company_id = course.company_id
      AND enrollment.course_id = course.id
) AS enrollment ON true
WHERE course.company_id = sqlc.arg(company_id)
  AND course.owner_type = 'partner'
GROUP BY course.owner_user_id
ORDER BY max(course.created_at) DESC, course.owner_user_id;

-- name: ListPartnerOwnedCourseReports :many
SELECT course.id AS course_id, course.owner_user_id,
    course.lifecycle_status, course.distribution_status,
    version.id AS course_version_id, version.number AS version_number,
    version.title AS course_title,
    count(enrollment.id)::integer AS enrollment_count,
    count(enrollment.id) FILTER (
        WHERE enrollment.progress_status = 'completed'
    )::integer AS completed_enrollment_count,
    count(enrollment.id) FILTER (
        WHERE enrollment.access_status = 'suspended'
    )::integer AS suspended_enrollment_count,
    COALESCE(avg(progress.progress_percent), 0)::integer AS average_progress_percent
FROM courses AS course
JOIN course_versions AS version
  ON version.company_id = course.company_id
 AND version.course_id = course.id
LEFT JOIN course_enrollments AS enrollment
  ON enrollment.company_id = course.company_id
 AND enrollment.course_id = course.id
 AND enrollment.course_version_id = version.id
LEFT JOIN LATERAL (
    SELECT CASE
        WHEN count(lesson.id) = 0 THEN 0
        ELSE (count(lesson.id) FILTER (
            WHERE lesson_progress.status = 'completed'
        ) * 100 / count(lesson.id))::integer
    END AS progress_percent
    FROM course_version_lessons AS lesson
    LEFT JOIN enrollment_lesson_progress AS lesson_progress
      ON lesson_progress.enrollment_id = enrollment.id
     AND lesson_progress.lesson_version_id = lesson.id
    WHERE lesson.course_version_id = enrollment.course_version_id
) AS progress ON enrollment.id IS NOT NULL
WHERE course.company_id = sqlc.arg(company_id)
  AND course.owner_type = 'partner'
  AND course.owner_user_id = sqlc.arg(owner_user_id)
GROUP BY course.id, course.owner_user_id, course.lifecycle_status,
    course.distribution_status, version.id, version.number, version.title
ORDER BY course.created_at DESC, version.number DESC, course.id;
