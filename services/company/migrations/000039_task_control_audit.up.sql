CREATE TABLE task_control_audit_events (
 sequence bigserial PRIMARY KEY, id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 company_id uuid NOT NULL, binding_id uuid NOT NULL, event_type text NOT NULL,
 scope jsonb NOT NULL, actor_id uuid, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), details jsonb NOT NULL,
 FOREIGN KEY(company_id,binding_id) REFERENCES task_control_connections(company_id,id)
);
CREATE INDEX task_control_audit_binding_idx ON task_control_audit_events(company_id,binding_id,sequence DESC);
CREATE INDEX task_control_publications_schema_page_idx ON task_control_publications(company_id,binding_id,(payload->>'schemaVersion'),version DESC);
-- Entries contain only bounded metadata, never assertions, tokens, original CRM captures or runtime payloads.
CREATE FUNCTION task_control_audit_rule_diff(old_rules jsonb,new_rules jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
 WITH old_u AS (SELECT coalesce(r->>'ruleUid',r->'rule'->>'ruleUid') uid,coalesce(r->'rule',r) r FROM jsonb_array_elements(coalesce(old_rules,'[]')) r), new_u AS (SELECT coalesce(r->>'ruleUid',r->'rule'->>'ruleUid') uid,coalesce(r->'rule',r) r FROM jsonb_array_elements(coalesce(new_rules,'[]')) r), changes AS (SELECT coalesce(o.uid,n.uid) uid,CASE WHEN o.uid IS NULL THEN 'added' WHEN n.uid IS NULL THEN 'removed' ELSE 'changed' END change FROM old_u o FULL JOIN new_u n USING(uid) WHERE o.r IS DISTINCT FROM n.r)
 SELECT jsonb_build_object('changedRuleCount',count(*),'ruleChanges',coalesce((SELECT jsonb_agg(to_jsonb(b)) FROM (SELECT * FROM changes ORDER BY uid LIMIT 100) b),'[]'::jsonb),'diffTruncated',count(*)>100) FROM changes;
$$;
CREATE FUNCTION task_control_audit_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c task_control_connections; kind text; actor uuid; data jsonb; old_rules jsonb; new_rules jsonb;
BEGIN
 actor := nullif(current_setting('task_control.actor_id',true),'')::uuid;
 IF TG_TABLE_NAME='task_control_connections' THEN
  c := NEW;
  IF TG_OP='INSERT' THEN kind:='connection_linked'; data:=jsonb_build_object('state',NEW.state,'mode',NEW.mode,'runtimeSchemaVersion',NEW.runtime_schema_version); actor:=NEW.linked_by;
  ELSE
   IF (OLD.state,OLD.mode,OLD.runtime_schema_version) IS NOT DISTINCT FROM (NEW.state,NEW.mode,NEW.runtime_schema_version) THEN RETURN NEW; END IF;
   kind:='connection_updated';
   IF OLD.mode IS DISTINCT FROM NEW.mode THEN kind:='mode_changed'; END IF;
   data:=jsonb_build_object('before',jsonb_build_object('state',OLD.state,'mode',OLD.mode,'runtimeSchemaVersion',OLD.runtime_schema_version),'after',jsonb_build_object('state',NEW.state,'mode',NEW.mode,'runtimeSchemaVersion',NEW.runtime_schema_version));
  END IF;
 ELSE
  SELECT * INTO c FROM task_control_connections WHERE company_id=NEW.company_id AND id=NEW.binding_id;
  IF TG_TABLE_NAME='task_control_drafts' THEN
   kind:='draft_updated'; actor:=NEW.updated_by;
   data:=jsonb_build_object('previousRevision',OLD.revision,'revision',NEW.revision,'previousChecksum',OLD.checksum,'checksum',NEW.checksum,'schemaVersion',NEW.schema_version);
   old_rules:=OLD.rules; new_rules:=NEW.rules;
   -- UID sets are bounded independently of rule payload size.
   data:=data || task_control_audit_rule_diff(old_rules,new_rules);
  ELSIF TG_TABLE_NAME='task_control_publications' THEN
   actor:=(NEW.payload->>'createdBy')::uuid; kind:='publication_published';
   IF NEW.payload->>'restoredFromId' IS NOT NULL THEN kind:='publication_restored'; END IF;
   data:=jsonb_build_object('publicationId',NEW.id,'previousPublicationId',c.publication_id,'version',NEW.version,'schemaVersion',NEW.payload->'schemaVersion','draftRevision',NEW.payload->'draftRevision','checksum',coalesce(NEW.payload->'intentChecksum',NEW.payload->'checksum'),'reason',NEW.payload->'reason','restoredFromId',NEW.payload->'restoredFromId');
   SELECT coalesce(payload->'intentRules',payload->'rules') INTO old_rules FROM task_control_publications WHERE company_id=c.company_id AND id=c.publication_id;
   data:=data || task_control_audit_rule_diff(old_rules,coalesce(NEW.payload->'intentRules',NEW.payload->'rules'));
  ELSIF TG_TABLE_NAME='task_control_import_objects' THEN
   IF NEW.kind<>'apply' THEN RETURN NEW; END IF;
   kind:='import_applied'; actor:=(NEW.payload->>'actorId')::uuid;
   data:=jsonb_build_object('requestId',NEW.id,'backupRevision',NEW.payload->'backup'->'Revision','revision',NEW.payload->'response'->'revision');
  ELSE
   -- Initial materialization belongs to the publication event. Recompute never changes intent.
   IF NOT EXISTS(SELECT 1 FROM task_control_publications WHERE company_id=NEW.company_id AND id=NEW.publication_id) THEN RETURN NEW; END IF;
   kind:='runtime_recomputed'; actor:=NULL;
   data:=jsonb_build_object('publicationId',NEW.publication_id,'snapshotId',NEW.id,'runtimeRevision',NEW.runtime_revision,'intentChecksum',NEW.payload->'intentChecksum','resolvedChecksum',NEW.payload->'resolvedChecksum','sourceRevisions',NEW.payload->'sourceRevisions');
  END IF;
 END IF;
 data := data || jsonb_build_object('reason',coalesce(nullif(data->>'reason',''),kind));
 INSERT INTO task_control_audit_events(company_id,binding_id,event_type,scope,actor_id,details)
 VALUES(c.company_id,c.id,kind,jsonb_build_object('product','task-control','companyId',c.company_id,'bindingId',c.id,'installationId',c.installation_id,'integrationId',c.integration_id,'accountId',c.account_id,'generation',c.generation,'bindingRevision',c.binding_revision,'modeEpoch',c.mode_epoch),actor,data);
 RETURN NEW;
END $$;
CREATE TRIGGER task_control_audit_connection AFTER INSERT OR UPDATE ON task_control_connections FOR EACH ROW EXECUTE FUNCTION task_control_audit_change();
CREATE TRIGGER task_control_audit_draft AFTER UPDATE ON task_control_drafts FOR EACH ROW EXECUTE FUNCTION task_control_audit_change();
CREATE TRIGGER task_control_audit_publication AFTER INSERT ON task_control_publications FOR EACH ROW EXECUTE FUNCTION task_control_audit_change();
CREATE TRIGGER task_control_audit_import AFTER INSERT ON task_control_import_objects FOR EACH ROW EXECUTE FUNCTION task_control_audit_change();
CREATE TRIGGER task_control_audit_runtime AFTER INSERT ON task_control_v2_snapshots FOR EACH ROW EXECUTE FUNCTION task_control_audit_change();
CREATE FUNCTION task_control_audit_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'Task Control audit is append-only'; END $$;
CREATE TRIGGER task_control_audit_immutable BEFORE UPDATE OR DELETE ON task_control_audit_events FOR EACH ROW EXECUTE FUNCTION task_control_audit_immutable();

INSERT INTO task_control_audit_events(company_id,binding_id,event_type,scope,actor_id,created_at,details)
SELECT p.company_id,p.binding_id,CASE WHEN p.payload->>'restoredFromId' IS NULL THEN 'publication_published' ELSE 'publication_restored' END,
coalesce(p.payload->'scopeAtPublication',p.payload->'scope'),(p.payload->>'createdBy')::uuid,(p.payload->>'createdAt')::timestamptz,
jsonb_build_object('publicationId',p.id,'version',p.version,'schemaVersion',p.payload->'schemaVersion','checksum',coalesce(p.payload->'intentChecksum',p.payload->'checksum'),'reason',p.payload->'reason','restoredFromId',p.payload->'restoredFromId','backfilled',true)
FROM task_control_publications p ORDER BY p.company_id,p.binding_id,p.version;
