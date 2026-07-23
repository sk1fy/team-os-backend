-- name: ListExternalAccessLandingOutline :many
SELECT lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, section.title AS section_title,
    section."order" AS section_order, lesson.title,
    lesson."order" AS lesson_order, lesson.estimated_minutes,
    (lesson.quiz_version_id IS NOT NULL) AS has_quiz
FROM external_personal_accesses AS access
JOIN course_version_sections AS section
  ON section.company_id = access.company_id
 AND section.course_version_id = access.course_version_id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = access.company_id
 AND lesson.course_version_id = access.course_version_id
 AND lesson.section_version_id = section.id
WHERE access.company_id = sqlc.arg(company_id)
  AND access.id = sqlc.arg(personal_access_id)
  AND access.status IN ('issued', 'activated')
ORDER BY section."order", lesson."order", lesson.id;

-- name: ListExternalCampaignLandingOutline :many
SELECT lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, section.title AS section_title,
    section."order" AS section_order, lesson.title,
    lesson."order" AS lesson_order, lesson.estimated_minutes,
    (lesson.quiz_version_id IS NOT NULL) AS has_quiz
FROM external_campaigns AS campaign
JOIN course_version_sections AS section
  ON section.company_id = campaign.company_id
 AND section.course_version_id = campaign.course_version_id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = campaign.company_id
 AND lesson.course_version_id = campaign.course_version_id
 AND lesson.section_version_id = section.id
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.id = sqlc.arg(campaign_id)
ORDER BY section."order", lesson."order", lesson.id;

-- name: GetExternalEnrollmentForSession :one
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, enrollment.external_learner_id,
    enrollment.source_type, enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.frozen_at,
    enrollment.suspended_at, enrollment.created_at, enrollment.updated_at,
    version.number AS course_version_number, version.title AS course_title,
    version.description AS course_description, version.cover_file_id,
    version.sequential
FROM course_enrollments AS enrollment
JOIN external_sessions AS session
  ON session.company_id = enrollment.company_id
 AND session.external_learner_id = enrollment.external_learner_id
JOIN course_versions AS version
  ON version.company_id = enrollment.company_id
 AND version.id = enrollment.course_version_id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND session.id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND enrollment.access_status IN ('active', 'expired', 'frozen');

-- name: GetExternalEnrollmentForMutationForUpdate :one
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, enrollment.external_learner_id,
    enrollment.source_type, enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.created_at, enrollment.updated_at
FROM course_enrollments AS enrollment
JOIN external_sessions AS session
  ON session.company_id = enrollment.company_id
 AND session.external_learner_id = enrollment.external_learner_id
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND session.id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND enrollment.access_status = 'active'
  AND enrollment.access_until > sqlc.arg(now)
  AND course.lifecycle_status = 'active'
  AND course.distribution_status <> 'blocked'
FOR UPDATE OF enrollment;

-- name: ListExternalEnrollmentOutlineForSession :many
SELECT lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, section.title AS section_title,
    section."order" AS section_order, lesson.title,
    lesson."order" AS lesson_order, lesson.estimated_minutes,
    (lesson.quiz_version_id IS NOT NULL) AS has_quiz,
    progress.status AS lesson_status, progress.first_opened_at,
    progress.completed_at, COALESCE(progress.active_seconds, 0)::bigint
        AS active_seconds,
    CASE
        WHEN enrollment.access_status = 'active'
             AND enrollment.access_until > sqlc.arg(now) THEN progress.status IS NOT NULL
        WHEN enrollment.access_status IN ('expired', 'frozen')
             THEN progress.status = 'completed'
        ELSE false
    END AS content_available
FROM course_enrollments AS enrollment
JOIN external_sessions AS session
  ON session.company_id = enrollment.company_id
 AND session.external_learner_id = enrollment.external_learner_id
JOIN course_version_sections AS section
  ON section.company_id = enrollment.company_id
 AND section.course_version_id = enrollment.course_version_id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
 AND lesson.section_version_id = section.id
LEFT JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
 AND progress.lesson_version_id = lesson.id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND session.id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
