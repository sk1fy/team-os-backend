-- Course templates are an independent aggregate. They deliberately do not
-- reference assignments, enrollments or progress, so instantiation can only
-- produce an independent course draft.
CREATE TABLE course_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    template_type text NOT NULL CHECK (template_type IN ('system', 'company')),
    system_template_key text,
    lifecycle_status text NOT NULL DEFAULT 'active'
        CHECK (lifecycle_status IN ('active', 'archived')),
    current_draft_version_id uuid,
    latest_published_version_id uuid,
    created_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    archived_by_id uuid,
    archived_at timestamptz,
    CONSTRAINT course_templates_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT course_templates_type_shape_check CHECK (
        (template_type = 'system'
            AND system_template_key IS NOT NULL
            AND system_template_key ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'
            AND archived_by_id IS NULL
            AND archived_at IS NULL)
        OR
        (template_type = 'company' AND system_template_key IS NULL)
    ),
    CONSTRAINT course_templates_archive_shape_check CHECK (
        (lifecycle_status = 'active'
            AND archived_by_id IS NULL AND archived_at IS NULL)
        OR
        (lifecycle_status = 'archived'
            AND archived_by_id IS NOT NULL AND archived_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX course_templates_system_key_uidx
    ON course_templates (company_id, system_template_key)
    WHERE template_type = 'system';

CREATE INDEX course_templates_company_type_lifecycle_idx
    ON course_templates (company_id, template_type, lifecycle_status, created_at DESC, id);

CREATE TABLE course_template_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    template_id uuid NOT NULL,
    number integer NOT NULL CHECK (number >= 1),
    status text NOT NULL CHECK (status IN ('draft', 'published', 'retired')),
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text,
    cover_file_id uuid,
    sequential boolean NOT NULL DEFAULT true,
    created_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_by_id uuid,
    published_at timestamptz,
    content_hash text,
    CONSTRAINT course_template_versions_template_fk
        FOREIGN KEY (company_id, template_id)
        REFERENCES course_templates (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_template_versions_company_template_id_key
        UNIQUE (company_id, template_id, id),
    CONSTRAINT course_template_versions_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT course_template_versions_template_number_key
        UNIQUE (template_id, number),
    CONSTRAINT course_template_versions_publication_shape_check CHECK (
        (status = 'draft'
            AND published_by_id IS NULL AND published_at IS NULL
            AND content_hash IS NULL)
        OR
        (status IN ('published', 'retired')
            AND published_by_id IS NOT NULL AND published_at IS NOT NULL
            AND content_hash ~ '^[0-9a-f]{64}$')
    )
);

CREATE UNIQUE INDEX course_template_versions_one_draft_uidx
    ON course_template_versions (template_id) WHERE status = 'draft';

CREATE INDEX course_template_versions_company_template_status_idx
    ON course_template_versions (company_id, template_id, status, number DESC, id);

CREATE TABLE course_template_version_sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    template_version_id uuid NOT NULL,
    stable_key uuid NOT NULL DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (btrim(title) <> ''),
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    CONSTRAINT course_template_version_sections_version_fk
        FOREIGN KEY (company_id, template_version_id)
        REFERENCES course_template_versions (company_id, id) ON DELETE CASCADE,
    CONSTRAINT course_template_version_sections_scope_key
        UNIQUE (company_id, template_version_id, id),
    CONSTRAINT course_template_version_sections_stable_key
        UNIQUE (template_version_id, stable_key)
);

CREATE INDEX course_template_version_sections_order_idx
    ON course_template_version_sections (template_version_id, "order", id);

-- The Academy stores a self-contained snapshot returned by KB. IDs below are
-- provenance only: there is intentionally no cross-service foreign key.
CREATE TABLE kb_article_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    source_article_id uuid NOT NULL,
    source_article_version_id uuid,
    source_article_version_number integer CHECK (source_article_version_number >= 1),
    reuse_grant_id uuid,
    requested_by_id uuid NOT NULL,
    requested_by_partner_id uuid,
    request_key text NOT NULL CHECK (
        btrim(request_key) <> '' AND octet_length(request_key) <= 512
    ),
    title text NOT NULL CHECK (btrim(title) <> ''),
    content jsonb NOT NULL CHECK (
        jsonb_typeof(content) = 'object' AND content ->> 'type' = 'doc'
    ),
    source_file_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
    content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT kb_article_snapshots_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT kb_article_snapshots_request_key
        UNIQUE (company_id, request_key),
    CONSTRAINT kb_article_snapshots_provenance_shape_check CHECK (
        (request_key LIKE 'legacy:%' AND reuse_grant_id IS NULL)
        OR
        (request_key NOT LIKE 'legacy:%'
            AND source_article_version_id IS NOT NULL
            AND source_article_version_number IS NOT NULL
            AND reuse_grant_id IS NOT NULL
            AND requested_by_partner_id IS NOT NULL)
    )
);

