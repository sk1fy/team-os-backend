-- name: CreateExternalPersonalAccess :one
INSERT INTO external_personal_accesses (
    id, company_id, course_id, course_version_id, partner_owner_id,
    external_learner_id, expected_email, normalized_expected_email,
    recipient_first_name, recipient_last_name, deadline_days, status,
    token_hash, token_prefix, root_access_id, repeat_of_access_id,
    attempt_number, issuance_idempotency_key, issued_by_id,
    issued_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(course_id),
    sqlc.arg(course_version_id), sqlc.arg(partner_owner_id),
    sqlc.narg(external_learner_id), sqlc.arg(expected_email),
    sqlc.arg(normalized_expected_email), sqlc.narg(recipient_first_name),
    sqlc.narg(recipient_last_name), sqlc.arg(deadline_days), 'issued',
    sqlc.arg(token_hash), sqlc.arg(token_prefix), sqlc.arg(root_access_id),
    sqlc.narg(repeat_of_access_id), sqlc.arg(attempt_number),
    sqlc.arg(issuance_idempotency_key), sqlc.arg(issued_by_id),
    sqlc.arg(issued_at), sqlc.arg(issued_at)
)
ON CONFLICT (company_id, partner_owner_id, issuance_idempotency_key)
DO UPDATE SET id = external_personal_accesses.id
RETURNING id, company_id, course_id, course_version_id, partner_owner_id,
    external_learner_id, expected_email, normalized_expected_email,
    recipient_first_name, recipient_last_name, deadline_days, status,
    token_hash, token_prefix, enrollment_id, root_access_id,
    repeat_of_access_id, attempt_number, issuance_idempotency_key,
    issued_by_id, issued_at, activated_at, token_rotated_at,
    revoked_at, closed_at, updated_at;

-- name: GetExternalPersonalAccess :one
SELECT access.id, access.company_id, access.course_id,
    access.course_version_id, access.partner_owner_id,
    access.external_learner_id, access.expected_email,
    access.normalized_expected_email, access.recipient_first_name,
    access.recipient_last_name, access.deadline_days, access.status,
    access.token_prefix, access.enrollment_id, access.root_access_id,
    access.repeat_of_access_id, access.attempt_number,
    access.issuance_idempotency_key, access.issued_by_id, access.issued_at,
    access.activated_at, access.token_rotated_at, access.revoked_at,
    access.closed_at, access.updated_at,
    course.title AS course_title, course.lifecycle_status,
    course.distribution_status, version.number AS course_version_number,
    version.title AS course_version_title, version.status AS course_version_status
FROM external_personal_accesses AS access
JOIN courses AS course
  ON course.company_id = access.company_id AND course.id = access.course_id
JOIN course_versions AS version
  ON version.company_id = access.company_id
 AND version.course_id = access.course_id
 AND version.id = access.course_version_id
WHERE access.company_id = sqlc.arg(company_id)
  AND access.id = sqlc.arg(id);

-- name: GetExternalPersonalAccessForUpdate :one
SELECT access.id, access.company_id, access.course_id,
    access.course_version_id, access.partner_owner_id,
    access.external_learner_id, access.expected_email,
    access.normalized_expected_email, access.recipient_first_name,
    access.recipient_last_name, access.deadline_days, access.status,
    access.token_hash, access.token_prefix, access.enrollment_id,
    access.root_access_id, access.repeat_of_access_id, access.attempt_number,
    access.issuance_idempotency_key, access.issued_by_id, access.issued_at,
    access.activated_at, access.token_rotated_at, access.revoked_at,
    access.closed_at, access.updated_at
FROM external_personal_accesses AS access
WHERE access.company_id = sqlc.arg(company_id)
  AND access.id = sqlc.arg(id)
FOR UPDATE;

-- name: GetExternalPersonalAccessByTokenHash :one
SELECT access.id, access.company_id, access.course_id,
    access.course_version_id, access.partner_owner_id,
    access.external_learner_id, access.expected_email,
    access.normalized_expected_email, access.recipient_first_name,
    access.recipient_last_name, access.deadline_days, access.status,
    access.token_hash, access.token_prefix, access.enrollment_id,
    access.root_access_id, access.repeat_of_access_id, access.attempt_number,
    access.issued_by_id, access.issued_at, access.activated_at,
    access.token_rotated_at, access.revoked_at, access.closed_at,
    access.updated_at, course.title AS course_title,
    course.lifecycle_status, course.distribution_status,
    version.number AS course_version_number,
    version.title AS course_version_title,
    version.description AS course_version_description,
    version.cover_file_id, version.status AS course_version_status,
    version.sequential
