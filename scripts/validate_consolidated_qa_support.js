#!/usr/bin/env node
"use strict";
// Adversarial tests for the *real browser module* used in RC11.
// No server or network is needed; all service evidence is synthetic.
const assert=require("node:assert/strict");
const support=require("../internal/webui/static/qa-consolidated-support.js");
const {createHash}=require("node:crypto");
const decoder=new TextDecoder();
const secret="USER_SUPPLIED_BEARER_COOKIE_PROMPT_PRIVATE_QA";
const hash=(kind)=>kind+"-"+"a".repeat(24);
const good={
 workspace:{
  schema_version:2,scope:"canonical_project_workspace",
  build_version:secret,build_revision:secret,
  captured_tasks:2,max_tasks:50,truncated:false,
  captured_timeline_events:3,max_timeline_events:96,timeline_truncated:false,
  tasks:[{id:secret,task_ref:secret,state:"failed",objective:secret,execution:{
   status:"failed",model_response_json:secret}},{state:"complete",path:secret}],
  timeline:[{event_type:"task.failed",event_ref:secret,trace_ref:secret,payload_json:secret},
   {event_type:"task.started",error:secret}],
  private_script:secret,source_code:secret
 },
 browser:{
  schema_version:1,source:"onepane_browser_opt_in",status:"stopped",
  started_utc:"2026-10-10T00:00:00.000Z",ended_utc:"2026-10-10T00:00:01Z",
  dropped_events:0,events:[{kind:"api",action:"response",at_utc:"2026-10-10T00:00:01Z",
   subsystem:"models",method:"POST",status:502,duration_ms:17,
   request_body:secret,raw_url:"https://"+secret,headers:secret},
   {kind:"ui",action:"button",route:"models",value:secret,innerHTML:secret},
   {kind:"browser",action:"uncaught_error",exception:secret,stack:secret},
   {kind:"capture",action:"stopped",outcome:"stopped",message:secret}]
 },
 model:{
  schema_version:2,scope:"authorised_managed_model_deployment",
  deployment_ref:hash("deployment"),deployment_status:"ready",observed_residency_state:"stopped",
  deployment_updated_at_ms:1700000000000,captured_sessions:1,sessions_truncated:false,
  sessions:[{session_ref:hash("testbed"),status:"cancelled",started_at_ms:1700000000000,
   recorded_successful_turns:2,recorded_tool_probe_turns:1,
   failure_details:"structured_failure_category_only",last_failure_stage:"inference_dispatch",
   last_failure_category:"deadline_exceeded",failure_observed_at_ms:1700000000050,
   raw_request:secret,raw_response:secret,last_error:secret}],
  backend_stdout:secret,placement_json:secret
 },
 node:{
  schema_version:3,scope:"admin_authorised_local_control_plane",
  node_ref:hash("node"),registered_local_node:true,recorded_trust_state:"local",
  last_seen_at_ms:1700000000000,node_record_updated_at_ms:1700000000000,
  manifest_state:"not_recorded",pairing_status:"not_recorded",
  wake_attempts:{total_recorded:2,ready:1,failed:1,timed_out:0,in_progress:0,last_recorded_state:"ready"},
  inference_receipts:{total_recorded:1,succeeded:1,failed:0,unknown:0,executing:0},
  operating_system_service_state:"running",
  local_service_observation:{manager:"systemd",collection:"observed",state:"running",observed_at_ms:1700000000000},
  backend_readiness:{scope:"canonical_local_backend",
   observed_at_ms:1700000000100,database_response:"responding_read_only",
   schema_record_status:"recorded_versions_match_embedded",
   recorded_schema_version:42,embedded_schema_version:42,
   local_node_registration:"registered",
   task_service_wiring:"configured_not_probed",model_service_wiring:"configured_not_probed",
   vault_service_wiring:"configured_not_probed",federation_service_wiring:"not_configured",
   sensitive_sqlite_path:secret,raw_error:secret,process_env:secret},

  endpoint:secret,manifest_json:secret,peer_certificate_pem:secret,config_file:secret
 }
};
function readZip(b){
 const raw=Buffer.from(b);
 assert.equal(raw.readUInt32LE(raw.length-22),0x06054b50,"valid End-of-Central-Directory");
 const entries={};let pos=0;
 while(pos+4<raw.length&&raw.readUInt32LE(pos)===0x04034b50){
  const method=raw.readUInt16LE(pos+8);
  assert.equal(method,0,"STORE-only ZIP, no compressor dependency");
  const expectedCRC=raw.readUInt32LE(pos+14);
  const size=raw.readUInt32LE(pos+18),nameSize=raw.readUInt16LE(pos+26),
   extra=raw.readUInt16LE(pos+28),start=pos+30+nameSize+extra;
  const name=raw.subarray(pos+30,pos+30+nameSize).toString("utf8");
  assert.ok(["manifest.json","workspace.json","browser.json","agent-check.json","node.json"].includes(name),
   "file names are fixed, never user-controlled");
  assert.ok(!Object.hasOwn(entries,name),"no duplicate ZIP entries");
  const value=raw.subarray(start,start+size);
  assert.equal(value.length,size,"untruncated ZIP member");
  let crc=0xffffffff;
  for(const byte of value){
   crc^=byte;for(let bit=0;bit<8;bit++)crc=crc&1?(crc>>>1)^0xedb88320:crc>>>1;
  }
  assert.equal((crc^0xffffffff)>>>0,expectedCRC,"CRC32 is valid");
  entries[name]=JSON.parse(value.toString("utf8"));
  pos=start+size;
 }
 assert.equal(raw.readUInt32LE(pos),0x02014b50,"central directory follows all entries");
 assert.equal(raw.readUInt16LE(raw.length-12),Object.keys(entries).length,
  "central directory counts match");
 return entries;
}
// Verify local SHA-256 against standard published vectors and Node crypto,
// including a multi-block payload and non-ASCII UTF-8.
const utf8=new TextEncoder();
assert.equal(support.sha256Bytes(utf8.encode("")),
 "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
assert.equal(support.sha256Bytes(utf8.encode("abc")),
 "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
for(const value of ["OnePane — SANDBOX ✓","x".repeat(150000)]){
 const raw=utf8.encode(value);
 assert.equal(support.sha256Bytes(raw),createHash("sha256").update(raw).digest("hex"));
}
const prepared=support.prepare(good);
assert.ok(prepared.json.length>100);
assert.equal(prepared.files["workspace.json"].captured_tasks,2);
assert.equal(prepared.files["workspace.json"].task_states.failed,1);
assert.equal(prepared.files["workspace.json"].attention_events["task.failed"],1);
assert.equal(prepared.files["browser.json"].events[0].status,502);
assert.equal(prepared.files["agent-check.json"].sessions[0].last_failure_stage,"inference_dispatch");
assert.equal(prepared.files["node.json"].local_service_observation.state,"running");
assert.equal(prepared.files["node.json"].backend_readiness.database_response,"responding_read_only");
assert.equal(prepared.files["node.json"].backend_readiness.local_node_registration,"registered");
assert.equal(prepared.files["node.json"].backend_readiness.recorded_schema_version,42);

assert.ok(!prepared.json.includes(secret),"raw user information must not appear in preview");
const bytes=support.zip(prepared);
assert.ok(bytes.byteLength<262144);
assert.ok(!decoder.decode(bytes).includes(secret),"raw secrets must not appear in archive");
assert.equal(prepared.files["manifest.json"].integrity_algorithm,"SHA-256");
assert.equal(prepared.files["manifest.json"].integrity_scope,"extracted_utf8_member_bytes");
assert.deepEqual(Object.keys(prepared.files["manifest.json"].member_sha256).sort(),
 ["agent-check.json","browser.json","node.json","workspace.json"]);
const entries=readZip(bytes);
assert.deepEqual(Object.keys(entries).sort(),[
 "agent-check.json","browser.json","manifest.json","node.json","workspace.json"].sort());
assert.equal(entries["node.json"].local_service_observation.state,"running");
assert.equal(entries["manifest.json"].included_sources.length,4);
for(const [name,expected] of Object.entries(entries["manifest.json"].member_sha256)){
 const encoded=utf8.encode(JSON.stringify(entries[name],null,2)+"\\n");
 assert.equal(createHash("sha256").update(encoded).digest("hex"),expected,
  "manifest digest must match the extracted JSON bytes for "+name);
}

assert.equal(entries["agent-check.json"].sessions[0].last_failure_category,"deadline_exceeded");
// Unsupported source data must never be echoed. Every route is fixed.
for(const [key,expected] of [
 ["workspace",{workspace:{...good.workspace,scope:secret}}],
 ["model",{workspace:good.workspace,model:{...good.model,schema_version:11}}],
 ["node",{workspace:good.workspace,node:{...good.node,scope:secret}}],
 ["browser",{workspace:good.workspace,browser:{...good.browser,status:"recording"}}],
]){
 assert.throws(()=>support.prepare(expected),/schema|capture/i,key+" should fail closed");
}
assert.throws(()=>support.prepare({workspace:{...good.workspace,build_version:secret,scope:"other"}}));
assert.throws(()=>support.prepare({workspace:good.workspace,browser:{...good.browser,events:Array(121).fill(good.browser.events[0])}}),/limit/);
assert.throws(()=>support.prepare({workspace:good.workspace,model:{...good.model,sessions:Array(11).fill(good.model.sessions[0])}}),/cap/);
const hostile={
 ...good,model:{...good.model,sessions:[{...good.model.sessions[0],
  status:secret,last_failure_stage:secret,last_failure_category:secret,session_ref:secret}]},
 node:{...good.node,recorded_trust_state:secret,pairing_status:secret,
  local_service_observation:{manager:secret,collection:secret,state:secret,executable:secret},
  backend_readiness:{scope:"canonical_local_backend",database_response:secret,
   schema_record_status:secret,recorded_schema_version:secret,
   local_node_registration:secret,task_service_wiring:secret,raw_credentials:secret}},
 browser:{...good.browser,events:[{kind:"api",action:secret,subsystem:secret,method:secret,
  route:secret,request_body:secret,duration_ms:999999999}]}
};
const attacker=support.prepare(hostile);
assert.ok(!attacker.json.includes(secret));
assert.equal(attacker.files["agent-check.json"].sessions[0].session_ref,"unavailable");
assert.equal(attacker.files["node.json"].operating_system_service_state,"not_collected");
assert.equal(attacker.files["node.json"].backend_readiness.database_response,"unavailable");
assert.equal(attacker.files["node.json"].backend_readiness.schema_record_status,"unavailable");
assert.equal(attacker.files["node.json"].backend_readiness.task_service_wiring,"unavailable");

assert.equal(attacker.files["browser.json"].events[0].duration_ms,0);
// The viewed JSON is a seal on ZIP contents; mutating a projected source
// after review must prevent export (no accidental unreviewed data).
const tamper=support.prepare(good);
tamper.files["node.json"].extra_private=secret;
assert.throws(()=>support.zip(tamper),/changed/);
// A caller cannot update a projected member and re-create the preview while
// leaving stale digests: ZIP generation checks every allowlisted member hash.
const changed=support.prepare(good);
changed.files["workspace.json"].captured_tasks=49;
changed.json=JSON.stringify({manifest:changed.files["manifest.json"],sources:changed.files},null,2);
assert.throws(()=>support.zip(changed),/integrity digest changed/);
const polluted=support.prepare(good);
polluted.files["manifest.json"].member_sha256["workspace.json"]="0".repeat(64);
polluted.json=JSON.stringify({manifest:polluted.files["manifest.json"],sources:polluted.files},null,2);
assert.throws(()=>support.zip(polluted),/integrity digest changed/);
const required=support.prepare({workspace:good.workspace});
assert.deepEqual(Object.keys(readZip(support.zip(required))).sort(),["manifest.json","workspace.json"]);
console.log("PASS: bounded reviewed QA ZIP, SHA-256 member integrity, CRC32, redaction and tamper prevention");
