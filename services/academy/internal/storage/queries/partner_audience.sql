-- name: GetCoursePartnerAudience :one
-- Resolves the effective partner audience for a company course. A missing
-- controls row means the default deny ('none').
SELECT COALESCE(audience.audience, 'none')::text AS audience
FROM courses AS course
LEFT JOIN course_partner_audiences AS audience
  ON audience.company_id = course.company_id
 AND audience.course_id = course.id
WHERE course.company_id = sqlc.arg(company_id) AND course.id = sqlc.arg(course_id);

-- name: ListCoursePartnerAudienceMembers :many
SELECT partner_user_id
FROM course_partner_audience_members
WHERE company_id = sqlc.arg(company_id) AND course_id = sqlc.arg(course_id)
ORDER BY partner_user_id;

-- name: CoursePartnerAudienceHasMember :one
SELECT EXISTS (
    SELECT 1 FROM course_partner_audience_members
    WHERE company_id = sqlc.arg(company_id)
      AND course_id = sqlc.arg(course_id)
      AND partner_user_id = sqlc.arg(partner_user_id)
) AS allowed;

-- name: UpsertCoursePartnerAudience :exec
INSERT INTO course_partner_audiences (company_id, course_id, audience, updated_at)
VALUES (sqlc.arg(company_id), sqlc.arg(course_id), sqlc.arg(audience), sqlc.arg(updated_at))
ON CONFLICT (company_id, course_id)
DO UPDATE SET audience = excluded.audience, updated_at = excluded.updated_at;

-- name: DeleteCoursePartnerAudienceMembers :exec
DELETE FROM course_partner_audience_members
WHERE company_id = sqlc.arg(company_id) AND course_id = sqlc.arg(course_id);

-- name: InsertCoursePartnerAudienceMember :exec
INSERT INTO course_partner_audience_members (company_id, course_id, partner_user_id)
VALUES (sqlc.arg(company_id), sqlc.arg(course_id), sqlc.arg(partner_user_id))
ON CONFLICT (company_id, course_id, partner_user_id) DO NOTHING;

-- name: ListPartnerAudienceCourseIDs :many
-- Company course ids a partner may access, resolved in one query so course
-- listings can gate partner visibility without a per-course lookup.
SELECT course.id
FROM courses AS course
LEFT JOIN course_partner_audiences AS audience
  ON audience.company_id = course.company_id
 AND audience.course_id = course.id
WHERE course.company_id = sqlc.arg(company_id)
  AND course.owner_type = 'company'
  AND (
        COALESCE(audience.audience, 'none') = 'all_partners'
        OR (COALESCE(audience.audience, 'none') = 'selected_partners'
            AND EXISTS (
                SELECT 1 FROM course_partner_audience_members AS member
                WHERE member.company_id = course.company_id
                  AND member.course_id = course.id
                  AND member.partner_user_id = sqlc.arg(partner_user_id)
            ))
      );

-- name: CountCatalogCourses :one
SELECT count(*)::bigint AS total
FROM courses AS course
LEFT JOIN course_partner_audiences AS audience
  ON audience.company_id = course.company_id
 AND audience.course_id = course.id
WHERE course.company_id = sqlc.arg(company_id)
  AND course.owner_type = 'company'
  AND course.lifecycle_status = 'active'
  AND course.distribution_status = 'active'
  AND course.status = 'published'
  AND course.visibility IN ('public', 'company')
  AND course.latest_published_version_id IS NOT NULL
  AND (
        sqlc.arg(is_partner)::boolean = false
        OR COALESCE(audience.audience, 'none') = 'all_partners'
        OR (COALESCE(audience.audience, 'none') = 'selected_partners'
            AND EXISTS (
                SELECT 1 FROM course_partner_audience_members AS member
                WHERE member.company_id = course.company_id
                  AND member.course_id = course.id
                  AND member.partner_user_id = sqlc.arg(user_id)
            ))
      )
  AND (
        sqlc.narg(search)::text IS NULL
        OR course.title ILIKE '%' || sqlc.narg(search)::text || '%'
        OR COALESCE(course.description, '') ILIKE '%' || sqlc.narg(search)::text || '%'
      );

-- name: ListCatalogCourses :many
SELECT
    course.id,
    course.title,
    course.description,
    course.cover_url,
    version.number AS latest_version_number,
    COALESCE(aggregate.lesson_count, 0)::integer AS lesson_count,
    COALESCE(aggregate.estimated_minutes, 0)::integer AS estimated_minutes
FROM courses AS course
JOIN course_versions AS version
  ON version.company_id = course.company_id
 AND version.id = course.latest_published_version_id
LEFT JOIN course_partner_audiences AS audience
  ON audience.company_id = course.company_id
 AND audience.course_id = course.id
LEFT JOIN LATERAL (
    SELECT count(*)::integer AS lesson_count,
           COALESCE(sum(lesson.estimated_minutes), 0)::integer AS estimated_minutes
    FROM course_version_lessons AS lesson
    WHERE lesson.company_id = course.company_id
      AND lesson.course_version_id = course.latest_published_version_id
) AS aggregate ON true
WHERE course.company_id = sqlc.arg(company_id)
  AND course.owner_type = 'company'
  AND course.lifecycle_status = 'active'
  AND course.distribution_status = 'active'
  AND course.status = 'published'
  AND course.visibility IN ('public', 'company')
  AND course.latest_published_version_id IS NOT NULL
  AND (
        sqlc.arg(is_partner)::boolean = false
        OR COALESCE(audience.audience, 'none') = 'all_partners'
        OR (COALESCE(audience.audience, 'none') = 'selected_partners'
            AND EXISTS (
                SELECT 1 FROM course_partner_audience_members AS member
                WHERE member.company_id = course.company_id
                  AND member.course_id = course.id
                  AND member.partner_user_id = sqlc.arg(user_id)
            ))
      )
  AND (
        sqlc.narg(search)::text IS NULL
        OR course.title ILIKE '%' || sqlc.narg(search)::text || '%'
        OR COALESCE(course.description, '') ILIKE '%' || sqlc.narg(search)::text || '%'
      )
ORDER BY lower(course.title), course.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ListUserCourseEnrollmentsForCatalog :many
-- Latest active enrollment per course for the given user, restricted to a page
-- of course ids so the catalog stays a bounded number of queries (no N+1).
SELECT DISTINCT ON (enrollment.course_id)
    enrollment.course_id,
    enrollment.id AS enrollment_id,
    COALESCE(progress.completed_count * 100 / NULLIF(progress.lesson_count, 0), 0)::integer
        AS progress_percent
FROM course_enrollments AS enrollment
LEFT JOIN LATERAL (
    SELECT count(*)::integer AS lesson_count,
           count(*) FILTER (WHERE lesson_progress.status = 'completed')::integer AS completed_count
    FROM course_version_lessons AS lesson
    LEFT JOIN enrollment_lesson_progress AS lesson_progress
      ON lesson_progress.company_id = enrollment.company_id
     AND lesson_progress.enrollment_id = enrollment.id
     AND lesson_progress.lesson_version_id = lesson.id
    WHERE lesson.company_id = enrollment.company_id
      AND lesson.course_version_id = enrollment.course_version_id
) AS progress ON true
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.learner_type = 'user'
  AND enrollment.user_id = sqlc.arg(user_id)
  AND enrollment.course_id = ANY(sqlc.arg(course_ids)::uuid[])
  AND enrollment.access_status NOT IN ('revoked', 'closed', 'expired')
ORDER BY enrollment.course_id, enrollment.created_at DESC, enrollment.id DESC;
