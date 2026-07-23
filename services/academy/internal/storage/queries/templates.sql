-- name: SeedSystemCourseTemplates :one
SELECT academy_seed_system_templates(sqlc.arg(company_id))::integer AS inserted_count;

-- name: ListSystemTemplateSeedCheckpoints :many
SELECT company_id, system_template_key, seed_version, template_id,
    template_version_id, content_hash, applied_at
FROM system_template_seed_checkpoints
WHERE company_id = sqlc.arg(company_id)
ORDER BY system_template_key, seed_version;

-- name: ListCourseTemplates :many
SELECT id, company_id, template_type, system_template_key, lifecycle_status,
    current_draft_version_id, latest_published_version_id,
    created_by_id, created_at, archived_by_id, archived_at
FROM course_templates
WHERE company_id = sqlc.arg(company_id)
  AND (sqlc.narg(template_type)::text IS NULL
       OR template_type = sqlc.narg(template_type)::text)
  AND (sqlc.narg(lifecycle_status)::text IS NULL
       OR lifecycle_status = sqlc.narg(lifecycle_status)::text)
ORDER BY template_type, created_at DESC, id;

-- name: ListCourseTemplateSummaries :many
SELECT template.id, template.company_id, template.template_type,
    template.system_template_key, template.lifecycle_status,
    template.current_draft_version_id, template.latest_published_version_id,
    template.created_by_id, template.created_at,
    template.archived_by_id, template.archived_at,
    version.title, version.description, version.cover_file_id,
    version.number AS latest_version_number,
    COALESCE((
        SELECT count(*)::integer
        FROM course_template_version_lessons AS lesson
        WHERE lesson.company_id = template.company_id
          AND lesson.template_version_id = version.id
    ), 0)::integer AS lesson_count,
    count(*) OVER() AS total_count
FROM course_templates AS template
LEFT JOIN course_template_versions AS version
  ON version.company_id = template.company_id
 AND version.template_id = template.id
 AND version.id = COALESCE(
     template.latest_published_version_id,
     template.current_draft_version_id
 )
WHERE template.company_id = sqlc.arg(company_id)
  AND (sqlc.narg(template_type)::text IS NULL
       OR template.template_type = sqlc.narg(template_type)::text)
  AND (sqlc.narg(lifecycle_status)::text IS NULL
       OR template.lifecycle_status = sqlc.narg(lifecycle_status)::text)
  AND (
      NOT sqlc.arg(require_published)::boolean
      OR template.latest_published_version_id IS NOT NULL
  )
  AND (
      sqlc.narg(query)::text IS NULL
      OR version.title ILIKE '%' || sqlc.narg(query)::text || '%'
      OR version.description ILIKE '%' || sqlc.narg(query)::text || '%'
      OR template.system_template_key ILIKE '%' || sqlc.narg(query)::text || '%'
  )
ORDER BY template.template_type, template.created_at DESC, template.id
LIMIT sqlc.arg(page_size)
OFFSET sqlc.arg(page_offset);

-- name: GetCourseTemplate :one
SELECT id, company_id, template_type, system_template_key, lifecycle_status,
    current_draft_version_id, latest_published_version_id,
    created_by_id, created_at, archived_by_id, archived_at
FROM course_templates
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id);

-- name: GetCourseTemplateForUpdate :one
SELECT id, company_id, template_type, system_template_key, lifecycle_status,
    current_draft_version_id, latest_published_version_id,
    created_by_id, created_at, archived_by_id, archived_at
FROM course_templates
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id)
FOR UPDATE;

-- name: CreateCompanyCourseTemplate :one
INSERT INTO course_templates (
    id, company_id, template_type, lifecycle_status, current_draft_version_id,
    latest_published_version_id, created_by_id, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), 'company', 'active', NULL, NULL,
    sqlc.arg(created_by_id), sqlc.arg(created_at)
)
RETURNING id, company_id, template_type, system_template_key, lifecycle_status,
    current_draft_version_id, latest_published_version_id,
    created_by_id, created_at, archived_by_id, archived_at;

-- name: ArchiveCompanyCourseTemplate :one
UPDATE course_templates
SET lifecycle_status = 'archived', archived_by_id = sqlc.arg(archived_by_id),
    archived_at = sqlc.arg(archived_at)
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND template_type = 'company'
  AND lifecycle_status = 'active'
RETURNING id, company_id, template_type, system_template_key, lifecycle_status,
    current_draft_version_id, latest_published_version_id,
    created_by_id, created_at, archived_by_id, archived_at;

-- name: ListCourseTemplateVersions :many
SELECT id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at,
    published_by_id, published_at, content_hash
FROM course_template_versions
WHERE company_id = sqlc.arg(company_id)
  AND template_id = sqlc.arg(template_id)
ORDER BY number DESC, id;

-- name: GetCourseTemplateVersion :one
SELECT id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at,
    published_by_id, published_at, content_hash
FROM course_template_versions
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id);

-- name: GetCourseTemplateVersionForUpdate :one
SELECT id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at,
    published_by_id, published_at, content_hash
