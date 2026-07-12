CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE courses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text,
    cover_url text,
    status text NOT NULL CHECK (status IN ('draft', 'published')),
    author_id uuid NOT NULL,
    sequential boolean NOT NULL DEFAULT true,
    deadline_days integer CHECK (deadline_days >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX courses_company_idx ON courses (company_id, created_at);

CREATE TABLE course_sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    title text NOT NULL CHECK (btrim(title) <> ''),
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0)
);

CREATE INDEX course_sections_course_order_idx ON course_sections (course_id, "order");

CREATE TABLE lessons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    section_id uuid NOT NULL REFERENCES course_sections (id) ON DELETE CASCADE,
    title text NOT NULL CHECK (btrim(title) <> ''),
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    content jsonb NOT NULL,
    source_article_id uuid,
    -- Article title at link/replication time: kb.article.updated only renames
    -- lessons whose title still matches it (i.e. not renamed by an editor).
    source_article_title text,
    source_mode text CHECK (source_mode IN ('link', 'copy')),
    quiz_id uuid
);

CREATE INDEX lessons_course_idx ON lessons (course_id, section_id, "order");
CREATE INDEX lessons_linked_article_idx ON lessons (source_article_id) WHERE source_mode = 'link';

CREATE TABLE quizzes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    lesson_id uuid NOT NULL UNIQUE REFERENCES lessons (id) ON DELETE CASCADE,
    questions jsonb NOT NULL DEFAULT '[]',
    passing_score integer NOT NULL DEFAULT 0 CHECK (passing_score BETWEEN 0 AND 100),
    max_attempts integer CHECK (max_attempts >= 1)
);

CREATE TABLE assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    course_id uuid NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    assignee_type text NOT NULL CHECK (assignee_type IN ('user', 'position', 'department', 'external')),
    assignee_id uuid,
    invite_token text UNIQUE,
    due_date timestamptz,
    -- User list resolved via company RPC at assignment time (§9); the deadline
    -- worker uses this snapshot without synchronous cross-service calls.
    resolved_user_ids uuid[] NOT NULL DEFAULT '{}',
    due_soon_sent_at timestamptz,
    assigned_by_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX assignments_company_course_idx ON assignments (company_id, course_id);
CREATE INDEX assignments_due_soon_idx ON assignments (due_date)
    WHERE due_date IS NOT NULL AND due_soon_sent_at IS NULL;

CREATE TABLE progress (
    company_id uuid NOT NULL,
    user_id uuid NOT NULL,
    course_id uuid NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('not_started', 'in_progress', 'completed', 'overdue')),
    completed_lesson_ids uuid[] NOT NULL DEFAULT '{}',
    started_at timestamptz,
    completed_at timestamptz,
    PRIMARY KEY (user_id, course_id)
);

CREATE INDEX progress_company_course_idx ON progress (company_id, course_id);

CREATE TABLE quiz_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    quiz_id uuid NOT NULL REFERENCES quizzes (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    score integer NOT NULL CHECK (score BETWEEN 0 AND 100),
    passed boolean NOT NULL DEFAULT false,
    pending_review boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX quiz_attempts_quiz_user_idx ON quiz_attempts (quiz_id, user_id, created_at DESC);

CREATE TABLE outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    subject text NOT NULL,
    payload jsonb NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text
);

CREATE INDEX outbox_unpublished_idx ON outbox (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE TABLE processed_events (
    event_id uuid PRIMARY KEY,
    company_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);
