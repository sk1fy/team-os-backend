-- Expand academy progress into one version-pinned enrollment aggregate. Legacy
-- progress and attempt columns stay in place during the dual-read window.

-- Every legacy assignment is pinned to the snapshot which represented the
-- course when immutable versions were introduced. A missing version is a
-- migration error rather than a silent reassignment to newer content.
ALTER TABLE assignments
    ADD COLUMN course_version_id uuid;

UPDATE assignments AS assignment
SET course_version_id = version.id
FROM course_versions AS version
WHERE version.company_id = assignment.company_id
  AND version.course_id = assignment.course_id
  AND version.number = 1;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM assignments WHERE course_version_id IS NULL) THEN
        RAISE EXCEPTION 'academy enrollment backfill: assignment version 1 is missing';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM progress AS legacy_progress
        JOIN courses AS course ON course.id = legacy_progress.course_id
        WHERE course.company_id <> legacy_progress.company_id
    ) THEN
        RAISE EXCEPTION 'academy enrollment backfill: progress tenant mismatch';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM progress AS legacy_progress
        CROSS JOIN LATERAL unnest(legacy_progress.completed_lesson_ids) AS completed(lesson_id)
        WHERE NOT EXISTS (
            SELECT 1
            FROM course_version_lessons AS lesson
            JOIN course_versions AS version ON version.id = lesson.course_version_id
            WHERE lesson.id = completed.lesson_id
              AND lesson.company_id = legacy_progress.company_id
              AND version.course_id = legacy_progress.course_id
              AND version.number = 1
        )
    ) THEN
        RAISE EXCEPTION 'academy enrollment backfill: completed lesson cannot be mapped to version 1';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM quiz_attempts AS attempt
        JOIN quizzes AS quiz ON quiz.id = attempt.quiz_id
        JOIN lessons AS lesson ON lesson.id = quiz.lesson_id
        WHERE attempt.company_id <> quiz.company_id
           OR lesson.company_id <> attempt.company_id
    ) THEN
        RAISE EXCEPTION 'academy enrollment backfill: quiz attempt tenant mismatch';
    END IF;
END
$$;

