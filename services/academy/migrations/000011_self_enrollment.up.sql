ALTER TABLE course_enrollments
    DROP CONSTRAINT course_enrollments_source_type_check;

ALTER TABLE course_enrollments
    ADD CONSTRAINT course_enrollments_source_type_check CHECK (source_type IN (
        'assignment', 'personal_access', 'partner_promo_campaign',
        'company_candidate_campaign', 'repeat_training', 'legacy',
        'self_enrollment'
    ));

CREATE UNIQUE INDEX course_enrollments_self_enrollment_uidx
    ON course_enrollments (company_id, user_id, course_id)
    WHERE source_type = 'self_enrollment' AND learner_type = 'user';

ALTER TABLE assignments
    ADD COLUMN revoked_at timestamptz,
    ADD COLUMN revoked_by_id uuid;

DROP INDEX assignments_recipient_uidx;

CREATE UNIQUE INDEX assignments_recipient_uidx
    ON assignments (company_id, course_id, assignee_type, assignee_id)
    WHERE assignee_id IS NOT NULL AND revoked_at IS NULL;
