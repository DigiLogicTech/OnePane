/* Alpha 3.2 Project / Workspace QA remediation.
 * Loaded after app.js so Projects can be a management surface while Workspaces owns project work.
 * Keeps layout edits transient until Done and removes Inspector tab chrome in favour of drag reordering.
 */
let a35WorkspaceEditSession=null;

function a35Clone(value){
  try{return JSON.parse(JSON.stringify(value))}catch{return value}
}
function a35LatestProject(project){
  return qa4ProjectHub.projects.find(x=>x.id===project?.id)||project;
}
function a35LatestWorkspace(project,workspace){
  const latestProject=a35LatestProject(project),rows=latestProject?qa4Workspaces(latestProject):[];
  return {project:latestProject,workspace:rows.find(x=>x.id===workspace?.id)||workspace};
}
function a35WorkspaceEditing(){return !!a35WorkspaceEditSession}
function a35WorkspaceSessionMatches(project,workspace){
  return !!a35WorkspaceEditSession&&a35WorkspaceEditSession.projectID===project?.id&&a35WorkspaceEditSession.workspaceID===workspace?.id;
}
function a35CancelWorkspaceEdit(){
  a35WorkspaceEditSession=null;
  state.projectWorkspaceEdit=false;
}
function a35BeginWorkspaceEdit(project,workspace){
  a35CancelWorkspaceEdit();
  const widgets=a35Clone(workspace.widgets||[]);
  a31NormalizeLayout(widgets);
  a35WorkspaceEditSession={projectID:project.id,workspaceID:workspace.id,widgets};
}
function a35WorkspaceItems(project,workspace){
  return a35WorkspaceSessionMatches(project,workspace)?a35WorkspaceEditSession.widgets:(workspace.widgets||[]);
}
async function a35CommitWorkspaceEdit(project,workspace){
  if(!a35WorkspaceSessionMatches(project,workspace))return;
  const widgets=a35Clone(a35WorkspaceEditSession.widgets||[]);
  a31NormalizeLayout(widgets);
  a31ResolveLayout(widgets,null);
  const invalid=widgets.some(w=>![w.x,w.y,w.width,w.height].every(v=>Number.isFinite(Number(v)))||Number(w.width)<1||Number(w.height)<1)||widgets.some((a,i)=>widgets.slice(i+1).some(b=>a31Overlap(a,b)));
  if(invalid){
    notice("Workspace layout contains invalid geometry.","bad");
    return;
  }
  const latest=a35LatestWorkspace(project,workspace),baseProject=latest.project,baseWorkspace=latest.workspace;
  const next=qa4Workspaces(baseProject).map(w=>w.id===baseWorkspace.id?{...a35Clone(w),widgets}:a35Clone(w));
  try{
    await qa4SaveProjectWorkspaces(baseProject,next);
    a35CancelWorkspaceEdit();
    notice("Workspace layout saved.");
  }catch(ex){
    notice("Layout save failed: "+ex.message,"bad");
    throw ex;
  }
}
function a35ResetWorkspaceDraft(project,workspace){
  if(!a35WorkspaceSessionMatches(project,workspace))return;
  const items=a35WorkspaceEditSession.widgets;
  for(const item of items){
    const fallback=a31FallbackLayoutSize(item);
    delete item.x;delete item.y;
    item.width=fallback.width;item.height=fallback.height;item.col=fallback.width;item.row=fallback.height;
  }
  a31NormalizeLayout(items);
  a31ResolveLayout(items,null);
}
function a35ProjectTaskCount(projectID){
  return a31Array(liveOps?.tasks).filter(t=>String(t.project_id||"")===String(projectID)&&!["complete","cancelled","failed"].includes(String(t.state||"").toLowerCase())).length;
}
function a35ProjectUpdated(project){
  const raw=Number(project.updated_at||project.UpdatedAt||0);
  return raw?new Date(raw).toLocaleString():"No recent timestamp";
}
function a35OpenWorkspace(projectID,workspaceID=""){
  const project=qa4ProjectHub.projects.find(p=>p.id===projectID);
  if(!project)return;
  a35CancelWorkspaceEdit();
  qa4ProjectHub.activeProjectID=projectID;
  const rows=qa4Workspaces(project);
  qa4ProjectHub.activeWorkspaceID=rows.some(w=>w.id===workspaceID)?workspaceID:(rows[0]?.id||"");
  openRoute("workspaces");
}
function a35ProjectCard(project){
  const workspaces=qa4Workspaces(project),active=a35ProjectTaskCount(project.id);
  return `<article class="project-overview-card" data-a35-project-card="${escapeHtml(project.id)}">
    <div class="project-overview-card-head">
      <div><h2>${escapeHtml(project.name||"Project")}</h2><div class="page-subtitle">${escapeHtml(project.description||"No project description.")}</div></div>
      <span class="pill ${project.status==="active"?"good":""}">${escapeHtml(project.status||"active")}</span>
    </div>
    <div class="project-overview-metrics">
      <div><strong>${workspaces.length}</strong><span>Workspace${workspaces.length===1?"":"s"}</span></div>
      <div><strong>${active}</strong><span>Active task${active===1?"":"s"}</span></div>
      <div><strong>${escapeHtml(a35ProjectUpdated(project))}</strong><span>Updated</span></div>
    </div>
    <div class="project-overview-workspaces">${workspaces.slice(0,4).map(w=>`<button class="project-workspace-chip" data-a35-open-project="${escapeHtml(project.id)}" data-a35-open-workspace="${escapeHtml(w.id)}">${escapeHtml(w.name||"Workspace")}</button>`).join("")}${workspaces.length>4?`<span class="list-meta">+${workspaces.length-4} more</span>`:""}</div>
    <div class="toolbar project-overview-actions">
      <button class="btn primary" data-a35-open-project="${escapeHtml(project.id)}">Open</button>
      <button class="btn" data-a35-project-settings="${escapeHtml(project.id)}">Project settings</button>
      <button class="btn danger" data-a35-delete-project="${escapeHtml(project.id)}">Delete project</button>
    </div>
  </article>`;
}