ALTER TABLE assignments
    ALTER COLUMN course_version_id SET NOT NULL,
    ADD CONSTRAINT assignments_course_version_fk
        FOREIGN KEY (company_id, course_id, course_version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT;

CREATE INDEX assignments_company_version_idx
    ON assignments (company_id, course_version_id, created_at, id);

-- These tenant-qualified keys are used by progress/attempt foreign keys. The
-- row id remains the global primary key; the extra keys prevent cross-tenant
-- references even if a caller supplies otherwise valid UUIDs.
ALTER TABLE course_version_lessons
    ADD CONSTRAINT course_version_lessons_company_id_key
        UNIQUE (company_id, id);

ALTER TABLE course_version_quizzes
    ADD CONSTRAINT course_version_quizzes_company_id_key
        UNIQUE (company_id, id);

CREATE TABLE course_enrollments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    course_version_id uuid NOT NULL,
    learner_type text NOT NULL CHECK (learner_type IN ('user', 'external')),
    user_id uuid,
    external_learner_id uuid,
    source_type text NOT NULL CHECK (source_type IN (
        'assignment', 'personal_access', 'partner_promo_campaign',
        'company_candidate_campaign', 'repeat_training', 'legacy'
    )),
    source_id uuid,
    attempt_number integer NOT NULL DEFAULT 1 CHECK (attempt_number >= 1),
    progress_status text NOT NULL DEFAULT 'not_started'
        CHECK (progress_status IN ('not_started', 'in_progress', 'completed')),
    access_status text NOT NULL DEFAULT 'ready'
        CHECK (access_status IN (
            'invited', 'ready', 'active', 'expired', 'frozen',
            'suspended', 'revoked', 'closed'
        )),
    current_lesson_version_id uuid,
    activated_at timestamptz,
    access_until timestamptz,
    started_at timestamptz,
    completed_at timestamptz,
    last_activity_at timestamptz,
    frozen_at timestamptz,
    suspended_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT course_enrollments_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT course_enrollments_identity_check CHECK (
        (learner_type = 'user' AND user_id IS NOT NULL AND external_learner_id IS NULL)
        OR
        (learner_type = 'external' AND user_id IS NULL AND external_learner_id IS NOT NULL)
    ),
    CONSTRAINT course_enrollments_source_shape_check CHECK (
        (source_type = 'legacy') OR source_id IS NOT NULL
    ),
    CONSTRAINT course_enrollments_external_deadline_check CHECK (
        (learner_type = 'user' AND access_until IS NULL)
        OR
        (learner_type = 'external'
            AND (activated_at IS NULL OR access_until IS NOT NULL))
    ),
    CONSTRAINT course_enrollments_access_window_check CHECK (
        access_until IS NULL
        OR (activated_at IS NOT NULL AND access_until > activated_at)
    ),
    CONSTRAINT course_enrollments_completion_check CHECK (
        progress_status <> 'completed' OR completed_at IS NOT NULL
    ),
    CONSTRAINT course_enrollments_frozen_check CHECK (
        access_status <> 'frozen' OR frozen_at IS NOT NULL
    ),
    CONSTRAINT course_enrollments_suspended_check CHECK (
        access_status <> 'suspended' OR suspended_at IS NOT NULL
    ),
    CONSTRAINT course_enrollments_course_version_fk
        FOREIGN KEY (company_id, course_id, course_version_id)
        REFERENCES course_versions (company_id, course_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT course_enrollments_current_lesson_fk
        FOREIGN KEY (company_id, course_version_id, current_lesson_version_id)
        REFERENCES course_version_lessons (company_id, course_version_id, id)
        ON DELETE RESTRICT
);

CREATE INDEX course_enrollments_company_course_version_idx
    ON course_enrollments (company_id, course_id, course_version_id, created_at, id);

CREATE INDEX course_enrollments_company_user_created_idx
    ON course_enrollments (company_id, user_id, created_at DESC, id DESC)
    WHERE learner_type = 'user';

CREATE INDEX course_enrollments_company_external_created_idx
    ON course_enrollments (company_id, external_learner_id, created_at DESC, id DESC)
    WHERE learner_type = 'external';

CREATE INDEX course_enrollments_source_idx
    ON course_enrollments (source_type, source_id, created_at, id)
    WHERE source_id IS NOT NULL;

CREATE INDEX course_enrollments_active_until_idx
    ON course_enrollments (access_until, id)
    WHERE access_status = 'active';

CREATE UNIQUE INDEX course_enrollments_assignment_user_uidx
    ON course_enrollments (company_id, source_id, user_id)
    WHERE source_type = 'assignment' AND learner_type = 'user';

CREATE UNIQUE INDEX course_enrollments_legacy_user_course_attempt_uidx
    ON course_enrollments (company_id, user_id, course_id, attempt_number)
    WHERE source_type = 'legacy' AND learner_type = 'user';

CREATE UNIQUE INDEX course_enrollments_campaign_learner_uidx
    ON course_enrollments (source_id, external_learner_id)
    WHERE source_type IN ('partner_promo_campaign', 'company_candidate_campaign');

-- Preserve one legacy run for every employee/course pair. If the employee was
-- assigned the course, retain the earliest matching assignment as provenance.
INSERT INTO course_enrollments (
    company_id, course_id, course_version_id, learner_type, user_id,
    source_type, source_id, attempt_number, progress_status, access_status,
    current_lesson_version_id, started_at, completed_at, last_activity_at,
    created_at, updated_at
)
SELECT legacy_progress.company_id,
       legacy_progress.course_id,
       version.id,
       'user',
       legacy_progress.user_id,
       CASE WHEN source_assignment.id IS NULL THEN 'legacy' ELSE 'assignment' END,
       source_assignment.id,
       1,
       CASE
           WHEN lesson_stats.lesson_count > 0 AND next_lesson.id IS NULL
               THEN 'completed'
           WHEN legacy_progress.status IN ('in_progress', 'completed')
                OR legacy_progress.started_at IS NOT NULL
                OR cardinality(legacy_progress.completed_lesson_ids) > 0 THEN 'in_progress'
           ELSE 'not_started'
       END,
       CASE
           WHEN legacy_progress.status = 'not_started'
                AND legacy_progress.started_at IS NULL
                AND cardinality(legacy_progress.completed_lesson_ids) = 0 THEN 'ready'
           ELSE 'active'
       END,
       next_lesson.id,
       legacy_progress.started_at,
       CASE
           WHEN lesson_stats.lesson_count > 0 AND next_lesson.id IS NULL
               THEN COALESCE(legacy_progress.completed_at,
                             legacy_progress.started_at, version.created_at)
       END,
       COALESCE(legacy_progress.completed_at, legacy_progress.started_at,
                version.created_at),
       COALESCE(legacy_progress.started_at, version.created_at),
       COALESCE(legacy_progress.completed_at, legacy_progress.started_at, version.created_at)
FROM progress AS legacy_progress
JOIN course_versions AS version
  ON version.company_id = legacy_progress.company_id
 AND version.course_id = legacy_progress.course_id
 AND version.number = 1
LEFT JOIN LATERAL (
    SELECT assignment.id
    FROM assignments AS assignment
    WHERE assignment.company_id = legacy_progress.company_id
      AND assignment.course_id = legacy_progress.course_id
      AND legacy_progress.user_id = ANY(assignment.resolved_user_ids)
      AND assignment.assignee_type IN ('user', 'position', 'department')
    ORDER BY assignment.created_at, assignment.id
    LIMIT 1
) AS source_assignment ON true
LEFT JOIN LATERAL (
    SELECT lesson.id
    FROM course_version_lessons AS lesson
    JOIN course_version_sections AS section ON section.id = lesson.section_version_id
    WHERE lesson.company_id = legacy_progress.company_id
      AND lesson.course_version_id = version.id
      AND NOT (lesson.id = ANY(legacy_progress.completed_lesson_ids))
    ORDER BY section."order", lesson."order", lesson.id
    LIMIT 1
) AS next_lesson ON true
CROSS JOIN LATERAL (
    SELECT count(*)::integer AS lesson_count
    FROM course_version_lessons AS lesson
    WHERE lesson.company_id = legacy_progress.company_id
      AND lesson.course_version_id = version.id
) AS lesson_stats;

-- Assignments that had not produced a legacy progress row still receive a
-- ready enrollment. Overlapping legacy assignments deliberately reuse the
-- first employee/course enrollment instead of duplicating unknown progress.
WITH assignment_users AS (
    SELECT DISTINCT ON (assignment.company_id, assignment.course_id, member.user_id)
           assignment.id AS assignment_id,
           assignment.company_id,
           assignment.course_id,
           assignment.course_version_id,
           member.user_id,
           assignment.created_at
    FROM assignments AS assignment
    CROSS JOIN LATERAL unnest(assignment.resolved_user_ids) AS member(user_id)
    WHERE assignment.assignee_type IN ('user', 'position', 'department')
    ORDER BY assignment.company_id, assignment.course_id, member.user_id,
             assignment.created_at, assignment.id
)
INSERT INTO course_enrollments (
    company_id, course_id, course_version_id, learner_type, user_id,
    source_type, source_id, attempt_number, progress_status, access_status,
    current_lesson_version_id, created_at, updated_at
)
SELECT member.company_id,
       member.course_id,
       member.course_version_id,
       'user',
       member.user_id,
       'assignment',
       member.assignment_id,
       1,
       'not_started',
       'ready',
       first_lesson.id,
       member.created_at,
       member.created_at
FROM assignment_users AS member
LEFT JOIN LATERAL (
    SELECT lesson.id
    FROM course_version_lessons AS lesson
    JOIN course_version_sections AS section ON section.id = lesson.section_version_id
    WHERE lesson.company_id = member.company_id
      AND lesson.course_version_id = member.course_version_id
    ORDER BY section."order", lesson."order", lesson.id
    LIMIT 1
) AS first_lesson ON true
WHERE NOT EXISTS (
    SELECT 1
    FROM course_enrollments AS existing
    WHERE existing.company_id = member.company_id
      AND existing.course_id = member.course_id
      AND existing.user_id = member.user_id
);

-- A historical attempt may exist without a progress row. Create a legacy run
-- so every attempt can be attached without changing its score or timestamps.
WITH attempted_courses AS (
    SELECT DISTINCT attempt.company_id, attempt.user_id, lesson.course_id
    FROM quiz_attempts AS attempt
    JOIN quizzes AS quiz ON quiz.id = attempt.quiz_id
    JOIN lessons AS lesson ON lesson.id = quiz.lesson_id
)
INSERT INTO course_enrollments (
    company_id, course_id, course_version_id, learner_type, user_id,
    source_type, attempt_number, progress_status, access_status,
    current_lesson_version_id, started_at, last_activity_at,
    created_at, updated_at
)
SELECT attempted.company_id,
       attempted.course_id,
       version.id,
       'user',
       attempted.user_id,
       'legacy',
       1,
       'in_progress',
       'active',
       first_lesson.id,
       first_attempt.created_at,
       last_attempt.created_at,
       first_attempt.created_at,
       last_attempt.created_at
FROM attempted_courses AS attempted
JOIN course_versions AS version
  ON version.company_id = attempted.company_id
 AND version.course_id = attempted.course_id
 AND version.number = 1
JOIN LATERAL (
    SELECT min(attempt.created_at) AS created_at
    FROM quiz_attempts AS attempt
    JOIN quizzes AS quiz ON quiz.id = attempt.quiz_id
    JOIN lessons AS lesson ON lesson.id = quiz.lesson_id
    WHERE attempt.company_id = attempted.company_id
      AND attempt.user_id = attempted.user_id
      AND lesson.course_id = attempted.course_id
) AS first_attempt ON true
JOIN LATERAL (
    SELECT max(attempt.created_at) AS created_at
    FROM quiz_attempts AS attempt
    JOIN quizzes AS quiz ON quiz.id = attempt.quiz_id
    JOIN lessons AS lesson ON lesson.id = quiz.lesson_id
    WHERE attempt.company_id = attempted.company_id
      AND attempt.user_id = attempted.user_id
      AND lesson.course_id = attempted.course_id
) AS last_attempt ON true
LEFT JOIN LATERAL (
    SELECT lesson.id
    FROM course_version_lessons AS lesson
    JOIN course_version_sections AS section ON section.id = lesson.section_version_id
    WHERE lesson.company_id = attempted.company_id
      AND lesson.course_version_id = version.id
    ORDER BY section."order", lesson."order", lesson.id
    LIMIT 1
) AS first_lesson ON true
WHERE NOT EXISTS (
    SELECT 1
    FROM course_enrollments AS existing
    WHERE existing.company_id = attempted.company_id
      AND existing.course_id = attempted.course_id
      AND existing.user_id = attempted.user_id
);

CREATE TABLE enrollment_lesson_progress (
    company_id uuid NOT NULL,
    enrollment_id uuid NOT NULL,
    lesson_version_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('available', 'current', 'completed')),
    first_opened_at timestamptz,
    completed_at timestamptz,
    active_seconds bigint NOT NULL DEFAULT 0 CHECK (active_seconds >= 0),
    last_position text,
    PRIMARY KEY (enrollment_id, lesson_version_id),
    CONSTRAINT enrollment_lesson_progress_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id)
        ON DELETE CASCADE,
    CONSTRAINT enrollment_lesson_progress_lesson_fk
        FOREIGN KEY (company_id, lesson_version_id)
        REFERENCES course_version_lessons (company_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT enrollment_lesson_progress_completion_check CHECK (
        status <> 'completed' OR completed_at IS NOT NULL
    )
);