ORDER BY section."order", lesson."order", lesson.id;

-- name: GetExternalLessonContentForSession :one
SELECT lesson.id, lesson.company_id, lesson.course_version_id,
    lesson.section_version_id, lesson.title, lesson."order", lesson.content,
    lesson.source_type, lesson.estimated_minutes, lesson.file_ids,
    lesson.quiz_version_id, progress.status AS lesson_status,
    progress.first_opened_at, progress.completed_at,
    progress.active_seconds, progress.last_position
FROM course_enrollments AS enrollment
JOIN external_sessions AS session
  ON session.company_id = enrollment.company_id
 AND session.external_learner_id = enrollment.external_learner_id
JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
 AND lesson.id = progress.lesson_version_id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND lesson.id = sqlc.arg(lesson_version_id)
  AND session.id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND (
      (enrollment.access_status = 'active'
       AND enrollment.access_until > sqlc.arg(now))
      OR
      (enrollment.access_status IN ('expired', 'frozen')
       AND progress.status = 'completed')
  );

-- name: GetExternalQuizForSession :one
SELECT quiz.id, quiz.company_id, quiz.course_version_id,
    quiz.lesson_version_id, quiz.questions, quiz.passing_score,
    quiz.max_attempts
FROM course_enrollments AS enrollment
JOIN external_sessions AS session
  ON session.company_id = enrollment.company_id
 AND session.external_learner_id = enrollment.external_learner_id
JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
JOIN course_version_quizzes AS quiz
  ON quiz.company_id = enrollment.company_id
 AND quiz.course_version_id = enrollment.course_version_id
 AND quiz.id = sqlc.arg(quiz_version_id)
 AND quiz.lesson_version_id = progress.lesson_version_id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND session.id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND enrollment.access_status = 'active'
  AND enrollment.access_until > sqlc.arg(now)
  AND progress.status IN ('current', 'available');

-- name: ListExternalLearnersForReport :many
SELECT learner.id, learner.company_id, learner.email,
    learner.normalized_email, learner.first_name, learner.last_name,
    learner.phone, learner.email_verified_at, learner.created_at,
    learner.updated_at,
    count(DISTINCT enrollment.id)::integer AS enrollment_count,
    count(DISTINCT enrollment.id) FILTER (
        WHERE enrollment.progress_status = 'completed'
    )::integer AS completed_enrollment_count,
    max(enrollment.last_activity_at) AS last_activity_at
FROM external_learners AS learner
JOIN course_enrollments AS enrollment
  ON enrollment.company_id = learner.company_id
 AND enrollment.external_learner_id = learner.id
 AND enrollment.learner_type = 'external'
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
WHERE learner.company_id = sqlc.arg(company_id)
  AND learner.deleted_at IS NULL
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
  AND (sqlc.narg(search)::text IS NULL
       OR learner.normalized_email ILIKE '%' || sqlc.narg(search)::text || '%'
       OR COALESCE(learner.first_name, '') ILIKE '%' || sqlc.narg(search)::text || '%'
       OR COALESCE(learner.last_name, '') ILIKE '%' || sqlc.narg(search)::text || '%')
GROUP BY learner.id
ORDER BY max(enrollment.last_activity_at) DESC NULLS LAST,
    learner.created_at DESC, learner.id DESC;

-- name: GetExternalLearnerForReport :one
SELECT learner.id, learner.company_id, learner.email,
    learner.normalized_email, learner.first_name, learner.last_name,
    learner.phone, learner.email_verified_at, learner.created_at,
    learner.updated_at,
    count(DISTINCT enrollment.id)::integer AS enrollment_count,
    count(DISTINCT enrollment.id) FILTER (
        WHERE enrollment.progress_status = 'completed'
    )::integer AS completed_enrollment_count,
    max(enrollment.last_activity_at) AS last_activity_at
FROM external_learners AS learner
JOIN course_enrollments AS enrollment
  ON enrollment.company_id = learner.company_id
 AND enrollment.external_learner_id = learner.id
 AND enrollment.learner_type = 'external'
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
WHERE learner.company_id = sqlc.arg(company_id)
  AND learner.id = sqlc.arg(learner_id)
  AND learner.deleted_at IS NULL
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
GROUP BY learner.id;

