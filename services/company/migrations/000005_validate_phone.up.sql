UPDATE users
SET phone = NULL,
    updated_at = now()
WHERE phone IS NOT NULL
  AND NOT (
      phone = btrim(phone)
      AND phone ~ '^\+?[0-9() -]+$'
      AND length(regexp_replace(phone, '[^0-9]', '', 'g')) BETWEEN 7 AND 15
  );

ALTER TABLE users
ADD CONSTRAINT users_phone_format CHECK (
    phone IS NULL OR (
        phone = btrim(phone)
        AND phone ~ '^\+?[0-9() -]+$'
        AND length(regexp_replace(phone, '[^0-9]', '', 'g')) BETWEEN 7 AND 15
    )
);
