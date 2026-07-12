CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    name text NOT NULL CHECK (btrim(name) <> ''),
    parent_id uuid REFERENCES sections (id) ON DELETE RESTRICT,
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    access jsonb NOT NULL DEFAULT '{"scope":"company","departmentIds":[],"positionIds":[],"userIds":[]}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sections_company_parent_order_idx ON sections (company_id, parent_id, "order");

CREATE TABLE articles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    section_id uuid NOT NULL REFERENCES sections (id) ON DELETE RESTRICT,
    title text NOT NULL CHECK (btrim(title) <> ''),
    content jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('draft', 'published')),
    author_id uuid NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    requires_acknowledgement boolean NOT NULL DEFAULT false,
    plain_text text NOT NULL DEFAULT '',
    search tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('russian', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('russian', coalesce(plain_text, '')), 'B')
    ) STORED,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX articles_company_section_idx ON articles (company_id, section_id);
CREATE INDEX articles_search_idx ON articles USING gin (search);

CREATE TABLE article_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    article_id uuid NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version >= 1),
    title text NOT NULL,
    content jsonb NOT NULL,
    author_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX article_versions_article_created_idx ON article_versions (article_id, created_at DESC);

CREATE TABLE acknowledgements (
    company_id uuid NOT NULL,
    article_id uuid NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    acknowledged_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, user_id)
);

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
