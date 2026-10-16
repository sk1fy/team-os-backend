// Feed a fixture encoded by company into the actual widget protocol validator.
const assert = require('node:assert/strict');
const path = require('node:path');
const fixture = require('../contracts/fixtures/task-control-runtime-v2.json');
const widgetDir = process.env.WIDGET_DIR || path.resolve(__dirname, '../../rakurs/rakurs-widgets-new/rkrs_lead_notasks');
const Snapshot = require(path.join(widgetDir, 'lib/snapshot.js'));
const Rules = require(path.join(widgetDir, 'lib/rules.js'));
const TeamOS = require(path.join(widgetDir, 'lib/teamos.js'))(Snapshot, Rules);
(async () => {
  let acknowledgement;
  const now = Date.parse(fixture.issuedAt);
  const client = new TeamOS.Client({accountId:fixture.scope.accountId, settings:()=>({}), coreOrigin:'https://core.example.invalid', backendOrigin:'https://backend.example.invalid', now:()=>now, clock:()=>0, uuid:()=> 'a0000000-0000-4000-8000-000000000002', request:async(url,options)=>{
    if(url.endsWith('/sessions')) return {scope:fixture.scope,actorId:fixture.actorId,accessToken:'x'.repeat(43),expiresAt:fixture.validUntil,runtimeSchemaVersion:2};
    if(url.endsWith('/bootstrap')) return fixture;
    if(url.endsWith('/ack')) {acknowledgement=options.body;return null;}
    throw new Error(`unexpected request ${url}`);
  }});
  client.proof=async()=>({assertion:'synthetic'});
  const good=await client.refresh();
  assert(good, 'production envelope rejected by the widget');
  assert.equal(good.schema,2);
  assert.deepEqual(good.config.rules,fixture.runtimeRules);
  assert.equal(good.config.published.hash,fixture.payloadChecksum);
  const engine=new Rules.RuleEngine(good.config.rules,Number(fixture.actorId));
  assert(engine.myRules().length>0);
  await client.acknowledge(good);
  assert.equal(acknowledgement.snapshotId,fixture.snapshotId);
  assert.equal(acknowledgement.payloadChecksum,fixture.payloadChecksum);
  assert.equal(acknowledgement.runtimeRevision,fixture.runtimeRevision);
  console.log('Task Control v2: company JSON → widget validation → engine → ACK match');
})().catch(error=>{console.error(error);process.exitCode=1;});
