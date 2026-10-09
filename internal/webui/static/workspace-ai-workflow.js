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
 <p class="list-meta">Queue an objective to the governed Project Task engine. OnePane selects a qualified model and available compute, favouring local resources; a slow or busy machine may delay completion. Research mode uses separately pinned models.</p>
 <form id="a49TaskForm" class="a49-task-form">
  <label>Development or research objective<textarea name="objective" required rows="3" maxlength="8000" placeholder="Create a world map generator and tests. Publish the verified map artifact into the Project Library."></textarea></label>
  <div class="a49-task-actions">
   <label>Execution priority<select name="priority"><option value="10">Normal</option><option value="20">High</option><option value="5">Background</option></select></label>
   <button type="submit" class="btn primary">Queue governed Task</button>
  </div><div id="a49TaskStatus" role="status"></div>
 </form>`;
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
    completion:{type:"operator_review"}
   })});
   status.innerHTML='<span class="good">Queued '+escapeHtml(created.id||"Task")+' for this Workspace. Review progress in Tasks and Inspector.</span>';
   form.querySelector('[name="objective"]').value="";
  }catch(err){status.innerHTML='<span class="error">'+escapeHtml(err.message)+'</span>'}
  finally{button.disabled=false}
 });
}
