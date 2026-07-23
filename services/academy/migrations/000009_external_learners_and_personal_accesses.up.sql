-- External learners are company-local identities and never become TeamOS
-- users implicitly. Personal access is only a source for a version-pinned
-- course enrollment; progress remains owned by course_enrollments.

CREATE TABLE external_learners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    email text NOT NULL CHECK (
        btrim(email) <> '' AND char_length(email) <= 320
    ),
    normalized_email text NOT NULL CHECK (
        normalized_email = lower(btrim(email))
        AND char_length(normalized_email) <= 320
    ),
    first_name text CHECK (
        first_name IS NULL
        OR (btrim(first_name) <> '' AND char_length(first_name) <= 200)
    ),
    last_name text CHECK (
        last_name IS NULL
        OR (btrim(last_name) <> '' AND char_length(last_name) <= 200)
    ),
    phone text CHECK (
        phone IS NULL OR (btrim(phone) <> '' AND char_length(phone) <= 64)
    ),
    email_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT external_learners_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT external_learners_company_email_key
        UNIQUE (company_id, normalized_email),
    CONSTRAINT external_learners_time_order_check CHECK (
        updated_at >= created_at
        AND (email_verified_at IS NULL OR email_verified_at >= created_at)
        AND (deleted_at IS NULL OR deleted_at >= created_at)
    )
);