renderProjects=async function(){
  a35CancelWorkspaceEdit();
  const epoch=qa31ViewEpoch;
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Projects","Create and manage overarching projects. Open a Project to work inside its Workspaces.",'<button class="btn primary" id="qa4NewProject">New project</button>')}<div class="project-hub-loading widget-body">Loading projects…</div></section>`;
  $("#qa4NewProject").onclick=openProjectDialog;
  await qa4LoadProjectHub(true);
  if(epoch!==qa31ViewEpoch||currentTab()?.route!=="projects")return;
  const host=$(".project-hub-loading");if(!host)return;
  if(!qa4ProjectHub.projects.length){
    host.outerHTML='<div class="empty-state"><strong>No projects yet.</strong><br>Create a project to begin adding workspaces.</div>';
    return;
  }
  host.outerHTML=`<div class="project-overview-grid" id="a35ProjectGrid">${qa4ProjectHub.projects.map(a35ProjectCard).join("")}</div>`;
  $$("[data-a35-open-project]").forEach(b=>b.onclick=()=>a35OpenWorkspace(b.dataset.a35OpenProject,b.dataset.a35OpenWorkspace||""));
  $$("[data-a35-project-settings]").forEach(b=>b.onclick=()=>{const p=qa4ProjectHub.projects.find(x=>x.id===b.dataset.a35ProjectSettings);if(p)qa4Inspect("project",p.id,p.name,p)});
  $$("[data-a35-delete-project]").forEach(b=>b.onclick=()=>{const p=qa4ProjectHub.projects.find(x=>x.id===b.dataset.a35DeleteProject);if(p)a33DeleteProject(p)});
};

