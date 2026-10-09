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
  <div id="a49TaskList"></div>
 </section>`;
 let taskRows=[];
 const doneStates=new Set(["complete","failed","cancelled","blocked"]);
 const paintTasks=()=>{
  if(!section.isConnected)return;
  const choice=section.querySelector("#a49TaskFilter")?.value||"all";
  const rows=taskRows.filter(t=>choice==="all"||(choice==="finished"?doneStates.has(t.state):!doneStates.has(t.state)));
  const count=section.querySelector("#a49TaskCount"),list=section.querySelector("#a49TaskList");
  if(!count||!list)return;
  count.textContent=rows.length+" shown · "+taskRows.length+" Workspace Tasks in the most recent tenant Task inventory";
  list.innerHTML=rows.length?rows.map(t=>{
   const updated=Number(t.updated_at)>0?new Date(Number(t.updated_at)).toLocaleString():"Unknown";
   return '<article class="a49-task-row panel-card"><div class="card-header"><strong>'+
    escapeHtml(t.objective||"Untitled objective")+'</strong><span class="pill">'+
    escapeHtml(t.state||"unknown")+'</span></div><div class="list-meta">Task '+
    escapeHtml(t.id)+" · "+escapeHtml(updated)+"</div></article>";
  }).join(""):'<div class="empty-state compact">No Workspace Tasks match this filter.</div>';
 };
 const loadTasks=async()=>{
  if(!section.isConnected)return;
  const count=section.querySelector("#a49TaskCount");
  if(count)count.textContent="Refreshing verified Task inventory…";
  try{
   const list=await apiRequest("/v1/tasks?workspace_id="+encodeURIComponent(onepaneWorkspace)+"&limit=200");
   if(!section.isConnected||section.dataset.workspaceId!==String(workspace.id))return;
   taskRows=(Array.isArray(list)?list:[]).filter(t=>t.project_id===project.id&&
    t.project_workspace_id===canonical.id).sort((a,b)=>Number(b.updated_at||0)-Number(a.updated_at||0)).slice(0,15);
   paintTasks();
  }catch(error){
   if(count)count.textContent="Task inventory unavailable: "+String(error.message||"Permission denied");
   const list=section.querySelector("#a49TaskList");
   if(list)list.innerHTML='<div class="error" role="alert">'+escapeHtml(error.message||"Unable to read Workspace Tasks")+'</div>';
  }
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
