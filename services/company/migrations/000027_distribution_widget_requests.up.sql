ALTER TABLE distribution_service_grants DROP CONSTRAINT distribution_service_grants_capability_check;
ALTER TABLE distribution_service_grants ADD CONSTRAINT distribution_service_grants_capability_check CHECK(capability IN('widget-access','decision-validation','event-delivery','result-delivery','widget-runtime'));
CREATE TABLE distribution_widget_requests (
 company_id uuid NOT NULL REFERENCES companies(id), request_id uuid NOT NULL,
 binding_id uuid NOT NULL, employee_id uuid NOT NULL, request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','completed','rejected')),
 response jsonb, error_kind text, error_message text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(company_id,request_id)
);
