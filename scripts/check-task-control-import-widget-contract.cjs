// Cross-repository import contract: no CRM writes and no network access.
const assert = require('node:assert/strict');
const path = require('node:path');
const widgetDir = process.env.WIDGET_DIR || path.resolve(__dirname, '../../rakurs/rakurs-widgets-new/rkrs_lead_notasks');
const payload = require('../contracts/fixtures/task-control-import-source-result.json');
const widget = require(path.join(widgetDir, 'tests/fixtures/r04-source-result.json'));
const Constants = require(path.join(widgetDir, 'lib/constants.js'));
const Rules = require(path.join(widgetDir, 'lib/rules.js'));
const Snapshot = require(path.join(widgetDir, 'lib/snapshot.js'));
const Migration = require(path.join(widgetDir, 'lib/migration.js'))(Constants, Rules, Snapshot);
assert.deepEqual(payload.payload, widget.payload, 'widget and server original source fixtures differ');
assert.equal(payload.sourceChecksum, widget.sourceChecksum);
const source = payload.payload.raw;
const fields = source.fields;
const storage = {catalog_id:Number(payload.payload.catalogId),fields:payload.payload.fieldIds,enums:{}};
for (const definition of Constants.FIELD_DEFINITIONS) {
  if (definition.type !== 'select') continue;
  const field = fields.find(f => f.code === definition.code);
  storage.enums[definition.key] = {};
  for (const value of field.enums) storage.enums[definition.key][String(value.value)==='1'?'true':'false'] = Number(value.id);
}
const rules = source.elements.filter(e => !e.is_deleted).map(e => Rules.fromCatalogElement(e, storage));
Migration.validateRules(rules);
assert.deepEqual(rules.map(Migration.canonical), payload.payload.rules);
assert.equal(Snapshot.hashRules(rules), payload.sourceChecksum);
assert.equal(rules[0].enabled,false);
assert.equal(rules[0].archived,true);
assert(rules[0].pipelines[0].status_ids.includes(142));
assert(rules[0].pipelines[0].status_ids.includes(143));
console.log('Task Control import: raw catalog → actual widget mapper → company DTO/hash preserve UID, flags and closed stages');
