-- Downgrade only with all auth writers stopped. Original binaries cannot read the new replay protocol safely.
ALTER TABLE sessions DROP COLUMN family_id, DROP COLUMN replay_until,
    DROP COLUMN replay_nonce, DROP COLUMN rotation_not_before;
DROP TABLE session_families;
ALTER TABLE sessions DROP CONSTRAINT sessions_rotated_from_fkey,
    ADD CONSTRAINT sessions_rotated_from_fkey FOREIGN KEY (rotated_from) REFERENCES sessions(id);
ALTER TABLE sessions DROP CONSTRAINT sessions_replaced_by_fkey,
    ADD CONSTRAINT sessions_replaced_by_fkey FOREIGN KEY (replaced_by) REFERENCES sessions(id);
