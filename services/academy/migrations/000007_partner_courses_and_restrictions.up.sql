-- Administrative controls for partner-owned courses and immutable provenance
-- for independent company copies. Existing course/version/enrollment data is
-- intentionally preserved; all references are tenant-qualified.

CREATE TABLE course_restrictions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    restriction_type text NOT NULL CHECK (restriction_type IN ('pause', 'block')),
    reason text NOT NULL CHECK (
        btrim(reason) <> '' AND char_length(reason) <= 2000
    ),
    created_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_by_id uuid,
    resolved_at timestamptz,
    resolution_reason text CHECK (
        resolution_reason IS NULL
        OR (btrim(resolution_reason) <> '' AND char_length(resolution_reason) <= 2000)
    ),
    CONSTRAINT course_restrictions_course_fk
        FOREIGN KEY (company_id, course_id)
        REFERENCES courses (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_restrictions_resolution_shape_check CHECK (
        (resolved_at IS NULL
            AND resolved_by_id IS NULL
            AND resolution_reason IS NULL)
        OR
        (resolved_at IS NOT NULL
            AND resolved_by_id IS NOT NULL
            AND resolution_reason IS NOT NULL
            AND resolved_at >= created_at)
    )
);

CREATE UNIQUE INDEX course_restrictions_one_active_type_uidx
    ON course_restrictions (company_id, course_id, restriction_type)
    WHERE resolved_at IS NULL;

CREATE INDEX course_restrictions_course_resolved_idx
    ON course_restrictions (course_id, resolved_at, created_at DESC, id DESC);

CREATE INDEX course_restrictions_company_course_created_idx
    ON course_restrictions (company_id, course_id, created_at DESC, id DESC);

CREATE FUNCTION academy_validate_course_restriction_target()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_owner_type text;
    target_lifecycle_status text;
BEGIN
    SELECT owner_type, lifecycle_status
    INTO target_owner_type, target_lifecycle_status
    FROM courses
    WHERE company_id = NEW.company_id
      AND id = NEW.course_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'academy restriction: course does not belong to company';
    END IF;
    IF target_owner_type <> 'partner' THEN
        RAISE EXCEPTION 'academy restriction: only partner course can be restricted';
    END IF;
    IF target_lifecycle_status = 'deleted' THEN
        RAISE EXCEPTION 'academy restriction: deleted course cannot be restricted';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_restrictions_validate_target_trigger
BEFORE INSERT ON course_restrictions
FOR EACH ROW EXECUTE FUNCTION academy_validate_course_restriction_target();

CREATE FUNCTION academy_preserve_course_restriction_history()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'academy restriction: history is immutable';
    END IF;
    IF OLD.resolved_at IS NOT NULL THEN
        RAISE EXCEPTION 'academy restriction: resolved history is immutable';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.company_id IS DISTINCT FROM OLD.company_id
       OR NEW.course_id IS DISTINCT FROM OLD.course_id
       OR NEW.restriction_type IS DISTINCT FROM OLD.restriction_type
       OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.created_by_id IS DISTINCT FROM OLD.created_by_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'academy restriction: history is immutable';
    END IF;
    IF NEW.resolved_at IS NULL THEN
        RAISE EXCEPTION 'academy restriction: update must resolve restriction';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_restrictions_preserve_history_trigger
BEFORE UPDATE OR DELETE ON course_restrictions
FOR EACH ROW EXECUTE FUNCTION academy_preserve_course_restriction_history();

