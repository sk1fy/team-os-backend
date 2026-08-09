-- Preserve backfilled integrations that have acquired dependent data after the
-- migration. Deleting those rows would cascade into external identities or old
-- provisioning audit records.
DELETE FROM company_integrations AS integration
WHERE integration.metadata->>'backfilledByMigration' = '000011_legacy_amo_integrations'
  AND NOT EXISTS (
      SELECT 1
      FROM user_external_identities AS identity
      WHERE identity.integration_id = integration.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM provisioning_requests AS request
      WHERE request.integration_id = integration.id
  );

DROP INDEX IF EXISTS companies_amo_account_id_idx;
