ALTER TABLE users
    DROP CONSTRAINT users_email_key;

ALTER TABLE users
    ADD CONSTRAINT users_company_email_unique UNIQUE (company_id, email);

CREATE TABLE registration_login_reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login text NOT NULL DEFAULT next_teamos_login(),
    token_hash bytea NOT NULL UNIQUE
        CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT registration_login_reservations_login_format
        CHECK (login ~ '^tm[0-9]{7}$'),
    CONSTRAINT registration_login_reservations_login_unique UNIQUE (login)
);

CREATE INDEX registration_login_reservations_cleanup_idx
    ON registration_login_reservations (expires_at, consumed_at);

-- Один sequence обслуживает и пользователей, и резервации. После полного
-- оборота пространства логинов триггер пропускает ещё активные резервации.
CREATE OR REPLACE FUNCTION create_teamos_user_login() RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    candidate text;
    attempts integer := 0;
BEGIN
    LOOP
        candidate := next_teamos_login();
        EXIT WHEN NOT EXISTS (
            SELECT 1 FROM user_logins WHERE login = candidate
        ) AND NOT EXISTS (
            SELECT 1 FROM registration_login_reservations WHERE login = candidate
        );
        attempts := attempts + 1;
        IF attempts >= 10000000 THEN
            RAISE EXCEPTION 'Свободные логины TeamOS закончились';
        END IF;
    END LOOP;

    INSERT INTO user_logins (company_id, user_id, login)
    VALUES (NEW.company_id, NEW.id, candidate);
    RETURN NEW;
END
$$;
