#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const {build}=require("../internal/webui/static/qa-report-summary.js");

const canary="SUPER_SECRET_CANARY";
const valid=build({
 schema_version:2,
 build_version:"alpha3.3-rc11",
 build_revision:"a1b2c3d4e5f67890",
 generated_utc:"2026-10-10T04:45:00Z",
 max_tasks:50,captured_tasks:3,truncated:true,
 max_timeline_events:96,captured_timeline_events:5,timeline_truncated:true,
 tasks:[
  {id:"task-"+canary,objective:"Do not copy "+canary,state:"blocked",
   execution:{last_error:canary,run_id:"run-"+canary}},
  {id:"task2",state:"running",secret_ref:canary},
  {id:"task3",state:"corrupt-"+canary},
 ],
 timeline:[
  {event_type:"task.failed",payload:{password:canary},trace_id:"trace-"+canary},
  {event_type:"task.blocked",secret:canary},
  {event_type:"agent_worker.step",message:canary},
  {event_type:"task."+canary},
  {event_type:"task.waiting_approval",actor:canary},
 ]
});
assert.match(valid,/alpha3\.3-rc11/);
assert.match(valid,/a1b2c3d4e5f67890/);
assert.match(valid,/Tasks in snapshot: 3 \/ maximum 50 \(additional tasks omitted\)/);
assert.match(valid,/Known Task\/Worker events: 5 \/ maximum 96 \(older events omitted\)/);
assert.match(valid,/task.failed=1/);
assert.match(valid,/task.blocked=1/);
assert.match(valid,/task.waiting_approval=1/);
assert.match(valid,/unavailable=1/);
assert.doesNotMatch(valid,/SUPER_SECRET_CANARY|task2|task3|trace-|run-|last_error|password|actor:/);
assert.doesNotMatch(valid,/tool.?result|workspace_id|project_workspace_id/);

const hostile=build({
 schema_version:2,
 build_version:canary,build_revision:canary,
 generated_utc:canary,
 tasks:[{state:canary},{state:"complete",objective:canary}],
 timeline:[{event_type:canary},{event_type:"task."+canary}],
 captured_tasks:999999999999,max_tasks:999999999999,
 captured_timeline_events:-300,max_timeline_events:Infinity,
});
assert.doesNotMatch(hostile,/SUPER_SECRET_CANARY/);
assert.match(hostile,/Build: unavailable · Revision: unavailable/);
assert.match(hostile,/Snapshot UTC: unavailable/);
assert.match(hostile,/Tasks in snapshot: 0 \/ maximum 0/);
assert.match(hostile,/Known Task\/Worker events: 0 \/ maximum 0/);
assert.equal(typeof build(null),"string");
assert.ok(valid.length<3000,"pasteable summary must remain compact");
console.log("PASS: QA summary emits only allowlisted metadata and safe aggregate counts");
