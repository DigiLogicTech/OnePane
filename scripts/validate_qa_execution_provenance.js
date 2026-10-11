#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const fs=require("node:fs"),path=require("node:path");
const {project,format}=require("../internal/webui/static/qa-execution-provenance.js");
const t="task-"+"a".repeat(24),other="task-"+"b".repeat(24),run="run-"+"c".repeat(24);
const trace="trace-"+"d".repeat(24),canary="SUPER_SECRET_OPERATOR_PASSWORD_CANARY";
const tool={
 task_ref:t,run_ref:run,source:"tool",target_ref:"tool-"+"e".repeat(24),
 observation_ref:"observation-"+"f".repeat(24),journal_status:"succeeded",
 target_status:"succeeded",link_kind:"persisted_same_task_attempt_reference",
 credential:canary,path:canary,output:canary
};
const operation={
 task_ref:t,run_ref:run,source:"operation",target_ref:"operation-"+"a".repeat(24),
 journal_status:"unknown",target_status:"unknown_outcome",
 link_kind:"persisted_same_task_attempt_reference",resource_ref:canary
};
const assurance={
 task_ref:t,verification_ref:"verification-"+"b".repeat(24),worker_run_ref:run,
 operation_ref:"operation-"+"a".repeat(24),verification_status:"pass",
 assurance_status:"passed",required_level:"V1",achieved_level:"V2",
 evidence_digest_recorded:true,distinct_verifier_recorded:true,
 recorded_assurance_pass:true,operation_state:"committed",
 verified_by:canary,evidence_hash:canary
};
const snapshot={
 schema_version:2,scope:"canonical_project_workspace",
 tasks:[{task_ref:t,id:canary,objective:canary}],
 timeline:[
  {event_type:"task.created",task_ref:t,trace_ref:trace,occurred_at_ms:1000},
  {event_type:"agent_worker.step",task_ref:t,trace_ref:trace,occurred_at_ms:2000}
 ],
 worker_tool_links:[tool,operation,{...tool,task_ref:other},{...tool,link_kind:canary}],
 assurance_evidence:[assurance,{...assurance,task_ref:other},
  {...assurance,worker_run_ref:"run-invalid",recorded_assurance_pass:true}],
 worker_tool_links_truncated:true,assurance_evidence_truncated:false
};
const capture={schema_version:1,source:"operator_tagged_local_http",
 observations:[
  {trace_ref:trace,subsystem:"tasks",method:"POST",status_code:201,at_utc:"2026-10-10T00:00:00Z"},
  {trace_ref:trace,subsystem:"tasks",method:"POST",status_code:201,private_payload:canary},
  {trace_ref:"trace-"+"f".repeat(24),subsystem:"tasks",method:"POST",status_code:201},
  {trace_ref:canary,subsystem:"tasks",method:"POST",status_code:201}
 ]};
