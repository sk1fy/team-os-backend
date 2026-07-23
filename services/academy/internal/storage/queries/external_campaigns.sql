-- name: CreateExternalCampaign :one
INSERT INTO external_campaigns (
    id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status,
    token_hash, token_prefix, created_by_id, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(course_id),
    sqlc.arg(course_version_id), sqlc.arg(owner_type),
    sqlc.narg(owner_user_id), sqlc.arg(purpose), sqlc.arg(name),
    sqlc.arg(deadline_days), 'active', sqlc.arg(token_hash),
    sqlc.arg(token_prefix), sqlc.arg(created_by_id),
    sqlc.arg(created_at), sqlc.arg(created_at)
)
RETURNING id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status, token_prefix,
    created_by_id, created_at, paused_at, revoked_at, closed_at,
    token_rotated_at, updated_at;

-- name: GetExternalCampaign :one
SELECT campaign.id, campaign.company_id, campaign.course_id,
    campaign.course_version_id, campaign.owner_type,
    campaign.owner_user_id, campaign.purpose, campaign.name,
    campaign.deadline_days, campaign.status, campaign.token_prefix,
    campaign.created_by_id, campaign.created_at, campaign.paused_at,
    campaign.revoked_at, campaign.closed_at, campaign.token_rotated_at,
    campaign.updated_at,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    course.lifecycle_status AS course_lifecycle_status,
    course.distribution_status AS course_distribution_status,
    course.deleted_at AS course_deleted_at,
    course.archived_at AS course_archived_at,
    version.number AS course_version_number,
    version.status AS course_version_status
FROM external_campaigns AS campaign
JOIN courses AS course
  ON course.company_id = campaign.company_id
 AND course.id = campaign.course_id
JOIN course_versions AS version
  ON version.company_id = campaign.company_id
 AND version.course_id = campaign.course_id
 AND version.id = campaign.course_version_id
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.id = sqlc.arg(id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR campaign.owner_user_id = sqlc.narg(partner_owner_id)::uuid);

-- name: GetExternalCampaignForUpdate :one
SELECT id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status, token_hash,
    token_prefix, created_by_id, created_at, paused_at, revoked_at,
    closed_at, token_rotated_at, updated_at
FROM external_campaigns
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR owner_user_id = sqlc.narg(partner_owner_id)::uuid)
FOR UPDATE;

-- token_hash is globally unique and is the only tenant-bootstrap lookup.
-- Every subsequent query must use the returned company_id.
-- name: ResolveExternalCampaignByTokenHash :one
SELECT campaign.id, campaign.company_id, campaign.course_id,
    campaign.course_version_id, campaign.owner_type,
    campaign.owner_user_id, campaign.purpose, campaign.name,
    campaign.deadline_days, campaign.status, campaign.token_hash,
    campaign.token_prefix, campaign.created_by_id, campaign.created_at,
    campaign.paused_at, campaign.revoked_at, campaign.closed_at,
    campaign.token_rotated_at, campaign.updated_at,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN NULL ELSE version.description END AS course_description,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN NULL ELSE version.cover_file_id END AS cover_file_id,
    version.number AS course_version_number,
    version.sequential, version.status AS course_version_status,
    course.lifecycle_status AS course_lifecycle_status,
    course.distribution_status AS course_distribution_status,
    (campaign.status = 'active'
     AND version.status = 'published'
     AND course.lifecycle_status = 'active'
     AND course.distribution_status = 'active') AS can_activate
FROM external_campaigns AS campaign
JOIN courses AS course
  ON course.company_id = campaign.company_id
 AND course.id = campaign.course_id
JOIN course_versions AS version
  ON version.company_id = campaign.company_id
 AND version.course_id = campaign.course_id
 AND version.id = campaign.course_version_id
WHERE campaign.token_hash = sqlc.arg(token_hash);

-- name: ListExternalCampaigns :many
SELECT campaign.id, campaign.company_id, campaign.course_id,
    campaign.course_version_id, campaign.owner_type,
    campaign.owner_user_id, campaign.purpose, campaign.name,
    campaign.deadline_days, campaign.status, campaign.token_prefix,
    campaign.created_by_id, campaign.created_at, campaign.paused_at,
    campaign.revoked_at, campaign.closed_at, campaign.token_rotated_at,
    campaign.updated_at,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    version.number AS course_version_number,
    course.lifecycle_status AS course_lifecycle_status,
    course.distribution_status AS course_distribution_status,
    count(enrollment.id)::integer AS enrollment_count,
    count(enrollment.id) FILTER (
        WHERE enrollment.progress_status = 'completed'
    )::integer AS completed_enrollment_count
