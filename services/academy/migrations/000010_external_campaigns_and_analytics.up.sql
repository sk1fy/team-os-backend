-- Campaign links create one version-pinned enrollment per verified external
-- learner. Marketing analytics is retained with the campaign/course tombstone;
-- neither the public token nor a raw IP address is stored.

CREATE TABLE external_campaigns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    course_version_id uuid NOT NULL,
    owner_type text NOT NULL CHECK (owner_type IN ('company', 'partner')),
    owner_user_id uuid,
    purpose text NOT NULL CHECK (
        purpose IN ('company_candidate', 'partner_promo')
    ),
    name text NOT NULL CHECK (
        btrim(name) <> '' AND char_length(name) <= 300
    ),
    deadline_days smallint NOT NULL CHECK (deadline_days BETWEEN 1 AND 7),
    status text NOT NULL DEFAULT 'active' CHECK (
        status IN ('active', 'paused', 'revoked', 'closed')
    ),
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    token_prefix text NOT NULL CHECK (
        token_prefix ~ '^[A-Za-z0-9_-]{6,24}$'
    ),
    created_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    paused_at timestamptz,
    revoked_at timestamptz,
    closed_at timestamptz,
    token_rotated_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_campaigns_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT external_campaigns_token_hash_key UNIQUE (token_hash),
    CONSTRAINT external_campaigns_course_version_fk
        FOREIGN KEY (company_id, course_id, course_version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT external_campaigns_owner_shape_check CHECK (
        (owner_type = 'company' AND owner_user_id IS NULL
            AND purpose = 'company_candidate')
        OR
        (owner_type = 'partner' AND owner_user_id IS NOT NULL
            AND purpose = 'partner_promo'
            AND created_by_id = owner_user_id)
    ),
    CONSTRAINT external_campaigns_status_shape_check CHECK (
        (status = 'active' AND paused_at IS NULL
            AND revoked_at IS NULL AND closed_at IS NULL)
        OR
        (status = 'paused' AND paused_at IS NOT NULL
            AND revoked_at IS NULL AND closed_at IS NULL)
        OR
        (status = 'revoked' AND paused_at IS NULL AND revoked_at IS NOT NULL
            AND closed_at IS NULL)
        OR
        (status = 'closed' AND paused_at IS NULL AND closed_at IS NOT NULL)
    ),
    CONSTRAINT external_campaigns_time_order_check CHECK (
        updated_at >= created_at
        AND (paused_at IS NULL OR paused_at >= created_at)
        AND (revoked_at IS NULL OR revoked_at >= created_at)
        AND (closed_at IS NULL OR closed_at >= created_at)
        AND (token_rotated_at IS NULL OR token_rotated_at >= created_at)
    )
);

CREATE INDEX external_campaigns_company_created_idx
    ON external_campaigns (company_id, created_at DESC, id DESC);

CREATE INDEX external_campaigns_course_status_idx
    ON external_campaigns (
        company_id, course_id, status, created_at DESC, id DESC
    );

CREATE INDEX external_campaigns_partner_status_idx
    ON external_campaigns (
        company_id, owner_user_id, status, created_at DESC, id DESC
    ) WHERE owner_type = 'partner';

CREATE FUNCTION academy_validate_external_campaign()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_owner_type text;
    target_owner_user_id uuid;
    target_lifecycle_status text;
    target_distribution_status text;
    target_version_status text;
BEGIN
    SELECT course.owner_type, course.owner_user_id,
           course.lifecycle_status, course.distribution_status,
           version.status
    INTO target_owner_type, target_owner_user_id,
         target_lifecycle_status, target_distribution_status,
         target_version_status
    FROM courses AS course
    JOIN course_versions AS version
      ON version.company_id = course.company_id
     AND version.course_id = course.id
    WHERE course.company_id = NEW.company_id
      AND course.id = NEW.course_id
      AND version.id = NEW.course_version_id;

    IF NOT FOUND OR target_owner_type <> NEW.owner_type THEN
        RAISE EXCEPTION 'academy campaign: course ownership mismatch';
    END IF;
    IF NEW.owner_type = 'partner'
       AND target_owner_user_id IS DISTINCT FROM NEW.owner_user_id THEN
        RAISE EXCEPTION 'academy campaign: partner course ownership mismatch';
    END IF;
    IF NEW.owner_type = 'company' AND target_owner_user_id IS NOT NULL THEN
        RAISE EXCEPTION 'academy campaign: company course ownership mismatch';
    END IF;
    IF TG_OP = 'INSERT'
       AND (target_version_status <> 'published'
            OR target_lifecycle_status <> 'active'
            OR target_distribution_status <> 'active') THEN
        RAISE EXCEPTION 'academy campaign: course is unavailable for creation';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER external_campaigns_validate_trigger
