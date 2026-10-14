CREATE INDEX distribution_delivery_stage_source_history
ON distribution_delivery_inbox(company_id,binding_id,binding_revision,account_id,lead_id)
WHERE message_kind='event' AND payload->'event'->>'kind'='lead.status_changed';

CREATE INDEX distribution_delivery_source_event
ON distribution_delivery_inbox(company_id,binding_id,binding_revision,event_id)
WHERE message_kind='event';
