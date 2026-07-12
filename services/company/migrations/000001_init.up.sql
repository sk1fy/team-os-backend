CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE companies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    logo_url text,
    owner_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    email citext NOT NULL UNIQUE,
    first_name text NOT NULL,
    last_name text NOT NULL,
    phone text,
    avatar_url text,
    role text NOT NULL CHECK (role IN ('owner', 'admin', 'employee', 'partner')),
    status text NOT NULL CHECK (status IN ('active', 'invited', 'deactivated')),
    birth_date date,
    hired_at date,
    vacation_allowance smallint CHECK (vacation_allowance BETWEEN 0 AND 366),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE companies
    ADD CONSTRAINT companies_owner_fk
    FOREIGN KEY (owner_id) REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE credentials (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at timestamptz,
    rotated_from uuid REFERENCES sessions(id),
    replaced_by uuid REFERENCES sessions(id),
    user_agent text,
    ip_address inet
);

CREATE INDEX sessions_user_active_idx
    ON sessions (user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    email citext,
    token text NOT NULL UNIQUE,
    role text NOT NULL CHECK (role IN ('owner', 'admin', 'employee', 'partner')),
    position_id uuid,
    department_id uuid,
    invited_by_id uuid NOT NULL REFERENCES users(id),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'expired')),
    expires_at timestamptz NOT NULL DEFAULT (now() + interval '7 days'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX invites_company_created_idx ON invites (company_id, created_at DESC);

CREATE TABLE departments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    parent_id uuid REFERENCES departments(id),
    head_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    valuable_final_product text,
    "order" integer NOT NULL DEFAULT 0 CHECK ("order" >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (company_id, parent_id, "order") DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX departments_company_parent_idx ON departments (company_id, parent_id, "order");

CREATE TABLE positions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    department_id uuid NOT NULL REFERENCES departments(id),
    level smallint NOT NULL DEFAULT 0 CHECK (level BETWEEN 0 AND 4),
    description text,
    article_ids uuid[] NOT NULL DEFAULT '{}',
    required_course_ids uuid[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX positions_company_department_idx ON positions (company_id, department_id);

CREATE TABLE user_positions (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    position_id uuid NOT NULL REFERENCES positions(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, position_id),
    UNIQUE (user_id)
);

CREATE INDEX user_positions_position_idx ON user_positions (position_id);

CREATE TABLE user_schedules (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    template jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE shift_exceptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date date NOT NULL,
    type text NOT NULL CHECK (type IN ('work', 'off', 'vacation', 'sick', 'trip')),
    start_time time,
    end_time time,
    note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, date),
    CHECK ((type = 'work' AND start_time IS NOT NULL AND end_time IS NOT NULL) OR type <> 'work'),
    CHECK (start_time IS NULL OR end_time IS NULL OR start_time < end_time)
);

CREATE INDEX shift_exceptions_company_date_idx ON shift_exceptions (company_id, date);

CREATE TABLE outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
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
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    processed_at timestamptz NOT NULL DEFAULT now()
);
