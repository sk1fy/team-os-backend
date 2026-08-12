CREATE SEQUENCE teamos_login_seq
    AS bigint
    MINVALUE 0
    MAXVALUE 9999999
    START WITH 0
    CYCLE;

CREATE FUNCTION next_teamos_login() RETURNS text
LANGUAGE sql
VOLATILE
PARALLEL UNSAFE
AS $$
    SELECT 'tm' || lpad(
        (((nextval('teamos_login_seq') * 7415541 + 8901912) % 10000000)::bigint)::text,
        7,
        '0'
    )
$$;

-- Не допускаем вставку пользователя между backfill и установкой триггера.
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF (SELECT count(*) FROM users) > 10000000 THEN
        RAISE EXCEPTION 'Невозможно выдать семизначные логины более чем 10000000 пользователям';
    END IF;
END
$$;

CREATE TABLE user_logins (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid PRIMARY KEY,
    login text NOT NULL DEFAULT next_teamos_login(),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_logins_user_fk
        FOREIGN KEY (company_id, user_id) REFERENCES users(company_id, id) ON DELETE CASCADE,
    CONSTRAINT user_logins_login_format CHECK (login ~ '^tm[0-9]{7}$'),
    CONSTRAINT user_logins_login_unique UNIQUE (login)
);

WITH numbered AS (
    SELECT id, row_number() OVER (ORDER BY created_at, id) - 1 AS number
    FROM users
)
INSERT INTO user_logins (company_id, user_id, login)
SELECT users.company_id, users.id, 'tm' || lpad(
    (((numbered.number * 7415541 + 8901912) % 10000000)::bigint)::text,
    7,
    '0'
)
FROM users
JOIN numbered ON numbered.id = users.id;

SELECT setval('teamos_login_seq', (SELECT count(*) % 10000000 FROM users), false);

CREATE FUNCTION create_teamos_user_login() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO user_logins (company_id, user_id)
    VALUES (NEW.company_id, NEW.id);
    RETURN NEW;
END
$$;

CREATE TRIGGER users_create_login
AFTER INSERT ON users
FOR EACH ROW
EXECUTE FUNCTION create_teamos_user_login();
