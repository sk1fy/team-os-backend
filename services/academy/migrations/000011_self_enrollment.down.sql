DROP INDEX IF EXISTS course_enrollments_self_enrollment_uidx;

-- Preserve learner progress while narrowing the historical enum. Legacy is
-- the only source kind whose source_id has no live command aggregate.
UPDATE course_enrollments
SET source_type = 'legacy', source_id = NULL
WHERE source_type = 'self_enrollment';

DROP INDEX IF EXISTS assignments_recipient_uidx;

-- Rollback keeps one assignment per active uniqueness key. If an assignment
-- was revoked and later recreated, the active row wins deterministically.
DELETE FROM assignments AS old
USING assignments AS kept
WHERE old.company_id = kept.company_id
  AND old.course_id = kept.course_id
  AND old.assignee_type = kept.assignee_type
  AND old.assignee_id = kept.assignee_id
  AND old.id <> kept.id
  AND old.revoked_at IS NOT NULL
  AND (kept.revoked_at IS NULL OR (old.created_at, old.id) < (kept.created_at, kept.id));

CREATE UNIQUE INDEX assignments_recipient_uidx
    ON assignments (company_id, course_id, assignee_type, assignee_id)
    WHERE assignee_id IS NOT NULL;

ALTER TABLE assignments
    DROP COLUMN revoked_by_id,
    DROP COLUMN revoked_at;

ALTER TABLE course_enrollments
    DROP CONSTRAINT course_enrollments_source_type_check;

ALTER TABLE course_enrollments
    ADD CONSTRAINT course_enrollments_source_type_check CHECK (source_type IN (
        'assignment', 'personal_access', 'partner_promo_campaign',
        'company_candidate_campaign', 'repeat_training', 'legacy'
    ));