CREATE INDEX kb_article_snapshots_source_idx
    ON kb_article_snapshots (
        company_id, source_article_id, source_article_version_number,
        created_at DESC, id
    );

CREATE TABLE course_template_version_lessons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    template_version_id uuid NOT NULL,
    section_version_id uuid NOT NULL,
    stable_key uuid NOT NULL DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (btrim(title) <> ''),
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    content jsonb NOT NULL CHECK (
        jsonb_typeof(content) = 'object' AND content ->> 'type' = 'doc'
    ),
    source_type text NOT NULL DEFAULT 'manual'
        CHECK (source_type IN ('manual', 'kb_snapshot')),
    kb_snapshot_id uuid,
    estimated_minutes integer CHECK (estimated_minutes >= 1),
    file_ids uuid[] NOT NULL DEFAULT '{}'::uuid[] CHECK (
        array_position(file_ids, NULL) IS NULL
    ),
    quiz_version_id uuid,
    CONSTRAINT course_template_version_lessons_section_fk
        FOREIGN KEY (company_id, template_version_id, section_version_id)
        REFERENCES course_template_version_sections (
            company_id, template_version_id, id
        ) ON DELETE CASCADE,
    CONSTRAINT course_template_version_lessons_snapshot_fk
        FOREIGN KEY (company_id, kb_snapshot_id)
        REFERENCES kb_article_snapshots (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT course_template_version_lessons_scope_key
        UNIQUE (company_id, template_version_id, id),
    CONSTRAINT course_template_version_lessons_stable_key
        UNIQUE (template_version_id, stable_key),
    CONSTRAINT course_template_version_lessons_source_shape_check CHECK (
        (source_type = 'manual' AND kb_snapshot_id IS NULL)
        OR (source_type = 'kb_snapshot' AND kb_snapshot_id IS NOT NULL)
    )
);

CREATE INDEX course_template_version_lessons_order_idx
    ON course_template_version_lessons (
        template_version_id, section_version_id, "order", id
    );

CREATE INDEX course_template_version_lessons_snapshot_idx
    ON course_template_version_lessons (company_id, kb_snapshot_id)
    WHERE kb_snapshot_id IS NOT NULL;

CREATE TABLE course_template_version_quizzes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    template_version_id uuid NOT NULL,
    lesson_version_id uuid NOT NULL,
    questions jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(questions) = 'array'),
    passing_score integer NOT NULL CHECK (passing_score BETWEEN 0 AND 100),
    max_attempts integer CHECK (max_attempts >= 1),
    CONSTRAINT course_template_version_quizzes_lesson_fk
        FOREIGN KEY (company_id, template_version_id, lesson_version_id)
        REFERENCES course_template_version_lessons (
            company_id, template_version_id, id
        ) ON DELETE CASCADE,
    CONSTRAINT course_template_version_quizzes_scope_key
        UNIQUE (company_id, template_version_id, id),
    CONSTRAINT course_template_version_quizzes_lesson_key
        UNIQUE (lesson_version_id)
);

ALTER TABLE course_template_version_lessons
    ADD CONSTRAINT course_template_version_lessons_quiz_fk
        FOREIGN KEY (company_id, template_version_id, quiz_version_id)
        REFERENCES course_template_version_quizzes (
            company_id, template_version_id, id
        ) ON DELETE SET NULL (quiz_version_id);

