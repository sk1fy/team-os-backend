-- Reports deliberately do not filter lifecycle/status. Archived courses remain
-- visible and deleted courses are returned as tombstones with their results.

-- name: GetExternalCampaignAnalyticsReport :one
SELECT campaign.id AS campaign_id, campaign.company_id,
    campaign.course_id, campaign.course_version_id,
    campaign.owner_type, campaign.owner_user_id, campaign.purpose,
    campaign.name, campaign.status, campaign.deadline_days,
    campaign.created_at,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    version.number AS course_version_number,
    course.lifecycle_status AS course_lifecycle_status,
    course.distribution_status AS course_distribution_status,
    course.archived_at AS course_archived_at,
    course.deleted_at AS course_deleted_at,
    event_stats.views, event_stats.unique_visitors,
    event_stats.form_submits, event_stats.verification_requests,
    event_stats.verified_emails, event_stats.activations,
    event_stats.first_lesson_starts, event_stats.lesson_completions,
    event_stats.quiz_submissions, event_stats.event_completions,
    event_stats.deadline_expirations, event_stats.return_visits,
    event_stats.average_event_progress,
    event_stats.average_completion_seconds,
    event_stats.median_completion_seconds,
    enrollment_stats.enrollment_count,
    enrollment_stats.completed_enrollment_count,
    enrollment_stats.expired_enrollment_count,
    enrollment_stats.average_enrollment_progress,
    enrollment_stats.median_enrollment_progress,
    CASE WHEN event_stats.views = 0 THEN 0::numeric
         ELSE round(event_stats.form_submits::numeric * 100
                    / event_stats.views, 2)
         END AS view_to_form_percent,
    CASE WHEN event_stats.form_submits = 0 THEN 0::numeric
         ELSE round(event_stats.verified_emails::numeric * 100
                    / event_stats.form_submits, 2)
         END AS form_to_verified_percent,
    CASE WHEN event_stats.verified_emails = 0 THEN 0::numeric
         ELSE round(event_stats.activations::numeric * 100
                    / event_stats.verified_emails, 2)
         END AS verified_to_activation_percent,
    CASE WHEN event_stats.activations = 0 THEN 0::numeric
         ELSE round(enrollment_stats.completed_enrollment_count::numeric * 100
                    / event_stats.activations, 2)
         END AS activation_to_completion_percent
FROM external_campaigns AS campaign
JOIN courses AS course
  ON course.company_id = campaign.company_id
 AND course.id = campaign.course_id
JOIN course_versions AS version
  ON version.company_id = campaign.company_id
 AND version.id = campaign.course_version_id
