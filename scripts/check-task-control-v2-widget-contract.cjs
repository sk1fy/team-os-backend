// Feed a fixture encoded by company into the actual widget protocol validator.
const assert = require('node:assert/strict');
const path = require('node:path');
const fixture = require('../contracts/fixtures/task-control-runtime-v2.json');
const widgetDir = process.env.WIDGET_DIR || path.resolve(__dirname, '../../rakurs/rakurs-widgets-new/rkrs_lead_notasks');
const Snapshot = require(path.join(widgetDir, 'lib/snapshot.js'));
const Rules = require(path.join(widgetDir, 'lib/rules.js'));
const TeamOS = require(path.join(widgetDir, 'lib/teamos.js'))(Snapshot, Rules);
(async () => {
  const telemetry=[]; let sequence=0;
  const now = Date.parse(fixture.issuedAt);
  const client = new TeamOS.Client({accountId:fixture.scope.accountId, settings:()=>({}), coreOrigin:'https://core.example.invalid', backendOrigin:'https://backend.example.invalid', now:()=>now, clock:()=>0, uuid:()=> 'a0000000-0000-4000-8000-'+String(++sequence).padStart(12,'0'), request:async(url,options)=>{
    if(url.endsWith('/sessions')) return {scope:fixture.scope,actorId:fixture.actorId,accessToken:'x'.repeat(43),expiresAt:fixture.validUntil,runtimeSchemaVersion:2};
    if(url.endsWith('/bootstrap')) return fixture;
    if(url.endsWith('/telemetry')) {telemetry.push(options.body);return null;}
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
  assert.deepEqual(telemetry.map(event=>event.event),['received','applied']);
  assert.notEqual(telemetry[0].eventId,telemetry[1].eventId);
  const acknowledgement=telemetry[1];
  assert.equal(acknowledgement.snapshotId,fixture.snapshotId);
  assert.equal(acknowledgement.payloadChecksum,fixture.payloadChecksum);
  assert.equal(acknowledgement.runtimeRevision,fixture.runtimeRevision);
  const output=path.resolve(__dirname,'../contracts/fixtures/task-control-telemetry.json');
  if(process.argv.includes('--write-fixture')) require('node:fs').writeFileSync(output,JSON.stringify(telemetry,null,2)+'\n');
  else {
    // The fixture describes the protocol, while runtimeVersion identifies the
    // tested package. Keep every other wire field under the exact comparison.
    const version = require(path.join(widgetDir, 'package.json')).version;
    const expected = require(output).map(event => ({ ...event, runtimeVersion: version }));
    assert.deepEqual(telemetry,expected,'actual widget telemetry differs from company wire fixture');
  }
  console.log('Task Control v2: company JSON → widget validation → engine → distinct received/applied telemetry match');
})().catch(error=>{console.error(error);process.exitCode=1;});