FROM external_campaigns AS campaign
JOIN courses AS course
  ON course.company_id = campaign.company_id
 AND course.id = campaign.course_id
JOIN course_versions AS version
  ON version.company_id = campaign.company_id
 AND version.id = campaign.course_version_id
LEFT JOIN course_enrollments AS enrollment
  ON enrollment.company_id = campaign.company_id
 AND enrollment.source_id = campaign.id
 AND enrollment.source_type IN (
     'partner_promo_campaign', 'company_candidate_campaign'
 )
WHERE campaign.company_id = sqlc.arg(company_id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR campaign.owner_user_id = sqlc.narg(partner_owner_id)::uuid)
  AND (sqlc.narg(course_id)::uuid IS NULL
       OR campaign.course_id = sqlc.narg(course_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL
       OR campaign.status = sqlc.narg(status)::text)
  AND (sqlc.narg(purpose)::text IS NULL
       OR campaign.purpose = sqlc.narg(purpose)::text)
GROUP BY campaign.id, course.id, version.id
ORDER BY campaign.created_at DESC, campaign.id DESC;

-- name: PauseExternalCampaign :one
UPDATE external_campaigns
SET status = 'paused', paused_at = sqlc.arg(changed_at),
    updated_at = sqlc.arg(changed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status = 'active'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR owner_user_id = sqlc.narg(partner_owner_id)::uuid)
RETURNING id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status, token_prefix,
    created_by_id, created_at, paused_at, revoked_at, closed_at,
    token_rotated_at, updated_at;

-- name: ResumeExternalCampaign :one
UPDATE external_campaigns AS campaign
SET status = 'active', paused_at = NULL, updated_at = sqlc.arg(changed_at)
FROM courses AS course, course_versions AS version
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.id = sqlc.arg(id)
  AND campaign.status = 'paused'
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR campaign.owner_user_id = sqlc.narg(partner_owner_id)::uuid)
  AND course.company_id = campaign.company_id
  AND course.id = campaign.course_id
  AND course.lifecycle_status = 'active'
  AND course.distribution_status <> 'blocked'
  AND version.company_id = campaign.company_id
  AND version.id = campaign.course_version_id
  AND version.status = 'published'
RETURNING campaign.id, campaign.company_id, campaign.course_id,
    campaign.course_version_id, campaign.owner_type,
    campaign.owner_user_id, campaign.purpose, campaign.name,
    campaign.deadline_days, campaign.status, campaign.token_prefix,
    campaign.created_by_id, campaign.created_at, campaign.paused_at,
    campaign.revoked_at, campaign.closed_at, campaign.token_rotated_at,
    campaign.updated_at;

-- name: RotateExternalCampaignToken :one
UPDATE external_campaigns
SET token_hash = sqlc.arg(token_hash), token_prefix = sqlc.arg(token_prefix),
    token_rotated_at = sqlc.arg(rotated_at), updated_at = sqlc.arg(rotated_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status IN ('active', 'paused')
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR owner_user_id = sqlc.narg(partner_owner_id)::uuid)
RETURNING id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status, token_prefix,
    created_by_id, created_at, paused_at, revoked_at, closed_at,
    token_rotated_at, updated_at;

-- name: RevokeExternalCampaign :one
UPDATE external_campaigns
SET status = 'revoked', paused_at = NULL,
    revoked_at = sqlc.arg(changed_at),
    updated_at = sqlc.arg(changed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status IN ('active', 'paused')
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR owner_user_id = sqlc.narg(partner_owner_id)::uuid)
RETURNING id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status, token_prefix,
    created_by_id, created_at, paused_at, revoked_at, closed_at,
    token_rotated_at, updated_at;

