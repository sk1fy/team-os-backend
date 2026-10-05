DO $$ BEGIN IF EXISTS(SELECT 1 FROM distribution_widget_requests) THEN RAISE EXCEPTION 'widget request evidence must be preserved'; END IF; END $$;
DROP TABLE distribution_widget_requests;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM distribution_service_grants WHERE capability='widget-runtime') THEN RAISE EXCEPTION 'widget runtime grants must be explicitly revoked before rollback'; END IF; END $$;
ALTER TABLE distribution_service_grants DROP CONSTRAINT distribution_service_grants_capability_check;
ALTER TABLE distribution_service_grants ADD CONSTRAINT distribution_service_grants_capability_check CHECK(capability IN('widget-access','decision-validation','event-delivery','result-delivery'));
