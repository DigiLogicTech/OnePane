/* Local-first governed Workspace development jobs.
 * Submit Tasks to the durable scheduler; never call a model or host shell
 * directly, and never interpret queued as completed work.
 */
async function a49MountDevelopmentTasks(project,workspace,container){
 if(!container.isConnected)return;
 const section=document.createElement("section");
 section.className="a49-workflow a44-environment-subsection";
 section.dataset.workspaceId=String(workspace.id);
 section.innerHTML='<h3>Workspace Tasks & AI development</h3><p class="list-meta">Resolving Project Workspace identity…</p>';
 container.append(section);
 let canonical=null;
 try{
  const views=await apiRequest("/v1/projects/"+encodeURIComponent(project.id)+"/workspaces");
  canonical=(Array.isArray(views)?views:[]).find(w=>a45BackendLegacyID(w)===String(workspace.id));
 }catch(e){section.innerHTML='<h3>Workspace Tasks</h3><div class="error" role="alert">'+escapeHtml(e.message)+'</div>';return}
 if(!container.isConnected)return;
 if(!canonical){
  section.innerHTML='<h3>Workspace Tasks</h3><p class="list-meta">Register the Workspace before assigning it autonomous jobs.</p>';
  return;
 }
 section.innerHTML=`<h3>Workspace Tasks & AI development</h3>
 <p class="list-meta">Queue an objective to the governed Project Task engine. New Workspace Tasks run local-first by default: OnePane uses qualified local compute even when slower. Cloud routing requires an explicit choice. Research Council seats are separately pinned.</p>
 <form id="a49TaskForm" class="a49-task-form">
  <label>Development or research objective<textarea name="objective" required rows="3" maxlength="8000" placeholder="Create a world map generator and tests. Publish the verified map artifact into the Project Library."></textarea></label>
  <div class="a49-task-actions">
   <label>Execution priority<select name="priority"><option value="10">Normal</option><option value="20">High</option><option value="5">Background</option></select></label>
   <label><input type="checkbox" name="allow_remote" value="yes"> Allow approved cloud models for this Task</label>
   <button type="submit" class="btn primary">Queue governed Task</button>
  </div><div id="a49TaskStatus" role="status"></div>
 </form>
 <section class="panel-card a49-task-queue">
  <div class="card-header"><div><h4>Workspace Task queue</h4>
   <p class="list-meta">Observed Task states from the OnePane scheduler; queueing does not mean execution completed.</p></div>
   <button type="button" class="btn" id="a49RefreshTasks">Refresh</button></div>
  <label>Show <select id="a49TaskFilter" aria-label="Filter Workspace Tasks">
   <option value="all">All</option><option value="active">Active and waiting</option>
   <option value="finished">Completed, blocked or failed</option></select></label>
  <p class="list-meta" id="a49TaskCount" role="status">Loading Workspace Tasks…</p>
  <p class="list-meta" id="a49OutputStatus" role="status"></p>
  <div id="a49PublicationReviews" role="status"></div>
  <div id="a49TaskList"></div>
  <details class="a49-qa-snapshot" id="a49QASnapshot">
   <summary>QA diagnostic snapshot (read-only, opt-in)</summary>
   <p class="list-meta">A limited sanitised snapshot of this canonical Workspace's Tasks, hard dependencies, Worker checkpoints and existing event metadata. Not a capture of raw logs, installer errors or Node internals.</p>
   <div class="a49-task-actions">
    <button class="btn" type="button" id="a49QAPreview">Review included data</button>
    <button class="btn" type="button" id="a49QADownload" disabled>Generate QA ZIP</button>
    <button class="btn" type="button" id="a49QACopySummary" disabled>Copy sanitized QA summary</button>
   </div>
   <p class="list-meta" id="a49QAStatus" role="status">Preview before downloading. No data is sent off-device. Timeline includes only known Task/Worker event kinds, timestamps and pseudonymous correlation references.</p>
   <p class="list-meta">Copyable QA summary (build, observed state counts and coverage only; Task objectives and identities omitted):</p>
   <textarea id="a49QASummaryContent" rows="8" readonly aria-label="Sanitized QA summary" class="a49-qa-summary"></textarea>
   <pre class="a49-qa-snapshot-preview" id="a49QAPreviewContent" aria-label="Redacted QA snapshot preview"></pre>
  </details>
  <details class="a49-qa-snapshot" id="a59ScopedTraceCorrelation">
   <summary>Correlate observed Task and Worker events (authorised Workspace only)</summary>
   <p class="list-meta">Read the latest 96 bounded, already scoped Task/Worker events. Match only observed opaque trace/request references within the same canonical Workspace and Task. Related persisted model requests, Tool invocations, operations and verification statuses can be joined by the same authorised Task identity. These are not end-to-end trace links or proof of external success. Only tagged Task creation carries a server-minted HTTP trace. Asynchronous Task → Attempt → Worker links are checked by persisted database identity, not a propagated HTTP header. Stored assurance probe integrity and freshness are rechecked; historical evidence is NOT a newly executed external readback.</p>
   <div class="a49-task-actions">
    <button class="btn" id="a59Correlate" type="button">Load and correlate observed events</button>
    <label><input type="checkbox" id="a63IncludeCapture"> Include my Node Admin-owned HTTP capture (separate permission check)</label>
    <label>Severity <select id="a59Severity" aria-label="Observed event severity filter">
     <option value="all">All</option><option value="attention">Attention</option>
     <option value="waiting">Waiting</option><option value="information">Information</option>
    </select></label>
   </div>
   <p class="list-meta" id="a59CorrelationStatus" role="status">Not loaded. No diagnostics fetched automatically. The optional HTTP capture is only accessible to the administrator credential that started it.</p>
   <pre class="a49-qa-snapshot-preview a59-correlation-preview" id="a59CorrelationPreview" aria-label="Scoped Task Worker correlation" aria-live="polite"></pre>
  </details>
  <details class="a49-qa-snapshot" id="a54IncidentCapture">
   <summary>Browser incident capture (opt-in, maximum 10 minutes)</summary>
   <p class="list-meta">Captures only generic OnePane route/button/form actions, broad API subsystems, HTTP status and duration, plus counts of browser errors. No typed text, URL paths, tokens, request bodies, model outputs or error messages. Captures across this browser tab's OnePane pages while enabled. Data stays in memory and is lost on reload.</p>
   <div class="a49-task-actions">
    <button class="btn" type="button" id="a54CaptureStart">Start capture</button>
    <button class="btn" type="button" id="a54CaptureMark">Mark issue</button>
    <button class="btn" type="button" id="a54CaptureStop">Stop capture</button>
    <button class="btn" type="button" id="a54CapturePreview">Review incident trace</button>
    <button class="btn" type="button" id="a54CaptureDownload" disabled>Download reviewed JSON</button>
    <button class="btn" type="button" id="a54CaptureClear">Clear capture</button>
   </div>
   <p class="list-meta" id="a54CaptureStatus" role="status">Capture is off by default. This JSON is separate from the Workspace QA ZIP; nothing is automatically uploaded.</p>
   <pre class="a49-qa-snapshot-preview" id="a54CaptureContent" aria-label="Sanitized browser incident trace preview"></pre>
  </details>
  <details class="a49-qa-snapshot" id="a56SupportBundle">
   <summary>Consolidated QA support ZIP (review all data before export)</summary>
   <p class="list-meta">Create one bounded local ZIP from a fresh authorised Workspace summary, plus explicitly selected browser, model and Node evidence. Every source is re-projected through strict field allowlists; no raw Task identifiers, logs, prompts, model outputs, secrets, credentials or external upload.</p>
   <div class="a49-task-actions">
    <label><input type="checkbox" id="a56IncludeBrowser"> Include completed opt-in browser incident capture</label>
    <label><input type="checkbox" id="a56IncludeModel"> Include authorised Agent Check
     <input id="a56ModelDeployment" type="text" maxlength="192" placeholder="Model deployment ID (from Local Models)" aria-label="Authorised model deployment ID"></label>
    <label><input type="checkbox" id="a56IncludeNode"> Include Node administrator evidence
     <input id="a56NodeID" type="text" maxlength="192" placeholder="Registered Node ID (from Nodes)" aria-label="Node ID"></label>
   </div>
   <div class="a49-task-actions">
    <button class="btn" type="button" id="a56Review">Review consolidated data</button>
    <button class="btn" type="button" id="a56Export" disabled>Download reviewed support ZIP</button>
    <button class="btn" type="button" id="a56Discard">Discard review</button>
   </div>
   <p class="list-meta" id="a56Status" role="status">No capture or upload starts automatically. Optional model and Node reads require their own permissions. If any requested source is denied, the combined review fails rather than silently omitting it.</p>
   <pre class="a49-qa-snapshot-preview" id="a56Preview" aria-label="Complete sanitised consolidated support bundle preview"></pre>
  </details>
 </section>`;
 let taskRows=[],publishedOutputs=[];
 const doneStates=new Set(["complete","failed","cancelled","blocked"]);
 const paintTasks=()=>{
  if(!section.isConnected)return;
  const choice=section.querySelector("#a49TaskFilter")?.value||"all";
  const rows=taskRows.filter(t=>choice==="all"||(choice==="finished"?doneStates.has(t.state):!doneStates.has(t.state)));
  const count=section.querySelector("#a49TaskCount"),list=section.querySelector("#a49TaskList");
  if(!count||!list)return;
  count.textContent=rows.length+" shown · "+taskRows.length+" recent Tasks in this Project Workspace (up to 15)";
  list.innerHTML=rows.length?rows.map(t=>{
   const updated=Number(t.updated_at)>0?new Date(Number(t.updated_at)).toLocaleString():"Unknown";
   const kind=t.state==="waiting_dependency"?t.wait?.kind:null;
   const wait=kind==="model_resources"||kind==="toolchain_resources"?t.wait:null;
   const waitingToolchain=kind==="toolchain_resources";
   const displayState=waitingToolchain?"Waiting for approved toolchain":(wait?"Waiting for local model":(t.state||"unknown"));
   const retry=wait&&Number.isFinite(Number(wait.retry_at_ms))&&Number(wait.retry_at_ms)>0?
    '<div class="list-meta">'+(waitingToolchain?'Next registered-runtime check: ':'Local-first retry: ')+
    escapeHtml(new Date(Number(wait.retry_at_ms)).toLocaleString())+
    (waitingToolchain?' · Existing Attempt preserved; OCI isolation and tools are verified before any build.':
     ' · Attempt '+escapeHtml(String(wait.attempt||1)))+
    (wait.reason?' · '+escapeHtml(wait.reason):'')+'</div>':'';
   const dep=t.dependencies&&typeof t.dependencies==="object"?t.dependencies:null;
   const validDep=dep&&["total","completed","failed","blocked","restricted","remaining"].every(k=>
    Number.isSafeInteger(Number(dep[k]))&&Number(dep[k])>=0);
   const prerequisites=validDep&&Number(dep.total)>0?
    '<div class="a49-task-prerequisites" aria-label="Durable Task dependencies">'+
     '<strong>Hard prerequisites: '+Number(dep.completed)+' / '+Number(dep.total)+' complete</strong>'+
     '<span class="list-meta">'+Number(dep.remaining)+' not complete · '+
      Number(dep.failed)+' failed/cancelled · '+Number(dep.blocked)+' blocked'+
      (Number(dep.restricted)>0?' · '+Number(dep.restricted)+' restricted Workspace links':'')+
     '</span>'+
     (Number(dep.failed)>0||Number(dep.blocked)>0?
      '<span class="list-meta a49-checkpoint-review">A prerequisite failed or requires review. The parent cannot safely complete until its dependencies are resolved.</span>':'')+
     (Number(dep.restricted)>0?
      '<span class="list-meta">Other Workspace prerequisite identities and states are not disclosed here.</span>':'')+
    '</div>':'';
   const x=t.execution&&typeof t.execution==="object"?t.execution:null;
   const checkpoint=x&&Number.isSafeInteger(Number(x.steps_used))&&
    Number.isSafeInteger(Number(x.max_steps))&&Number(x.max_steps)>0?
    '<div class="a49-task-checkpoint" aria-label="Persisted execution checkpoint">'+
     '<strong>Worker: '+escapeHtml(String(x.status||"unavailable"))+'</strong>'+
     '<span class="list-meta">Journalled steps '+Number(x.steps_used)+' / '+Number(x.max_steps)+
      (x.last_step_kind?' · Last '+escapeHtml(String(x.last_step_kind))+
       ' ('+escapeHtml(String(x.last_step_status||"unobserved"))+')':'')+
      ' · '+escapeHtml(Number(x.updated_at)>0?new Date(Number(x.updated_at)).toLocaleString():"Unknown time")+
     '</span>'+
     (x.review_required?'<span class="list-meta a49-checkpoint-review">Interrupted or uncertain execution: review Task evidence before any retry. External actions must not replay automatically.</span>':'')+
    '</div>':'';
   const outputs=publishedOutputs.filter(o=>o.task_id===t.id);
   const published=outputs.length?'<div class="a49-published-outputs"><div class="list-meta">Verified Project Library outputs</div>'+
    outputs.map(o=>'<div class="a49-published-output"><span>'+
      escapeHtml(o.relative_path)+' · v'+Number(o.version)+' · '+Number(o.size_bytes)+' bytes</span>'+
      '<a class="btn" title="Workspace read access is verified at download time" href="/v1/projects/'+
      encodeURIComponent(project.id)+'/library/'+encodeURIComponent(o.asset_id)+'/versions/'+
      Number(o.version)+'/content?workspace_id='+encodeURIComponent(canonical.id)+
      '">Download</a></div>').join("")+'</div>':'';
   return '<article class="a49-task-row panel-card"><div class="card-header"><strong>'+
    escapeHtml(t.objective||"Untitled objective")+'</strong><span class="pill">'+
    escapeHtml(displayState)+'</span></div><div class="list-meta">Task '+
    escapeHtml(t.id)+" · "+escapeHtml(updated)+"</div>"+retry+prerequisites+checkpoint+published+"</article>";
  }).join(""):'<div class="empty-state compact">No Workspace Tasks match this filter.</div>';
 };
 const loadTasks=async()=>{
  if(!section.isConnected)return;
  const count=section.querySelector("#a49TaskCount");
  if(count)count.textContent="Refreshing verified Task inventory…";
  try{
   const list=await apiRequest("/v1/tasks?workspace_id="+encodeURIComponent(onepaneWorkspace)+
    "&project_id="+encodeURIComponent(project.id)+
    "&project_workspace_id="+encodeURIComponent(canonical.id)+"&limit=15");
   if(!section.isConnected||section.dataset.workspaceId!==String(workspace.id))return;
   taskRows=(Array.isArray(list)?list:[]).filter(t=>t.project_id===project.id&&
    t.project_workspace_id===canonical.id).sort((a,b)=>Number(b.updated_at||0)-Number(a.updated_at||0)).slice(0,15);
   const outputStatus=section.querySelector("#a49OutputStatus");
   try{
    const results=await apiRequest("/v1/projects/"+encodeURIComponent(project.id)+
     "/workspaces/"+encodeURIComponent(canonical.id)+"/published-outputs");
    if(!section.isConnected||section.dataset.workspaceId!==String(workspace.id))return;
    publishedOutputs=(Array.isArray(results)?results:[]).filter(o=>
     o&&typeof o.task_id==="string"&&typeof o.asset_id==="string"&&
     Number.isSafeInteger(Number(o.version))&&Number(o.version)>0);
    if(outputStatus)outputStatus.textContent=publishedOutputs.length+
     " verified, currently authorised Task outputs in recent publication history";
   }catch(error){
    publishedOutputs=[];
    if(outputStatus)outputStatus.textContent="Published output inventory unavailable: "+
     String(error.message||"Permission denied");
   }
   const reviewHost=section.querySelector("#a49PublicationReviews");
   try{
    const reviews=await apiRequest("/v1/projects/"+encodeURIComponent(project.id)+
     "/workspaces/"+encodeURIComponent(canonical.id)+"/publication-reviews");
    if(!section.isConnected||section.dataset.workspaceId!==String(workspace.id))return;
    const pending=Array.isArray(reviews)?reviews:[];
    if(reviewHost)reviewHost.innerHTML=pending.length?
     '<section class="a49-publication-review" aria-label="Publication recovery review">'+
     '<strong>Publication review needed</strong>'+
     '<p class="list-meta">These records have been incomplete for at least five minutes. A write may still be in progress or its outcome may be unknown. Inspect Task evidence and logs before any retry.</p>'+
     pending.map(v=>'<div class="a49-publication-review-row"><strong>'+
       escapeHtml(v.relative_path)+'</strong><span>Task '+escapeHtml(v.task_id)+
       ' · '+escapeHtml(v.stage==="artifact_recorded"?"Managed artifact recorded":"Reserved; artifact status unknown")+
       ' · Last update '+escapeHtml(Number(v.last_updated_at)>0?
        new Date(Number(v.last_updated_at)).toLocaleString():"Unknown")+
       '</span></div>').join("")+'</section>':"";
   }catch(error){
    if(reviewHost)reviewHost.textContent="Publication review status unavailable: "+
     String(error.message||"Permission denied");
   }
   paintTasks();
  }catch(error){
   if(count)count.textContent="Task inventory unavailable: "+String(error.message||"Permission denied");
   const list=section.querySelector("#a49TaskList");
   if(list)list.innerHTML='<div class="error" role="alert">'+escapeHtml(error.message||"Unable to read Workspace Tasks")+'</div>';
  }
 };
 // Opt-in, scope-preserving Task ↔ Worker linkage. This is a derived view
 // of the SAME authenticated canonical Workspace snapshot, never a new API.
 const correlationLoad=section.querySelector("#a59Correlate");
 const correlationSeverity=section.querySelector("#a59Severity");
 const correlationStatus=section.querySelector("#a59CorrelationStatus");
 const correlationPreview=section.querySelector("#a59CorrelationPreview");
 const correlationCapture=section.querySelector("#a63IncludeCapture");
 let executionReport=null;
 let correlationReport=null,correlationEpoch=0;
 const paintCorrelation=()=>{
  if(!section.isConnected)return;
  if(!correlationReport||typeof a59ScopedCorrelation==="undefined"){
   correlationPreview.textContent="";return;
  }
  correlationPreview.textContent=a59ScopedCorrelation.format(
   correlationReport,correlationSeverity.value);
  if(executionReport&&typeof a63ExecutionProvenance!=="undefined")
   correlationPreview.textContent+="\n"+a63ExecutionProvenance.format(executionReport);
 };
 correlationSeverity.addEventListener("change",paintCorrelation);
 correlationLoad.addEventListener("click",async()=>{
  const epoch=++correlationEpoch;
  correlationReport=null;executionReport=null;correlationPreview.textContent="";
  correlationLoad.disabled=true;
  correlationStatus.textContent="Loading a fresh, permission-checked canonical Workspace QA timeline…";
  try{
   const snapshot=await apiRequest("/v1/qa/workspace-snapshot?"+qaQuery,{cache:"no-store"});
   if(!section.isConnected||epoch!==correlationEpoch)return;
   if(typeof a59ScopedCorrelation==="undefined")throw Error("Unavailable");
   if(typeof a63ExecutionProvenance==="undefined")throw Error("Provenance projection unavailable");
   // The optional capture requires its own separate Node Admin permission,
   // and is never silently fetched or combined with another Workspace.
   const capture=correlationCapture.checked?
    await apiRequest("/v1/qa/api-capture",{cache:"no-store"}):undefined;
   if(!section.isConnected||epoch!==correlationEpoch)return;
   correlationReport=a59ScopedCorrelation.correlate(snapshot);
   executionReport=a63ExecutionProvenance.project(snapshot,capture);
   paintCorrelation();
   correlationStatus.textContent="Correlated "+correlationReport.groups_shown+
    " scoped groups from "+correlationReport.events_considered+" recent events"+
    (correlationReport.timeline_truncated?" (older chronology omitted)":"")+
    ". Shared refs and persisted execution statuses are observations, not proof of external tool effects, model correctness or independent verification.";
  }catch(_){
   if(!section.isConnected||epoch!==correlationEpoch)return;
   correlationReport=null;correlationPreview.textContent="";
   correlationStatus.textContent="Scoped correlation unavailable or access denied. No events have been shown.";
  }finally{if(section.isConnected&&epoch===correlationEpoch)correlationLoad.disabled=false}
 });
 const qaPreview=section.querySelector("#a49QAPreview");
 const qaDownload=section.querySelector("#a49QADownload");
 const qaCopySummary=section.querySelector("#a49QACopySummary");
 const qaSummaryContent=section.querySelector("#a49QASummaryContent");
 const qaStatus=section.querySelector("#a49QAStatus");
 const qaPreviewContent=section.querySelector("#a49QAPreviewContent");
 const qaScope={workspace_id:onepaneWorkspace,project_id:project.id,project_workspace_id:canonical.id};
 const qaQuery=Object.entries(qaScope).map(([k,v])=>encodeURIComponent(k)+"="+encodeURIComponent(v)).join("&");
 let qaReviewed=false;
 qaPreview?.addEventListener("click",async()=>{
  qaReviewed=false;qaDownload.disabled=true;qaCopySummary.disabled=true;
  qaSummaryContent.value="";
  qaPreview.disabled=true;qaStatus.textContent="Loading permission-checked QA snapshot…";
  try{
   const snapshot=await apiRequest("/v1/qa/workspace-snapshot?"+qaQuery);
   if(!section.isConnected)return;
   qaPreviewContent.textContent=JSON.stringify(snapshot,null,2);
   if(typeof a52MakeQASummary==="function"){
    qaSummaryContent.value=a52MakeQASummary(snapshot);
    qaCopySummary.disabled=false;
   }
   const timelineCount=Number(snapshot.captured_timeline_events||0);
   const timelineLimit=Number(snapshot.max_timeline_events||0);
   const chronologyLabel="Scoped Task/Worker event chronology: "+timelineCount+
    " of up to "+timelineLimit+" recent entries"+
    (snapshot.timeline_truncated?" (older events omitted)":"")+". ";
   qaStatus.textContent=chronologyLabel+"Review the categories and Task metadata above before export. "+
    "Excluded: raw logs, prompts, model output, credentials, source code, private files and Node data. "+
    "Export creates a fresh snapshot; Task status may change between preview and download.";
   qaReviewed=true;qaDownload.disabled=false;
  }catch(err){
   qaPreviewContent.textContent="";
   qaSummaryContent.value="";qaCopySummary.disabled=true;
   qaStatus.textContent="QA snapshot unavailable: "+String(err.message||"Not authorised");
  }finally{qaPreview.disabled=false}
 });
 qaCopySummary?.addEventListener("click",async()=>{
  if(!qaReviewed||qaCopySummary.disabled||!qaSummaryContent.value)return;
  try{
   if(!navigator.clipboard?.writeText)throw Error("Clipboard API unavailable");
   await navigator.clipboard.writeText(qaSummaryContent.value);
   qaStatus.textContent="Sanitized QA summary copied. It contains counts and build metadata only; attach the ZIP separately if useful.";
  }catch(error){
   // Manual copy still works on offline and restricted browser environments.
   qaSummaryContent.focus();qaSummaryContent.select();
   qaStatus.textContent="Automatic clipboard unavailable. The sanitized summary is selected; use Copy manually.";
  }
 });
 qaDownload?.addEventListener("click",async()=>{
  if(!qaReviewed||!section.isConnected)return;
  qaDownload.disabled=true;qaStatus.textContent="Generating local QA ZIP (no upload)…";
  try{
   const response=await fetch("/v1/qa/workspace-bundle",{
    method:"POST",credentials:"same-origin",
    headers:{"Content-Type":"application/json","X-OnePane-CSRF":csrfCookie()},
    body:JSON.stringify(qaScope)
   });
   if(!response.ok){
    let message="HTTP "+response.status;
    try{const details=await response.json();message=details.error||details.message||message}catch{}
    throw Error(message);
   }
   const payload=await response.blob();
   if(payload.size>131072||payload.size===0)throw Error("QA ZIP size is outside safe bounds");
   const url=URL.createObjectURL(payload);
   try{
    const link=document.createElement("a");
    link.href=url;link.download="onepane-workspace-qa-snapshot.zip";
    link.style.display="none";document.body.appendChild(link);link.click();link.remove();
   }finally{URL.revokeObjectURL(url)}
   qaStatus.textContent="QA ZIP created locally. Inspect its contents before sharing; this is an intentionally limited Task snapshot.";
  }catch(err){qaStatus.textContent="QA export failed: "+String(err.message||"Unavailable")}
  finally{qaDownload.disabled=false}
 });
 // Browser-only incident capture never sends observations back through APIs.
 // The recorder is global to this browser tab, so route changes do not end it;
 // the operator's Stop/Clear or the ten-minute expiry does.
 const incident=typeof a54QACapture!=="undefined"?a54QACapture:null;
 const incidentActions={
  start:section.querySelector("#a54CaptureStart"),
  mark:section.querySelector("#a54CaptureMark"),
  stop:section.querySelector("#a54CaptureStop"),
  preview:section.querySelector("#a54CapturePreview"),
  download:section.querySelector("#a54CaptureDownload"),
  clear:section.querySelector("#a54CaptureClear")
 };
 const incidentStatus=section.querySelector("#a54CaptureStatus");
 const incidentContent=section.querySelector("#a54CaptureContent");
 let incidentReviewedJSON="";
 const paintIncident=()=>{
  if(!section.isConnected||!incident)return;
  const state=incident.snapshot();
  const active=state.status==="recording";
  incidentActions.start.disabled=active;
  incidentActions.mark.disabled=!active;
  incidentActions.stop.disabled=!active;
  incidentActions.preview.disabled=active||!state.events.length;
  incidentActions.download.disabled=active||!incidentReviewedJSON;
  incidentActions.clear.disabled=state.status==="idle";
  incidentStatus.textContent="Browser capture: "+state.status+
   " · "+state.events.length+" / "+state.max_events+" recent events"+
   (state.dropped_events?" · "+state.dropped_events+" older events discarded":"")+
   " · raw URLs, bodies, typed inputs, prompts and error messages excluded.";
 };
 if(incident){
  incidentActions.start.onclick=()=>{
   incidentReviewedJSON="";incidentContent.textContent="";
   incident.start();paintIncident();
  };
  incidentActions.mark.onclick=()=>{incident.mark();paintIncident()};
  incidentActions.stop.onclick=()=>{
   incident.stop();incidentReviewedJSON="";incidentContent.textContent="";paintIncident();
  };
  incidentActions.preview.onclick=()=>{
   const snapshot=incident.snapshot();
   if(snapshot.status==="recording"||!snapshot.events.length)return;
   incidentReviewedJSON=JSON.stringify(snapshot,null,2);
   incidentContent.textContent=incidentReviewedJSON;
   paintIncident();
  };
  incidentActions.download.onclick=()=>{
   if(!incidentReviewedJSON||incident.snapshot().status==="recording")return;
   // Export exactly what was previewed. A changed session requires review again.
   if(JSON.stringify(incident.snapshot(),null,2)!==incidentReviewedJSON){
    incidentReviewedJSON="";
    incidentStatus.textContent="Capture changed. Review the trace again before downloading.";
    paintIncident();return;
   }
   const blob=new Blob([incidentReviewedJSON],{type:"application/json"});
   if(blob.size>65536){incidentStatus.textContent="Capture too large to export safely.";return}
   const url=URL.createObjectURL(blob);
   try{
    const link=document.createElement("a");
    link.href=url;link.download="onepane-browser-incident.json";
    link.style.display="none";document.body.appendChild(link);link.click();link.remove();
   }finally{URL.revokeObjectURL(url)}
   incidentStatus.textContent="Sanitized incident trace downloaded locally. Review before sharing.";
  };
  incidentActions.clear.onclick=()=>{
   incident.clear();incidentReviewedJSON="";incidentContent.textContent="";paintIncident();
  };
  paintIncident();
 }else{
  for(const button of Object.values(incidentActions))if(button)button.disabled=true;
  incidentStatus.textContent="Browser incident recorder unavailable in this build.";
 }
 // Client-local reviewed multi-source QA bundle. Optional source IDs are
 // input selectors only and are NEVER exported; each request reauthorises.
 const supportRoot=section.querySelector("#a56SupportBundle");
 const supportReview=section.querySelector("#a56Review");
 const supportExport=section.querySelector("#a56Export");
 const supportDiscard=section.querySelector("#a56Discard");
 const supportStatus=section.querySelector("#a56Status");
 const supportPreview=section.querySelector("#a56Preview");
 let supportReviewed=null;
 // Invalidates asynchronous review and export when a selector changes or
 // the user discards their review; stale requests must never restore it.
 let supportEpoch=0;
 const supportOptions=()=>({
  browser:section.querySelector("#a56IncludeBrowser").checked,
  model:section.querySelector("#a56IncludeModel").checked,
  modelID:section.querySelector("#a56ModelDeployment").value.trim(),
  node:section.querySelector("#a56IncludeNode").checked,
  nodeID:section.querySelector("#a56NodeID").value.trim()
 });
 const supportSignature=()=>JSON.stringify(supportOptions());
 const discardSupport=()=>{
  supportEpoch++;
  supportReviewed=null;supportExport.disabled=true;
  supportPreview.textContent="";
 };
 // All selected backend permissions are checked again at export, not only at
 // preview. No new data enters the ZIP: only the exact prepared preview does.
 // This requires a live authenticated backend; an offline/revoked session fails closed.
 const reauthorizeSupportSources=async selected=>{
  // Bypass browser HTTP cache, otherwise a cached 200 might mask revocation.
  const liveOnly={cache:"no-store"};
  await apiRequest("/v1/qa/workspace-snapshot?"+qaQuery,liveOnly);
  if(selected.model)await apiRequest("/v1/qa/model-deployments/"+
   encodeURIComponent(selected.modelID)+"/agent-check",liveOnly);
  if(selected.node)await apiRequest("/v1/qa/nodes/"+
   encodeURIComponent(selected.nodeID)+"/evidence",liveOnly);
 };
 const supportReviewStillValid=(reviewed,epoch)=>Boolean(
  reviewed&&supportReviewed===reviewed&&supportEpoch===epoch&&section.isConnected&&
  reviewed.signature===supportSignature()&&
  Date.now()>=reviewed.reviewedAt&&Date.now()-reviewed.reviewedAt<=120000&&
  supportPreview.textContent===reviewed.prepared.json&&
  (reviewed.browserFingerprint===null||
   (incident&&JSON.stringify(incident.snapshot())===reviewed.browserFingerprint))
 );
 supportDiscard.onclick=()=>{
  discardSupport();
  supportStatus.textContent="Reviewed support evidence discarded from this panel. No files uploaded.";
 };
 for(const field of supportRoot.querySelectorAll("input")){
  field.addEventListener("change",discardSupport);
  field.addEventListener("input",discardSupport);
 }
 supportReview.onclick=async()=>{
  discardSupport();
  const epoch=supportEpoch;
  supportReview.disabled=true;supportStatus.textContent="Checking access and sanitising selected QA sources…";
  const selected=supportOptions(),signature=supportSignature();
  try{
   if(typeof a56SupportBundle==="undefined")throw Error("Consolidated QA formatter unavailable");
   if(selected.model&&(!selected.modelID||selected.modelID.length>192)||
      selected.node&&(!selected.nodeID||selected.nodeID.length>192))
    throw Error("Select a registered model deployment or Node ID for each optional source");
   const sources={workspace:await apiRequest("/v1/qa/workspace-snapshot?"+qaQuery)};
   if(selected.browser){
    if(!incident)throw Error("Browser recorder unavailable");
    const observed=incident.snapshot();
    if(!["stopped","expired"].includes(observed.status)||!observed.events.length)
     throw Error("Stop and review a browser capture before including it");
    sources.browser=observed;
   }
   if(selected.model)sources.model=await apiRequest("/v1/qa/model-deployments/"+
      encodeURIComponent(selected.modelID)+"/agent-check");
   if(selected.node)sources.node=await apiRequest("/v1/qa/nodes/"+
      encodeURIComponent(selected.nodeID)+"/evidence");
   if(!section.isConnected||epoch!==supportEpoch||signature!==supportSignature())return;
   const prepared=a56SupportBundle.prepare(sources);
   supportPreview.textContent=prepared.json;
   supportReviewed={prepared,signature,reviewedAt:Date.now(),
    browserFingerprint:selected.browser?JSON.stringify(sources.browser):null};
   supportExport.disabled=false;
   supportStatus.textContent="Review every included field. Export uses exactly this preview, rechecks current source permissions without incorporating re-fetched data, never uploads and expires after two minutes. Missing source permissions fail closed.";
  }catch(_){
   if(epoch!==supportEpoch)return;
   discardSupport();
   supportStatus.textContent="Unable to create the reviewed bundle. Check Workspace read permissions, selected optional source IDs and Node Admin/model.read access. No partial export is available.";
  }finally{supportReview.disabled=false}
 };
 supportExport.onclick=async()=>{
  const reviewed=supportReviewed,epoch=supportEpoch;
  if(!reviewed||!section.isConnected)return;
  if(!supportReviewStillValid(reviewed,epoch)){
   discardSupport();
   supportStatus.textContent="Selected sources, browser capture, review or review expiry changed. Review again before export.";
   return;
  }
  supportExport.disabled=true;supportReview.disabled=true;
  supportStatus.textContent="Rechecking current Workspace, model and Node permissions before local export…";
  try{
   await reauthorizeSupportSources(supportOptions());
   // Selection, capture, review or permission state may change during the
   // asynchronous GETs. Never let a stale in-flight export download a ZIP.
   if(!supportReviewStillValid(reviewed,epoch)){
    if(epoch===supportEpoch){
     discardSupport();
     supportStatus.textContent="Review changed or expired during the access check. Review again.";
    }
    return;
   }
   const bytes=a56SupportBundle.zip(reviewed.prepared);
   if(bytes.byteLength>262144||!bytes.byteLength)throw Error("Invalid support ZIP size");
   const blob=new Blob([bytes],{type:"application/zip"});
   const url=URL.createObjectURL(blob);
   try{
    const link=document.createElement("a");
    link.href=url;link.download="onepane-reviewed-qa-support.zip";
    link.style.display="none";document.body.appendChild(link);link.click();link.remove();
   }finally{URL.revokeObjectURL(url)}
   supportStatus.textContent="Reviewed local support ZIP downloaded after fresh access checks. Inspect before sharing.";
   discardSupport();
  }catch(_){
   if(epoch===supportEpoch){
    discardSupport();
    supportStatus.textContent="Support export denied or unavailable. Permissions may have changed, the backend may be offline, or the review expired. Review again.";
   }
  }finally{supportReview.disabled=false}
 };
 section.querySelector("#a49RefreshTasks")?.addEventListener("click",loadTasks);
 section.querySelector("#a49TaskFilter")?.addEventListener("change",paintTasks);
 void loadTasks();
 const form=section.querySelector("#a49TaskForm");
 form.addEventListener("submit",async e=>{
  e.preventDefault();
  const values=Object.fromEntries(new FormData(form)),objective=String(values.objective||"").trim();
  if(!objective)return;
  const button=form.querySelector('button[type="submit"]'),status=form.querySelector("#a49TaskStatus");
  button.disabled=true;status.textContent="Submitting Task…";
  try{
   const created=await apiRequest("/v1/tasks",{method:"POST",body:JSON.stringify({
    workspace_id:onepaneWorkspace,project_id:project.id,project_workspace_id:canonical.id,
    objective,scheduling_class:"user_interactive",priority:Number(values.priority),
    completion:{type:"operator_review",onepane_routing:{
     project_workspace_id:canonical.id,
     workspace_access:{
      mode:"brokered",project_workspace_id:canonical.id,
      remote_models:values.allow_remote==="yes",
      filesystem:"workspace-only",internet:false,lan:false,
      browser:false,computer:false,secrets:"none"
     }
    }}
   })});
   status.innerHTML='<span class="good">Queued '+escapeHtml(created.id||"Task")+' for this Workspace. Review progress in Tasks and Inspector.</span>';
   form.querySelector('[name="objective"]').value="";
   await loadTasks();
  }catch(err){status.innerHTML='<span class="error">'+escapeHtml(err.message)+'</span>'}
  finally{button.disabled=false}
 });
}