-- name: ListExternalLearnerEnrollmentsForReport :many
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, version.number AS course_version_number,
    version.title AS course_title, enrollment.source_type,
    enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.frozen_at,
    enrollment.suspended_at, enrollment.created_at,
    count(lesson.id)::integer AS lesson_count,
    count(lesson.id) FILTER (
        WHERE progress.status = 'completed'
    )::integer AS completed_lesson_count,
    CASE WHEN count(lesson.id) = 0 THEN 0
         ELSE (count(lesson.id) FILTER (WHERE progress.status = 'completed')
               * 100 / count(lesson.id))::integer END AS progress_percent,
    COALESCE(sum(progress.active_seconds), 0)::bigint AS active_seconds
FROM course_enrollments AS enrollment
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
JOIN course_versions AS version
  ON version.company_id = enrollment.company_id
 AND version.id = enrollment.course_version_id
LEFT JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
LEFT JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
 AND progress.lesson_version_id = lesson.id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.external_learner_id = sqlc.arg(learner_id)
  AND enrollment.learner_type = 'external'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
GROUP BY enrollment.id, version.number, version.title
ORDER BY enrollment.created_at DESC, enrollment.id DESC;

-- name: GetExternalEnrollmentIndividualReport :one
SELECT enrollment.id AS enrollment_id, enrollment.company_id,
    enrollment.course_id, enrollment.course_version_id,
    version.number AS course_version_number, version.title AS course_title,
    learner.id AS external_learner_id, learner.email,
    learner.first_name, learner.last_name, learner.email_verified_at,
    enrollment.source_type, enrollment.source_id,
    enrollment.attempt_number, enrollment.progress_status,
    enrollment.access_status, enrollment.current_lesson_version_id,
    access.id AS personal_access_id, access.status AS personal_access_status,
    access.issued_by_id, access.issued_at, access.deadline_days,
    access.activated_at AS access_activated_at,
    count(lesson.id)::integer AS lesson_count,
    count(lesson.id) FILTER (
        WHERE progress.status = 'completed'
    )::integer AS completed_lesson_count,
    CASE WHEN count(lesson.id) = 0 THEN 0
         ELSE (count(lesson.id) FILTER (WHERE progress.status = 'completed')
               * 100 / count(lesson.id))::integer END AS progress_percent,
    COALESCE(sum(progress.active_seconds), 0)::bigint AS active_seconds,
    attempt_stats.attempt_count, attempt_stats.best_score,
    attempt_stats.pending_review_count,
    session_stats.session_count,
    enrollment.activated_at, enrollment.access_until,
    enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.frozen_at,
    enrollment.suspended_at, enrollment.created_at, enrollment.updated_at
FROM course_enrollments AS enrollment
JOIN external_learners AS learner
  ON learner.company_id = enrollment.company_id
 AND learner.id = enrollment.external_learner_id
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
JOIN course_versions AS version
  ON version.company_id = enrollment.company_id
 AND version.id = enrollment.course_version_id
LEFT JOIN external_personal_accesses AS access
  ON access.company_id = enrollment.company_id
 AND access.id = enrollment.source_id
 AND enrollment.source_type IN ('personal_access', 'repeat_training')
LEFT JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
LEFT JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
 AND progress.lesson_version_id = lesson.id
LEFT JOIN LATERAL (
    SELECT count(*)::integer AS attempt_count,
           max(attempt.score)::integer AS best_score,
           count(*) FILTER (
               WHERE attempt.pending_review AND attempt.reviewed_at IS NULL
           )::integer AS pending_review_count
    FROM quiz_attempts AS attempt
    WHERE attempt.company_id = enrollment.company_id
      AND attempt.enrollment_id = enrollment.id
) AS attempt_stats ON true
LEFT JOIN LATERAL (
    SELECT count(*)::integer AS session_count
    FROM external_sessions AS session
    WHERE session.company_id = enrollment.company_id
      AND session.external_learner_id = enrollment.external_learner_id
) AS session_stats ON true
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
GROUP BY enrollment.id, version.number, version.title, learner.id,
    access.id, attempt_stats.attempt_count, attempt_stats.best_score,
    attempt_stats.pending_review_count, session_stats.session_count;

