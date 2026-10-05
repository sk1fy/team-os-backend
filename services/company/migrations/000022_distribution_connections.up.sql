-- Company-local references to Core are UUID values, never foreign database FKs.
CREATE TABLE distribution_bindings (
 id uuid PRIMARY KEY, company_id uuid NOT NULL REFERENCES companies(id),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 9007199254740991),
 installation_id uuid NOT NULL, integration_id uuid NOT NULL, account_id text NOT NULL CHECK(account_id ~ '^[1-9][0-9]{0,18}$'),
 state text NOT NULL CHECK(state IN ('pending','active','revoked')),
 mapping_revision bigint NOT NULL DEFAULT 0 CHECK(mapping_revision BETWEEN 0 AND 9007199254740991),
 mapping_ack_revision bigint NOT NULL DEFAULT 0 CHECK(mapping_ack_revision BETWEEN 0 AND mapping_revision), intent_id uuid NOT NULL UNIQUE,
 initiated_by uuid, initiator_snapshot uuid NOT NULL, expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(company_id,initiated_by) REFERENCES users(company_id,id) ON DELETE SET NULL (initiated_by),
 UNIQUE(company_id,id), UNIQUE(company_id,id,revision),
 CHECK(account_id::numeric <= 9223372036854775807)
);
CREATE UNIQUE INDEX distribution_one_current_company ON distribution_bindings(company_id) WHERE state <> 'revoked';
CREATE UNIQUE INDEX distribution_current_installation ON distribution_bindings(installation_id) WHERE state <> 'revoked';
CREATE UNIQUE INDEX distribution_current_account ON distribution_bindings(account_id) WHERE state <> 'revoked';
CREATE TABLE distribution_binding_versions (
 company_id uuid NOT NULL, binding_id uuid NOT NULL, revision bigint NOT NULL,
 installation_id uuid NOT NULL, integration_id uuid NOT NULL, account_id text NOT NULL, state text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(company_id,binding_id,revision),
 FOREIGN KEY(company_id,binding_id) REFERENCES distribution_bindings(company_id,id)
);
CREATE TABLE distribution_employee_mappings (
 id uuid PRIMARY KEY, company_id uuid NOT NULL, binding_id uuid NOT NULL,
 user_id uuid, user_id_snapshot uuid NOT NULL, crm_user_id text NOT NULL CHECK(crm_user_id ~ '^[1-9][0-9]{0,18}$'),
 state text NOT NULL CHECK(state IN ('verified','unavailable','ambiguous')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 9007199254740991), verified_at timestamptz NOT NULL,
 FOREIGN KEY(company_id,binding_id) REFERENCES distribution_bindings(company_id,id),
 FOREIGN KEY(company_id,user_id) REFERENCES users(company_id,id) ON DELETE SET NULL (user_id),
 CHECK(state <> 'verified' OR user_id IS NOT NULL),
 CHECK(crm_user_id::numeric <= 9223372036854775807),
 UNIQUE(company_id,binding_id,user_id), UNIQUE(company_id,binding_id,crm_user_id)
);
-- Immutable history has no user FK: deleted employee identity is preserved.
CREATE TABLE distribution_mapping_versions (
 company_id uuid NOT NULL, mapping_id uuid NOT NULL, revision bigint NOT NULL,
 binding_id uuid NOT NULL, user_id uuid, user_id_snapshot uuid NOT NULL, crm_user_id text NOT NULL, state text NOT NULL,
 verified_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,mapping_id,revision),
 FOREIGN KEY(company_id,binding_id) REFERENCES distribution_bindings(company_id,id)
);
CREATE FUNCTION distribution_mapping_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' THEN
   INSERT INTO distribution_mapping_versions(company_id,mapping_id,revision,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at)
   VALUES(OLD.company_id,OLD.id,OLD.revision+1,OLD.binding_id,OLD.user_id,OLD.user_id_snapshot,OLD.crm_user_id,'unavailable',OLD.verified_at);
   RETURN OLD;
 END IF;
 INSERT INTO distribution_mapping_versions(company_id,mapping_id,revision,binding_id,user_id,user_id_snapshot,crm_user_id,state,verified_at)
 VALUES(NEW.company_id,NEW.id,NEW.revision,NEW.binding_id,NEW.user_id,NEW.user_id_snapshot,NEW.crm_user_id,NEW.state,NEW.verified_at);
 RETURN NEW;
END $$;
CREATE FUNCTION distribution_mapping_tombstone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.user_id IS NULL AND OLD.user_id IS NOT NULL THEN NEW.state='unavailable'; NEW.revision=OLD.revision+1; END IF; RETURN NEW; END $$;
CREATE TRIGGER distribution_mapping_tombstone BEFORE UPDATE ON distribution_employee_mappings FOR EACH ROW EXECUTE FUNCTION distribution_mapping_tombstone();
CREATE TRIGGER distribution_mapping_audit AFTER INSERT OR UPDATE OR DELETE ON distribution_employee_mappings FOR EACH ROW EXECUTE FUNCTION distribution_mapping_history();
CREATE FUNCTION distribution_user_unavailable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status <> 'active' AND NEW.status IS DISTINCT FROM OLD.status THEN
 UPDATE distribution_employee_mappings SET state='unavailable',revision=revision+1 WHERE company_id=NEW.company_id AND user_id=NEW.id AND state <> 'unavailable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER distribution_user_status AFTER UPDATE OF status ON users FOR EACH ROW EXECUTE FUNCTION distribution_user_unavailable();
CREATE TABLE distribution_service_nonces(key_id text NOT NULL,nonce uuid NOT NULL,expires_at timestamptz NOT NULL,PRIMARY KEY(key_id,nonce));
-- Provisioned by an operator. Possessing a signing key alone grants no tenant.
CREATE TABLE distribution_service_grants (
 key_id text NOT NULL, company_id uuid NOT NULL REFERENCES companies(id), installation_id uuid NOT NULL,
 capability text NOT NULL CHECK(capability='widget-access'), active boolean NOT NULL DEFAULT true,
 PRIMARY KEY(key_id,company_id,installation_id,capability)
);

CREATE TABLE distribution_mapping_snapshots (
 company_id uuid NOT NULL, binding_id uuid NOT NULL, revision bigint NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='array'), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(company_id,binding_id,revision), FOREIGN KEY(company_id,binding_id) REFERENCES distribution_bindings(company_id,id)
);