BEFORE INSERT OR UPDATE OF company_id, course_id, course_version_id,
    owner_type, owner_user_id, purpose, created_by_id
ON external_campaigns
FOR EACH ROW EXECUTE FUNCTION academy_validate_external_campaign();

-- The older index omitted company_id. Campaign UUIDs are global, but the
-- tenant-qualified key makes the isolation invariant explicit and is also the
-- conflict target used by activation queries.
DROP INDEX IF EXISTS course_enrollments_campaign_learner_uidx;

CREATE UNIQUE INDEX course_enrollments_campaign_learner_uidx
    ON course_enrollments (company_id, source_id, external_learner_id)
    WHERE learner_type = 'external'
      AND source_type IN (
          'partner_promo_campaign', 'company_candidate_campaign'
      );

CREATE FUNCTION academy_validate_campaign_enrollment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    campaign_purpose text;
BEGIN
    IF NEW.source_type NOT IN (
        'partner_promo_campaign', 'company_candidate_campaign'
    ) THEN
        RETURN NEW;
    END IF;

    SELECT purpose
    INTO campaign_purpose
    FROM external_campaigns
    WHERE company_id = NEW.company_id
      AND id = NEW.source_id
      AND course_id = NEW.course_id
      AND course_version_id = NEW.course_version_id;

    IF NOT FOUND
       OR (campaign_purpose = 'partner_promo'
           AND NEW.source_type <> 'partner_promo_campaign')
       OR (campaign_purpose = 'company_candidate'
           AND NEW.source_type <> 'company_candidate_campaign') THEN
        RAISE EXCEPTION 'academy campaign: enrollment source mismatch';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_enrollments_campaign_source_trigger
BEFORE INSERT OR UPDATE OF company_id, course_id, course_version_id,
    source_type, source_id
ON course_enrollments
FOR EACH ROW EXECUTE FUNCTION academy_validate_campaign_enrollment();