const extra={
 async_lineage:[
  {task_ref:t,attempt_ref:"attempt-"+"f".repeat(24),worker_run_ref:run,
   attempt_state:"running",worker_state:"running",steps_recorded:4,
   link_kind:"persisted_task_attempt_worker_foreign_keys",raw_task:canary},
  {task_ref:other,attempt_ref:"attempt-"+"e".repeat(24),worker_run_ref:run,
   attempt_state:"running",worker_state:"running",steps_recorded:1,
   link_kind:"persisted_task_attempt_worker_foreign_keys"}
 ],
 probe_witnesses:[
  {task_ref:t,verification_ref:assurance.verification_ref,
   observation_ref:"observation-"+"a".repeat(24),
   role:"integration",source_trust:"unverified_derived",
   observed_after_attempt:true,observed_after_operation_start:true,
   integrity_rechecked:true,independent_source:true,
   recorded_assurance_pass:true,corroborating_integration_probe:true,
   evidence_class:"recorded_probe_observation_not_external_effect_attestation",
   raw_probe:canary},
  {task_ref:t,verification_ref:assurance.verification_ref,
   observation_ref:"observation-"+"b".repeat(24),
   role:"integration",source_trust:"unverified_derived",
   observed_after_attempt:false,observed_after_operation_start:true,
   integrity_rechecked:true,independent_source:true,
   recorded_assurance_pass:true,corroborating_integration_probe:true,
   evidence_class:"recorded_probe_observation_not_external_effect_attestation"},
  {task_ref:other,verification_ref:assurance.verification_ref,
   observation_ref:"observation-"+"c".repeat(24),role:"integration",
   integrity_rechecked:true,independent_source:true,
   recorded_assurance_pass:true,corroborating_integration_probe:true,
   evidence_class:"recorded_probe_observation_not_external_effect_attestation"}
 ],
 async_lineage_truncated:true,probe_witnesses_truncated:false
};
Object.assign(snapshot,extra);
const safe=project(snapshot,capture);
assert.equal(safe.lineage.length,1);
assert.equal(safe.lineage[0].worker_run_ref,run);
assert.equal(safe.probes.length,2);
assert.equal(safe.probes[0].corroborating_integration_probe,true);
assert.equal(safe.probes[1].corroborating_integration_probe,false,"stale observation must not corroborate");
assert.equal(safe.lineage_truncated,true);
assert.ok(format(safe).includes("Asynchronous persisted Task"));
assert.ok(format(safe).includes("not a new external probe"));
assert.equal(safe.created.length,1);
assert.equal(safe.created[0].task_ref,t);
assert.equal(safe.created[0].trace_ref,trace);
assert.equal(safe.worker.length,2);
assert.equal(safe.worker[0].source,"tool");
assert.equal(safe.worker[1].target_status,"unknown_outcome");
assert.equal(safe.assurance.length,2);
assert.equal(safe.assurance[0].recorded_assurance_pass,true);
assert.equal(safe.assurance[1].recorded_assurance_pass,false);
assert.equal(safe.worker_truncated,true);
assert.equal(project(snapshot).created.length,0);
assert.equal(project(snapshot).tagged_capture_reviewed,false);
for(const output of [JSON.stringify(safe),format(safe)]){
 assert.ok(!output.includes(canary),"raw source data escaped");
 assert.ok(!output.includes('"id"'),"raw ID escaped");
 assert.ok(!output.includes('"objective"'),"Task text escaped");
}
assert.ok(format(safe).includes("Trusted HTTP → Task creation matches: 1"));
assert.ok(format(safe).includes("recorded-assurance-pass=true"));
assert.ok(format(safe).includes("NOT proof of correct model answers"));
assert.throws(()=>project({...snapshot,async_lineage:Array(25).fill(extra.async_lineage[0])}));
assert.throws(()=>project({...snapshot,probe_witnesses:Array(25).fill(extra.probe_witnesses[0])}));
assert.equal(project({...snapshot,probe_witnesses:[
 {...extra.probe_witnesses[0],integrity_rechecked:false}]}).probes[0].corroborating_integration_probe,false);
assert.throws(()=>project({...snapshot,scope:"other"}));
assert.throws(()=>project({...snapshot,worker_tool_links:Array(25).fill(tool)}));
assert.throws(()=>project({...snapshot,assurance_evidence:Array(25).fill(assurance)}));
assert.throws(()=>project(snapshot,{...capture,source:"forged"}));
assert.throws(()=>project(snapshot,{...capture,observations:Array(65).fill(capture.observations[0])}));
const ambiguity=project({...snapshot,
 timeline:[...snapshot.timeline,{event_type:"task.created",task_ref:t,trace_ref:trace,
 occurred_at_ms:1234}]},capture);
assert.equal(ambiguity.created.length,0,"duplicate Task creation trace is ambiguous");
const inconsistent=project({...snapshot,
 assurance_evidence:[{...assurance,achieved_level:"V0",recorded_assurance_pass:true}]});
assert.equal(inconsistent.assurance[0].recorded_assurance_pass,false,"cannot claim pass below required level");
const read=p=>fs.readFileSync(path.join(__dirname,"..",p),"utf8");
const ui=read("internal/webui/static/workspace-ai-workflow.js");
const index=read("internal/webui/static/index.html");
const backend=read("internal/api/qa_api_capture.go");
const server=read("internal/api/server.go");
const workflow=read(".github/workflows/project-workspace-development-checks.yml");
for(const x of ['id="a63IncludeCapture"','a63ExecutionProvenance.project(snapshot,capture)',
 '"/v1/qa/api-capture",{cache:"no-store"}','a63ExecutionProvenance.format(executionReport)'])
 assert.ok(ui.includes(x),"UI permission control missing: "+x);
assert.ok(index.includes('/qa-execution-provenance.js'));
assert.ok(index.indexOf('/qa-execution-provenance.js')<index.indexOf('/workspace-ai-workflow.js'));
assert.ok(backend.includes('r.WithContext(context.WithValue('));
assert.ok(server.includes('traceID := qaTrustedTaskTrace(r)'));
assert.ok(workflow.includes("node scripts/validate_qa_execution_provenance.js"));
console.log("PASS: independently authorized minted Task trace, Task/Attempt-linked tool and operation records, conservative assurance projection, caps and injection privacy");