-- name: CloseExternalCampaign :one
UPDATE external_campaigns
SET status = 'closed', paused_at = NULL,
    closed_at = sqlc.arg(changed_at),
    updated_at = sqlc.arg(changed_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status IN ('active', 'paused', 'revoked')
RETURNING id, company_id, course_id, course_version_id, owner_type,
    owner_user_id, purpose, name, deadline_days, status, token_prefix,
    created_by_id, created_at, paused_at, revoked_at, closed_at,
    token_rotated_at, updated_at;

-- name: CloseExternalCampaignsForCourse :many
UPDATE external_campaigns
SET status = 'closed', paused_at = NULL, closed_at = sqlc.arg(changed_at),
    updated_at = sqlc.arg(changed_at)
WHERE company_id = sqlc.arg(company_id)
  AND course_id = sqlc.arg(course_id)
  AND status <> 'closed'
RETURNING id;

-- name: ActivateExternalCampaign :one
WITH target AS (
    SELECT campaign.id, campaign.company_id, campaign.course_id,
           campaign.course_version_id, campaign.purpose,
           campaign.deadline_days, version.sequential,
           first_lesson.id AS first_lesson_id
    FROM external_campaigns AS campaign
    JOIN courses AS course
      ON course.company_id = campaign.company_id
     AND course.id = campaign.course_id
    JOIN course_versions AS version
      ON version.company_id = campaign.company_id
     AND version.course_id = campaign.course_id
     AND version.id = campaign.course_version_id
    LEFT JOIN LATERAL (
        SELECT lesson.id
        FROM course_version_lessons AS lesson
        JOIN course_version_sections AS section
          ON section.id = lesson.section_version_id
        WHERE lesson.company_id = campaign.company_id
          AND lesson.course_version_id = campaign.course_version_id
        ORDER BY section."order", lesson."order", lesson.id
        LIMIT 1
    ) AS first_lesson ON true
    WHERE campaign.company_id = sqlc.arg(company_id)
      AND campaign.id = sqlc.arg(campaign_id)
      AND campaign.status = 'active'
      AND version.status = 'published'
      AND course.lifecycle_status = 'active'
      AND course.distribution_status = 'active'
    FOR UPDATE OF campaign
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
           CASE WHEN target.purpose = 'partner_promo'
                THEN 'partner_promo_campaign'
                ELSE 'company_candidate_campaign' END,
           target.id, 1, 'in_progress', 'active', target.first_lesson_id,
           sqlc.arg(activated_at),
           sqlc.arg(activated_at)
               + make_interval(days => target.deadline_days::integer),
           sqlc.arg(activated_at), sqlc.arg(activated_at),
           sqlc.arg(activated_at), sqlc.arg(activated_at)
    FROM target
    ON CONFLICT (company_id, source_id, external_learner_id)
        WHERE learner_type = 'external'
          AND source_type IN (
              'partner_promo_campaign', 'company_candidate_campaign'
          )
    DO UPDATE SET updated_at = course_enrollments.updated_at
    RETURNING id, company_id, course_id, course_version_id,
        external_learner_id, source_type, source_id, attempt_number,
        progress_status, access_status, current_lesson_version_id,
        activated_at, access_until, started_at, completed_at,
        last_activity_at, frozen_at, suspended_at, created_at, updated_at
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
    JOIN course_versions AS version
      ON version.id = inserted.course_version_id
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
LEFT JOIN (SELECT count(*) AS seeded_count FROM seeded) AS seed_result ON true;

-- name: InsertExternalCampaignHistory :one
INSERT INTO external_campaign_history (
    id, company_id, campaign_id, event_type, actor_type, actor_id,
    idempotency_key, previous_status, current_status,
    previous_token_prefix, current_token_prefix, details, occurred_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(campaign_id),
    sqlc.arg(event_type), sqlc.arg(actor_type), sqlc.narg(actor_id),
    sqlc.narg(idempotency_key), sqlc.narg(previous_status),
    sqlc.arg(current_status), sqlc.narg(previous_token_prefix),
    sqlc.narg(current_token_prefix), sqlc.arg(details),
    sqlc.arg(occurred_at)
)
ON CONFLICT (company_id, campaign_id, event_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL
DO UPDATE SET id = external_campaign_history.id
RETURNING id, company_id, campaign_id, event_type, actor_type, actor_id,
    idempotency_key, previous_status, current_status,
    previous_token_prefix, current_token_prefix, details, occurred_at;

-- name: ListExternalCampaignHistory :many
SELECT id, company_id, campaign_id, event_type, actor_type, actor_id,
    idempotency_key, previous_status, current_status,
    previous_token_prefix, current_token_prefix, details, occurred_at
FROM external_campaign_history
WHERE company_id = sqlc.arg(company_id)
  AND campaign_id = sqlc.arg(campaign_id)
ORDER BY occurred_at, id;
