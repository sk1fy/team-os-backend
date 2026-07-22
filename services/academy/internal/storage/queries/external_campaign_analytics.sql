-- name: InsertExternalCampaignAnalyticsEvent :one
INSERT INTO analytics_events (
    id, company_id, campaign_id, enrollment_id, external_learner_id,
    event_type, event_idempotency_key, request_hash,
    visitor_hash, visitor_hash_key_id,
    request_ip_hash, request_ip_hash_key_id,
    utm_source, utm_medium, utm_campaign, utm_term, utm_content,
    referrer, lesson_version_id, progress_percent, completion_seconds,
    metadata, occurred_at, received_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(campaign_id),
    sqlc.narg(enrollment_id), sqlc.narg(external_learner_id),
    sqlc.arg(event_type), sqlc.arg(event_idempotency_key),
    sqlc.arg(request_hash), sqlc.narg(visitor_hash),
    sqlc.narg(visitor_hash_key_id), sqlc.narg(request_ip_hash),
    sqlc.narg(request_ip_hash_key_id), sqlc.narg(utm_source),
    sqlc.narg(utm_medium), sqlc.narg(utm_campaign), sqlc.narg(utm_term),
    sqlc.narg(utm_content), sqlc.narg(referrer),
    sqlc.narg(lesson_version_id), sqlc.narg(progress_percent),
    sqlc.narg(completion_seconds), sqlc.arg(metadata),
    sqlc.arg(occurred_at), sqlc.arg(received_at)
)
ON CONFLICT (company_id, campaign_id, event_idempotency_key)
DO UPDATE SET id = analytics_events.id
RETURNING id, company_id, campaign_id, enrollment_id,
    external_learner_id, event_type, event_idempotency_key, request_hash,
    visitor_hash, visitor_hash_key_id, request_ip_hash,
    request_ip_hash_key_id, utm_source, utm_medium, utm_campaign,
    utm_term, utm_content, referrer, lesson_version_id,
    progress_percent, completion_seconds, metadata, occurred_at,
    received_at;

-- name: ListExternalMaintenanceCompanyIDs :many
SELECT company_id
FROM (
    SELECT company_id FROM course_enrollments WHERE learner_type = 'external'
    UNION
    SELECT company_id FROM external_verification_challenges
) AS companies
ORDER BY company_id;

-- name: ListExternalCampaignIDsForAnalyticsMaintenance :many
SELECT company_id, id AS campaign_id
FROM external_campaigns
ORDER BY company_id, id;

-- name: CountRecentCampaignAnalyticsByRequestIPHash :one
SELECT count(*)::integer
FROM analytics_events
WHERE request_ip_hash_key_id = sqlc.arg(request_ip_hash_key_id)
  AND request_ip_hash = sqlc.arg(request_ip_hash)
  AND occurred_at >= sqlc.arg(since);

-- name: GetExternalCampaignAttributionByVisitorHash :one
SELECT utm_source, utm_medium, utm_campaign, utm_term, utm_content,
    referrer
FROM analytics_events
WHERE company_id = sqlc.arg(company_id)
  AND campaign_id = sqlc.arg(campaign_id)
  AND visitor_hash = sqlc.arg(visitor_hash)
  AND event_type IN ('landing_viewed', 'return_visit')
ORDER BY occurred_at DESC, id DESC
LIMIT 1;

-- name: GetExternalCampaignAttributionByEnrollment :one
SELECT visitor_hash, utm_source, utm_medium, utm_campaign, utm_term,
    utm_content, referrer