function a35WorkspaceWidget(w,project,workspace){
  const edit=a35WorkspaceSessionMatches(project,workspace);
  const controls=edit?`<div class="dashboard-edit-bar workspace-drag-bar" data-pw-drag="${escapeHtml(w.id)}" title="Drag component"><strong>${escapeHtml(w.title||w.type)}</strong><span class="dashboard-edit-spacer"></span><select class="dashboard-size" data-pw-preset="${escapeHtml(w.id)}"><option value="">Size…</option><option value="small">Small</option><option value="medium">Medium</option><option value="large">Large</option><option value="wide">Wide</option><option value="full">Full</option></select><button class="tiny danger" data-pw-remove="${escapeHtml(w.id)}" title="Remove component">×</button></div>`:"";
  return `<section class="workspace-widget dashboard-widget a31-layout-item ${edit?"editable":""}" data-pw-widget="${escapeHtml(w.id)}">${controls}<div class="dashboard-widget-content"><div class="widget-handle"><strong>${escapeHtml(w.title||w.type)}</strong></div>${qa6ComponentContent(w,project,workspace)}</div>${edit?a31ResizeHandles(w.id,"data-pw-resize",w.title||w.type):""}</section>`;
}
function a35RenderWorkspaceGrid(project,workspace){
  const root=$("#qa4WorkspaceGrid");if(!root)return;
  const items=a35WorkspaceItems(project,workspace);
  a31NormalizeLayout(items);
  root.innerHTML=items.map(w=>a35WorkspaceWidget(w,project,workspace)).join("");
  a31ApplyLayout(root,items,"data-pw-widget");
  a35BindWorkspaceEdit(project,workspace);
  if(!a35WorkspaceSessionMatches(project,workspace))qa6BindProjectComponents(project,workspace);
  qa7BindWorkspaceControls(project,workspace,root);
  qa4BindProjectNotes(root);
}
function a35BindWorkspaceEdit(project,workspace){
  if(!a35WorkspaceSessionMatches(project,workspace))return;
  const root=$("#qa4WorkspaceGrid"),items=a35WorkspaceEditSession.widgets;if(!root)return;
  a31BindLayout(root,items,{attr:"data-pw-widget",dragAttr:"data-pw-drag",resizeAttr:"data-pw-resize",persist:async()=>{}});
  $$("[data-pw-preset]",root).forEach(sel=>sel.onchange=()=>{
    if(!sel.value)return;const w=items.find(x=>x.id===sel.dataset.pwPreset);if(!w)return;
    const p=workspacePreset(sel.value,w);w.width=p.col;w.height=p.row;w.col=p.col;w.row=p.row;
    a31ResolveLayout(items,w.id);a31ApplyLayout(root,items,"data-pw-widget",true);sel.value="";
  });
  $$("[data-pw-remove]",root).forEach(b=>b.onclick=()=>{
    const i=items.findIndex(x=>x.id===b.dataset.pwRemove);if(i<0)return;
    items.splice(i,1);a31NormalizeLayout(items);a31ResolveLayout(items,null);a35RenderWorkspaceGrid(project,workspace);
  });
}
qa4BindWorkspaceEdit=a35BindWorkspaceEdit;

qa6ToggleMaximize=async function(project,workspace,id){
  const latest=a35LatestWorkspace(project,workspace);project=latest.project;workspace=latest.workspace;
  workspace.maximized_widget_id=workspace.maximized_widget_id===id?"":id;
  await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));
  if(currentTab()?.route==="workspaces")await renderWorkspaces();else await renderProjects();
};
qa6PromoteInspectorComponent=async function(project,workspace,type){
  const latest=a35LatestWorkspace(project,workspace);project=latest.project;workspace=latest.workspace;
  const m=qa6Meta(type),size=qa6SizeFor(type);
  workspace.widgets=workspace.widgets||[];
  workspace.widgets.push({id:`pw-${type}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,5)}`,type,title:m.title,col:size.col,row:size.row,config:type==="follow"?{task_id:"auto"}:{}});
  await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));
  if(currentTab()?.route==="workspaces")await renderWorkspaces();else await renderProjects();
};

qa4AddWorkspaceComponent=function(project,workspace){
  if(!a35WorkspaceSessionMatches(project,workspace))return notice("Enter Edit layout before adding components.","bad");
  const items=a35WorkspaceEditSession.widgets,used=new Set(items.map(x=>x.type)),available=qa4WorkspaceCatalogue().filter(([t])=>!used.has(t)||["notes","chat"].includes(t));
  openModal("Add workspace component",available.length?`<div class="component-picker-grid">${available.map(([t,title])=>`<button class="component-choice" data-a35-add-component="${t}"><strong>${escapeHtml(title)}</strong><span>Add to ${escapeHtml(workspace.name)}</span></button>`).join("")}</div>`:'<div class="empty-state compact">All workspace components are already present.</div>');
  $$("[data-a35-add-component]").forEach(b=>b.onclick=()=>{
    const [type,title]=qa4WorkspaceCatalogue().find(x=>x[0]===b.dataset.a35AddComponent),fallback=a31FallbackLayoutSize({type});
    const item={id:`pw-${type}-${Date.now().toString(36)}`,type,title,width:fallback.width,height:fallback.height,col:fallback.width,row:fallback.height};
    a31NormalizeLayout(items);const slot=a31FirstFree(items,item,null);item.x=slot.x;item.y=slot.y;items.push(item);
    closeModal();a35RenderWorkspaceGrid(project,workspace);
  });
};

