-- Legacy companies stored the amoCRM Account ID only in companies. Backfill
-- the integration projection so the registration flow and future integration
-- reads agree with the existing company record.
--
-- The old schema assigned 31355990 by default, so duplicate legacy values are
-- possible. Do not attach an ambiguous account to an arbitrary company. The
-- application keeps reading companies.amo_account_id as a compatibility
-- fallback, which still blocks duplicate registration until the data is
-- reconciled explicitly.
CREATE INDEX companies_amo_account_id_idx
    ON companies (amo_account_id)
    WHERE amo_account_id IS NOT NULL;

WITH unambiguous_accounts AS (
    SELECT amo_account_id
    FROM companies
    WHERE amo_account_id IS NOT NULL
      AND btrim(amo_account_id) <> ''
    GROUP BY amo_account_id
    HAVING count(*) = 1
)
INSERT INTO company_integrations (
    id,
    company_id,
    provider,
    external_account_id,
    entitlements,
    status,
    last_verified_at,
    metadata,
    created_at,
    updated_at
)
SELECT
    gen_random_uuid(),
    company.id,
    'rakurs',
    company.amo_account_id,
    '{}',
    CASE company.status
        WHEN 'frozen' THEN 'frozen'
        WHEN 'suspended' THEN 'suspended'
        ELSE 'active'
    END,
    now(),
    jsonb_build_object('backfilledByMigration', '000011_legacy_amo_integrations'),
    now(),
    now()
FROM companies AS company
JOIN unambiguous_accounts AS account
  ON account.amo_account_id = company.amo_account_id
WHERE NOT EXISTS (
    SELECT 1
    FROM company_integrations AS integration
    WHERE integration.company_id = company.id
      AND integration.provider = 'rakurs'
)
  AND NOT EXISTS (
    SELECT 1
    FROM company_integrations AS integration
    WHERE integration.provider = 'rakurs'
      AND integration.external_account_id = company.amo_account_id
)
ON CONFLICT DO NOTHING;
