/* RC11 support bundle: purely local, field-allowlisted source projections.
 * Inputs are untrusted even after backend authorisation. Never insert raw
 * server documents, user content or recorder objects into an export.
 * ZIP is STORE-only and assembled in memory. No upload, packages or temp files.
 */
(function(root){
 "use strict";
 const MAX_ZIP=256*1024,MAX_PREVIEW=192*1024;
 const str=(v)=>typeof v==="string"?v:"";
 const enumOf=(v,values,fallback="unavailable")=>values.includes(v)?v:fallback;
 const count=(v,limit=100000)=>Number.isSafeInteger(v)&&v>=0&&v<=limit?v:0;
 const timestamp=(v)=>Number.isSafeInteger(v)&&v>0&&v<=9999999999999?v:null;
 const utc=(v)=>typeof v==="string"&&/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(v)&&
  Number.isFinite(Date.parse(v))?v:null;
 const hash=(v,prefix)=>new RegExp("^"+prefix+"-[0-9a-f]{24}$").test(str(v))?v:"unavailable";
 const states=["created","ready","running","waiting_dependency","waiting_approval","paused","completion_requested",
  "verifying","blocked","failed","complete","cancel_requested","cancelling","compensating","cancelled"];
 const events=["task.created","task.ready","task.started","task.waiting_dependency","task.waiting_approval",
  "task.paused","task.resumed","task.completion_requested","task.verification_started",
  "task.verification_retry","task.completed","task.blocked","task.failed",
  "task.cancel_requested","task.attempt_interrupted","task.archived","task.unarchived",
  "agent_worker.started","agent_worker.step"];
 function requireShape(data,version,scope){
  if(!data||typeof data!=="object"||Array.isArray(data)||
   data.schema_version!==version||data.scope!==scope)throw Error("Unrecognised QA source schema or scope");
 }
 function workspace(s){
  requireShape(s,2,"canonical_project_workspace");
  const tasks=Array.isArray(s.tasks)?s.tasks.slice(0,50):[];
  const timeline=Array.isArray(s.timeline)?s.timeline.slice(0,96):[];
  const task_states={};const attention_events={};
  for(const t of tasks){const state=enumOf(t?.state,states);task_states[state]=(task_states[state]||0)+1}
  for(const event of timeline){
   const type=enumOf(event?.event_type,events);
   if(["task.failed","task.blocked","task.attempt_interrupted","task.waiting_approval"].includes(type))
    attention_events[type]=(attention_events[type]||0)+1;
  }
  // No raw task IDs, objectives, timeline refs, artifact names or project IDs.
  return {schema_version:1,source:"authorised_workspace_aggregate",
   captured_tasks:count(s.captured_tasks,50),max_tasks:50,truncated:s.truncated===true,
   task_states,observed_timeline_events:count(s.captured_timeline_events,96),
   max_timeline_events:96,timeline_truncated:s.timeline_truncated===true,
   attention_events,
   evidence_limit:"aggregate of authorised Task/Worker metadata; not a proof of external actions"};
 }
 const routes=["operations","workspaces","projects","tasks","models","nodes","agents","sandboxes",
  "routines","providers","integrations","secrets","evidence","settings","library",
  "assistant","skills","web-chat","chat","other"];
 const subsystems=["diagnostics","models","projects","tasks","nodes","authentication","providers",
  "agents","evidence","system","api_other"];
 const methods=["GET","POST","PUT","PATCH","DELETE","HEAD","OPTIONS","OTHER"];
 function browser(b){
  if(!b||b.schema_version!==1||b.source!=="onepane_browser_opt_in"||
   !["stopped","expired"].includes(b.status))throw Error("Browser capture must be stopped and reviewed");
  if(!Array.isArray(b.events)||b.events.length>120)throw Error("Browser incident source exceeded event limit");
  const safe=[];
  for(const e of b.events){
   if(!e||typeof e!=="object")continue;
   const kind=enumOf(e.kind,["capture","browser","ui","api"],"other");
   if(kind==="other")continue;
   const action=kind==="capture"?enumOf(e.action,["started","stopped","marked_issue"]):
    kind==="browser"?enumOf(e.action,["uncaught_error","unhandled_rejection"]):
    kind==="ui"?enumOf(e.action,["button","link","control","form_submit"]):
    enumOf(e.action,["response","transport_failure"]);
   const item={kind,action,at_utc:utc(e.at_utc)};
   if(kind==="browser"||kind==="ui"||kind==="capture")item.route=enumOf(e.route,routes,"other");
   if(kind==="api"){
    item.subsystem=enumOf(e.subsystem,subsystems);
    item.method=enumOf(e.method,methods);
    item.status=count(e.status,599);
    item.duration_ms=count(e.duration_ms,60000);
   }
   if(kind==="capture")item.outcome=enumOf(e.outcome,["recording","stopped","expired"]);
   safe.push(item);
  }
  return {schema_version:1,source:"onepane_browser_opt_in",
   status:b.status,started_utc:utc(b.started_utc),ended_utc:utc(b.ended_utc),
   dropped_events:count(b.dropped_events,1000000),max_events:120,events:safe,
   evidence_limit:"only generic interaction categories, API status and durations; no contents"};
 }
 const modelStatuses=["discovered","qualifying","ready","degraded","draining","unavailable","disabled"];
 const residencies=["stopped","loading","resident","busy","draining","failed","not_observed"];
 const failureStages=["runtime_acquire","deployment_read","model_read","inference_dispatch",
  "response_validation","turn_store","completion_validation","completion_persist",
  "runtime_release","runtime_unload","session_abort","unavailable"];
 const failureCats=["cancelled","deadline_exceeded","not_found","invalid_response",
  "storage_failure","abort_requested","execution_failure","unknown"];
 function model(m){
  requireShape(m,2,"authorised_managed_model_deployment");
  if(!Array.isArray(m.sessions)||m.sessions.length>10)throw Error("Model evidence exceeds session cap");
  return {schema_version:1,source:"authorised_agent_check_aggregate",
   deployment_ref:hash(m.deployment_ref,"deployment"),
   deployment_status:enumOf(m.deployment_status,modelStatuses),
   observed_residency_state:enumOf(m.observed_residency_state,residencies,"not_observed"),
   deployment_updated_at_ms:timestamp(m.deployment_updated_at_ms),
   captured_sessions:count(m.captured_sessions,10),sessions_truncated:m.sessions_truncated===true,
   sessions:m.sessions.map(s=>({
    session_ref:hash(s?.session_ref,"testbed"),
    status:enumOf(s?.status,["active","completed","cancelled"]),
    started_at_ms:timestamp(s?.started_at_ms),
    ended_at_ms:timestamp(s?.ended_at_ms),
    recorded_successful_turns:count(s?.recorded_successful_turns,100000),
    recorded_tool_probe_turns:count(s?.recorded_tool_probe_turns,100000),
    failure_details:enumOf(s?.failure_details,
     ["not_recorded_as_structured_evidence","structured_failure_category_only"]),
    last_failure_stage:enumOf(s?.last_failure_stage,failureStages),
    last_failure_category:enumOf(s?.last_failure_category,failureCats,"unknown"),
    failure_observed_at_ms:timestamp(s?.failure_observed_at_ms)
   })),
   evidence_limit:"recorded Agent Check counts and typed stages; no independent GPU/CPU measurement"};
 }
 const trust=["local","discovered","pairing","paired","revoked","unavailable"];
 const pairing=["requested","pairing","paired","rejected","revoked","expired","failed","not_recorded"];
 const wakeStates=["requested","wake_sent","reconnecting","preparing","ready","failed","timed_out","not_recorded"];
 const serviceStates=["running","stopped","starting","stopping","failed","paused","not_installed","not_collected"];
 function node(n){
  requireShape(n,3,"admin_authorised_local_control_plane");
  const w=n.wake_attempts||{},f=n.inference_receipts||{},local=n.local_service_observation;
  let service=null;
  if(n.registered_local_node===true&&local&&typeof local==="object"){
   service={manager:enumOf(local.manager,["systemd","windows_scm","not_collected"]),
    collection:enumOf(local.collection,["observed","timeout","unavailable","not_collected"]),
    state:enumOf(local.state,serviceStates,"not_collected"),
    observed_at_ms:timestamp(local.observed_at_ms)};
   if(service.collection!=="observed")service.state="not_collected";
  }
  const readiness=n.registered_local_node===true&&n.backend_readiness&&
   typeof n.backend_readiness==="object"&&!Array.isArray(n.backend_readiness)&&
   n.backend_readiness.scope==="canonical_local_backend"?n.backend_readiness:null;
  const safeReadiness=readiness?{
   scope:"canonical_local_backend",
   observed_at_ms:timestamp(readiness.observed_at_ms),
   database_response:enumOf(readiness.database_response,["responding_read_only","unavailable"]),
   schema_record_status:enumOf(readiness.schema_record_status,
    ["recorded_versions_match_embedded","recorded_versions_incomplete_or_extra","unavailable"]),
   recorded_schema_version:count(readiness.recorded_schema_version,9999),
   embedded_schema_version:count(readiness.embedded_schema_version,9999),
   local_node_registration:enumOf(readiness.local_node_registration,
    ["registered","missing","not_verified"]),
   task_service_wiring:enumOf(readiness.task_service_wiring,["configured_not_probed","not_configured"]),
   model_service_wiring:enumOf(readiness.model_service_wiring,["configured_not_probed","not_configured"]),
   vault_service_wiring:enumOf(readiness.vault_service_wiring,["configured_not_probed","not_configured"]),
   federation_service_wiring:enumOf(readiness.federation_service_wiring,
    ["configured_not_probed","not_configured"]),
   evidence_limit:"read-only DB/schema registration observations; no write, integrity or operational attestation"
  }:null;
  return {schema_version:1,source:"admin_authorised_node_control_plane",
   node_ref:hash(n.node_ref,"node"),registered_local_node:n.registered_local_node===true,
   recorded_trust_state:enumOf(n.recorded_trust_state,trust),
   last_seen_at_ms:timestamp(n.last_seen_at_ms),node_record_updated_at_ms:timestamp(n.node_record_updated_at_ms),
   manifest_state:enumOf(n.manifest_state,["not_recorded","recorded_expiry_unverified"]),
   manifest_sequence:count(n.manifest_sequence,100000000),manifest_received_at_ms:timestamp(n.manifest_received_at_ms),
   manifest_expires_at_ms:timestamp(n.manifest_expires_at_ms),
   pairing_status:enumOf(n.pairing_status,pairing,"not_recorded"),
   wake_attempts:{total_recorded:count(w.total_recorded),ready:count(w.ready),failed:count(w.failed),
    timed_out:count(w.timed_out),in_progress:count(w.in_progress),
    last_recorded_state:enumOf(w.last_recorded_state,wakeStates,"not_recorded")},
   inference_receipts:{total_recorded:count(f.total_recorded),succeeded:count(f.succeeded),
    failed:count(f.failed),unknown:count(f.unknown),executing:count(f.executing)},
   operating_system_service_state:service?service.state:"not_collected",
   local_service_observation:service,
   backend_readiness:safeReadiness,
   evidence_limit:"service manager state only for actual local Node; remote services and end-to-end health unverified"};
 }
 function prepare(sources){
  if(!sources||!sources.workspace)throw Error("Workspace scope and preview required");
  const files={"workspace.json":workspace(sources.workspace)};
  if(sources.browser)files["browser.json"]=browser(sources.browser);
  if(sources.model)files["agent-check.json"]=model(sources.model);
  if(sources.node)files["node.json"]=node(sources.node);
  const names=Object.keys(files).sort();
  const manifest={schema_version:1,product:"OnePane",format:"reviewed_local_qa_support_zip",
   included_sources:names,
   excluded:["credentials and API keys","raw user and model text","raw Workspace/Task identifiers",
    "raw logs, process details and network endpoints","model weights and private Project files",
    "unverified health claims, full installer logs and screenshots"],
   notes:"All content separately projected through fixed allowlists. No upload or remote collection."};
  files["manifest.json"]=manifest;
  const json=JSON.stringify({manifest,sources:files},null,2);
  if(new TextEncoder().encode(json).length>MAX_PREVIEW)throw Error("Combined reviewed preview exceeds size limit");
  return {json,files};
 }
 function crc32(data){
  let crc=0xffffffff;
  for(const n of data){crc^=n;for(let i=0;i<8;i++)crc=crc&1?(crc>>>1)^0xedb88320:crc>>>1}
  return (crc^0xffffffff)>>>0;
 }
 function zip(reviewed){
  if(!reviewed||!reviewed.files||typeof reviewed.json!=="string")throw Error("Review required");
  const encoder=new TextEncoder(),entries=[];
  const expected=JSON.stringify({manifest:reviewed.files["manifest.json"],sources:reviewed.files},null,2);
  if(reviewed.json!==expected)throw Error("Reviewed content changed");
  for(const name of Object.keys(reviewed.files).sort()){
   if(!["workspace.json","browser.json","agent-check.json","node.json","manifest.json"].includes(name))
    throw Error("Unsupported QA bundle entry");
   const filename=encoder.encode(name),content=encoder.encode(JSON.stringify(reviewed.files[name],null,2)+"\n");
   if(content.length>128*1024)throw Error("Source exceeds per-file limit");
   entries.push({filename,content,checksum:crc32(content)});
  }
  let localLength=0,centralLength=0;
  for(const e of entries){localLength+=30+e.filename.length+e.content.length;centralLength+=46+e.filename.length}
  const size=localLength+centralLength+22;
  if(size>MAX_ZIP)throw Error("Combined ZIP exceeds 256 KiB cap");
  const data=new Uint8Array(size);const v=new DataView(data.buffer);
  let offset=0;const directory=[];
  for(const e of entries){
   const at=offset;
   v.setUint32(offset,0x04034b50,true);v.setUint16(offset+4,20,true);
   v.setUint16(offset+6,0,true);v.setUint16(offset+8,0,true);
   v.setUint32(offset+14,e.checksum,true);
   v.setUint32(offset+18,e.content.length,true);v.setUint32(offset+22,e.content.length,true);
   v.setUint16(offset+26,e.filename.length,true);
   offset+=30;data.set(e.filename,offset);offset+=e.filename.length;
   data.set(e.content,offset);offset+=e.content.length;
   directory.push({entry:e,at});
  }
  const directoryAt=offset;
  for(const {entry:e,at} of directory){
   v.setUint32(offset,0x02014b50,true);v.setUint16(offset+4,20,true);v.setUint16(offset+6,20,true);
   v.setUint32(offset+16,e.checksum,true);
   v.setUint32(offset+20,e.content.length,true);v.setUint32(offset+24,e.content.length,true);
   v.setUint16(offset+28,e.filename.length,true);v.setUint32(offset+42,at,true);
   offset+=46;data.set(e.filename,offset);offset+=e.filename.length;
  }
  v.setUint32(offset,0x06054b50,true);v.setUint16(offset+8,entries.length,true);
  v.setUint16(offset+10,entries.length,true);v.setUint32(offset+12,offset-directoryAt,true);
  v.setUint32(offset+16,directoryAt,true);
  if(offset+22!==size)throw Error("ZIP layout mismatch");
  return data;
 }
 const api={prepare,zip,workspace,browser,model,node};
 root.a56SupportBundle=api;
 if(typeof module==="object"&&module&&module.exports)module.exports=api;
})(typeof globalThis!=="undefined"?globalThis:this);