CREATE UNIQUE INDEX enrollment_lesson_progress_one_current_uidx
    ON enrollment_lesson_progress (enrollment_id)
    WHERE status = 'current';

CREATE INDEX enrollment_lesson_progress_company_lesson_idx
    ON enrollment_lesson_progress (company_id, lesson_version_id, status, enrollment_id);

-- Seed completed/current rows for sequential versions (a missing row means
-- locked) and the whole outline for non-sequential versions.
INSERT INTO enrollment_lesson_progress (
    company_id, enrollment_id, lesson_version_id, status,
    first_opened_at, completed_at, active_seconds
)
SELECT enrollment.company_id,
       enrollment.id,
       lesson.id,
       CASE
           WHEN lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
               THEN 'completed'
           WHEN lesson.id = enrollment.current_lesson_version_id THEN 'current'
           ELSE 'available'
       END,
       CASE
           WHEN lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
                OR lesson.id = enrollment.current_lesson_version_id
               THEN enrollment.started_at
       END,
       CASE
           WHEN lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
               THEN COALESCE(enrollment.completed_at, enrollment.last_activity_at,
                             enrollment.started_at, enrollment.created_at)
       END,
       0
FROM course_enrollments AS enrollment
JOIN course_versions AS version ON version.id = enrollment.course_version_id
JOIN course_version_lessons AS lesson
  ON lesson.company_id = enrollment.company_id
 AND lesson.course_version_id = enrollment.course_version_id