FROM analytics_events
WHERE company_id = sqlc.arg(company_id)
  AND campaign_id = sqlc.arg(campaign_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
  AND (visitor_hash IS NOT NULL OR utm_source IS NOT NULL
       OR utm_medium IS NOT NULL OR utm_campaign IS NOT NULL
       OR utm_term IS NOT NULL OR utm_content IS NOT NULL
       OR referrer IS NOT NULL)
ORDER BY occurred_at DESC, id DESC
LIMIT 1;

-- name: ListExternalCampaignAnalyticsEvents :many
SELECT id, company_id, campaign_id, enrollment_id,
    external_learner_id, event_type, event_idempotency_key,
    visitor_hash, visitor_hash_key_id, utm_source, utm_medium,
    utm_campaign, utm_term, utm_content, referrer,
    lesson_version_id, progress_percent, completion_seconds,
    metadata, occurred_at, received_at
FROM analytics_events
WHERE company_id = sqlc.arg(company_id)
  AND campaign_id = sqlc.arg(campaign_id)
  AND occurred_at >= sqlc.arg(from_time)
  AND occurred_at < sqlc.arg(to_time)
ORDER BY occurred_at, id;

-- Worker-owned exact replacement/upsert for one day and UTM slice. The
-- source_event_count lets the worker cheaply detect stale aggregates.
-- name: UpsertExternalCampaignFunnelDaily :one
INSERT INTO external_campaign_funnel_daily (
    company_id, campaign_id, bucket_date,
    utm_source, utm_medium, utm_campaign,
    landing_views, unique_visitors, form_submits,
    verification_requests, verified_emails, activations,
    first_lesson_starts, lesson_completions, quiz_submissions,
    course_completions, deadline_expirations, return_visits,
    progress_sum, progress_samples,
    completion_seconds_sum, completion_samples,
    source_event_count, last_event_at, aggregated_at
) VALUES (
    sqlc.arg(company_id), sqlc.arg(campaign_id), sqlc.arg(bucket_date),
    sqlc.arg(utm_source), sqlc.arg(utm_medium), sqlc.arg(utm_campaign),
    sqlc.arg(landing_views), sqlc.arg(unique_visitors),
    sqlc.arg(form_submits), sqlc.arg(verification_requests),
    sqlc.arg(verified_emails), sqlc.arg(activations),
    sqlc.arg(first_lesson_starts), sqlc.arg(lesson_completions),
    sqlc.arg(quiz_submissions), sqlc.arg(course_completions),
    sqlc.arg(deadline_expirations), sqlc.arg(return_visits),
    sqlc.arg(progress_sum), sqlc.arg(progress_samples),
    sqlc.arg(completion_seconds_sum), sqlc.arg(completion_samples),
    sqlc.arg(source_event_count), sqlc.narg(last_event_at),
    sqlc.arg(aggregated_at)
)
ON CONFLICT (
    company_id, campaign_id, bucket_date,
    utm_source, utm_medium, utm_campaign
) DO UPDATE SET
    landing_views = EXCLUDED.landing_views,
    unique_visitors = EXCLUDED.unique_visitors,
    form_submits = EXCLUDED.form_submits,
    verification_requests = EXCLUDED.verification_requests,
    verified_emails = EXCLUDED.verified_emails,
    activations = EXCLUDED.activations,
    first_lesson_starts = EXCLUDED.first_lesson_starts,
    lesson_completions = EXCLUDED.lesson_completions,
    quiz_submissions = EXCLUDED.quiz_submissions,
    course_completions = EXCLUDED.course_completions,
    deadline_expirations = EXCLUDED.deadline_expirations,
    return_visits = EXCLUDED.return_visits,
    progress_sum = EXCLUDED.progress_sum,
    progress_samples = EXCLUDED.progress_samples,
    completion_seconds_sum = EXCLUDED.completion_seconds_sum,
    completion_samples = EXCLUDED.completion_samples,
    source_event_count = EXCLUDED.source_event_count,
    last_event_at = EXCLUDED.last_event_at,
    aggregated_at = EXCLUDED.aggregated_at
RETURNING company_id, campaign_id, bucket_date,
    utm_source, utm_medium, utm_campaign,
    landing_views, unique_visitors, form_submits,
    verification_requests, verified_emails, activations,
    first_lesson_starts, lesson_completions, quiz_submissions,
    course_completions, deadline_expirations, return_visits,
    progress_sum, progress_samples, completion_seconds_sum,
    completion_samples, source_event_count, last_event_at, aggregated_at;

-- name: ListExternalCampaignFunnelDaily :many
SELECT company_id, campaign_id, bucket_date,
    utm_source, utm_medium, utm_campaign,
    landing_views, unique_visitors, form_submits,
    verification_requests, verified_emails, activations,
    first_lesson_starts, lesson_completions, quiz_submissions,
    course_completions, deadline_expirations, return_visits,
    progress_sum, progress_samples, completion_seconds_sum,
    completion_samples, source_event_count, last_event_at, aggregated_at
FROM external_campaign_funnel_daily
WHERE company_id = sqlc.arg(company_id)
  AND campaign_id = sqlc.arg(campaign_id)
  AND bucket_date >= sqlc.arg(from_date)
  AND bucket_date <= sqlc.arg(to_date)
ORDER BY bucket_date, utm_source, utm_medium, utm_campaign;

-- name: RebuildExternalCampaignFunnelDaily :many
WITH dimensions AS (
    SELECT event.company_id, event.campaign_id,
           (event.occurred_at AT TIME ZONE 'UTC')::date AS bucket_date,
           COALESCE(event.utm_source, '') AS utm_source,
           COALESCE(event.utm_medium, '') AS utm_medium,
           COALESCE(event.utm_campaign, '') AS utm_campaign,
           count(*) FILTER (WHERE event.event_type = 'landing_viewed')::bigint
               AS landing_views,
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
               AS course_completions,
           count(*) FILTER (WHERE event.event_type = 'deadline_expired')::bigint
               AS deadline_expirations,
           count(*) FILTER (WHERE event.event_type = 'return_visit')::bigint
               AS return_visits,
           COALESCE(sum(event.progress_percent), 0)::bigint AS progress_sum,
           count(event.progress_percent)::bigint AS progress_samples,
           COALESCE(sum(event.completion_seconds), 0)::numeric(30, 0)
               AS completion_seconds_sum,
           count(event.completion_seconds)::bigint AS completion_samples,
           count(*)::bigint AS source_event_count,
           max(event.occurred_at) AS last_event_at
    FROM analytics_events AS event
    WHERE event.company_id = sqlc.arg(company_id)
      AND event.campaign_id = sqlc.arg(campaign_id)
      AND event.occurred_at >= sqlc.arg(from_time)
      AND event.occurred_at < sqlc.arg(to_time)
    GROUP BY event.company_id, event.campaign_id,
        (event.occurred_at AT TIME ZONE 'UTC')::date,
        COALESCE(event.utm_source, ''), COALESCE(event.utm_medium, ''),
        COALESCE(event.utm_campaign, '')
), upserted AS (
    INSERT INTO external_campaign_funnel_daily (
        company_id, campaign_id, bucket_date,
        utm_source, utm_medium, utm_campaign,
        landing_views, unique_visitors, form_submits,
        verification_requests, verified_emails, activations,
        first_lesson_starts, lesson_completions, quiz_submissions,
        course_completions, deadline_expirations, return_visits,
        progress_sum, progress_samples,
        completion_seconds_sum, completion_samples,
        source_event_count, last_event_at, aggregated_at
    )
    SELECT dimensions.company_id, dimensions.campaign_id,
           dimensions.bucket_date, dimensions.utm_source,
           dimensions.utm_medium, dimensions.utm_campaign,
           dimensions.landing_views, dimensions.unique_visitors,
           dimensions.form_submits, dimensions.verification_requests,
           dimensions.verified_emails, dimensions.activations,
           dimensions.first_lesson_starts, dimensions.lesson_completions,
           dimensions.quiz_submissions, dimensions.course_completions,
           dimensions.deadline_expirations, dimensions.return_visits,
           dimensions.progress_sum, dimensions.progress_samples,
           dimensions.completion_seconds_sum,
           dimensions.completion_samples, dimensions.source_event_count,
           dimensions.last_event_at, sqlc.arg(aggregated_at)
    FROM dimensions
    ON CONFLICT (
        company_id, campaign_id, bucket_date,
        utm_source, utm_medium, utm_campaign
    ) DO UPDATE SET
        landing_views = EXCLUDED.landing_views,
        unique_visitors = EXCLUDED.unique_visitors,
        form_submits = EXCLUDED.form_submits,
        verification_requests = EXCLUDED.verification_requests,
        verified_emails = EXCLUDED.verified_emails,
        activations = EXCLUDED.activations,
        first_lesson_starts = EXCLUDED.first_lesson_starts,
        lesson_completions = EXCLUDED.lesson_completions,
        quiz_submissions = EXCLUDED.quiz_submissions,
        course_completions = EXCLUDED.course_completions,
        deadline_expirations = EXCLUDED.deadline_expirations,
        return_visits = EXCLUDED.return_visits,
        progress_sum = EXCLUDED.progress_sum,
        progress_samples = EXCLUDED.progress_samples,
        completion_seconds_sum = EXCLUDED.completion_seconds_sum,
        completion_samples = EXCLUDED.completion_samples,
        source_event_count = EXCLUDED.source_event_count,
        last_event_at = EXCLUDED.last_event_at,
        aggregated_at = EXCLUDED.aggregated_at
    RETURNING company_id, campaign_id, bucket_date,
        utm_source, utm_medium, utm_campaign,
        landing_views, unique_visitors, form_submits,
        verification_requests, verified_emails, activations,
        first_lesson_starts, lesson_completions, quiz_submissions,
        course_completions, deadline_expirations, return_visits,
        progress_sum, progress_samples, completion_seconds_sum,
        completion_samples, source_event_count, last_event_at, aggregated_at
)
SELECT * FROM upserted
ORDER BY bucket_date, utm_source, utm_medium, utm_campaign;