function a35AddWorkspace(project){
  a35CancelWorkspaceEdit();
  openModal("Add workspace",`<form id="a35AddWorkspaceForm" class="qa-form"><label>Name<input name="name" required placeholder="Development"></label><div class="page-subtitle">The workspace inherits secure defaults and can then override sandbox, routing, models and agents.</div><button class="btn primary">Create workspace</button></form>`);
  $("#a35AddWorkspaceForm").onsubmit=async e=>{
    e.preventDefault();const name=new FormData(e.currentTarget).get("name").trim();if(!name)return;
    const rows=qa4Workspaces(project),ws=qa4DefaultWorkspace(project,name);
    try{await qa4SaveProjectWorkspaces(project,[...rows,ws]);qa4ProjectHub.activeProjectID=project.id;qa4ProjectHub.activeWorkspaceID=ws.id;closeModal();await renderWorkspaces();notice("Workspace created.");}
    catch(ex){notice(ex.message,"bad")}
  };
}
async function a35DeleteWorkspace(project,workspace){
  const rows=qa4Workspaces(project),index=rows.findIndex(x=>x.id===workspace.id);
  if(index<0)return notice("Workspace is no longer available.","bad");
  if(rows.length<=1)return notice("A project must keep at least one workspace.","bad");
  const fallback=rows[index+1]||rows[index-1],remaining=rows.filter(x=>x.id!==workspace.id);
  openModal("Delete workspace",`<div class="widget-body"><strong>Delete ${escapeHtml(workspace.name||"this workspace")}?</strong><p>This removes its workspace-owned layout, settings, notes and routing configuration. The overarching Project is not deleted.</p></div>`,'<button class="btn" id="a35CancelDeleteWorkspace">Cancel</button><button class="btn danger" id="a35ConfirmDeleteWorkspace">Delete workspace</button>');
  $("#a35CancelDeleteWorkspace").onclick=closeModal;
  $("#a35ConfirmDeleteWorkspace").onclick=async e=>{
    const button=e.currentTarget;button.disabled=true;button.textContent="Deleting…";
    try{
      await qa4SaveProjectWorkspaces(project,remaining);a35CancelWorkspaceEdit();qa4ProjectHub.activeProjectID=project.id;qa4ProjectHub.activeWorkspaceID=fallback?.id||"";
      if(qa4Inspector?.kind==="workspace"&&qa4Inspector.id===workspace.id){qa4Inspector={kind:"system",id:"system",title:"OnePane",data:{}};qa4InspectorTab="overview";setInspectorOpen(false)}
      closeModal();await renderWorkspaces();notice(`Workspace "${workspace.name||"Workspace"}" deleted.`);
    }catch(ex){button.disabled=false;button.textContent="Delete workspace";notice("Workspace delete failed: "+ex.message,"bad")}
  };
}
a32DeleteWorkspace=a35DeleteWorkspace;