-- name: ListExternalEnrollmentLessonReport :many
SELECT lesson.id AS lesson_version_id, lesson.title,
    section."order" AS section_order, lesson."order" AS lesson_order,
    progress.status, progress.first_opened_at, progress.completed_at,
    COALESCE(progress.active_seconds, 0)::bigint AS active_seconds,
    progress.last_position
FROM course_enrollments AS enrollment
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
JOIN course_version_sections AS section ON section.id = lesson.section_version_id
LEFT JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
 AND progress.lesson_version_id = lesson.id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
ORDER BY section."order", lesson."order", lesson.id;

-- name: ListExternalEnrollmentQuizAttemptReport :many
SELECT attempt.id, attempt.company_id, attempt.enrollment_id,
    attempt.quiz_version_id, attempt.answers, attempt.score,
    attempt.passed, attempt.pending_review, attempt.reviewed_by_id,
    attempt.reviewed_at, attempt.review_comment, attempt.created_at
FROM quiz_attempts AS attempt
JOIN course_enrollments AS enrollment
  ON enrollment.company_id = attempt.company_id
 AND enrollment.id = attempt.enrollment_id
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
WHERE attempt.company_id = sqlc.arg(company_id)
  AND attempt.enrollment_id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
ORDER BY attempt.created_at DESC, attempt.id DESC;

-- name: ListExternalQuizResultsForSession :many
SELECT attempt.id, attempt.company_id, attempt.enrollment_id,
    attempt.quiz_version_id, attempt.answers, attempt.score,
    attempt.passed, attempt.pending_review, attempt.reviewed_at,
    attempt.review_comment, attempt.created_at
FROM quiz_attempts AS attempt
JOIN course_enrollments AS enrollment
  ON enrollment.company_id = attempt.company_id
 AND enrollment.id = attempt.enrollment_id
JOIN external_sessions AS session
  ON session.company_id = enrollment.company_id
 AND session.external_learner_id = enrollment.external_learner_id
WHERE attempt.company_id = sqlc.arg(company_id)
  AND attempt.enrollment_id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND session.id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(now)
  AND enrollment.access_status IN ('active', 'expired', 'frozen')
ORDER BY attempt.created_at DESC, attempt.id DESC;

-- name: ListExternalPersonalAccessHistory :many
SELECT history.id, history.company_id, history.personal_access_id,
    history.external_learner_id, history.enrollment_id, history.event_type,
    history.actor_type, history.actor_id, history.idempotency_key,
    history.previous_token_prefix, history.current_token_prefix,
    history.access_until_before, history.access_until_after,
    history.details, history.occurred_at
FROM external_personal_access_history AS history
JOIN external_personal_accesses AS access
  ON access.company_id = history.company_id
 AND access.id = history.personal_access_id
WHERE history.company_id = sqlc.arg(company_id)
  AND history.personal_access_id = sqlc.arg(personal_access_id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR access.partner_owner_id = sqlc.narg(partner_owner_id)::uuid)
ORDER BY history.occurred_at, history.id;

-- name: ListCourseEnrollmentAccessHistory :many
SELECT history.id, history.company_id, history.enrollment_id,
    history.from_access_status, history.to_access_status,
    history.actor_type, history.actor_id, history.reason,
    history.access_until_before, history.access_until_after,
    history.occurred_at
FROM course_enrollment_access_history AS history
JOIN course_enrollments AS enrollment
  ON enrollment.company_id = history.company_id
 AND enrollment.id = history.enrollment_id
JOIN courses AS course
  ON course.company_id = enrollment.company_id
 AND course.id = enrollment.course_id
WHERE history.company_id = sqlc.arg(company_id)
  AND history.enrollment_id = sqlc.arg(enrollment_id)
  AND enrollment.learner_type = 'external'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
ORDER BY history.occurred_at, history.id;