CROSS JOIN LATERAL (
    SELECT
        count(*) FILTER (WHERE event.event_type = 'landing_viewed')::bigint
            AS views,
        count(DISTINCT event.visitor_hash) FILTER (
            WHERE event.visitor_hash IS NOT NULL
        )::bigint AS unique_visitors,
        count(*) FILTER (WHERE event.event_type = 'form_submitted')::bigint
            AS form_submits,
        count(*) FILTER (
            WHERE event.event_type = 'verification_requested'
        )::bigint AS verification_requests,
        count(*) FILTER (WHERE event.event_type = 'email_verified')::bigint
            AS verified_emails,
        count(*) FILTER (WHERE event.event_type = 'course_activated')::bigint
            AS activations,
        count(*) FILTER (
            WHERE event.event_type = 'first_lesson_started'
        )::bigint AS first_lesson_starts,
        count(*) FILTER (WHERE event.event_type = 'lesson_completed')::bigint
            AS lesson_completions,
        count(*) FILTER (WHERE event.event_type = 'quiz_submitted')::bigint
            AS quiz_submissions,
        count(*) FILTER (WHERE event.event_type = 'course_completed')::bigint
            AS event_completions,
        count(*) FILTER (WHERE event.event_type = 'deadline_expired')::bigint
            AS deadline_expirations,
        count(*) FILTER (WHERE event.event_type = 'return_visit')::bigint
            AS return_visits,
        COALESCE(avg(event.progress_percent), 0)::numeric(8, 2)
            AS average_event_progress,
        COALESCE(avg(event.completion_seconds), 0)::numeric(20, 2)
            AS average_completion_seconds,
        COALESCE(percentile_cont(0.5) WITHIN GROUP (
            ORDER BY event.completion_seconds
        ) FILTER (WHERE event.completion_seconds IS NOT NULL), 0)::numeric(20, 2)
            AS median_completion_seconds
    FROM analytics_events AS event
    WHERE event.company_id = campaign.company_id
      AND event.campaign_id = campaign.id
      AND event.occurred_at >= sqlc.arg(from_time)
      AND event.occurred_at < sqlc.arg(to_time)
) AS event_stats
CROSS JOIN LATERAL (
    SELECT count(*)::bigint AS enrollment_count,
        count(*) FILTER (
            WHERE enrollment.progress_status = 'completed'
        )::bigint AS completed_enrollment_count,
        count(*) FILTER (
            WHERE enrollment.access_status = 'expired'
        )::bigint AS expired_enrollment_count,
        COALESCE(avg(progress.progress_percent), 0)::numeric(8, 2)
            AS average_enrollment_progress,
        COALESCE(percentile_cont(0.5) WITHIN GROUP (
            ORDER BY progress.progress_percent
        ), 0)::numeric(8, 2) AS median_enrollment_progress
    FROM course_enrollments AS enrollment
    CROSS JOIN LATERAL (
        SELECT CASE WHEN count(lesson.id) = 0 THEN 0
                    ELSE count(lesson.id) FILTER (
                             WHERE lesson_progress.status = 'completed'
                         ) * 100 / count(lesson.id)
               END::integer AS progress_percent
        FROM course_version_lessons AS lesson
        LEFT JOIN enrollment_lesson_progress AS lesson_progress
          ON lesson_progress.company_id = enrollment.company_id
         AND lesson_progress.enrollment_id = enrollment.id
         AND lesson_progress.lesson_version_id = lesson.id
        WHERE lesson.company_id = enrollment.company_id
          AND lesson.course_version_id = enrollment.course_version_id
    ) AS progress
    WHERE enrollment.company_id = campaign.company_id
      AND enrollment.source_id = campaign.id
      AND enrollment.source_type IN (
          'partner_promo_campaign', 'company_candidate_campaign'
      )
) AS enrollment_stats
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.id = sqlc.arg(campaign_id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR campaign.owner_user_id = sqlc.narg(partner_owner_id)::uuid);

-- name: ListCourseExternalCampaignAnalyticsReports :many
SELECT campaign.id AS campaign_id, campaign.company_id,
    campaign.course_id, campaign.course_version_id, campaign.name,
    campaign.purpose, campaign.status, campaign.owner_type,
    campaign.owner_user_id, campaign.created_at,
    version.number AS course_version_number,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    course.lifecycle_status AS course_lifecycle_status,
    course.distribution_status AS course_distribution_status,
    stats.views, stats.unique_visitors, stats.form_submits,
    stats.verified_emails, stats.activations, stats.enrollment_count,
    stats.completed_enrollment_count, stats.expired_enrollment_count
FROM external_campaigns AS campaign
JOIN courses AS course
  ON course.company_id = campaign.company_id
 AND course.id = campaign.course_id
JOIN course_versions AS version
  ON version.company_id = campaign.company_id
 AND version.id = campaign.course_version_id