async function renderWorkspaces(){
  await qa4LoadProjectHub(false);
  const project=qa4ActiveProject();
  if(!project){
    $("#viewHost").innerHTML=`<section class="page">${pageHeader("Workspace","No Project selected",'<button class="btn primary" id="a35GoProjects">Open Projects</button>')}<div class="empty-state">Create or select a Project before opening a Workspace.</div></section>`;
    $("#a35GoProjects").onclick=()=>openRoute("projects");return;
  }
  const rows=qa4Workspaces(project);
  if(!rows.some(w=>w.id===qa4ProjectHub.activeWorkspaceID))qa4ProjectHub.activeWorkspaceID=rows[0]?.id||"";
  const workspace=qa4ActiveWorkspace();if(!workspace)return;
  qa7NormalizeWorkspace(project,workspace);
  if(a35WorkspaceEditing()&&!a35WorkspaceSessionMatches(project,workspace))a35CancelWorkspaceEdit();
  const edit=a35WorkspaceSessionMatches(project,workspace);
  const actions=`<button class="btn" id="a35AddWorkspace">Add workspace</button><button class="btn ${edit?"primary":""}" id="qa4EditWorkspace">${edit?"Done":"Edit layout"}</button>${edit?'<button class="btn" id="qa4AddComponent">Add component</button><button class="btn" id="a35ResetWorkspaceLayout">Reset layout</button>':""}`;
  $("#viewHost").innerHTML=`<section class="page workspace-page">${pageHeader(project.name||"Workspace",project.description||"Project workspace",actions)}
    <div class="workspace-tabs">${rows.map(w=>`<button class="workspace-tab ${w.id===workspace.id?"active":""}" data-a35-workspace="${escapeHtml(w.id)}">${escapeHtml(w.name||"Workspace")}</button>`).join("")}</div>
    <div class="workspace-context-bar"><div><strong>${escapeHtml(workspace.name)}</strong><span class="list-meta"> · workspace sandbox ${workspace.sandbox?.internet?"internet allowed":"internet blocked"} · ${workspace.routing?.enabled!==false?"routing enabled":"single-path"} · ${escapeHtml(titleCase(workspace.orchestration?.mode||"direct"))}</span></div><div class="toolbar compact a32-workspace-actions"><button class="btn" id="qa4WorkspaceSettings">Workspace settings</button><button class="btn danger" id="a32DeleteWorkspace" ${rows.length<=1?"disabled":""}>Delete workspace</button></div></div>
    <div class="workspace-grid ${edit?"editing":""}" id="qa4WorkspaceGrid"></div>
  </section>`;
  a35RenderWorkspaceGrid(project,workspace);
  $$("[data-a35-workspace]").forEach(b=>b.onclick=()=>{a35CancelWorkspaceEdit();qa4ProjectHub.activeWorkspaceID=b.dataset.a35Workspace;renderWorkspaces();renderNav()});
  $("#a35AddWorkspace").onclick=()=>a35AddWorkspace(project);
  $("#qa4WorkspaceSettings").onclick=()=>qa6OpenInInspector(project,workspace,"settings");
  $("#a32DeleteWorkspace").onclick=()=>a35DeleteWorkspace(project,workspace);
  $("#qa4EditWorkspace").onclick=async e=>{
    if(!a35WorkspaceSessionMatches(project,workspace)){a35BeginWorkspaceEdit(project,workspace);await renderWorkspaces();return}
    const button=e.currentTarget;button.disabled=true;button.textContent="Saving…";
    try{await a35CommitWorkspaceEdit(project,workspace);await renderWorkspaces()}catch{button.disabled=false;button.textContent="Done"}
  };
  $("#qa4AddComponent")?.addEventListener("click",()=>qa4AddWorkspaceComponent(project,workspace));
  $("#a35ResetWorkspaceLayout")?.addEventListener("click",()=>{a35ResetWorkspaceDraft(project,workspace);a35RenderWorkspaceGrid(project,workspace)});
  qa6StartEventStream();renderNav();
}

const a35RenderNavBase=renderNav;
renderNav=function(){
  a35RenderNavBase();
  const nav=$("#primaryNav"),route=currentTab()?.route;if(!nav)return;
  const projectsButton=$('[data-route="projects"]',nav);if(projectsButton)projectsButton.classList.toggle("active",route==="projects");
  $$("[data-a31-project-nav]",nav).forEach(b=>{
    const isWorkspace=!!b.dataset.a31WorkspaceNav,projectID=b.dataset.a31ProjectNav,workspaceID=b.dataset.a31WorkspaceNav||"";
    const active=route==="workspaces"&&projectID===qa4ProjectHub.activeProjectID&&(!isWorkspace||workspaceID===qa4ProjectHub.activeWorkspaceID);
    b.classList.toggle("active",active);
    b.onclick=e=>{e.preventDefault();e.stopPropagation();a35OpenWorkspace(projectID,workspaceID)};
  });
  const legacyWorkspace=$('[data-route="workspaces"]',nav);if(legacyWorkspace)legacyWorkspace.remove();
};

const a35ActivateTabBase=activateTab;
activateTab=function(id){
  const next=state.tabs.find(t=>t.id===id);
  if(a35WorkspaceEditing()&&currentTab()?.route==="workspaces"&&next?.route!=="workspaces")a35CancelWorkspaceEdit();
  return a35ActivateTabBase(id);
};
const a35CloseTabBase=closeTab;
closeTab=function(id){
  if(a35WorkspaceEditing()&&state.activeTab===id&&state.tabs.find(t=>t.id===id)?.route==="workspaces")a35CancelWorkspaceEdit();
  return a35CloseTabBase(id);
};

const a35RenderActiveViewBase=renderActiveView;
renderActiveView=async function(){
  const t=currentTab();if(t?.route!=="workspaces")return a35RenderActiveViewBase();
  const epoch=++qa31ViewEpoch;if(t.state==="suspended")t.state="active";
  try{await renderWorkspaces()}
  finally{const host=$("#viewHost");if(epoch===qa31ViewEpoch){if(host)host.dataset.renderedRoute="workspaces";renderNav()}}
};

