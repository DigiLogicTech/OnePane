/* Alpha 3 workspace/model/runtime consolidation. Appended to the canonical WebUI bundle. */
const QA8_RELEASE='0.1.0-alpha.3';
const qa8VisibleModes=['direct','team','council'];

navItems.splice(0,navItems.length,...navItems.filter(([route])=>['operations','projects','tasks','models','agents'].includes(route)));
pages.projects.title='Projects';
pages.models.title='Models';
pages.agents.title='Agents';
pages.settings.title='Settings';

function qa8Mode(v){v=String(v||'').toLowerCase();return v==='team'||v==='council'?v:'direct';}
const qa8NormalizeBase=qa7NormalizeWorkspace;
qa7NormalizeWorkspace=function(project,workspace){
  const w=qa8NormalizeBase(project,workspace);
  w.orchestration=w.orchestration||{};
  w.orchestration.mode=qa8Mode(w.orchestration.mode);
  w.orchestration.supervisor=qa7NormalizeRole(w.orchestration.supervisor,'agent.md');
  w.orchestration.team=qa7NormalizeRole(w.orchestration.team,'onepane-default');
  w.orchestration.council=qa7NormalizeRole(w.orchestration.council,'onepane-default');
  w.compute=w.compute||{preference:'auto'};
  return w;
};

function qa8ComputeBadge(mode){const v=String(mode||'auto').toLowerCase();const label=v==='cpu_gpu'||v==='hybrid'?'HYBRID':v==='gpu'?'GPU':v==='cpu'?'CPU':'AUTO';return `<span class="pill qa8-compute ${label.toLowerCase()}">${label}</span>`;}
function qa8ModelAssignment(workspace,mode){const o=workspace.orchestration||{};return mode==='direct'?(o.supervisor||{}):(o[mode]||{});}
function qa8ModelStackRole(mode,label,workspace){
  const x=qa8ModelAssignment(workspace,mode),models=qa4ModelOptions(),agents=qa4AgentOptions();
  const fallback=x.fallback_model||'auto',compute=x.compute_preference||workspace.compute?.preference||'auto';
  return `<div class="qa8-stack-pane" data-qa8-stack-pane="${mode}"><div class="qa8-stack-head"><div><strong>${label}</strong><div class="list-meta">Workspace-owned assignment · changes apply only here</div></div><button class="btn tiny" type="button" data-qa8-recommend="${mode}">Recommend</button></div><label>Primary model<select data-qa8-model="${mode}">${qa4OptionRows(models,x.model||'auto')}</select></label><label>Agent<select data-qa8-agent="${mode}">${qa4OptionRows(agents,x.agent||(mode==='direct'?'agent.md':'onepane-default'))}</select></label><label>Compute preference<select data-qa8-compute="${mode}"><option value="auto" ${compute==='auto'?'selected':''}>Auto</option><option value="prefer-gpu" ${compute==='prefer-gpu'?'selected':''}>Prefer GPU</option><option value="prefer-cpu" ${compute==='prefer-cpu'?'selected':''}>Prefer CPU</option><option value="gpu-only" ${compute==='gpu-only'?'selected':''}>GPU only</option><option value="cpu-only" ${compute==='cpu-only'?'selected':''}>CPU only</option></select></label><label>Fallback model<select data-qa8-fallback="${mode}">${qa4OptionRows(models,fallback)}</select></label>${mode!=='direct'?`<div class="list-meta">Active model-powered seats are capped at 8, including a model-powered coordinator/synthesiser.</div>`:''}</div>`;
}
function qa8ModelStackContent(w,project,workspace){
  qa7NormalizeWorkspace(project,workspace);const active=w.config?.tab||'direct';
  return `<div class="qa8-model-stack" data-qa8-stack-root="${escapeHtml(w.id)}"><div class="qa8-stack-tabs">${qa8VisibleModes.map(m=>`<button type="button" class="subtab ${active===m?'active':''}" data-qa8-stack-tab="${m}" data-qa8-stack-id="${escapeHtml(w.id)}">${titleCase(m)}</button>`).join('')}</div>${qa8ModelStackRole(active,titleCase(active),workspace)}<div class="qa8-stack-footer"><span class="pill ${workspace.routing?.enabled!==false?'good':''}">${workspace.routing?.enabled!==false?'Routing enabled':'Routing disabled'}</span><span class="list-meta">Deployment fallback (same model on another CPU/GPU/node) is attempted before model fallback.</span></div></div>`;
}

