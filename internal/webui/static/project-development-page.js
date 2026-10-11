/* Project-first Workspace surface: Stage 1 of Development Environments RFC.
 * Displays observed, API-backed *Project-scoped* runtime state honestly.
 * Does NOT represent this legacy runtime as isolated to a Project Workspace.
 */
const a44RenderWorkspaceDashboard=renderWorkspaces;
const a44WorkspacePresentation=new Map();

function a44DevelopmentSection(label,value,detail){
 return `<div class="a44-environment-tile"><div class="list-meta">${escapeHtml(label)}</div><strong>${escapeHtml(value)}</strong><p class="list-meta">${escapeHtml(detail)}</p></div>`;
}
function a44RuntimeStatusLabel(status){
 return ({defined:"Defined — not provisioned",provisioning:"Provisioning",stopped:"Stopped",starting:"Starting",running:"Running",degraded:"Degraded",failed:"Failed",suspended:"Suspended",deleting:"Deleting"})[status]||String(status||"Unknown");
}
async function a44LoadProjectEnvironment(project,workspace,container){
 if(!container?.isConnected)return;
 let runtime=null,apps=[],status="none",error="";
 try{runtime=await apiRequest(`/v1/projects/${encodeURIComponent(project.id)}/runtime`);status=runtime?.status||"defined";}
 catch(err){
  const message=String(err?.message||"");
  if(!/not found|no rows|404/i.test(message)){status="error";error=message;}
 }
 if(runtime?.id){
  try{const res=await apiRequest(`/v1/project-runtimes/${encodeURIComponent(runtime.id)}/applications`);apps=Array.isArray(res)?res:[];}
  catch(err){error="Application inventory unavailable: "+String(err?.message||"unknown error");}
 }
 // Avoid repainting a different Workspace after a slow API response.
 if(!container.isConnected || container.dataset.workspaceId!==String(workspace.id))return;
 const safe=(text)=>escapeHtml(String(text??""));
 const observed=runtime?String(runtime.status||"unknown"):"not created";
 const desired=runtime?String(runtime.desired_state||"unknown"):"not requested";
 const appList=apps.length?apps.map(a=>`<div class="a44-installed-app"><div><strong>${safe(a.name||"Application")}</strong><div class="list-meta">${safe(a.source_kind||"source")} · ${safe(a.source_ref||"")} · ${safe(a.status||"declared")}</div></div><span class="pill">${safe(a.desired_state||"defined")}</span></div>`).join(""):'<div class="empty-state compact">No Project applications have been registered in the shared sandbox.</div>';
 container.innerHTML=`
  <section class="panel-card a44-development-shortcuts">
   <div class="card-header"><div><div class="card-title">Workspace development</div>
    <p class="list-meta">Use the dedicated Workspace sandbox for isolated tools, then run Tasks and publish verified outputs to the Project Library.</p></div></div>
   <div class="toolbar a44-shortcut-actions">
    <button type="button" class="btn" id="a44OpenProjectTasks">Open Tasks</button>
    <button type="button" class="btn" id="a44OpenProjectModels">Choose Models</button>
   </div>
  </section>
  ${runtime?`<details class="panel-card a44-legacy-runtime">
   <summary><span>Legacy shared Project sandbox (compatibility)</span>
    <span class="pill ${observed==="running"?"good":observed==="failed"?"bad":""}">${safe(a44RuntimeStatusLabel(observed))}</span></summary>
   <div class="a44-environment-note"><p>This older sandbox belongs to <strong>${safe(project.name||"the Project")}</strong>
    and is shared rather than isolated to <strong>${safe(workspace.name||"this")}</strong> Workspace.
    It is preserved for compatibility. New development tools belong in the dedicated Workspace sandbox above.</p></div>
   <div class="a44-environment-grid">
    ${a44DevelopmentSection("Observed legacy runtime",a44RuntimeStatusLabel(observed),"Requested state: "+desired)}
    ${a44DevelopmentSection("Legacy execution backend",runtime?.backend||"Not assigned",runtime?.node_id?"Node: "+runtime.node_id:"No verified execution Node assigned")}
    ${a44DevelopmentSection("Registered legacy applications",String(apps.length),"Application states are read from the Project runtime")}
    ${a44DevelopmentSection("Workspace isolation","Shared Project scope","Not a dedicated Workspace toolchain")}
   </div>
   <div class="a44-environment-subsection"><h3>Legacy installed applications</h3>
    <p class="list-meta">Declared applications are not proof that installation or execution succeeded.</p>
    <div class="a44-app-list">${appList}</div></div>
  </details>`:""}
  ${error?`<div class="error" role="alert">${safe(error)}</div>`:""}`;
 container.querySelector("#a44OpenProjectTasks")?.addEventListener("click",()=>openRoute("tasks"));
 container.querySelector("#a44OpenProjectModels")?.addEventListener("click",()=>openRoute("models"));
 // One canonical renderer owns the Workspace view: feature modules provide
 // explicit mount functions rather than stacking global function overrides.
 if(typeof a48MountWorkspaceRuntime==="function")await a48MountWorkspaceRuntime(project,workspace,container);
 if(typeof a45MountCollaboration==="function")await a45MountCollaboration(project,workspace,container);
 if(typeof a46MountWorkspaceLibrary==="function")await a46MountWorkspaceLibrary(project,workspace,container);
 if(typeof a49MountDevelopmentTasks==="function")await a49MountDevelopmentTasks(project,workspace,container);
 if(typeof a61MountProjectTaskGraphs==="function")await a61MountProjectTaskGraphs(project,workspace,container);
}