FROM external_personal_accesses AS access
JOIN courses AS course
  ON course.company_id = access.company_id AND course.id = access.course_id
JOIN course_versions AS version
  ON version.company_id = access.company_id
 AND version.course_id = access.course_id
 AND version.id = access.course_version_id
WHERE access.company_id = sqlc.arg(company_id)
  AND access.token_hash = sqlc.arg(token_hash);

-- Resolve is the only tenant-bootstrap query. token_hash is globally unique;
-- every operation after this lookup must use the returned company_id.
-- name: ResolveExternalPersonalAccessByTokenHash :one
SELECT access.id, access.company_id, access.course_id,
    access.course_version_id, access.partner_owner_id,
    access.external_learner_id, access.expected_email,
    access.normalized_expected_email, access.recipient_first_name,
    access.recipient_last_name, access.deadline_days, access.status,
    access.token_hash, access.token_prefix, access.enrollment_id,
    access.root_access_id, access.repeat_of_access_id, access.attempt_number,
    access.issued_by_id, access.issued_at, access.activated_at,
    access.token_rotated_at, access.revoked_at, access.closed_at,
    access.updated_at, course.title AS course_title,
    course.lifecycle_status, course.distribution_status,
    version.number AS course_version_number,
    version.title AS course_version_title,
    version.description AS course_version_description,
    version.cover_file_id, version.status AS course_version_status,
    version.sequential,
    COALESCE(enrollment.access_until <= sqlc.arg(now), false)
        AS deadline_expired
FROM external_personal_accesses AS access
JOIN courses AS course
  ON course.company_id = access.company_id AND course.id = access.course_id
JOIN course_versions AS version
  ON version.company_id = access.company_id
 AND version.course_id = access.course_id
 AND version.id = access.course_version_id
LEFT JOIN course_enrollments AS enrollment
  ON enrollment.company_id = access.company_id
 AND enrollment.id = access.enrollment_id
WHERE access.token_hash = sqlc.arg(token_hash)
  AND access.status IN ('issued', 'activated');

-- name: ListExternalPersonalAccesses :many
SELECT access.id, access.company_id, access.course_id,
    access.course_version_id, access.partner_owner_id,
    access.external_learner_id, access.expected_email,
    access.normalized_expected_email, access.recipient_first_name,
    access.recipient_last_name, access.deadline_days, access.status,
    access.token_prefix, access.enrollment_id, access.root_access_id,
    access.repeat_of_access_id, access.attempt_number,
    access.issued_by_id, access.issued_at, access.activated_at,
    access.token_rotated_at, access.revoked_at, access.closed_at,
    access.updated_at, course.title AS course_title,
    version.number AS course_version_number
FROM external_personal_accesses AS access
JOIN courses AS course
  ON course.company_id = access.company_id AND course.id = access.course_id
