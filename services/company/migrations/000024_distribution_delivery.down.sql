DO $$ BEGIN IF EXISTS(SELECT 1 FROM distribution_delivery_inbox) OR EXISTS(SELECT 1 FROM distribution_operation_mirrors) OR EXISTS(SELECT 1 FROM distribution_lead_heads) OR EXISTS(SELECT 1 FROM distribution_event_receipts) OR EXISTS(SELECT 1 FROM distribution_operation_mirror_versions) THEN RAISE EXCEPTION 'distribution delivery history is retained; disable workers/grants instead'; END IF; END $$;
DROP TABLE distribution_operation_mirror_versions,distribution_operation_mirrors,distribution_observed_entries,distribution_lead_heads,distribution_event_receipts,distribution_delivery_inbox;
DELETE FROM distribution_service_grants WHERE capability IN ('event-delivery','result-delivery');
ALTER TABLE distribution_service_grants DROP CONSTRAINT distribution_service_grants_capability_check;
ALTER TABLE distribution_service_grants ADD CONSTRAINT distribution_service_grants_capability_check CHECK(capability IN ('widget-access','decision-validation'));