LEFT JOIN progress AS legacy_progress
  ON legacy_progress.company_id = enrollment.company_id
 AND legacy_progress.course_id = enrollment.course_id
 AND legacy_progress.user_id = enrollment.user_id
WHERE enrollment.progress_status <> 'not_started'
  AND (NOT version.sequential
       OR lesson.id = ANY(COALESCE(legacy_progress.completed_lesson_ids, '{}'::uuid[]))
       OR lesson.id = enrollment.current_lesson_version_id);

-- Keep the legacy attempt identity for v1 compatibility, and add the immutable
-- enrollment/version identity required by all new commands and reports.
ALTER TABLE quiz_attempts
    ADD COLUMN enrollment_id uuid,
    ADD COLUMN quiz_version_id uuid,
    ADD COLUMN answers jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN reviewed_by_id uuid,
    ADD COLUMN reviewed_at timestamptz,
    ADD COLUMN review_comment text;

UPDATE quiz_attempts AS attempt
SET enrollment_id = enrollment.id,
    quiz_version_id = version_quiz.id
FROM quizzes AS legacy_quiz
JOIN lessons AS legacy_lesson ON legacy_lesson.id = legacy_quiz.lesson_id
JOIN course_version_quizzes AS version_quiz ON version_quiz.id = legacy_quiz.id
JOIN course_enrollments AS enrollment
  ON enrollment.company_id = legacy_quiz.company_id
 AND enrollment.course_id = legacy_lesson.course_id