CREATE TABLE external_campaign_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN (
        'created', 'paused', 'resumed', 'token_rotated',
        'revoked', 'closed'
    )),
    actor_type text NOT NULL CHECK (
        actor_type IN ('internal', 'external', 'system')
    ),
    actor_id uuid,
    idempotency_key text CHECK (
        idempotency_key IS NULL
        OR (btrim(idempotency_key) <> ''
            AND octet_length(idempotency_key) <= 512)
    ),
    previous_status text CHECK (
        previous_status IS NULL
        OR previous_status IN ('active', 'paused', 'revoked', 'closed')
    ),
    current_status text NOT NULL CHECK (
        current_status IN ('active', 'paused', 'revoked', 'closed')
    ),
    previous_token_prefix text,
    current_token_prefix text,
    details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (
        jsonb_typeof(details) = 'object'
    ),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_campaign_history_campaign_fk
        FOREIGN KEY (company_id, campaign_id)
        REFERENCES external_campaigns (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_campaign_history_actor_shape_check CHECK (
        (actor_type = 'system' AND actor_id IS NULL)
        OR (actor_type IN ('internal', 'external') AND actor_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX external_campaign_history_command_uidx
    ON external_campaign_history (
        company_id, campaign_id, event_type, idempotency_key
    ) WHERE idempotency_key IS NOT NULL;

CREATE INDEX external_campaign_history_campaign_time_idx
    ON external_campaign_history (
        company_id, campaign_id, occurred_at, id
    );

CREATE TRIGGER external_campaign_history_immutable_trigger
BEFORE UPDATE OR DELETE ON external_campaign_history
FOR EACH ROW EXECUTE FUNCTION academy_preserve_external_history();

CREATE TABLE analytics_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    enrollment_id uuid,
    external_learner_id uuid,
    event_type text NOT NULL CHECK (event_type IN (
        'landing_viewed', 'form_submitted', 'verification_requested',
        'email_verified', 'course_activated', 'first_lesson_started',
        'lesson_completed', 'quiz_submitted', 'course_completed',
        'deadline_expired', 'return_visit'
    )),
    event_idempotency_key text NOT NULL CHECK (
        btrim(event_idempotency_key) <> ''
        AND octet_length(event_idempotency_key) <= 512
    ),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    -- visitor_hash is an HMAC of the first-party random visitor cookie.
    visitor_hash bytea CHECK (
        visitor_hash IS NULL OR octet_length(visitor_hash) = 32
    ),
    visitor_hash_key_id text CHECK (
        visitor_hash_key_id IS NULL
        OR (btrim(visitor_hash_key_id) <> ''
            AND char_length(visitor_hash_key_id) <= 100)
    ),
    -- request_ip_hash is a short-lived keyed HMAC using a rotating key. There
    -- is deliberately no raw-IP or user-agent address column in this table.
    request_ip_hash bytea CHECK (
        request_ip_hash IS NULL OR octet_length(request_ip_hash) = 32
    ),
    request_ip_hash_key_id text CHECK (
        request_ip_hash_key_id IS NULL
        OR (btrim(request_ip_hash_key_id) <> ''
            AND char_length(request_ip_hash_key_id) <= 100)
    ),
    utm_source text CHECK (utm_source IS NULL OR char_length(utm_source) <= 500),
    utm_medium text CHECK (utm_medium IS NULL OR char_length(utm_medium) <= 500),
    utm_campaign text CHECK (utm_campaign IS NULL OR char_length(utm_campaign) <= 500),
    utm_term text CHECK (utm_term IS NULL OR char_length(utm_term) <= 500),
    utm_content text CHECK (utm_content IS NULL OR char_length(utm_content) <= 500),
    referrer text CHECK (referrer IS NULL OR char_length(referrer) <= 2048),
    lesson_version_id uuid,
    progress_percent smallint CHECK (
        progress_percent IS NULL OR progress_percent BETWEEN 0 AND 100
    ),
    completion_seconds bigint CHECK (
        completion_seconds IS NULL OR completion_seconds >= 0
    ),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (
        jsonb_typeof(metadata) = 'object'
    ),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT analytics_events_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT analytics_events_campaign_fk
        FOREIGN KEY (company_id, campaign_id)
        REFERENCES external_campaigns (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT analytics_events_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT analytics_events_learner_fk
        FOREIGN KEY (company_id, external_learner_id)
        REFERENCES external_learners (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT analytics_events_lesson_fk
        FOREIGN KEY (company_id, lesson_version_id)
        REFERENCES course_version_lessons (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT analytics_events_hash_key_shape_check CHECK (
        ((visitor_hash IS NULL) = (visitor_hash_key_id IS NULL))
        AND ((request_ip_hash IS NULL) = (request_ip_hash_key_id IS NULL))
    ),
    CONSTRAINT analytics_events_event_key UNIQUE (
        company_id, campaign_id, event_idempotency_key
    )
);

CREATE INDEX analytics_events_campaign_occurred_idx
    ON analytics_events (campaign_id, occurred_at, id);

CREATE INDEX analytics_events_company_campaign_type_idx
    ON analytics_events (
        company_id, campaign_id, event_type, occurred_at, id
    );

CREATE INDEX analytics_events_campaign_visitor_idx
    ON analytics_events (campaign_id, visitor_hash, occurred_at, id)
    WHERE visitor_hash IS NOT NULL;

CREATE INDEX analytics_events_campaign_learner_idx
    ON analytics_events (company_id, campaign_id, external_learner_id, occurred_at, id)
    WHERE external_learner_id IS NOT NULL;

CREATE INDEX analytics_events_request_abuse_idx
    ON analytics_events (
        request_ip_hash_key_id, request_ip_hash, occurred_at DESC, id DESC
    ) WHERE request_ip_hash IS NOT NULL;

CREATE FUNCTION academy_validate_campaign_analytics_event()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    linked_learner_id uuid;
    linked_source_id uuid;
    linked_source_type text;
BEGIN
    IF NEW.enrollment_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT external_learner_id, source_id, source_type
    INTO linked_learner_id, linked_source_id, linked_source_type
    FROM course_enrollments
    WHERE company_id = NEW.company_id AND id = NEW.enrollment_id;

    IF NOT FOUND
       OR linked_source_id IS DISTINCT FROM NEW.campaign_id
       OR linked_source_type NOT IN (
           'partner_promo_campaign', 'company_candidate_campaign'
       )
       OR (NEW.external_learner_id IS NOT NULL
           AND linked_learner_id IS DISTINCT FROM NEW.external_learner_id) THEN
        RAISE EXCEPTION 'academy analytics: campaign enrollment mismatch';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER analytics_events_scope_trigger
BEFORE INSERT OR UPDATE OF company_id, campaign_id, enrollment_id,
    external_learner_id
ON analytics_events
FOR EACH ROW EXECUTE FUNCTION academy_validate_campaign_analytics_event();

-- The aggregate is rebuilt/upserted by the analytics worker. Keeping its UTM
-- dimensions nullable preserves the campaign-wide row and optional slices.
CREATE TABLE external_campaign_funnel_daily (
    company_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    bucket_date date NOT NULL,
    utm_source text NOT NULL DEFAULT '',
    utm_medium text NOT NULL DEFAULT '',
    utm_campaign text NOT NULL DEFAULT '',
    landing_views bigint NOT NULL DEFAULT 0 CHECK (landing_views >= 0),
    unique_visitors bigint NOT NULL DEFAULT 0 CHECK (unique_visitors >= 0),
    form_submits bigint NOT NULL DEFAULT 0 CHECK (form_submits >= 0),
    verification_requests bigint NOT NULL DEFAULT 0 CHECK (verification_requests >= 0),
    verified_emails bigint NOT NULL DEFAULT 0 CHECK (verified_emails >= 0),
    activations bigint NOT NULL DEFAULT 0 CHECK (activations >= 0),
    first_lesson_starts bigint NOT NULL DEFAULT 0 CHECK (first_lesson_starts >= 0),
    lesson_completions bigint NOT NULL DEFAULT 0 CHECK (lesson_completions >= 0),
    quiz_submissions bigint NOT NULL DEFAULT 0 CHECK (quiz_submissions >= 0),
    course_completions bigint NOT NULL DEFAULT 0 CHECK (course_completions >= 0),
    deadline_expirations bigint NOT NULL DEFAULT 0 CHECK (deadline_expirations >= 0),
    return_visits bigint NOT NULL DEFAULT 0 CHECK (return_visits >= 0),
    progress_sum bigint NOT NULL DEFAULT 0 CHECK (progress_sum >= 0),
    progress_samples bigint NOT NULL DEFAULT 0 CHECK (progress_samples >= 0),
    completion_seconds_sum numeric(30, 0) NOT NULL DEFAULT 0
        CHECK (completion_seconds_sum >= 0),
    completion_samples bigint NOT NULL DEFAULT 0 CHECK (completion_samples >= 0),
    source_event_count bigint NOT NULL DEFAULT 0 CHECK (source_event_count >= 0),
    last_event_at timestamptz,
    aggregated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (
        company_id, campaign_id, bucket_date,
        utm_source, utm_medium, utm_campaign
    ),
    CONSTRAINT external_campaign_funnel_daily_campaign_fk
        FOREIGN KEY (company_id, campaign_id)
        REFERENCES external_campaigns (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_campaign_funnel_daily_utm_length_check CHECK (
        char_length(utm_source) <= 500
        AND char_length(utm_medium) <= 500
        AND char_length(utm_campaign) <= 500
    )
);

CREATE INDEX external_campaign_funnel_daily_campaign_date_idx
    ON external_campaign_funnel_daily (
        company_id, campaign_id, bucket_date DESC
    );