QA6_COMPONENTS.modelstack={title:'Model Stack',description:'Direct, Team and Council model assignments, fallbacks and compute preference.',multiple:false,size:'large'};
QA6_COMPONENTS.library={title:'Library',description:'Project-owned files, datasets, artifacts and packages granted to this workspace.',multiple:false,size:'large'};
QA6_COMPONENTS.integrations={title:'Integrations',description:'Workspace grants for connected application-level integrations.',multiple:false,size:'medium'};
QA6_COMPONENTS.approvals={title:'Approvals',description:'Pending confirm-class actions for this workspace.',multiple:false,size:'medium'};

const qa8DefaultWorkspaceBase=qa4DefaultWorkspace;
qa4DefaultWorkspace=function(project,name='Main workspace'){
  const w=qa7NormalizeWorkspace(project,qa8DefaultWorkspaceBase(project,name));
  w.orchestration.mode='direct';
  const now=Date.now();
  const keep=(w.widgets||[]).filter(x=>!['models'].includes(x.type));
  if(!keep.some(x=>x.type==='chat'))keep.unshift({id:`pw-chat-${now}`,type:'chat',title:'Chat',col:6,row:5});
  if(!keep.some(x=>x.type==='modelstack'))keep.splice(1,0,{id:`pw-modelstack-${now+1}`,type:'modelstack',title:'Model Stack',col:6,row:5,config:{tab:'direct'}});
  w.widgets=keep;
  return w;
};

const qa8ComponentBase=qa6ComponentContent;
qa6ComponentContent=function(w,project,workspace){
  if(w.type==='modelstack')return qa8ModelStackContent(w,project,workspace);
  if(w.type==='library')return '<div class="empty-state compact">Project Library is enabled for this workspace. Upload and access grants are enforced by the control plane.</div>';
  if(w.type==='integrations')return '<div class="empty-state compact">Integration connections are application-level; this workspace controls its own capability grants.</div>';
  if(w.type==='approvals')return '<div class="empty-state compact">Confirm-class actions appear here and in Follow before execution continues.</div>';
  return qa8ComponentBase(w,project,workspace);
};

qa7EffectiveChatMode=function(workspace,requested='default'){
  let mode=requested==='default'?qa8Mode(workspace.orchestration?.mode):qa8Mode(requested);
  if(workspace.routing?.enabled===false&&mode!=='direct')mode='direct';
  return mode;
};
qa7RoutingEnvelope=function(workspace,requested='default'){
  const enabled=workspace.routing?.enabled!==false,mode=qa7EffectiveChatMode(workspace,requested),role=mode==='direct'?'supervisor':mode,selected=qa4RouteSelection(workspace,role),fallback=qa7RouteFallback(workspace,role);
  return {enabled,mode,role,candidate_id:selected.candidate_id,agent_profile:selected.agent_profile,fallback_candidate_ids:enabled?fallback.candidate_ids:[],fallback_agent_profiles:enabled?fallback.agent_profiles:[],project_workspace_id:workspace.id,workspace_access:qa7WorkspaceAccess(workspace),compute_preference:qa8ModelAssignment(workspace,mode).compute_preference||workspace.compute?.preference||'auto'};
};

