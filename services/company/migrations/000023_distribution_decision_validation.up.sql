-- Separate callback capability: a widget-access grant cannot authorize the
-- pre-effect business-decision validation endpoint.
ALTER TABLE distribution_service_grants DROP CONSTRAINT distribution_service_grants_capability_check;
ALTER TABLE distribution_service_grants ADD CONSTRAINT distribution_service_grants_capability_check
 CHECK(capability IN ('widget-access','decision-validation'));
