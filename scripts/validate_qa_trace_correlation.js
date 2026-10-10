#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict"),fs=require("node:fs"),path=require("node:path");
const {correlate,format}=require("../internal/webui/static/qa-trace-correlation.js");
const t="task-"+"a".repeat(24),other="task-"+"b".repeat(24),trace="trace-"+"c".repeat(24);
const request="request-"+"d".repeat(24),run="run-"+"e".repeat(24);
const canary="PRIVATE_TOKEN_PROMPT_PASSWORD_12345";
const event=(type,task,ts,extra={})=>({
 event_type:type,task_ref:task,occurred_at_ms:ts,
 severity:"untrusted severity "+canary,private_payload:canary,
 raw_request_id:canary,raw_trace_id:canary,...extra
});
const s={
 schema_version:2,scope:"canonical_project_workspace",
 tasks:[{id:canary,objective:canary,state:"failed"}],
 timeline:[
  event("task.started",t,1000,{trace_ref:trace,request_ref:request}),
  event("agent_worker.started",t,1100,{trace_ref:trace,run_ref:run}),
  event("agent_worker.step",t,1200,{trace_ref:trace,run_ref:run}),
  event("task.failed",t,1300,{trace_ref:trace}),
  event("task.started",other,1300,{trace_ref:trace}),
  event("agent_worker.step",other,1350,{trace_ref:"trace-"+ "f".repeat(24)}),
  event("task.waiting_approval",t,1400,{request_ref:request}),
  event("agent_worker.started",t,1500,{request_ref:request,run_ref:run}),
  event("agent_worker.started",t,1600,{run_ref:run}),
  event("task.failed",t,1700,{event_ref:canary}),
  event("task."+canary,t,1800,{trace_ref:trace}),
  event("task.failed",canary,1900,{trace_ref:trace}),
  event("task.ready",t,-1)
 ],
 timeline_truncated:true,truncated:true,
 workspace_id:canary,raw_log:canary
};
const got=correlate(s);
assert.equal(got.schema_version,1);
assert.equal(got.browser_api_link,"unavailable_no_shared_trace");
assert.equal(got.tool_gateway_link,"not_in_this_evidence_source");
assert.equal(got.independent_verification,"not_established_by_event_chronology");
assert.equal(got.attention_events_observed,2);
assert.equal(got.omitted_events,1);
const linked=got.groups.find(g=>g.task_ref===t&&g.correlation_ref===trace);
assert.ok(linked);
assert.equal(linked.cross_component_link,"observed_shared_trace");
assert.equal(linked.correlation_level,"trace");
assert.deepEqual(linked.observed_sources,["task","worker"]);
assert.deepEqual(linked.events.map(e=>e.type),["task.started","agent_worker.started","agent_worker.step","task.failed"]);
assert.equal(linked.severity,"attention"); // derived from typed event, not raw severity
const requestGroup=got.groups.find(g=>g.correlation_ref===request&&g.correlation_level==="request");
assert.equal(requestGroup.cross_component_link,"observed_shared_request");
const otherGroup=got.groups.find(g=>g.task_ref===other&&g.correlation_ref===trace);
assert.equal(otherGroup.cross_component_link,"not_observed"); // no cross-Task linking
assert.ok(got.groups.some(g=>g.correlation_level==="run_only"));
assert.ok(got.groups.some(g=>g.correlation_level==="task_only"));
const text=format(got),attention=format(got,"attention"),waiting=format(got,"waiting");
assert.ok(text.includes("Browser → backend: unavailable"));
assert.ok(text.includes("Independent verification: not established"));
assert.ok(attention.includes("task.failed"));
assert.ok(waiting.includes("task.waiting_approval"));
assert.ok(!waiting.includes("task.failed"));
for(const output of [JSON.stringify(got),text,attention,waiting]){
 assert.ok(!output.includes(canary),"raw private field leaked");
 assert.ok(!output.includes('"id"'),"raw Task ID copied");
 assert.ok(!output.includes('"objective"'),"Task objective copied");
}
assert.throws(()=>correlate({...s,scope:"other_workspace"}));
assert.throws(()=>correlate({...s,schema_version:3}));
assert.throws(()=>correlate({...s,timeline:Array(97).fill(s.timeline[0])}));
assert.throws(()=>correlate({...s,tasks:Array(51).fill(s.tasks[0])}));
const many=correlate({...s,timeline:Array.from({length:96},(_,i)=>event("task.started",t,i+1))});
assert.equal(many.groups.length,48);
assert.equal(many.omitted_events,48);
assert.equal(format(got,"bad"),format(got,"all"));
const root=path.resolve(__dirname,".."),read=p=>fs.readFileSync(path.join(root,p),"utf8");
const workspaceUI=read("internal/webui/static/workspace-ai-workflow.js");
const index=read("internal/webui/static/index.html");
const workflow=read(".github/workflows/project-workspace-development-checks.yml");
for(const x of ['id="a59ScopedTraceCorrelation"','id="a59Correlate"','id="a59Severity"',
 'id="a59CorrelationPreview"','a59ScopedCorrelation.correlate(snapshot)',
 '"/v1/qa/workspace-snapshot?"+qaQuery,{cache:"no-store"}',
 'correlationPreview.textContent=a59ScopedCorrelation.format('])assert.ok(workspaceUI.includes(x),x);
assert.ok(index.includes('/qa-trace-correlation.js'));
assert.ok(index.indexOf('/qa-trace-correlation.js')<index.indexOf('/workspace-ai-workflow.js'));
assert.ok(workflow.includes("node scripts/validate_qa_trace_correlation.js"));
assert.ok(!read("internal/webui/static/qa-trace-correlation.js").includes("fetch("));
console.log("PASS: bounded cross-component Task/Worker trace grouping, cross-Task isolation, request-only distinction, missing evidence, filters and secret canaries");
