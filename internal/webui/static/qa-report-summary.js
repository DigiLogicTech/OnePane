/* Read-only OnePane QA summary; never insert raw Task/Run IDs, prompts,
 * diagnostic payloads, user-generated descriptions or secret-bearing paths.
 * Module stays pure so its privacy contract can be tested in Node and browser.
 */
(function(root){
 "use strict";
 const taskStates=new Set([
  "created","ready","running","waiting_dependency","waiting_approval",
  "paused","completion_requested","verifying","blocked","failed","complete",
  "cancel_requested","cancelling","compensating","cancelled"
 ]);
 const eventTypes=new Set([
  "task.created","task.ready","task.started","task.waiting_dependency",
  "task.waiting_approval","task.paused","task.resumed",
  "task.completion_requested","task.verification_started",
  "task.verification_retry","task.completed","task.blocked","task.failed",
  "task.cancel_requested","task.attempt_interrupted","task.archived",
  "task.unarchived","agent_worker.started","agent_worker.step"
 ]);
 const safeCount=(n,max=100000)=>Number.isSafeInteger(n)&&n>=0&&n<=max?n:0;
 const safeDate=(v)=>typeof v==="string"&&/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z$/.test(v)&&
  Number.isFinite(Date.parse(v))?v:"unavailable";
 function build(snapshot){
  const s=snapshot&&typeof snapshot==="object"?snapshot:{};
  // Build fields are controlled by trusted build metadata, but still
  // constrained here to prevent accidentally echoing arbitrary server text.
  const version=typeof s.build_version==="string"&&
   /^[A-Za-z0-9.+_-]{1,32}$/.test(s.build_version)?s.build_version:"unavailable";
  const revision=typeof s.build_revision==="string"&&
   /^[0-9a-fA-F]{7,40}$/.test(s.build_revision)?s.build_revision:"unavailable";
  const tasks=Array.isArray(s.tasks)?s.tasks.slice(0,50):[];
  const timeline=Array.isArray(s.timeline)?s.timeline.slice(0,96):[];
  const counts={};
  for(const t of tasks){
   const state=taskStates.has(t?.state)?t.state:"unavailable";
   counts[state]=(counts[state]||0)+1;
  }
  const incidents={};
  for(const e of timeline){
   if(!eventTypes.has(e?.event_type))continue;
   if(["task.failed","task.blocked","task.attempt_interrupted","task.waiting_approval"].includes(e.event_type)){
    incidents[e.event_type]=(incidents[e.event_type]||0)+1;
   }
  }
  const lines=[
   "OnePane RC11 — Sanitised Workspace QA summary",
   "Build: "+version+" · Revision: "+revision,
   "Snapshot UTC: "+safeDate(s.generated_utc),
   "Scope: one authorised canonical Project Workspace; IDs omitted",
   "Tasks in snapshot: "+safeCount(s.captured_tasks,50)+" / maximum "+safeCount(s.max_tasks,50)+
    (s.truncated===true?" (additional tasks omitted)":""),
   "Task states: "+(Object.keys(counts).length?Object.keys(counts).sort().map(k=>k+"="+counts[k]).join(", "):"none observed"),
   "Known Task/Worker events: "+safeCount(s.captured_timeline_events,96)+" / maximum "+
    safeCount(s.max_timeline_events,96)+(s.timeline_truncated===true?" (older events omitted)":""),
   "Attention event types: "+(Object.keys(incidents).length?Object.keys(incidents).sort().map(k=>k+"="+incidents[k]).join(", "):"none in captured timeline"),
   "Evidence: observed Task/Worker metadata only; not proof of external action success",
   "Excluded: raw logs and event payloads, prompts/model output, source files, secrets, cookies and Node/installer diagnostics",
   "Report status: "+(s.schema_version===2?"schema v2":"schema unavailable; check source"),
   "To investigate: attach the separately reviewed OnePane Workspace QA ZIP if appropriate."
  ];
  return lines.join("\n");
 }
 root.a52MakeQASummary=build;
 if(typeof module==="object"&&module&&module.exports)module.exports={build};
})(typeof globalThis!=="undefined"?globalThis:this);