FROM course_template_versions
WHERE company_id = sqlc.arg(company_id) AND id = sqlc.arg(id)
FOR UPDATE;

-- name: GetCurrentDraftCourseTemplateVersion :one
SELECT version.id, version.company_id, version.template_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.sequential, version.created_by_id, version.created_at,
    version.published_by_id, version.published_at, version.content_hash
FROM course_templates AS template
JOIN course_template_versions AS version
  ON version.id = template.current_draft_version_id
WHERE template.company_id = sqlc.arg(company_id)
  AND template.id = sqlc.arg(template_id);

-- name: GetLatestPublishedCourseTemplateVersion :one
SELECT version.id, version.company_id, version.template_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.sequential, version.created_by_id, version.created_at,
    version.published_by_id, version.published_at, version.content_hash
FROM course_templates AS template
JOIN course_template_versions AS version
  ON version.id = template.latest_published_version_id
WHERE template.company_id = sqlc.arg(company_id)
  AND template.id = sqlc.arg(template_id);

-- name: LockPublishedCourseTemplateVersionForInstantiate :one
SELECT template.id AS template_id, template.company_id,
    template.template_type, template.system_template_key,
    version.id AS template_version_id, version.number, version.title,
    version.description, version.cover_file_id, version.sequential,
    version.content_hash
FROM course_templates AS template
JOIN course_template_versions AS version
  ON version.company_id = template.company_id
 AND version.template_id = template.id
WHERE template.company_id = sqlc.arg(company_id)
  AND template.lifecycle_status = 'active'
  AND version.id = sqlc.arg(template_version_id)
  AND version.status = 'published'
FOR SHARE OF template, version;

-- name: GetNextCourseTemplateVersionNumber :one
SELECT COALESCE(max(number), 0)::integer + 1
FROM course_template_versions
WHERE company_id = sqlc.arg(company_id)
  AND template_id = sqlc.arg(template_id);

-- name: CreateCourseTemplateVersion :one
INSERT INTO course_template_versions (
    id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at
) SELECT sqlc.arg(id), template.company_id, template.id, sqlc.arg(number),
    'draft', sqlc.arg(title), sqlc.narg(description),
    sqlc.narg(cover_file_id), sqlc.arg(sequential),
    sqlc.arg(created_by_id), sqlc.arg(created_at)
FROM course_templates AS template
WHERE template.company_id = sqlc.arg(company_id)
  AND template.id = sqlc.arg(template_id)
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at,
    published_by_id, published_at, content_hash;

-- name: CreateNextDraftCourseTemplateVersion :one
INSERT INTO course_template_versions (
    id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at
) SELECT sqlc.arg(id), source.company_id, source.template_id,
    source.number + 1, 'draft', source.title, source.description,
    source.cover_file_id, source.sequential,
    sqlc.arg(created_by_id), sqlc.arg(created_at)
FROM course_template_versions AS source
JOIN course_templates AS template
  ON template.company_id = source.company_id
 AND template.id = source.template_id
WHERE source.company_id = sqlc.arg(company_id)
  AND source.id = sqlc.arg(source_version_id)
  AND source.status IN ('published', 'retired')
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at,
    published_by_id, published_at, content_hash;

-- name: UpdateDraftCourseTemplateVersion :one
UPDATE course_template_versions AS version
SET title = sqlc.arg(title), description = sqlc.narg(description),
    cover_file_id = sqlc.narg(cover_file_id),
    sequential = sqlc.arg(sequential), content_hash = NULL
FROM course_templates AS template
WHERE version.company_id = sqlc.arg(company_id)
  AND version.id = sqlc.arg(id)
  AND version.status = 'draft'
  AND template.company_id = version.company_id
  AND template.id = version.template_id
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING version.id, version.company_id, version.template_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.sequential, version.created_by_id, version.created_at,
    version.published_by_id, version.published_at, version.content_hash;

-- name: PublishCourseTemplateVersion :one
UPDATE course_template_versions AS version
SET status = 'published', published_by_id = sqlc.arg(published_by_id),
    published_at = sqlc.arg(published_at), content_hash = sqlc.arg(content_hash)
FROM course_templates AS template
WHERE version.company_id = sqlc.arg(company_id)
  AND version.template_id = sqlc.arg(template_id)
  AND version.id = sqlc.arg(id)
  AND version.status = 'draft'
  AND template.company_id = version.company_id
  AND template.id = version.template_id
  AND template.template_type = 'company'
  AND template.lifecycle_status = 'active'
RETURNING version.id, version.company_id, version.template_id, version.number,
    version.status, version.title, version.description, version.cover_file_id,
    version.sequential, version.created_by_id, version.created_at,
    version.published_by_id, version.published_at, version.content_hash;

-- name: RetireCourseTemplateVersion :one
UPDATE course_template_versions
SET status = 'retired'
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(id)
  AND status = 'published'
RETURNING id, company_id, template_id, number, status, title, description,
    cover_file_id, sequential, created_by_id, created_at,
    published_by_id, published_at, content_hash;