CREATE INDEX external_learners_company_created_idx
    ON external_learners (company_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE external_verification_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    normalized_email text NOT NULL CHECK (
        normalized_email = lower(btrim(normalized_email))
        AND btrim(normalized_email) <> ''
        AND char_length(normalized_email) <= 320
    ),
    purpose text NOT NULL CHECK (
        purpose IN ('personal_access', 'campaign_access', 'session_bootstrap')
    ),
    source_id uuid,
    claimed_first_name text CHECK (
        claimed_first_name IS NULL
        OR (btrim(claimed_first_name) <> ''
            AND char_length(claimed_first_name) <= 200)
    ),
    claimed_last_name text CHECK (
        claimed_last_name IS NULL
        OR (btrim(claimed_last_name) <> ''
            AND char_length(claimed_last_name) <= 200)
    ),
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    request_ip_hash bytea CHECK (
        request_ip_hash IS NULL OR octet_length(request_ip_hash) = 32
    ),
    expires_at timestamptz NOT NULL,
    attempts smallint NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts smallint NOT NULL DEFAULT 5 CHECK (
        max_attempts BETWEEN 1 AND 5 AND attempts <= max_attempts
    ),
    consumed_at timestamptz,
    invalidated_at timestamptz,
    invalidation_reason text CHECK (
        invalidation_reason IS NULL
        OR invalidation_reason IN (
            'expired', 'replaced', 'rate_limited', 'attempts_exhausted'
        )
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_verification_challenges_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT external_verification_challenges_source_shape_check CHECK (
        (purpose IN ('personal_access', 'campaign_access') AND source_id IS NOT NULL)
        OR (purpose = 'session_bootstrap' AND source_id IS NULL)
    ),
    CONSTRAINT external_verification_challenges_lifecycle_check CHECK (
        expires_at > created_at
        AND (consumed_at IS NULL OR consumed_at >= created_at)
        AND (invalidated_at IS NULL OR invalidated_at >= created_at)
        AND NOT (consumed_at IS NOT NULL AND invalidated_at IS NOT NULL)
        AND ((invalidated_at IS NULL AND invalidation_reason IS NULL)
             OR (invalidated_at IS NOT NULL AND invalidation_reason IS NOT NULL))
    )
);

CREATE INDEX external_verification_challenges_email_rate_idx
    ON external_verification_challenges (
        company_id, normalized_email, created_at DESC, id DESC
    );

CREATE INDEX external_verification_challenges_source_rate_idx
    ON external_verification_challenges (
        company_id, purpose, source_id, created_at DESC, id DESC
    ) WHERE source_id IS NOT NULL;

CREATE INDEX external_verification_challenges_ip_rate_idx
    ON external_verification_challenges (
        company_id, request_ip_hash, created_at DESC, id DESC
    ) WHERE request_ip_hash IS NOT NULL;

CREATE INDEX external_verification_challenges_expiry_idx
    ON external_verification_challenges (expires_at, id)
    WHERE consumed_at IS NULL AND invalidated_at IS NULL;

CREATE TABLE external_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    external_learner_id uuid NOT NULL,
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    revocation_reason text CHECK (
        revocation_reason IS NULL
        OR revocation_reason IN ('expired', 'manual', 'rotated')
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_sessions_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT external_sessions_token_hash_key UNIQUE (token_hash),
    CONSTRAINT external_sessions_learner_fk
        FOREIGN KEY (company_id, external_learner_id)
        REFERENCES external_learners (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_sessions_lifecycle_check CHECK (
        expires_at > created_at
        AND (last_used_at IS NULL OR last_used_at >= created_at)
        AND (revoked_at IS NULL OR revoked_at >= created_at)
        AND ((revoked_at IS NULL AND revocation_reason IS NULL)
             OR (revoked_at IS NOT NULL AND revocation_reason IS NOT NULL))
    )
);

CREATE INDEX external_sessions_company_learner_active_idx
    ON external_sessions (
        company_id, external_learner_id, expires_at DESC, id DESC
    ) WHERE revoked_at IS NULL;

CREATE INDEX external_sessions_expiry_idx
    ON external_sessions (expires_at, id) WHERE revoked_at IS NULL;

CREATE TABLE external_personal_accesses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    course_version_id uuid NOT NULL,
    partner_owner_id uuid NOT NULL,
    external_learner_id uuid,
    expected_email text NOT NULL CHECK (
        btrim(expected_email) <> '' AND char_length(expected_email) <= 320
    ),
    normalized_expected_email text NOT NULL CHECK (
        normalized_expected_email = lower(btrim(expected_email))
        AND char_length(normalized_expected_email) <= 320
    ),
    recipient_first_name text CHECK (
        recipient_first_name IS NULL
        OR (btrim(recipient_first_name) <> ''
            AND char_length(recipient_first_name) <= 200)
    ),
    recipient_last_name text CHECK (
        recipient_last_name IS NULL
        OR (btrim(recipient_last_name) <> ''
            AND char_length(recipient_last_name) <= 200)
    ),
    deadline_days smallint NOT NULL CHECK (deadline_days BETWEEN 1 AND 7),
    status text NOT NULL DEFAULT 'issued'
        CHECK (status IN ('issued', 'activated', 'revoked', 'closed')),
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    token_prefix text NOT NULL CHECK (
        token_prefix ~ '^[A-Za-z0-9_-]{6,24}$'
    ),
    enrollment_id uuid,
    root_access_id uuid NOT NULL,
    repeat_of_access_id uuid,
    attempt_number integer NOT NULL DEFAULT 1 CHECK (attempt_number >= 1),
    issuance_idempotency_key text NOT NULL CHECK (
        btrim(issuance_idempotency_key) <> ''
        AND octet_length(issuance_idempotency_key) <= 512
    ),
    issued_by_id uuid NOT NULL,
    issued_at timestamptz NOT NULL DEFAULT now(),
    activated_at timestamptz,
    token_rotated_at timestamptz,
    revoked_at timestamptz,
    closed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_personal_accesses_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT external_personal_accesses_token_hash_key UNIQUE (token_hash),
    CONSTRAINT external_personal_accesses_issue_key UNIQUE (
        company_id, partner_owner_id, issuance_idempotency_key
    ),
    CONSTRAINT external_personal_accesses_attempt_key UNIQUE (
        company_id, root_access_id, attempt_number
    ),
    CONSTRAINT external_personal_accesses_course_version_fk
        FOREIGN KEY (company_id, course_id, course_version_id)
        REFERENCES course_versions (company_id, course_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_personal_accesses_learner_fk
        FOREIGN KEY (company_id, external_learner_id)
        REFERENCES external_learners (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_personal_accesses_root_fk
        FOREIGN KEY (company_id, root_access_id)
        REFERENCES external_personal_accesses (company_id, id)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT external_personal_accesses_repeat_fk
        FOREIGN KEY (company_id, repeat_of_access_id)
        REFERENCES external_personal_accesses (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_personal_accesses_repeat_shape_check CHECK (
        (attempt_number = 1 AND root_access_id = id
            AND repeat_of_access_id IS NULL)
        OR
        (attempt_number > 1 AND root_access_id <> id
            AND repeat_of_access_id IS NOT NULL)
    ),
    CONSTRAINT external_personal_accesses_status_shape_check CHECK (
        (status = 'issued'
            AND activated_at IS NULL AND revoked_at IS NULL AND closed_at IS NULL)
        OR
        (status = 'activated'
            AND external_learner_id IS NOT NULL
            AND enrollment_id IS NOT NULL
            AND activated_at IS NOT NULL
            AND revoked_at IS NULL AND closed_at IS NULL)
        OR
        (status = 'revoked' AND revoked_at IS NOT NULL AND closed_at IS NULL)
        OR
        (status = 'closed' AND closed_at IS NOT NULL)
    ),
    CONSTRAINT external_personal_accesses_time_order_check CHECK (
        updated_at >= issued_at
        AND (activated_at IS NULL OR activated_at >= issued_at)
        AND (token_rotated_at IS NULL OR token_rotated_at >= issued_at)
        AND (revoked_at IS NULL OR revoked_at >= issued_at)
        AND (closed_at IS NULL OR closed_at >= issued_at)
    )
);

CREATE INDEX external_personal_accesses_partner_created_idx
    ON external_personal_accesses (
        company_id, partner_owner_id, issued_at DESC, id DESC
    );

CREATE INDEX external_personal_accesses_course_status_idx
    ON external_personal_accesses (
        company_id, course_id, status, issued_at DESC, id DESC
    );

CREATE INDEX external_personal_accesses_learner_created_idx
    ON external_personal_accesses (
        company_id, external_learner_id, issued_at DESC, id DESC
    ) WHERE external_learner_id IS NOT NULL;

ALTER TABLE external_personal_accesses
    ADD CONSTRAINT external_personal_accesses_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX course_enrollments_external_personal_source_uidx
    ON course_enrollments (company_id, source_id)
    WHERE learner_type = 'external'
      AND source_type IN ('personal_access', 'repeat_training');

CREATE FUNCTION academy_validate_external_personal_access()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_owner_type text;
    target_owner_user_id uuid;
    target_lifecycle_status text;
    target_distribution_status text;
    target_version_status text;
    linked_source_type text;
    linked_source_id uuid;
    linked_learner_id uuid;
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

    IF NOT FOUND
       OR target_owner_type <> 'partner'
       OR target_owner_user_id IS DISTINCT FROM NEW.partner_owner_id THEN
        RAISE EXCEPTION 'academy personal access: partner course ownership mismatch';
    END IF;
    IF TG_OP = 'INSERT'
       AND (target_version_status <> 'published'
            OR target_lifecycle_status <> 'active'
            OR target_distribution_status <> 'active') THEN
        RAISE EXCEPTION 'academy personal access: course is unavailable for issuance';
    END IF;

    IF NEW.enrollment_id IS NOT NULL THEN
        SELECT source_type, source_id, external_learner_id
        INTO linked_source_type, linked_source_id, linked_learner_id
        FROM course_enrollments
        WHERE company_id = NEW.company_id
          AND id = NEW.enrollment_id
          AND course_id = NEW.course_id
          AND course_version_id = NEW.course_version_id;

        IF NOT FOUND
           OR linked_source_type NOT IN ('personal_access', 'repeat_training')
           OR linked_source_id IS DISTINCT FROM NEW.id
           OR linked_learner_id IS DISTINCT FROM NEW.external_learner_id THEN
            RAISE EXCEPTION 'academy personal access: enrollment linkage mismatch';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER external_personal_accesses_validate_trigger
BEFORE INSERT OR UPDATE ON external_personal_accesses
FOR EACH ROW EXECUTE FUNCTION academy_validate_external_personal_access();

CREATE TABLE external_mutation_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    external_learner_id uuid NOT NULL,
    operation text NOT NULL CHECK (operation IN (
        'activate', 'complete_lesson', 'submit_quiz',
        'extend_access', 'rotate_access', 'revoke_access', 'repeat_access'
    )),
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> ''
        AND octet_length(idempotency_key) <= 512
    ),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    aggregate_id uuid NOT NULL,
    result_id uuid,
    enrollment_id uuid,
    result_payload jsonb,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_mutation_idempotency_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT external_mutation_idempotency_key UNIQUE (
        company_id, external_learner_id, operation, idempotency_key
    ),
    CONSTRAINT external_mutation_idempotency_learner_fk
        FOREIGN KEY (company_id, external_learner_id)
        REFERENCES external_learners (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_mutation_idempotency_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_mutation_idempotency_completion_check CHECK (
        (completed_at IS NULL AND result_payload IS NULL AND result_id IS NULL)
        OR completed_at IS NOT NULL
    )
);

CREATE INDEX external_mutation_idempotency_aggregate_idx
    ON external_mutation_idempotency (
        company_id, aggregate_id, operation, created_at DESC, id DESC
    );

CREATE TABLE external_personal_access_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    personal_access_id uuid NOT NULL,
    external_learner_id uuid,
    enrollment_id uuid,
    event_type text NOT NULL CHECK (event_type IN (
        'issued', 'verification_requested', 'email_verified', 'activated',
        'extended', 'token_rotated', 'revoked', 'closed',
        'repeat_created', 'deadline_expired'
    )),
    actor_type text NOT NULL CHECK (actor_type IN ('internal', 'external', 'system')),
    actor_id uuid,
    idempotency_key text CHECK (
        idempotency_key IS NULL
        OR (btrim(idempotency_key) <> ''
            AND octet_length(idempotency_key) <= 512)
    ),
    previous_token_prefix text,
    current_token_prefix text,
    access_until_before timestamptz,
    access_until_after timestamptz,
    details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (
        jsonb_typeof(details) = 'object'
    ),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_personal_access_history_access_fk
        FOREIGN KEY (company_id, personal_access_id)
        REFERENCES external_personal_accesses (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_personal_access_history_learner_fk
        FOREIGN KEY (company_id, external_learner_id)
        REFERENCES external_learners (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_personal_access_history_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT external_personal_access_history_actor_shape_check CHECK (
        (actor_type = 'system' AND actor_id IS NULL)
        OR (actor_type IN ('internal', 'external') AND actor_id IS NOT NULL)
    ),
    CONSTRAINT external_personal_access_history_extension_check CHECK (
        event_type <> 'extended'
        OR (access_until_before IS NOT NULL
            AND access_until_after IS NOT NULL
            AND access_until_after > access_until_before)
    )
);

CREATE UNIQUE INDEX external_personal_access_history_command_uidx
    ON external_personal_access_history (
        company_id, personal_access_id, event_type, idempotency_key
    ) WHERE idempotency_key IS NOT NULL;

CREATE INDEX external_personal_access_history_access_time_idx
    ON external_personal_access_history (
        company_id, personal_access_id, occurred_at, id
    );

CREATE TABLE course_enrollment_access_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    enrollment_id uuid NOT NULL,
    from_access_status text,
    to_access_status text NOT NULL CHECK (to_access_status IN (
        'invited', 'ready', 'active', 'expired', 'frozen',
        'suspended', 'revoked', 'closed'
    )),
    actor_type text NOT NULL CHECK (actor_type IN ('internal', 'external', 'system')),
    actor_id uuid,
    reason text CHECK (
        reason IS NULL
        OR (btrim(reason) <> '' AND char_length(reason) <= 2000)
    ),
    access_until_before timestamptz,
    access_until_after timestamptz,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT course_enrollment_access_history_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_enrollment_access_history_from_status_check CHECK (
        from_access_status IS NULL OR from_access_status IN (
            'invited', 'ready', 'active', 'expired', 'frozen',
            'suspended', 'revoked', 'closed'
        )
    ),
    CONSTRAINT course_enrollment_access_history_actor_shape_check CHECK (
        (actor_type = 'system' AND actor_id IS NULL)
        OR (actor_type IN ('internal', 'external') AND actor_id IS NOT NULL)
    )
);

CREATE INDEX course_enrollment_access_history_enrollment_time_idx
    ON course_enrollment_access_history (
        company_id, enrollment_id, occurred_at, id
    );

CREATE FUNCTION academy_preserve_external_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'academy external history: history is immutable';
END
$$;

CREATE TRIGGER external_personal_access_history_immutable_trigger
BEFORE UPDATE OR DELETE ON external_personal_access_history
FOR EACH ROW EXECUTE FUNCTION academy_preserve_external_history();

CREATE TRIGGER course_enrollment_access_history_immutable_trigger
BEFORE UPDATE OR DELETE ON course_enrollment_access_history
FOR EACH ROW EXECUTE FUNCTION academy_preserve_external_history();