CREATE TABLE course_origins (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    target_course_id uuid NOT NULL,
    origin_type text NOT NULL CHECK (
        origin_type IN ('partner_course', 'system_template', 'company_template')
    ),
    source_course_id uuid,
    source_course_version_id uuid,
    source_partner_id uuid,
    source_template_id uuid,
    source_template_version_id uuid,
    instantiated_by_id uuid NOT NULL,
    instantiated_at timestamptz NOT NULL DEFAULT now(),
    acquisition_type text NOT NULL DEFAULT 'free_copy'
        CHECK (acquisition_type IN ('free_copy')),
    entitlement_id uuid,
    CONSTRAINT course_origins_target_course_fk
        FOREIGN KEY (company_id, target_course_id)
        REFERENCES courses (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_origins_source_course_fk
        FOREIGN KEY (company_id, source_course_id)
        REFERENCES courses (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_origins_source_course_version_fk
        FOREIGN KEY (company_id, source_course_id, source_course_version_id)
        REFERENCES course_versions (company_id, course_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_origins_source_shape_check CHECK (
        (origin_type = 'partner_course'
            AND source_course_id IS NOT NULL
            AND source_course_version_id IS NOT NULL
            AND source_partner_id IS NOT NULL
            AND source_template_id IS NULL
            AND source_template_version_id IS NULL)
        OR
        (origin_type IN ('system_template', 'company_template')
            AND source_course_id IS NULL
            AND source_course_version_id IS NULL
            AND source_partner_id IS NULL
            AND source_template_id IS NOT NULL
            AND source_template_version_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX course_origins_target_course_uidx
    ON course_origins (company_id, target_course_id);

CREATE INDEX course_origins_partner_source_idx
    ON course_origins (
        company_id, source_partner_id, source_course_id,
        source_course_version_id, instantiated_at DESC, id DESC
    )
    WHERE origin_type = 'partner_course';

CREATE INDEX course_origins_template_source_idx
    ON course_origins (
        company_id, source_template_id, source_template_version_id,
        instantiated_at DESC, id DESC
    )
    WHERE origin_type IN ('system_template', 'company_template');

CREATE FUNCTION academy_validate_course_origin()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_owner_type text;
    source_owner_type text;
    source_owner_user_id uuid;
    source_version_status text;
BEGIN
    SELECT owner_type
    INTO target_owner_type
    FROM courses
    WHERE company_id = NEW.company_id
      AND id = NEW.target_course_id;

    IF NOT FOUND OR target_owner_type <> 'company' THEN
        RAISE EXCEPTION 'academy origin: target must be a company course';
    END IF;

    IF NEW.origin_type = 'partner_course' THEN
        SELECT course.owner_type, course.owner_user_id, version.status
        INTO source_owner_type, source_owner_user_id, source_version_status
        FROM courses AS course
        JOIN course_versions AS version
          ON version.company_id = course.company_id
         AND version.course_id = course.id
         AND version.id = NEW.source_course_version_id
        WHERE course.company_id = NEW.company_id
          AND course.id = NEW.source_course_id;

        IF NOT FOUND
           OR source_owner_type <> 'partner'
           OR source_owner_user_id IS DISTINCT FROM NEW.source_partner_id
           OR source_version_status NOT IN ('published', 'retired') THEN
            RAISE EXCEPTION 'academy origin: source must be a published partner version';
        END IF;
    END IF;

    RETURN NEW;
END
$$;

CREATE TRIGGER course_origins_validate_trigger
BEFORE INSERT ON course_origins
FOR EACH ROW EXECUTE FUNCTION academy_validate_course_origin();

CREATE FUNCTION academy_preserve_course_origin()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'academy origin: provenance is immutable';
END
$$;

CREATE TRIGGER course_origins_preserve_trigger
BEFORE UPDATE OR DELETE ON course_origins
FOR EACH ROW EXECUTE FUNCTION academy_preserve_course_origin();

CREATE TABLE partner_course_copy_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    source_course_id uuid NOT NULL,
    source_course_version_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> '' AND octet_length(idempotency_key) <= 512
    ),
    target_course_id uuid NOT NULL,
    target_course_version_id uuid NOT NULL,
    origin_id uuid NOT NULL,
    created_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT partner_course_copy_idempotency_key
        UNIQUE (company_id, source_course_id, source_course_version_id, idempotency_key),
    CONSTRAINT partner_course_copy_idempotency_source_version_fk
        FOREIGN KEY (company_id, source_course_id, source_course_version_id)
        REFERENCES course_versions (company_id, course_id, id) ON DELETE RESTRICT,
    CONSTRAINT partner_course_copy_idempotency_target_version_fk
        FOREIGN KEY (company_id, target_course_id, target_course_version_id)
        REFERENCES course_versions (company_id, course_id, id) ON DELETE RESTRICT,
    CONSTRAINT partner_course_copy_idempotency_origin_fk
        FOREIGN KEY (origin_id)
        REFERENCES course_origins (id) ON DELETE RESTRICT
);

CREATE INDEX partner_course_copy_idempotency_target_idx
    ON partner_course_copy_idempotency (
        company_id, target_course_id, target_course_version_id
    );

-- Preserve the access state which a temporary block must restore. This is
-- distinct from progress_status: answers and progress never move backwards.
ALTER TABLE course_enrollments
    ADD COLUMN restriction_previous_access_status text,
    ADD CONSTRAINT course_enrollments_restriction_previous_access_check CHECK (
        restriction_previous_access_status IS NULL
        OR restriction_previous_access_status IN (
            'invited', 'ready', 'active', 'expired', 'frozen',
            'revoked', 'closed'
        )
    ),
    ADD CONSTRAINT course_enrollments_restriction_suspension_shape_check CHECK (
        (access_status = 'suspended'
            AND restriction_previous_access_status IS NOT NULL
            AND suspended_at IS NOT NULL)
        OR
        (restriction_previous_access_status IS NULL)
    ) NOT VALID;

-- Legacy suspended rows did not record a previous access state, so the new
-- shape is introduced as NOT VALID. New Phase 4 writes always populate it;
-- the later contract migration may validate after legacy suspension cutover.

CREATE INDEX course_enrollments_course_access_idx
    ON course_enrollments (company_id, course_id, access_status, created_at, id);