CROSS JOIN LATERAL (
    SELECT
        (SELECT count(*) FILTER (
             WHERE event.event_type = 'landing_viewed'
         )::bigint
         FROM analytics_events AS event
         WHERE event.company_id = campaign.company_id
           AND event.campaign_id = campaign.id
           AND event.occurred_at >= sqlc.arg(from_time)
           AND event.occurred_at < sqlc.arg(to_time)) AS views,
        (SELECT count(DISTINCT event.visitor_hash)::bigint
         FROM analytics_events AS event
         WHERE event.company_id = campaign.company_id
           AND event.campaign_id = campaign.id
           AND event.visitor_hash IS NOT NULL
           AND event.occurred_at >= sqlc.arg(from_time)
           AND event.occurred_at < sqlc.arg(to_time)) AS unique_visitors,
        (SELECT count(*) FILTER (
             WHERE event.event_type = 'form_submitted'
         )::bigint
         FROM analytics_events AS event
         WHERE event.company_id = campaign.company_id
           AND event.campaign_id = campaign.id
           AND event.occurred_at >= sqlc.arg(from_time)
           AND event.occurred_at < sqlc.arg(to_time)) AS form_submits,
        (SELECT count(*) FILTER (
             WHERE event.event_type = 'email_verified'
         )::bigint
         FROM analytics_events AS event
         WHERE event.company_id = campaign.company_id
           AND event.campaign_id = campaign.id
           AND event.occurred_at >= sqlc.arg(from_time)
           AND event.occurred_at < sqlc.arg(to_time)) AS verified_emails,
        (SELECT count(*) FILTER (
             WHERE event.event_type = 'course_activated'
         )::bigint
         FROM analytics_events AS event
         WHERE event.company_id = campaign.company_id
           AND event.campaign_id = campaign.id
           AND event.occurred_at >= sqlc.arg(from_time)
           AND event.occurred_at < sqlc.arg(to_time)) AS activations,
        (SELECT count(*)::bigint
         FROM course_enrollments AS enrollment
         WHERE enrollment.company_id = campaign.company_id
           AND enrollment.source_id = campaign.id
           AND enrollment.source_type IN (
               'partner_promo_campaign', 'company_candidate_campaign'
           )) AS enrollment_count,
        (SELECT count(*)::bigint
         FROM course_enrollments AS enrollment
         WHERE enrollment.company_id = campaign.company_id
           AND enrollment.source_id = campaign.id
           AND enrollment.progress_status = 'completed'
           AND enrollment.source_type IN (
               'partner_promo_campaign', 'company_candidate_campaign'
           )) AS completed_enrollment_count,
        (SELECT count(*)::bigint
         FROM course_enrollments AS enrollment
         WHERE enrollment.company_id = campaign.company_id
           AND enrollment.source_id = campaign.id
           AND enrollment.access_status = 'expired'
           AND enrollment.source_type IN (
               'partner_promo_campaign', 'company_candidate_campaign'
           )) AS expired_enrollment_count
) AS stats
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.course_id = sqlc.arg(course_id)
ORDER BY campaign.created_at DESC, campaign.id DESC;

-- name: ListPartnerExternalCampaignAnalyticsReports :many
SELECT campaign.id AS campaign_id, campaign.company_id,
    campaign.course_id, campaign.course_version_id, campaign.name,
    campaign.purpose, campaign.status, campaign.created_at,
    version.number AS course_version_number,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    course.lifecycle_status AS course_lifecycle_status,
    stats_events.views, stats_events.unique_visitors,
    stats_events.activations, stats_enrollments.enrollment_count,
    stats_enrollments.completed_enrollment_count
FROM external_campaigns AS campaign
JOIN courses AS course
  ON course.company_id = campaign.company_id
 AND course.id = campaign.course_id
JOIN course_versions AS version
  ON version.company_id = campaign.company_id
 AND version.id = campaign.course_version_id
CROSS JOIN LATERAL (
    SELECT
        count(*) FILTER (
            WHERE event.event_type = 'landing_viewed'
        )::bigint AS views,
        count(DISTINCT event.visitor_hash) FILTER (
            WHERE event.visitor_hash IS NOT NULL
        )::bigint AS unique_visitors,
        count(*) FILTER (
            WHERE event.event_type = 'course_activated'
        )::bigint AS activations
    FROM analytics_events AS event
    WHERE event.company_id = campaign.company_id
      AND event.campaign_id = campaign.id
      AND event.occurred_at >= sqlc.arg(from_time)
      AND event.occurred_at < sqlc.arg(to_time)
) AS stats_events
CROSS JOIN LATERAL (
    SELECT count(*)::bigint AS enrollment_count,
        count(*) FILTER (
            WHERE enrollment.progress_status = 'completed'
        )::bigint AS completed_enrollment_count
    FROM course_enrollments AS enrollment
    WHERE enrollment.company_id = campaign.company_id
      AND enrollment.source_id = campaign.id
      AND enrollment.source_type = 'partner_promo_campaign'
) AS stats_enrollments
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.owner_type = 'partner'
  AND campaign.owner_user_id = sqlc.arg(partner_owner_id)