ALTER TABLE course_templates
    ADD CONSTRAINT course_templates_current_draft_fk
        FOREIGN KEY (company_id, id, current_draft_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT course_templates_latest_published_fk
        FOREIGN KEY (company_id, id, latest_published_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT;

CREATE INDEX course_templates_current_draft_idx
    ON course_templates (current_draft_version_id)
    WHERE current_draft_version_id IS NOT NULL;

CREATE INDEX course_templates_latest_published_idx
    ON course_templates (latest_published_version_id)
    WHERE latest_published_version_id IS NOT NULL;

CREATE TABLE course_template_publish_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    template_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> '' AND octet_length(idempotency_key) <= 512
    ),
    template_version_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT course_template_publish_idempotency_key
        UNIQUE (company_id, template_id, idempotency_key),
    CONSTRAINT course_template_publish_idempotency_version_fk
        FOREIGN KEY (company_id, template_id, template_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT
);

CREATE TABLE system_template_seed_checkpoints (
    company_id uuid NOT NULL,
    system_template_key text NOT NULL,
    seed_version integer NOT NULL CHECK (seed_version >= 1),
    template_id uuid NOT NULL,
    template_version_id uuid NOT NULL,
    content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, system_template_key, seed_version),
    CONSTRAINT system_template_seed_checkpoints_template_fk
        FOREIGN KEY (company_id, template_id)
        REFERENCES course_templates (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT system_template_seed_checkpoints_version_fk
        FOREIGN KEY (company_id, template_id, template_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT
);

ALTER TABLE course_origins
    ADD CONSTRAINT course_origins_company_id_id_key UNIQUE (company_id, id),
    ADD CONSTRAINT course_origins_source_template_fk
        FOREIGN KEY (company_id, source_template_id)
        REFERENCES course_templates (company_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT course_origins_source_template_version_fk
        FOREIGN KEY (company_id, source_template_id, source_template_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT;

CREATE TABLE course_template_instantiation_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    source_template_id uuid NOT NULL,
    source_template_version_id uuid NOT NULL,
    target_owner_type text NOT NULL CHECK (target_owner_type IN ('company', 'partner')),
    target_owner_user_id uuid,
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> '' AND octet_length(idempotency_key) <= 512
    ),
    target_course_id uuid NOT NULL,
    target_course_version_id uuid NOT NULL,
    origin_id uuid NOT NULL,
    instantiated_by_id uuid NOT NULL,
    instantiated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT course_template_instantiation_owner_shape_check CHECK (
        (target_owner_type = 'company' AND target_owner_user_id IS NULL)
        OR (target_owner_type = 'partner' AND target_owner_user_id IS NOT NULL)
    ),
    CONSTRAINT course_template_instantiation_idempotency_key UNIQUE NULLS NOT DISTINCT (
        company_id, source_template_id, source_template_version_id,
        target_owner_type, target_owner_user_id, idempotency_key
    ),
    CONSTRAINT course_template_instantiation_source_fk
        FOREIGN KEY (company_id, source_template_id, source_template_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT course_template_instantiation_target_fk
        FOREIGN KEY (company_id, target_course_id, target_course_version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT course_template_instantiation_origin_fk
        FOREIGN KEY (company_id, origin_id)
        REFERENCES course_origins (company_id, id) ON DELETE RESTRICT
);

CREATE INDEX course_template_instantiation_target_idx
    ON course_template_instantiation_idempotency (
        company_id, target_course_id, target_course_version_id
    );

CREATE FUNCTION academy_validate_template_instantiation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_status text;
    source_lifecycle text;
    target_owner_type text;
    target_owner_user_id uuid;
    target_version_status text;
    saved_origin_type text;
    saved_origin_target uuid;
    saved_origin_template uuid;
    saved_origin_version uuid;
BEGIN
    SELECT version.status, template.lifecycle_status
    INTO source_status, source_lifecycle
    FROM course_template_versions AS version
    JOIN course_templates AS template
      ON template.company_id = version.company_id
     AND template.id = version.template_id
    WHERE version.company_id = NEW.company_id
      AND version.template_id = NEW.source_template_id
      AND version.id = NEW.source_template_version_id;

    SELECT course.owner_type, course.owner_user_id, version.status
    INTO target_owner_type, target_owner_user_id, target_version_status
    FROM courses AS course
    JOIN course_versions AS version
      ON version.company_id = course.company_id
     AND version.course_id = course.id
    WHERE course.company_id = NEW.company_id
      AND course.id = NEW.target_course_id
      AND version.id = NEW.target_course_version_id;

    SELECT origin.origin_type, origin.target_course_id,
           origin.source_template_id, origin.source_template_version_id
    INTO saved_origin_type, saved_origin_target,
         saved_origin_template, saved_origin_version
    FROM course_origins AS origin
    WHERE origin.company_id = NEW.company_id AND origin.id = NEW.origin_id;

    IF source_status IS DISTINCT FROM 'published'
       OR source_lifecycle IS DISTINCT FROM 'active' THEN
        RAISE EXCEPTION 'Источник шаблона недоступен для создания курса';
    END IF;
    IF target_version_status IS DISTINCT FROM 'draft'
       OR target_owner_type IS DISTINCT FROM NEW.target_owner_type
       OR target_owner_user_id IS DISTINCT FROM NEW.target_owner_user_id THEN
        RAISE EXCEPTION 'Владелец курса не соответствует созданию из шаблона';
    END IF;
    IF saved_origin_target IS DISTINCT FROM NEW.target_course_id
       OR saved_origin_template IS DISTINCT FROM NEW.source_template_id
       OR saved_origin_version IS DISTINCT FROM NEW.source_template_version_id
       OR saved_origin_type IS DISTINCT FROM CASE
            WHEN EXISTS (
                SELECT 1 FROM course_templates
                WHERE company_id = NEW.company_id
                  AND id = NEW.source_template_id
                  AND template_type = 'system'
            ) THEN 'system_template'
            ELSE 'company_template'
          END THEN
        RAISE EXCEPTION 'Происхождение курса не соответствует шаблону';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_template_instantiation_validate_trigger
BEFORE INSERT ON course_template_instantiation_idempotency
FOR EACH ROW EXECUTE FUNCTION academy_validate_template_instantiation();

-- CloneFilesForOwner is an external call, so its retryable state is committed
-- with the Academy transaction and processed as a saga.
CREATE TABLE academy_file_clone_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    operation_type text NOT NULL CHECK (
        operation_type IN ('template_instantiate', 'partner_course_copy', 'kb_snapshot')
    ),
    aggregate_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> '' AND octet_length(idempotency_key) <= 512
    ),
    source_owner_type text NOT NULL CHECK (
        source_owner_type IN ('course_version', 'template_version', 'kb_article_version')
    ),
    source_owner_id uuid NOT NULL,
    target_owner_type text NOT NULL CHECK (
        target_owner_type IN ('course_version', 'template_version', 'kb_snapshot')
    ),
    target_owner_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CONSTRAINT academy_file_clone_jobs_company_id_id_key UNIQUE (company_id, id),
    CONSTRAINT academy_file_clone_jobs_idempotency_key
        UNIQUE (company_id, operation_type, idempotency_key),
    CONSTRAINT academy_file_clone_jobs_completion_shape_check CHECK (
        (status = 'completed' AND completed_at IS NOT NULL AND last_error IS NULL)
        OR (status = 'failed' AND completed_at IS NULL AND last_error IS NOT NULL)
        OR (status IN ('pending', 'running')
            AND completed_at IS NULL AND last_error IS NULL)
    )
);

CREATE INDEX academy_file_clone_jobs_retry_idx
    ON academy_file_clone_jobs (next_attempt_at, created_at, id)
    WHERE status IN ('pending', 'failed');

CREATE TABLE academy_file_clone_job_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    job_id uuid NOT NULL,
    source_file_id uuid NOT NULL,
    target_file_id uuid,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'completed', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT academy_file_clone_job_items_job_fk
        FOREIGN KEY (company_id, job_id)
        REFERENCES academy_file_clone_jobs (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT academy_file_clone_job_items_source_key
        UNIQUE (job_id, source_file_id),
    CONSTRAINT academy_file_clone_job_items_result_shape_check CHECK (
        (status = 'completed' AND target_file_id IS NOT NULL AND last_error IS NULL)
        OR (status = 'failed' AND target_file_id IS NULL AND last_error IS NOT NULL)
        OR (status = 'pending' AND target_file_id IS NULL AND last_error IS NULL)
    )
);

CREATE INDEX academy_file_clone_job_items_pending_idx
    ON academy_file_clone_job_items (job_id, status, id);

CREATE FUNCTION academy_preserve_completed_file_clone()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_TABLE_NAME = 'academy_file_clone_jobs' THEN
        IF NEW.id IS DISTINCT FROM OLD.id
           OR NEW.company_id IS DISTINCT FROM OLD.company_id
           OR NEW.operation_type IS DISTINCT FROM OLD.operation_type
           OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
           OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
           OR NEW.source_owner_type IS DISTINCT FROM OLD.source_owner_type
           OR NEW.source_owner_id IS DISTINCT FROM OLD.source_owner_id
           OR NEW.target_owner_type IS DISTINCT FROM OLD.target_owner_type
           OR NEW.target_owner_id IS DISTINCT FROM OLD.target_owner_id THEN
            RAISE EXCEPTION 'Идентификаторы задания копирования файлов неизменяемы';
        END IF;
    ELSE
        IF NEW.id IS DISTINCT FROM OLD.id
           OR NEW.company_id IS DISTINCT FROM OLD.company_id
           OR NEW.job_id IS DISTINCT FROM OLD.job_id
           OR NEW.source_file_id IS DISTINCT FROM OLD.source_file_id THEN
            RAISE EXCEPTION 'Идентификаторы файла в задании копирования неизменяемы';
        END IF;
    END IF;
    IF OLD.status = 'completed' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'Завершённое копирование файлов неизменяемо';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER academy_file_clone_jobs_preserve_trigger
BEFORE UPDATE ON academy_file_clone_jobs
FOR EACH ROW EXECUTE FUNCTION academy_preserve_completed_file_clone();
CREATE TRIGGER academy_file_clone_job_items_preserve_trigger
BEFORE UPDATE ON academy_file_clone_job_items
FOR EACH ROW EXECUTE FUNCTION academy_preserve_completed_file_clone();

-- Existing KB snapshots predate reusable snapshot IDs. Materialize their
-- current content as immutable legacy snapshots without consulting KB.
INSERT INTO kb_article_snapshots (
    id, company_id, source_article_id, source_article_version_number,
    requested_by_id, request_key, title, content, content_hash, created_at
)
SELECT md5(lesson.company_id::text || ':legacy-kb-snapshot:' || lesson.id::text)::uuid,
       lesson.company_id, lesson.source_article_id, lesson.source_article_version,
       version.created_by_id, 'legacy:' || lesson.id::text,
       lesson.title, lesson.content,
       encode(digest(lesson.content::text, 'sha256'), 'hex'), version.created_at
FROM course_version_lessons AS lesson
JOIN course_versions AS version
  ON version.company_id = lesson.company_id
 AND version.id = lesson.course_version_id
WHERE lesson.source_type = 'kb_snapshot'
ON CONFLICT (company_id, request_key) DO NOTHING;

ALTER TABLE course_version_lessons
    ADD COLUMN kb_snapshot_id uuid,
    ADD COLUMN file_ids uuid[] NOT NULL DEFAULT '{}'::uuid[] CHECK (
        array_position(file_ids, NULL) IS NULL
    );

ALTER TABLE course_version_lessons
    DISABLE TRIGGER course_version_lessons_immutable_trigger;

UPDATE course_version_lessons AS lesson
SET kb_snapshot_id = snapshot.id
FROM kb_article_snapshots AS snapshot
WHERE lesson.company_id = snapshot.company_id
  AND snapshot.request_key = 'legacy:' || lesson.id::text
  AND lesson.source_type = 'kb_snapshot';

ALTER TABLE course_version_lessons
    ENABLE TRIGGER course_version_lessons_immutable_trigger;

ALTER TABLE course_version_lessons
    ADD CONSTRAINT course_version_lessons_kb_snapshot_fk
        FOREIGN KEY (company_id, kb_snapshot_id)
        REFERENCES kb_article_snapshots (company_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT course_version_lessons_source_template_fk
        FOREIGN KEY (company_id, source_template_id)
        REFERENCES course_templates (company_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT course_version_lessons_source_template_version_fk
        FOREIGN KEY (company_id, source_template_id, source_template_version_id)
        REFERENCES course_template_versions (company_id, template_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT course_version_lessons_kb_snapshot_shape_check CHECK (
        kb_snapshot_id IS NULL
        OR source_type IN ('kb_link', 'kb_snapshot')
    ),
    ADD CONSTRAINT course_version_lessons_template_provenance_check CHECK (
        source_type <> 'template_snapshot'
        OR (source_template_id IS NOT NULL
            AND source_template_version_id IS NOT NULL)
    );

CREATE INDEX course_version_lessons_kb_snapshot_idx
    ON course_version_lessons (company_id, kb_snapshot_id)
    WHERE kb_snapshot_id IS NOT NULL;

CREATE FUNCTION academy_guard_template_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.status = 'draft'
       AND NEW.status NOT IN ('draft', 'published') THEN
        RAISE EXCEPTION 'Недопустимый переход состояния версии шаблона'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' AND OLD.status <> 'draft' THEN
        RAISE EXCEPTION 'Опубликованная версия шаблона неизменяема'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.status <> 'draft' THEN
        IF to_jsonb(NEW) - 'status' IS DISTINCT FROM to_jsonb(OLD) - 'status'
           OR NOT (OLD.status = 'published' AND NEW.status = 'retired') THEN
            RAISE EXCEPTION 'Опубликованная версия шаблона неизменяема'
                USING ERRCODE = '55000';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_template_versions_immutable_trigger
BEFORE UPDATE OR DELETE ON course_template_versions
FOR EACH ROW EXECUTE FUNCTION academy_guard_template_version_mutation();

CREATE FUNCTION academy_guard_template_content_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    old_status text;
    new_status text;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        SELECT status INTO old_status FROM course_template_versions
        WHERE id = OLD.template_version_id;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        SELECT status INTO new_status FROM course_template_versions
        WHERE id = NEW.template_version_id;
    END IF;
    IF (TG_OP IN ('UPDATE', 'DELETE') AND old_status IS DISTINCT FROM 'draft')
       OR (TG_OP IN ('INSERT', 'UPDATE') AND new_status IS DISTINCT FROM 'draft') THEN
        RAISE EXCEPTION 'Содержимое опубликованной версии шаблона неизменяемо'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER course_template_sections_immutable_trigger
BEFORE INSERT OR UPDATE OR DELETE ON course_template_version_sections
FOR EACH ROW EXECUTE FUNCTION academy_guard_template_content_mutation();
CREATE TRIGGER course_template_lessons_immutable_trigger
BEFORE INSERT OR UPDATE OR DELETE ON course_template_version_lessons
FOR EACH ROW EXECUTE FUNCTION academy_guard_template_content_mutation();
CREATE TRIGGER course_template_quizzes_immutable_trigger
BEFORE INSERT OR UPDATE OR DELETE ON course_template_version_quizzes
FOR EACH ROW EXECUTE FUNCTION academy_guard_template_content_mutation();

CREATE FUNCTION academy_guard_kb_snapshot_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Снимок статьи базы знаний неизменяем' USING ERRCODE = '55000';
END
$$;
CREATE TRIGGER kb_article_snapshots_immutable_trigger
BEFORE UPDATE OR DELETE ON kb_article_snapshots
FOR EACH ROW EXECUTE FUNCTION academy_guard_kb_snapshot_mutation();

CREATE FUNCTION academy_guard_system_template_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.template_type = 'system'
       AND current_setting('academy.system_template_seed', true) IS DISTINCT FROM 'on' THEN
        RAISE EXCEPTION 'Системный шаблон неизменяем' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER course_templates_system_immutable_trigger
BEFORE UPDATE OR DELETE ON course_templates
FOR EACH ROW EXECUTE FUNCTION academy_guard_system_template_mutation();

CREATE OR REPLACE FUNCTION academy_validate_course_origin()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_owner_type text;
    target_owner_user_id uuid;
    source_owner_type text;
    source_owner_user_id uuid;
    source_version_status text;
    source_template_type text;
    source_template_lifecycle text;
BEGIN
    SELECT owner_type, owner_user_id
    INTO target_owner_type, target_owner_user_id
    FROM courses
    WHERE company_id = NEW.company_id AND id = NEW.target_course_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'Курс назначения принадлежит другой компании';
    END IF;

    IF NEW.origin_type = 'partner_course' THEN
        IF target_owner_type <> 'company' THEN
            RAISE EXCEPTION 'Копия партнёрского курса должна принадлежать компании';
        END IF;
        SELECT course.owner_type, course.owner_user_id, version.status
        INTO source_owner_type, source_owner_user_id, source_version_status
        FROM courses AS course
        JOIN course_versions AS version
          ON version.company_id = course.company_id
         AND version.course_id = course.id
         AND version.id = NEW.source_course_version_id
        WHERE course.company_id = NEW.company_id
          AND course.id = NEW.source_course_id;
        IF NOT FOUND OR source_owner_type <> 'partner'
           OR source_owner_user_id IS DISTINCT FROM NEW.source_partner_id
           OR source_version_status NOT IN ('published', 'retired') THEN
            RAISE EXCEPTION 'Источник должен быть опубликованной версией партнёрского курса';
        END IF;
    ELSE
        SELECT template.template_type, template.lifecycle_status, version.status
        INTO source_template_type, source_template_lifecycle, source_version_status
        FROM course_templates AS template
        JOIN course_template_versions AS version
          ON version.company_id = template.company_id
         AND version.template_id = template.id
         AND version.id = NEW.source_template_version_id
        WHERE template.company_id = NEW.company_id
          AND template.id = NEW.source_template_id;
        IF NOT FOUND
           OR source_template_type <> CASE NEW.origin_type
                WHEN 'system_template' THEN 'system' ELSE 'company' END
           OR source_template_lifecycle <> 'active'
           OR source_version_status <> 'published' THEN
            RAISE EXCEPTION 'Источник должен быть активным опубликованным шаблоном';
        END IF;
        IF target_owner_type = 'partner' AND target_owner_user_id IS NULL THEN
            RAISE EXCEPTION 'У партнёрского курса должен быть владелец';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

-- Idempotent per-company seed. The same function is used by the migration
-- backfill and by the company.created.v1 consumer.
CREATE FUNCTION academy_seed_system_templates(p_company_id uuid)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    seed record;
    v_template_id uuid;
    v_version_id uuid;
    v_section_one_id uuid;
    v_section_two_id uuid;
    v_lesson_one_id uuid;
    v_lesson_two_id uuid;
    v_lesson_three_id uuid;
    v_quiz_id uuid;
    v_content_hash text;
    v_inserted_count integer := 0;
    v_system_actor constant uuid := '00000000-0000-0000-0000-000000000001';
    v_seed_time constant timestamptz := '2026-07-22 00:00:00+00';
BEGIN
    IF p_company_id IS NULL THEN
        RAISE EXCEPTION 'Для системных шаблонов требуется компания';
    END IF;
    PERFORM set_config('academy.system_template_seed', 'on', true);

    FOR seed IN
        SELECT * FROM (VALUES
            ('employee-onboarding', 'Адаптация нового сотрудника',
             'Готовый маршрут от знакомства с компанией до плана первой недели.',
             'Добро пожаловать', 'Рабочий старт',
             'Компания и команда', 'Правила взаимодействия', 'План первой недели'),
            ('sales-manager-onboarding', 'Адаптация менеджера по продажам',
             'Воронка, квалификация клиента и стандарты первой сделки.',
             'Основа продаж', 'Практика сделки',
             'Ценность для клиента', 'Этапы воронки', 'Следующий шаг по сделке'),
            ('manager-onboarding', 'Адаптация руководителя',
             'Ритм управления, постановка задач и обратная связь команде.',
             'Роль руководителя', 'Управленческий ритм',
             'Цели команды', 'Делегирование', 'Развивающая обратная связь'),
            ('company-and-product-intro', 'Компания и продукт',
             'Единое введение в миссию, аудиторию и ценность продукта.',
             'О компании', 'О продукте',
             'Миссия и принципы', 'Клиенты и задачи', 'Ценность продукта'),
            ('information-security', 'Информационная безопасность',
             'Базовые правила защиты учётных записей, данных и устройств.',
             'Защита доступа', 'Работа с данными',
             'Надёжные пароли', 'Фишинг и сообщения', 'Инциденты безопасности'),
            ('customer-service-standards', 'Стандарт клиентского сервиса',
             'Практика общения, фиксации договорённостей и работы с претензиями.',
             'Коммуникация', 'Сложные ситуации',
             'Ожидания клиента', 'Договорённости', 'Работа с претензией'),
            ('crm-basics', 'Основы работы в CRM',
             'Карточка клиента, этапы сделки и обязательная фиксация следующего шага.',
             'Данные клиента', 'Работа со сделкой',
             'Карточка клиента', 'Этапы воронки', 'Следующая задача в CRM'),
            ('regulations-knowledge-check', 'Проверка знания регламентов',
             'Как находить актуальный регламент, применять его и сообщать о расхождениях.',
             'Работа с регламентом', 'Проверка знаний',
             'Источник правил', 'Применение регламента', 'Разбор рабочей ситуации'),
            ('intern-preparation', 'Подготовка стажёра',
             'Безопасный старт стажировки, учебный маршрут и критерии готовности.',
             'Начало стажировки', 'Первые задачи',
             'Роль наставника', 'Учебный маршрут', 'Готовность к самостоятельной работе'),
            ('external-partner-course', 'Курс для внешнего партнёра',
             'Знакомство с правилами сотрудничества, продуктом и каналами поддержки.',
             'Сотрудничество', 'Практическая работа',
             'Формат взаимодействия', 'Продукт и аудитория', 'Канал поддержки партнёра')
        ) AS catalog(
            template_key, title, description, section_one, section_two,
            lesson_one, lesson_two, lesson_three
        )
    LOOP
        v_template_id := md5(p_company_id::text || ':system-template:' || seed.template_key)::uuid;
        v_version_id := md5(v_template_id::text || ':version:1')::uuid;
        v_section_one_id := md5(v_version_id::text || ':section:1')::uuid;
        v_section_two_id := md5(v_version_id::text || ':section:2')::uuid;
        v_lesson_one_id := md5(v_version_id::text || ':lesson:1')::uuid;
        v_lesson_two_id := md5(v_version_id::text || ':lesson:2')::uuid;
        v_lesson_three_id := md5(v_version_id::text || ':lesson:3')::uuid;
        v_quiz_id := md5(v_version_id::text || ':quiz:1')::uuid;

        INSERT INTO course_templates (
            id, company_id, template_type, system_template_key,
            lifecycle_status, created_by_id, created_at
        ) VALUES (
            v_template_id, p_company_id, 'system', seed.template_key,
            'active', v_system_actor, v_seed_time
        ) ON CONFLICT (company_id, system_template_key)
          WHERE template_type = 'system' DO NOTHING;

        IF NOT EXISTS (
            SELECT 1 FROM course_template_versions AS version
            WHERE version.company_id = p_company_id
              AND version.template_id = v_template_id
              AND number = 1
        ) THEN
            INSERT INTO course_template_versions (
                id, company_id, template_id, number, status, title,
                description, sequential, created_by_id, created_at
            ) VALUES (
                v_version_id, p_company_id, v_template_id, 1, 'draft', seed.title,
                seed.description, true, v_system_actor, v_seed_time
            );
            INSERT INTO course_template_version_sections (
                id, company_id, template_version_id, stable_key, title, "order"
            ) VALUES
                (v_section_one_id, p_company_id, v_version_id, v_section_one_id,
                 seed.section_one, 0),
                (v_section_two_id, p_company_id, v_version_id, v_section_two_id,
                 seed.section_two, 1);
            INSERT INTO course_template_version_lessons (
                id, company_id, template_version_id, section_version_id,
                stable_key, title, "order", content, source_type,
                estimated_minutes
            ) VALUES
                (v_lesson_one_id, p_company_id, v_version_id, v_section_one_id,
                 v_lesson_one_id, seed.lesson_one, 0,
                 jsonb_build_object('type', 'doc', 'content', jsonb_build_array(
                     jsonb_build_object('type', 'heading', 'attrs',
                         jsonb_build_object('level', 2), 'content', jsonb_build_array(
                             jsonb_build_object('type', 'text', 'text', seed.lesson_one))),
                     jsonb_build_object('type', 'paragraph', 'content', jsonb_build_array(
                         jsonb_build_object('type', 'text', 'text',
                             'Разберите ключевые принципы темы и обсудите, как они применяются в вашей команде.')))
                 )), 'manual', 12),
                (v_lesson_two_id, p_company_id, v_version_id, v_section_one_id,
                 v_lesson_two_id, seed.lesson_two, 1,
                 jsonb_build_object('type', 'doc', 'content', jsonb_build_array(
                     jsonb_build_object('type', 'heading', 'attrs',
                         jsonb_build_object('level', 2), 'content', jsonb_build_array(
                             jsonb_build_object('type', 'text', 'text', seed.lesson_two))),
                     jsonb_build_object('type', 'paragraph', 'content', jsonb_build_array(
                         jsonb_build_object('type', 'text', 'text',
                             'Зафиксируйте рабочий алгоритм, ответственных и ожидаемый результат.')))
                 )), 'manual', 15),
                (v_lesson_three_id, p_company_id, v_version_id, v_section_two_id,
                 v_lesson_three_id, seed.lesson_three, 0,
                 jsonb_build_object('type', 'doc', 'content', jsonb_build_array(
                     jsonb_build_object('type', 'heading', 'attrs',
                         jsonb_build_object('level', 2), 'content', jsonb_build_array(
                             jsonb_build_object('type', 'text', 'text', seed.lesson_three))),
                     jsonb_build_object('type', 'paragraph', 'content', jsonb_build_array(
                         jsonb_build_object('type', 'text', 'text',
                             'Выполните практическое задание и проверьте понимание в итоговом тесте.')))
                 )), 'manual', 18);
            INSERT INTO course_template_version_quizzes (
                id, company_id, template_version_id, lesson_version_id,
                questions, passing_score, max_attempts
            ) VALUES (
                v_quiz_id, p_company_id, v_version_id, v_lesson_three_id,
                jsonb_build_array(jsonb_build_object(
                    'id', 'q1', 'type', 'single',
                    'text', 'Как лучше закрепить результат этого модуля?',
                    'options', jsonb_build_array(
                        jsonb_build_object('id', 'a', 'text',
                            'Применить алгоритм на практике и зафиксировать результат',
                            'correct', true),
                        jsonb_build_object('id', 'b', 'text',
                            'Пропустить практику и не обсуждать вопросы',
                            'correct', false)
                    )
                )), 80, 3
            );
            UPDATE course_template_version_lessons
            SET quiz_version_id = v_quiz_id WHERE id = v_lesson_three_id;

            SELECT encode(digest(jsonb_build_object(
                'title', seed.title, 'description', seed.description,
                'sequential', true,
                'sections', jsonb_build_array(seed.section_one, seed.section_two),
                'lessons', jsonb_build_array(seed.lesson_one, seed.lesson_two, seed.lesson_three),
                'quiz', 'q1'
            )::text, 'sha256'), 'hex') INTO v_content_hash;

            UPDATE course_template_versions
            SET status = 'published', published_by_id = v_system_actor,
                published_at = v_seed_time, content_hash = v_content_hash
            WHERE id = v_version_id AND status = 'draft';
            v_inserted_count := v_inserted_count + 1;
        END IF;

        UPDATE course_templates
        SET current_draft_version_id = NULL,
            latest_published_version_id = v_version_id
        WHERE company_id = p_company_id AND id = v_template_id;

        INSERT INTO system_template_seed_checkpoints (
            company_id, system_template_key, seed_version,
            template_id, template_version_id, content_hash, applied_at
        )
        SELECT p_company_id, seed.template_key, 1, v_template_id,
               v_version_id, version.content_hash, v_seed_time
        FROM course_template_versions AS version
        WHERE version.company_id = p_company_id AND version.id = v_version_id
        ON CONFLICT (company_id, system_template_key, seed_version) DO NOTHING;
    END LOOP;
    RETURN v_inserted_count;
END
$$;

DO $$
DECLARE tenant record;
BEGIN
    FOR tenant IN SELECT DISTINCT company_id FROM courses LOOP
        PERFORM academy_seed_system_templates(tenant.company_id);
    END LOOP;
END
$$;
