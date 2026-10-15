// Cross-repository compatibility check; does not modify widget code or settings.
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const widgetDir = process.env.WIDGET_DIR || path.resolve(
  __dirname, '../../rakurs/rakurs-widgets-new/rkrs_lead_notasks',
);
const snapshot = require(path.join(widgetDir, 'lib/snapshot.js'));
const rules = require(path.join(widgetDir, 'lib/rules.js'));
const fixture = JSON.parse(fs.readFileSync(
  path.join(__dirname, '../contracts/fixtures/task-control-v1.json'), 'utf8',
));
function crmId(value) {
  const number = Number(value);
  assert(Number.isSafeInteger(number) && number > 0 && String(number) === value);
  return number;
}
const mapped = fixture.rules.map((rule) => ({
  uid: rule.ruleUid,
  name: rule.name,
  users: rule.users.map(crmId),
  pipelines: rule.pipelines.map((pipeline) => ({
    pipeline_id: crmId(pipeline.pipelineId),
    status_ids: pipeline.statusIds.map(crmId),
  })),
  customer_statuses: rule.customerStatuses.map(crmId),
  show_modal: rule.showModal,
  deny_exit: rule.denyExit,
  check_responsible: rule.checkResponsible,
  customers: rule.customers,
  enabled: rule.enabled,
  archived: rule.archived,
}));
assert.deepEqual(mapped, fixture.legacyRules);
for (const rule of mapped) assert.equal(rules.validateRule(rule).valid, true);
assert.equal(snapshot.canonicalRulesJson(mapped), fixture.canonicalRulesJson);
assert.equal(snapshot.hashRules(mapped), fixture.checksum);
assert.throws(() => crmId('9007199254740993'));
const engine = new rules.RuleEngine(mapped, 7);
assert.equal(engine.myRules().length, 1, 'archived rule must remain inactive');
assert.equal(engine.match({ entityType: 'customers', statusId: 999, responsibleUserId: 2 }).denyExit, true);
console.log('Task Control v1: DTO → legacy semantics and checksum match');
