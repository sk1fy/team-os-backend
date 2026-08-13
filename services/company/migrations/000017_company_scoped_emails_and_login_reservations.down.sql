CREATE OR REPLACE FUNCTION create_teamos_user_login() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO user_logins (company_id, user_id)
    VALUES (NEW.company_id, NEW.id);
    RETURN NEW;
END
$$;

DROP TABLE registration_login_reservations;

ALTER TABLE users
    DROP CONSTRAINT users_company_email_unique;

-- Откат намеренно завершается ошибкой, если после миграции один email уже
-- используется в нескольких компаниях: такие данные нельзя безопасно слить.
ALTER TABLE users
    ADD CONSTRAINT users_email_key UNIQUE (email);