const A35_INSPECTOR_TYPE_ALIASES={modelstack:"models"};
function a35InspectorType(type){
  const raw=String(type||"").trim().toLowerCase();
  return A35_INSPECTOR_TYPE_ALIASES[raw]||raw;
}
function a35InspectorComponentTypes(){
  return [...new Set(Object.keys(QA6_COMPONENTS).map(a35InspectorType).filter(Boolean))];
}
function a35RefreshInspectorRegistry(){
  const types=a35InspectorComponentTypes();
  QA6_INSPECTOR_COMPONENTS.splice(0,QA6_INSPECTOR_COMPONENTS.length,...types);
  return new Set(types);
}
function a35CanInspectWorkspaceType(type){
  return a35RefreshInspectorRegistry().has(a35InspectorType(type));
}
a35RefreshInspectorRegistry();

const a35InspectorTabsBase=qa6InspectorTabs;
qa6InspectorTabs=function(){
  const d=typeof qa6WorkspaceInspectorContext==="function"?qa6WorkspaceInspectorContext():null;
  if(!d)return a35InspectorTabsBase();
  const cfg=qa6InspectorConfig(d.workspace),supported=a35RefreshInspectorRegistry(),normalized=[];
  for(const raw of cfg.tabs||[]){
    const type=a35InspectorType(raw);
    if(supported.has(type)&&!normalized.includes(type))normalized.push(type);
  }
  cfg.tabs.splice(0,cfg.tabs.length,...normalized);
  return ["overview",...normalized,...(cfg.tiles.length?["panels"]:[])].filter((x,i,a)=>a.indexOf(x)===i);
};

qa6OpenInInspector=async function(project,workspace,type){
  const target=a35InspectorType(type),supported=a35RefreshInspectorRegistry();
  if(!supported.has(target)){
    notice(`Inspector is not available for ${qa6Meta(type).title||titleCase(type)}.`,"bad");
    return false;
  }
  let latest=a35LatestWorkspace(project,workspace);
  project=latest.project;workspace=latest.workspace;
  const cfg=qa6InspectorConfig(workspace),normalized=[];
  for(const raw of cfg.tabs||[]){
    const item=a35InspectorType(raw);
    if(supported.has(item)&&!normalized.includes(item))normalized.push(item);
  }
  const before=JSON.stringify(cfg.tabs||[]);
  cfg.tabs.splice(0,cfg.tabs.length,...normalized);
  if(!cfg.tabs.includes(target))cfg.tabs.push(target);
  if(JSON.stringify(cfg.tabs)!==before)await qa6SaveInspector(project,workspace);
  latest=a35LatestWorkspace(project,workspace);
  qa4Inspector={kind:"workspace",id:latest.workspace.id,title:`${latest.project.name} / ${latest.workspace.name}`,data:{project:latest.project,workspace:latest.workspace}};
  qa4InspectorTab=target;
  setInspectorOpen(true);
  renderInspector();
  return qa4InspectorTab===target;
};

const a35BindProjectComponentsBase=qa6BindProjectComponents;
qa6BindProjectComponents=function(project,workspace){
  a35BindProjectComponentsBase(project,workspace);
  const supported=a35RefreshInspectorRegistry(),root=$("#qa4WorkspaceGrid")||document;
  $$("[data-qa6-inspector]",root).forEach(button=>{
    const widget=(workspace.widgets||[]).find(x=>x.id===button.dataset.qa6Inspector),target=widget?a35InspectorType(widget.type):"";
    if(!supported.has(target)){button.remove();return}
    button.dataset.qa6InspectorTarget=target;
    button.title=`Open ${qa6Meta(target).title} in Inspector`;
  });
};