ORDER BY campaign.created_at DESC, campaign.id DESC;

-- name: ListExternalCampaignUTMReport :many
SELECT COALESCE(event.utm_source, '') AS utm_source,
    COALESCE(event.utm_medium, '') AS utm_medium,
    COALESCE(event.utm_campaign, '') AS utm_campaign,
    COALESCE(event.utm_term, '') AS utm_term,
    COALESCE(event.utm_content, '') AS utm_content,
    COALESCE(event.referrer, '') AS referrer,
    count(*) FILTER (WHERE event.event_type = 'landing_viewed')::bigint
        AS views,
    count(DISTINCT event.visitor_hash) FILTER (
        WHERE event.visitor_hash IS NOT NULL
    )::bigint AS unique_visitors,
    count(*) FILTER (WHERE event.event_type = 'form_submitted')::bigint
        AS form_submits,
    count(*) FILTER (WHERE event.event_type = 'course_activated')::bigint
        AS activations,
    count(*) FILTER (WHERE event.event_type = 'course_completed')::bigint
        AS completions,
    min(event.occurred_at) AS first_event_at,
    max(event.occurred_at) AS last_event_at
FROM analytics_events AS event
JOIN external_campaigns AS campaign
  ON campaign.company_id = event.company_id
 AND campaign.id = event.campaign_id
WHERE event.company_id = sqlc.arg(company_id)
  AND event.campaign_id = sqlc.arg(campaign_id)
  AND event.occurred_at >= sqlc.arg(from_time)
  AND event.occurred_at < sqlc.arg(to_time)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR campaign.owner_user_id = sqlc.narg(partner_owner_id)::uuid)
GROUP BY COALESCE(event.utm_source, ''), COALESCE(event.utm_medium, ''),
    COALESCE(event.utm_campaign, ''), COALESCE(event.utm_term, ''),
    COALESCE(event.utm_content, ''), COALESCE(event.referrer, '')
ORDER BY views DESC, unique_visitors DESC,
    utm_source, utm_medium, utm_campaign, utm_term, utm_content, referrer;

-- name: ListExternalCampaignLessonDropOffReport :many
SELECT lesson.id AS lesson_version_id,
    count(progress.enrollment_id) FILTER (
        WHERE progress.first_opened_at IS NOT NULL
           OR progress.status IN ('current', 'completed')
    )::bigint AS reached,
    count(progress.enrollment_id) FILTER (
        WHERE progress.status = 'completed'
    )::bigint AS completed
FROM external_campaigns AS campaign
JOIN course_version_lessons AS lesson
  ON lesson.company_id = campaign.company_id
 AND lesson.course_version_id = campaign.course_version_id
LEFT JOIN course_enrollments AS enrollment
  ON enrollment.company_id = campaign.company_id
 AND enrollment.source_id = campaign.id
 AND enrollment.source_type IN (
     'partner_promo_campaign', 'company_candidate_campaign'
 )
LEFT JOIN enrollment_lesson_progress AS progress
  ON progress.company_id = enrollment.company_id
 AND progress.enrollment_id = enrollment.id
 AND progress.lesson_version_id = lesson.id
WHERE campaign.company_id = sqlc.arg(company_id)
  AND campaign.id = sqlc.arg(campaign_id)
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR campaign.owner_user_id = sqlc.narg(partner_owner_id)::uuid)
GROUP BY lesson.id, lesson."order"
ORDER BY lesson."order", lesson.id;