-- name: DeleteDraftCourseTemplateVersion :execrows
DELETE FROM course_template_versions AS version
USING course_templates AS template
WHERE version.company_id = sqlc.arg(company_id)
  AND version.id = sqlc.arg(id)
  AND version.status = 'draft'
  AND template.company_id = version.company_id
  AND template.id = version.template_id
  AND template.template_type = 'company';

-- name: SetCourseTemplateCurrentDraftVersion :execrows
UPDATE course_templates AS template
SET current_draft_version_id = version.id
FROM course_template_versions AS version
WHERE template.company_id = sqlc.arg(company_id)
  AND template.id = sqlc.arg(template_id)
  AND template.template_type = 'company'
  AND version.company_id = template.company_id
  AND version.template_id = template.id
  AND version.id = sqlc.arg(version_id)
  AND version.status = 'draft';

-- name: SetCourseTemplatePublishedVersionPointers :execrows
UPDATE course_templates AS template
SET current_draft_version_id = NULL,
    latest_published_version_id = version.id
FROM course_template_versions AS version
WHERE template.company_id = sqlc.arg(company_id)
  AND template.id = sqlc.arg(template_id)
  AND template.current_draft_version_id = version.id
  AND version.company_id = template.company_id
  AND version.template_id = template.id
  AND version.id = sqlc.arg(version_id)
  AND version.status = 'published';

-- name: ClearCourseTemplateCurrentDraftVersion :execrows
UPDATE course_templates
SET current_draft_version_id = NULL
WHERE company_id = sqlc.arg(company_id)
  AND id = sqlc.arg(template_id)
  AND current_draft_version_id = sqlc.arg(version_id)
  AND template_type = 'company';

-- name: GetCourseTemplatePublishIdempotency :one
SELECT id, company_id, template_id, idempotency_key,
    template_version_id, created_at
FROM course_template_publish_idempotency
WHERE company_id = sqlc.arg(company_id)
  AND template_id = sqlc.arg(template_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateCourseTemplatePublishIdempotency :one
INSERT INTO course_template_publish_idempotency (
    id, company_id, template_id, idempotency_key,
    template_version_id, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(template_id),
    sqlc.arg(idempotency_key), sqlc.arg(template_version_id),
    sqlc.arg(created_at)
)
ON CONFLICT (company_id, template_id, idempotency_key) DO NOTHING
RETURNING id, company_id, template_id, idempotency_key,
    template_version_id, created_at;

-- name: GetCourseTemplateInstantiationIdempotency :one
SELECT id, company_id, source_template_id, source_template_version_id,
    target_owner_type, target_owner_user_id, idempotency_key,
    target_course_id, target_course_version_id, origin_id,
    instantiated_by_id, instantiated_at
FROM course_template_instantiation_idempotency
WHERE company_id = sqlc.arg(company_id)
  AND source_template_id = sqlc.arg(source_template_id)
  AND source_template_version_id = sqlc.arg(source_template_version_id)
  AND target_owner_type = sqlc.arg(target_owner_type)
  AND target_owner_user_id IS NOT DISTINCT FROM sqlc.narg(target_owner_user_id)::uuid
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateCourseTemplateInstantiationIdempotency :one
INSERT INTO course_template_instantiation_idempotency (
    id, company_id, source_template_id, source_template_version_id,
    target_owner_type, target_owner_user_id, idempotency_key,
    target_course_id, target_course_version_id, origin_id,
    instantiated_by_id, instantiated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(company_id), sqlc.arg(source_template_id),
    sqlc.arg(source_template_version_id), sqlc.arg(target_owner_type),
    sqlc.narg(target_owner_user_id), sqlc.arg(idempotency_key),
    sqlc.arg(target_course_id), sqlc.arg(target_course_version_id),
    sqlc.arg(origin_id), sqlc.arg(instantiated_by_id), sqlc.arg(instantiated_at)
)
ON CONFLICT ON CONSTRAINT course_template_instantiation_idempotency_key
DO NOTHING
RETURNING id, company_id, source_template_id, source_template_version_id,
    target_owner_type, target_owner_user_id, idempotency_key,
    target_course_id, target_course_version_id, origin_id,
    instantiated_by_id, instantiated_at;

-- name: CreateTemplateCourseOrigin :one
INSERT INTO course_origins (
    id, company_id, target_course_id, origin_type,
    source_template_id, source_template_version_id,
    instantiated_by_id, instantiated_at, acquisition_type
) SELECT sqlc.arg(id), target.company_id, target.id, sqlc.arg(origin_type),
    sqlc.arg(source_template_id), sqlc.arg(source_template_version_id),
    sqlc.arg(instantiated_by_id), sqlc.arg(instantiated_at), 'free_copy'
FROM courses AS target
WHERE target.company_id = sqlc.arg(company_id)
  AND target.id = sqlc.arg(target_course_id)
  AND target.owner_type IN ('company', 'partner')
RETURNING id, company_id, target_course_id, origin_type,
    source_course_id, source_course_version_id, source_partner_id,
    source_template_id, source_template_version_id,
    instantiated_by_id, instantiated_at, acquisition_type, entitlement_id;
