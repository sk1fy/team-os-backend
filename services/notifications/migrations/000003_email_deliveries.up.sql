CREATE TABLE email_deliveries (
  id uuid PRIMARY KEY,
  event_id uuid NOT NULL UNIQUE,
  company_id uuid NOT NULL,
  challenge_id uuid NOT NULL,
  purpose text NOT NULL CHECK (length(purpose) BETWEEN 1 AND 64),
  recipient_fingerprint bytea NOT NULL CHECK (octet_length(recipient_fingerprint) = 32),
  status text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'expired')),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 5),
  expires_at timestamptz NOT NULL,
  last_attempt_at timestamptz,
  sent_at timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (company_id, challenge_id)
);

CREATE INDEX email_deliveries_retry_idx
  ON email_deliveries (status, last_attempt_at)
  WHERE status IN ('pending', 'sending', 'failed');

CREATE INDEX email_deliveries_company_created_idx
  ON email_deliveries (company_id, created_at DESC);

COMMENT ON COLUMN email_deliveries.recipient_fingerprint IS
  'SHA-256(company_id || NUL || normalized_email); raw email is intentionally not persisted';

COMMENT ON TABLE email_deliveries IS
  'Durable state for outbound email. Verification codes and message bodies are intentionally not persisted.';
