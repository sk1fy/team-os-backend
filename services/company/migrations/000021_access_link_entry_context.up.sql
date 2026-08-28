ALTER TABLE access_links
    ADD COLUMN entry_context text,
    ADD COLUMN entry_context_consumed_at timestamptz,
    ADD CONSTRAINT access_links_entry_context_check
        CHECK (entry_context IS NULL OR entry_context = 'company_created'),
    ADD CONSTRAINT access_links_entry_context_consumed_check
        CHECK (entry_context_consumed_at IS NULL OR entry_context IS NOT NULL);
