/* RC11: separate source-rechecked, typed provenance projector. None of these
 * persisted links establishes external effects. All identity is opaque.
 */
(function(root){
 "use strict";
 const valid={
  task:/^task-[a-f0-9]{24}$/,run:/^run-[a-f0-9]{24}$/,attempt:/^attempt-[a-f0-9]{24}$/,
  tool:/^tool-[a-f0-9]{24}$/,operation:/^operation-[a-f0-9]{24}$/,
  observation:/^observation-[a-f0-9]{24}$/,
  verification:/^verification-[a-f0-9]{24}$/,trace:/^trace-[a-f0-9]{24}$/
 };
 const sourceKinds=["tool","operation"];
 const toolStates=["created","authorized","running","succeeded","failed","timed_out","cancelled","interrupted"];
 const operationStates=["proposed","authorized","prepared","executing","observing","verified",
  "committed","denied","failed","aborted","unknown_outcome",
  "blocked_unknown_outcome","compensating","compensated","compensation_failed"];
 const levels=["V0","V1","V2","V3","V4","V5"];
 const states=["pending","pass","fail","inconclusive","stale"];
 const assuranceStates=["queued","running","waiting_evidence","waiting_human",
  "passed","failed","inconclusive","interrupted","not_recorded"];
 const en=(v,allowed)=>allowed.includes(v)?v:"unavailable";
 function project(snapshot,capture){
  if(!snapshot||snapshot.schema_version!==2||snapshot.scope!=="canonical_project_workspace"||
    !Array.isArray(snapshot.tasks)||snapshot.tasks.length>50||
    !Array.isArray(snapshot.timeline)||snapshot.timeline.length>96)
   throw Error("Invalid scoped QA snapshot");
  const permittedTasks=new Set(snapshot.tasks.filter(t=>valid.task.test(t?.task_ref||"")).map(t=>t.task_ref));
  const rows=snapshot.worker_tool_links;
  const assuranceRows=snapshot.assurance_evidence;
  if(rows!==undefined&&(!Array.isArray(rows)||rows.length>24))throw Error("Invalid Worker provenance");
  if(assuranceRows!==undefined&&(!Array.isArray(assuranceRows)||assuranceRows.length>24))throw Error("Invalid assurance provenance");
  const rawLineage=snapshot.async_lineage,rawProbes=snapshot.probe_witnesses;
  if(rawLineage!==undefined&&(!Array.isArray(rawLineage)||rawLineage.length>24))
   throw Error("Oversized asynchronous lineage");
  if(rawProbes!==undefined&&(!Array.isArray(rawProbes)||rawProbes.length>24))
   throw Error("Oversized probe witnesses");
  const lineage=[];
  for(const e of rawLineage||[]){
   if(!e||!permittedTasks.has(e.task_ref)||!valid.attempt.test(e.attempt_ref||"")||
    !valid.run.test(e.worker_run_ref||"")||
    e.link_kind!=="persisted_task_attempt_worker_foreign_keys"||
    !Number.isSafeInteger(e.steps_recorded)||e.steps_recorded<0||e.steps_recorded>256)continue;
   lineage.push({task_ref:e.task_ref,attempt_ref:e.attempt_ref,worker_run_ref:e.worker_run_ref,
    attempt_state:en(e.attempt_state,["created","queued","running","waiting","succeeded",
     "failed","cancelled","interrupted"]),
    worker_state:en(e.worker_state,["running","waiting","blocked","succeeded","failed","interrupted"]),
    steps_recorded:e.steps_recorded});
  }
  const probes=[];
  for(const e of rawProbes||[]){
   if(!e||!permittedTasks.has(e.task_ref)||
    !valid.verification.test(e.verification_ref||"")||
    !valid.observation.test(e.observation_ref||"")||
    !["direct","integration"].includes(e.role)||
    e.evidence_class!=="recorded_probe_observation_not_external_effect_attestation")continue;
   const intact=e.integrity_rechecked===true,independent=e.independent_source===true;
   const fresh=e.observed_after_attempt===true&&e.observed_after_operation_start===true;
   const qualified=e.corroborating_integration_probe===true&&e.recorded_assurance_pass===true&&
    e.role==="integration"&&intact&&independent&&fresh;
   probes.push({task_ref:e.task_ref,verification_ref:e.verification_ref,
    observation_ref:e.observation_ref,role:e.role,
    source_trust:en(e.source_trust,["trusted_control","trusted_procedure","authoritative_data",
     "user_instruction","untrusted_content","unverified_derived","verified_derived"]),
    integrity_rechecked:intact,independent_source:independent,fresh,
    recorded_assurance_pass:e.recorded_assurance_pass===true,
    corroborating_integration_probe:qualified});
  }
  const worker=[];
  for(const e of rows||[]){
   if(!e||!permittedTasks.has(e.task_ref)||!valid.run.test(e.run_ref||"")||
    !sourceKinds.includes(e.source)||e.link_kind!=="persisted_same_task_attempt_reference")continue;
   const target=valid[e.source];
   if(!target.test(e.target_ref||"")||!["succeeded","unknown"].includes(e.journal_status))continue;
   const observation=e.source==="tool"&&valid.observation.test(e.observation_ref||"")?e.observation_ref:"";
   if(e.source==="tool"&&!observation)continue;
   worker.push({task_ref:e.task_ref,run_ref:e.run_ref,source:e.source,
    target_ref:e.target_ref,observation_ref:observation,journal_status:e.journal_status,
    target_status:en(e.target_status,e.source==="tool"?toolStates:operationStates)});
  }
  const assurance=[];
  for(const e of assuranceRows||[]){
   if(!e||!permittedTasks.has(e.task_ref)||!valid.verification.test(e.verification_ref||""))continue;
   const run=valid.run.test(e.worker_run_ref||"")?e.worker_run_ref:"";
   const op=valid.operation.test(e.operation_ref||"")?e.operation_ref:"";
   const required=en(e.required_level,levels),achieved=en(e.achieved_level,levels);
   const verification=en(e.verification_status,states),ass=en(e.assurance_status,assuranceStates);
   const present=e.evidence_digest_recorded===true,distinct=e.distinct_verifier_recorded===true&&!!run;
   const pass=e.recorded_assurance_pass===true&&present&&distinct&&
    verification==="pass"&&ass==="passed"&&levels.indexOf(required)>=1&&
    levels.indexOf(achieved)>=levels.indexOf(required);
   assurance.push({task_ref:e.task_ref,verification_ref:e.verification_ref,
    worker_run_ref:run,operation_ref:op,required_level:required,achieved_level:achieved,
    verification_status:verification,assurance_status:ass,
    evidence_digest_recorded:present,distinct_verifier_recorded:distinct,
    recorded_assurance_pass:pass,operation_state:en(e.operation_state,operationStates)});
  }
  const created=[];
  if(capture!==undefined){
   if(!capture||capture.schema_version!==1||capture.source!=="operator_tagged_local_http"||
    !Array.isArray(capture.observations)||capture.observations.length>64)throw Error("Invalid operator-owned capture");
   const taskByTrace=new Map();
   for(const e of snapshot.timeline){
    if(!e||e.event_type!=="task.created"||!permittedTasks.has(e.task_ref)||
     !valid.trace.test(e.trace_ref||""))continue;
    if(taskByTrace.has(e.trace_ref))taskByTrace.set(e.trace_ref,null);
    else taskByTrace.set(e.trace_ref,e.task_ref);
   }
   const claimed=new Set();
   for(const e of capture.observations){
    if(e?.subsystem!=="tasks"||e.method!=="POST"||e.status_code!==201||
     !valid.trace.test(e.trace_ref||"")||claimed.has(e.trace_ref))continue;
    claimed.add(e.trace_ref);
    const task=taskByTrace.get(e.trace_ref);
    if(task)created.push({task_ref:task,trace_ref:e.trace_ref});
   }
  }
  return {scope:"canonical_project_workspace",schema_version:1,
   tagged_capture_reviewed:capture!==undefined,created,
   worker,assurance,lineage,probes,
   lineage_truncated:snapshot.async_lineage_truncated===true,
   probes_truncated:snapshot.probe_witnesses_truncated===true,
   worker_truncated:snapshot.worker_tool_links_truncated===true,
   assurance_truncated:snapshot.assurance_evidence_truncated===true,
   limitations:"Persistent same-Task associations only; no timing-based inference or external side-effect attestation"};
 }
 function format(p){
  if(p?.scope!=="canonical_project_workspace"||p?.schema_version!==1)throw Error("Invalid provenance report");
  const lines=["",
   "OnePane RC11 — persisted execution provenance",
   "Trusted HTTP → Task creation matches: "+p.created.length+
    (p.tagged_capture_reviewed?" (operator-owned capture checked)":" (capture not requested)"),
   ...p.created.map(e=>"  POST /v1/tasks → "+e.task_ref+" via minted "+e.trace_ref),
   "Worker → Tool/operation journal links: "+p.worker.length+(p.worker_truncated?" (older groups omitted)":""),
   ...p.worker.map(e=>"  Task "+e.task_ref+" Worker "+e.run_ref+" → "+e.source+" "+e.target_ref+
     "; journal="+e.journal_status+" persisted="+e.target_status+
     (e.observation_ref?" observation="+e.observation_ref:"")),
   "Asynchronous persisted Task → Attempt → Worker links: "+(p.lineage?.length||0)+
    (p.lineage_truncated?" (older runs omitted)":""),
   ...(p.lineage||[]).map(e=>"  Task "+e.task_ref+" → Attempt "+e.attempt_ref+" ("+e.attempt_state+
    ") → Worker "+e.worker_run_ref+" ("+e.worker_state+") steps="+e.steps_recorded+
    " [FK-linked, not propagated HTTP trace]"),
   "Rechecked assurance probe observations: "+(p.probes?.length||0)+
    (p.probes_truncated?" (older evidence omitted)":""),
   ...(p.probes||[]).map(e=>"  Task "+e.task_ref+" verification="+e.verification_ref+
    " observation="+e.observation_ref+" role="+e.role+" trust="+e.source_trust+
    " integrity-rechecked="+e.integrity_rechecked+" fresh="+e.fresh+
    " independent-source="+e.independent_source+
    " recorded-assurance-pass="+e.recorded_assurance_pass+
    " corroborating-integration-record="+e.corroborating_integration_probe+
    " [revalidated stored evidence, not a new external probe]"),
   "Independent-assurance records: "+p.assurance.length+(p.assurance_truncated?" (older records omitted)":""),
   ...p.assurance.map(e=>"  Task "+e.task_ref+" verification="+e.verification_ref+
    " state="+e.verification_status+" assurance="+e.assurance_status+
    " level="+e.achieved_level+"/"+e.required_level+
    " digest-recorded="+e.evidence_digest_recorded+
    " distinct-verifier="+e.distinct_verifier_recorded+
    " recorded-assurance-pass="+e.recorded_assurance_pass+
    (e.operation_ref?" operation="+e.operation_ref+" state="+e.operation_state:"")),
   "Limits: durable associations and recorded assurance statuses only; NOT proof of correct model answers, physical Tool effects, or external world state."
  ];
  return lines.join("\n");
 }
 const api={project,format};
 root.a63ExecutionProvenance=api;
 if(typeof module==="object"&&module.exports)module.exports=api;
})(typeof globalThis!=="undefined"?globalThis:this);
