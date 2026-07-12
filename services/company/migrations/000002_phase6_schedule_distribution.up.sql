ALTER TABLE shift_exceptions DROP CONSTRAINT IF EXISTS shift_exceptions_check1;

CREATE TABLE distribution_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    description text,
    active boolean NOT NULL DEFAULT true,
    algorithm text NOT NULL DEFAULT 'round_robin' CHECK (algorithm IN ('round_robin', 'least_loaded', 'priority')),
    member_ids uuid[] NOT NULL CHECK (cardinality(member_ids) > 0),
    disabled_member_ids uuid[] NOT NULL DEFAULT '{}',
    source text NOT NULL DEFAULT 'Все новые сделки',
    deal_limit integer NOT NULL DEFAULT 10 CHECK (deal_limit >= 1),
    unclaimed_minutes integer NOT NULL DEFAULT 15 CHECK (unclaimed_minutes >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (disabled_member_ids <@ member_ids)
);

CREATE TABLE distribution_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES distribution_groups(id) ON DELETE CASCADE,
    deal_number bigint NOT NULL,
    user_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('accepted', 'in_progress', 'reassigned', 'declined')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, deal_number)
);

CREATE INDEX distribution_groups_company_created_idx ON distribution_groups (company_id, created_at, id);
CREATE INDEX distribution_events_group_created_idx ON distribution_events (group_id, created_at DESC, id DESC);
CREATE SEQUENCE distribution_deal_numbers START WITH 4822;