renderWorkspaces=async function(){
 await a44RenderWorkspaceDashboard();
 if(currentTab()?.route!=="workspaces")return;
 const project=qa4ActiveProject(),workspace=qa4ActiveWorkspace(),grid=$("#qa4WorkspaceGrid");
 if(!project||!workspace||!grid)return;
 const root=grid.parentElement;
 const tabbar=document.createElement("div");
 tabbar.className="a44-development-tabs";
 tabbar.setAttribute("role","tablist");
 tabbar.setAttribute("aria-label","Workspace view");
 tabbar.innerHTML=`<button type="button" class="subtab" id="a44DevelopmentTab" role="tab" aria-controls="a44DevelopmentPanel">Development</button>
 <button type="button" class="subtab" id="a44DashboardTab" role="tab" aria-controls="qa4WorkspaceGrid">Dashboard</button>`;
 const panel=document.createElement("section");
 panel.id="a44DevelopmentPanel";panel.className="a44-development-panel";panel.dataset.workspaceId=String(workspace.id);
 panel.setAttribute("role","tabpanel");
 panel.innerHTML='<div class="widget-body">Loading Project environment status…</div>';
 grid.before(tabbar,panel);
 const layoutControls=["qa4EditWorkspace","a35CancelWorkspaceLayout","qa4AddComponent","a43WorkspacePreset","a35ResetWorkspaceLayout"];
 const choice=a44WorkspacePresentation.get(workspace.id)||"development";
 function activate(which){
  const selected=which==="dashboard"?"dashboard":"development";
  a44WorkspacePresentation.set(workspace.id,selected);
  const isDashboard=selected==="dashboard";
  panel.hidden=isDashboard;grid.hidden=!isDashboard;
  tabbar.querySelector("#a44DashboardTab").classList.toggle("active",isDashboard);
  tabbar.querySelector("#a44DevelopmentTab").classList.toggle("active",!isDashboard);
  tabbar.querySelector("#a44DashboardTab").setAttribute("aria-selected",String(isDashboard));
  tabbar.querySelector("#a44DevelopmentTab").setAttribute("aria-selected",String(!isDashboard));
  for(const id of layoutControls){
   const element=root.querySelector("#"+id);
   if(element)element.hidden=!isDashboard;
  }
 }
 tabbar.querySelector("#a44DevelopmentTab").onclick=()=>activate("development");
 tabbar.querySelector("#a44DashboardTab").onclick=()=>activate("dashboard");
 activate(typeof a35WorkspaceEditing==="function"&&a35WorkspaceEditing()?"dashboard":choice);
 await a44LoadProjectEnvironment(project,workspace,panel);
};
