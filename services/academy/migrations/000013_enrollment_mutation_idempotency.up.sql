CREATE TABLE enrollment_mutation_idempotency (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    enrollment_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    operation text NOT NULL CHECK (operation IN (
        'complete_lesson', 'submit_quiz'
    )),
    idempotency_key text NOT NULL CHECK (
        btrim(idempotency_key) <> ''
        AND octet_length(idempotency_key) BETWEEN 8 AND 255
    ),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    result_id uuid,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT enrollment_mutation_idempotency_company_id_id_key
        UNIQUE (company_id, id),
    CONSTRAINT enrollment_mutation_idempotency_key UNIQUE (
        company_id, actor_user_id, operation, idempotency_key
    ),
    CONSTRAINT enrollment_mutation_idempotency_enrollment_fk
        FOREIGN KEY (company_id, enrollment_id)
        REFERENCES course_enrollments (company_id, id) ON DELETE RESTRICT,
    CONSTRAINT enrollment_mutation_idempotency_completion_check CHECK (
        (completed_at IS NULL AND result_id IS NULL)
        OR (completed_at IS NOT NULL
            AND (operation = 'complete_lesson' OR result_id IS NOT NULL))
    )
);

CREATE INDEX enrollment_mutation_idempotency_enrollment_idx
    ON enrollment_mutation_idempotency (
        company_id, enrollment_id, operation, created_at DESC, id DESC
    );
