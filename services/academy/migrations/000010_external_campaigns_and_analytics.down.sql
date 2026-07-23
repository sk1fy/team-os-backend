DROP TABLE IF EXISTS external_campaign_funnel_daily;

DROP TRIGGER IF EXISTS analytics_events_scope_trigger ON analytics_events;
DROP FUNCTION IF EXISTS academy_validate_campaign_analytics_event();
DROP TABLE IF EXISTS analytics_events;

DROP TRIGGER IF EXISTS external_campaign_history_immutable_trigger
    ON external_campaign_history;
DROP TABLE IF EXISTS external_campaign_history;

DROP TRIGGER IF EXISTS course_enrollments_campaign_source_trigger
    ON course_enrollments;
DROP FUNCTION IF EXISTS academy_validate_campaign_enrollment();

DROP INDEX IF EXISTS course_enrollments_campaign_learner_uidx;
CREATE UNIQUE INDEX course_enrollments_campaign_learner_uidx
    ON course_enrollments (source_id, external_learner_id)
    WHERE source_type IN (
        'partner_promo_campaign', 'company_candidate_campaign'
    );

DROP TRIGGER IF EXISTS external_campaigns_validate_trigger
    ON external_campaigns;
DROP FUNCTION IF EXISTS academy_validate_external_campaign();
DROP TABLE IF EXISTS external_campaigns;
