/* RC11 scoped Task/Worker correlation. Pure allowlist projection only.
 * Input MUST come from the existing authenticated canonical Workspace QA API.
 * A matching opaque trace links observed events, not verified side effects.
 */
(function(root){
 "use strict";
 const KNOWN_TYPES=new Set([
  "task.created","task.ready","task.started","task.waiting_dependency",
  "task.waiting_approval","task.paused","task.resumed","task.completion_requested",
  "task.verification_started","task.verification_retry","task.completed",
  "task.blocked","task.failed","task.cancel_requested","task.attempt_interrupted",
  "task.archived","task.unarchived","agent_worker.started","agent_worker.step"
 ]);
 const taskPattern=/^task-[a-f0-9]{24}$/;
 const refs={
  trace:/^trace-[a-f0-9]{24}$/,
  request:/^request-[a-f0-9]{24}$/,
  run:/^run-[a-f0-9]{24}$/
 };
 const safeRef=(raw,re)=>typeof raw==="string"&&re.test(raw)?raw:"";
 const CAP_EVENTS=96,CAP_GROUPS=48;
 const timeValue=n=>Number.isSafeInteger(n)&&n>0&&n<4102444800000?n:0;
 function correlate(snapshot){
  if(!snapshot||snapshot.schema_version!==2||snapshot.scope!=="canonical_project_workspace"||
   !Array.isArray(snapshot.timeline)||snapshot.timeline.length>CAP_EVENTS||
   !Array.isArray(snapshot.tasks)||snapshot.tasks.length>50)
   throw Error("Unrecognised or oversized canonical Workspace QA evidence");
  const groups=new Map();
  let omitted=0,attention=0,weak=0;
  for(const [index,e] of snapshot.timeline.entries()){
   if(!e||!KNOWN_TYPES.has(e.event_type)||!timeValue(e.occurred_at_ms))continue;
   const task=safeRef(e.task_ref,taskPattern);
   if(!task){omitted++;continue}
   const trace=safeRef(e.trace_ref,refs.trace);
   const request=safeRef(e.request_ref,refs.request);
   const run=safeRef(e.run_ref,refs.run);
   const isWorker=e.event_type.startsWith("agent_worker.");
   // Severity is derived from the known event type, not an externally
   // supplied free-form severity field.
   const severity=["task.failed","task.blocked","task.attempt_interrupted"].includes(e.event_type)?"attention":
    ["task.waiting_approval","task.waiting_dependency","task.cancel_requested"].includes(e.event_type)?"waiting":"information";
   const event={type:e.event_type,severity,at:e.occurred_at_ms,
    source:isWorker?"worker":"task"};
   if(severity==="attention")attention++;
   // Trace is strongest. A shared request is weaker; run-only and task-only
   // observations NEVER pretend to be a correlated cross-component trace.
   const level=trace?"trace":request?"request":run?"run_only":"task_only";
   const key=task+"|"+level+"|"+(trace||request||run||String(index));
   if(!trace&&!request)weak++;
   const ref=trace||request||run||"";
   if(!groups.has(key)){
    if(groups.size>=CAP_GROUPS){omitted++;continue}
    groups.set(key,{task_ref:task,correlation_level:level,correlation_ref:ref,
     run_ref:run,first_seen_ms:event.at,last_seen_ms:event.at,
     observed_sources:[],events:[],severity:"information",verified_effects:false});
   }
   const g=groups.get(key);
   // No raw text or dynamic identifiers from the source are retained.
   g.first_seen_ms=Math.min(g.first_seen_ms,event.at);
   g.last_seen_ms=Math.max(g.last_seen_ms,event.at);
   g.events.push(event);
   if(!g.observed_sources.includes(event.source))g.observed_sources.push(event.source);
   if(severity==="attention")g.severity="attention";
   else if(severity==="waiting"&&g.severity==="information")g.severity="waiting";
  }
  const result=[...groups.values()].map(g=>{
   g.events.sort((a,b)=>a.at-b.at);
   g.observed_sources.sort();
   g.cross_component_link=g.correlation_level==="trace"&&
    g.observed_sources.length===2?"observed_shared_trace":
    g.correlation_level==="request"&&g.observed_sources.length===2?"observed_shared_request":
    "not_observed";
   return g;
  }).sort((a,b)=>b.last_seen_ms-a.last_seen_ms||a.task_ref.localeCompare(b.task_ref));
  return {
   schema_version:1,scope:"canonical_project_workspace",
   evidence_kind:"observed_task_worker_timeline_only",
   groups:result,events_considered:Math.min(CAP_EVENTS,snapshot.timeline.length),
   groups_shown:result.length,omitted_events:omitted,
   timeline_truncated:snapshot.timeline_truncated===true,
   tasks_truncated:snapshot.truncated===true,
   attention_events_observed:attention,
   events_without_trace_or_request:weak,
   browser_api_link:"unavailable_no_shared_trace",
   tool_gateway_link:"not_in_this_evidence_source",
   independent_verification:"not_established_by_event_chronology",
   note:"Shared opaque trace/request refs reflect observed metadata, not verified tool execution or artifact correctness"
  };
 }
 const summaries={
  observed_shared_trace:"Task and Worker events share an observed trace reference",
  observed_shared_request:"Task and Worker events share a request reference; trace unavailable",
  not_observed:"Cross-component trace link not observed"
 };
 function format(report,filter="all"){
  if(!report||report.schema_version!==1||report.scope!=="canonical_project_workspace")
   throw Error("Invalid correlation report");
  const chosen=["all","attention","waiting","information"].includes(filter)?filter:"all";
  const rows=report.groups.filter(g=>chosen==="all"||g.severity===chosen);
  const lines=[
   "OnePane RC11 — scoped Task/Worker correlation",
   "Scope: one authorised canonical Project Workspace",
   "Events examined: "+report.events_considered+"; groups: "+report.groups_shown+
    "; omitted events: "+report.omitted_events,
   report.timeline_truncated?"Coverage: latest timeline truncated":"Coverage: bounded recent timeline only",
   "Browser → backend: unavailable (no shared trace ID recorded)",
   "Tool Gateway → external action: not observed in this evidence",
   "Independent verification: not established by event chronology",
   "Filter: "+chosen
  ];
  for(const g of rows){
   lines.push(
    "Task reference "+g.task_ref+"; "+g.correlation_level+
    (g.correlation_ref?" "+g.correlation_ref:"")+
    "; "+g.cross_component_link+"; attention="+g.severity,
    ...g.events.map(e=>"  "+e.source+" "+e.type+" "+e.severity+" @ "+e.at)
   );
  }
  if(!rows.length)lines.push("No records in this filtered timeline");
  return lines.join("\n");
 }
 root.a59ScopedCorrelation={correlate,format,summaries};
 if(typeof module==="object"&&module.exports)module.exports={correlate,format,summaries};
})(typeof globalThis!=="undefined"?globalThis:this);