-- name: ListExternalLearnerTimeline :many
SELECT enrollment.id AS enrollment_id, enrollment.company_id,
    enrollment.external_learner_id, enrollment.course_id,
    enrollment.course_version_id, version.number AS course_version_number,
    CASE WHEN course.lifecycle_status = 'deleted'
         THEN 'Удалённый курс'
         ELSE version.title END AS course_title,
    course.owner_type AS course_owner_type,
    course.owner_user_id AS course_owner_user_id,
    course.lifecycle_status AS course_lifecycle_status,
    course.distribution_status AS course_distribution_status,
    course.archived_at AS course_archived_at,
    course.deleted_at AS course_deleted_at,
    enrollment.source_type, enrollment.source_id,
    CASE
        WHEN campaign.id IS NOT NULL THEN campaign.name
        WHEN personal.id IS NOT NULL THEN 'Персональный доступ'
        ELSE enrollment.source_type
    END AS source_name,
    campaign.purpose AS campaign_purpose,
    campaign.status AS campaign_status,
    enrollment.attempt_number, enrollment.progress_status,
    enrollment.access_status, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at,
    enrollment.completed_at, enrollment.last_activity_at,
    enrollment.created_at,
    progress.lesson_count, progress.completed_lesson_count,
    progress.progress_percent, progress.active_seconds
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
LEFT JOIN external_campaigns AS campaign
  ON campaign.company_id = enrollment.company_id
 AND campaign.id = enrollment.source_id
 AND enrollment.source_type IN (
     'partner_promo_campaign', 'company_candidate_campaign'
 )
LEFT JOIN external_personal_accesses AS personal
  ON personal.company_id = enrollment.company_id
 AND personal.id = enrollment.source_id
 AND enrollment.source_type IN ('personal_access', 'repeat_training')
CROSS JOIN LATERAL (
    SELECT count(lesson.id)::integer AS lesson_count,
        count(lesson.id) FILTER (
            WHERE lesson_progress.status = 'completed'
        )::integer AS completed_lesson_count,
        CASE WHEN count(lesson.id) = 0 THEN 0
             ELSE (count(lesson.id) FILTER (
                       WHERE lesson_progress.status = 'completed'
                   ) * 100 / count(lesson.id))::integer
             END AS progress_percent,
        COALESCE(sum(lesson_progress.active_seconds), 0)::bigint
            AS active_seconds
    FROM course_version_lessons AS lesson
    LEFT JOIN enrollment_lesson_progress AS lesson_progress
      ON lesson_progress.company_id = enrollment.company_id
     AND lesson_progress.enrollment_id = enrollment.id
     AND lesson_progress.lesson_version_id = lesson.id
    WHERE lesson.company_id = enrollment.company_id
      AND lesson.course_version_id = enrollment.course_version_id
) AS progress
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.external_learner_id = sqlc.arg(external_learner_id)
  AND enrollment.learner_type = 'external'
  AND learner.deleted_at IS NULL
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
ORDER BY COALESCE(enrollment.last_activity_at, enrollment.created_at) DESC,
    enrollment.id DESC;

-- name: ListScopedExternalEnrollmentsForReport :many
SELECT enrollment.id, enrollment.company_id, enrollment.course_id,
    enrollment.course_version_id, version.number AS course_version_number,
    enrollment.external_learner_id, enrollment.source_type,
    enrollment.source_id, enrollment.attempt_number,
    enrollment.progress_status, enrollment.access_status,
    enrollment.current_lesson_version_id, enrollment.activated_at,
    enrollment.access_until, enrollment.started_at, enrollment.completed_at,
    enrollment.last_activity_at, enrollment.frozen_at,
    enrollment.suspended_at, enrollment.created_at, enrollment.updated_at,
    CASE WHEN count(lesson.id) = 0 THEN 0
         ELSE (count(lesson.id) FILTER (WHERE progress.status = 'completed')
               * 100 / count(lesson.id))::integer END AS progress_percent
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
  AND enrollment.learner_type = 'external'
  AND (sqlc.narg(course_id)::uuid IS NULL
       OR enrollment.course_id = sqlc.narg(course_id)::uuid)
  AND (sqlc.narg(campaign_id)::uuid IS NULL
       OR (enrollment.source_id = sqlc.narg(campaign_id)::uuid
           AND enrollment.source_type IN (
               'partner_promo_campaign', 'company_candidate_campaign'
           )))
  AND (sqlc.narg(partner_owner_id)::uuid IS NULL
       OR (course.owner_type = 'partner'
           AND course.owner_user_id = sqlc.narg(partner_owner_id)::uuid))
GROUP BY enrollment.id, version.number
ORDER BY enrollment.created_at DESC, enrollment.id DESC;
