ALTER TABLE courses
    ADD COLUMN owner_type text NOT NULL DEFAULT 'company',
    ADD COLUMN owner_user_id uuid,
    ADD COLUMN created_by_id uuid,
    ADD COLUMN lifecycle_status text NOT NULL DEFAULT 'active',
    ADD COLUMN distribution_status text NOT NULL DEFAULT 'active',
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN archived_by_id uuid,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN deleted_by_id uuid;

-- Legacy rows have no reliable partner ownership metadata. Treat every
-- existing course as company-owned and preserve the author as its creator.
UPDATE courses
SET owner_type = 'company',
    owner_user_id = NULL,
    created_by_id = author_id,
    lifecycle_status = 'active',
    distribution_status = 'active';

-- created_by_id intentionally remains nullable during the expand window so
-- an older application binary can still insert a course. New writes populate
-- it; NOT NULL belongs to the later contract migration after legacy writers stop.
ALTER TABLE courses
    ADD CONSTRAINT courses_owner_type_check
        CHECK (owner_type IN ('company', 'partner')) NOT VALID,
    ADD CONSTRAINT courses_owner_shape_check
        CHECK (
            (owner_type = 'company' AND owner_user_id IS NULL)
            OR (owner_type = 'partner' AND owner_user_id IS NOT NULL)
        ) NOT VALID,
    ADD CONSTRAINT courses_lifecycle_status_check
        CHECK (lifecycle_status IN ('active', 'archived', 'deleted')) NOT VALID,
    ADD CONSTRAINT courses_distribution_status_check
        CHECK (distribution_status IN ('active', 'paused', 'blocked')) NOT VALID,
    ADD CONSTRAINT courses_archived_metadata_check
        CHECK (
            lifecycle_status <> 'archived'
            OR (archived_at IS NOT NULL AND archived_by_id IS NOT NULL)
        ) NOT VALID,
    ADD CONSTRAINT courses_deleted_metadata_check
        CHECK (
            lifecycle_status <> 'deleted'
            OR (deleted_at IS NOT NULL AND deleted_by_id IS NOT NULL)
        ) NOT VALID;

ALTER TABLE courses VALIDATE CONSTRAINT courses_owner_type_check;
ALTER TABLE courses VALIDATE CONSTRAINT courses_owner_shape_check;
ALTER TABLE courses VALIDATE CONSTRAINT courses_lifecycle_status_check;
ALTER TABLE courses VALIDATE CONSTRAINT courses_distribution_status_check;
ALTER TABLE courses VALIDATE CONSTRAINT courses_archived_metadata_check;
ALTER TABLE courses VALIDATE CONSTRAINT courses_deleted_metadata_check;

CREATE INDEX courses_company_owner_lifecycle_created_idx
    ON courses (company_id, owner_type, lifecycle_status, created_at, id);

CREATE INDEX courses_company_owner_user_lifecycle_idx
    ON courses (company_id, owner_user_id, lifecycle_status, created_at, id)
    WHERE owner_user_id IS NOT NULL;

CREATE INDEX courses_company_distribution_lifecycle_idx
    ON courses (company_id, distribution_status, lifecycle_status, created_at, id);

CREATE TABLE audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    actor_role text NOT NULL CHECK (btrim(actor_role) <> ''),
    action text NOT NULL CHECK (btrim(action) <> ''),
    aggregate_type text NOT NULL CHECK (btrim(aggregate_type) <> ''),
    aggregate_id uuid NOT NULL,
    before_state jsonb,
    after_state jsonb,
    reason text,
    request_id text,
    ip_hash text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_company_aggregate_created_idx
    ON audit_log (company_id, aggregate_type, aggregate_id, created_at DESC, id DESC);

CREATE INDEX audit_log_company_actor_created_idx
    ON audit_log (company_id, actor_id, created_at DESC, id DESC);
