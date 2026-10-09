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
   <p class="list-meta">First Debug Centre slice: a sanitised, bounded snapshot of this canonical Workspace's Task states, hard dependencies and last Worker checkpoints only. Not a full capture of logs, installer errors or Node internals.</p>
   <div class="a49-task-actions">
    <button class="btn" type="button" id="a49QAPreview">Review included data</button>
    <button class="btn" type="button" id="a49QADownload" disabled>Generate QA ZIP</button>
   </div>
   <p class="list-meta" id="a49QAStatus" role="status">Preview before downloading. No data is sent off-device.</p>
   <pre class="a49-qa-snapshot-preview" id="a49QAPreviewContent" aria-label="Redacted QA snapshot preview"></pre>
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
   const wait=t.state==="waiting_dependency"&&t.wait?.kind==="model_resources"?t.wait:null;
   const displayState=wait?"Waiting for local model":(t.state||"unknown");
   const retry=wait&&Number(wait.retry_at_ms)>0?
    '<div class="list-meta">Local-first retry: '+escapeHtml(new Date(Number(wait.retry_at_ms)).toLocaleString())+
    ' · Attempt '+escapeHtml(String(wait.attempt||1))+
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
 const qaPreview=section.querySelector("#a49QAPreview");
 const qaDownload=section.querySelector("#a49QADownload");
 const qaStatus=section.querySelector("#a49QAStatus");
 const qaPreviewContent=section.querySelector("#a49QAPreviewContent");
 const qaScope={workspace_id:onepaneWorkspace,project_id:project.id,project_workspace_id:canonical.id};
 const qaQuery=Object.entries(qaScope).map(([k,v])=>encodeURIComponent(k)+"="+encodeURIComponent(v)).join("&");
 let qaReviewed=false;
 qaPreview?.addEventListener("click",async()=>{
  qaReviewed=false;qaDownload.disabled=true;
  qaPreview.disabled=true;qaStatus.textContent="Loading permission-checked QA snapshot…";
  try{
   const snapshot=await apiRequest("/v1/qa/workspace-snapshot?"+qaQuery);
   if(!section.isConnected)return;
   qaPreviewContent.textContent=JSON.stringify(snapshot,null,2);
   qaStatus.textContent="Review the categories and Task metadata above before export. "+
    "Excluded: raw logs, prompts, model output, credentials, source code, private files and Node data. "+
    "Export creates a fresh snapshot; Task status may change between preview and download.";
   qaReviewed=true;qaDownload.disabled=false;
  }catch(err){
   qaPreviewContent.textContent="";
   qaStatus.textContent="QA snapshot unavailable: "+String(err.message||"Not authorised");
  }finally{qaPreview.disabled=false}
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