WHERE legacy_quiz.id = attempt.quiz_id
  AND enrollment.user_id = attempt.user_id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM quiz_attempts
        WHERE enrollment_id IS NULL OR quiz_version_id IS NULL
    ) THEN
        RAISE EXCEPTION 'academy enrollment backfill: quiz attempt cannot be pinned';
    END IF;
END
$$;

ALTER TABLE quiz_attempts
    ALTER COLUMN enrollment_id SET NOT NULL,
    ALTER COLUMN quiz_version_id SET NOT NULL,
    ADD CONSTRAINT quiz_attempts_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT quiz_attempts_version_quiz_fk
        FOREIGN KEY (company_id, quiz_version_id)
        REFERENCES course_version_quizzes (company_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT quiz_attempts_review_metadata_check CHECK (
        (reviewed_at IS NULL AND reviewed_by_id IS NULL)
        OR (reviewed_at IS NOT NULL AND reviewed_by_id IS NOT NULL)
    );

CREATE INDEX quiz_attempts_enrollment_quiz_created_idx
    ON quiz_attempts (enrollment_id, quiz_version_id, created_at DESC, id DESC);

CREATE INDEX quiz_attempts_company_pending_review_idx
    ON quiz_attempts (company_id, created_at, id)
    WHERE pending_review AND reviewed_at IS NULL;

-- Polymorphic enrollment rows need scope guards in addition to their
-- individual foreign keys: a lesson/quiz must belong to the pinned version.
CREATE FUNCTION academy_validate_enrollment_lesson_progress_scope()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM course_enrollments AS enrollment
        JOIN course_version_lessons AS lesson
          ON lesson.company_id = enrollment.company_id
         AND lesson.course_version_id = enrollment.course_version_id
        WHERE enrollment.company_id = NEW.company_id
          AND enrollment.id = NEW.enrollment_id
          AND lesson.id = NEW.lesson_version_id
    ) THEN
        RAISE EXCEPTION 'lesson does not belong to enrollment course version'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER enrollment_lesson_progress_scope_trigger
BEFORE INSERT OR UPDATE ON enrollment_lesson_progress
FOR EACH ROW EXECUTE FUNCTION academy_validate_enrollment_lesson_progress_scope();

CREATE FUNCTION academy_validate_quiz_attempt_scope()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM course_enrollments AS enrollment
        JOIN course_version_quizzes AS quiz
          ON quiz.company_id = enrollment.company_id
         AND quiz.course_version_id = enrollment.course_version_id
        WHERE enrollment.company_id = NEW.company_id
          AND enrollment.id = NEW.enrollment_id
          AND quiz.id = NEW.quiz_version_id
    ) THEN
        RAISE EXCEPTION 'quiz does not belong to enrollment course version'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER quiz_attempts_enrollment_scope_trigger
BEFORE INSERT OR UPDATE ON quiz_attempts
FOR EACH ROW EXECUTE FUNCTION academy_validate_quiz_attempt_scope();

-- External assignment is a legacy transport only. Existing rows remain
-- readable, but new external access must use a token/access aggregate.
CREATE FUNCTION academy_reject_new_external_assignment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.assignee_type = 'external' THEN
        RAISE EXCEPTION 'new external assignments are disabled'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER assignments_reject_new_external_trigger
BEFORE INSERT ON assignments
FOR EACH ROW EXECUTE FUNCTION academy_reject_new_external_assignment();
