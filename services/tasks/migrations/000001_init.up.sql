CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE boards (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    name text NOT NULL CHECK (btrim(name) <> ''),
    type text NOT NULL CHECK (type IN ('personal', 'department', 'project')),
    department_id uuid,
    owner_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX boards_company_idx ON boards (company_id);

CREATE TABLE columns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id uuid NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    color text,
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0)
);

CREATE INDEX columns_board_order_idx ON columns (board_id, "order");

CREATE TABLE tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    board_id uuid NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    column_id uuid NOT NULL REFERENCES columns (id) ON DELETE RESTRICT,
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    title text NOT NULL CHECK (btrim(title) <> ''),
    description jsonb,
    author_id uuid NOT NULL,
    assignee_ids uuid[] NOT NULL DEFAULT '{}',
    assignee_position_id uuid,
    watcher_ids uuid[] NOT NULL DEFAULT '{}',
    due_date timestamptz,
    priority text NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')) DEFAULT 'medium',
    label_ids uuid[] NOT NULL DEFAULT '{}',
    checklist jsonb NOT NULL DEFAULT '[]'::jsonb,
    attachments jsonb NOT NULL DEFAULT '[]'::jsonb,
    source jsonb,
    linked_article_ids uuid[] NOT NULL DEFAULT '{}',
    recurrence jsonb,
    recurrence_generated_at timestamptz,
    completed_at timestamptz,
    due_soon_sent_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tasks_board_column_order_idx ON tasks (board_id, column_id, "order");
CREATE INDEX tasks_company_board_idx ON tasks (company_id, board_id);
CREATE INDEX tasks_due_soon_idx ON tasks (due_date)
    WHERE completed_at IS NULL AND due_soon_sent_at IS NULL;

CREATE TABLE comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    author_id uuid NOT NULL,
    content jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX comments_task_created_idx ON comments (task_id, created_at);

CREATE TABLE labels (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    name text NOT NULL CHECK (btrim(name) <> ''),
    color text NOT NULL CHECK (btrim(color) <> '')
);

CREATE INDEX labels_company_idx ON labels (company_id);

CREATE TABLE outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    aggregate_id uuid NOT NULL,
    event_order bigserial NOT NULL,
    subject text NOT NULL,
    payload jsonb NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text
);

CREATE INDEX outbox_unpublished_idx ON outbox (company_id, aggregate_id, event_order, next_attempt_at)
    WHERE published_at IS NULL;

CREATE TABLE processed_events (
    event_id uuid PRIMARY KEY,
    company_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);
