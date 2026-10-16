// Called by the opt-in PostgreSQL integration test. Only loopback HTTP is allowed.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { randomUUID } = require('node:crypto');
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const origin = new URL(input.origin);
assert.equal(origin.hostname, '127.0.0.1');
assert.equal(origin.protocol, 'http:');
const widget = process.env.WIDGET_DIR;
assert(widget, 'WIDGET_DIR is required');
const Snapshot = require(path.join(widget, 'lib/snapshot.js'));
const Rules = require(path.join(widget, 'lib/rules.js'));
const TeamOS = require(path.join(widget, 'lib/teamos.js'))(Snapshot, Rules);
(async () => {
  const receipts = [];
  const client = new TeamOS.Client({
    accountId: '123', settings: () => ({}), coreOrigin: 'https://core.example.invalid', backendOrigin: 'https://backend.example.invalid',
    now: () => Date.parse(input.now), clock: () => 0, uuid: randomUUID,
    request: async (url, options = {}) => {
      assert.equal(new URL(url).origin, 'https://backend.example.invalid');
      const response = await fetch(new URL(new URL(url).pathname, origin), {
        method: options.method || 'GET',
        headers: { 'Content-Type': 'application/json', ...options.headers },
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        redirect: 'error', signal: AbortSignal.timeout(10000),
      });
      if (!response.ok) {
        const error = new Error(`loopback API returned ${response.status}`);
        error.status = response.status;
        throw error;
      }
      if (url.endsWith('/telemetry')) receipts.push(options.body.event);
      return response.status === 204 ? null : response.json();
    },
  });
  // Only the external amoCRM/Core proof is synthetic. Session exchange, bootstrap,
  // publication checksum and received/applied receipts use the real company service.
  client.proof = async () => ({ assertion: input.assertion });
  const config = await client.refresh();
  assert(config, 'actual server envelope must be accepted');
  assert.equal(config.schema, 2);
  const engine = new Rules.RuleEngine(config.config.rules, 7);
  assert.equal(engine.myRules().length, input.expectedRules);
  for (const rule of engine.myRules()) assert.deepEqual(rule.users, [7]);
  await client.acknowledge(config);
  assert.deepEqual(receipts, ['received', 'applied']);
  assert.equal(config.scope.accountId, '123');
  console.log(JSON.stringify({ schema: config.schema, rules: engine.myRules().length, receipts }));
})().catch(error => { console.error(error.message); process.exitCode = 1; });