function qa8Recommend(workspace,mode){
  const role=qa8ModelAssignment(workspace,mode);
  if(!role.model)role.model='auto';
  if(!role.agent)role.agent=mode==='direct'?'agent.md':'onepane-default';
  if(!role.fallback_model)role.fallback_model='auto';
  if(!role.compute_preference)role.compute_preference='auto';
}
function qa8BindModelStack(project,workspace,root=document){
  $$('[data-qa8-stack-tab]',root).forEach(b=>b.onclick=async()=>{const w=qa7FindComponent(workspace,b.dataset.qa8StackId);if(!w)return;w.config=w.config||{};w.config.tab=b.dataset.qa8StackTab;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();});
  $$('[data-qa8-recommend]',root).forEach(b=>b.onclick=async()=>{qa8Recommend(workspace,b.dataset.qa8Recommend);await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));notice(`${titleCase(b.dataset.qa8Recommend)} recommendation applied without replacing pinned choices.`);renderProjects();});
  $$('[data-qa8-model]',root).forEach(x=>x.onchange=async()=>{const a=qa8ModelAssignment(workspace,x.dataset.qa8Model);a.model=x.value;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));});
  $$('[data-qa8-agent]',root).forEach(x=>x.onchange=async()=>{const a=qa8ModelAssignment(workspace,x.dataset.qa8Agent);a.agent=x.value;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));});
  $$('[data-qa8-compute]',root).forEach(x=>x.onchange=async()=>{const a=qa8ModelAssignment(workspace,x.dataset.qa8Compute);a.compute_preference=x.value;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));});
  $$('[data-qa8-fallback]',root).forEach(x=>x.onchange=async()=>{const a=qa8ModelAssignment(workspace,x.dataset.qa8Fallback);a.fallback_model=x.value;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));});
}
const qa8BindWorkspaceBase=qa7BindWorkspaceControls;
qa7BindWorkspaceControls=function(project,workspace,root=document){qa8BindWorkspaceBase(project,workspace,root);qa8BindModelStack(project,workspace,root);};

function qa8ComputeModeForDeployment(d){
  const raw=String(d?.compute_mode||d?.run_mode||d?.placement?.mode||'').toLowerCase();
  if(raw.includes('hybrid')||raw.includes('offload')||raw.includes('cpu_gpu'))return 'hybrid';
  if(raw.includes('gpu')||['cuda','rocm','hip','vulkan','metal','directml','sycl'].includes(String(d?.compute_backend||d?.runtime_backend||'').toLowerCase()))return 'gpu';
  if(raw.includes('cpu'))return 'cpu';
  return 'auto';
}
function qa8DecorateModels(){
  const heading=$('#viewHost .page-heading h1');if(heading)heading.textContent='Models';
  const rows=$$('#qa5ManagedModels .model-tile');
  rows.forEach((el,i)=>{const d=qa5ManagedDeployments[i];if(!d)return;const meta=el.querySelector('.list-meta');if(meta&&!el.querySelector('.qa8-model-compute'))meta.insertAdjacentHTML('beforebegin',`<div class="toolbar qa8-model-compute">${qa8ComputeBadge(qa8ComputeModeForDeployment(d))}<span class="list-meta">${escapeHtml(d.compute_backend||d.runtime_backend||'')}</span></div>`);if(d.qualified||String(d.investigation_state||'').toLowerCase()==='qualified')el.querySelector('[data-agent-check]')?.remove();});
}
const qa8RenderModelsBase=renderModels;
renderModels=async function(){await qa8RenderModelsBase();queueMicrotask(qa8DecorateModels);};

function qa8SettingsEnhance(){
  const about=$('.about-block strong');if(about)about.textContent=`OnePane v${QA8_RELEASE}`;
  const mode=$('#qa5DefaultMode');if(mode){const current=qa8Mode(mode.value);mode.innerHTML=qa8VisibleModes.map(v=>`<option value="${v}" ${current===v?'selected':''}>${titleCase(v)}</option>`).join('');}
}
const qa8RenderSettingsBase=renderSettings;
renderSettings=async function(){await qa8RenderSettingsBase();qa8SettingsEnhance();};

try{const p=qa5Prefs();if(['supervisor','workers'].includes(p.workspace_defaults?.orchestration)){p.workspace_defaults.orchestration='direct';localStorage.setItem(QA5_PREFS_KEY,JSON.stringify(p));}}catch{}