JOIN course_versions AS version ON version.id = access.course_version_id
WHERE access.company_id = sqlc.arg(company_id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR access.partner_owner_id = sqlc.narg(partner_owner_id)::uuid)
  AND (sqlc.narg(course_id)::uuid IS NULL
       OR access.course_id = sqlc.narg(course_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL
       OR access.status = sqlc.narg(status)::text)
ORDER BY access.issued_at DESC, access.id DESC;

-- name: BindExternalPersonalAccessLearner :one
UPDATE external_personal_accesses
SET external_learner_id = sqlc.arg(external_learner_id),
    updated_at = sqlc.arg(updated_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status IN ('issued', 'activated')
  AND normalized_expected_email = sqlc.arg(normalized_email)
  AND (external_learner_id IS NULL
       OR external_learner_id = sqlc.arg(external_learner_id))
RETURNING id, company_id, course_id, course_version_id, partner_owner_id,
    external_learner_id, expected_email, normalized_expected_email,
    recipient_first_name, recipient_last_name, deadline_days, status,
    token_prefix, enrollment_id, root_access_id, repeat_of_access_id,
    attempt_number, issued_by_id, issued_at, activated_at,
    token_rotated_at, revoked_at, closed_at, updated_at;

-- name: ActivateExternalPersonalAccess :one
WITH target AS (
    SELECT access.id, access.company_id, access.course_id,
           access.course_version_id, access.deadline_days,
           access.attempt_number, access.status, access.external_learner_id,
           version.sequential,
           first_lesson.id AS first_lesson_id
    FROM external_personal_accesses AS access
    JOIN courses AS course
      ON course.company_id = access.company_id AND course.id = access.course_id
    JOIN course_versions AS version
      ON version.company_id = access.company_id
     AND version.course_id = access.course_id
     AND version.id = access.course_version_id
    LEFT JOIN LATERAL (
        SELECT lesson.id
        FROM course_version_lessons AS lesson
        JOIN course_version_sections AS section
          ON section.id = lesson.section_version_id
        WHERE lesson.company_id = access.company_id
          AND lesson.course_version_id = access.course_version_id
        ORDER BY section."order", lesson."order", lesson.id
        LIMIT 1
    ) AS first_lesson ON true
    WHERE access.company_id = sqlc.arg(company_id)
      AND access.id = sqlc.arg(personal_access_id)
      AND access.status IN ('issued', 'activated')
      AND access.external_learner_id = sqlc.arg(external_learner_id)
      AND version.status = 'published'
      AND course.lifecycle_status = 'active'
      AND course.distribution_status <> 'blocked'
      AND (access.status = 'activated' OR course.distribution_status = 'active')
    FOR UPDATE OF access
), inserted AS (
    INSERT INTO course_enrollments (
        id, company_id, course_id, course_version_id, learner_type,
        external_learner_id, source_type, source_id, attempt_number,
        progress_status, access_status, current_lesson_version_id,
        activated_at, access_until, started_at, last_activity_at,
        created_at, updated_at
    )
    SELECT sqlc.arg(enrollment_id), target.company_id, target.course_id,
           target.course_version_id, 'external',
           sqlc.arg(external_learner_id),
           CASE WHEN target.attempt_number = 1
                THEN 'personal_access' ELSE 'repeat_training' END,
           target.id, target.attempt_number, 'in_progress', 'active',
           target.first_lesson_id, sqlc.arg(activated_at),
           sqlc.arg(activated_at)
               + make_interval(days => target.deadline_days::integer),
           sqlc.arg(activated_at), sqlc.arg(activated_at),
           sqlc.arg(activated_at), sqlc.arg(activated_at)
    FROM target
    ON CONFLICT (company_id, source_id)
        WHERE learner_type = 'external'
          AND source_type IN ('personal_access', 'repeat_training')
    DO UPDATE SET updated_at = course_enrollments.updated_at
    RETURNING id, company_id, course_id, course_version_id,
        external_learner_id, source_type, source_id, attempt_number,
        progress_status, access_status, current_lesson_version_id,
        activated_at, access_until, started_at, completed_at,
        last_activity_at, frozen_at, suspended_at, created_at, updated_at
), linked AS (
    UPDATE external_personal_accesses AS access
    SET status = 'activated',
        enrollment_id = inserted.id,
        activated_at = COALESCE(access.activated_at, inserted.activated_at),
        updated_at = GREATEST(access.updated_at, sqlc.arg(activated_at))
    FROM inserted
    WHERE access.company_id = inserted.company_id
      AND access.id = inserted.source_id
      AND access.external_learner_id = inserted.external_learner_id
    RETURNING access.id AS personal_access_id
), seeded AS (
    INSERT INTO enrollment_lesson_progress (
        company_id, enrollment_id, lesson_version_id, status,
        first_opened_at, active_seconds
    )
    SELECT inserted.company_id, inserted.id, lesson.id,
           CASE WHEN lesson.id = inserted.current_lesson_version_id
                THEN 'current' ELSE 'available' END,
           CASE WHEN lesson.id = inserted.current_lesson_version_id
                THEN inserted.activated_at END,
           0
    FROM inserted
    JOIN course_versions AS version ON version.id = inserted.course_version_id
    JOIN course_version_lessons AS lesson
      ON lesson.company_id = inserted.company_id
     AND lesson.course_version_id = inserted.course_version_id
    WHERE NOT version.sequential
       OR lesson.id = inserted.current_lesson_version_id
    ON CONFLICT (enrollment_id, lesson_version_id) DO NOTHING
    RETURNING enrollment_id
)
SELECT inserted.id, inserted.company_id, inserted.course_id,
    inserted.course_version_id, inserted.external_learner_id,
    inserted.source_type, inserted.source_id, inserted.attempt_number,
    inserted.progress_status, inserted.access_status,
    inserted.current_lesson_version_id, inserted.activated_at,
    inserted.access_until, inserted.started_at, inserted.completed_at,
    inserted.last_activity_at, inserted.frozen_at, inserted.suspended_at,
    inserted.created_at, inserted.updated_at
FROM inserted
JOIN linked ON linked.personal_access_id = inserted.source_id
LEFT JOIN (SELECT count(*) AS seeded_count FROM seeded) AS seed_result ON true;

-- name: RotateExternalPersonalAccessToken :one
UPDATE external_personal_accesses
SET token_hash = sqlc.arg(token_hash),
    token_prefix = sqlc.arg(token_prefix),
    token_rotated_at = sqlc.arg(rotated_at),
    updated_at = sqlc.arg(rotated_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND partner_owner_id = sqlc.arg(partner_owner_id)
  AND status IN ('issued', 'activated')
RETURNING id, company_id, course_id, course_version_id, partner_owner_id,
    external_learner_id, expected_email, normalized_expected_email,
    recipient_first_name, recipient_last_name, deadline_days, status,
    token_prefix, enrollment_id, root_access_id, repeat_of_access_id,
    attempt_number, issued_by_id, issued_at, activated_at,
    token_rotated_at, revoked_at, closed_at, updated_at;

-- name: ExtendExternalPersonalAccess :one
WITH extended AS (
    UPDATE course_enrollments AS enrollment
    SET access_until = enrollment.access_until
            + make_interval(days => sqlc.arg(extension_days)::integer),
        access_status = CASE
            WHEN enrollment.access_status = 'expired' THEN 'active'
            ELSE enrollment.access_status
        END,
        updated_at = sqlc.arg(extended_at)
    FROM external_personal_accesses AS access, courses AS course
    WHERE access.company_id = sqlc.arg(company_id)
      AND access.id = sqlc.arg(personal_access_id)
      AND access.partner_owner_id = sqlc.arg(partner_owner_id)
      AND access.status = 'activated'
      AND access.enrollment_id = enrollment.id
      AND enrollment.company_id = access.company_id
      AND enrollment.external_learner_id = access.external_learner_id
      AND enrollment.access_until IS NOT NULL
      AND enrollment.access_status IN ('active', 'expired', 'frozen')
      AND sqlc.arg(extension_days)::integer BETWEEN 1 AND 7
      AND course.company_id = access.company_id
      AND course.id = access.course_id
      AND course.lifecycle_status <> 'deleted'
      AND course.distribution_status <> 'blocked'
    RETURNING enrollment.id, enrollment.company_id,
        enrollment.access_status, enrollment.access_until,
        enrollment.updated_at
), touched AS (
    UPDATE external_personal_accesses AS access
    SET updated_at = sqlc.arg(extended_at)
    FROM extended
    WHERE access.company_id = extended.company_id
      AND access.enrollment_id = extended.id
    RETURNING access.id AS personal_access_id
)
SELECT extended.id AS enrollment_id, extended.company_id,
    touched.personal_access_id, extended.access_status,
    extended.access_until, extended.updated_at
FROM extended JOIN touched ON true;

-- name: RevokeExternalPersonalAccess :one
WITH target AS (
    SELECT access.id, access.company_id, access.enrollment_id
    FROM external_personal_accesses AS access
    WHERE access.company_id = sqlc.arg(company_id)
      AND access.id = sqlc.arg(id)
      AND access.partner_owner_id = sqlc.arg(partner_owner_id)
      AND access.status IN ('issued', 'activated')
    FOR UPDATE OF access
), revoked_enrollment AS (
    UPDATE course_enrollments AS enrollment
    SET access_status = 'revoked',
        updated_at = sqlc.arg(revoked_at)
    FROM target
    WHERE enrollment.company_id = target.company_id
      AND enrollment.id = target.enrollment_id
    RETURNING enrollment.id
)
UPDATE external_personal_accesses AS access
SET status = 'revoked',
    revoked_at = sqlc.arg(revoked_at),
    updated_at = sqlc.arg(revoked_at)
FROM target
LEFT JOIN revoked_enrollment ON true
WHERE access.company_id = target.company_id
  AND access.id = target.id
RETURNING access.id, access.company_id, access.course_id,
    access.course_version_id, access.partner_owner_id,
    access.external_learner_id, access.expected_email,
    access.normalized_expected_email, access.recipient_first_name,
    access.recipient_last_name, access.deadline_days, access.status,
    access.token_prefix, access.enrollment_id, access.root_access_id,
    access.repeat_of_access_id, access.attempt_number,
    access.issued_by_id, access.issued_at, access.activated_at,
    access.token_rotated_at, access.revoked_at, access.closed_at,
    access.updated_at;

-- name: CloseExternalPersonalAccessesForCourse :many
UPDATE external_personal_accesses
SET status = 'closed', closed_at = sqlc.arg(changed_at),
    updated_at = sqlc.arg(changed_at)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND status <> 'closed'
RETURNING id;

-- name: CloseExternalPersonalAccessForRepeat :one
UPDATE external_personal_accesses
SET status = 'closed',
    closed_at = sqlc.arg(closed_at),
    updated_at = sqlc.arg(closed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND partner_owner_id = sqlc.arg(partner_owner_id)
  AND status = 'activated'
  AND enrollment_id IS NOT NULL
RETURNING id, company_id, course_id, course_version_id, partner_owner_id,
    external_learner_id, expected_email, normalized_expected_email,
    recipient_first_name, recipient_last_name, deadline_days, status,
    token_prefix, enrollment_id, root_access_id, repeat_of_access_id,
    attempt_number, issued_by_id, issued_at, activated_at,
    token_rotated_at, revoked_at, closed_at, updated_at;

-- name: GetPersonalAccessRootForUpdate :one
SELECT id, company_id, course_id, course_version_id, partner_owner_id,
    external_learner_id, expected_email, normalized_expected_email,
    recipient_first_name, recipient_last_name, deadline_days, status,
    enrollment_id, root_access_id, repeat_of_access_id, attempt_number,
    issued_by_id, issued_at, activated_at, revoked_at, closed_at, updated_at
FROM external_personal_accesses
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(root_access_id)
  AND root_access_id = id
  AND partner_owner_id = sqlc.arg(partner_owner_id)
FOR UPDATE;

-- name: GetNextPersonalAccessAttemptNumber :one
SELECT COALESCE(max(attempt_number), 0)::integer + 1
FROM external_personal_accesses
WHERE company_id = sqlc.arg(company_id)
  AND root_access_id = sqlc.arg(root_access_id);

-- name: CreateExternalPersonalAccessHistory :one
INSERT INTO external_personal_access_history (
    id, company_id, personal_access_id, external_learner_id,
    enrollment_id, event_type, actor_type, actor_id, idempotency_key,
    previous_token_prefix, current_token_prefix,
    access_until_before, access_until_after, details, occurred_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(personal_access_id),
    sqlc.narg(external_learner_id), sqlc.narg(enrollment_id),
    sqlc.arg(event_type), sqlc.arg(actor_type), sqlc.narg(actor_id),
    sqlc.narg(idempotency_key), sqlc.narg(previous_token_prefix),
    sqlc.narg(current_token_prefix), sqlc.narg(access_until_before),
    sqlc.narg(access_until_after), sqlc.arg(details), sqlc.arg(occurred_at)
)
ON CONFLICT (company_id, personal_access_id, event_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL
DO UPDATE SET id = external_personal_access_history.id
RETURNING id, company_id, personal_access_id, external_learner_id,
    enrollment_id, event_type, actor_type, actor_id, idempotency_key,
    previous_token_prefix, current_token_prefix, access_until_before,
    access_until_after, details, occurred_at;

-- name: CreateCourseEnrollmentAccessHistory :one
INSERT INTO course_enrollment_access_history (
    id, company_id, enrollment_id, from_access_status, to_access_status,
    actor_type, actor_id, reason, access_until_before,
    access_until_after, occurred_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(enrollment_id),
    sqlc.narg(from_access_status), sqlc.arg(to_access_status),
    sqlc.arg(actor_type), sqlc.narg(actor_id), sqlc.narg(reason),
    sqlc.narg(access_until_before), sqlc.narg(access_until_after),
    sqlc.arg(occurred_at)
)
RETURNING id, company_id, enrollment_id, from_access_status,
    to_access_status, actor_type, actor_id, reason,
    access_until_before, access_until_after, occurred_at;

-- name: MaterializeExpiredExternalEnrollments :many
WITH expired AS (
    UPDATE course_enrollments AS enrollment
    SET access_status = 'expired',
        updated_at = sqlc.arg(now)
    WHERE enrollment.id IN (
        SELECT candidate.id
        FROM course_enrollments AS candidate
        WHERE candidate.company_id = sqlc.arg(company_id)
          AND candidate.learner_type = 'external'
          AND candidate.access_status = 'active'
          AND candidate.access_until <= sqlc.arg(now)
        ORDER BY candidate.access_until, candidate.id
        LIMIT sqlc.arg(batch_size)
        FOR UPDATE SKIP LOCKED
    )
    RETURNING enrollment.id, enrollment.company_id, enrollment.course_id,
        enrollment.course_version_id, enrollment.external_learner_id,
        enrollment.source_type, enrollment.source_id,
        enrollment.attempt_number, enrollment.progress_status,
        enrollment.access_status, enrollment.current_lesson_version_id,
        enrollment.activated_at, enrollment.access_until,
        enrollment.started_at, enrollment.completed_at,
        enrollment.last_activity_at, enrollment.frozen_at,
        enrollment.suspended_at, enrollment.created_at,
        enrollment.updated_at
), enrollment_history AS (
    INSERT INTO course_enrollment_access_history (
        company_id, enrollment_id, from_access_status, to_access_status,
        actor_type, reason, access_until_before, access_until_after,
        occurred_at
    )
    SELECT expired.company_id, expired.id, 'active', 'expired',
           'system', 'deadline_expired', expired.access_until,
           expired.access_until, sqlc.arg(now)
    FROM expired
    RETURNING enrollment_id
), access_history AS (
    INSERT INTO external_personal_access_history (
        company_id, personal_access_id, external_learner_id,
        enrollment_id, event_type, actor_type,
        access_until_before, access_until_after, occurred_at
    )
    SELECT expired.company_id, access.id, expired.external_learner_id,
           expired.id, 'deadline_expired', 'system',
           expired.access_until, expired.access_until, sqlc.arg(now)
    FROM expired
    JOIN external_personal_accesses AS access
      ON access.company_id = expired.company_id
     AND access.id = expired.source_id
    WHERE expired.source_type IN ('personal_access', 'repeat_training')
    RETURNING enrollment_id
), campaign_analytics AS (
    INSERT INTO analytics_events (
        company_id, campaign_id, enrollment_id, external_learner_id,
        event_type, event_idempotency_key, request_hash,
        progress_percent, metadata, occurred_at, received_at
    )
    SELECT expired.company_id, expired.source_id, expired.id,
           expired.external_learner_id, 'deadline_expired',
           'deadline-expired:' || expired.id::text,
           encode(digest('deadline-expired:' || expired.id::text, 'sha256'), 'hex'),
           NULL, '{}'::jsonb, expired.access_until, sqlc.arg(now)
    FROM expired
    WHERE expired.source_type IN (
        'partner_promo_campaign', 'company_candidate_campaign'
    )
    ON CONFLICT (company_id, campaign_id, event_idempotency_key) DO NOTHING
    RETURNING enrollment_id
)
SELECT expired.id, expired.company_id, expired.course_id,
    expired.course_version_id, expired.external_learner_id,
    expired.source_type, expired.source_id, expired.attempt_number,
    expired.progress_status, expired.access_status,
    expired.current_lesson_version_id, expired.activated_at,
    expired.access_until, expired.started_at, expired.completed_at,
    expired.last_activity_at, expired.frozen_at, expired.suspended_at,
    expired.created_at, expired.updated_at
FROM expired
JOIN enrollment_history ON enrollment_history.enrollment_id = expired.id
LEFT JOIN access_history ON access_history.enrollment_id = expired.id
LEFT JOIN campaign_analytics ON campaign_analytics.enrollment_id = expired.id;