function a35InspectorContext(){
  const d=typeof qa6WorkspaceInspectorContext==="function"?qa6WorkspaceInspectorContext():null;
  if(!d)return null;
  const latest=a35LatestWorkspace(d.project,d.workspace),fresh={project:latest.project,workspace:latest.workspace};
  return {d:fresh,cfg:qa6InspectorConfig(fresh.workspace)};
}
function a35RemoveInspectorTab(type){
  const ctx=a35InspectorContext();if(!ctx)return;
  openModal("Remove Inspector tab",`<div class="widget-body"><strong>Remove ${escapeHtml(titleCase(type))}?</strong><p>The tab configuration is removed from this Workspace. The underlying component data is unchanged.</p></div>`,'<button class="btn" id="a35CancelInspectorRemove">Cancel</button><button class="btn danger" id="a35ConfirmInspectorRemove">Remove tab</button>');
  $("#a35CancelInspectorRemove").onclick=closeModal;
  $("#a35ConfirmInspectorRemove").onclick=async()=>{ctx.cfg.tabs=ctx.cfg.tabs.filter(x=>x!==type);if(qa4InspectorTab===type)qa4InspectorTab="overview";await qa6SaveInspector(ctx.d.project,ctx.d.workspace);closeModal();renderInspector()};
}
async function a35ReorderInspectorTab(type,delta){
  const ctx=a35InspectorContext();if(!ctx)return false;
  const i=ctx.cfg.tabs.indexOf(type);if(i<0)return false;
  const j=Math.max(0,Math.min(ctx.cfg.tabs.length-1,i+delta));if(i===j)return false;
  const [tab]=ctx.cfg.tabs.splice(i,1);ctx.cfg.tabs.splice(j,0,tab);
  await qa6SaveInspector(ctx.d.project,ctx.d.workspace);renderInspector();return true;
}
function a35BindInspectorTabs(){
  const ctx=a35InspectorContext();if(!ctx)return;
  $$(".qa4-inspector-tab-wrap",$("#inspector")).forEach(wrap=>{
    const button=$("[data-qa6-inspector-tab]",wrap),type=button?.dataset.qa6InspectorTab;if(!type||!ctx.cfg.tabs.includes(type))return;
    $(".qa4-inspector-tab-tools",wrap)?.remove();
    const tools=document.createElement("span");tools.className="qa4-inspector-tab-tools inspector-tab-close-tools";tools.innerHTML='<button type="button" data-a35-inspector-close title="Remove tab" aria-label="Remove tab">×</button>';wrap.appendChild(tools);
    const closeButton=$("[data-a35-inspector-close]",tools);closeButton.onclick=e=>{e.preventDefault();e.stopPropagation();a35RemoveInspectorTab(type)};
    wrap.classList.add("inspector-tab-draggable");wrap.title="Drag to reorder";
    let active=false,moved=false,startX=0,width=0,dx=0;
    const move=e=>{if(!active)return;dx=e.clientX-startX;if(Math.abs(dx)>6)moved=true;if(!moved)return;e.preventDefault();wrap.style.transform=`translate3d(${dx}px,0,0)`;wrap.classList.add("dragging")};
    const finish=async()=>{if(!active)return;active=false;window.removeEventListener("pointermove",move);window.removeEventListener("pointerup",finish);window.removeEventListener("pointercancel",cancel);wrap.style.removeProperty("transform");wrap.classList.remove("dragging");if(moved){wrap.dataset.inspectorDragged="true";const steps=Math.max(1,Math.round(Math.abs(dx)/Math.max(1,width)))*(dx<0?-1:1);await a35ReorderInspectorTab(type,steps);setTimeout(()=>{if(wrap.isConnected)delete wrap.dataset.inspectorDragged},0)}};
    const cancel=()=>{active=false;window.removeEventListener("pointermove",move);window.removeEventListener("pointerup",finish);window.removeEventListener("pointercancel",cancel);wrap.style.removeProperty("transform");wrap.classList.remove("dragging")};
    wrap.onpointerdown=e=>{if(e.button!==0||e.target.closest?.("[data-a35-inspector-close]"))return;active=true;moved=false;dx=0;startX=e.clientX;width=wrap.getBoundingClientRect().width+4;window.addEventListener("pointermove",move,{passive:false});window.addEventListener("pointerup",finish,{once:true});window.addEventListener("pointercancel",cancel,{once:true})};
    wrap.addEventListener("click",e=>{if(wrap.dataset.inspectorDragged==="true"){e.preventDefault();e.stopImmediatePropagation();delete wrap.dataset.inspectorDragged}},true);
    wrap.oncontextmenu=e=>{e.preventDefault();a35RemoveInspectorTab(type)};
  });
}
const a35RenderInspectorBase=renderInspector;
renderInspector=function(){a35RenderInspectorBase();a35BindInspectorTabs()};

if(state.projectWorkspaceEdit){state.projectWorkspaceEdit=false;persist()}
