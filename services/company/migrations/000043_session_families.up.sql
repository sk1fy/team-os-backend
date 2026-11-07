CREATE TABLE session_families (
    id uuid PRIMARY KEY,
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
ALTER TABLE sessions
    ADD COLUMN family_id uuid REFERENCES session_families(id),
    ADD COLUMN replay_until timestamptz,
    ADD COLUMN replay_nonce bytea CHECK (replay_nonce IS NULL OR octet_length(replay_nonce) = 32),
    ADD COLUMN rotation_not_before timestamptz;

WITH RECURSIVE chains AS (
    SELECT id, id AS family_id, company_id, user_id FROM sessions WHERE rotated_from IS NULL
    UNION ALL
    SELECT s.id, c.family_id, s.company_id, s.user_id
    FROM sessions s JOIN chains c ON s.rotated_from = c.id
    WHERE s.company_id = c.company_id AND s.user_id = c.user_id
), families AS (
    INSERT INTO session_families (id, company_id, user_id)
    SELECT DISTINCT family_id, company_id, user_id FROM chains
    RETURNING id
)
UPDATE sessions s SET family_id = c.family_id FROM chains c, families f
WHERE s.id = c.id AND f.id = c.family_id;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM sessions WHERE family_id IS NULL) THEN
        RAISE EXCEPTION 'Invalid refresh history: cycle or cross-account chain';
    END IF;
END $$;
CREATE INDEX sessions_family_idx ON sessions(family_id);
CREATE UNIQUE INDEX sessions_family_current_idx ON sessions(family_id)
    WHERE family_id IS NOT NULL AND revoked_at IS NULL;
ALTER TABLE sessions DROP CONSTRAINT sessions_rotated_from_fkey,
    ADD CONSTRAINT sessions_rotated_from_fkey FOREIGN KEY (rotated_from) REFERENCES sessions(id) ON DELETE SET NULL;
ALTER TABLE sessions DROP CONSTRAINT sessions_replaced_by_fkey,
    ADD CONSTRAINT sessions_replaced_by_fkey FOREIGN KEY (replaced_by) REFERENCES sessions(id) ON DELETE SET NULL;
