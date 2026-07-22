DROP FUNCTION IF EXISTS academy_seed_system_templates(uuid);

DROP TRIGGER IF EXISTS course_template_instantiation_validate_trigger
    ON course_template_instantiation_idempotency;
DROP FUNCTION IF EXISTS academy_validate_template_instantiation();
DROP TABLE IF EXISTS course_template_instantiation_idempotency;

ALTER TABLE course_origins
    DROP CONSTRAINT IF EXISTS course_origins_source_template_version_fk,
    DROP CONSTRAINT IF EXISTS course_origins_source_template_fk,
    DROP CONSTRAINT IF EXISTS course_origins_company_id_id_key;

CREATE OR REPLACE FUNCTION academy_validate_course_origin()
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

DROP TABLE IF EXISTS course_template_publish_idempotency;
DROP TABLE IF EXISTS system_template_seed_checkpoints;
DROP TRIGGER IF EXISTS academy_file_clone_job_items_preserve_trigger
    ON academy_file_clone_job_items;
DROP TRIGGER IF EXISTS academy_file_clone_jobs_preserve_trigger
    ON academy_file_clone_jobs;
DROP FUNCTION IF EXISTS academy_preserve_completed_file_clone();
DROP TABLE IF EXISTS academy_file_clone_job_items;
DROP TABLE IF EXISTS academy_file_clone_jobs;

DROP INDEX IF EXISTS course_version_lessons_kb_snapshot_idx;
ALTER TABLE course_version_lessons
    DROP CONSTRAINT IF EXISTS course_version_lessons_template_provenance_check,
    DROP CONSTRAINT IF EXISTS course_version_lessons_source_template_version_fk,
    DROP CONSTRAINT IF EXISTS course_version_lessons_source_template_fk,
    DROP CONSTRAINT IF EXISTS course_version_lessons_kb_snapshot_shape_check,
    DROP CONSTRAINT IF EXISTS course_version_lessons_kb_snapshot_fk,
    DROP COLUMN IF EXISTS file_ids,
    DROP COLUMN IF EXISTS kb_snapshot_id;

DROP TRIGGER IF EXISTS course_templates_system_immutable_trigger ON course_templates;
DROP FUNCTION IF EXISTS academy_guard_system_template_mutation();
DROP TRIGGER IF EXISTS course_template_quizzes_immutable_trigger
    ON course_template_version_quizzes;
DROP TRIGGER IF EXISTS course_template_lessons_immutable_trigger
    ON course_template_version_lessons;
DROP TRIGGER IF EXISTS course_template_sections_immutable_trigger
    ON course_template_version_sections;
DROP FUNCTION IF EXISTS academy_guard_template_content_mutation();
DROP TRIGGER IF EXISTS course_template_versions_immutable_trigger
    ON course_template_versions;
DROP FUNCTION IF EXISTS academy_guard_template_version_mutation();

ALTER TABLE course_template_version_lessons
    DROP CONSTRAINT IF EXISTS course_template_version_lessons_quiz_fk;
DROP TABLE IF EXISTS course_template_version_quizzes;
DROP TABLE IF EXISTS course_template_version_lessons;
DROP TABLE IF EXISTS course_template_version_sections;

ALTER TABLE course_templates
    DROP CONSTRAINT IF EXISTS course_templates_current_draft_fk,
    DROP CONSTRAINT IF EXISTS course_templates_latest_published_fk;

DROP TABLE IF EXISTS course_template_versions;
DROP TABLE IF EXISTS course_templates;

DROP TRIGGER IF EXISTS kb_article_snapshots_immutable_trigger
    ON kb_article_snapshots;
DROP FUNCTION IF EXISTS academy_guard_kb_snapshot_mutation();
DROP TABLE IF EXISTS kb_article_snapshots;
