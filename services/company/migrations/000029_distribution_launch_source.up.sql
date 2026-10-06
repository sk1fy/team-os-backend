-- Launch source of a distribution rule:
--   legacy_stage     - existing behaviour: any webhook entry into the configured stage
--   creation         - a lead created in the selected pipeline (any stage inside it)
--   digital_pipeline - an authenticated Digital Pipeline widget trigger only
ALTER TABLE distribution_rules ADD COLUMN source text NOT NULL DEFAULT 'legacy_stage' CHECK (source IN ('legacy_stage', 'creation', 'digital_pipeline'));

-- Trusted trigger evidence. event_kind records the CRM event that produced the
-- entry; trigger_evidence is set only by the authenticated DP receiver, so a
-- normal webhook entry can never be mistaken for a digital_pipeline trigger.
ALTER TABLE distribution_observed_entries ADD COLUMN event_kind text NOT NULL DEFAULT '';
ALTER TABLE distribution_observed_entries ADD COLUMN trigger_evidence jsonb;
-- Group selected in the trusted Digital Pipeline settings. Admission for a
-- digital_pipeline rule requires this to equal the rule's group, so several
-- groups sharing one stage cannot steal each other's triggers.
ALTER TABLE distribution_observed_entries ADD COLUMN trigger_group_id uuid;
