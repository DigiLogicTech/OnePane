const $ = (s, root=document) => root.querySelector(s);
const $$ = (s, root=document) => [...root.querySelectorAll(s)];

const STORAGE_KEY = 'onepane.ui.v1';
const TOUR_KEY = 'onepane.ui.product-tour.v1.completed';
const TOUR_COMPLETE_VALUE = 'done';
const TAB_SUSPEND_MS = 5 * 60 * 1000;
const TAB_BACKGROUND_MS = 20 * 1000;
const PHONE_MEDIA = window.matchMedia('(max-width: 700px)');

const navItems = [
  ['operations','▣','Operations'], ['workspaces','▱','Workspaces'], ['tasks','☑','Tasks'], ['projects','▢','Projects'],
  ['models','◇','Models'], ['nodes','⬡','Nodes'], ['agents','♙','Agents'], ['sandboxes','⬢','Sandboxes'],
  ['routines','⟳','Routines'], ['providers','⌁','Providers'], ['integrations','⊞','Integrations'], ['secrets','▣','Secrets'],
  ['evidence','◎','Evidence / Audit']
];

const pages = {
  operations: { title:'Operations', icon:'▣' }, workspaces:{title:'Workspaces',icon:'▱'}, tasks:{title:'Tasks',icon:'☑'}, projects:{title:'Projects',icon:'▢'},
  models:{title:'Models',icon:'◇'}, nodes:{title:'Nodes',icon:'⬡'}, agents:{title:'Agents',icon:'♙'}, sandboxes:{title:'Sandboxes',icon:'⬢'},
  routines:{title:'Routines',icon:'⟳'}, providers:{title:'Providers',icon:'⌁'}, integrations:{title:'Integrations',icon:'⊞'}, secrets:{title:'Secrets',icon:'▣'},
  evidence:{title:'Evidence / Audit',icon:'◎'}, settings:{title:'Settings',icon:'⚙'}
};

const state = loadState();
let activeDrawerTab = 'logs';
let lastDragWidget = null;
let suspendTimer = null;
let backgroundTimer = null;

function isPhoneLayout(){ return PHONE_MEDIA.matches; }

function defaultState(){
  return {
    theme:'system', sidebar:'expanded', inspector:'open', inspectorWidth:360, drawer:'open', drawerHeight:210, approvalLevel:'medium',
    tabs:[{id:'tab-operations',route:'operations',title:'Operations',pinned:true,state:'active',lastActive:Date.now()}],
    activeTab:'tab-operations', workspaceEdit:false, operationsEdit:false, controlChatCollapsed:false,
    operationsWidgets:[
      {id:'op-metrics',title:'System metrics',type:'metrics',x:0,y:0,width:12,height:3,col:12,row:3},
      {id:'op-tasks',title:'Active Tasks',type:'tasks',x:0,y:3,width:6,height:5,col:6,row:5},
      {id:'op-nodes',title:'Nodes',type:'nodes',x:6,y:3,width:3,height:5,col:3,row:5},
      {id:'op-resources',title:'Resource Utilisation',type:'resources',x:9,y:3,width:3,height:5,col:3,row:5},
      {id:'op-activity',title:'Recent Activity',type:'activity',x:0,y:8,width:6,height:5,col:6,row:5},
      {id:'op-attention',title:'Attention',type:'attention',x:6,y:8,width:3,height:5,col:3,row:5},
      {id:'op-providers',title:'Provider Health',type:'providers',x:9,y:8,width:3,height:5,col:3,row:5}
    ],
    workspaceWidgets:[
      {id:'ww-tasks',title:'Active Tasks',type:'tasks',col:4,row:4},
      {id:'ww-nodes',title:'Nodes',type:'nodes',col:4,row:4},
      {id:'ww-logs',title:'Live Logs',type:'logs',col:4,row:5},
      {id:'ww-models',title:'Models',type:'models',col:4,row:4},
      {id:'ww-attention',title:'Attention',type:'attention',col:4,row:3}
    ]
  };
}
function a32LegacyOperationsLayoutBroken(rows){
  if(!Array.isArray(rows)||rows.length<4)return false;
  const tiny=rows.filter(w=>Number(w.width||w.col||0)<3).length;
  const oneRow=rows.every(w=>Number(w.y||0)===0);
  return tiny>=Math.ceil(rows.length/2)||oneRow;
}
function loadState(){
  try {
    const base=defaultState(),stored=JSON.parse(localStorage.getItem(STORAGE_KEY)||'{}'),merged=Object.assign(base,stored);
    if(Number(stored.operationsLayoutVersion||0)<2&&a32LegacyOperationsLayoutBroken(stored.operationsWidgets)){
      merged.operationsWidgets=base.operationsWidgets;
      merged.operationsLayoutVersion=2;
    }else if(Number(stored.operationsLayoutVersion||0)<2){
      merged.operationsLayoutVersion=2;
    }
    return merged;
  } catch { return defaultState(); }
}
function persist(){ localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); }

const api = {
  async json(path, opts={}){
    try {
      const res = await fetch(path, {credentials:'same-origin', ...opts});
      if(!res.ok) throw new Error(`${res.status}`);
      return await res.json();
    } catch(err){ return null; }
  },
  health(){ return this.json('/v1/health'); },
  nodes(){ return this.json('/v1/nodes'); },
  nodePairings(){ return this.json('/v1/node-pairings'); },
  nodeCapabilities(id){ return this.json(`/v1/nodes/${encodeURIComponent(id)}/capabilities`); },
  modelManagement(id){ return this.json(`/v1/nodes/${encodeURIComponent(id)}/model-management`); },
  remoteModelRecommendations(id){ return this.json(`/v1/nodes/${encodeURIComponent(id)}/models/recommendations`, {method:'POST',headers:{'content-type':'application/json'},body:'{}'}); },
  chatCommands(){ return this.json('/v1/chat-commands'); },
  chatCommand(id,input){ return this.json(`/v1/chat-sessions/${encodeURIComponent(id)}/commands`, {method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({input})}); }
};

const mock = {
  tasks:[
    ['T-1832','Model qualification: Qwen 32B','AI-Workstation','68%'],['T-1831','Code generation: Feature branch','MacBook-Pro','41%'],
    ['T-1830','Research: Market analysis','Cloud (OpenRouter)','12%'],['T-1829','Testbed: Llama 70B','AI-Lab-02','Queued'],['T-1828','Data processing pipeline','Main Workstation','Waiting']
  ],
  nodes:[
    ['AI-Workstation','Online','RTX 5090 · 24 GB','good'],['MacBook-Pro','Idle','Apple Silicon · 8 GB','good'],['AI-Lab-02','Dormant','Wakeable','warn'],
    ['Media-Node','Online','16 GB','good'],['Cloud-RTX','Online','48 GB','good'],['Edge-Node','Offline','—','bad']
  ],
  providers:[['OpenAI','Healthy','14 models'],['Anthropic','Healthy','11 models'],['Gemini','Healthy','9 models'],['OpenRouter','Healthy','63 models'],['Ollama','Healthy','12 models']],
  logs:[
    ['10:42:31','INFO','model','Loading model: Qwen 32B Q4_K_M'],['10:42:28','INFO','scheduler','Allocated GPU 0 (24 GB)'],['10:42:26','INFO','runtime','Initialising llama.cpp backend'],
    ['10:42:24','DEBUG','sandbox','Environment ready'],['10:42:22','INFO','task','T-1832 started on AI-Workstation'],['10:42:21','INFO','node','Heartbeat received (latency 12 ms)'],
    ['10:41:58','INFO','watchdog','Control-plane heartbeat healthy'],['10:41:40','INFO','federation','AI-Lab-02 advertised dormant/wakeable state']
  ]
};

function init(){
  document.documentElement.dataset.theme = state.theme;
  document.documentElement.dataset.formFactor = isPhoneLayout() ? 'phone' : (innerWidth < 1100 ? 'tablet' : 'desktop');
  document.documentElement.dataset.uiVisibility = document.hidden ? 'hidden' : 'visible';
  $('#app').dataset.sidebar = state.sidebar;
  $('#app').dataset.inspector = state.inspector === 'open' ? 'open' : 'closed';
  applyInspectorWidth();
  document.documentElement.style.setProperty('--drawer', state.drawer==='open'?`${state.drawerHeight}px`:'0px');
  $('#bottomDrawer').dataset.state = state.drawer;
  renderNav(); renderTabs(); renderActiveView(); renderInspector(); renderDrawer(); bindShell(); startLifecycleTimers(); hydrateHealth();
  window.setTimeout(maybeStartProductTour, 350);
}

function renderNav(){
  const activeRoute = currentTab()?.route;
  $('#primaryNav').innerHTML = navItems.map(([route,icon,label]) => `<button class="nav-item ${route===activeRoute?'active':''}" data-route="${route}" title="${label}"><span class="nav-icon">${icon}</span><span class="nav-label">${label}</span></button>`).join('');
  renderMobileNav();
}

function renderMobileNav(){
  const activeRoute=currentTab()?.route;
  $$('[data-mobile-route]', $('#mobileNav')).forEach(b=>b.classList.toggle('active', b.dataset.mobileRoute===activeRoute));
  $('#mobileTitle').textContent=pages[activeRoute]?.title||'OnePane';
}


function renderTabs(){
  const html=state.tabs.map(t => `<button class="tab ${t.id===state.activeTab?'active':''} ${t.state==='suspended'?'suspended':''}" data-tab="${t.id}" role="tab" aria-selected="${t.id===state.activeTab}"><span>${pages[t.route]?.icon||'◫'}</span><span class="tab-label">${escapeHtml(t.title)}</span>${t.pinned?'':'<span class="tab-close" data-close-tab="'+t.id+'">×</span>'}</button>`).join('');
  $('#tabStrip').innerHTML=html;
  $('#mobileTabStrip').innerHTML=html;
  renderMobileNav();
}

function openRoute(route, {newTab=false}={}){
  const existing = state.tabs.find(t => t.route===route && !newTab);
  if(existing) return activateTab(existing.id);
  const id = `tab-${route}-${Math.random().toString(36).slice(2,7)}`;
  state.tabs.push({id,route,title:pages[route]?.title||route,pinned:false,state:'active',lastActive:Date.now()});
  activateTab(id);
}
function activateTab(id){
  const prev=currentTab();
  if(prev && prev.id!==id){ prev.state='background'; prev.backgroundAt=Date.now(); }
  state.activeTab=id;
  const t=currentTab(); if(t){ t.state='active'; t.lastActive=Date.now(); t.backgroundAt=null; }
  persist(); renderTabs(); renderNav(); renderActiveView(); scheduleTabLifecycle();
}
function closeTab(id){
  const idx=state.tabs.findIndex(t=>t.id===id); if(idx<0 || state.tabs[idx].pinned) return;
  const wasActive=state.activeTab===id; state.tabs.splice(idx,1);
  if(wasActive) state.activeTab=(state.tabs[Math.max(0,idx-1)]||state.tabs[0]).id;
  persist(); renderTabs(); renderNav(); renderActiveView();
}
function currentTab(){ return state.tabs.find(t=>t.id===state.activeTab) || state.tabs[0]; }

function startLifecycleTimers(){ setInterval(scheduleTabLifecycle, 5000); setInterval(()=>refreshOperationalDataQA(), 15000); document.addEventListener('visibilitychange', handleVisibilityChange); PHONE_MEDIA.addEventListener?.('change', handleLayoutChange); window.addEventListener('resize', handleLayoutChange, {passive:true}); }
function handleLayoutChange(){ document.documentElement.dataset.formFactor=isPhoneLayout()?'phone':(innerWidth<1100?'tablet':'desktop'); applyInspectorWidth(); renderMobileNav(); syncPanelRestoreButtons(); }
function handleVisibilityChange(){
  document.documentElement.dataset.uiVisibility=document.hidden?'hidden':'visible';
  const t=currentTab();
  if(document.hidden){ if(t){t.state='background';t.backgroundAt=Date.now();} state.uiBackgrounded=true; }
  else { state.uiBackgrounded=false; if(t){t.state='active';t.lastActive=Date.now();t.backgroundAt=null;} }
  persist(); renderTabs(); scheduleTabLifecycle();
}
function scheduleTabLifecycle(){
  const now=Date.now();
  state.tabs.forEach(t=>{
    if(t.id===state.activeTab && !document.hidden){ t.state='active'; return; }
    const age=now-(t.backgroundAt||t.lastActive||now);
    if(age>TAB_SUSPEND_MS && !t.pinned) t.state='suspended';
    else if(age>TAB_BACKGROUND_MS) t.state='background';
  });
  persist(); renderTabs();
}

function renderActiveView(){
  const t=currentTab(); if(!t) return;
  if(t.state==='suspended') t.state='active';
  const renderers={operations:renderOperations,workspaces:renderWorkspaces,tasks:()=>renderTablePage('Tasks','Canonical task queue and execution history',taskTable()),projects:()=>renderPlaceholder('Projects','Project configuration, applications, endpoints, workspace bindings and runtime state.'),models:renderModels,nodes:renderNodes,agents:renderAgents,sandboxes:renderSandboxes,routines:()=>renderPlaceholder('Routines','Scheduled and event-driven automation, triggers and execution history.'),providers:renderProviders,integrations:()=>renderPlaceholder('Integrations','External tools and SaaS connectors. Credentials remain stored separately in Secrets.'),secrets:renderSecrets,evidence:()=>renderPlaceholder('Evidence / Audit','Event Ledger, Artifacts, Observations, Verifications, Operations and CapabilityLease activity.'),settings:renderSettings};
  (renderers[t.route]||renderOperations)();
}

function pageHeader(title, subtitle, actions=''){
  return `<div class="page-heading"><div><h1>${title}</h1><div class="page-subtitle">${subtitle}</div></div><div class="page-actions">${actions}</div></div>`;
}
function renderOperations(){
  const edit=!!state.operationsEdit;
  const actions=`<button class="btn">Last 1 hour⌄</button><button class="btn ${edit?'primary':''}" id="editOperations">${edit?'Done':'Edit layout'}</button>${edit?'<button class="btn" id="addOperationsComponent">Add component</button><button class="btn" id="resetOperationsLayout">Reset layout</button>':''}`;
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Operations','System overview, activity, health and recent events',actions)}
    <div class="subtabs"><button class="subtab active">Overview</button><button class="subtab">Activity</button><button class="subtab" data-drawer-tab="logs">Logs</button><button class="subtab">Health</button><button class="subtab" data-route="nodes">Nodes</button><button class="subtab" data-route="providers">Providers</button><button class="subtab">Recovery</button></div>
    <div class="operations-layout-grid ${edit?'editing':''}" id="operationsLayout">${state.operationsWidgets.map(renderOperationsWidget).join('')}</div>
  </section>`;
  $('#editOperations').onclick=()=>{state.operationsEdit=!state.operationsEdit;persist();renderOperations();};
  $('#addOperationsComponent')?.addEventListener('click',openOperationsComponentPicker);
  $('#resetOperationsLayout')?.addEventListener('click',()=>{state.operationsWidgets=defaultState().operationsWidgets;persist();renderOperations();});
  enableOperationsLayout();
  bindViewActions($('#viewHost'));
  refreshOperationalDataQA();
}
function operationsComponentContent(type){
  const active=liveOps.tasks.filter(t=>!['complete','cancelled','failed'].includes(String(t.state||'').toLowerCase()));
  const waiting=active.filter(t=>String(t.state||'').startsWith('waiting_')).length;
  const online=liveOps.nodes.filter(n=>['online','ready','active'].includes(String(n.status||n.state||'').toLowerCase())).length;
  const connected=liveOps.providers.filter(p=>String(p.status||'').toLowerCase()==='connected').length;
  const controlState=!liveOpsReported('health')||liveOps.health==='unknown'?'Unknown':liveOps.health==='ok'?'Healthy':'Degraded';
  const attentionComplete=liveOpsAttentionReported();
  if(type==='metrics') return `<div class="metric-grid operations-metrics">${metric('System Health',controlState,controlState==='Healthy'?'Control plane responding':controlState==='Degraded'?`Reported status: ${liveOps.health}`:'Not reported',controlState==='Healthy'?'good':controlState==='Degraded'?'bad':'')}${metric('Active Tasks',liveOpsReported('tasks')?String(active.length):'Not reported',liveOpsReported('tasks')?`${active.filter(t=>t.state==='running').length} running · ${waiting} waiting`:'Task feed unavailable')}${metric('Nodes',liveOpsReported('nodes')?String(liveOps.nodes.length):'Not reported',liveOpsReported('nodes')?`${online} online/ready`:'Node feed unavailable',liveOpsReported('nodes')&&liveOps.nodes.length>0&&online===liveOps.nodes.length?'good':'')}${metric('Providers',liveOpsReported('providers')?String(liveOps.providers.length):'Not reported',liveOpsReported('providers')?`${connected} connected`:'Provider feed unavailable',liveOpsReported('providers')&&liveOps.providers.length>0&&connected===liveOps.providers.length?'good':'')}${metric('Attention',attentionComplete?String(ATTENTION_ITEMS.length):'Not reported',attentionComplete?(ATTENTION_ITEMS.length?'Requires review':'Nothing currently requires attention'):'Operational feeds incomplete',attentionComplete?(ATTENTION_ITEMS.length?'bad':'good'):'')}</div>`;
  if(type==='tasks') return taskCard();
  if(type==='scheduled') return qa4ScheduledCard();
  if(type==='nodes') return nodesCard();
  if(type==='resources') return resourceCard();
  if(type==='activity') return activityCard();
  if(type==='attention') return attentionCard();
  if(type==='providers') return providersCard();
  return card('Component','<div class="widget-body">Component unavailable.</div>');
}
function renderOperationsWidget(w){
  const controls=state.operationsEdit?`<div class="dashboard-edit-bar"><span class="dashboard-drag" title="Drag to move">⋮⋮</span><strong>${escapeHtml(w.title)}</strong><span class="dashboard-edit-spacer"></span><button class="tiny" data-op-move="up" data-op-id="${w.id}" title="Move earlier">↑</button><button class="tiny" data-op-move="down" data-op-id="${w.id}" title="Move later">↓</button><select class="dashboard-size" data-op-size data-op-id="${w.id}" aria-label="Component size"><option value="small">Small</option><option value="medium">Medium</option><option value="large">Large</option><option value="wide">Wide</option><option value="full">Full</option></select><button class="tiny danger" data-op-remove="${w.id}" title="Remove component">×</button></div>`:'';
  return `<section class="dashboard-widget ${state.operationsEdit?'editable':''}" draggable="${state.operationsEdit}" data-op-widget="${w.id}" style="grid-column:span ${Math.max(2,Math.min(12,w.col||4))};grid-row:span ${Math.max(2,Math.min(10,w.row||4))}">${controls}<div class="dashboard-widget-content">${operationsComponentContent(w.type)}</div></section>`;
}
function operationsSizePreset(name,w){
  const presets={small:[3,4],medium:[4,5],large:[6,6],wide:[8,5],full:[12,6]};
  let [col,row]=presets[name]||presets.medium;
  if(w.type==='metrics'&&name==='medium') [col,row]=[12,3];
  return {col,row};
}
function nearestOperationsSize(w){
  const candidates=['small','medium','large','wide','full'];
  let best='medium',score=Infinity;
  for(const name of candidates){const p=operationsSizePreset(name,w);const d=Math.abs((w.col||4)-p.col)+Math.abs((w.row||5)-p.row);if(d<score){score=d;best=name;}}
  return best;
}
function enableOperationsLayout(){
  if(!state.operationsEdit)return;
  $$('.dashboard-widget').forEach(el=>{
    el.addEventListener('dragstart',()=>{lastDragWidget=el.dataset.opWidget;el.classList.add('dragging');});
    el.addEventListener('dragend',()=>el.classList.remove('dragging'));
    el.addEventListener('dragover',e=>e.preventDefault());
    el.addEventListener('drop',e=>{e.preventDefault();const target=el.dataset.opWidget;if(!lastDragWidget||target===lastDragWidget)return;const a=state.operationsWidgets.findIndex(x=>x.id===lastDragWidget),b=state.operationsWidgets.findIndex(x=>x.id===target);if(a<0||b<0)return;[state.operationsWidgets[a],state.operationsWidgets[b]]=[state.operationsWidgets[b],state.operationsWidgets[a]];persist();renderOperations();});
  });
  $$('[data-op-size]').forEach(sel=>{const w=state.operationsWidgets.find(x=>x.id===sel.dataset.opId);if(w)sel.value=nearestOperationsSize(w);sel.onchange=()=>{const item=state.operationsWidgets.find(x=>x.id===sel.dataset.opId);if(!item)return;Object.assign(item,operationsSizePreset(sel.value,item));persist();renderOperations();};});
  $$('[data-op-move]').forEach(b=>b.onclick=()=>moveOperationsWidget(b.dataset.opId,b.dataset.opMove==='up'?-1:1));
  $$('[data-op-remove]').forEach(b=>b.onclick=()=>{state.operationsWidgets=state.operationsWidgets.filter(x=>x.id!==b.dataset.opRemove);persist();renderOperations();});
}
function moveOperationsWidget(id,delta){const i=state.operationsWidgets.findIndex(x=>x.id===id),j=i+delta;if(i<0||j<0||j>=state.operationsWidgets.length)return;[state.operationsWidgets[i],state.operationsWidgets[j]]=[state.operationsWidgets[j],state.operationsWidgets[i]];persist();renderOperations();}
function openOperationsComponentPicker(){
  const catalogue=[['metrics','System metrics'],['tasks','Task list'],['scheduled','Scheduled tasks'],['nodes','Nodes'],['resources','Resource Utilisation'],['activity','Recent Activity'],['attention','Attention'],['providers','Cloud Provider Health']];
  const used=new Set(state.operationsWidgets.map(x=>x.type));
  const available=catalogue.filter(([type])=>!used.has(type));
  const root=$('#overlayRoot');
  root.innerHTML=`<div class="overlay"><section class="component-picker" role="dialog" aria-modal="true" aria-label="Add Operations component"><div class="component-picker-header"><div><h2>Add component</h2><p class="page-subtitle">Choose a component for the Operations dashboard.</p></div><button class="icon-button" id="closeComponentPicker">×</button></div><div class="component-picker-grid">${available.length?available.map(([type,title])=>`<button class="component-choice" data-add-op="${type}"><strong>${title}</strong><span>${type==='metrics'?'System health and headline metrics':`Add ${title.toLowerCase()} to Operations`}</span></button>`).join(''):'<div class="empty-state compact">All available Operations components are already on the dashboard.</div>'}</div></section></div>`;
  const close=()=>root.innerHTML='';
  $('#closeComponentPicker').onclick=close;
  $$('[data-add-op]',root).forEach(b=>b.onclick=()=>{const [type,title]=catalogue.find(x=>x[0]===b.dataset.addOp);const defaults=type==='metrics'?{col:12,row:3}:{col:4,row:5};state.operationsWidgets.push({id:`op-${type}-${Date.now().toString(36)}`,title,type,...defaults});persist();close();renderOperations();});
  root.firstElementChild.onclick=e=>{if(e.target===root.firstElementChild)close();};
}
function metric(label,value,detail,cls=''){return `<div class="metric-card"><div class="metric-label">${escapeHtml(label)}</div><div class="metric-value ${cls}">${escapeHtml(value)}</div><div class="metric-detail">${escapeHtml(detail)}</div></div>`;}
function card(title,body,action=''){return `<section class="panel-card"><div class="card-header"><div class="card-title">${escapeHtml(title)}</div>${action?`<button class="card-action">${escapeHtml(action)}</button>`:''}</div>${body}</section>`;}
function taskCard(){
  if(!liveOpsReported('tasks'))return card('Active Tasks','<div class="empty-state compact">Task status not reported.</div>','View all');
  const rows=liveOps.tasks.filter(t=>!['complete','cancelled'].includes(String(t.state||'').toLowerCase())).slice(0,6);
  return card('Active Tasks',rows.length?`<ul class="list">${rows.map(t=>`<li class="list-row" data-route="tasks"><span class="pill ${['failed','blocked','waiting_approval'].includes(t.state)?'warn':'good'}">${escapeHtml(t.state||'created')}</span><div class="list-main"><div class="list-title">${escapeHtml(t.objective||t.id||'Task')}</div><div class="list-meta">${escapeHtml(t.scheduling_class||'')} · priority ${Number(t.priority||0)}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No active tasks.</div>','View all');
}
function nodesCard(){
  if(!liveOpsReported('nodes'))return card('Nodes','<div class="empty-state compact">Node status not reported.</div>','View all');
  return card('Nodes',liveOps.nodes.length?`<ul class="list">${liveOps.nodes.slice(0,6).map(n=>`<li class="list-row" data-route="nodes"><span>⬡</span><div class="list-main"><div class="list-title">${escapeHtml(n.display_name||n.node_id||n.id||'Node')}</div><div class="list-meta">${escapeHtml(n.peer_endpoint||n.advertise_url||'local')}</div></div><span class="pill">${escapeHtml(n.status||n.state||n.trust_state||'registered')}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No node records returned.</div>','View all');
}
function resourceCard(){
  if(!localProfileQA)return card('Resource Utilisation','<div class="empty-state compact">Live capacity telemetry is not fabricated. Use Models → Detect hardware to load this node’s hardware profile.</div>');
  const g=Array.isArray(localProfileQA.gpus)?localProfileQA.gpus:[];
  return card('Resource Utilisation',`<div class="widget-body"><strong>${escapeHtml(localProfileQA.cpu?.name||'CPU')}</strong><div class="list-meta">${escapeHtml(bytesQA(localProfileQA.memory?.total_bytes||0))} RAM</div>${g.map(x=>`<div class="resource-row"><span>${escapeHtml(x.name||'GPU')}</span><strong>${escapeHtml(bytesQA(x.vram_bytes||0))} VRAM</strong></div>`).join('')||'<div class="list-meta">No GPU detected.</div>'}</div>`);
}
function eventLabel(e){return String(e.event_type||e.Type||'event').replaceAll('.',' · ').replaceAll('_',' ');}
function eventTime(e){const ts=Number(e.occurred_at||e.OccurredAt||0);if(!ts)return '';return new Date(ts).toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});}
function activityCard(){
  const rows=[...liveOps.events].slice(-8).reverse();
  return card('Recent Activity',rows.length?`<ul class="list">${rows.map(e=>`<li class="list-row"><span>◉</span><div class="list-main"><div>${escapeHtml(eventLabel(e))}</div><div class="list-meta">${escapeHtml(e.aggregate_type||e.AggregateType||'')} · ${escapeHtml(e.aggregate_id||e.AggregateID||'')}</div></div><span class="list-meta">${escapeHtml(eventTime(e))}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No recent workspace events.</div>','View all');
}
function attentionCard(){
  if(!liveOpsAttentionReported())return card('Attention','<div class="empty-state compact">Attention status not fully reported because one or more operational feeds are unavailable.</div>');
  return card(`Attention (${ATTENTION_ITEMS.length})`,ATTENTION_ITEMS.length?`<ul class="list">${ATTENTION_ITEMS.map((n,i)=>`<li class="list-row"><span class="pill ${n.severity||'warn'}">!</span><div class="list-main"><div>${escapeHtml(n.title)}</div><div class="list-meta">${escapeHtml(n.detail)}</div></div><button class="btn" data-attention-item="${i}">${n.drawer?'View logs':'Open'}</button></li>`).join('')}</ul>`:'<div class="empty-state compact">Nothing currently requires approval or intervention.</div>');
}
function providersCard(){
  if(!liveOpsReported('providers'))return card('Provider Health','<div class="empty-state compact">Provider status not reported.</div>','View all');
  return card('Provider Health',liveOps.providers.length?`<ul class="list">${liveOps.providers.slice(0,8).map(p=>`<li class="list-row" data-route="providers"><span>⌁</span><div class="list-main"><div>${escapeHtml(p.display_name||p.provider||'Provider')}</div></div><span class="pill ${p.status==='connected'?'good':p.status==='unavailable'?'bad':''}">${escapeHtml(p.status||'configured')}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No providers configured.</div>','View all');
}
function renderWorkspaces(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Workspaces','Composable operational views with draggable, resizable components',`<button class="btn ${state.workspaceEdit?'primary':''}" id="editWorkspace">${state.workspaceEdit?'Done':'Edit layout'}</button><button class="btn">Add component</button>`)}
    <div class="workspace-toolbar"><select><option>Development</option><option>AI Lab</option><option>Infrastructure</option></select><button class="btn">Duplicate</button><button class="btn">Save preset</button></div>
    <div class="workspace-grid" id="workspaceGrid">${state.workspaceWidgets.map(renderWorkspaceWidget).join('')}</div></section>`;
  $('#editWorkspace').onclick=()=>{state.workspaceEdit=!state.workspaceEdit;persist();renderWorkspaces();};
  enableWorkspaceDrag(); bindViewActions();
}
function renderWorkspaceWidget(w){
  const content={tasks:'Task queue · 7 active',nodes:'5 online · 2 dormant · 1 offline',logs:'Streaming scheduler / watchdog / node events',models:'Local · Remote · Cloud · Testbed',attention:'3 items need human attention'}[w.type]||'Widget';
  const mobileSize=w.mobileSize||'medium';
  const mobileControls=state.workspaceEdit?`<div class="widget-mobile-controls" aria-label="Mobile widget controls"><button data-widget-move="up" data-widget-id="${w.id}">↑ Move</button><button data-widget-move="down" data-widget-id="${w.id}">↓ Move</button>${['small','medium','large','full'].map(size=>`<button data-widget-size="${size}" data-widget-id="${w.id}" aria-pressed="${mobileSize===size}">${titleCase(size)}</button>`).join('')}</div>`:'';
  return `<section class="workspace-widget" draggable="${state.workspaceEdit}" data-widget="${w.id}" data-mobile-size="${mobileSize}" style="grid-column:span ${w.col};grid-row:span ${w.row}"><div class="widget-handle"><strong>${w.title}</strong><span>${state.workspaceEdit?'⋮⋮':''}</span></div><div class="widget-body">${content}<br><br>${state.workspaceEdit?(isPhoneLayout()?'Use Move and size controls below.':'Drag to reorder. Resize from the lower-right corner.'):'Open or filter this component from its contextual menu.'}</div>${mobileControls}</section>`;
}
function enableWorkspaceDrag(){
  if(!state.workspaceEdit) return;
  $$('.workspace-widget').forEach(el=>{
    el.addEventListener('dragstart',()=>{lastDragWidget=el.dataset.widget;el.classList.add('dragging');});
    el.addEventListener('dragend',()=>el.classList.remove('dragging'));
    el.addEventListener('dragover',e=>e.preventDefault());
    el.addEventListener('drop',e=>{e.preventDefault(); const target=el.dataset.widget; if(!lastDragWidget||target===lastDragWidget)return; const a=state.workspaceWidgets.findIndex(x=>x.id===lastDragWidget),b=state.workspaceWidgets.findIndex(x=>x.id===target); [state.workspaceWidgets[a],state.workspaceWidgets[b]]=[state.workspaceWidgets[b],state.workspaceWidgets[a]]; persist();renderWorkspaces();});
  });
  $$('[data-widget-size]').forEach(b=>b.onclick=()=>{const w=state.workspaceWidgets.find(x=>x.id===b.dataset.widgetId);if(!w)return;w.mobileSize=b.dataset.widgetSize;persist();renderWorkspaces();});
  $$('[data-widget-move]').forEach(b=>b.onclick=()=>moveWorkspaceWidget(b.dataset.widgetId,b.dataset.widgetMove==='up'?-1:1));
}
function moveWorkspaceWidget(id,delta){const i=state.workspaceWidgets.findIndex(x=>x.id===id),j=i+delta;if(i<0||j<0||j>=state.workspaceWidgets.length)return;[state.workspaceWidgets[i],state.workspaceWidgets[j]]=[state.workspaceWidgets[j],state.workspaceWidgets[i]];persist();renderWorkspaces();}

function taskTable(){return `<div class="table-shell"><table class="data-table"><thead><tr><th>Task</th><th>Status</th><th>Runtime</th><th>Node</th><th>Progress</th></tr></thead><tbody>${mock.tasks.map((t,i)=>`<tr data-inspect="task"><td>${t[0]} · ${t[1]}</td><td><span class="pill ${i<3?'good':'warn'}">${i<3?'Running':'Queued'}</span></td><td>${i%2?'Claude Code':'Model'}</td><td>${t[2]}</td><td>${t[3]}</td></tr>`).join('')}</tbody></table></div>`;}
function renderTablePage(title,subtitle,body){$('#viewHost').innerHTML=`<section class="page">${pageHeader(title,subtitle,'<button class="btn">Filter</button><button class="btn primary">New</button>')}${body}</section>`;bindViewActions();}
function renderModels(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Models','Local, remote and cloud models with hardware fit and empirical qualification','<button class="btn">Refresh catalogue</button><button class="btn primary">Install model</button>')}<div class="subtabs"><button class="subtab active">Installed</button><button class="subtab">Available</button><button class="subtab">Recommended</button><button class="subtab">Remote</button><button class="subtab">Cloud</button><button class="subtab">Testbed</button></div>
  <div class="table-shell"><table class="data-table"><thead><tr><th>Model</th><th>Source</th><th>Placement</th><th>Context</th><th>Qualification</th><th>Measured</th></tr></thead><tbody>
  <tr><td>Qwen 32B Q4_K_M</td><td>Local · AI-Workstation</td><td>RTX 5090</td><td>64K</td><td><span class="pill good">Qualified</span></td><td>74 tok/s</td></tr>
  <tr><td>Llama 70B Q4</td><td>Remote · AI-Lab-02</td><td>Dormant · Wakeable</td><td>32K</td><td><span class="pill good">Qualified</span></td><td>31 tok/s</td></tr>
  <tr><td>GPT-5.6</td><td>Cloud · OpenAI</td><td>Provider</td><td>Managed</td><td><span class="pill">Provider</span></td><td>—</td></tr>
  </tbody></table></div></section>`;bindViewActions();
}
function renderNodes(){renderTablePage('Nodes','Federated compute, hardware, power state and remote model capacity',`<div class="table-shell"><table class="data-table"><thead><tr><th>Node</th><th>State</th><th>OS</th><th>Compute</th><th>Wake</th><th>Last seen</th></tr></thead><tbody>${mock.nodes.map((n,i)=>`<tr data-inspect-node="${n[0]}"><td>${n[0]}</td><td><span class="pill ${n[3]}">${n[1]}</span></td><td>${i===1?'macOS':i===0?'Windows':'Linux'}</td><td>${n[2]}</td><td>${n[1]==='Dormant'?'WoL':'—'}</td><td>${i<2?'12 sec ago':'1 min ago'}</td></tr>`).join('')}</tbody></table></div>`);}
function renderProviders(){renderTablePage('Providers','Connect and monitor AI inference providers',`<div class="table-shell"><table class="data-table"><thead><tr><th>Provider</th><th>Status</th><th>Models</th><th>Credential</th><th>Health</th></tr></thead><tbody>${mock.providers.map(p=>`<tr><td>${p[0]}</td><td><span class="pill good">Connected</span></td><td>${p[2]}</td><td>Vault-backed</td><td>${p[1]}</td></tr>`).join('')}</tbody></table></div>`);}
function renderSecrets(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Secrets','Canonical Vault management for API keys, tokens, certificates and service credentials','<button class="btn">Namespaces</button><button class="btn primary">Add secret</button>')}<div class="table-shell"><table class="data-table"><thead><tr><th>Secret reference</th><th>Namespace</th><th>Used by</th><th>Last used</th><th>Status</th></tr></thead><tbody><tr><td>provider/openai/api-key</td><td>provider</td><td>OpenAI</td><td>2 min ago</td><td><span class="pill good">Stored</span></td></tr><tr><td>plugin/github/access-token</td><td>plugin</td><td>GitHub</td><td>1 hr ago</td><td><span class="pill good">Stored</span></td></tr><tr><td>agent-runtime/claude-code/access-token</td><td>agent-runtime</td><td>Claude Code</td><td>8 min ago</td><td><span class="pill good">Stored</span></td></tr></tbody></table></div><div class="page-subtitle" style="margin-top:10px">Secret values are write-only after storage and are never rendered back into the UI.</div></section>`;
}
function renderSandboxes(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Sandboxes','Reusable execution environments with explicit network, host, secret and device access','<button class="btn primary">New sandbox profile</button>')}<div class="table-shell"><table class="data-table"><thead><tr><th>Profile</th><th>Internet</th><th>LAN</th><th>Host files</th><th>GPU</th><th>Secrets</th></tr></thead><tbody><tr><td>dev-standard</td><td>Outbound</td><td>Blocked</td><td>Workspace RW</td><td>Allowed</td><td>Selected only</td></tr><tr><td>research-restricted</td><td>HTTP/S</td><td>Blocked</td><td>Workspace RO</td><td>None</td><td>None</td></tr><tr><td>trusted-admin</td><td>Full outbound</td><td>Restricted</td><td>Explicit mounts</td><td>Allowed</td><td>Selected only</td></tr></tbody></table></div></section>`;
}
function renderSettings(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Settings','Application preferences and defaults inherited by new workspaces')}<div class="operations-grid"><section class="panel-card"><div class="card-header"><div class="card-title">Appearance</div></div><div class="widget-body">Theme: ${state.theme}<br><br>Density: Comfortable<br><br>Phone layout: Native responsive · automatic<br><br>Inactive tab suspension: Balanced<br><br><button class="btn" data-action="product-tour">Restart product tour</button></div></section><section class="panel-card"><div class="card-header"><div class="card-title">Security & Approvals</div><span class="pill good" style="margin-left:auto">Recommended: Medium</span></div><div class="widget-body"><p class="page-subtitle">Approval strictness controls how often OnePane asks before an operation that you are already authorized to approve.</p><div class="approval-profile-grid"><button class="approval-profile ${state.approvalLevel==='high'?'selected':''}" data-approval-default="high"><strong>High</strong><span>Prompt for every approval-class operation.</span></button><button class="approval-profile ${state.approvalLevel==='medium'?'selected':''}" data-approval-default="medium"><strong>Medium</strong><span>Auto-approve low-risk actions; prompt for medium, high and critical.</span></button><button class="approval-profile ${state.approvalLevel==='low'?'selected':''}" data-approval-default="low"><strong>Low</strong><span>Auto-approve low and medium risk; prompt for high and critical.</span></button></div><div class="page-subtitle" style="margin-top:10px">YOLO remains session-only and auto-approves all actions you are already authorized to approve. Hard policy denials and unknown-outcome protections still apply.</div></div></section><section class="panel-card"><div class="card-header"><div class="card-title">Performance</div></div><div class="widget-body">Suspend inactive tabs after 5 minutes<br><br>Pause background telemetry ✓<br><br>Keep pinned tabs warm ✓</div></section><section class="panel-card"><div class="card-header"><div class="card-title">Federation</div></div><div class="widget-body">Node discovery: Enabled<br><br>Remote inference: Enabled<br><br>Wake-to-execute: Enabled</div></section></div></section>`; bindViewActions($('#viewHost'));
}
function renderPlaceholder(title,copy){$('#viewHost').innerHTML=`<section class="page">${pageHeader(title,copy,'<button class="btn">Configure</button>')}<div class="empty-state"><div><div style="font-size:36px;opacity:.5">${pages[currentTab().route]?.icon||'◫'}</div><h2>${title}</h2><p>${copy}</p><p>This canonical management surface is scaffolded into the new shell.</p></div></div></section>`;}





function bindShell(){
  $('#sidebarToggle').onclick=()=>setSidebarExpanded(state.sidebar!=='expanded');
  $('#primaryNav').addEventListener('click',e=>{const b=e.target.closest('[data-route]');if(b)openRoute(b.dataset.route);});
  $('#tabStrip').addEventListener('click',e=>{const close=e.target.closest('[data-close-tab]');if(close){e.stopPropagation();return closeTab(close.dataset.closeTab);} const t=e.target.closest('[data-tab]');if(t)activateTab(t.dataset.tab);});
  $('#newTabButton').onclick=()=>openRoute('operations',{newTab:true});
  $('#mobileNav').addEventListener('click',e=>{const route=e.target.closest('[data-mobile-route]');if(route)return openRoute(route.dataset.mobileRoute);if(e.target.closest('[data-mobile-more]'))openMobileMore();});
  $('#mobileTabStrip').addEventListener('click',e=>{const close=e.target.closest('[data-close-tab]');if(close){e.stopPropagation();return closeTab(close.dataset.closeTab);}const t=e.target.closest('[data-tab]');if(t)activateTab(t.dataset.tab);});
  $('#mobileHeaderMore').onclick=openMobileMore;
  $('#mobileAttentionButton').onclick=e=>openAttentionPopover(e.currentTarget);
  $('#mobileHealthButton').onclick=e=>openHealthPopover(e.currentTarget);
  $('#drawerToggle').onclick=()=>{state.drawer='closed';$('#bottomDrawer').dataset.state='closed';document.documentElement.style.setProperty('--drawer','0px');persist();};
  $('#commandButton').onclick=openCommandPalette; $('#themeButton').onclick=e=>openThemePopover(e.currentTarget); $('#attentionButton').onclick=e=>openAttentionPopover(e.currentTarget); $('#healthButton').onclick=e=>openHealthPopover(e.currentTarget);
  $('#sidebarToggle').addEventListener('dblclick',()=>openRoute('operations'));
  document.addEventListener('keydown',e=>{if((e.metaKey||e.ctrlKey)&&e.key.toLowerCase()==='k'){e.preventDefault();openCommandPalette();} if(e.key==='Escape')$('#overlayRoot').innerHTML='';});
  document.addEventListener('click',e=>{const command=e.target.closest('[data-action="command-palette"]');if(command)openCommandPalette(); const tour=e.target.closest('[data-action="product-tour"]');if(tour)startProductTour({replay:true}); const mobileMore=e.target.closest('[data-action="mobile-more"]');if(mobileMore)openMobileMore(); const route=e.target.closest('.mobile-header [data-route]');if(route)openRoute(route.dataset.route);});
  bindDrawerResize();
}

function renderTaskInspector(){
  $('#inspector').innerHTML=`<div class="inspector-header">Task <button class="inspector-close" id="closeInspector">×</button></div><div class="inspector-section"><div class="inspector-title">☑ T-1832 <span class="pill good" style="margin-left:auto">Running</span></div><div class="page-subtitle">Model qualification: Qwen 32B</div></div><div class="inspector-tabs"><button class="active">Overview</button><button>Attempts</button><button>Evidence</button><button data-drawer-tab="logs">Logs</button></div><div class="inspector-section"><dl class="definition-grid"><dt>Workspace</dt><dd>AI Lab</dd><dt>Agent</dt><dd>Local model worker</dd><dt>Model</dt><dd>Qwen 32B Q4_K_M</dd><dt>Node</dt><dd>AI-Workstation</dd><dt>Progress</dt><dd>68%</dd><dt>Verification</dt><dd>Pending</dd></dl></div>`;
  $('#closeInspector').onclick=()=>{state.inspector='closed';$('#app').dataset.inspector='closed';persist();}; bindViewActions($('#inspector'));
}
function bindDrawerResize(){
  const handle=$('#drawerResizer'); let startY=0,startH=0;
  handle.onpointerdown=e=>{if(state.drawer!=='open')return;startY=e.clientY;startH=state.drawerHeight;handle.setPointerCapture(e.pointerId);};
  handle.onpointermove=e=>{if(!handle.hasPointerCapture(e.pointerId))return; state.drawerHeight=Math.max(120,Math.min(520,startH+(startY-e.clientY))); document.documentElement.style.setProperty('--drawer',`${state.drawerHeight}px`);};
  handle.onpointerup=e=>{if(handle.hasPointerCapture(e.pointerId))handle.releasePointerCapture(e.pointerId);persist();};
}
function inspectorWidthCap(){return Math.max(280,Math.min(720,Math.floor(innerWidth*0.48)));}
function applyInspectorWidth(){
  const cap=inspectorWidthCap(),width=Math.max(280,Math.min(cap,Number(state.inspectorWidth)||360));
  state.inspectorWidth=width;
  document.documentElement.style.setProperty('--inspector-open',`${width}px`);
}
function bindInspectorResize(){
  const handle=$('#inspectorResizer');if(!handle)return;let startX=0,startW=0;
  handle.onpointerdown=e=>{if(state.inspector!=='open'||isPhoneLayout())return;startX=e.clientX;startW=Number(state.inspectorWidth)||360;handle.setPointerCapture(e.pointerId);document.documentElement.dataset.resizingInspector='true';};
  handle.onpointermove=e=>{if(!handle.hasPointerCapture(e.pointerId))return;state.inspectorWidth=Math.max(280,Math.min(inspectorWidthCap(),startW+(startX-e.clientX)));applyInspectorWidth();};
  handle.onpointerup=e=>{if(handle.hasPointerCapture(e.pointerId))handle.releasePointerCapture(e.pointerId);delete document.documentElement.dataset.resizingInspector;persist();};
  handle.ondblclick=()=>{state.inspectorWidth=360;applyInspectorWidth();persist();};
}

function openMobileMore(){
  const root=$('#overlayRoot');
  const active=currentTab()?.route;
  const extras=navItems.filter(([route])=>!['operations','tasks','agents'].includes(route));
  root.innerHTML=`<div class="mobile-sheet-overlay"><section class="mobile-sheet" role="dialog" aria-modal="true" aria-label="OnePane navigation"><div class="mobile-sheet-handle"></div><div class="mobile-sheet-title">OnePane <button class="icon-button" id="mobileSheetClose" aria-label="Close">×</button></div><div class="mobile-menu-section">Navigate</div><div class="mobile-menu-grid">${extras.map(([route,icon,label])=>`<button class="mobile-menu-item ${route===active?'active':''}" data-mobile-sheet-route="${route}"><span class="nav-icon">${icon}</span><span>${label}</span></button>`).join('')}<button class="mobile-menu-item ${active==='settings'?'active':''}" data-mobile-sheet-route="settings"><span class="nav-icon">⚙</span><span>Settings</span></button></div><div class="mobile-menu-section">Quick actions</div><div class="mobile-menu-grid"><button class="mobile-menu-item" data-mobile-sheet-action="command"><span class="nav-icon">⌕</span><span>Command palette</span></button><button class="mobile-menu-item" data-mobile-sheet-action="tour"><span class="nav-icon">?</span><span>Product tour</span></button><button class="mobile-menu-item" data-mobile-sheet-action="theme"><span class="nav-icon">◐</span><span>Theme</span></button></div></section></div>`;
  const close=()=>root.innerHTML='';
  $('#mobileSheetClose').onclick=close;
  $$('.mobile-menu-item[data-mobile-sheet-route]',root).forEach(b=>b.onclick=()=>{const route=b.dataset.mobileSheetRoute;close();openRoute(route);});
  $$('[data-mobile-sheet-action]',root).forEach(b=>b.onclick=()=>{const a=b.dataset.mobileSheetAction;close();if(a==='command')openCommandPalette();else if(a==='tour')startProductTour({replay:true});else if(a==='theme')openThemePopover($('#mobileHeaderMore'));});
  root.firstElementChild.onclick=e=>{if(e.target===root.firstElementChild)close();};
}

function openCommandPalette(){
  const root=$('#overlayRoot');
  root.innerHTML=`<div class="overlay"><div class="palette"><input id="paletteInput" placeholder="Search pages, commands, nodes, models…" autofocus><div class="palette-results" id="paletteResults"></div></div></div>`;
  const commands=[...navItems.map(([route,icon,label])=>({label,meta:'Page',run:()=>openRoute(route)})),{label:'Wake AI-Workstation',meta:'Node action',run:()=>{}},{label:'Open Testbed',meta:'Models',run:()=>openRoute('models')},{label:'Add secret',meta:'Vault',run:()=>openRoute('secrets')},{label:'Create workspace',meta:'Workspace',run:()=>openRoute('workspaces')}];
  const input=$('#paletteInput'),results=$('#paletteResults');
  const draw=()=>{const q=input.value.toLowerCase(); const filtered=commands.filter(c=>c.label.toLowerCase().includes(q)); results.innerHTML=filtered.map((c,i)=>`<button class="palette-item ${i===0?'selected':''}" data-command="${commands.indexOf(c)}"><span>›</span><strong>${c.label}</strong><span>${c.meta}</span></button>`).join(''); $$('.palette-item',results).forEach(b=>b.onclick=()=>{commands[+b.dataset.command].run();root.innerHTML='';});};
  input.oninput=draw; draw(); input.focus(); root.firstElementChild.onclick=e=>{if(e.target===root.firstElementChild)root.innerHTML='';};
}
let activePopoverCleanup=null;
function closePopover(){
  if(activePopoverCleanup){const cleanup=activePopoverCleanup;activePopoverCleanup=null;cleanup();}
}
function popoverFor(anchor,html){
  closePopover();
  const root=$('#overlayRoot');
  root.innerHTML=html;
  const p=root.firstElementChild,r=anchor.getBoundingClientRect();
  if(isPhoneLayout())p.classList.add('mobile-popover');
  else{p.style.top=`${r.bottom+8}px`;p.style.right=`${Math.max(8,innerWidth-r.right)}px`;}
  const closer=e=>{
    if(!p.isConnected){closePopover();return;}
    if(!p.contains(e.target)&&e.target!==anchor){root.innerHTML='';closePopover();}
  };
  const timer=setTimeout(()=>document.addEventListener('click',closer,{capture:true}),0);
  activePopoverCleanup=()=>{clearTimeout(timer);document.removeEventListener('click',closer,{capture:true});};
}
function openThemePopover(anchor){popoverFor(anchor,`<div class="popover"><h3>Theme</h3><div class="theme-grid">${['system','light','dark','graphite','midnight','forest'].map(t=>`<button class="theme-choice ${state.theme===t?'active':''}" data-theme-choice="${t}">${titleCase(t)}</button>`).join('')}</div></div>`);$$('[data-theme-choice]').forEach(b=>b.onclick=()=>{state.theme=b.dataset.themeChoice;document.documentElement.dataset.theme=state.theme;persist();$('#overlayRoot').innerHTML='';});}
async function openAttentionPopover(anchor){
  try{await refreshOperationalDataQA(true)}catch{}
  const items=a31Array(ATTENTION_ITEMS),complete=liveOpsAttentionReported();
  const rows=items.slice(0,8).map(item=>`<div class="popover-row"><strong>${escapeHtml(item.title||'Attention item')}</strong><div class="list-meta">${escapeHtml(item.detail||'No detail reported')}</div></div>`).join('');
  const empty=complete?'<div class="popover-row"><strong>No attention items reported.</strong><div class="list-meta">The current task, provider, node, and event feeds report no conditions requiring attention.</div></div>':'<div class="popover-row"><strong>Attention status not fully reported.</strong><div class="list-meta">One or more operational feeds are unavailable, so OnePane will not infer a healthy zero.</div></div>';
  popoverFor(anchor,`<div class="popover"><h3>Attention</h3>${rows||empty}</div>`);
}
async function openHealthPopover(anchor){
  try{await refreshOperationalDataQA(true)}catch{}
  const nodeRows=a31Array(liveOps.nodes),providerRows=a31Array(liveOps.providers);
  const healthyNodes=nodeRows.filter(n=>['online','ready','active'].includes(String(n.status||n.state||'').toLowerCase())).length;
  const healthyProviders=providerRows.filter(p=>['connected','ready'].includes(String(p.status||'').toLowerCase())).length;
  const watchdogSeen=liveOpsReported('events')?[...a31Array(liveOps.events)].reverse().find(e=>String(e.aggregate_type||'').toLowerCase()==='watchdog'||/watchdog/i.test(String(e.event_type||''))):null;
  const controlState=!liveOpsReported('health')||liveOps.health==='unknown'?'Unknown':liveOps.health==='ok'?'Healthy':'Degraded';
  const controlKind=controlState==='Healthy'?'good':controlState==='Degraded'?'bad':'warn';
  const row=(label,value,kind='')=>`<div class="popover-row health-popover-row"><span>${escapeHtml(label)}</span><span class="${kind}">${escapeHtml(value)}</span></div>`;
  const federation=!liveOpsReported('nodes')?'Not reported':nodeRows.length?`${healthyNodes} / ${nodeRows.length} healthy`:'No nodes reported';
  const providers=!liveOpsReported('providers')?'Not reported':providerRows.length?`${healthyProviders} / ${providerRows.length} connected`:'No providers reported';
  const watchdog=!liveOpsReported('events')?'Not reported':watchdogSeen?'Observed':'Not observed in recent events';
  popoverFor(anchor,`<div class="popover"><h3>System Health</h3>${row('Control plane',controlState,controlKind)}${row('Watchdog',watchdog,watchdogSeen?'good':'')}${row('Database','Not reported')}${row('Federation',federation,liveOpsReported('nodes')&&nodeRows.length&&healthyNodes===nodeRows.length?'good':'')}${row('Providers',providers,liveOpsReported('providers')&&providerRows.length&&healthyProviders===providerRows.length?'good':'')}</div>`);
}


function productTourCompleted(){return ['1',TOUR_COMPLETE_VALUE].includes(localStorage.getItem(TOUR_KEY));}
function maybeStartProductTour(){
  if(productTourCompleted()) return;
  startProductTour({welcome:true});
}

function startProductTour({replay=false,welcome=false}={}){
  const mobile=isPhoneLayout();
  const app=$('#app');
  const drawer=$('#bottomDrawer');
  const originalChrome={
    sidebar:app?.dataset.sidebar||state.sidebar,
    inspector:app?.dataset.inspector||state.inspector,
    drawer:drawer?.dataset.state||state.drawer,
    drawerHeight:getComputedStyle(document.documentElement).getPropertyValue('--drawer')
  };
  const openSidebarForTour=()=>{if(!mobile&&app)app.dataset.sidebar='expanded';};
  const openInspectorForTour=()=>{
    if(!app)return;
    app.dataset.inspector='open';
    if(mobile&&drawer)drawer.dataset.state='closed';
    renderInspector();
  };
  const openDrawerForTour=()=>{
    if(!drawer)return;
    if(mobile&&app)app.dataset.inspector='closed';
    drawer.dataset.state='open';
    document.documentElement.style.setProperty('--drawer',`${Math.max(150,state.drawerHeight||220)}px`);
    renderDrawer();
  };
  const steps=[
    {title:'Welcome to OnePane',target:null,body:'OnePane brings tasks, models, agents, nodes and applications into one control plane. This short tour shows you where the important controls live.'},
    {title:'Operations',target:'#viewHost .metric-grid',fallback:'#viewHost .page-heading',placement:'below',padding:10,body:'Operations is your default home. It summarises health, active work, nodes, providers, Watchdog state, attention items and recent activity.'},
    {title:'Navigation',target:mobile?'#mobileNav':'.sidebar',placement:mobile?'above':'right',padding:6,prepare:openSidebarForTour,body:mobile?'Use the bottom bar for Operations, Tasks and Chat. More opens the rest of OnePane without crowding the screen.':'Canonical pages live here: Projects, Tasks, Models, Nodes, Agents, Integrations, Secrets and audit. Sandbox controls live with each Project instead of being a separate destination.'},
    {title:'Hot Swap keeps tasks alive',target:null,body:'Managed Hot Swap is a OnePane capability across compatible runtimes. OnePane can preserve a task while changing models or execution targets, including slower large-model transitions, instead of failing the task just because the inference backend changes.'},
    {title:'Colibri large-model runtime',target:null,body:'Colibri is bundled as a managed component. It can use local GPU/model resources or qualified Colibri-capable OnePane nodes for larger models. Its sandbox permits outbound networking and trusted-node connectivity, while node enrollment, qualification and policy remain under OnePane control.'},
    {title:'OmniRoute provider routing',target:null,body:'OmniRoute is bundled as a managed component for provider and model routing. Its sandbox has outbound network access and brokered credentials. Colibri and OmniRoute can be enabled or disabled independently without disabling Hot Swap or OnePane.'},
    {title:'Resource-aware tabs',target:mobile?'#mobileTabStrip':'#tabStrip',placement:'below',padding:6,body:'Open pages and workspaces as tabs. Inactive tabs automatically background and suspend expensive UI work while backend tasks continue running.'},
    {title:'Attention',target:mobile?'#mobileAttentionButton':'#attentionButton',placement:'below',padding:8,body:'Approvals, blocked work, failures and other items that need you surface here instead of taking over the main navigation.'},
    {title:'Inspector',target:'#inspector',placement:mobile?'above':'left',padding:8,prepare:openInspectorForTour,body:mobile?'Select a Task, Model or Node and its Inspector slides up as a touch-friendly sheet.':'Select any Operations item, Task, Model, Node, Project or Workspace and its contextual details open here. Add Notepad or Model chat tabs without leaving your current view.'},
    {title:'Observability drawer',target:'#bottomDrawer',placement:'above',padding:8,prepare:openDrawerForTour,body:mobile?'Logs, Events, Watchdog, Metrics, Evidence and Terminal open as a swipe-friendly bottom sheet.':'Logs, Events, Watchdog, Metrics, Evidence and Terminal views live in this resizable drawer. Hidden or suspended views stop unnecessary rendering work.'},
    {title:'Command palette and chat commands',target:mobile?'#mobileHeaderMore':'#commandButton',placement:'below',padding:8,body:mobile?'Use More → Command palette to jump around OnePane. In chats, type / for commands such as /queue, /steer, /model and /approvals.':'Use Ctrl+K to jump around OnePane. In chats, type / for session commands such as /queue, /steer, /model and /approvals.'},
    {title:'You’re ready',target:null,body:'Start in Operations, create a Project and Workspace, then choose local/cloud models and connect any direct cloud providers you need. You can replay this tour any time from Help / Tour or Settings.'}
  ];
  let index=0;
  let activeTarget=null;
  const root=$('#overlayRoot');
  const limit=(value,min,max)=>Math.min(Math.max(value,min),Math.max(min,max));
  const restoreChrome=()=>{
    if(app){app.dataset.sidebar=originalChrome.sidebar;app.dataset.inspector=originalChrome.inspector;}
    if(drawer)drawer.dataset.state=originalChrome.drawer;
    if(originalChrome.drawerHeight)document.documentElement.style.setProperty('--drawer',originalChrome.drawerHeight);
  };
  const getTarget=step=>{
    for(const selector of [step.target,step.fallback]){
      if(!selector)continue;
      const el=$(selector);
      if(!el)continue;
      const rect=el.getBoundingClientRect();
      const style=getComputedStyle(el);
      if(rect.width>2&&rect.height>2&&style.display!=='none'&&style.visibility!=='hidden')return el;
    }
    return null;
  };
  const positionTourStep=()=>{
    const overlay=$('.tour-overlay',root);
    const card=$('#tourCard',root);
    const spotlight=$('#tourSpotlight',root);
    if(!overlay||!card||!spotlight)return;
    const step=steps[index];
    activeTarget=getTarget(step);
    if(!activeTarget){
      overlay.dataset.focus='none';
      spotlight.hidden=true;
      card.dataset.placement='center';
      card.style.left='50%';
      card.style.top='50%';
      card.style.right='auto';
      card.style.bottom='auto';
      card.style.transform='translate(-50%,-50%)';
      card.style.visibility='visible';
      return;
    }
    activeTarget.scrollIntoView({block:'nearest',inline:'nearest',behavior:'auto'});
    const rect=activeTarget.getBoundingClientRect();
    const pad=step.padding??8;
    const margin=10;
    const left=Math.max(margin,rect.left-pad);
    const top=Math.max(margin,rect.top-pad);
    const right=Math.min(innerWidth-margin,rect.right+pad);
    const bottom=Math.min(innerHeight-margin,rect.bottom+pad);
    overlay.dataset.focus='target';
    spotlight.hidden=false;
    spotlight.style.left=`${left}px`;
    spotlight.style.top=`${top}px`;
    spotlight.style.width=`${Math.max(8,right-left)}px`;
    spotlight.style.height=`${Math.max(8,bottom-top)}px`;
    spotlight.style.borderRadius=getComputedStyle(activeTarget).borderRadius||'12px';

    card.style.visibility='hidden';
    card.style.transform='none';
    card.style.right='auto';
    card.style.bottom='auto';
    card.style.left='0';
    card.style.top='0';
    const cardRect=card.getBoundingClientRect();
    const gap=16;
    const vw=innerWidth,vh=innerHeight;
    const candidates={
      right:{left:right+gap,top:limit(top,margin,vh-cardRect.height-margin)},
      left:{left:left-cardRect.width-gap,top:limit(top,margin,vh-cardRect.height-margin)},
      below:{left:limit(left,margin,vw-cardRect.width-margin),top:bottom+gap},
      above:{left:limit(left,margin,vw-cardRect.width-margin),top:top-cardRect.height-gap}
    };
    const fits=pos=>pos.left>=margin&&pos.top>=margin&&pos.left+cardRect.width<=vw-margin&&pos.top+cardRect.height<=vh-margin;
    let order=[step.placement,'right','left','below','above'].filter((v,i,a)=>v&&a.indexOf(v)===i);
    if(mobile){
      order=rect.top>vh*.58?['above','right','left','below']:['below','right','left','above'];
    }
    let placement=order.find(name=>candidates[name]&&fits(candidates[name]));
    let pos=placement?candidates[placement]:null;
    if(!pos){
      placement='floating';
      const preferTop=rect.top>vh/2;
      pos={
        left:limit((vw-cardRect.width)/2,margin,vw-cardRect.width-margin),
        top:preferTop?margin:Math.max(margin,vh-cardRect.height-margin)
      };
    }
    card.dataset.placement=placement;
    card.style.left=`${Math.round(pos.left)}px`;
    card.style.top=`${Math.round(pos.top)}px`;
    card.style.visibility='visible';
  };
  const cleanup=()=>{
    window.removeEventListener('resize',positionTourStep);
    document.removeEventListener('keydown',onKeyDown);
    restoreChrome();
    root.innerHTML='';
  };
  const finish=()=>{localStorage.setItem(TOUR_KEY,'1');cleanup();};
  const draw=()=>{
    const step=steps[index];
    step.prepare?.();
    root.innerHTML=`<div class="tour-overlay" data-focus="${step.target?'target':'none'}"><div id="tourSpotlight" class="tour-spotlight" aria-hidden="true"${step.target?'':' hidden'}></div><section id="tourCard" class="tour-card" role="dialog" aria-modal="true" aria-live="polite" aria-label="OnePane product tour"><div class="tour-progress"><span>${index+1} / ${steps.length}</span><span>${Math.round(((index+1)/steps.length)*100)}%</span></div><div class="tour-progress-track" aria-hidden="true"><span style="width:${((index+1)/steps.length)*100}%"></span></div><h2>${escapeHtml(step.title)}</h2><p>${escapeHtml(step.body)}</p><div class="tour-actions"><button class="btn" id="tourSkip">${index===steps.length-1?'Close':'Skip tour'}</button><span class="tour-spacer"></span>${index>0?'<button class="btn" id="tourBack">Back</button>':''}<button class="btn primary" id="tourNext">${index===steps.length-1?'Finish':'Next'}</button></div></section></div>`;
    $('#tourSkip').onclick=finish;
    $('#tourBack')?.addEventListener('click',()=>{index--;draw();});
    $('#tourNext').onclick=()=>{if(index===steps.length-1)return finish();index++;draw();};
    requestAnimationFrame(()=>requestAnimationFrame(positionTourStep));
  };
  function onKeyDown(e){
    if(e.key==='Escape'){e.preventDefault();finish();return;}
    if((e.key==='ArrowRight'||e.key==='Enter')&&e.target?.tagName!=='BUTTON'){$('#tourNext')?.click();}
    if(e.key==='ArrowLeft'){$('#tourBack')?.click();}
  }
  if(replay)localStorage.removeItem(TOUR_KEY);
  window.addEventListener('resize',positionTourStep);
  document.addEventListener('keydown',onKeyDown);
  draw();
}
async function hydrateHealth(){
  const h=await api.health(),buttons=[$('#healthButton'),$('#mobileHealthButton')].filter(Boolean);
  const status=String(h?.status||'unknown').toLowerCase(),label=status==='ok'?'Healthy':status==='unknown'?'Unknown':'Degraded';
  for(const b of buttons){b.classList.toggle('health-ok',status==='ok');b.classList.toggle('bad',status!=='ok'&&status!=='unknown');b.classList.toggle('health-unknown',status==='unknown');const text=$('span:last-child',b);if(text&&text!==$('.status-dot',b))text.textContent=label;}
}
function titleCase(s){return s.replace(/(^|[-_ ])\w/g,m=>m.toUpperCase());}
function escapeHtml(v){return String(v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));}


/* === OnePane alpha.1 Windows QA integration pass === */
let onepaneIdentity=null;
let onepaneWorkspace='';
let localProfileQA=null;
let uiProjects=[];
let ATTENTION_ITEMS=[];
let NOTIFICATIONS=[];
const liveOps={health:'unknown',tasks:[],routines:[],nodes:[],providers:[],events:[],reported:{health:false,tasks:false,routines:false,nodes:false,providers:false,events:false},lastRefresh:0};
function liveOpsReported(key){return !!liveOps.reported?.[key]}
function liveOpsAttentionReported(){return ['tasks','providers','nodes','events'].every(liveOpsReported)}
let operationsRefreshInFlight=null;
const THEME_PALETTES={
  system:{mode:'dark',caption:'#0d1721',text:'#e7edf4',border:'#203142'},
  light:{mode:'light',caption:'#e9eef5',text:'#182532',border:'#cbd6e2'},
  dark:{mode:'dark',caption:'#111316',text:'#eef1f4',border:'#2b3138'},
  graphite:{mode:'dark',caption:'#171a1d',text:'#eef2f5',border:'#31373d'},
  midnight:{mode:'dark',caption:'#070d1b',text:'#edf4ff',border:'#263654'},
  forest:{mode:'dark',caption:'#0b1812',text:'#e8f7ef',border:'#244638'},
  ocean:{mode:'dark',caption:'#071a24',text:'#e7f7ff',border:'#16485e'},
  violet:{mode:'dark',caption:'#171225',text:'#f1eaff',border:'#493a66'},
  ember:{mode:'dark',caption:'#21130f',text:'#fff0e8',border:'#62372b'}
};

function csrfCookie(){
  const p=document.cookie.split('; ').find(x=>x.startsWith('onepane_csrf='));
  return p?decodeURIComponent(p.split('=').slice(1).join('=')):'';
}
async function apiRequest(path,opts={}){
  const method=(opts.method||'GET').toUpperCase();
  const headers={...(opts.headers||{})};
  if(opts.body && !headers['content-type'] && !headers['Content-Type']) headers['content-type']='application/json';
  if(!['GET','HEAD','OPTIONS'].includes(method)) headers['X-OnePane-CSRF']=csrfCookie();
  const res=await fetch(path,{credentials:'same-origin',...opts,method,headers});
  const text=await res.text(); let body={};
  try{body=text?JSON.parse(text):{}}catch{body={error:text}}
  if(!res.ok) throw new Error(body?.error||body?.message||`HTTP ${res.status}`);
  return body;
}
api.json=async function(path,opts={}){try{return await apiRequest(path,opts)}catch(err){console.warn('OnePane API',path,err);return null}};

function attentionFromLiveData(){
  const out=[];
  for(const t of liveOps.tasks){const st=String(t.state||'').toLowerCase();if(st==='waiting_approval')out.push({id:`task-${t.id}-approval`,title:'Approval required',detail:t.objective||t.id,route:'tasks',severity:'warn'});else if(st==='blocked'||st==='failed')out.push({id:`task-${t.id}-${st}`,title:`Task ${st}`,detail:t.objective||t.id,route:'tasks',severity:'bad'});}
  for(const p of liveOps.providers){const st=String(p.status||'').toLowerCase();if(['degraded','rate_limited','expired','reauth_required','unavailable'].includes(st))out.push({id:`provider-${p.id}-${st}`,title:`Provider ${st.replaceAll('_',' ')}`,detail:p.display_name||p.provider||p.id,route:'providers',severity:st==='degraded'||st==='rate_limited'?'warn':'bad'});}
  for(const n of liveOps.nodes){const st=String(n.status||n.state||'').toLowerCase();if(['offline','failed','unavailable','stale'].includes(st))out.push({id:`node-${n.id||n.node_id}-${st}`,title:'Node unavailable',detail:n.display_name||n.node_id||n.id,route:'nodes',severity:'bad'});}
  const failure=/failed|error|blocked|approval_required|degraded|unavailable|rate_limited|recovery_required/i;
  for(const e of [...liveOps.events].reverse()){const type=String(e.event_type||'');if(!failure.test(type))continue;out.push({id:`event-${e.sequence||e.id}`,title:eventLabel(e),detail:`${e.aggregate_type||''} ${e.aggregate_id||''}`.trim(),route:type.includes('provider')?'providers':type.includes('node')?'nodes':type.includes('task')?'tasks':'operations',drawer:type.includes('log')?'logs':null,severity:/failed|error|blocked|unavailable|recovery/i.test(type)?'bad':'warn'});if(out.length>=20)break;}
  const seen=new Set();return out.filter(x=>x.id&&!seen.has(x.id)&&(seen.add(x.id),true)).slice(0,20);
}
function syncLiveNotifications(){
  ATTENTION_ITEMS=attentionFromLiveData();
  let read=[];try{read=JSON.parse(localStorage.getItem('onepane:read-notifications')||'[]')}catch{}
  const readSet=new Set(Array.isArray(read)?read:[]);
  NOTIFICATIONS=ATTENTION_ITEMS.map(n=>({...n,unread:!readSet.has(n.id)}));
  syncNotificationBadges();
}
function markNotificationRead(id){
  const n=NOTIFICATIONS.find(x=>x.id===id);if(n)n.unread=false;
  const read=NOTIFICATIONS.filter(x=>x.unread===false).map(x=>x.id).slice(-200);
  localStorage.setItem('onepane:read-notifications',JSON.stringify(read));syncNotificationBadges();
}
async function refreshOperationalDataQA(force=false){
  if(!onepaneWorkspace)return;
  if(operationsRefreshInFlight)return operationsRefreshInFlight;
  if(!force&&Date.now()-liveOps.lastRefresh<5000)return;
  operationsRefreshInFlight=(async()=>{
    const qs=encodeURIComponent(onepaneWorkspace);
    const results=await Promise.allSettled([
      apiRequest('/v1/health'),
      apiRequest(`/v1/tasks?workspace_id=${qs}&limit=100`),
      apiRequest(`/v1/routines?workspace_id=${qs}`),
      apiRequest('/v1/nodes'),
      apiRequest(`/v1/providers?workspace_id=${qs}`),
      apiRequest(`/v1/events?workspace_id=${qs}&limit=100&latest=1`)
    ]);
    const ok=i=>results[i]?.status==='fulfilled',value=i=>ok(i)?results[i].value:null;
    liveOps.reported={health:ok(0),tasks:ok(1),routines:ok(2),nodes:ok(3),providers:ok(4),events:ok(5)};
    const health=value(0),tasks=value(1),routines=value(2),nodes=value(3),providers=value(4),events=value(5);
    liveOps.health=liveOps.reported.health?String(health?.status||'unknown').toLowerCase():'unknown';
    liveOps.tasks=liveOps.reported.tasks&&Array.isArray(tasks)?tasks:[];
    liveOps.routines=liveOps.reported.routines&&Array.isArray(routines)?routines:[];
    liveOps.nodes=liveOps.reported.nodes?(Array.isArray(nodes)?nodes:(nodes?.nodes||[])):[];
    liveOps.providers=liveOps.reported.providers?(Array.isArray(providers)?providers:(providers?.providers||[])):[];
    liveOps.events=liveOps.reported.events&&Array.isArray(events)?events:[];
    liveOps.lastRefresh=Date.now();syncLiveNotifications();
    if(currentTab()?.route==='operations')renderOperations();
  })().finally(()=>{operationsRefreshInFlight=null;});
  return operationsRefreshInFlight;
}
function normalizedWorkspace(me){
  const rows=Array.isArray(me?.workspaces)?me.workspaces:[];
  const first=rows[0];
  if(typeof first==='string') return first;
  return first?.id||first?.workspace_id||first?.ID||'';
}
function setAuthStage(id){
  ['boot','setup','login'].forEach(x=>$('#'+x)?.classList.toggle('hidden',x!==id));
  $('#authGate')?.classList.toggle('hidden',id==='app');
  $('#app')?.classList.toggle('hidden',id!=='app');
  $('#mobileNav')?.classList.toggle('hidden',id!=='app'||!isPhoneLayout());
}
async function bootOnePane(){
  applyTheme(state.theme||'system');
  qa5ApplyAuthLanguage(qa5SelectedLanguage());
  $('#setup-form')?.addEventListener('submit',async e=>{
    e.preventDefault(); const err=$('#setup-error'); err.textContent='';
    try{const f=Object.fromEntries(new FormData(e.currentTarget)); await apiRequest('/v1/setup/admin',{method:'POST',body:JSON.stringify(f)}); await enterOnePane();}
    catch(ex){err.textContent=ex.message;}
  });
  $('#login-form')?.addEventListener('submit',async e=>{
    e.preventDefault(); const err=$('#login-error'); err.textContent='';
    try{const f=Object.fromEntries(new FormData(e.currentTarget)); await apiRequest('/v1/auth/login',{method:'POST',body:JSON.stringify(f)}); await enterOnePane();}
    catch(ex){err.textContent=ex.message;}
  });
  try{
    const setup=await apiRequest('/v1/setup/status');
    sendNativeReady();
    if(setup?.required){setAuthStage('setup');qa5PrepareFirstRunLanguage();return;}
    try{await enterOnePane();}catch{qa5ApplyAuthLanguage(qa5SelectedLanguage());setAuthStage('login');}
  }catch(ex){const boot=$('#boot .page-subtitle');if(boot)boot.textContent=`Control plane unavailable: ${ex.message}`;}
}
async function enterOnePane(){
  onepaneIdentity=await apiRequest('/v1/auth/me');
  onepaneWorkspace=normalizedWorkspace(onepaneIdentity);
  if(!onepaneWorkspace) throw new Error('No workspace is assigned to this account.');
  setAuthStage('app');
  init();
  const user=$('#userButton');
  if(user){const display=onepaneIdentity.display_name||onepaneIdentity.username||'OP';user.textContent=display.split(/\s+/).map(x=>x[0]).join('').slice(0,2).toUpperCase()||'OP';}
  refreshOperationalDataQA(true);
}

function effectiveThemePalette(name=state.theme){
  if(name==='system'){
    if(matchMedia('(prefers-color-scheme: light)').matches) return {mode:'light',caption:'#e9eef5',text:'#182532',border:'#cbd6e2'};
    return THEME_PALETTES.system;
  }
  return THEME_PALETTES[name]||THEME_PALETTES.dark;
}
function sendNativeTheme(){
  const p=effectiveThemePalette();
  try{window.chrome?.webview?.postMessage(`onepane-theme|${p.mode}|${p.caption}|${p.text}|${p.border}`)}catch{}
  const meta=$('meta[name="theme-color"]'); if(meta) meta.content=p.caption;
}
async function sendNativeReady(){
  try{
    const about=await apiRequest('/v1/about');
    window.chrome?.webview?.postMessage(`onepane-ui-ready|${about?.version||'unknown'}`);
  }catch{}
}
function applyTheme(name){
  state.theme=name||'system';
  document.documentElement.dataset.theme=state.theme;
  try{persist()}catch{}
  sendNativeTheme();
}
matchMedia('(prefers-color-scheme: light)').addEventListener?.('change',()=>{if(state.theme==='system')sendNativeTheme();});

function renderActiveView(){
  const t=currentTab(); if(!t)return;
  if(t.state==='suspended')t.state='active';
  const renderers={
    operations:renderOperations,workspaces:renderWorkspaces,tasks:renderTasks,projects:renderProjects,
    models:renderModels,nodes:renderNodes,agents:renderAgents,sandboxes:renderSandboxes,
    routines:()=>renderPlaceholder('Routines','Scheduled and event-driven automation, triggers and execution history.'),
    providers:renderProviders,integrations:renderIntegrations,secrets:renderSecrets,
    evidence:()=>renderPlaceholder('Evidence / Audit','Event Ledger, Artifacts, Observations, Verifications, Operations and CapabilityLease activity.'),settings:renderSettings
  };
  (renderers[t.route]||renderOperations)();
}

function workspaceCatalogue(){return [['tasks','Active Tasks'],['nodes','Nodes'],['logs','Live Logs'],['models','Models'],['attention','Attention'],['resources','Resource Utilisation'],['providers','Provider Health'],['activity','Recent Activity']];}
function workspacePreset(name,w){const p={small:[3,3],medium:[4,4],large:[6,5],wide:[8,5],full:[12,6]};const [col,row]=p[name]||p.medium;return {col,row};}
function nearestWorkspaceSize(w){let best='medium',score=999;for(const n of ['small','medium','large','wide','full']){const p=workspacePreset(n,w),d=Math.abs((w.col||4)-p.col)+Math.abs((w.row||4)-p.row);if(d<score){score=d;best=n;}}return best;}
function workspaceContent(type){
  const c={tasks:'Task queue and active execution.',nodes:'Local and federated compute nodes.',logs:'Scheduler, Watchdog and node events.',models:'Local, remote and cloud model status.',attention:'Approvals, blocked work and failures.',resources:'CPU, RAM, GPU and storage utilisation.',providers:'Connected inference providers and health.',activity:'Recent control-plane events.'};
  return c[type]||'OnePane component';
}
function renderWorkspaces(){
  const edit=!!state.workspaceEdit;
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Workspaces','Composable operational views with snap-sized, movable components',`<button class="btn ${edit?'primary':''}" id="editWorkspace">${edit?'Done':'Edit layout'}</button>${edit?'<button class="btn" id="addWorkspaceComponent">Add component</button><button class="btn" id="resetWorkspaceLayout">Reset layout</button>':''}`)}
  <div class="workspace-toolbar"><select><option>Development</option><option>AI Lab</option><option>Infrastructure</option></select><button class="btn">Duplicate</button><button class="btn">Save preset</button></div>
  <div class="workspace-grid ${edit?'editing':''}" id="workspaceGrid">${state.workspaceWidgets.map(renderWorkspaceWidget).join('')}</div></section>`;
  $('#editWorkspace').onclick=()=>{state.workspaceEdit=!state.workspaceEdit;persist();renderWorkspaces();};
  $('#addWorkspaceComponent')?.addEventListener('click',openWorkspaceComponentPicker);
  $('#resetWorkspaceLayout')?.addEventListener('click',()=>{state.workspaceWidgets=defaultState().workspaceWidgets;persist();renderWorkspaces();});
  enableWorkspaceDrag();bindViewActions($('#viewHost'));
}
function renderWorkspaceWidget(w){
  const edit=!!state.workspaceEdit;
  const controls=edit?`<div class="dashboard-edit-bar"><span class="dashboard-drag" title="Drag to move">⋮⋮</span><strong>${escapeHtml(w.title)}</strong><span class="dashboard-edit-spacer"></span><button class="tiny" data-ws-move="up" data-ws-id="${w.id}">↑</button><button class="tiny" data-ws-move="down" data-ws-id="${w.id}">↓</button><select class="dashboard-size" data-ws-size data-ws-id="${w.id}">${['small','medium','large','wide','full'].map(n=>`<option value="${n}">${titleCase(n)}</option>`).join('')}</select><button class="tiny danger" data-ws-remove="${w.id}">×</button></div>`:'';
  return `<section class="workspace-widget dashboard-widget ${edit?'editable':''}" draggable="${edit}" data-widget="${w.id}" style="grid-column:span ${Math.max(2,Math.min(12,w.col||4))};grid-row:span ${Math.max(2,Math.min(10,w.row||4))}">${controls}<div class="dashboard-widget-content"><div class="widget-handle"><strong>${escapeHtml(w.title)}</strong></div><div class="widget-body">${escapeHtml(workspaceContent(w.type))}</div></div></section>`;
}
function enableWorkspaceDrag(){
  if(!state.workspaceEdit)return;
  $$('.workspace-widget').forEach(el=>{
    el.addEventListener('dragstart',()=>{lastDragWidget=el.dataset.widget;el.classList.add('dragging');});
    el.addEventListener('dragend',()=>el.classList.remove('dragging'));
    el.addEventListener('dragover',e=>e.preventDefault());
    el.addEventListener('drop',e=>{e.preventDefault();const target=el.dataset.widget;if(!lastDragWidget||target===lastDragWidget)return;const a=state.workspaceWidgets.findIndex(x=>x.id===lastDragWidget),b=state.workspaceWidgets.findIndex(x=>x.id===target);if(a<0||b<0)return;[state.workspaceWidgets[a],state.workspaceWidgets[b]]=[state.workspaceWidgets[b],state.workspaceWidgets[a]];persist();renderWorkspaces();});
  });
  $$('[data-ws-size]').forEach(sel=>{const w=state.workspaceWidgets.find(x=>x.id===sel.dataset.wsId);if(w)sel.value=nearestWorkspaceSize(w);sel.onchange=()=>{const x=state.workspaceWidgets.find(y=>y.id===sel.dataset.wsId);if(!x)return;Object.assign(x,workspacePreset(sel.value,x));persist();renderWorkspaces();};});
  $$('[data-ws-move]').forEach(b=>b.onclick=()=>moveWorkspaceWidget(b.dataset.wsId,b.dataset.wsMove==='up'?-1:1));
  $$('[data-ws-remove]').forEach(b=>b.onclick=()=>{state.workspaceWidgets=state.workspaceWidgets.filter(x=>x.id!==b.dataset.wsRemove);persist();renderWorkspaces();});
}
function openWorkspaceComponentPicker(){
  const catalogue=workspaceCatalogue(),used=new Set(state.workspaceWidgets.map(x=>x.type)),available=catalogue.filter(([t])=>!used.has(t));
  openModal('Add workspace component',available.length?`<div class="component-picker-grid">${available.map(([type,title])=>`<button class="component-choice" data-add-ws="${type}"><strong>${title}</strong><span>${workspaceContent(type)}</span></button>`).join('')}</div>`:'<div class="empty-state compact">All components are already present.</div>');
  $$('[data-add-ws]', $('#overlayRoot')).forEach(b=>b.onclick=()=>{const [type,title]=catalogue.find(x=>x[0]===b.dataset.addWs);state.workspaceWidgets.push({id:`ww-${type}-${Date.now().toString(36)}`,title,type,col:4,row:4});persist();closeModal();renderWorkspaces();});
}

function openModal(title,body,footer=''){
  closePopover();
  const root=$('#overlayRoot');
  root.innerHTML=`<div class="overlay modal-backdrop"><section class="qa-modal" role="dialog" aria-modal="true"><div class="component-picker-header"><div><h2>${escapeHtml(title)}</h2></div><button class="icon-button" data-close-modal>×</button></div><div class="qa-modal-body">${body}</div>${footer?`<div class="qa-modal-footer">${footer}</div>`:''}</section></div>`;
  $('[data-close-modal]',root).onclick=closeModal;root.firstElementChild.onclick=e=>{if(e.target===root.firstElementChild)closeModal();};
}
function closeModal(){closePopover();const r=$('#overlayRoot');if(r)r.innerHTML='';}
function notice(text,kind='good'){const r=$('#overlayRoot');r.innerHTML=`<div class="toast ${kind}">${escapeHtml(text)}</div>`;setTimeout(()=>{if(r.textContent.includes(text))r.innerHTML='';},2600);}

async function renderTasks(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Tasks','Canonical task queue and execution history','<button class="btn primary" id="newTaskButton">New Task</button>')}<div id="tasksBody" class="table-shell"><div class="widget-body">Loading tasks…</div></div></section>`;
  $('#newTaskButton').onclick=openNewTask;
  try{const rows=await apiRequest('/v1/tasks?workspace_id='+encodeURIComponent(onepaneWorkspace)+'&limit=200');liveOps.tasks=Array.isArray(rows)?rows:[];liveOps.reported.tasks=true;syncLiveNotifications();$('#tasksBody').innerHTML=`<table class="data-table"><thead><tr><th>Objective</th><th>State</th><th>Scheduling</th><th>Priority</th><th>Updated</th></tr></thead><tbody>${liveOps.tasks.length?liveOps.tasks.map(t=>`<tr><td><strong>${escapeHtml(t.objective||t.id)}</strong><div class="list-meta">${escapeHtml(t.id||'')}</div></td><td><span class="pill ${['failed','blocked','waiting_approval'].includes(t.state)?'warn':t.state==='complete'?'good':''}">${escapeHtml(t.state||'')}</span></td><td>${escapeHtml(t.scheduling_class||'')}</td><td>${Number(t.priority||0)}</td><td>${t.updated_at?escapeHtml(new Date(Number(t.updated_at)).toLocaleString()):''}</td></tr>`).join(''):'<tr><td colspan="5" class="muted-cell">No tasks yet.</td></tr>'}</tbody></table>`;}catch(ex){$('#tasksBody').innerHTML=`<div class="widget-body error">${escapeHtml(ex.message)}</div>`;}
}
function openNewTask(){
  openModal('New Task',`<form id="newTaskForm" class="qa-form"><label>Objective<textarea name="objective" rows="4" required placeholder="What should OnePane accomplish?"></textarea></label><div class="form-grid"><label>Priority<input name="priority" type="number" min="-100" max="100" value="0"></label><label>Scheduling<select name="scheduling_class"><option value="user_interactive">Interactive</option><option value="normal_task">Normal</option><option value="background_routine">Background</option></select></label></div><label>Project ID <span class="page-subtitle">(optional)</span><input name="project_id" placeholder="project_…"></label><div class="error" id="newTaskError"></div><button class="btn primary" type="submit">Create task</button></form>`);
  $('#newTaskForm').onsubmit=async e=>{e.preventDefault();const f=Object.fromEntries(new FormData(e.currentTarget));const payload={workspace_id:onepaneWorkspace,objective:f.objective,priority:Number(f.priority||0),scheduling_class:f.scheduling_class};if(f.project_id)payload.project_id=f.project_id;try{await apiRequest('/v1/tasks',{method:'POST',body:JSON.stringify(payload)});liveOps.lastRefresh=0;closeModal();await renderTasks();notice('Task created and handed to the OnePane scheduler.');}catch(ex){$('#newTaskError').textContent=ex.message;}};
}

async function renderProjects(){
  const qa31ProjectRenderEpoch=qa31ViewEpoch;
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Projects','Project configuration, applications, endpoints, workspace bindings and runtime state.','<button class="btn primary" id="configureProject">Configure project</button>')}<div id="projectsBody" class="table-shell"><div class="widget-body">Loading projects…</div></div></section>`;
  $('#configureProject').onclick=openProjectDialog;
  try{const rows=await apiRequest('/v1/projects?workspace_id='+encodeURIComponent(onepaneWorkspace));uiProjects=Array.isArray(rows)?rows:[];$('#projectsBody').innerHTML=`<table class="data-table"><thead><tr><th>Project</th><th>ID</th><th>Description</th><th>Status</th></tr></thead><tbody>${uiProjects.length?uiProjects.map(p=>`<tr><td><strong>${escapeHtml(p.name||'Project')}</strong></td><td>${escapeHtml(p.id||'')}</td><td>${escapeHtml(p.description||'')}</td><td><span class="pill ${p.status==='active'?'good':''}">${escapeHtml(p.status||'configured')}</span></td></tr>`).join(''):'<tr><td colspan="4" class="muted-cell">No projects configured.</td></tr>'}</tbody></table>`;}catch(ex){$('#projectsBody').innerHTML=`<div class="widget-body error">${escapeHtml(ex.message)}</div>`;}
}

function openProjectDialog(){
  openModal('Configure project',`<form id="projectForm" class="qa-form"><label>Name<input name="name" required placeholder="My project"></label><label>Description<textarea name="description" rows="3"></textarea></label><div class="error" id="projectError"></div><button class="btn primary" type="submit">Create project</button></form>`);
  $('#projectForm').onsubmit=async e=>{e.preventDefault();const f=Object.fromEntries(new FormData(e.currentTarget));try{const p=await apiRequest('/v1/projects',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,name:f.name,description:f.description})});uiProjects.unshift(p);closeModal();renderProjects();notice('Project configured.');}catch(ex){$('#projectError').textContent=ex.message;}};
}

async function renderNodes(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Nodes','Federated compute, hardware, power state and remote model capacity','<button class="btn primary" id="newNode">New node</button>')}<div id="nodeBody" class="table-shell"><div class="widget-body">Loading nodes…</div></div></section>`;
  $('#newNode').onclick=openPairNode;
  try{const out=await apiRequest('/v1/nodes');const rows=Array.isArray(out)?out:(out?.nodes||[]);$('#nodeBody').innerHTML=`<table class="data-table"><thead><tr><th>Node</th><th>State</th><th>Endpoint</th><th>Trust</th></tr></thead><tbody>${rows.length?rows.map(n=>`<tr data-inspect-node="${escapeHtml(n.display_name||n.node_id||n.id||'Node')}"><td><strong>${escapeHtml(n.display_name||n.node_id||n.id||'Node')}</strong></td><td><span class="pill ${(n.status||n.state)==='online'?'good':''}">${escapeHtml(n.status||n.state||'registered')}</span></td><td>${escapeHtml(n.peer_endpoint||n.advertise_url||'local')}</td><td>${escapeHtml(n.trust_state||n.trust||'paired')}</td></tr>`).join(''):'<tr><td colspan="4" class="muted-cell">No paired remote nodes yet. The local node remains active.</td></tr>'}</tbody></table>`;bindViewActions($('#nodeBody'));}
  catch(ex){$('#nodeBody').innerHTML=`<div class="widget-body error">${escapeHtml(ex.message)}</div>`;}
}
function openPairNode(){
  openModal('Pair a node',`<form id="pairNodeForm" class="qa-form"><label>Remote node ID<input name="node_id" required placeholder="node_…"></label><div class="page-subtitle">Pairing is explicitly confirmed on both nodes. Discovery never implies trust.</div><div id="pairStage"></div><div class="error" id="pairNodeError"></div><button class="btn primary" type="submit">Begin pairing</button></form>`);
  $('#pairNodeForm').onsubmit=async e=>{e.preventDefault();const id=new FormData(e.currentTarget).get('node_id').trim();try{const out=await apiRequest(`/v1/nodes/${encodeURIComponent(id)}/pair`,{method:'POST',body:'{}'});$('#pairStage').innerHTML=`<div class="pair-code"><span>Pairing code</span><strong>${escapeHtml(out.pairing_code||'')}</strong></div><label>Confirm code<input id="pairConfirmCode" value="${escapeHtml(out.pairing_code||'')}" required></label><button type="button" class="btn primary" id="confirmPair">Confirm pairing</button>`;$('#confirmPair').onclick=async()=>{try{await apiRequest(`/v1/nodes/${encodeURIComponent(id)}/pair/confirm`,{method:'POST',body:JSON.stringify({code:$('#pairConfirmCode').value})});closeModal();renderNodes();notice('Node pairing confirmed.');}catch(ex){$('#pairNodeError').textContent=ex.message;}};}catch(ex){$('#pairNodeError').textContent=ex.message;}};
}

async function providerPresetsQA(){const p=await apiRequest('/v1/provider-presets');return Array.isArray(p)?p:[];}
async function renderProviders(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Providers','Connect and monitor AI inference providers','<button class="btn primary" id="newProvider">New provider</button>')}<div id="providersBody" class="table-shell"><div class="widget-body">Loading provider catalogue…</div></div></section>`;
  $('#newProvider').onclick=openProviderDialog;
  try{const presets=await providerPresetsQA();let connected=[];try{const c=await apiRequest('/v1/providers?workspace_id='+encodeURIComponent(onepaneWorkspace));connected=Array.isArray(c)?c:(c?.providers||[]);}catch{}
    $('#providersBody').innerHTML=`<table class="data-table"><thead><tr><th>Provider</th><th>Access</th><th>Endpoint</th><th>Status</th></tr></thead><tbody>${presets.map(p=>{const on=connected.some(x=>String(x.provider||x.preset_id||x.provider_id||'')===String(p.id));return `<tr><td><strong>${escapeHtml(p.display_name)}</strong><div class="list-meta">${escapeHtml(p.description||'')}</div></td><td>${escapeHtml(p.access_mode||'')}</td><td>${escapeHtml(p.default_endpoint||p.endpoint_template||'Configure')}</td><td><span class="pill ${on?'good':''}">${on?'Connected':'Available'}</span></td></tr>`}).join('')}</tbody></table>`;
  }catch(ex){$('#providersBody').innerHTML=`<div class="widget-body error">${escapeHtml(ex.message)}</div>`;}
}
async function openProviderDialog(){
  let presets=[];try{presets=await providerPresetsQA();}catch(ex){return notice(ex.message,'bad');}
  openModal('Connect provider',`<form id="providerForm" class="qa-form"><label>Provider<select id="providerPreset" name="preset_id">${presets.map(p=>`<option value="${p.id}">${escapeHtml(p.display_name)}</option>`).join('')}</select></label><label>Endpoint<input id="providerEndpoint" name="base_url"></label><label>Model<input name="model_ref" placeholder="Provider model ID"></label><label>Credential reference <span class="page-subtitle">(optional; create in Secrets)</span><input name="secret_ref" placeholder="secret_… or logical reference"></label><div class="toolbar"><button type="button" class="btn" id="probeProvider">Probe</button><button class="btn primary" type="submit">Connect</button></div><div class="error" id="providerError"></div><div id="providerProbeResult" class="page-subtitle"></div></form>`);
  const select=$('#providerPreset'),endpoint=$('#providerEndpoint');const sync=()=>{const p=presets.find(x=>x.id===select.value);endpoint.value=p?.default_endpoint||p?.endpoint_template||'';};select.onchange=sync;sync();
  const payload=()=>{const f=Object.fromEntries(new FormData($('#providerForm')));const x={workspace_id:onepaneWorkspace,preset_id:f.preset_id,base_url:f.base_url,model_ref:f.model_ref||''};if(f.secret_ref)x.secret_ref=f.secret_ref;return x;};
  $('#probeProvider').onclick=async()=>{try{const out=await apiRequest('/v1/providers/probe',{method:'POST',body:JSON.stringify(payload())});$('#providerProbeResult').textContent=`Probe succeeded${out?.models?.length?` · ${out.models.length} models`:''}.`;$('#providerError').textContent='';}catch(ex){$('#providerError').textContent=ex.message;}};
  $('#providerForm').onsubmit=async e=>{e.preventDefault();try{await apiRequest('/v1/providers',{method:'POST',body:JSON.stringify(payload())});closeModal();renderProviders();notice('Provider connected.');}catch(ex){$('#providerError').textContent=ex.message;}};
}

async function renderIntegrations(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Integrations','External tools and SaaS connectors. Credentials remain stored separately in Secrets.','<button class="btn" id="integrationSecrets">Open Secrets</button>')}<div class="filter-row"><input id="integrationFilter" placeholder="Filter integrations…"></div><div id="integrationBody" class="integration-grid"></div></section>`;
  $('#integrationSecrets').onclick=()=>openRoute('secrets');
  try{const rows=await apiRequest('/v1/plugin-presets');const list=Array.isArray(rows)?rows:[];const draw=()=>{const q=$('#integrationFilter').value.toLowerCase();$('#integrationBody').innerHTML=list.filter(p=>`${p.display_name} ${p.category} ${p.id}`.toLowerCase().includes(q)).map(p=>`<section class="panel-card integration-card"><div class="card-header"><div><div class="card-title">${escapeHtml(p.display_name)}</div><div class="list-meta">${escapeHtml(p.category||'integration')}</div></div></div><div class="widget-body">${escapeHtml(p.description||'')}<div class="integration-actions"><span class="pill">${escapeHtml(p.credential_kind||'access-token')}</span><button class="btn" data-config-plugin="${p.id}">Configure</button></div></div></section>`).join('')||'<div class="empty-state compact">No integrations match this filter.</div>';$$('[data-config-plugin]').forEach(b=>b.onclick=()=>openPluginCredential(list.find(p=>p.id===b.dataset.configPlugin)));};$('#integrationFilter').oninput=draw;draw();}
  catch(ex){$('#integrationBody').innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;}
}
function openPluginCredential(p){
  openModal(`Configure ${p.display_name}`,`<form id="pluginCredentialForm" class="qa-form"><div class="page-subtitle">Credential is encrypted in OnePane Vault and is never displayed again after storage.</div><label>${escapeHtml(p.credential_kind||'Credential')}<div class="secret-entry"><input id="pluginSecret" type="password" name="value" required autocomplete="off"><button type="button" class="btn" data-toggle-secret="#pluginSecret">Show</button></div></label><div class="error" id="pluginCredentialError"></div><button class="btn primary" type="submit">Store credential</button></form>`);
  bindSecretToggles($('#overlayRoot'));
  $('#pluginCredentialForm').onsubmit=async e=>{e.preventDefault();const value=new FormData(e.currentTarget).get('value');try{await apiRequest('/v1/vault/plugin-credentials',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,plugin_id:p.id,kind:p.credential_kind||'access-token',value,display_label:p.display_name})});closeModal();notice(`${p.display_name} credential stored.`);}catch(ex){$('#pluginCredentialError').textContent=ex.message;}};
}

async function renderSecrets(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Secrets','Preconfigured provider credentials stored in the canonical OnePane Vault','<button class="btn" id="refreshSecrets">Refresh</button>')}<div class="secret-toolbar"><input id="secretFilter" placeholder="Filter providers…"><select id="secretScope"><option value="all">All scopes</option><option value="provider">Direct provider</option><option value="omniroute">OmniRoute</option></select><select id="secretStatus"><option value="all">All statuses</option><option value="stored">Stored</option><option value="missing">Not configured</option></select></div><div id="secretCatalogue" class="secret-catalogue"><div class="widget-body">Loading Vault catalogue…</div></div><div class="page-subtitle" style="margin-top:10px">Stored secret values are write-only. Show/Hide applies only to text you are currently entering; OnePane never reveals a stored credential.</div></section>`;
  $('#refreshSecrets').onclick=renderSecrets;
  try{
    const [presets,storedRaw]=await Promise.all([providerPresetsQA(),apiRequest('/v1/vault/provider-credentials?workspace_id='+encodeURIComponent(onepaneWorkspace)).catch(()=>[])]);
    const stored=Array.isArray(storedRaw)?storedRaw:[];
    const providerOf=r=>String(r.upstream_provider||r.UpstreamProvider||r.provider_type||r.ProviderType||r.logical_name||r.LogicalName||'').toLowerCase();
    const statusFor=p=>stored.some(r=>providerOf(r).includes(String(p.credential_provider||p.id).toLowerCase()));
    const draw=()=>{
      const q=$('#secretFilter').value.toLowerCase(),scope=$('#secretScope').value,st=$('#secretStatus').value;
      const rows=[];
      for(const p of presets){
        if(p.auth_type==='oauth2-pkce') continue;
        const provider=p.credential_provider||p.id,has=statusFor(p),kind=p.auth_type==='api_key'?'api-key':'access-token';
        for(const sc of (scope==='all'?['provider','omniroute']:[scope])){
          if(q&&!`${p.display_name} ${provider} ${sc}`.toLowerCase().includes(q))continue;
          if(st==='stored'&&!has)continue;if(st==='missing'&&has)continue;
          const id=`secret-${String(p.id).replace(/[^a-z0-9_-]/gi,'-')}-${sc}`;
          rows.push(`<div class="secret-row"><div class="secret-provider"><strong>${escapeHtml(p.display_name)}</strong><span>${escapeHtml(provider)} · ${escapeHtml(sc)}</span></div><span class="pill ${has?'good':''}">${has?'Stored':'Not configured'}</span><select data-secret-kind="${id}"><option value="${kind}">${kind==='api-key'?'API key':'Access token'}</option><option value="api-key">API key</option><option value="access-token">Access token</option></select><div class="secret-entry"><input id="${id}" type="password" autocomplete="off" placeholder="Paste new credential"><button class="btn" type="button" data-toggle-secret="#${id}">Show</button></div><button class="btn primary" data-save-provider-secret="${escapeHtml(provider)}" data-secret-input="${id}" data-secret-scope="${sc}" data-secret-label="${escapeHtml(p.display_name)}">${has?'Update':'Store'}</button></div>`);
        }
      }
      $('#secretCatalogue').innerHTML=rows.join('')||'<div class="empty-state compact">No credential rows match the filters.</div>';
      bindSecretToggles($('#secretCatalogue'));
      $$('[data-save-provider-secret]').forEach(b=>b.onclick=async()=>{const input=$('#'+b.dataset.secretInput),value=input.value;if(!value)return notice('Enter a credential value first.','bad');const kind=$(`[data-secret-kind="${b.dataset.secretInput}"]`).value;try{await apiRequest('/v1/vault/provider-credentials',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,scope:b.dataset.secretScope,upstream_provider:b.dataset.saveProviderSecret,kind,value,display_label:b.dataset.secretLabel})});input.value='';notice(`${b.dataset.secretLabel} credential stored.`);setTimeout(renderSecrets,400);}catch(ex){notice(ex.message,'bad');}});
    };
    $('#secretFilter').oninput=draw;$('#secretScope').onchange=draw;$('#secretStatus').onchange=draw;draw();
  }catch(ex){$('#secretCatalogue').innerHTML=`<div class="error widget-body">${escapeHtml(ex.message)}</div>`;}
}
function bindSecretToggles(root=document){$$('[data-toggle-secret]',root).forEach(b=>b.onclick=()=>{const input=$(b.dataset.toggleSecret);if(!input)return;input.type=input.type==='password'?'text':'password';b.textContent=input.type==='password'?'Show':'Hide';});}

async function renderModels(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Models','Hardware-aware local model selection, managed model pool and optional OmniRoute fallback','<button class="btn" id="modelSettings">Model pool settings</button>')}<div class="models-top-grid"><section class="panel-card"><div class="card-header"><div class="card-title">Local AI</div><span class="pill good">Requested during install</span></div><div class="widget-body"><p>Detect this node and ask OnePane for models that fit its CPU, RAM, GPU/VRAM and configured model pool.</p><button class="btn primary" id="detectLocal">Detect hardware</button><div id="localResult" class="model-result"></div></div></section><section class="panel-card"><div class="card-header"><div class="card-title">OmniRoute</div></div><div class="widget-body"><p>Optional local gateway. OnePane stays healthy when OmniRoute is not installed or running.</p><label>Gateway URL<input id="omniUrl" value="http://127.0.0.1:20128/v1"></label><label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label><div class="toolbar"><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" disabled>Connect</button></div><div id="omniResult" class="page-subtitle">Probe the gateway before connecting.</div></div></section></div><section class="panel-card model-catalogue-panel"><div class="card-header"><div><div class="card-title">Model catalogue</div><div class="list-meta">Recommendations are guidance; you can browse the full OnePane catalogue.</div></div><input id="modelFilter" class="catalogue-filter" placeholder="Filter model catalogue…"></div><div id="modelCatalogue" class="model-catalogue-scroll"><div class="widget-body">Loading catalogue…</div></div></section></section>`;
  $('#modelSettings').onclick=()=>openRoute('settings');$('#detectLocal').onclick=detectLocalQA;$('#omniProbe').onclick=()=>omniQA(false);$('#omniConnect').onclick=()=>omniQA(true);loadModelCatalogueQA();
}
async function detectLocalQA(){const box=$('#localResult');box.textContent='Detecting…';try{localProfileQA=await apiRequest('/v1/local-ai/detect',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace})});const g=Array.isArray(localProfileQA.gpus)?localProfileQA.gpus:[];const gpu=g.length?g.map(x=>`${escapeHtml(x.name||'GPU')} · ${bytesQA(x.vram_bytes)}`).join('<br>'):'No GPU detected';box.innerHTML=`<div class="hardware-summary"><strong>${escapeHtml(localProfileQA.cpu?.name||'CPU')}</strong><br>${gpu}<br>${bytesQA(localProfileQA.memory?.total_bytes)} RAM · ${bytesQA(localProfileQA.storage?.available_bytes)} free</div><button class="btn" id="recommendLocal">Recommend models</button><div id="recommendationsQA"></div>`;$('#recommendLocal').onclick=recommendLocalQA;}catch(ex){box.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;}}
async function recommendLocalQA(){const box=$('#recommendationsQA');box.textContent='Calculating fit…';try{const rows=await apiRequest('/v1/local-ai/recommendations',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,profile_id:localProfileQA.id,use_case:'general',context_tokens:8192,limit:8,storage_headroom_pct:20,prefer_gpu:true})});const list=Array.isArray(rows)?rows:[];box.innerHTML=list.length?`<div class="recommendation-list">${list.map(x=>`<div class="recommendation-row"><div><strong>${escapeHtml(x.model?.display_name||x.model_ref||'Model')}</strong><div class="list-meta">${escapeHtml(x.quantization||'')} · ${escapeHtml(x.run_mode||'')}</div></div><span class="pill good">${escapeHtml(x.fit_level||'fit')}</span></div>`).join('')}</div>`:'<p class="page-subtitle">No safe recommendation for this hardware profile.</p>';}catch(ex){box.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;}}
async function loadModelCatalogueQA(){
  const host=$('#modelCatalogue');
  try{
    const out=await apiRequest('/v1/local-ai/catalog');
    const rows=Array.isArray(out)?out:[];
    const draw=()=>{const q=($('#modelFilter')?.value||'').toLowerCase();const filtered=rows.filter(m=>JSON.stringify(m).toLowerCase().includes(q));host.innerHTML=filtered.length?`<table class="data-table"><thead><tr><th>Model</th><th>Parameters</th><th>Context</th><th>Runtime</th><th>Quantisation</th><th>Reference</th></tr></thead><tbody>${filtered.map(m=>`<tr><td><strong>${escapeHtml(m.display_name||m.name||m.model_ref||'Model')}</strong></td><td>${escapeHtml(String(m.parameter_count||m.parameters||m.parameter_scale||'—'))}</td><td>${formatContextQA(m.max_context_tokens||m.context_tokens)}</td><td>${escapeHtml(m.runtime_family||m.runtime||'llama.cpp')}</td><td>${escapeHtml(Array.isArray(m.quantizations)?m.quantizations.join(', '):(m.quantization||'—'))}</td><td class="mono-cell">${escapeHtml(m.model_ref||m.id||'')}</td></tr>`).join('')}</tbody></table>`:'<div class="empty-state compact">No models match this filter.</div>';};
    $('#modelFilter').oninput=draw;draw();
  }catch(ex){host.innerHTML=`<div class="error widget-body">${escapeHtml(ex.message)}</div>`;}
}
function bytesQA(n){if(!n)return'0 B';const u=['B','KB','MB','GB','TB'];let v=Number(n),i=0;while(v>=1024&&i<u.length-1){v/=1024;i++;}return`${v.toFixed(i>1?1:0)} ${u[i]}`;}
function formatContextQA(n){n=Number(n||0);if(!n)return'—';return n>=1024?`${Math.round(n/1024)}K`:String(n);}
async function omniQA(connect){const box=$('#omniResult'),button=$('#omniConnect');box.textContent=connect?'Connecting…':'Probing…';const payload={workspace_id:onepaneWorkspace,base_url:$('#omniUrl').value};if(connect){payload.require_strict_zero_cost=$('#omniStrict').checked;payload.default_model=payload.require_strict_zero_cost?'auto/coding':'auto';}try{const out=await apiRequest(connect?'/v1/providers/omniroute':'/v1/providers/omniroute/probe',{method:'POST',body:JSON.stringify(payload)});const p=connect?(out.probe||out):out;box.innerHTML=`<strong class="good">${p.reachable?'Gateway reachable':'Probe completed'}</strong>${p.strict_zero_cost_verified?' · strict $0 verified':' · strict $0 not verified'}${Array.isArray(p.models)&&p.models.length?`<br>${escapeHtml(p.models.slice(0,8).join(', '))}`:''}`;if(!connect)button.disabled=!p.reachable;}catch(ex){button.disabled=true;box.innerHTML=`<span class="warn">OmniRoute is not reachable at this address.</span><br><span>Install/start OmniRoute or change the endpoint. This does not affect OnePane health.</span><br><span class="list-meta">${escapeHtml(ex.message)}</span>`;}}

async function renderSettings(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Settings','Application preferences and defaults inherited by new workspaces')}<div class="settings-grid"><section class="panel-card"><div class="card-header"><div class="card-title">Appearance</div></div><div class="widget-body"><p>Theme</p><div class="theme-grid settings-theme-grid">${Object.keys(THEME_PALETTES).map(t=>`<button class="theme-choice ${state.theme===t?'active':''}" data-settings-theme="${t}">${titleCase(t)}</button>`).join('')}</div><br><button class="btn" data-action="product-tour">Restart product tour</button></div></section><section class="panel-card"><div class="card-header"><div class="card-title">Local model pool</div></div><div class="widget-body"><p class="page-subtitle">Choose the absolute folder where managed LLM weights and runtimes are stored.</p><label>Model pool path<input id="modelPoolPath" placeholder="D:\\OnePane\\Models"></label><div class="toolbar"><button class="btn primary" id="saveModelPool">Save model pool</button></div><div id="modelPoolStatus" class="page-subtitle"></div></div></section><section class="panel-card"><div class="card-header"><div class="card-title">Security & Approvals</div><span class="pill good" style="margin-left:auto">Recommended: Medium</span></div><div class="widget-body"><div class="approval-profile-grid">${['high','medium','low'].map(v=>`<button class="approval-profile ${state.approvalLevel===v?'selected':''}" data-approval-default="${v}"><strong>${titleCase(v)}</strong><span>${v==='high'?'Prompt for every approval-class operation.':v==='medium'?'Auto-approve low-risk actions; prompt for medium/high/critical.':'Auto-approve low and medium risk; prompt for high/critical.'}</span></button>`).join('')}</div></div></section><section class="panel-card"><div class="card-header"><div class="card-title">Performance & panels</div></div><div class="widget-body">Inactive tab suspension: Balanced<br><br><button class="btn" id="restoreInspectorSettings">Show Inspector</button> <button class="btn" id="restoreDrawerSettings">Show Logs drawer</button></div></section></div></section>`;
  $$('[data-settings-theme]').forEach(b=>b.onclick=()=>{applyTheme(b.dataset.settingsTheme);renderSettings();});
  $('#restoreInspectorSettings').onclick=()=>setInspectorOpen(true);$('#restoreDrawerSettings').onclick=()=>setDrawerOpen(true);
  try{let s;try{s=await apiRequest('/desktop/settings');}catch{s=await apiRequest('/v1/settings/local-ai?workspace_id='+encodeURIComponent(onepaneWorkspace));}$('#modelPoolPath').value=s.model_pool_path||'';}catch(ex){$('#modelPoolStatus').textContent='The installer model-pool selection remains authoritative until this setting is changed.';}
  $('#saveModelPool').onclick=async()=>{const path=$('#modelPoolPath').value.trim();if(!path)return;try{let out;try{out=await apiRequest('/desktop/settings/model-pool',{method:'POST',body:JSON.stringify({model_pool_path:path})});$('#modelPoolStatus').innerHTML=`<span class="good">Administrator approval requested. OnePane will restart with: ${escapeHtml(path)}</span>`;}catch{out=await apiRequest('/v1/settings/local-ai',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,model_pool_path:path})});$('#modelPoolStatus').innerHTML=`<span class="good">Saved: ${escapeHtml(out.model_pool_path||path)}</span>`;}}catch(ex){$('#modelPoolStatus').innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`;}};
  bindViewActions($('#viewHost'));
}

function qa31ChevronIcon(direction){
  const paths={left:'M15 18l-6-6 6-6',right:'M9 6l6 6-6 6',up:'M6 15l6-6 6 6',down:'M6 9l6 6 6-6'};
  return `<svg class="panel-toggle-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="${paths[direction]||paths.right}"/></svg>`;
}
function qa31SetPanelToggle(button,direction,label,expanded){
  if(!button)return;
  button.innerHTML=qa31ChevronIcon(direction);
  button.setAttribute('aria-label',label);
  button.setAttribute('title',label);
  if(expanded!==undefined)button.setAttribute('aria-expanded',String(!!expanded));
}
function setSidebarExpanded(expanded){
  state.sidebar=expanded?'expanded':'collapsed';
  $('#app').dataset.sidebar=state.sidebar;
  persist();
  syncPanelRestoreButtons();
}
function setInspectorOpen(open){state.inspector=open?'open':'closed';$('#app').dataset.inspector=state.inspector;applyInspectorWidth();persist();if(open)renderInspector();syncPanelRestoreButtons();}
function setDrawerOpen(open){state.drawer=open?'open':'closed';$('#bottomDrawer').dataset.state=state.drawer;document.documentElement.style.setProperty('--drawer',open?`${state.drawerHeight}px`:'0px');persist();if(open)renderDrawer();syncPanelRestoreButtons();}
function syncPanelRestoreButtons(){
  const phone=isPhoneLayout(),sidebarToggle=$('#sidebarToggle'),inspectorToggle=$('#inspectorRestore'),drawerToggle=$('#drawerToggle'),drawerRestore=$('#drawerRestore'),resizer=$('#inspectorResizer');
  qa31SetPanelToggle(sidebarToggle,state.sidebar==='expanded'?'left':'right',state.sidebar==='expanded'?'Collapse navigation':'Expand navigation',state.sidebar==='expanded');
  if(inspectorToggle){
    inspectorToggle.classList.toggle('hidden',phone);
    qa31SetPanelToggle(inspectorToggle,state.inspector==='open'?'right':'left',state.inspector==='open'?'Collapse Inspector':'Open Inspector',state.inspector==='open');
  }
  qa31SetPanelToggle(drawerToggle,'down','Collapse Logs drawer',true);
  qa31SetPanelToggle(drawerRestore,'up','Open Logs drawer',false);
  drawerRestore?.classList.toggle('hidden',state.drawer==='open'||phone);
  resizer?.classList.toggle('hidden',state.inspector!=='open'||phone);
}

function bindShell(){
  $('#sidebarToggle').onclick=()=>setSidebarExpanded(state.sidebar!=='expanded');
  $('#newTabButton').onclick=openNewTabPicker;
  $('#drawerToggle').onclick=()=>setDrawerOpen(false);$('#drawerRestore').onclick=()=>setDrawerOpen(true);$('#inspectorRestore').onclick=()=>setInspectorOpen(state.inspector!=='open');
  $('#userButton').onclick=()=>openUserMenu($('#userButton'));
  $$('[data-route]').forEach(b=>b.onclick=()=>openRoute(b.dataset.route));
  $$('[data-action="command-palette"]').forEach(b=>b.onclick=openCommandPalette);
  $$('[data-action="theme"]').forEach(b=>b.onclick=()=>openThemePopover(b));
  $$('[data-action="attention"]').forEach(b=>b.onclick=()=>openAttentionPopover(b));
  $$('[data-action="health-popover"]').forEach(b=>b.onclick=()=>openHealthPopover(b));
  $$('[data-action="mobile-more"]').forEach(b=>b.onclick=openMobileMore);
  $$('[data-action="product-tour"]').forEach(b=>b.onclick=()=>startProductTour({replay:true}));
  $$('[data-mobile-route]').forEach(b=>b.onclick=()=>openRoute(b.dataset.mobileRoute));$$('[data-mobile-more]').forEach(b=>b.onclick=openMobileMore);
  $('#tabStrip').onclick=$('#mobileTabStrip').onclick=e=>{const close=e.target.closest('[data-close-tab]');if(close){e.stopPropagation();return closeTab(close.dataset.closeTab);}const tab=e.target.closest('[data-tab]');if(tab)activateTab(tab.dataset.tab);};
  document.addEventListener('keydown',e=>{if((e.ctrlKey)&&e.key.toLowerCase()==='k'){e.preventDefault();openCommandPalette();}});
  bindDrawerResize();bindInspectorResize();syncPanelRestoreButtons();syncNotificationBadges();sendNativeTheme();
}
function openNewTabPicker(){const options=navItems.map(([route,icon,label])=>`<button class="component-choice" data-new-tab-route="${route}"><strong>${icon} ${label}</strong><span>Open ${label} in a new tab</span></button>`).join('');openModal('Open a new tab',`<div class="component-picker-grid">${options}</div>`);$$('[data-new-tab-route]').forEach(b=>b.onclick=()=>{const r=b.dataset.newTabRoute;closeModal();openRoute(r,{newTab:true});});}
function openUserMenu(anchor){popoverFor(anchor,`<div class="popover"><h3>${escapeHtml(onepaneIdentity?.display_name||onepaneIdentity?.username||'OnePane user')}</h3><div class="popover-row">Workspace<div class="list-meta">${escapeHtml(onepaneWorkspace)}</div></div><button class="btn" id="logoutButton" style="width:100%">Sign out</button></div>`);$('#logoutButton').onclick=async()=>{try{await apiRequest('/v1/auth/logout',{method:'POST',body:'{}'});}catch{}location.reload();};}
function openThemePopover(anchor){popoverFor(anchor,`<div class="popover"><h3>Theme</h3><div class="theme-grid">${Object.keys(THEME_PALETTES).map(t=>`<button class="theme-choice ${state.theme===t?'active':''}" data-theme-choice="${t}">${titleCase(t)}</button>`).join('')}</div></div>`);$$('[data-theme-choice]').forEach(b=>b.onclick=()=>{applyTheme(b.dataset.themeChoice);$('#overlayRoot').innerHTML='';});}
function unreadNotifications(){return NOTIFICATIONS.filter(n=>n.unread!==false);}
function syncNotificationBadges(){const count=unreadNotifications().length;$$('[data-notification-badge]').forEach(b=>{b.textContent=String(count);b.classList.toggle('hidden',count===0);});}
function pushNotification(n){NOTIFICATIONS.unshift({...n,unread:n.unread!==false});syncNotificationBadges();}
async function openAttentionPopover(anchor){
  try{await refreshOperationalDataQA(true);}catch{}
  const rows=unreadNotifications();
  popoverFor(anchor,`<div class="popover notification-center"><div class="popover-title-row"><h3>Notifications</h3><div class="toolbar compact"><span class="pill">${rows.length}</span>${rows.length?'<button class="btn tiny" id="markNotificationsRead">Clear</button>':''}</div></div>${rows.length?rows.map(n=>`<button class="notification-row" data-notification-id="${escapeHtml(n.id||'')}"><strong>${escapeHtml(n.title)}</strong><span>${escapeHtml(n.detail)}</span></button>`).join(''):'<div class="notification-empty">No unread notifications.</div>'}</div>`);
  $('#markNotificationsRead')?.addEventListener('click',()=>{NOTIFICATIONS.forEach(n=>markNotificationRead(n.id));$('#overlayRoot').innerHTML='';});
  $$('[data-notification-id]').forEach(b=>b.onclick=()=>{const n=NOTIFICATIONS.find(x=>x.id===b.dataset.notificationId);if(!n)return;markNotificationRead(n.id);$('#overlayRoot').innerHTML='';if(n.route)openRoute(n.route);if(n.drawer){setDrawerOpen(true);activeDrawerTab=n.drawer;renderDrawer();}});
}



function renderDrawer(){
  const tabs=['logs','events','terminal','watchdog','metrics','evidence'];
  $('#drawerTabs').innerHTML=tabs.map(x=>`<button class="${x===activeDrawerTab?'active':''}" data-drawer-tab="${x}">${titleCase(x)}</button>`).join('');
  const c=$('#drawerContent');
  const events=[...liveOps.events].slice(-200).reverse();
  const eventRows=events.map(e=>{const type=String(e.event_type||'event'),bad=/failed|error|blocked|unavailable|recovery/i.test(type),warn=/warn|approval|required|degraded|rate_limited/i.test(type);const level=bad?'ERROR':warn?'WARN':'INFO';const at=e.occurred_at?new Date(Number(e.occurred_at)).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit',second:'2-digit'}):'';return `<tr><td class="log-time">${escapeHtml(at)}</td><td class="log-level"><span class="pill ${bad?'bad':warn?'warn':''}">${level}</span></td><td class="log-component">${escapeHtml(e.aggregate_type||'event')}</td><td>${escapeHtml(eventLabel(e))}<div class="list-meta">${escapeHtml(e.aggregate_id||'')}</div></td></tr>`;}).join('');
  if(activeDrawerTab==='logs'||activeDrawerTab==='events') c.innerHTML=events.length?`<table class="log-table"><tbody>${eventRows}</tbody></table>`:'<div class="empty-state compact">No Event Ledger entries yet.</div>';
  else if(activeDrawerTab==='watchdog') c.innerHTML=`<div class="widget-body"><strong class="${liveOps.health==='ok'?'good':'warn'}">Control plane ${escapeHtml(liveOps.health||'unknown')}</strong><br><br>Tasks: ${liveOps.tasks.length}<br>Nodes: ${liveOps.nodes.length}<br>Providers: ${liveOps.providers.length}<br>Attention: ${ATTENTION_ITEMS.length}</div>`;
  else if(activeDrawerTab==='metrics') c.innerHTML=localProfileQA?`<div class="widget-body"><strong>${escapeHtml(localProfileQA.cpu?.name||'CPU')}</strong><br>${bytesQA(localProfileQA.memory?.total_bytes)} RAM<br>${Array.isArray(localProfileQA.gpus)&&localProfileQA.gpus.length?localProfileQA.gpus.map(g=>`${escapeHtml(g.name||'GPU')} · ${bytesQA(g.vram_bytes)}`).join('<br>'):'No accelerator reported'}<br>${bytesQA(localProfileQA.storage?.available_bytes)} storage available</div>`:'<div class="widget-body">Run Models → Detect hardware to populate host resource capacity.</div>';
  else if(activeDrawerTab==='evidence') c.innerHTML=`<div class="widget-body">${events.length?`${events.length} recent Event Ledger entr${events.length===1?'y':'ies'} loaded.`:'No recent evidence/event entries loaded.'}<br><br>Use Evidence / Audit for the complete assurance trail.</div>`;
  else c.innerHTML='<div class="widget-body">No interactive terminal is opened. OnePane only exposes terminal execution through governed Task/ToolGateway paths.</div>';
  $$('[data-drawer-tab]', $('#bottomDrawer')).forEach(b=>b.onclick=()=>{activeDrawerTab=b.dataset.drawerTab;renderDrawer();});
  syncPanelRestoreButtons();
}

function a31CommandRegistry(){
  const pageItems=navItems.map(([route,icon,label])=>({category:'Navigate',label,icon,keywords:[route,label],run:()=>openRoute(route)}));
  pageItems.push({category:'Navigate',label:'Settings',icon:'⚙',keywords:['settings','preferences','defaults'],run:()=>openRoute('settings')});
  return pageItems.concat([
    {category:'Models',label:'Open Local Models',icon:'◇',keywords:['model','local','llama','gpu','cpu'],run:()=>{openRoute('models');a31SetModelView('local')}},
    {category:'Models',label:'Open Cloud Models',icon:'☁',keywords:['provider','cloud','oauth','api'],run:()=>{openRoute('models');a31SetModelView('cloud')}},
    {category:'Models',label:'Detect local hardware',icon:'◎',keywords:['gpu','cpu','hardware','vram'],run:()=>{openRoute('models');a31SetModelView('local');setTimeout(()=>$('#detectLocal')?.click(),80)}},
    {category:'Create',label:'New task',icon:'＋',keywords:['task','create','work'],run:()=>{openRoute('tasks');setTimeout(()=>$('#newTaskButton')?.click(),80)}},
    {category:'Create',label:'New scheduled task',icon:'◷',keywords:['schedule','routine','recurring','task'],run:()=>{openRoute('tasks');setTimeout(()=>$('#newScheduledTask')?.click(),80)}},
    {category:'Create',label:'New project',icon:'▢',keywords:['project','workspace','create'],run:()=>{openRoute('projects');setTimeout(()=>$('#qa4NewProject')?.click(),80)}},
    {category:'Skills',label:'Capability Matrix',icon:'✦',keywords:['skill','tool','capability','model','assignment'],run:()=>{openRoute('skills');a31SetSkillsView('matrix')}},
    {category:'Shell',label:state.inspector==='open'?'Collapse Inspector':'Open Inspector',icon:'◫',keywords:['inspector','panel','collapse'],run:()=>setInspectorOpen(state.inspector!=='open')},
    {category:'Shell',label:state.drawer==='open'?'Collapse Logs drawer':'Open Logs drawer',icon:'▤',keywords:['logs','drawer','events'],run:()=>setDrawerOpen(state.drawer!=='open')},
    {category:'Settings',label:'Change language',icon:'文',keywords:['language','locale','translation'],run:()=>{openRoute('settings');a31SetSettingsView('general')}},
    {category:'Settings',label:'Appearance & themes',icon:'◐',keywords:['theme','dark','light','graphite','midnight'],run:()=>{openRoute('settings');a31SetSettingsView('appearance')}},
    {category:'Help',label:'Restart product tour',icon:'?',keywords:['help','tour','onboarding'],run:()=>startProductTour({replay:true})}
  ]);
}
function openCommandPalette(){
  const root=$('#overlayRoot');root.innerHTML=`<div class="overlay"><section class="command-palette"><input id="paletteInput" autofocus placeholder="Search pages or run a command…"><div id="paletteResults"></div><div class="palette-help"><span>↑↓ Navigate</span><span>Enter Run</span><span>Esc Close</span><span>Ctrl+K Toggle</span></div></section></div>`;
  const input=$('#paletteInput'),results=$('#paletteResults'),items=a31CommandRegistry();let selected=0,current=[];
  const draw=()=>{const q=input.value.trim().toLowerCase();current=items.filter(x=>!q||[x.label,x.category,...a31Array(x.keywords)].join(' ').toLowerCase().includes(q));selected=Math.min(selected,Math.max(0,current.length-1));let last='';results.innerHTML=current.length?current.map((x,i)=>{const group=x.category!==last?`<div class="palette-category">${escapeHtml(x.category)}</div>`:'';last=x.category;return group+`<button class="palette-row ${i===selected?'selected':''}" data-palette-index="${i}"><span>${x.icon||'›'}</span><strong>${escapeHtml(x.label)}</strong><small>${escapeHtml(x.category)}</small></button>`}).join(''):'<div class="empty-state compact">No matching pages or commands.</div>';$$('[data-palette-index]',results).forEach(b=>b.onclick=()=>{const item=current[Number(b.dataset.paletteIndex)];root.innerHTML='';item?.run?.()})};
  input.oninput=()=>{selected=0;draw()};input.onkeydown=e=>{if(e.key==='ArrowDown'){e.preventDefault();selected=Math.min(selected+1,current.length-1);draw()}else if(e.key==='ArrowUp'){e.preventDefault();selected=Math.max(0,selected-1);draw()}else if(e.key==='Enter'&&current[selected]){e.preventDefault();root.innerHTML='';current[selected].run?.()}else if(e.key==='Escape')root.innerHTML=''};root.firstElementChild.onclick=e=>{if(e.target===root.firstElementChild)root.innerHTML=''};draw();input.focus();
}

/* QA hardening: Vault-backed provider selection and OmniRoute credential flow. */
function parseSecretMetadata(record){
  try{return typeof record?.metadata==='string'?JSON.parse(record.metadata):(record?.metadata||{});}catch{return {};}
}
function vaultRef(record){return record?.id?`vault:${record.id}`:'';}
function secretMatches(record,{scope,provider}){
  if(!record||record.status!=='active')return false;
  const meta=parseSecretMetadata(record);
  const actualScope=String(meta.credential_scope||String(record.provider_type||'').split(':')[0]||'').toLowerCase();
  const actualProvider=String(meta.upstream_provider||String(record.provider_type||'').split(':').slice(1).join(':')||'').toLowerCase();
  return actualScope===String(scope||'').toLowerCase()&&actualProvider===String(provider||'').toLowerCase();
}
async function loadVaultRecords(){
  const out=await apiRequest('/v1/vault/provider-credentials?workspace_id='+encodeURIComponent(onepaneWorkspace));
  return Array.isArray(out)?out:[];
}
async function openProviderDialog(){
  let presets=[],stored=[];try{[presets,stored]=await Promise.all([providerPresetsQA(),loadVaultRecords().catch(()=>[])]);}catch(ex){return notice(ex.message,'bad');}
  openModal('Connect provider',`<form id="providerForm" class="qa-form"><label>Provider<select id="providerPreset" name="preset_id">${presets.filter(p=>p.id!=='omniroute'&&p.id!=='openai_chatgpt_plan').map(p=>`<option value="${p.id}">${escapeHtml(p.display_name)}</option>`).join('')}</select></label><label>Endpoint<input id="providerEndpoint" name="base_url"></label><label>Model<input name="model_ref" placeholder="Provider model ID" required></label><label>Credential<select id="providerCredential" name="secret_ref"></select></label><div class="page-subtitle" id="providerCredentialHint">Credentials are managed in Secrets.</div><div class="toolbar"><button type="button" class="btn" id="openProviderSecrets">Open Secrets</button><button type="button" class="btn" id="probeProvider">Probe</button><button class="btn primary" type="submit">Connect</button></div><div class="error" id="providerError"></div><div id="providerProbeResult" class="page-subtitle"></div></form>`);
  const select=$('#providerPreset'),endpoint=$('#providerEndpoint'),credential=$('#providerCredential');
  const sync=()=>{const p=presets.find(x=>x.id===select.value);endpoint.value=p?.default_endpoint||p?.endpoint_template||'';const provider=String(p?.credential_provider||p?.id||'').toLowerCase();const matches=stored.filter(r=>secretMatches(r,{scope:'provider',provider}));credential.innerHTML=`<option value="">${p?.auth_type==='none'?'No credential required':'Select stored credential…'}</option>`+matches.map(r=>`<option value="${vaultRef(r)}">${escapeHtml(parseSecretMetadata(r).display_label||r.logical_name)} · v${r.version}</option>`).join('');$('#providerCredentialHint').textContent=matches.length?`${matches.length} matching Vault credential${matches.length===1?'':'s'} available.`:'No matching credential stored yet. Open Secrets to add one.';};
  select.onchange=sync;sync();
  $('#openProviderSecrets').onclick=()=>{closeModal();openRoute('secrets');};
  const payload=()=>{const f=Object.fromEntries(new FormData($('#providerForm')));const x={workspace_id:onepaneWorkspace,preset_id:f.preset_id,base_url:f.base_url,model_ref:f.model_ref||''};if(f.secret_ref)x.secret_ref=f.secret_ref;return x;};
  $('#probeProvider').onclick=async()=>{try{const out=await apiRequest('/v1/providers/probe',{method:'POST',body:JSON.stringify(payload())});$('#providerProbeResult').textContent=`Probe succeeded${out?.models?.length?` · ${out.models.length} models`:''}.`;$('#providerError').textContent='';}catch(ex){$('#providerError').textContent=ex.message;}};
  $('#providerForm').onsubmit=async e=>{e.preventDefault();try{await apiRequest('/v1/providers',{method:'POST',body:JSON.stringify(payload())});closeModal();renderProviders();notice('Provider connected.');}catch(ex){$('#providerError').textContent=ex.message;}};
}
async function renderSecrets(){
  $('#viewHost').innerHTML=`<section class="page secrets-page">${pageHeader('Secrets','Provider credentials stored in the canonical OnePane Vault','<button class="btn" id="refreshSecrets">Refresh</button>')}<div class="secret-toolbar"><input id="secretFilter" placeholder="Filter provider, scope or credential type…"><select id="secretScope"><option value="all">All scopes</option><option value="provider">Direct provider</option><option value="omniroute">OmniRoute</option></select><select id="secretStatus"><option value="all">All statuses</option><option value="stored">Stored</option><option value="missing">Not configured</option></select></div><div id="secretCatalogue" class="secret-catalogue"><div class="widget-body">Loading Vault catalogue…</div></div><div class="page-subtitle" style="margin-top:10px">Stored values are never revealed. Show/Hide only affects text you are entering now. Existing credentials can be rotated by entering a replacement value.</div></section>`;
  $('#refreshSecrets').onclick=renderSecrets;
  try{
    const [presets,stored]=await Promise.all([providerPresetsQA(),loadVaultRecords().catch(()=>[])]);
    const providerRows=presets.filter(p=>p.auth_type!=='oauth2-pkce').map(p=>({id:p.id,label:p.display_name,provider:p.credential_provider||p.id,kind:p.auth_type==='api_key'?'api-key':'access-token'}));
    if(!providerRows.some(x=>x.provider==='gateway'))providerRows.unshift({id:'omniroute-gateway',label:'OmniRoute Gateway',provider:'gateway',kind:'access-token',onlyScope:'omniroute'});
    const draw=()=>{
      const q=$('#secretFilter').value.toLowerCase(),scopeFilter=$('#secretScope').value,statusFilter=$('#secretStatus').value;const rows=[];
      for(const p of providerRows){for(const scope of (p.onlyScope?[p.onlyScope]:(scopeFilter==='all'?['provider','omniroute']:[scopeFilter]))){if(p.onlyScope&&scopeFilter!=='all'&&scopeFilter!==p.onlyScope)continue;const active=stored.find(r=>secretMatches(r,{scope,provider:p.provider}));const has=!!active;if(q&&!`${p.label} ${p.provider} ${scope} ${p.kind}`.toLowerCase().includes(q))continue;if(statusFilter==='stored'&&!has)continue;if(statusFilter==='missing'&&has)continue;const id=`secret-${String(p.id).replace(/[^a-z0-9_-]/gi,'-')}-${scope}`;rows.push(`<div class="secret-row"><div class="secret-provider"><strong>${escapeHtml(p.label)}</strong><span>${escapeHtml(p.provider)} · ${scope==='provider'?'Direct provider':'OmniRoute'}</span>${has?`<code class="secret-ref">${escapeHtml(vaultRef(active))}</code>`:''}</div><span class="pill ${has?'good':''}">${has?'Stored':'Not configured'}</span><select data-secret-kind="${id}"><option value="${p.kind}">${p.kind==='api-key'?'API key':'Access token'}</option><option value="api-key">API key</option><option value="access-token">Access token</option></select><div class="secret-entry"><input id="${id}" type="password" autocomplete="off" placeholder="${has?'Paste replacement credential':'Paste credential'}"><button class="btn" type="button" data-toggle-secret="#${id}">Show</button></div>${has?`<button class="btn" data-copy-secret-ref="${escapeHtml(vaultRef(active))}">Copy ref</button>`:''}<button class="btn primary" data-save-provider-secret="${escapeHtml(p.provider)}" data-secret-input="${id}" data-secret-scope="${scope}" data-secret-label="${escapeHtml(p.label)}">${has?'Update':'Store'}</button></div>`);}}
      $('#secretCatalogue').innerHTML=rows.join('')||'<div class="empty-state compact">No credential rows match the filters.</div>';bindSecretToggles($('#secretCatalogue'));
      $$('[data-copy-secret-ref]').forEach(b=>b.onclick=async()=>{try{await navigator.clipboard.writeText(b.dataset.copySecretRef);notice('Vault reference copied.');}catch{notice(b.dataset.copySecretRef);}});
      $$('[data-save-provider-secret]').forEach(b=>b.onclick=async()=>{const input=$('#'+b.dataset.secretInput),value=input.value;if(!value)return notice('Enter a credential value first.','bad');const kind=$(`[data-secret-kind="${b.dataset.secretInput}"]`).value;try{await apiRequest('/v1/vault/provider-credentials',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,scope:b.dataset.secretScope,upstream_provider:b.dataset.saveProviderSecret,kind,value,display_label:b.dataset.secretLabel})});input.value='';notice(`${b.dataset.secretLabel} credential stored.`);setTimeout(renderSecrets,350);}catch(ex){notice(ex.message,'bad');}});
    };
    $('#secretFilter').oninput=draw;$('#secretScope').onchange=draw;$('#secretStatus').onchange=draw;draw();
  }catch(ex){$('#secretCatalogue').innerHTML=`<div class="error widget-body">${escapeHtml(ex.message)}</div>`;}
}
async function populateOmniCredentials(){
  const sel=$('#omniCredential');if(!sel)return;let rows=[];try{rows=await loadVaultRecords();}catch{}const matches=rows.filter(r=>secretMatches(r,{scope:'omniroute',provider:'gateway'}));sel.innerHTML='<option value="">No gateway credential</option>'+matches.map(r=>`<option value="${vaultRef(r)}">${escapeHtml(parseSecretMetadata(r).display_label||r.logical_name)} · v${r.version}</option>`).join('');
}
async function renderModels(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Models','Hardware-aware local model selection, managed model pool and optional OmniRoute fallback','<button class="btn" id="modelSettings">Model pool settings</button>')}<div class="models-top-grid"><section class="panel-card"><div class="card-header"><div class="card-title">Local AI</div><span class="pill good">Available</span></div><div class="widget-body"><p>Detect this node and ask OnePane for models that fit its CPU, RAM, GPU/VRAM and configured model pool.</p><button class="btn primary" id="detectLocal">Detect hardware</button><div id="localResult" class="model-result"></div></div></section><section class="panel-card"><div class="card-header"><div class="card-title">OmniRoute</div></div><div class="widget-body"><p>Connect a local OmniRoute gateway or an HTTPS gateway running on another trusted host.</p><label>Gateway URL<input id="omniUrl" value="http://127.0.0.1:20128/v1"></label><label>Gateway credential<select id="omniCredential"><option value="">No gateway credential</option></select></label><label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label><div class="toolbar"><button class="btn" id="omniSecrets">Open Secrets</button><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" disabled>Connect</button></div><div id="omniResult" class="page-subtitle">Probe the configured gateway before connecting.</div></div></section></div><section class="panel-card model-catalogue-panel"><div class="card-header"><div><div class="card-title">Model catalogue</div><div class="list-meta">Recommendations are guidance; browse or filter the full OnePane catalogue to choose your own model.</div></div><input id="modelFilter" class="catalogue-filter" placeholder="Filter model catalogue…"></div><div id="modelCatalogue" class="model-catalogue-scroll"><div class="widget-body">Loading catalogue…</div></div></section></section>`;
  $('#modelSettings').onclick=()=>openRoute('settings');$('#detectLocal').onclick=detectLocalQA;$('#omniSecrets').onclick=()=>openRoute('secrets');$('#omniProbe').onclick=()=>omniQA(false);$('#omniConnect').onclick=()=>omniQA(true);
  const savedOmni=localStorage.getItem('onepane:omniroute-url');if(savedOmni)$('#omniUrl').value=savedOmni;
  $('#omniUrl').addEventListener('change',()=>localStorage.setItem('onepane:omniroute-url',$('#omniUrl').value.trim()));
  try{const rows=await apiRequest('/v1/providers?workspace_id='+encodeURIComponent(onepaneWorkspace));const omni=(Array.isArray(rows)?rows:[]).find(p=>String(p.provider||'').toLowerCase()==='omniroute');if(omni){const base=omni.connection?.base_url||omni.connection?.BaseURL;if(base){$('#omniUrl').value=base;localStorage.setItem('onepane:omniroute-url',base);}$('#omniResult').innerHTML=`<span class="good">Connected</span> · ${escapeHtml(omni.status||'configured')}`;}}
  catch{}
  populateOmniCredentials();loadModelCatalogueQA();
}
async function omniQA(connect){
  const box=$('#omniResult'),button=$('#omniConnect');box.textContent=connect?'Connecting…':'Probing…';const payload={workspace_id:onepaneWorkspace,base_url:$('#omniUrl').value.trim()};const ref=$('#omniCredential')?.value;if(ref)payload.secret_ref=ref;if(connect){payload.require_strict_zero_cost=$('#omniStrict').checked;payload.default_model=payload.require_strict_zero_cost?'auto/coding':'auto';}
  try{localStorage.setItem('onepane:omniroute-url',payload.base_url);const out=await apiRequest(connect?'/v1/providers/omniroute':'/v1/providers/omniroute/probe',{method:'POST',body:JSON.stringify(payload)});const p=connect?(out.probe||out):out;box.innerHTML=`<strong class="good">${p.reachable?'Gateway reachable':'Probe completed'}</strong>${p.strict_zero_cost_verified?' · strict $0 verified':' · strict $0 not verified'}${Array.isArray(p.models)&&p.models.length?`<br>${escapeHtml(p.models.slice(0,8).join(', '))}`:''}`;if(!connect)button.disabled=!p.reachable;else{liveOps.lastRefresh=0;refreshOperationalDataQA(true);}}catch(ex){button.disabled=true;box.innerHTML=`<span class="warn">OmniRoute is not reachable at this address.</span><br><span>Start OmniRoute locally, enter an HTTPS remote gateway, or leave OmniRoute disconnected and use direct providers/local models.</span><br><span class="list-meta">${escapeHtml(ex.message)}</span>`;}
}

/* === QA4 product architecture consolidation: Projects -> Workspaces, unified Tasks, Models + Cloud Providers, contextual Inspector === */
const QA4_ROUTE_ALIASES={workspaces:'projects',sandboxes:'projects',providers:'models',routines:'tasks'};
navItems.splice(0,navItems.length,...navItems.filter(([route])=>!Object.prototype.hasOwnProperty.call(QA4_ROUTE_ALIASES,route)));
pages.projects.title='Projects';
pages.models.title='Models';
for(const t of state.tabs){if(QA4_ROUTE_ALIASES[t.route]){t.route=QA4_ROUTE_ALIASES[t.route];t.title=pages[t.route]?.title||t.route;}}
state.tabs=state.tabs.filter((t,i,a)=>i===a.findIndex(x=>x.id===t.id));
if(!state.tabs.some(t=>t.id===state.activeTab))state.activeTab=state.tabs[0]?.id||'tab-operations';
persist();

let qa4ProjectHub={projects:[],activeProjectID:'',activeWorkspaceID:'',catalog:[],providers:[],providerPresets:[],agentPresets:[],routines:[],candidates:[]};
let qa4Inspector={kind:'system',id:'system',title:'OnePane',data:{}};
let qa4InspectorTab='overview';
const QA4_INSPECTOR_TAB_KEY='onepane.inspector.tabs.v2';
const QA4_NOTES_KEY='onepane.context.notes.v2';

function qa4JSON(v,fallback={}){try{if(v==null)return fallback;if(typeof v==='string')return JSON.parse(v||'{}');return v;}catch{return fallback;}}
function qa4ProjectPolicy(p){const x=qa4JSON(p?.project_policy||p?.ProjectPolicyJSON||{},{});return x&&typeof x==='object'?x:{};}
function qa4ProjectUI(p){const policy=qa4ProjectPolicy(p);return policy.onepane_ui&&typeof policy.onepane_ui==='object'?policy.onepane_ui:{};}
function qa4DefaultSandbox(){return {network:false,internet:false,lan:false,computer:false,browser:false,host_files:'workspace-only',secrets:'selected'};}
function qa4ProjectSandbox(p){return {...qa4DefaultSandbox(),...(qa4ProjectUI(p).sandbox||{})};}
function qa4DefaultWorkspace(project,name='Main workspace'){
  const d=qa5Prefs().workspace_defaults||{},mode=['direct','team','council'].includes(d.orchestration)?d.orchestration:'direct',seats=Math.max(1,Math.min(8,Number(d.seats||2)));
  return {id:`pws-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,7)}`,name,
    widgets:[{id:`pw-chat-${Date.now()}`,type:'chat',title:'Workspace chat',col:6,row:5},{id:`pw-tasks-${Date.now()+1}`,type:'tasks',title:'Tasks',col:6,row:4},{id:`pw-notes-${Date.now()+2}`,type:'notes',title:'Notes',col:4,row:4}],
    orchestration:{mode,supervisor:{model:'auto',agent:'agent.md'},team:{model:'auto',agent:'agent.md',count:seats},council:{model:'auto',agent:'agent.md',count:seats}}};
}

function qa4Workspaces(project){const rows=qa4ProjectUI(project).workspaces;if(Array.isArray(rows)&&rows.length)return rows;if(!project.__qa4workspaces)project.__qa4workspaces=[qa4DefaultWorkspace(project)];return project.__qa4workspaces;}
function qa4ActiveProject(){return qa4ProjectHub.projects.find(p=>p.id===qa4ProjectHub.activeProjectID)||qa4ProjectHub.projects[0]||null;}
function qa4ActiveWorkspace(){const p=qa4ActiveProject();if(!p)return null;const rows=qa4Workspaces(p);return rows.find(w=>w.id===qa4ProjectHub.activeWorkspaceID)||rows[0]||null;}
function qa4PolicyWithUI(project,patch){const policy=qa4ProjectPolicy(project);return {...policy,onepane_ui:{...qa4ProjectUI(project),...patch}};}
async function qa4SaveProjectUI(project,patch){if(!project)return null;const updated=await apiRequest(`/v1/projects/${encodeURIComponent(project.id)}`,{method:'PATCH',body:JSON.stringify({expected_revision:Number(project.revision||1),project_policy:qa4PolicyWithUI(project,patch)})});const i=qa4ProjectHub.projects.findIndex(x=>x.id===project.id);if(i>=0)qa4ProjectHub.projects[i]=updated;return updated;}
async function qa4SaveProjectWorkspaces(project,workspaces){return qa4SaveProjectUI(project,{workspaces});}
async function qa4EnsureProjectRuntime(project){
  try{return await apiRequest(`/v1/projects/${encodeURIComponent(project.id)}/runtime`);}catch(ex){
    if(!/404|not found/i.test(String(ex?.message||ex)))throw ex;
    return apiRequest(`/v1/projects/${encodeURIComponent(project.id)}/runtime`,{method:'POST',body:JSON.stringify({isolation_mode:'sandboxed_container',desired_state:'stopped'})});
  }
}
async function qa4ApplyProjectSandboxRuntime(project,sandbox){
  const runtime=await qa4EnsureProjectRuntime(project);
  const external=Boolean(sandbox.internet||sandbox.lan);
  const networkPolicy=external?{mode:'external',ingress:'proxy_only',egress:[{kind:'external'}]}:{mode:'deny_by_default',ingress:'proxy_only',egress:[]};
  const filesystemPolicy={root:'ephemeral',project_workspace:{mount:'/workspace',mode:'read_write'},host_mounts:[],docker_socket:false,device_passthrough:false,no_new_privileges:true};
  return apiRequest(`/v1/project-runtimes/${encodeURIComponent(runtime.id)}/policy`,{method:'PATCH',body:JSON.stringify({expected_revision:Number(runtime.revision||1),network_policy:networkPolicy,filesystem_policy:filesystemPolicy})});
}
function qa4ModelOptions(){
  const opts=[['auto','Automatic / scheduler selected']];
  for(const c of qa4ProjectHub.candidates.filter(x=>x.kind==='model_deployment'&&x.schedulable)){opts.push([`candidate:${c.id}`,`${c.local?'Local':'Cloud'} · ${c.display_name||c.provider||c.id}${c.qualification?` · ${c.qualification}`:''}`]);}
  return opts;
}
function qa4AgentOptions(){const opts=[['agent.md','Project agent.md'],['onepane-default','OnePane default agent']];for(const c of qa4ProjectHub.candidates.filter(x=>x.kind==='agent_runtime'&&x.schedulable)){opts.push([`candidate:${c.id}`,`Runtime · ${c.display_name||c.provider||c.id}`]);}for(const a of qa4ProjectHub.agentPresets){const id=a.id||a.preset_id;if(id)opts.push([`runtime:${id}`,a.display_name||a.name||id]);}return opts;}
function qa4RouteSelection(workspace,role='supervisor'){const r=workspace?.orchestration?.[role]||{};const agent=String(r.agent||'');const model=String(r.model||'');const value=agent.startsWith('candidate:')?agent:model.startsWith('candidate:')?model:'';return {candidate_id:value?value.slice('candidate:'.length):'',agent_profile:agent&&!agent.startsWith('candidate:')?agent:'',model:model};}
function qa4OptionRows(rows,value){return rows.map(([v,l])=>`<option value="${escapeHtml(v)}" ${String(value)===String(v)?'selected':''}>${escapeHtml(l)}</option>`).join('');}

async function qa4LoadProjectHub(force=false){
  if(!force&&qa4ProjectHub.projects.length)return;
  const qs=encodeURIComponent(onepaneWorkspace);
  const [projects,catalog,providers,presets,agents,routines,candidates]=await Promise.all([
    apiRequest(`/v1/projects?workspace_id=${qs}`).catch(()=>[]),apiRequest('/v1/local-ai/catalog').catch(()=>[]),apiRequest(`/v1/providers?workspace_id=${qs}`).catch(()=>[]),
    apiRequest('/v1/provider-presets').catch(()=>[]),apiRequest('/v1/agent-runtime-presets').catch(()=>[]),apiRequest(`/v1/routines?workspace_id=${qs}`).catch(()=>[]),
    apiRequest(`/v1/scheduler/candidates?workspace_id=${qs}&capability_id=inference.general&role_name=general`).catch(()=>[])
  ]);
  qa4ProjectHub.projects=Array.isArray(projects)?projects:[];qa4ProjectHub.catalog=Array.isArray(catalog)?catalog:[];qa4ProjectHub.providers=Array.isArray(providers)?providers:[];qa4ProjectHub.providerPresets=Array.isArray(presets)?presets:[];qa4ProjectHub.agentPresets=Array.isArray(agents)?agents:[];qa4ProjectHub.routines=Array.isArray(routines)?routines:[];qa4ProjectHub.candidates=Array.isArray(candidates)?candidates:[];
  if(!qa4ProjectHub.activeProjectID&&qa4ProjectHub.projects[0])qa4ProjectHub.activeProjectID=qa4ProjectHub.projects[0].id;
  const p=qa4ActiveProject();if(p){const rows=qa4Workspaces(p);if(!rows.some(w=>w.id===qa4ProjectHub.activeWorkspaceID))qa4ProjectHub.activeWorkspaceID=rows[0]?.id||'';}
}

function qa4TriggerLabel(r){const t=qa4JSON(r?.TriggerJSON||r?.trigger_json||{},{});if(t.kind==='interval')return `Every ${Math.max(1,Math.round(Number(t.every_seconds||0)/60))} min`;if(t.kind==='daily'){const days=Array.isArray(t.weekdays)&&t.weekdays.length?` · days ${t.weekdays.join(',')}`:'';return `${t.local_time||'Daily'}${days}`;}return t.kind||'Scheduled';}
function qa4RoutineObjective(r){const p=qa4JSON(r?.PolicyJSON||r?.policy_json||{},{});return p.objective||r.name||'Scheduled task';}
async function renderTasks(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Tasks','One-off execution plus recurring and scheduled work','<button class="btn" id="newScheduledTask">New scheduled task</button><button class="btn primary" id="newTaskButton">New task</button>')}<div class="subtabs"><button class="subtab active" data-task-tab="current">Task list</button><button class="subtab" data-task-tab="scheduled">Recurring / Scheduled</button></div><div id="tasksBody" class="table-shell"><div class="widget-body">Loading tasks…</div></div></section>`;
  $('#newTaskButton').onclick=openNewTask;$('#newScheduledTask').onclick=qa4OpenScheduledTask;
  let tasks=[],routines=[];try{[tasks,routines]=await Promise.all([apiRequest(`/v1/tasks?workspace_id=${encodeURIComponent(onepaneWorkspace)}&limit=250`),apiRequest(`/v1/routines?workspace_id=${encodeURIComponent(onepaneWorkspace)}`)]);}catch(ex){$('#tasksBody').innerHTML=`<div class="widget-body error">${escapeHtml(ex.message)}</div>`;return;}
  liveOps.tasks=Array.isArray(tasks)?tasks:[];qa4ProjectHub.routines=Array.isArray(routines)?routines:[];syncLiveNotifications();
  const draw=(tab='current')=>{$$('[data-task-tab]').forEach(b=>b.classList.toggle('active',b.dataset.taskTab===tab));if(tab==='scheduled'){$('#tasksBody').innerHTML=`<table class="data-table"><thead><tr><th>Name / Objective</th><th>Schedule</th><th>Status</th><th>Timezone</th><th>Updated</th></tr></thead><tbody>${qa4ProjectHub.routines.length?qa4ProjectHub.routines.map(r=>`<tr data-inspect-kind="routine" data-inspect-id="${escapeHtml(r.id)}"><td><strong>${escapeHtml(r.name||qa4RoutineObjective(r))}</strong><div class="list-meta">${escapeHtml(qa4RoutineObjective(r))}</div></td><td>${escapeHtml(qa4TriggerLabel(r))}</td><td><span class="pill ${r.status==='active'?'good':''}">${escapeHtml(r.status||'active')}</span></td><td>${escapeHtml(r.timezone||'')}</td><td>${r.updated_at?escapeHtml(new Date(Number(r.updated_at)).toLocaleString()):''}</td></tr>`).join(''):'<tr><td colspan="5" class="muted-cell">No recurring or scheduled tasks yet.</td></tr>'}</tbody></table>`;}else{$('#tasksBody').innerHTML=`<table class="data-table"><thead><tr><th>Objective</th><th>State</th><th>Scheduling</th><th>Priority</th><th>Updated</th></tr></thead><tbody>${liveOps.tasks.length?liveOps.tasks.map(t=>`<tr data-inspect-kind="task" data-inspect-id="${escapeHtml(t.id)}"><td><strong>${escapeHtml(t.objective||t.id)}</strong><div class="list-meta">${escapeHtml(t.project_id||t.id||'')}</div></td><td><span class="pill ${['failed','blocked','waiting_approval'].includes(t.state)?'warn':t.state==='complete'?'good':''}">${escapeHtml(t.state||'')}</span></td><td>${escapeHtml(t.scheduling_class||'')}</td><td>${Number(t.priority||0)}</td><td>${t.updated_at?escapeHtml(new Date(Number(t.updated_at)).toLocaleString()):''}</td></tr>`).join(''):'<tr><td colspan="5" class="muted-cell">No tasks yet.</td></tr>'}</tbody></table>`;}bindViewActions($('#tasksBody'));};
  $$('[data-task-tab]').forEach(b=>b.onclick=()=>draw(b.dataset.taskTab));draw('current');
}
function qa4OpenScheduledTask(){
  const tz=Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC';
  openModal('New scheduled task',`<form id="qa4RoutineForm" class="qa-form"><label>Name<input name="name" required placeholder="Daily system review"></label><label>Objective<textarea name="objective" rows="3" required></textarea></label><div class="form-grid"><label>Schedule<select name="kind" id="qa4RoutineKind"><option value="interval">Interval</option><option value="daily">Daily / selected weekdays</option></select></label><label>Timezone<input name="timezone" value="${escapeHtml(tz)}"></label></div><div id="qa4Interval"><label>Every (minutes)<input name="minutes" type="number" min="1" value="60"></label></div><div id="qa4Daily" class="hidden"><label>Local time<input name="local_time" type="time" value="08:00"></label><label>Weekdays (1=Mon … 7=Sun)<input name="weekdays" value="1,2,3,4,5"></label></div><label>Priority<input name="priority" type="number" value="0" min="-100" max="100"></label><div class="error" id="qa4RoutineError"></div><button class="btn primary" type="submit">Create scheduled task</button></form>`);
  $('#qa4RoutineKind').onchange=()=>{$('#qa4Interval').classList.toggle('hidden',$('#qa4RoutineKind').value!=='interval');$('#qa4Daily').classList.toggle('hidden',$('#qa4RoutineKind').value!=='daily');};
  $('#qa4RoutineForm').onsubmit=async e=>{e.preventDefault();const f=Object.fromEntries(new FormData(e.currentTarget));const trigger=f.kind==='interval'?{kind:'interval',every_seconds:Math.max(60,Number(f.minutes||1)*60)}:{kind:'daily',local_time:f.local_time||'08:00',weekdays:String(f.weekdays||'').split(',').map(x=>Number(x.trim())).filter(x=>x>=1&&x<=7)};try{await apiRequest('/v1/routines',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,name:f.name,timezone:f.timezone||tz,trigger,policy:{catch_up:'latest',max_catch_up:10,objective:f.objective,priority:Number(f.priority||0)}})});closeModal();await renderTasks();notice('Scheduled task created.');}catch(ex){$('#qa4RoutineError').textContent=ex.message;}};
}

function qa4WorkspaceCatalogue(){return [['tasks','Task list'],['scheduled','Scheduled tasks'],['models','Model routing'],['nodes','Nodes'],['activity','Recent activity'],['attention','Attention'],['notes','Notepad'],['chat','Model chat']];}
function qa4WorkspaceWidgetContent(w,project,workspace){
  if(w.type==='tasks'){const rows=liveOps.tasks.filter(t=>!t.project_id||t.project_id===project.id).slice(0,8);return rows.length?`<ul class="list">${rows.map(t=>`<li class="list-row" data-inspect-kind="task" data-inspect-id="${escapeHtml(t.id)}"><div class="list-main"><div class="list-title">${escapeHtml(t.objective||t.id)}</div><div class="list-meta">${escapeHtml(t.state||'created')}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No tasks for this project.</div>';}
  if(w.type==='scheduled'){return qa4ProjectHub.routines.length?`<ul class="list">${qa4ProjectHub.routines.slice(0,8).map(r=>`<li class="list-row" data-inspect-kind="routine" data-inspect-id="${escapeHtml(r.id)}"><div class="list-main"><div class="list-title">${escapeHtml(r.name||qa4RoutineObjective(r))}</div><div class="list-meta">${escapeHtml(qa4TriggerLabel(r))}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No scheduled tasks.</div>';}
  if(w.type==='models'){const o=workspace.orchestration||{},mode=typeof qa8Mode==='function'?qa8Mode(o.mode):(o.mode==='team'||o.mode==='council'?o.mode:'direct'),direct=o.supervisor||{},active=mode==='team'?(o.team||{}):mode==='council'?(o.council||{}):direct;return `<div class="widget-body"><strong>${escapeHtml(titleCase(mode))}</strong><div class="list-meta">Primary: ${escapeHtml(active.model||'auto')} · ${escapeHtml(active.agent||(mode==='direct'?'agent.md':'onepane-default'))}</div><div class="list-meta">Modes: Direct · Team · Council</div></div>`;}
  if(w.type==='nodes')return liveOps.nodes.length?`<ul class="list">${liveOps.nodes.slice(0,6).map(n=>`<li class="list-row" data-inspect-kind="node" data-inspect-id="${escapeHtml(n.id||n.node_id||'local')}"><div class="list-main"><div class="list-title">${escapeHtml(n.display_name||n.node_id||n.id||'Node')}</div><div class="list-meta">${escapeHtml(n.status||n.state||'registered')}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No node records.</div>';
  if(w.type==='activity')return liveOps.events.length?`<ul class="list">${[...liveOps.events].slice(-6).reverse().map(e=>`<li class="list-row" data-inspect-kind="event" data-inspect-id="${escapeHtml(e.id||e.sequence||'event')}"><div class="list-main"><div class="list-title">${escapeHtml(eventLabel(e))}</div><div class="list-meta">${escapeHtml(eventTime(e))}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No recent activity.</div>';
  if(w.type==='attention')return ATTENTION_ITEMS.length?`<ul class="list">${ATTENTION_ITEMS.slice(0,6).map((n,i)=>`<li class="list-row" data-inspect-kind="attention" data-inspect-id="${i}"><div class="list-main"><div>${escapeHtml(n.title)}</div><div class="list-meta">${escapeHtml(n.detail)}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">Nothing requires attention.</div>';
  if(w.type==='notes'){const key=`project:${project.id}:workspace:${workspace.id}`;return `<textarea class="workspace-note" data-project-note="${escapeHtml(key)}" placeholder="Workspace notes…">${escapeHtml(qa4ContextNote(key))}</textarea>`;}
  if(w.type==='chat')return `<div class="widget-body"><strong>Workspace chat</strong><p class="page-subtitle">Messages become governed interactive Tasks using this workspace's routing policy.</p><button class="btn" data-project-chat="${escapeHtml(workspace.id)}">Open chat</button></div>`;
  return `<div class="widget-body">${escapeHtml(w.title||w.type)}</div>`;
}
function qa4RenderWorkspaceWidget(w,project,workspace){const edit=!!state.projectWorkspaceEdit;const controls=edit?`<div class="dashboard-edit-bar"><span class="dashboard-drag">⋮⋮</span><strong>${escapeHtml(w.title)}</strong><span class="dashboard-edit-spacer"></span><button class="tiny" data-pw-move="up" data-pw-id="${w.id}">↑</button><button class="tiny" data-pw-move="down" data-pw-id="${w.id}">↓</button><select class="dashboard-size" data-pw-size data-pw-id="${w.id}">${['small','medium','large','wide','full'].map(n=>`<option value="${n}">${titleCase(n)}</option>`).join('')}</select><button class="tiny danger" data-pw-remove="${w.id}">×</button></div>`:'';return `<section class="workspace-widget dashboard-widget ${edit?'editable':''}" draggable="${edit}" data-pw-widget="${w.id}" style="grid-column:span ${Math.max(2,Math.min(12,w.col||4))};grid-row:span ${Math.max(2,Math.min(10,w.row||4))}">${controls}<div class="dashboard-widget-content"><div class="widget-handle"><strong>${escapeHtml(w.title)}</strong></div>${qa4WorkspaceWidgetContent(w,project,workspace)}</div></section>`;}

async function renderProjects(){
  const qa31ProjectRenderEpoch=qa31ViewEpoch;
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Projects','Projects contain one or more working surfaces, sandbox policy, routing policy and governed model chat','<button class="btn primary" id="qa4NewProject">New project</button>')}<div class="project-hub-loading widget-body">Loading projects…</div></section>`;
  $('#qa4NewProject').onclick=openProjectDialog;
  await qa4LoadProjectHub(true);if(qa31ProjectRenderEpoch!==qa31ViewEpoch||currentTab()?.route!=='projects')return;const host=$('.project-hub-loading');
  if(!qa4ProjectHub.projects.length){host.outerHTML='<div class="empty-state"><strong>No projects yet.</strong><br>Create a project to get a workspace, sandbox policy and model chat.</div>';return;}
  const p=qa4ActiveProject(),workspaces=qa4Workspaces(p),ws=qa4ActiveWorkspace();
  host.outerHTML=`<div class="project-hub"><aside class="project-rail"><div class="project-rail-title">Projects</div>${qa4ProjectHub.projects.map(x=>`<button class="project-choice ${x.id===p.id?'active':''}" data-qa4-project="${escapeHtml(x.id)}"><strong>${escapeHtml(x.name||'Project')}</strong><span>${escapeHtml(x.description||'')}</span></button>`).join('')}</aside><section class="project-main"><div class="project-toolbar"><div><h2>${escapeHtml(p.name||'Project')}</h2><div class="page-subtitle">${escapeHtml(p.description||'')}</div></div><div class="toolbar"><button class="btn" id="qa4ProjectSettings">Project settings</button><button class="btn" id="qa4AddWorkspace">Add workspace</button><button class="btn ${state.projectWorkspaceEdit?'primary':''}" id="qa4EditWorkspace">${state.projectWorkspaceEdit?'Done':'Edit layout'}</button>${state.projectWorkspaceEdit?'<button class="btn" id="qa4AddComponent">Add component</button>':''}</div></div><div class="workspace-tabs">${workspaces.map(x=>`<button class="workspace-tab ${x.id===ws.id?'active':''}" data-qa4-workspace="${escapeHtml(x.id)}">${escapeHtml(x.name)}</button>`).join('')}</div><div class="workspace-context-bar"><div><strong>${escapeHtml(ws.name)}</strong><span class="list-meta"> · project sandbox ${qa4ProjectSandbox(p).internet?'internet allowed':'internet blocked'} · ${escapeHtml(titleCase(ws.orchestration?.mode||'supervisor'))}</span></div><button class="btn" id="qa4WorkspaceSettings">Configure workspace</button></div><div class="workspace-grid ${state.projectWorkspaceEdit?'editing':''}" id="qa4WorkspaceGrid">${(ws.widgets||[]).map(w=>qa4RenderWorkspaceWidget(w,p,ws)).join('')}</div><section class="workspace-chat-panel"><div class="card-header"><div><div class="card-title">${escapeHtml(ws.name)} chat</div><div class="list-meta">Governed interactive task · model/agent routing comes from workspace configuration</div></div></div><div class="workspace-chat-history" id="qa4ChatHistory"></div><form id="qa4WorkspaceChat" class="workspace-chat-composer"><textarea id="qa4ChatInput" rows="2" placeholder="Ask the models assigned to this workspace…"></textarea><button class="btn primary">Send</button></form></section></section></div>`;
  $$('[data-qa4-project]').forEach(b=>b.onclick=()=>{qa4ProjectHub.activeProjectID=b.dataset.qa4Project;qa4ProjectHub.activeWorkspaceID='';renderProjects();});
  $$('[data-qa4-workspace]').forEach(b=>b.onclick=()=>{qa4ProjectHub.activeWorkspaceID=b.dataset.qa4Workspace;renderProjects();});
  $('#qa4AddWorkspace').onclick=()=>qa4AddWorkspace(p);$('#qa4ProjectSettings').onclick=()=>qa4Inspect('project',p.id,p.name,p);$('#qa4WorkspaceSettings').onclick=()=>qa4Inspect('workspace',ws.id,`${p.name} / ${ws.name}`,{project:p,workspace:ws});
  $('#qa4EditWorkspace').onclick=()=>{state.projectWorkspaceEdit=!state.projectWorkspaceEdit;persist();renderProjects();};$('#qa4AddComponent')?.addEventListener('click',()=>qa4AddWorkspaceComponent(p,ws));
  qa4BindWorkspaceEdit(p,ws);bindViewActions($('#viewHost'));qa4BindProjectNotes($('#viewHost'));
  $('#qa4WorkspaceChat').onsubmit=e=>{e.preventDefault();qa4SendWorkspaceChat(p,ws,$('#qa4ChatInput').value.trim(),$('#qa4ChatHistory'));};
  $$('[data-project-chat]').forEach(b=>b.onclick=()=>$('#qa4ChatInput')?.focus());
}
function qa4AddWorkspace(project){openModal('Add workspace',`<form id="qa4AddWorkspaceForm" class="qa-form"><label>Name<input name="name" required placeholder="Development"></label><div class="page-subtitle">Each workspace inherits secure defaults and can then override sandbox/model/agent routing.</div><button class="btn primary">Create workspace</button></form>`);$('#qa4AddWorkspaceForm').onsubmit=async e=>{e.preventDefault();const name=new FormData(e.currentTarget).get('name').trim();const rows=qa4Workspaces(project);const ws=qa4DefaultWorkspace(project,name);try{await qa4SaveProjectWorkspaces(project,[...rows,ws]);qa4ProjectHub.activeWorkspaceID=ws.id;closeModal();renderProjects();notice('Workspace created.');}catch(ex){notice(ex.message,'bad');}};}
function qa4AddWorkspaceComponent(project,workspace){const used=new Set((workspace.widgets||[]).map(x=>x.type));const available=qa4WorkspaceCatalogue().filter(([t])=>!used.has(t)||['notes','chat'].includes(t));openModal('Add workspace component',`<div class="component-picker-grid">${available.map(([t,title])=>`<button class="component-choice" data-qa4-add-component="${t}"><strong>${title}</strong><span>Add to ${escapeHtml(workspace.name)}</span></button>`).join('')}</div>`);$$('[data-qa4-add-component]').forEach(b=>b.onclick=async()=>{const [type,title]=qa4WorkspaceCatalogue().find(x=>x[0]===b.dataset.qa4AddComponent);workspace.widgets=workspace.widgets||[];workspace.widgets.push({id:`pw-${type}-${Date.now().toString(36)}`,type,title,col:type==='tasks'||type==='scheduled'?6:4,row:4});try{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));closeModal();renderProjects();}catch(ex){notice(ex.message,'bad');}});}
function qa4BindWorkspaceEdit(project,workspace){if(!state.projectWorkspaceEdit)return;let drag='';$$('[data-pw-widget]').forEach(el=>{el.ondragstart=()=>drag=el.dataset.pwWidget;el.ondragover=e=>e.preventDefault();el.ondrop=async e=>{e.preventDefault();const a=workspace.widgets.findIndex(x=>x.id===drag),b=workspace.widgets.findIndex(x=>x.id===el.dataset.pwWidget);if(a<0||b<0||a===b)return;[workspace.widgets[a],workspace.widgets[b]]=[workspace.widgets[b],workspace.widgets[a]];await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();};});$$('[data-pw-size]').forEach(sel=>{const w=workspace.widgets.find(x=>x.id===sel.dataset.pwId);sel.value=nearestWorkspaceSize(w);sel.onchange=async()=>{Object.assign(w,workspacePreset(sel.value,w));await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();};});$$('[data-pw-move]').forEach(b=>b.onclick=async()=>{const i=workspace.widgets.findIndex(x=>x.id===b.dataset.pwId),j=i+(b.dataset.pwMove==='up'?-1:1);if(i<0||j<0||j>=workspace.widgets.length)return;[workspace.widgets[i],workspace.widgets[j]]=[workspace.widgets[j],workspace.widgets[i]];await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();});$$('[data-pw-remove]').forEach(b=>b.onclick=async()=>{workspace.widgets=workspace.widgets.filter(x=>x.id!==b.dataset.pwRemove);await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();});}
async function qa4SendWorkspaceChat(project,workspace,text,history){if(!text)return;history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message user"><strong>You</strong><span>${escapeHtml(text)}</span></div>`);$('#qa4ChatInput').value='';const mode=workspace.orchestration?.mode||'supervisor';const role=mode==='direct'?'supervisor':mode;const selected=qa4RouteSelection(workspace,role);const completion={type:'operator_review',onepane_routing:{mode,role,candidate_id:selected.candidate_id,agent_profile:selected.agent_profile,project_workspace_id:workspace.id}};try{const task=await apiRequest('/v1/tasks',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,project_id:project.id,objective:text,priority:10,scheduling_class:'user_interactive',completion})});history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system"><strong>OnePane</strong><span>Queued as governed task ${escapeHtml(task.id||'')}. ${selected.candidate_id?`Pinned scheduler candidate: ${escapeHtml(selected.candidate_id)}.`:'Scheduler will choose the best eligible candidate.'} ${selected.agent_profile?`Agent profile: ${escapeHtml(selected.agent_profile)}.`:''}</span></div>`);liveOps.lastRefresh=0;}catch(ex){history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system error"><strong>OnePane</strong><span>${escapeHtml(ex.message)}</span></div>`);}}

function qa4ContextKey(kind,id){return `${kind}:${id}`;}
function qa4ContextNote(key){try{return qa4JSON(localStorage.getItem(QA4_NOTES_KEY)||'{}',{})[key]||'';}catch{return '';}}
function qa4SetContextNote(key,value){const all=qa4JSON(localStorage.getItem(QA4_NOTES_KEY)||'{}',{});all[key]=value;localStorage.setItem(QA4_NOTES_KEY,JSON.stringify(all));}
const A32_RESEARCH_INSPECTOR=new Map();
async function a32LoadResearchIntegrity(taskID){
  if(!taskID||A32_RESEARCH_INSPECTOR.get(taskID)?.state==='loading')return;
  A32_RESEARCH_INSPECTOR.set(taskID,{state:'loading'});
  try{
    const session=await apiRequest(`/v1/tasks/${encodeURIComponent(taskID)}/team-session`);
    const sessionID=session.id||session.ID;
    if(!sessionID)throw new Error('Team/Council session not found');
    const [data,turnRows]=await Promise.all([apiRequest(`/v1/team-sessions/${encodeURIComponent(sessionID)}/manifest`),apiRequest(`/v1/team-sessions/${encodeURIComponent(sessionID)}/turns`).catch(()=>[])]);
    const manifest=data.manifest||data.Manifest||{},snapshot=a31JSON(manifest.snapshot??manifest.Snapshot,{});
    const researchMode=Boolean(manifest.research_mode??manifest.ResearchMode??snapshot.research_mode);
    A32_RESEARCH_INSPECTOR.set(taskID,{state:'ready',researchMode,session,manifest,snapshot,bindings:a31Array(data.seat_bindings||data.SeatBindings),turns:a31Array(turnRows)});
  }catch(ex){
    A32_RESEARCH_INSPECTOR.set(taskID,{state:'none',error:ex?.message||String(ex)});
  }
  if(qa4Inspector.kind==='task'&&String(qa4Inspector.id)===String(taskID))renderInspector();
}
function a32ResearchIntegrity(taskID){
  const row=A32_RESEARCH_INSPECTOR.get(taskID);
  if(!row||row.state==='loading')return '<div class="page-subtitle">Loading Research integrity provenance…</div>';
  if(row.state!=='ready'||!row.researchMode)return '<div class="page-subtitle">This task is not backed by a Research Council manifest.</div>';
  const m=row.manifest||{},s=row.snapshot||{},session=row.session||{},r=a31Object(s.research),bindings=a31Array(row.bindings),turns=a31Array(row.turns),members=a31Array(s.members),memberByID=new Map(members.map(x=>[String(x.id),x]));
  const sha=m.snapshot_sha256||m.SnapshotSHA256||'',mode=m.execution_mode||m.ExecutionMode||s.execution_mode||'council',status=session.status||session.Status||'',round=Number(session.round_number??session.RoundNumber??0),critiqueRounds=Math.max(1,Math.min(5,Number(r.critique_rounds||2))),totalRounds=1+critiqueRounds+(r.synthesis_pass?1:0);
  const currentTurns=turns.filter(t=>Number(t.round_number??t.RoundNumber??0)===round),phase=currentTurns[0]?.research_phase||currentTurns[0]?.ResearchPhase||(round===1?'independent':round>1&&round<=1+critiqueRounds?'critique':round===totalRounds?'synthesis':'');
  const succeeded=currentTurns.filter(t=>(t.status||t.Status)==='succeeded').length,incomplete=currentTurns.filter(t=>(t.status||t.Status)!=='succeeded').length,blocked=currentTurns.find(t=>['blocked','failed'].includes(t.status||t.Status));
  const retryAfter=Number(blocked?.retry_after??blocked?.RetryAfter??0),pauseReason=blocked?.error_text||blocked?.ErrorText||'Provider unavailable',paused=status==='paused_for_deliberation';
  const pausePanel=paused?`<div class='research-pause-panel'><div><strong>Research round paused</strong><div class='list-meta'>Round ${round} · ${escapeHtml(titleCase(phase||'research'))} · ${succeeded} completed · ${incomplete} incomplete</div><div class='page-subtitle'>${escapeHtml(pauseReason)}</div>${retryAfter?`<div class='page-subtitle'>Automatic retry eligible: ${escapeHtml(new Date(retryAfter).toLocaleString())}</div>`:`<div class='page-subtitle'>No provider reset time was supplied. This round stays paused until manually retried.</div>`}</div><button class='btn primary' id='a32RetryResearchRound' data-session-id='${escapeHtml(session.id||session.ID||'')}'>Retry this round</button></div>`:'';
  const flags=[
    ['Exact candidate pinning',r.pin_models],['No substitution',r.disable_model_substitution],['Same-candidate retries',r.same_model_retries],
    ['Preserve failed seats',r.preserve_failed_seats],['Independent first pass',r.independent_first_pass],['Scoped evidence',r.scoped_evidence],
    ['Raw outputs',r.record_raw_outputs],['Full provenance',r.full_provenance],['Require all seats',r.require_all_seats],
    ['Anonymised cross-critique',r.anonymized_cross_critique],['Synthesis pass',r.synthesis_pass]
  ];
  const seats=members.filter(x=>x.member_kind!=='human').map(member=>{
    const b=bindings.find(x=>String(x.member_id??x.MemberID)===String(member.id)),candidate=a31JSON(b?.candidate_snapshot??b?.CandidateSnapshot,{});
    const candidateID=b?.candidate_id||b?.CandidateID||'Not resolved yet',kind=b?.candidate_kind||b?.CandidateKind||'—';
    const provider=candidate.provider||candidate.Provider||'',node=candidate.node_id||candidate.NodeID||'',compute=candidate.compute_mode||candidate.ComputeMode||'';
    return `<tr><td><strong>${escapeHtml(member.display_name||member.role_name||member.id)}</strong><div class="list-meta">${escapeHtml(member.profile?.id||member.role_name||'')}</div></td><td class="mono-cell">${escapeHtml(candidateID)}</td><td>${escapeHtml(titleCase(kind))}</td><td>${escapeHtml([provider,node,compute].filter(Boolean).join(' · ')||'Pending first resolution')}</td></tr>`;
  }).join('');
  return `<div class="research-integrity-panel"><div class="card-header"><div><div class="card-title">Research Integrity</div><div class="list-meta">Immutable session manifest, automatic multi-round workflow and seat provenance</div></div><span class="pill ${paused?'warn':'good'}">${paused?'Paused':'Research'}</span></div>${pausePanel}<dl class="definition-grid"><dt>Execution</dt><dd>${escapeHtml(titleCase(mode))}</dd><dt>Workflow</dt><dd>Independent → ${critiqueRounds} critique round${critiqueRounds===1?'':'s'} → ${r.synthesis_pass?'Synthesis':'Complete'}</dd><dt>Current round</dt><dd>${round?`${round} / ${totalRounds} · ${escapeHtml(titleCase(phase||'pending'))}`:'Not started'}</dd><dt>Status</dt><dd>${escapeHtml(titleCase(status||'unknown'))}</dd><dt>Manifest SHA-256</dt><dd class="mono-cell">${escapeHtml(sha||'Unavailable')}</dd><dt>Frozen Team revision</dt><dd>${escapeHtml(String(s.team_revision??'—'))}</dd><dt>Task</dt><dd class="mono-cell">${escapeHtml(s.task_id||taskID)}</dd></dl><div class="tool-chip-row">${flags.map(([name,on])=>`<span class="tool-chip ${on?'good':''}">${on?'✓':'○'} ${escapeHtml(name)}</span>`).join('')}</div><div class="table-shell" style="margin-top:12px"><table class="data-table"><thead><tr><th>Seat</th><th>Bound candidate</th><th>Kind</th><th>Resolved provenance</th></tr></thead><tbody>${seats||'<tr><td colspan="4">No frozen Research seats.</td></tr>'}</tbody></table></div></div>`;
}
async function a32RetryResearchRound(taskID,sessionID){
  if(!sessionID)return;
  const btn=$('#a32RetryResearchRound');if(btn){btn.disabled=true;btn.textContent='Retrying…'}
  try{await apiRequest(`/v1/team-sessions/${encodeURIComponent(sessionID)}/research/retry`,{method:'POST',body:'{}'});A32_RESEARCH_INSPECTOR.delete(taskID);await a32LoadResearchIntegrity(taskID);notice('Research Council resumed on the same round.');}
  catch(ex){notice(ex.message,'bad');if(btn){btn.disabled=false;btn.textContent='Retry this round'}}
}
function qa4InspectorTabs(){let extra=[];try{extra=JSON.parse(localStorage.getItem(QA4_INSPECTOR_TAB_KEY)||'[]')}catch{}const research=qa4Inspector.kind==='task'&&A32_RESEARCH_INSPECTOR.get(qa4Inspector.id)?.researchMode?['research']:[];return ['overview',...research,...(qa4Inspector.kind==='project'||qa4Inspector.kind==='workspace'?['configuration']:[]),...extra.filter(x=>['notes','chat'].includes(x))].filter((x,i,a)=>a.indexOf(x)===i);}
function qa4Inspect(kind,id,title,data){qa4Inspector={kind,id,title:title||id,data:data||{}};qa4InspectorTab='overview';setInspectorOpen(true);renderInspector();if(kind==='task')a32LoadResearchIntegrity(id);}
function qa4FindEntity(kind,id){if(kind==='task')return liveOps.tasks.find(x=>x.id===id)||{};if(kind==='routine')return qa4ProjectHub.routines.find(x=>x.id===id)||{};if(kind==='node')return liveOps.nodes.find(x=>(x.id||x.node_id)===id)||{};if(kind==='provider')return liveOps.providers.find(x=>x.id===id)||{};if(kind==='event')return liveOps.events.find(x=>String(x.id||x.sequence)===String(id))||{};if(kind==='attention')return ATTENTION_ITEMS[Number(id)]||{};return qa4Inspector.data||{};}
function qa4InspectorOverview(){const d=qa4FindEntity(qa4Inspector.kind,qa4Inspector.id);if(qa4Inspector.kind==='workspace'){const w=d.workspace||{},p=d.project||{},sb=qa4ProjectSandbox(p);return `<dl class="definition-grid"><dt>Project</dt><dd>${escapeHtml(p.name||p.id||'')}</dd><dt>Workspace</dt><dd>${escapeHtml(w.name||w.id||'')}</dd><dt>Project sandbox</dt><dd>${sb.internet?'Internet allowed':'Internet blocked'} · ${sb.computer?'Computer allowed':'Computer blocked'}</dd><dt>Mode</dt><dd>${escapeHtml(titleCase(w.orchestration?.mode||'supervisor'))}</dd></dl>`;}if(qa4Inspector.kind==='project'){return `<dl class="definition-grid"><dt>Name</dt><dd>${escapeHtml(d.name||'')}</dd><dt>Status</dt><dd>${escapeHtml(d.status||'')}</dd><dt>Revision</dt><dd>${Number(d.revision||0)}</dd><dt>ID</dt><dd class="mono-cell">${escapeHtml(d.id||'')}</dd></dl>`;}const entries=Object.entries(d||{}).filter(([,v])=>['string','number','boolean'].includes(typeof v)).slice(0,14);return entries.length?`<dl class="definition-grid">${entries.map(([k,v])=>`<dt>${escapeHtml(titleCase(k))}</dt><dd>${escapeHtml(v)}</dd>`).join('')}</dl>`:'<div class="page-subtitle">No additional structured detail is available for this item.</div>';}
function qa4InspectorConfiguration(){const d=qa4Inspector.data||{};let project=d.project||d,workspace=d.workspace;if(qa4Inspector.kind==='project'){return `<div class="inspector-section"><strong>Project policy</strong><p class="page-subtitle">Workspace-specific sandbox and model/agent routing is configured per workspace. Project policy is durable and revision controlled.</p><button class="btn" id="qa4OpenFirstWorkspaceConfig">Configure first workspace</button></div>`;}if(!workspace||!project)return '<div class="page-subtitle">Workspace configuration unavailable.</div>';const s=qa4ProjectSandbox(project),o=workspace.orchestration||{},models=qa4ModelOptions(),agents=qa4AgentOptions();const role=(key,label)=>{const x=o[key]||{};return `<fieldset class="inspector-fieldset"><legend>${label}</legend><label>Model<select data-qa4-route-model="${key}">${qa4OptionRows(models,x.model||'auto')}</select></label><label>Agent<select data-qa4-route-agent="${key}">${qa4OptionRows(agents,x.agent||'onepane-default')}</select></label></fieldset>`;};return `<div class="inspector-config"><label>Workspace name<input id="qa4WorkspaceName" value="${escapeHtml(workspace.name||'Workspace')}"></label><fieldset class="inspector-fieldset"><legend>Project sandbox (shared by all project workspaces)</legend>${[['network','Project network'],['internet','Internet'],['lan','LAN'],['computer','Computer control'],['browser','Browser']].map(([k,l])=>`<label class="inline-check"><input type="checkbox" data-qa4-sandbox="${k}" ${s[k]?'checked':''}> ${l}</label>`).join('')}<label>Host files<select id="qa4HostFiles"><option value="workspace-only" selected>Project workspace only</option></select></label><div class="page-subtitle">Extra host mounts, Docker socket and device passthrough remain blocked by the sandbox. Internet/LAN currently share the same explicit external-network boundary on the rootless backend.</div><label>Secrets<select id="qa4SecretPolicy"><option value="none" ${s.secrets==='none'?'selected':''}>None</option><option value="selected" ${s.secrets!=='none'?'selected':''}>Selected Vault handles</option></select></label></fieldset><label>Orchestration mode<select id="qa4Mode">${['direct','team','council'].map(x=>`<option value="${x}" ${o.mode===x?'selected':''}>${titleCase(x)}</option>`).join('')}</select></label>${role('supervisor','Direct')}${role('team','Team')}${role('council','Council')}<button class="btn primary" id="qa4SaveWorkspaceConfig">Save workspace policy</button><div class="page-subtitle" id="qa4WorkspaceConfigStatus"></div></div>`;}
function qa4InspectorExtras(){let extra=[];try{extra=JSON.parse(localStorage.getItem(QA4_INSPECTOR_TAB_KEY)||'[]')}catch{}return extra.filter(x=>['notes','chat'].includes(x)).filter((x,i,a)=>a.indexOf(x)===i);}
function qa4SaveInspectorExtras(extra){localStorage.setItem(QA4_INSPECTOR_TAB_KEY,JSON.stringify(extra.filter(x=>['notes','chat'].includes(x))));}
function qa4MoveInspectorTab(tab,delta){const extra=qa4InspectorExtras(),i=extra.indexOf(tab);if(i<0)return;const j=i+delta;if(j<0||j>=extra.length)return;[extra[i],extra[j]]=[extra[j],extra[i]];qa4SaveInspectorExtras(extra);renderInspector();}
function qa4RemoveInspectorTab(tab){const extra=qa4InspectorExtras().filter(x=>x!==tab);qa4SaveInspectorExtras(extra);if(qa4InspectorTab===tab)qa4InspectorTab='overview';renderInspector();}
function renderInspector(){const tabs=qa4InspectorTabs();if(!tabs.includes(qa4InspectorTab))qa4InspectorTab='overview';const title=qa4Inspector.title||titleCase(qa4Inspector.kind),research=A32_RESEARCH_INSPECTOR.get(qa4Inspector.id)?.researchMode;$('#inspector').innerHTML=`<div class="inspector-header">Inspector</div><div class="inspector-section"><div class="inspector-title">${escapeHtml(title)}${research?'<span class="pill good" style="margin-left:auto">Research</span>':''}</div><div class="page-subtitle">${escapeHtml(titleCase(qa4Inspector.kind))}</div></div><div class="inspector-tabs qa4-inspector-tabs">${tabs.map(t=>`<span class="qa4-inspector-tab-wrap"><button class="${t===qa4InspectorTab?'active':''}" data-qa4-inspector-tab="${t}">${t==='research'?'Research Integrity':titleCase(t)}</button>${['notes','chat'].includes(t)?`<span class="qa4-inspector-tab-tools"><button title="Move tab left" data-qa4-inspector-move="${t}:-1">‹</button><button title="Move tab right" data-qa4-inspector-move="${t}:1">›</button><button title="Remove tab" data-qa4-inspector-remove="${t}">×</button></span>`:''}</span>`).join('')}<button class="inspector-tab-add" id="qa4InspectorAddTab" title="Add Inspector tab">+</button></div><div class="inspector-section" id="qa4InspectorContent"></div>`;$$('[data-qa4-inspector-tab]').forEach(b=>b.onclick=()=>{qa4InspectorTab=b.dataset.qa4InspectorTab;renderInspector();});$$('[data-qa4-inspector-remove]').forEach(b=>b.onclick=e=>{e.stopPropagation();qa4RemoveInspectorTab(b.dataset.qa4InspectorRemove);});$$('[data-qa4-inspector-move]').forEach(b=>b.onclick=e=>{e.stopPropagation();const [tab,delta]=b.dataset.qa4InspectorMove.split(':');qa4MoveInspectorTab(tab,Number(delta));});$('#qa4InspectorAddTab').onclick=qa4AddInspectorTab;const c=$('#qa4InspectorContent');if(qa4InspectorTab==='overview')c.innerHTML=qa4InspectorOverview();else if(qa4InspectorTab==='research'){c.innerHTML=a32ResearchIntegrity(qa4Inspector.id);$('#a32RetryResearchRound')?.addEventListener('click',e=>a32RetryResearchRound(qa4Inspector.id,e.currentTarget.dataset.sessionId));}else if(qa4InspectorTab==='configuration')c.innerHTML=qa4InspectorConfiguration();else if(qa4InspectorTab==='notes'){const key=qa4ContextKey(qa4Inspector.kind,qa4Inspector.id);c.innerHTML=`<textarea id="qa4InspectorNotes" class="inspector-notes" rows="14" placeholder="Notes for this item…">${escapeHtml(qa4ContextNote(key))}</textarea>`;$('#qa4InspectorNotes').oninput=e=>qa4SetContextNote(key,e.target.value);}else if(qa4InspectorTab==='chat'){c.innerHTML=`<div class="page-subtitle">Send a governed interactive Task from this context. No direct unmanaged model call is made.</div><div id="qa4InspectorChatHistory" class="mini-chat-history"></div><form id="qa4InspectorChatForm" class="mini-chat-form"><textarea rows="3" id="qa4InspectorChatInput" placeholder="Ask OnePane…"></textarea><button class="btn primary">Send</button></form>`;$('#qa4InspectorChatForm').onsubmit=e=>{e.preventDefault();qa4SendInspectorChat($('#qa4InspectorChatInput').value.trim());};}qa4BindInspectorConfig();syncPanelRestoreButtons();}
function qa4AddInspectorTab(){const tabs=qa4InspectorTabs();openModal('Add Inspector tab',`<div class="component-picker-grid">${!tabs.includes('notes')?'<button class="component-choice" data-add-inspector-tab="notes"><strong>Notepad</strong><span>Context-specific notes stored locally.</span></button>':''}${!tabs.includes('chat')?'<button class="component-choice" data-add-inspector-tab="chat"><strong>Model chat</strong><span>Governed task-based chat from the selected context.</span></button>':''}${tabs.includes('notes')&&tabs.includes('chat')?'<div class="empty-state compact">All optional Inspector tabs are enabled. Use the arrows or × on optional tabs to reorder or remove them.</div>':''}</div>`);$$('[data-add-inspector-tab]').forEach(b=>b.onclick=()=>{const extra=qa4InspectorExtras();if(!extra.includes(b.dataset.addInspectorTab))extra.push(b.dataset.addInspectorTab);qa4SaveInspectorExtras(extra);qa4InspectorTab=b.dataset.addInspectorTab;closeModal();renderInspector();});}
function qa4BindInspectorConfig(){if(qa4Inspector.kind==='project')$('#qa4OpenFirstWorkspaceConfig')?.addEventListener('click',()=>{const p=qa4Inspector.data,w=qa4Workspaces(p)[0];qa4Inspect('workspace',w.id,`${p.name} / ${w.name}`,{project:p,workspace:w});});if(qa4Inspector.kind!=='workspace'||qa4InspectorTab!=='configuration')return;const {project,workspace}=qa4Inspector.data;const extBoxes=$$('[data-qa4-sandbox="internet"],[data-qa4-sandbox="lan"]');extBoxes.forEach(x=>x.onchange=()=>{extBoxes.forEach(y=>y.checked=x.checked);});$('#qa4SaveWorkspaceConfig').onclick=async()=>{workspace.name=$('#qa4WorkspaceName').value.trim()||workspace.name;const sandbox=qa4ProjectSandbox(project);$$('[data-qa4-sandbox]').forEach(x=>sandbox[x.dataset.qa4Sandbox]=x.checked);if(sandbox.internet||sandbox.lan){sandbox.internet=true;sandbox.lan=true;}sandbox.host_files='workspace-only';sandbox.secrets=$('#qa4SecretPolicy').value;workspace.orchestration=workspace.orchestration||{};workspace.orchestration.mode=$('#qa4Mode').value;for(const key of ['supervisor','workers','team','council']){workspace.orchestration[key]=workspace.orchestration[key]||{};workspace.orchestration[key].model=$(`[data-qa4-route-model="${key}"]`).value;workspace.orchestration[key].agent=$(`[data-qa4-route-agent="${key}"]`).value;}workspace.orchestration.workers.count=Number($('[data-qa4-worker-count]').value||2);try{const updated=await qa4SaveProjectUI(project,{workspaces:qa4Workspaces(project),sandbox});await qa4ApplyProjectSandboxRuntime(updated,sandbox);qa4Inspector.data.project=updated;$('#qa4WorkspaceConfigStatus').innerHTML='<span class="good">Saved and applied to the Project Runtime.</span>';notice('Workspace and sandbox policy saved.');}catch(ex){$('#qa4WorkspaceConfigStatus').innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`;}};}
async function qa4SendInspectorChat(text){if(!text)return;const d=qa4Inspector.data||{},project=d.project||(qa4Inspector.kind==='project'?d:null),workspace=d.workspace||null;const payload={workspace_id:onepaneWorkspace,objective:text,priority:10,scheduling_class:'user_interactive'};if(project?.id)payload.project_id=project.id;if(workspace){const mode=workspace.orchestration?.mode||'supervisor',role=mode==='direct'?'supervisor':mode,selected=qa4RouteSelection(workspace,role);payload.completion={type:'operator_review',onepane_routing:{mode,role,candidate_id:selected.candidate_id,agent_profile:selected.agent_profile,project_workspace_id:workspace.id}};}const h=$('#qa4InspectorChatHistory');h.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message user"><span>${escapeHtml(text)}</span></div>`);$('#qa4InspectorChatInput').value='';try{const t=await apiRequest('/v1/tasks',{method:'POST',body:JSON.stringify(payload)});h.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system"><span>Queued ${escapeHtml(t.id||'task')} through OnePane policy.</span></div>`);}catch(ex){h.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system error"><span>${escapeHtml(ex.message)}</span></div>`);}}
function qa4BindProjectNotes(root){$$('[data-project-note]',root).forEach(x=>x.oninput=()=>qa4SetContextNote(x.dataset.projectNote,x.value));}

function metric(label,value,detail,cls=''){const id=String(label).toLowerCase().replace(/[^a-z0-9]+/g,'-');return `<div class="metric-card inspectable" data-inspect-kind="metric" data-inspect-id="${id}" data-inspect-title="${escapeHtml(label)}" data-inspect-json="${escapeHtml(JSON.stringify({label,value,detail}))}"><div class="metric-label">${escapeHtml(label)}</div><div class="metric-value ${cls}">${escapeHtml(value)}</div><div class="metric-detail">${escapeHtml(detail)}</div></div>`;}
function qa4ScheduledCard(){const rows=(liveOps.routines||qa4ProjectHub.routines||[]).slice(0,6);return card('Scheduled tasks',rows.length?`<ul class="list">${rows.map(r=>`<li class="list-row inspectable" data-inspect-kind="routine" data-inspect-id="${escapeHtml(r.id)}"><span>⟳</span><div class="list-main"><div class="list-title">${escapeHtml(r.name||qa4RoutineObjective(r))}</div><div class="list-meta">${escapeHtml(qa4TriggerLabel(r))}</div></div><span class="pill ${r.status==='active'?'good':''}">${escapeHtml(r.status||'active')}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No recurring or scheduled tasks.</div>','View all');}
function taskCard(){const rows=liveOps.tasks.filter(t=>!['complete','cancelled'].includes(String(t.state||'').toLowerCase())).slice(0,6);return card('Active Tasks',rows.length?`<ul class="list">${rows.map(t=>`<li class="list-row inspectable" data-inspect-kind="task" data-inspect-id="${escapeHtml(t.id)}"><span class="pill ${['failed','blocked','waiting_approval'].includes(t.state)?'warn':'good'}">${escapeHtml(t.state||'created')}</span><div class="list-main"><div class="list-title">${escapeHtml(t.objective||t.id||'Task')}</div><div class="list-meta">${escapeHtml(t.scheduling_class||'')} · priority ${Number(t.priority||0)}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No active tasks.</div>','View all');}
function nodesCard(){return card('Nodes',liveOps.nodes.length?`<ul class="list">${liveOps.nodes.slice(0,6).map(n=>`<li class="list-row inspectable" data-inspect-kind="node" data-inspect-id="${escapeHtml(n.id||n.node_id||'local')}"><span>⬡</span><div class="list-main"><div class="list-title">${escapeHtml(n.display_name||n.node_id||n.id||'Node')}</div><div class="list-meta">${escapeHtml(n.peer_endpoint||n.advertise_url||'local')}</div></div><span class="pill">${escapeHtml(n.status||n.state||n.trust_state||'registered')}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No node records returned.</div>','View all');}
function resourceCard(){if(!localProfileQA)return card('Resource Utilisation','<div class="empty-state compact inspectable" data-inspect-kind="resource" data-inspect-id="resource">Run Models → Detect hardware to load this node’s hardware profile.</div>');const g=Array.isArray(localProfileQA.gpus)?localProfileQA.gpus:[];return card('Resource Utilisation',`<div class="widget-body inspectable" data-inspect-kind="resource" data-inspect-id="resource"><strong>${escapeHtml(localProfileQA.cpu?.name||'CPU')}</strong><div class="list-meta">${escapeHtml(bytesQA(localProfileQA.memory?.total_bytes||0))} RAM</div>${g.map(x=>`<div class="resource-row"><span>${escapeHtml(x.name||'GPU')}</span><strong>${escapeHtml(bytesQA(x.vram_bytes||0))} VRAM</strong></div>`).join('')||'<div class="list-meta">No GPU detected.</div>'}</div>`);}
function activityCard(){const rows=[...liveOps.events].slice(-8).reverse();return card('Recent Activity',rows.length?`<ul class="list">${rows.map(e=>`<li class="list-row inspectable" data-inspect-kind="event" data-inspect-id="${escapeHtml(e.id||e.sequence||'event')}"><span>◉</span><div class="list-main"><div>${escapeHtml(eventLabel(e))}</div><div class="list-meta">${escapeHtml(e.aggregate_type||'')} · ${escapeHtml(e.aggregate_id||'')}</div></div><span class="list-meta">${escapeHtml(eventTime(e))}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No recent workspace events.</div>','View all');}
function attentionCard(){return card(`Attention (${ATTENTION_ITEMS.length})`,ATTENTION_ITEMS.length?`<ul class="list">${ATTENTION_ITEMS.map((n,i)=>`<li class="list-row inspectable" data-inspect-kind="attention" data-inspect-id="${i}"><span class="pill ${n.severity||'warn'}">!</span><div class="list-main"><div>${escapeHtml(n.title)}</div><div class="list-meta">${escapeHtml(n.detail)}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">Nothing requires approval or intervention.</div>');}
function providersCard(){return card('Cloud Provider Health',liveOps.providers.filter(p=>String(p.provider||'').toLowerCase()!=='omniroute').length?`<ul class="list">${liveOps.providers.filter(p=>String(p.provider||'').toLowerCase()!=='omniroute').slice(0,8).map(p=>`<li class="list-row inspectable" data-inspect-kind="provider" data-inspect-id="${escapeHtml(p.id)}"><span>☁</span><div class="list-main"><div>${escapeHtml(p.display_name||p.provider||'Cloud provider')}</div></div><span class="pill ${p.status==='connected'?'good':p.status==='unavailable'?'bad':''}">${escapeHtml(p.status||'configured')}</span></li>`).join('')}</ul>`:'<div class="empty-state compact">No cloud providers configured.</div>','View all');}

function bindViewActions(root=document){
  $$('[data-route]',root).forEach(b=>b.onclick=()=>openRoute(QA4_ROUTE_ALIASES[b.dataset.route]||b.dataset.route));
  $$('[data-drawer-tab]',root).forEach(b=>b.onclick=()=>{activeDrawerTab=b.dataset.drawerTab;setDrawerOpen(true);if(isPhoneLayout())setInspectorOpen(false);renderDrawer();});
  $$('[data-inspect-kind]',root).forEach(el=>el.onclick=e=>{if(e.target.closest('button,input,select,textarea,a'))return;const kind=el.dataset.inspectKind,id=el.dataset.inspectId,title=el.dataset.inspectTitle||'';let data={};if(el.dataset.inspectJson){try{data=JSON.parse(el.dataset.inspectJson)}catch{}}qa4Inspect(kind,id,title||qa4FindEntity(kind,id)?.display_name||qa4FindEntity(kind,id)?.objective||titleCase(kind),data);});
  $$('[data-attention-item]',root).forEach(b=>b.onclick=()=>qa4Inspect('attention',b.dataset.attentionItem,ATTENTION_ITEMS[Number(b.dataset.attentionItem)]?.title||'Attention',ATTENTION_ITEMS[Number(b.dataset.attentionItem)]||{}));
  $$('[data-approval-default]',root).forEach(b=>b.onclick=()=>{state.approvalLevel=b.dataset.approvalDefault;persist();renderSettings();bindViewActions($('#viewHost'));});
}

async function qa4InstallLocalModel(model){
  try{if(!localProfileQA)localProfileQA=await apiRequest('/v1/local-ai/detect',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace})});const quants=Array.isArray(model.quantizations)?model.quantizations:[];const q=quants[0]||model.quantization||'Q4_K_M';const job=await apiRequest('/v1/local-ai/install-jobs',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,profile_id:localProfileQA.id,role_name:'user-selected',use_case:'general',context_tokens:Math.min(Number(model.max_context_tokens||model.context_tokens||8192),32768),model_ref:model.model_ref||model.id,quantization:q,prefer_gpu:true})});notice(`Model install queued: ${job.id||model.display_name||model.model_ref}`);}catch(ex){notice(ex.message,'bad');}}
async function qa4RevokeProvider(id){try{await apiRequest(`/v1/providers/${encodeURIComponent(id)}/revoke`,{method:'POST',body:'{}'});notice('Cloud provider access revoked.');qa4ProjectHub.projects=[];renderModels();}catch(ex){notice(ex.message,'bad');}}
async function qa4ConnectCloudProvider(presetID){await openProviderDialog();const sel=$('#providerPreset');if(sel&&[...sel.options].some(o=>o.value===presetID)){sel.value=presetID;sel.dispatchEvent(new Event('change'));}}
async function renderModels(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Models','Local model management and direct cloud-provider connectivity. OmniRoute remains a distinct optional router.','<button class="btn" id="modelSettings">Model pool settings</button>')}<div id="qa4ModelsRoot" class="widget-body">Loading models and cloud providers…</div></section>`;$('#modelSettings').onclick=()=>openRoute('settings');
  const qs=encodeURIComponent(onepaneWorkspace);let catalog=[],presets=[],connections=[];try{[catalog,presets,connections]=await Promise.all([apiRequest('/v1/local-ai/catalog'),providerPresetsQA(),apiRequest(`/v1/providers?workspace_id=${qs}`)]);}catch(ex){$('#qa4ModelsRoot').innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return;}qa4ProjectHub.catalog=Array.isArray(catalog)?catalog:[];qa4ProjectHub.providerPresets=Array.isArray(presets)?presets:[];qa4ProjectHub.providers=Array.isArray(connections)?connections:[];
  const cloudPresets=qa4ProjectHub.providerPresets.filter(p=>p.id!=='omniroute');const omni=qa4ProjectHub.providers.find(p=>String(p.provider||'').toLowerCase()==='omniroute');
  $('#qa4ModelsRoot').outerHTML=`<div class="models-cloud-layout"><section class="panel-card"><div class="card-header"><div><div class="card-title">Local models</div><div class="list-meta">Download and qualify models into the configured OnePane model pool.</div></div><div class="toolbar"><button class="btn" id="detectLocal">Detect hardware</button><input id="qa4LocalFilter" class="catalogue-filter" placeholder="Filter local models…"></div></div><div id="localResult" class="model-result"></div><div id="qa4LocalModels" class="model-tile-scroll"></div></section><section class="panel-card"><div class="card-header"><div><div class="card-title">Cloud providers</div><div class="list-meta">Direct cloud connectivity and credentials. This is separate from OmniRoute.</div></div><button class="btn" id="qa4OpenSecrets">API keys</button></div><div id="qa4CloudProviders" class="provider-tile-grid"></div></section></div><section class="panel-card omniroute-separate"><div class="card-header"><div><div class="card-title">OmniRoute</div><div class="list-meta">Optional external routing gateway. Direct cloud providers above do not depend on OmniRoute.</div></div><span class="pill ${omni?'good':''}">${omni?'Connected':'Separate / optional'}</span></div><div class="widget-body omni-inline"><label>Gateway URL<input id="omniUrl" value="${escapeHtml(localStorage.getItem('onepane:omniroute-url')||omni?.connection?.base_url||'http://127.0.0.1:20128/v1')}"></label><label>Gateway credential<select id="omniCredential"><option value="">No gateway credential</option></select></label><label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label><div class="toolbar"><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" disabled>Connect</button></div><div id="omniResult" class="page-subtitle">${omni?'Connected provider record exists.':'Probe before connecting.'}</div></div></section>`;
  $('#detectLocal').onclick=detectLocalQA;$('#qa4OpenSecrets').onclick=()=>openRoute('secrets');$('#omniProbe').onclick=()=>omniQA(false);$('#omniConnect').onclick=()=>omniQA(true);populateOmniCredentials();
  const drawLocal=()=>{const q=($('#qa4LocalFilter').value||'').toLowerCase();const rows=qa4ProjectHub.catalog.filter(m=>JSON.stringify(m).toLowerCase().includes(q));$('#qa4LocalModels').innerHTML=rows.length?rows.map((m,i)=>`<article class="model-tile"><div><strong>${escapeHtml(m.display_name||m.name||m.model_ref||'Model')}</strong><div class="list-meta">${escapeHtml(String(m.parameter_count||m.parameter_scale||'—'))} · ${formatContextQA(m.max_context_tokens||m.context_tokens)} context</div><div class="list-meta">${escapeHtml(Array.isArray(m.quantizations)?m.quantizations.join(', '):(m.quantization||'llama.cpp'))}</div></div><button class="btn primary" data-download-model="${i}">Download</button></article>`).join(''):'<div class="empty-state compact">No local models match this filter.</div>';$$('[data-download-model]').forEach(b=>b.onclick=()=>qa4InstallLocalModel(rows[Number(b.dataset.downloadModel)]));};$('#qa4LocalFilter').oninput=drawLocal;drawLocal();
  $('#qa4CloudProviders').innerHTML=cloudPresets.map(p=>{const match=qa4ProjectHub.providers.find(c=>String(c.provider||'')===String(p.id)||String(c.display_name||'').toLowerCase()===String(p.display_name||'').toLowerCase());const connected=match&&String(match.status||'').toLowerCase()!=='revoked';const oauth=p.auth_type==='oauth2-pkce';return `<article class="provider-tile"><div class="provider-tile-head"><strong>${escapeHtml(p.display_name)}</strong><span class="pill ${connected?'good':''}">${connected?(oauth?'OAuth connected':'Connected'):(match?.status==='revoked'?'Revoked':'Available')}</span></div><p>${escapeHtml(p.description||'')}</p><div class="provider-meta"><span>${oauth?'OAuth':escapeHtml(p.auth_type||'API key')}</span><span>${escapeHtml(p.cost_hint||'')}</span></div><div class="toolbar">${connected?`<button class="btn danger" data-revoke-cloud="${escapeHtml(match.id)}">Revoke</button>`:oauth?`<button class="btn" data-oauth-info="${escapeHtml(p.id)}">Connect OAuth</button>`:`<button class="btn primary" data-connect-cloud="${escapeHtml(p.id)}">Connect</button>`}</div></article>`;}).join('');
  $$('[data-revoke-cloud]').forEach(b=>b.onclick=()=>qa4RevokeProvider(b.dataset.revokeCloud));$$('[data-connect-cloud]').forEach(b=>b.onclick=()=>qa4ConnectCloudProvider(b.dataset.connectCloud));$$('[data-oauth-info]').forEach(b=>b.onclick=()=>openModal('OAuth connection',`<div class="widget-body"><strong>${escapeHtml(cloudPresets.find(p=>String(p.id)===b.dataset.oauthInfo)?.display_name||'OAuth provider')}</strong><p>OnePane recognises OAuth-backed provider connections and exposes their status/revoke controls here. The dedicated browser OAuth start/callback broker is not yet exposed by this alpha API, so OnePane will not fake an OAuth sign-in.</p><p class="page-subtitle">API-key/token providers can be connected now. OAuth connections already registered by a supported broker will appear as “OAuth connected”.</p></div>`));bindViewActions($('#viewHost'));
}

async function renderIntegrations(){
  let providers=[],agents=[],plugins=[];try{[providers,agents,plugins]=await Promise.all([providerPresetsQA(),apiRequest('/v1/agent-runtime-presets').catch(()=>[]),apiRequest('/v1/plugin-presets').catch(()=>[])]);}catch{}
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Integrations','External AI runtimes, SaaS connectors and cloud-provider entry points')}<div class="integration-sections"><section class="panel-card"><div class="card-header"><div><div class="card-title">Cloud models & providers</div><div class="list-meta">Managed on Models; OmniRoute remains separate.</div></div><button class="btn" data-route="models">Open Models</button></div><div class="widget-body">${providers.filter(p=>p.id!=='omniroute').length} cloud provider presets available.</div></section><section class="panel-card"><div class="card-header"><div class="card-title">Agent runtimes</div></div><div class="integration-grid">${(Array.isArray(agents)?agents:[]).map(a=>`<div class="integration-card"><strong>${escapeHtml(a.display_name||a.name||a.id||'Agent runtime')}</strong><span>${escapeHtml(a.auth_type||a.transport||'runtime')}</span></div>`).join('')||'<div class="empty-state compact">No agent runtime presets.</div>'}</div></section><section class="panel-card"><div class="card-header"><div class="card-title">SaaS / tools</div></div><div class="integration-grid">${(Array.isArray(plugins)?plugins:[]).map(p=>`<div class="integration-card"><strong>${escapeHtml(p.display_name||p.name||p.id)}</strong><span>${escapeHtml(p.category||p.auth_type||'integration')}</span></div>`).join('')||'<div class="empty-state compact">No plugin presets.</div>'}</div></section></div></section>`;bindViewActions($('#viewHost'));
}

function attentionFromLiveData(){const out=[];for(const t of liveOps.tasks){const st=String(t.state||'').toLowerCase();if(st==='waiting_approval')out.push({id:`task-${t.id}-approval`,title:'Approval required',detail:t.objective||t.id,route:'tasks',severity:'warn'});else if(st==='blocked'||st==='failed')out.push({id:`task-${t.id}-${st}`,title:`Task ${st}`,detail:t.objective||t.id,route:'tasks',severity:'bad'});}for(const p of liveOps.providers){const st=String(p.status||'').toLowerCase();if(['degraded','rate_limited','expired','reauth_required','unavailable'].includes(st))out.push({id:`provider-${p.id}-${st}`,title:`Cloud provider ${st.replaceAll('_',' ')}`,detail:p.display_name||p.provider||p.id,route:'models',severity:st==='degraded'||st==='rate_limited'?'warn':'bad'});}for(const n of liveOps.nodes){const st=String(n.status||n.state||'').toLowerCase();if(['offline','failed','unavailable','stale'].includes(st))out.push({id:`node-${n.id||n.node_id}-${st}`,title:'Node unavailable',detail:n.display_name||n.node_id||n.id,route:'nodes',severity:'bad'});}const failure=/failed|error|blocked|approval_required|degraded|unavailable|rate_limited|recovery_required/i;for(const e of [...liveOps.events].reverse()){const type=String(e.event_type||'');if(!failure.test(type))continue;out.push({id:`event-${e.sequence||e.id}`,title:eventLabel(e),detail:`${e.aggregate_type||''} ${e.aggregate_id||''}`.trim(),route:type.includes('provider')?'models':type.includes('node')?'nodes':type.includes('task')?'tasks':'operations',severity:/failed|error|blocked|unavailable|recovery/i.test(type)?'bad':'warn'});if(out.length>=20)break;}const seen=new Set();return out.filter(x=>x.id&&!seen.has(x.id)&&(seen.add(x.id),true)).slice(0,20);}

let qa31ViewEpoch=0;
async function renderActiveView(){
  const epoch=++qa31ViewEpoch;
  const t=currentTab();if(!t)return;
  if(QA4_ROUTE_ALIASES[t.route]){t.route=QA4_ROUTE_ALIASES[t.route];t.title=pages[t.route]?.title||t.route;persist();}
  if(t.state==='suspended')t.state='active';
  const route=t.route;
  const renderers={operations:renderOperations,tasks:renderTasks,projects:renderProjects,models:renderModels,nodes:renderNodes,agents:renderAgents,routines:renderTasks,integrations:renderIntegrations,secrets:renderSecrets,evidence:()=>renderPlaceholder('Evidence / Audit','Event Ledger, Artifacts, Observations, Verifications, Operations and CapabilityLease activity.'),settings:renderSettings};
  try{await Promise.resolve((renderers[route]||renderOperations)());}
  finally{
    const host=$('#viewHost');
    if(epoch===qa31ViewEpoch){if(host)host.dataset.renderedRoute=route;}
    else if(currentTab()?.route!==route){
      const active=currentTab()?.route;
      if(active&&host?.dataset?.renderedRoute!==active)queueMicrotask(()=>{if(currentTab()?.route===active)renderActiveView();});
    }
  }
}

/* === QA5 / alpha.2 final product pass: global settings, packs, model Testbed and runtime strategies === */
const QA5_PREFS_KEY='onepane:global-preferences:v1';
const QA5_THEME_PACK_KEY='onepane:theme-packs:v1';
const QA5_LANGUAGE_PACK_KEY='onepane:language-packs:v1';
const QA5_SETUP_LANGUAGE_KEY='onepane:setup-language:v1';
const QA5_CORE_SKILLS=[
  {id:'workspace-files',name:'Workspace files',detail:'Read/write files only inside the Project workspace or explicitly authorised mounts.'},
  {id:'web-research',name:'Web research',detail:'HTTP/web research through ToolGateway when Project network policy permits it.'},
  {id:'code-data',name:'Code & data',detail:'Sandboxed code execution and data transforms under Project CapabilityLeases.'},
  {id:'browser-computer',name:'Browser & computer',detail:'Browser/computer actions only when the Project sandbox explicitly allows them.'},
  {id:'evidence-verification',name:'Evidence & verification',detail:'Observations, independent verification and checkpoint capture for governed Tasks.'}
];
const QA5_BUILTIN_LANGUAGES={
  'en-AU':{name:'English (Australia)',strings:{operations:'Operations',projects:'Projects',tasks:'Tasks',models:'Models',nodes:'Nodes',agents:'Agents',integrations:'Integrations',secrets:'Secrets',evidence:'Evidence / Audit',settings:'Settings',assistant:'OnePane Assistant',ask_onepane:'Ask OnePane or run a command…',global_context:'Global',project_orchestrator:'Project Orchestrator',ask_project:'Ask Project…',profiles:'Profiles',sessions:'Sessions',teams:'Teams',councils:'Councils',direct:'Direct',team:'Team',council:'Council',search:'Ask OnePane or run a command…'}},
  'en-US':{name:'English (US)',strings:{operations:'Operations',projects:'Projects',tasks:'Tasks',models:'Models',nodes:'Nodes',agents:'Agents',integrations:'Integrations',secrets:'Secrets',evidence:'Evidence / Audit',settings:'Settings',assistant:'OnePane Assistant',ask_onepane:'Ask OnePane or run a command…',global_context:'Global',project_orchestrator:'Project Orchestrator',ask_project:'Ask Project…',profiles:'Profiles',sessions:'Sessions',teams:'Teams',councils:'Councils',direct:'Direct',team:'Team',council:'Council',search:'Ask OnePane or run a command…'}},
  'it-IT':{name:'Italiano',strings:{operations:'Operazioni',projects:'Progetti',tasks:'Attività',models:'Modelli',nodes:'Nodi',agents:'Agenti',integrations:'Integrazioni',secrets:'Segreti',evidence:'Prove / Audit',settings:'Impostazioni',assistant:'Assistente OnePane',ask_onepane:'Chiedi a OnePane o esegui un comando…',global_context:'Globale',project_orchestrator:'Orchestratore progetto',ask_project:'Chiedi al progetto…',profiles:'Profili',sessions:'Sessioni',teams:'Team',councils:'Consigli',direct:'Diretto',team:'Team',council:'Consiglio',search:'Chiedi a OnePane o esegui un comando…'}},
  'ja-JP':{name:'日本語',strings:{operations:'運用',projects:'プロジェクト',tasks:'タスク',models:'モデル',nodes:'ノード',agents:'エージェント',integrations:'連携',secrets:'シークレット',evidence:'証拠 / 監査',settings:'設定',assistant:'OnePane アシスタント',ask_onepane:'OnePane に質問またはコマンドを実行…',global_context:'グローバル',project_orchestrator:'プロジェクト・オーケストレーター',ask_project:'プロジェクトに質問…',profiles:'プロファイル',sessions:'セッション',teams:'チーム',councils:'カウンシル',direct:'ダイレクト',team:'チーム',council:'カウンシル',search:'OnePane に質問またはコマンドを実行…'}},
  'zh-CN':{name:'简体中文',strings:{operations:'运行',projects:'项目',tasks:'任务',models:'模型',nodes:'节点',agents:'智能体',integrations:'集成',secrets:'密钥',evidence:'证据 / 审计',settings:'设置',assistant:'OnePane 助手',ask_onepane:'询问 OnePane 或运行命令…',global_context:'全局',project_orchestrator:'项目编排器',ask_project:'询问项目…',profiles:'配置档',sessions:'会话',teams:'团队',councils:'评议组',direct:'直接',team:'团队',council:'评议组',search:'询问 OnePane 或运行命令…'}},
  'zh-TW':{name:'繁體中文',strings:{operations:'運行',projects:'專案',tasks:'任務',models:'模型',nodes:'節點',agents:'代理',integrations:'整合',secrets:'密鑰',evidence:'證據 / 稽核',settings:'設定',assistant:'OnePane 助手',ask_onepane:'詢問 OnePane 或執行命令…',global_context:'全域',project_orchestrator:'專案協調器',ask_project:'詢問專案…',profiles:'設定檔',sessions:'工作階段',teams:'團隊',councils:'評議組',direct:'直接',team:'團隊',council:'評議組',search:'詢問 OnePane 或執行命令…'}}
};
const QA5_AUTH_COPY={
  'en-AU':{product_tagline:'Local-first autonomous AI control plane',choose_language_title:'Choose your language',choose_language_body:'Select the language to use in OnePane. You can change it later in Settings.',language:'Language',continue:'Continue',change_language:'Change language',setup_title:'Set up your control plane',setup_body:'Create the first local administrator. Credentials remain on this OnePane installation.',username:'Username',display_name:'Display name',workspace_name:'Workspace name',password:'Password',create_admin:'Create administrator',sign_in:'Sign in'},
  'en-US':{product_tagline:'Local-first autonomous AI control plane',choose_language_title:'Choose your language',choose_language_body:'Select the language to use in OnePane. You can change it later in Settings.',language:'Language',continue:'Continue',change_language:'Change language',setup_title:'Set up your control plane',setup_body:'Create the first local administrator. Credentials remain on this OnePane installation.',username:'Username',display_name:'Display name',workspace_name:'Workspace name',password:'Password',create_admin:'Create administrator',sign_in:'Sign in'},
  'it-IT':{product_tagline:'Piano di controllo AI autonomo, local-first',choose_language_title:'Scegli la lingua',choose_language_body:'Seleziona la lingua da usare in OnePane. Potrai cambiarla in seguito nelle Impostazioni.',language:'Lingua',continue:'Continua',change_language:'Cambia lingua',setup_title:'Configura il tuo piano di controllo',setup_body:'Crea il primo amministratore locale. Le credenziali restano su questa installazione di OnePane.',username:'Nome utente',display_name:'Nome visualizzato',workspace_name:'Nome area di lavoro',password:'Password',create_admin:'Crea amministratore',sign_in:'Accedi'},
  'ja-JP':{product_tagline:'ローカルファーストの自律AIコントロールプレーン',choose_language_title:'言語を選択',choose_language_body:'OnePaneで使用する言語を選択してください。後で設定から変更できます。',language:'言語',continue:'続行',change_language:'言語を変更',setup_title:'コントロールプレーンを設定',setup_body:'最初のローカル管理者を作成します。認証情報はこのOnePaneインストール内に保持されます。',username:'ユーザー名',display_name:'表示名',workspace_name:'ワークスペース名',password:'パスワード',create_admin:'管理者を作成',sign_in:'サインイン'},
  'zh-CN':{product_tagline:'本地优先的自主 AI 控制平面',choose_language_title:'选择语言',choose_language_body:'选择 OnePane 使用的语言。之后可在“设置”中更改。',language:'语言',continue:'继续',change_language:'更改语言',setup_title:'设置控制平面',setup_body:'创建第一个本地管理员。凭据仅保存在此 OnePane 安装中。',username:'用户名',display_name:'显示名称',workspace_name:'工作区名称',password:'密码',create_admin:'创建管理员',sign_in:'登录'},
  'zh-TW':{product_tagline:'本機優先的自主 AI 控制平面',choose_language_title:'選擇語言',choose_language_body:'選擇 OnePane 使用的語言。之後可在「設定」中變更。',language:'語言',continue:'繼續',change_language:'變更語言',setup_title:'設定控制平面',setup_body:'建立第一個本機管理員。認證資訊只會保留在此 OnePane 安裝中。',username:'使用者名稱',display_name:'顯示名稱',workspace_name:'工作區名稱',password:'密碼',create_admin:'建立管理員',sign_in:'登入'}
};
Object.assign(THEME_PALETTES,{
  aurora:{mode:'dark',caption:'#0a1520',text:'#eefcff',border:'#24506a'},
  cobalt:{mode:'dark',caption:'#08162b',text:'#eff5ff',border:'#254a78'},
  dusk:{mode:'dark',caption:'#181326',text:'#f5efff',border:'#513b69'}
});
function qa5JSONStorage(key,fallback={}){try{const x=JSON.parse(localStorage.getItem(key)||'');return x&&typeof x==='object'?x:fallback}catch{return fallback}}
function qa5SaveStorage(key,value){localStorage.setItem(key,JSON.stringify(value));}
function qa5Prefs(){
  const stored=qa5JSONStorage(QA5_PREFS_KEY,{}),legacy=stored.project_defaults||{},existing={...legacy,...(stored.workspace_defaults||{})};
  const mode=['direct','team','council'].includes(String(existing.orchestration||'').toLowerCase())?String(existing.orchestration).toLowerCase():'direct';
  const seats=Math.max(1,Math.min(8,Number(existing.seats||existing.workers||2)));
  return {language:'en-AU',landing:'operations',density:'comfortable',update_channel:'alpha',suspend_minutes:5,pause_background:true,default_runtime:'auto',
    assistant_defaults:{compute_preference:'auto',allow_subscription:false,allow_paid:false},...stored,
    assistant_defaults:{compute_preference:'auto',allow_subscription:false,allow_paid:false,...(stored.assistant_defaults||{})},
    workspace_defaults:{orchestration:mode,model_routing:true,remote_models:true,external_network:false,browser:false,computer:false,seats,...existing}};
}
function qa5SavePrefs(patch){const current=qa5Prefs();const next={...current,...patch,workspace_defaults:{...current.workspace_defaults,...(patch.workspace_defaults||{})}};delete next.project_defaults;qa5SaveStorage(QA5_PREFS_KEY,next);return next;}
function qa5ThemePacks(){return qa5JSONStorage(QA5_THEME_PACK_KEY,{});}
function qa5LanguagePacks(){return qa5JSONStorage(QA5_LANGUAGE_PACK_KEY,{});}
function qa5AllLanguages(){return {...QA5_BUILTIN_LANGUAGES,...qa5LanguagePacks()};}
function qa5CurrentLanguage(){const p=qa5Prefs();return qa5AllLanguages()[p.language]||QA5_BUILTIN_LANGUAGES['en-AU'];}
function qa5T(key,fallback=''){return qa5CurrentLanguage()?.strings?.[key]||fallback||key;}
function qa5DetectedLanguage(){
  const all=qa5AllLanguages(),ids=Object.keys(all),requested=[...(navigator.languages||[]),navigator.language].filter(Boolean);
  for(const raw of requested){const exact=ids.find(id=>id.toLowerCase()===String(raw).toLowerCase());if(exact)return exact;const base=String(raw).split('-')[0].toLowerCase(),family=ids.find(id=>id.split('-')[0].toLowerCase()===base);if(family)return family}
  return 'en-AU';
}
function qa5SelectedLanguage(){const all=qa5AllLanguages(),stored=qa5JSONStorage(QA5_PREFS_KEY,{});return stored.language&&all[stored.language]?stored.language:qa5DetectedLanguage()}
function qa5ApplyAuthLanguage(id){
  const all=qa5AllLanguages(),lang=all[id]?id:'en-AU',copy=QA5_AUTH_COPY[lang]||QA5_AUTH_COPY['en-AU'],pack=all[lang]||{};
  document.documentElement.lang=lang;document.documentElement.dir=pack.direction==='rtl'?'rtl':'ltr';
  $$('[data-auth-copy]').forEach(el=>{const key=el.dataset.authCopy;if(copy[key])el.textContent=copy[key]});
  const select=$('#setup-language');if(select&&select.value!==lang)select.value=lang;
}
function qa5PrepareFirstRunLanguage(){
  const select=$('#setup-language'),languageStep=$('#setup-language-step'),adminStep=$('#setup-admin-step');if(!select||!languageStep||!adminStep)return;
  const all=qa5AllLanguages(),initial=qa5SelectedLanguage();
  select.innerHTML=Object.entries(all).map(([id,x])=>'<option value="'+escapeHtml(id)+'">'+escapeHtml(x.name||id)+'</option>').join('');select.value=initial;qa5ApplyAuthLanguage(initial);
  const showLanguage=()=>{languageStep.classList.remove('hidden');adminStep.classList.add('hidden');select.focus?.()};
  const showAdmin=()=>{languageStep.classList.add('hidden');adminStep.classList.remove('hidden');adminStep.querySelector('input')?.focus?.()};
  const confirmed=localStorage.getItem(QA5_SETUP_LANGUAGE_KEY);if(confirmed&&all[confirmed]){select.value=confirmed;qa5ApplyAuthLanguage(confirmed);showAdmin()}else showLanguage();
  select.onchange=()=>qa5ApplyAuthLanguage(select.value);
  $('#setup-language-continue').onclick=()=>{const language=select.value;qa5SavePrefs({language});localStorage.setItem(QA5_SETUP_LANGUAGE_KEY,language);qa5ApplyAuthLanguage(language);qa5RefreshShellLanguage();showAdmin()};
  $('#setup-language-back').onclick=showLanguage;
}
function qa5ThemeName(id){return qa5ThemePacks()[id]?.name||titleCase(id);}
function qa5ClearCustomTheme(){for(const k of ['--bg','--panel','--panel-2','--panel-3','--text','--muted','--border','--accent','--on-accent','--accent-soft','--good','--warn','--bad','--app-gradient','--scrollbar-size','--scrollbar-track','--scrollbar-thumb','--scrollbar-thumb-hover','--scrollbar-thumb-active'])document.documentElement.style.removeProperty(k);}
function qa5ApplyCustomTheme(id){qa5ClearCustomTheme();const pack=qa5ThemePacks()[id];if(!pack)return false;const allowed=new Set(['--bg','--panel','--panel-2','--panel-3','--text','--muted','--border','--accent','--on-accent','--accent-soft','--good','--warn','--bad','--scrollbar-size','--scrollbar-track','--scrollbar-thumb','--scrollbar-thumb-hover','--scrollbar-thumb-active']);for(const [k,v] of Object.entries(pack.vars||{})){if(allowed.has(k)&&typeof v==='string'&&v.length<128)document.documentElement.style.setProperty(k,v);}if(typeof pack.gradient==='string'&&pack.gradient.length<256)document.documentElement.style.setProperty('--app-gradient',pack.gradient);return true;}
function effectiveThemePalette(name=state.theme){const custom=qa5ThemePacks()[name];if(custom)return {mode:custom.mode==='light'?'light':'dark',caption:custom.caption||custom.vars?.['--panel']||'#0d1721',text:custom.caption_text||custom.vars?.['--text']||'#e7edf4',border:custom.border||custom.vars?.['--border']||'#203142'};if(name==='system'){if(matchMedia('(prefers-color-scheme: light)').matches)return {mode:'light',caption:'#e8edf2',text:'#263746',border:'#c5d0da'};return THEME_PALETTES.system;}return THEME_PALETTES[name]||THEME_PALETTES.dark;}
function applyTheme(name){state.theme=name||'system';qa5ClearCustomTheme();if(qa5ThemePacks()[state.theme])qa5ApplyCustomTheme(state.theme);document.documentElement.dataset.theme=qa5ThemePacks()[state.theme]?'custom':state.theme;try{persist()}catch{}sendNativeTheme();}
function qa5ThemeButtons(){const ids=[...Object.keys(THEME_PALETTES),...Object.keys(qa5ThemePacks())];return ids.filter((x,i,a)=>a.indexOf(x)===i).map(t=>`<button class="theme-choice ${state.theme===t?'active':''}" data-settings-theme="${escapeHtml(t)}">${escapeHtml(qa5ThemeName(t))}</button>`).join('');}
async function qa5InstallThemePack(file){const raw=await file.text();const p=JSON.parse(raw);if(!p||typeof p.id!=='string'||!/^[a-z0-9][a-z0-9_-]{1,31}$/i.test(p.id)||typeof p.name!=='string'||!p.vars)throw new Error('Theme pack must include id, name and vars.');const packs=qa5ThemePacks();packs[p.id]={name:p.name,mode:p.mode==='light'?'light':'dark',vars:p.vars,gradient:p.gradient||'',caption:p.caption||'',caption_text:p.caption_text||'',border:p.border||''};qa5SaveStorage(QA5_THEME_PACK_KEY,packs);applyTheme(p.id);}
async function qa5InstallLanguagePack(file){const p=JSON.parse(await file.text());if(!p||typeof p.id!=='string'||!/^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$/.test(p.id)||typeof p.name!=='string'||!p.strings||typeof p.strings!=='object')throw new Error('Language pack must include id, name and strings.');const packs=qa5LanguagePacks();packs[p.id]={name:p.name,englishName:p.englishName||'',direction:p.direction==='rtl'?'rtl':'ltr',strings:p.strings};qa5SaveStorage(QA5_LANGUAGE_PACK_KEY,packs);qa5SavePrefs({language:p.id});qa5RefreshShellLanguage();}
function qa5RefreshShellLanguage(){const map={operations:'operations',projects:'projects',tasks:'tasks',models:'models',nodes:'nodes',agents:'agents',integrations:'integrations',secrets:'secrets',evidence:'evidence',settings:'settings'};for(const [route,key] of Object.entries(map)){if(pages[route])pages[route].title=qa5T(key,pages[route].title);}renderNav();renderTabs();renderMobileNav();const q=$('#commandButton');if(q){const txt=q.querySelector('span')||q;q.setAttribute('aria-label',qa5T('search','Search or run a command…'));}const search=$('#commandButton');if(search&&search.childNodes.length)search.childNodes[0].textContent=qa5T('search','Search or run a command…')+' ';}
function renderNav(){const activeRoute=currentTab()?.route;$('#primaryNav').innerHTML=navItems.map(([route,icon,label])=>{const key=route==='agents'?'agents':route;const translated=qa5T(key,label);return `<button class="nav-item ${route===activeRoute?'active':''}" data-route="${route}" title="${escapeHtml(translated)}"><span class="nav-icon">${icon}</span><span class="nav-label">${escapeHtml(translated)}</span></button>`;}).join('');renderMobileNav();$$('[data-route]',$('#primaryNav')).forEach(b=>b.onclick=()=>openRoute(b.dataset.route));}
function renderMobileNav(){const activeRoute=currentTab()?.route;$$('[data-mobile-route]',$('#mobileNav')).forEach(b=>b.classList.toggle('active',b.dataset.mobileRoute===activeRoute));const key=activeRoute==='agents'?'agents':activeRoute;$('#mobileTitle').textContent=qa5T(key,pages[activeRoute]?.title||'OnePane');}
function openThemePopover(anchor){popoverFor(anchor,`<div class="popover"><h3>Theme</h3><div class="theme-grid">${qa5ThemeButtons()}</div><button class="btn" id="qa5ThemeSettings" style="margin-top:10px;width:100%">Theme packs…</button></div>`);$$('[data-settings-theme]').forEach(b=>b.onclick=()=>{applyTheme(b.dataset.settingsTheme);$('#overlayRoot').innerHTML='';});$('#qa5ThemeSettings')?.addEventListener('click',()=>{closeModal?.();$('#overlayRoot').innerHTML='';openRoute('settings');});}

function qa4DefaultSandbox(){const d=qa5Prefs().workspace_defaults||{};const external=!!d.external_network;return {network:external,internet:external,lan:external,computer:!!d.computer,browser:!!d.browser,host_files:'workspace-only',secrets:'selected'};}
function qa4DefaultWorkspace(project,name='Main workspace'){
  const d=qa5Prefs().workspace_defaults||{},mode=['direct','team','council'].includes(d.orchestration)?d.orchestration:'direct',seats=Math.max(1,Math.min(8,Number(d.seats||2)));
  return {id:`pws-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,7)}`,name,
    widgets:[{id:`pw-chat-${Date.now()}`,type:'chat',title:'Workspace chat',col:6,row:5},{id:`pw-tasks-${Date.now()+1}`,type:'tasks',title:'Tasks',col:6,row:4},{id:`pw-notes-${Date.now()+2}`,type:'notes',title:'Notes',col:4,row:4}],
    orchestration:{mode,supervisor:{model:'auto',agent:'agent.md'},team:{model:'auto',agent:'agent.md',count:seats},council:{model:'auto',agent:'agent.md',count:seats}}};
}



let qa5ManagedDeployments=[];
async function qa5LoadManagedDeployments(){try{const out=await apiRequest(`/v1/local-ai/deployments?workspace_id=${encodeURIComponent(onepaneWorkspace)}`);qa5ManagedDeployments=Array.isArray(out)?out:(out.deployments||[]);}catch{qa5ManagedDeployments=[];}return qa5ManagedDeployments;}
async function qa5InspectModel(dep){let spec={};try{spec=await apiRequest(`/v1/model-deployments/${encodeURIComponent(dep.deployment_id)}/spec-sheet`);}catch(ex){spec={error:ex.message};}qa4Inspect('model',dep.deployment_id,dep.display_name||dep.model_ref||'Local model',{...dep,spec});}
async function qa5AgentCheck(dep){notice(`Starting Agent Check for ${dep.display_name||dep.model_ref}…`);try{const sess=await apiRequest(`/v1/model-deployments/${encodeURIComponent(dep.deployment_id)}/testbed/sessions`,{method:'POST',body:JSON.stringify({notes:'OnePane alpha.3 manual Agent Check'})});const sid=sess.id;const probes=[{prompt:'Reply with exactly: ONEPANE_OK',max_tokens:32},{prompt:'Return a compact JSON object with keys status and number, where status is ok and number is 7.',max_tokens:96},{prompt:'Call the synthetic onepane_test_probe tool with value agent-check if tools are supported; otherwise state that tool calling is unavailable.',synthetic_tool_probe:true,max_tokens:128}];for(const probe of probes)await apiRequest(`/v1/model-testbed/${encodeURIComponent(sid)}/turns`,{method:'POST',body:JSON.stringify(probe)});await apiRequest(`/v1/model-testbed/${encodeURIComponent(sid)}/complete`,{method:'POST',body:'{}'});notice('Agent Check completed; spec sheet updated.');await qa5LoadManagedDeployments();const fresh=qa5ManagedDeployments.find(x=>x.deployment_id===dep.deployment_id)||dep;await qa5InspectModel(fresh);if(currentTab()?.route==='models')renderModels();}catch(ex){notice(`Agent Check failed: ${ex.message}`,'bad');}}
async function qa5RegisterColibri(){let path='';try{const out=await apiRequest('/desktop/folder-picker',{method:'POST',body:JSON.stringify({title:'Select a Colibri model folder inside the OnePane model pool'})});path=out.path||'';}catch{}if(!path){path=prompt('Enter the absolute Colibri model folder path inside your OnePane model pool:','')||'';}if(!path)return;const ref=(prompt('Model reference/name:',path.split(/[\\/]/).filter(Boolean).pop()||'colibri-model')||'').trim();if(!ref)return;try{if(!localProfileQA)localProfileQA=await apiRequest('/v1/local-ai/detect',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace})});const dep=await apiRequest('/v1/local-ai/colibri/register',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,model_path:path,model_ref:ref,display_name:ref,context_tokens:8192})});notice('Colibri model registered in qualifying state. Run Agent Check before admission.');await qa5LoadManagedDeployments();await qa5InspectModel(qa5ManagedDeployments.find(x=>x.deployment_id===dep.id)||{deployment_id:dep.id,model_ref:ref,display_name:ref,runtime_name:'colibri'});renderModels();}catch(ex){notice(ex.message,'bad');}}
async function qa5ComponentStatus(){
  const out=await apiRequest('/v1/local-ai/components?workspace_id='+encodeURIComponent(onepaneWorkspace));
  if(Array.isArray(out))return Object.fromEntries(out.filter(Boolean).map(x=>[x.id,x]));
  return out&&typeof out==='object'?out:{};
}
async function qa5ModelComponents(){return qa5ComponentStatus();}
async function renderModels(){
  $('#viewHost').innerHTML=`<section class="page">${pageHeader('Models','Local model downloads, qualification and runtime strategy alongside direct cloud providers. OmniRoute remains a separate optional router.','<button class="btn" id="modelSettings">Local AI settings</button>')}<div id="qa5ModelsRoot" class="widget-body">Loading model inventory…</div></section>`;$('#modelSettings').onclick=()=>openRoute('settings');
  const qs=encodeURIComponent(onepaneWorkspace);let catalog=[],presets=[],connections=[],deployments=[],components={};try{[catalog,presets,connections,deployments,components]=await Promise.all([apiRequest('/v1/local-ai/catalog'),providerPresetsQA(),apiRequest(`/v1/providers?workspace_id=${qs}`),qa5LoadManagedDeployments(),qa5ModelComponents().catch(()=>({}))]);}catch(ex){$('#qa5ModelsRoot').innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return;}qa4ProjectHub.catalog=Array.isArray(catalog)?catalog:[];qa4ProjectHub.providerPresets=Array.isArray(presets)?presets:[];qa4ProjectHub.providers=Array.isArray(connections)?connections:[];
  const cloudPresets=qa4ProjectHub.providerPresets.filter(p=>p.id!=='omniroute'),omni=qa4ProjectHub.providers.find(p=>String(p.provider||'').toLowerCase()==='omniroute'),runtimePref=qa5Prefs().default_runtime||'auto';
  $('#qa5ModelsRoot').outerHTML=`<div class="models-cloud-layout qa5-models-layout"><section class="panel-card"><div class="card-header qa31-local-models-header"><div class="qa31-local-models-copy"><div class="card-title">Local models</div><div class="list-meta">Managed Hot Swap for ordinary models; Colibri Large Model for compatible sparse/MoE models spanning VRAM + RAM + NVMe.</div></div><div class="toolbar qa31-local-models-controls"><button class="btn qa31-detect-hardware" id="detectLocal">Detect hardware</button><select class="qa31-runtime-strategy" id="qa5RuntimeStrategy"><option value="auto" ${runtimePref==='auto'?'selected':''}>Auto</option><option value="hot-swap" ${runtimePref==='hot-swap'?'selected':''}>Managed Hot Swap</option><option value="colibri" ${runtimePref==='colibri'?'selected':''}>Colibri Large Model</option></select></div></div><div id="localResult" class="model-result"></div><div class="local-managed-section"><div class="subsection-title">Installed / registered</div><div id="qa5ManagedModels" class="model-tile-scroll"></div></div><div class="subsection-title">Available catalogue</div><input id="qa4LocalFilter" class="catalogue-filter" placeholder="Filter local models…"><div id="qa4LocalModels" class="model-tile-scroll"></div></section><section class="panel-card"><div class="card-header"><div><div class="card-title">Cloud providers</div><div class="list-meta">Direct cloud model/API connections. These are not OmniRoute.</div></div><button class="btn" id="qa4OpenSecrets">API keys</button></div><div id="qa4CloudProviders" class="provider-tile-grid"></div></section></div>
  <section class="panel-card colibri-separate"><div class="card-header"><div><div class="card-title">Colibri Large Model</div><div class="list-meta">Optional Apache-2.0 backend v1.12.1 for very large sparse/MoE models.</div></div><span class="pill ${components.colibri?.installed?'good':''}">${components.colibri?.installed?'Installed':'Optional'}</span></div><div class="widget-body"><p>OnePane remains scheduler/admission authority; Colibri manages model placement across VRAM, RAM and NVMe. Registered models are quarantined until Agent Check/Testbed qualification.</p><div class="toolbar"><button class="btn primary" id="qa5ColibriInstallModel">${components.colibri?.installed?'Installed':'Install runtime'}</button><button class="btn" id="qa5ColibriEnableModel">Enable</button><button class="btn" id="qa5ColibriDisableModel">Disable</button><button class="btn" id="qa5ColibriRegister">Register model folder</button><button class="btn danger" id="qa5ColibriRemoveModel">Remove runtime</button></div><div id="qa5ColibriInlineStatus" class="page-subtitle">${components.colibri?.installed?`Runtime installed${components.colibri?.python_ready===false?' · Python required':''}.`:'Install the runtime first, then place/download a compatible Colibri model inside the OnePane model pool.'}</div></div></section>
  <section class="panel-card omniroute-separate"><div class="card-header"><div><div class="card-title">OmniRoute</div><div class="list-meta">Optional routing gateway. Local models and direct cloud providers remain fully independent.</div></div><span class="pill ${components.omniroute?.installed||omni?'good':''}">${components.omniroute?.installed?'Installed':omni?'Connected':'Optional'}</span></div><div class="widget-body omni-inline"><div class="toolbar"><button class="btn" id="qa5OmniInstallModel">Install</button><button class="btn danger" id="qa5OmniRemoveModel">Remove</button></div><label>Gateway URL<input id="omniUrl" value="${escapeHtml(localStorage.getItem('onepane:omniroute-url')||omni?.connection?.base_url||'http://127.0.0.1:20128/v1')}"></label><label>Gateway credential<select id="omniCredential"><option value="">No gateway credential</option></select></label><label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label><div class="toolbar"><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" disabled>Connect</button></div><div id="omniResult" class="page-subtitle">${omni?'Connected provider record exists.':'Probe before connecting.'}</div></div></section>`;
  $('#detectLocal').onclick=detectLocalQA;$('#qa4OpenSecrets').onclick=()=>openRoute('secrets');$('#qa5RuntimeStrategy').onchange=e=>qa5SavePrefs({default_runtime:e.target.value});$('#qa5ColibriInstallModel').onclick=()=>qa5ComponentAction('colibri','install','#qa5ColibriInlineStatus');$('#qa5ColibriEnableModel').onclick=()=>qa5ComponentAction('colibri','enable','#qa5ColibriInlineStatus');$('#qa5ColibriDisableModel').onclick=()=>qa5ComponentAction('colibri','disable','#qa5ColibriInlineStatus');$('#qa5ColibriRemoveModel').onclick=()=>qa5ComponentAction('colibri','remove','#qa5ColibriInlineStatus');$('#qa5ColibriRegister').onclick=qa5RegisterColibri;$('#qa5OmniInstallModel').onclick=()=>qa5ComponentAction('omniroute','install','#omniResult');$('#qa5OmniRemoveModel').onclick=()=>qa5ComponentAction('omniroute','remove','#omniResult');$('#omniProbe').onclick=()=>omniQA(false);$('#omniConnect').onclick=()=>omniQA(true);populateOmniCredentials();
  const drawManaged=()=>{$('#qa5ManagedModels').innerHTML=deployments.length?deployments.map((d,i)=>`<article class="model-tile inspectable" data-model-inspect="${i}"><div><strong>${escapeHtml(d.display_name||d.model_ref||'Local model')}</strong><div class="list-meta">${escapeHtml(d.runtime_name||d.runtime_backend||'managed')} ${escapeHtml(d.runtime_version||'')} · ${escapeHtml(d.status||'unknown')} · admission ${escapeHtml(d.admission_status||'pending')}</div><div class="list-meta">${escapeHtml(d.quantization||'')} · ${d.context_max_verified?`${formatContextQA(d.context_max_verified)} verified context`:d.context_max_reported?`${formatContextQA(d.context_max_reported)} reported context`:'context pending qualification'}</div></div><div class="toolbar"><button class="btn" data-model-spec="${i}">Spec sheet</button><button class="btn primary" data-agent-check="${i}">Agent Check</button></div></article>`).join(''):'<div class="empty-state compact">No managed local models yet.</div>';$$('[data-model-spec]').forEach(b=>b.onclick=e=>{e.stopPropagation();qa5InspectModel(deployments[Number(b.dataset.modelSpec)]);});$$('[data-agent-check]').forEach(b=>b.onclick=e=>{e.stopPropagation();qa5AgentCheck(deployments[Number(b.dataset.agentCheck)]);});$$('[data-model-inspect]').forEach(el=>el.onclick=e=>{if(e.target.closest('button'))return;qa5InspectModel(deployments[Number(el.dataset.modelInspect)]);});};drawManaged();
  const drawLocal=()=>{const q=($('#qa4LocalFilter').value||'').toLowerCase(),rows=qa4ProjectHub.catalog.filter(m=>JSON.stringify(m).toLowerCase().includes(q));$('#qa4LocalModels').innerHTML=rows.length?rows.map((m,i)=>`<article class="model-tile"><div><strong>${escapeHtml(m.display_name||m.name||m.model_ref||'Model')}</strong><div class="list-meta">${escapeHtml(String(m.parameter_count||m.parameter_scale||'—'))} · ${formatContextQA(m.max_context_tokens||m.context_tokens)} context</div><div class="list-meta">${escapeHtml(Array.isArray(m.quantizations)?m.quantizations.join(', '):(m.quantization||'llama.cpp'))}</div></div><button class="btn primary" data-download-model="${i}">Download</button></article>`).join(''):'<div class="empty-state compact">No local models match this filter.</div>';$$('[data-download-model]').forEach(b=>b.onclick=()=>qa4InstallLocalModel(rows[Number(b.dataset.downloadModel)]));};$('#qa4LocalFilter').oninput=drawLocal;drawLocal();
  $('#qa4CloudProviders').innerHTML=cloudPresets.map(p=>{const match=qa4ProjectHub.providers.find(c=>String(c.provider||'')===String(p.id)||String(c.display_name||'').toLowerCase()===String(p.display_name||'').toLowerCase()),connected=match&&String(match.status||'').toLowerCase()!=='revoked',oauth=p.auth_type==='oauth2-pkce';return `<article class="provider-tile"><div class="provider-tile-head"><strong>${escapeHtml(p.display_name)}</strong><span class="pill ${connected?'good':''}">${connected?(oauth?'OAuth connected':'Connected'):(match?.status==='revoked'?'Revoked':'Available')}</span></div><p>${escapeHtml(p.description||'')}</p><div class="provider-meta"><span>${oauth?'OAuth':escapeHtml(p.auth_type||'API key')}</span><span>${escapeHtml(p.cost_hint||'')}</span></div><div class="toolbar">${connected?`<button class="btn danger" data-revoke-cloud="${escapeHtml(match.id)}">Revoke</button>`:oauth?`<button class="btn" data-oauth-info="${escapeHtml(p.id)}">Connect OAuth</button>`:`<button class="btn primary" data-connect-cloud="${escapeHtml(p.id)}">Connect</button>`}</div></article>`;}).join('');
  $$('[data-revoke-cloud]').forEach(b=>b.onclick=()=>qa4RevokeProvider(b.dataset.revokeCloud));$$('[data-connect-cloud]').forEach(b=>b.onclick=()=>qa4ConnectCloudProvider(b.dataset.connectCloud));$$('[data-oauth-info]').forEach(b=>b.onclick=()=>openModal('OAuth connection',`<div class="widget-body"><strong>${escapeHtml(cloudPresets.find(p=>String(p.id)===b.dataset.oauthInfo)?.display_name||'OAuth provider')}</strong><p>When an OAuth broker is available, OnePane records the connection here and exposes revoke/reconnect controls. This alpha will not fake an OAuth consent flow.</p></div>`));bindViewActions($('#viewHost'));
}
function qa4InspectorOverview(){const d=qa4FindEntity(qa4Inspector.kind,qa4Inspector.id);if(qa4Inspector.kind==='model'){const m=qa4Inspector.data||{},s=m.spec||{};return `<div class="model-spec-inspector"><dl class="definition-grid"><dt>Model</dt><dd>${escapeHtml(m.display_name||m.model_ref||'')}</dd><dt>Runtime</dt><dd>${escapeHtml(m.runtime_name||m.runtime_backend||'managed')} ${escapeHtml(m.runtime_version||'')}</dd><dt>Deployment</dt><dd class="mono-cell">${escapeHtml(m.deployment_id||qa4Inspector.id)}</dd><dt>Status</dt><dd>${escapeHtml(m.status||'')}</dd><dt>Admission</dt><dd>${escapeHtml(m.admission_status||s.admission_status||'pending')}</dd><dt>Context</dt><dd>${escapeHtml(String(m.context_max_verified||m.context_max_reported||'pending'))}</dd></dl><div class="subsection-title">Spec sheet</div>${s.error?`<div class="error">${escapeHtml(s.error)}</div>`:`<pre class="spec-sheet-json">${escapeHtml(JSON.stringify(s,null,2))}</pre>`}<button class="btn primary" id="qa5InspectorAgentCheck">Run Agent Check</button></div>`;}if(qa4Inspector.kind==='workspace'){const w=d.workspace||{},p=d.project||{},sb=qa4ProjectSandbox(p);return `<dl class="definition-grid"><dt>Project</dt><dd>${escapeHtml(p.name||p.id||'')}</dd><dt>Workspace</dt><dd>${escapeHtml(w.name||w.id||'')}</dd><dt>Project sandbox</dt><dd>${sb.internet?'External network allowed':'External network blocked'} · ${sb.computer?'Computer allowed':'Computer blocked'}</dd><dt>Mode</dt><dd>${escapeHtml(titleCase(w.orchestration?.mode||'supervisor'))}</dd></dl>`;}if(qa4Inspector.kind==='project'){return `<dl class="definition-grid"><dt>Name</dt><dd>${escapeHtml(d.name||'')}</dd><dt>Status</dt><dd>${escapeHtml(d.status||'')}</dd><dt>Revision</dt><dd>${Number(d.revision||0)}</dd><dt>ID</dt><dd class="mono-cell">${escapeHtml(d.id||'')}</dd></dl>`;}const entries=Object.entries(d||{}).filter(([,v])=>['string','number','boolean'].includes(typeof v)).slice(0,14);return entries.length?`<dl class="definition-grid">${entries.map(([k,v])=>`<dt>${escapeHtml(titleCase(k))}</dt><dd>${escapeHtml(v)}</dd>`).join('')}</dl>`:'<div class="page-subtitle">No additional structured detail is available for this item.</div>';}
const qa5RenderInspectorBase=renderInspector;
renderInspector=function(){qa5RenderInspectorBase();if(qa4Inspector.kind==='model'&&qa4InspectorTab==='overview')$('#qa5InspectorAgentCheck')?.addEventListener('click',()=>qa5AgentCheck(qa4Inspector.data));};

/* === Alpha 2 live workspace / Follow component preparation === */
const QA6_COMPONENTS={
  follow:{title:'Follow',description:'Follow the authoritative resource/action currently being worked on.',multiple:true,size:'large'},
  activity:{title:'Activity',description:'Realtime control-plane activity for this project.',multiple:true,size:'medium'},
  logs:{title:'Logs',description:'Event-ledger log view scoped to this project and its tasks.',multiple:true,size:'large'},
  agents:{title:'Agents',description:'Workspace orchestration roles, assignments and current task state.',multiple:true,size:'medium'},
  terminal:{title:'Terminal',description:'Governed command/tool execution activity for this workspace.',multiple:true,size:'large'},
  verification:{title:'Verification',description:'Live verification and assurance state.',multiple:true,size:'medium'},
  checkpoints:{title:'Checkpoints',description:'Task checkpoints and invalidation history.',multiple:true,size:'medium'},
  chat:{title:'Chat',description:'Governed workspace chat with OnePane /commands.',multiple:true,size:'large'},
  tasks:{title:'Task list',description:'Tasks associated with this project.',multiple:false,size:'large'},
  scheduled:{title:'Scheduled tasks',description:'Scheduled routines associated with this workspace.',multiple:false,size:'large'},
  models:{title:'Model routing',description:'Workspace orchestration and model routing.',multiple:false,size:'medium'},
  nodes:{title:'Nodes',description:'Local and federated compute nodes.',multiple:false,size:'medium'},
  attention:{title:'Attention',description:'Approvals, blocked work and failures.',multiple:false,size:'medium'},
  notes:{title:'Notepad',description:'Workspace-scoped notes.',multiple:true,size:'medium'}
};
const QA6_INSPECTOR_COMPONENTS=['follow','activity','logs','agents','terminal','verification','checkpoints','chat','notes'];
let qa6EventSource=null,qa6StreamWorkspace='',qa6LiveRenderTimer=null;

function qa6Meta(type){return QA6_COMPONENTS[type]||{title:titleCase(type),description:'OnePane component',multiple:true,size:'medium'};}
function qa6Payload(e){const p=e?.payload;try{return p&&typeof p==='object'?p:JSON.parse(p||'{}')}catch{return {}}}
function qa6ProjectTasks(project){return liveOps.tasks.filter(t=>!t.project_id||t.project_id===project.id);}
function qa6ActiveTask(project,w={}){const rows=qa6ProjectTasks(project),wanted=w?.config?.task_id;if(wanted&&wanted!=='auto'){const found=rows.find(t=>t.id===wanted);if(found)return found;}return rows.find(t=>!['complete','completed','cancelled','failed'].includes(String(t.state||'').toLowerCase()))||rows[0]||null;}
function qa6EventMatchesTask(e,taskID){if(!taskID)return false;const p=qa6Payload(e);return (e.aggregate_type==='task'&&e.aggregate_id===taskID)||String(p.task_id||'')===String(taskID);}
function qa6ProjectEvents(project){const ids=new Set(qa6ProjectTasks(project).map(t=>String(t.id)));return liveOps.events.filter(e=>{const p=qa6Payload(e);return (e.aggregate_type==='task'&&ids.has(String(e.aggregate_id)))||ids.has(String(p.task_id||''))||String(p.project_id||'')===String(project.id);});}
const qa31FollowSurfaceMeta={browser:['Browser','◎'],editor:['Editor','✎'],computer:['Computer','▣'],terminal:['Terminal','>_'],resource:['Resource','▤'],verification:['Verification','✓'],control:['Control plane','◉']};
function qa31SnapshotForEvent(e){
  const p=qa6Payload(e),raw=p.follow_snapshot||p.snapshot;
  if(!raw||typeof raw!=='object')return null;
  const surface=String(raw.surface_type||raw.surface||'control').toLowerCase();
  if(!qa31FollowSurfaceMeta[surface])return null;
  return {...raw,surface_type:surface,sequence:Number(raw.sequence||0),node_id:String(raw.node_id||p.node_id||''),member_id:String(raw.member_id||raw.execution_member_id||p.member_id||''),resource_ref:String(raw.resource_ref||p.resource_ref||p.path||p.url||''),artifact_ref:String(raw.artifact_ref||'')};
}
function qa31LatestSnapshot(events){
  const rows=events.map((e,index)=>({e,index,s:qa31SnapshotForEvent(e)})).filter(x=>x.s);
  rows.sort((a,b)=>(a.s.sequence-b.s.sequence)||(a.index-b.index));
  return rows.at(-1)||null;
}
function qa6SurfaceForEvent(e){const snap=qa31SnapshotForEvent(e);if(snap)return qa31FollowSurfaceMeta[snap.surface_type];const p=qa6Payload(e),tool=String(p.tool_id||''),resource=String(p.resource_ref||p.path||p.url||p.file||'');const hay=`${tool} ${resource}`.toLowerCase();if(/browser|web|http/.test(hay))return ['Browser','◎'];if(/terminal|shell|exec|command|powershell|bash|cmd/.test(hay))return ['Terminal','>_'];if(resource)return ['Resource','▤'];if(String(e?.event_type||'').startsWith('verification.'))return ['Verification','✓'];if(String(e?.event_type||'').startsWith('checkpoint.'))return ['Checkpoint','◆'];return ['Control plane','◉'];}
function qa6FollowContent(w,project){
  const task=qa6ActiveTask(project,w),events=task?[...liveOps.events].filter(e=>qa6EventMatchesTask(e,task.id)):qa6ProjectEvents(project),typed=qa31LatestSnapshot(events),latest=typed?.e||events.at(-1),p=qa6Payload(latest),snap=typed?.s,[surface,icon]=qa6SurfaceForEvent(latest),resource=snap?.resource_ref||p.resource_ref||p.path||p.url||p.tool_id||latest?.aggregate_id||'Waiting for the next authoritative operation',tasks=qa6ProjectTasks(project);
  const origin=snap?[snap.node_id&&`Node ${snap.node_id}`,snap.member_id&&`Member ${snap.member_id}`].filter(Boolean).join(' · '):'';
  const preview=snap?.artifact_ref?`<div class="list-meta">Snapshot artifact: ${escapeHtml(snap.artifact_ref)}</div>`:'';
  return `<div class="qa6-follow"><div class="qa6-follow-toolbar"><span class="qa6-live-dot"></span><strong>Live</strong><select data-qa6-follow-task="${escapeHtml(w.id)}"><option value="auto">Follow active task</option>${tasks.map(t=>`<option value="${escapeHtml(t.id)}" ${(w.config?.task_id===t.id)?'selected':''}>${escapeHtml((t.objective||t.id).slice(0,58))}</option>`).join('')}</select></div>${task?`<div class="qa6-follow-task"><strong>${escapeHtml(task.objective||task.id)}</strong><span class="pill">${escapeHtml(task.state||'created')}</span></div>`:'<div class="empty-state compact">No project task is active yet. Start work from Chat and Follow will bind automatically.</div>'}<div class="qa6-follow-surface"><div class="qa6-follow-icon">${icon}</div><div><div class="list-meta">${escapeHtml(surface)} · ${escapeHtml(latest?eventLabel(latest):'Waiting')}</div><strong class="qa6-follow-resource">${escapeHtml(resource)}</strong>${origin?`<div class="list-meta">${escapeHtml(origin)}</div>`:''}${preview}${p.tool_id?`<div class="list-meta">Tool: ${escapeHtml(p.tool_id)}</div>`:''}</div></div><div class="qa6-follow-events">${events.length?[...events].slice(-5).reverse().map(e=>`<div><span>${escapeHtml(eventTime(e))}</span><strong>${escapeHtml(eventLabel(e))}</strong></div>`).join(''):'<div class="list-meta">Follow is driven by harness events, not model narration.</div>'}</div></div>`;
}
function qa6LogsContent(project){const rows=qa6ProjectEvents(project).slice(-30).reverse();return rows.length?`<div class="qa6-log-list">${rows.map(e=>{const p=qa6Payload(e);return `<div class="qa6-log-row"><span>${escapeHtml(eventTime(e))}</span><strong>${escapeHtml(eventLabel(e))}</strong><small>${escapeHtml(p.resource_ref||p.tool_id||e.aggregate_id||'')}</small></div>`}).join('')}</div>`:'<div class="empty-state compact">No project events yet.</div>';}
function qa6AgentsContent(project,workspace){const o=workspace.orchestration||{},task=qa6ActiveTask(project),displayMode=typeof qa8Mode==='function'?qa8Mode(o.mode):(o.mode==='team'||o.mode==='council'?o.mode:'direct');const role=(name,x,active=false)=>`<div class="qa6-agent-row"><span class="qa6-agent-state ${task&&active?'live':''}"></span><div><strong>${escapeHtml(name)}${active?' · active':''}</strong><div class="list-meta">${escapeHtml(x?.agent||(name==='Direct'?'agent.md':'onepane-default'))} · ${escapeHtml(x?.model||'auto')}</div></div></div>`;return `<div class="qa6-agent-stack"><div class="list-meta">Mode: ${escapeHtml(titleCase(displayMode))}${task?` · task ${escapeHtml(task.state||'running')}`:''}</div>${role('Direct',o.supervisor,displayMode==='direct')}${role('Team',o.team,displayMode==='team')}${role('Council',o.council,displayMode==='council')}</div>`;}
function qa6TerminalContent(project){const rows=qa6ProjectEvents(project).filter(e=>{const p=qa6Payload(e);return /terminal|shell|exec|command|powershell|bash|cmd/i.test(`${p.tool_id||''} ${p.capability_id||''}`)}).slice(-20);return `<div class="qa6-terminal"><div class="qa6-terminal-head">Governed execution stream</div>${rows.length?rows.map(e=>{const p=qa6Payload(e);return `<div class="qa6-terminal-line"><span>${escapeHtml(eventTime(e))}</span> <strong>${escapeHtml(p.tool_id||p.capability_id||'tool')}</strong> ${escapeHtml(p.resource_ref||eventLabel(e))}</div>`}).join(''):'<div class="qa6-terminal-line muted">No terminal/command operation has been emitted for this project.</div>'}</div>`;}
function qa6AssuranceContent(project,prefix){const rows=qa6ProjectEvents(project).filter(e=>String(e.event_type||'').startsWith(prefix+'.')).slice(-20).reverse();return rows.length?`<ul class="list">${rows.map(e=>`<li class="list-row"><div class="list-main"><div class="list-title">${escapeHtml(eventLabel(e))}</div><div class="list-meta">${escapeHtml(eventTime(e))} · ${escapeHtml(e.aggregate_id||'')}</div></div></li>`).join('')}</ul>`:`<div class="empty-state compact">No ${escapeHtml(prefix)} events yet.</div>`;}
function qa6ActivityContent(project){const rows=qa6ProjectEvents(project).slice(-12).reverse();return rows.length?`<ul class="list">${rows.map(e=>`<li class="list-row"><div class="list-main"><div class="list-title">${escapeHtml(eventLabel(e))}</div><div class="list-meta">${escapeHtml(eventTime(e))} · ${escapeHtml(e.aggregate_type||'')}</div></div></li>`).join('')}</ul>`:'<div class="empty-state compact">No recent project activity.</div>';}
function qa6ChatContent(workspace){return `<div class="qa6-chat-component"><strong>${escapeHtml(workspace.name)} chat</strong><p class="page-subtitle">The main workspace composer remains the command surface. Slash commands use the existing OnePane /commands service.</p><div class="qa6-command-chips">${['/status','/model','/agent','/task','/plan','/diff','/verify','/checkpoint'].map(x=>`<button class="tiny" data-qa6-command-chip="${x}">${x}</button>`).join('')}</div><button class="btn primary" data-qa6-open-chat>Open chat composer</button></div>`;}
function qa6ComponentContent(w,project,workspace){switch(w.type){case'follow':return qa6FollowContent(w,project);case'logs':return qa6LogsContent(project);case'agents':return qa6AgentsContent(project,workspace);case'terminal':return qa6TerminalContent(project);case'verification':return qa6AssuranceContent(project,'verification');case'checkpoints':return qa6AssuranceContent(project,'checkpoint');case'activity':return qa6ActivityContent(project);case'chat':return qa6ChatContent(workspace);default:return qa6WorkspaceContentBase(w,project,workspace);}}

const qa6WorkspaceContentBase=qa4WorkspaceWidgetContent;
qa4WorkspaceWidgetContent=function(w,project,workspace){return qa6ComponentContent(w,project,workspace);};
qa4WorkspaceCatalogue=function(){return Object.entries(QA6_COMPONENTS).map(([type,m])=>[type,m.title]);};

const qa6DefaultWorkspaceBase=qa4DefaultWorkspace;
qa4DefaultWorkspace=function(project,name='Main workspace'){const ws=qa6DefaultWorkspaceBase(project,name);ws.widgets=Array.isArray(ws.widgets)?ws.widgets:[];if(!ws.widgets.some(x=>x.type==='follow'))ws.widgets.unshift({id:`pw-follow-${Date.now()}`,type:'follow',title:'Follow',col:8,row:6,config:{task_id:'auto'}});ws.inspector=ws.inspector||{tabs:[],tiles:[]};return ws;};

function qa6SizeFor(type){const size=qa6Meta(type).size,p=workspacePreset(size);return p;}
qa4AddWorkspaceComponent=function(project,workspace){const used=new Set((workspace.widgets||[]).map(x=>x.type));const available=Object.entries(QA6_COMPONENTS).filter(([t,m])=>m.multiple||!used.has(t));openModal('Add workspace component',`<div class="component-picker-grid">${available.map(([t,m])=>`<button class="component-choice" data-qa6-add-component="${t}"><strong>${escapeHtml(m.title)}</strong><span>${escapeHtml(m.description)}</span></button>`).join('')}</div>`);$$('[data-qa6-add-component]',$('#overlayRoot')).forEach(b=>b.onclick=async()=>{const type=b.dataset.qa6AddComponent,m=qa6Meta(type),size=qa6SizeFor(type);workspace.widgets=workspace.widgets||[];workspace.widgets.push({id:`pw-${type}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,5)}`,type,title:m.title,col:size.col,row:size.row,config:type==='follow'?{task_id:'auto'}:{}});try{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));closeModal();renderProjects();}catch(ex){notice(ex.message,'bad');}});};

function qa6InspectorConfig(workspace){if(!workspace.inspector||typeof workspace.inspector!=='object')workspace.inspector={tabs:[],tiles:[]};if(!Array.isArray(workspace.inspector.tabs))workspace.inspector.tabs=[];if(!Array.isArray(workspace.inspector.tiles))workspace.inspector.tiles=[];return workspace.inspector;}
async function qa6SaveInspector(project,workspace){await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));}
async function qa6OpenInInspector(project,workspace,type){const cfg=qa6InspectorConfig(workspace);if(!cfg.tabs.includes(type))cfg.tabs.push(type);await qa6SaveInspector(project,workspace);qa4Inspector={kind:'workspace',id:workspace.id,title:`${project.name} / ${workspace.name}`,data:{project,workspace}};qa4InspectorTab=type;setInspectorOpen(true);renderInspector();}
async function qa6AddInspectorTile(project,workspace,type){const cfg=qa6InspectorConfig(workspace),m=qa6Meta(type);cfg.tiles.push({id:`ip-${type}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,5)}`,type,title:m.title,config:type==='follow'?{task_id:'auto'}:{}});await qa6SaveInspector(project,workspace);qa4Inspector={kind:'workspace',id:workspace.id,title:`${project.name} / ${workspace.name}`,data:{project,workspace}};qa4InspectorTab='panels';setInspectorOpen(true);renderInspector();}
async function qa6PromoteInspectorComponent(project,workspace,type){const m=qa6Meta(type),size=qa6SizeFor(type);workspace.widgets=workspace.widgets||[];workspace.widgets.push({id:`pw-${type}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,5)}`,type,title:m.title,col:size.col,row:size.row,config:type==='follow'?{task_id:'auto'}:{}});await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();}

function qa6ProjectChatSession(project,workspace){return `project:${project.id}:workspace:${workspace.id}`;}
function qa6DrawProjectSlashSuggestions(input){let box=$('#qa6ProjectSlashSuggestions');if(!box){box=document.createElement('div');box.id='qa6ProjectSlashSuggestions';box.className='slash-suggestions qa6-project-slash';box.hidden=true;input.parentElement.insertBefore(box,input);}const items=suggestChatCommands(input.value);if(!items.length){box.hidden=true;box.innerHTML='';return;}box.hidden=false;box.innerHTML=items.map(c=>`<button class="slash-item" data-qa6-slash="${escapeHtml(c.usage)}"><strong>/${escapeHtml(c.name)}</strong><span>${escapeHtml(c.description)}</span><small>${escapeHtml(c.category)}</small></button>`).join('');$$('[data-qa6-slash]',box).forEach(b=>b.onclick=()=>{input.value=b.dataset.qa6Slash.replace(/\s*[<\[].*$/,' ');input.focus();qa6DrawProjectSlashSuggestions(input);});}
async function qa6RunProjectSlash(project,workspace,raw,history){history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message user"><strong>You</strong><span>${escapeHtml(raw)}</span></div>`);try{const out=await api.chatCommand(qa6ProjectChatSession(project,workspace),raw);const message=out?.message||`${raw} accepted by the OnePane command service.`;history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system"><strong>OnePane</strong><span>${escapeHtml(message)}</span></div>`);liveOps.lastRefresh=0;await refreshOperationalDataQA(true);}catch(ex){history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system error"><strong>OnePane</strong><span>${escapeHtml(ex.message)}</span></div>`);}}

function qa6ApplyMaximized(workspace){const id=workspace.maximized_widget_id||'';const grid=$('#qa4WorkspaceGrid');if(!grid)return;grid.classList.toggle('qa6-maximized-grid',!!id);$$('[data-pw-widget]',grid).forEach(el=>{const active=!id||el.dataset.pwWidget===id;el.classList.toggle('hidden',!active);el.classList.toggle('qa6-maximized-widget',!!id&&active);if(id&&active){el.style.gridColumn='span 12';el.style.gridRow='span 10';}});}
async function qa6ToggleMaximize(project,workspace,id){workspace.maximized_widget_id=workspace.maximized_widget_id===id?'':id;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();}
function qa6BindProjectComponents(project,workspace){qa6ApplyMaximized(workspace);const edit=!!state.projectWorkspaceEdit;$$('[data-pw-widget]',$('#qa4WorkspaceGrid')).forEach(el=>{const w=(workspace.widgets||[]).find(x=>x.id===el.dataset.pwWidget),handle=$('.widget-handle',el);if(!w||!handle||$('.qa6-widget-actions',handle))return;handle.insertAdjacentHTML('beforeend',`<span class="qa6-widget-actions"><button class="tiny" data-qa6-inspector="${escapeHtml(w.id)}" title="Open in Inspector">⇥</button>${!edit?`<button class="tiny" data-qa6-max="${escapeHtml(w.id)}" title="${workspace.maximized_widget_id===w.id?'Restore':'Maximise'}">${workspace.maximized_widget_id===w.id?'↙':'□'}</button>`:''}</span>`);});$$('[data-qa6-max]').forEach(b=>b.onclick=e=>{e.stopPropagation();qa6ToggleMaximize(project,workspace,b.dataset.qa6Max);});$$('[data-qa6-inspector]').forEach(b=>b.onclick=e=>{e.stopPropagation();const w=(workspace.widgets||[]).find(x=>x.id===b.dataset.qa6Inspector);if(w)qa6OpenInInspector(project,workspace,w.type);});$$('[data-qa6-follow-task]').forEach(sel=>sel.onchange=async()=>{const id=sel.dataset.qa6FollowTask,w=(workspace.widgets||[]).find(x=>x.id===id)||(qa6InspectorConfig(workspace).tiles||[]).find(x=>x.id===id);if(!w)return;w.config=w.config||{};w.config.task_id=sel.value;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));renderProjects();});$$('[data-qa6-open-chat]').forEach(b=>b.onclick=()=>$('#qa4ChatInput')?.focus());$$('[data-qa6-command-chip]').forEach(b=>b.onclick=()=>{const input=$('#qa4ChatInput');if(input){input.value=b.dataset.qa6CommandChip+' ';input.focus();qa6DrawProjectSlashSuggestions(input);}});const input=$('#qa4ChatInput'),form=$('#qa4WorkspaceChat'),history=$('#qa4ChatHistory');if(input&&form&&history){input.placeholder='Ask OnePane… Type / for commands';input.oninput=()=>qa6DrawProjectSlashSuggestions(input);input.onkeydown=e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();form.requestSubmit();}};form.onsubmit=e=>{e.preventDefault();const value=input.value.trim();if(!value)return;input.value='';$('#qa6ProjectSlashSuggestions')?.setAttribute('hidden','');if(value.startsWith('/'))qa6RunProjectSlash(project,workspace,value,history);else qa4SendWorkspaceChat(project,workspace,value,history);};}}

function qa6ScheduleLiveRender(){clearTimeout(qa6LiveRenderTimer);qa6LiveRenderTimer=setTimeout(()=>{syncLiveNotifications();const interacting=!!document.querySelector('.layout-interacting')||a31LayoutSaveInFlight>0;if(interacting){qa6ScheduleLiveRender();return}if(currentTab()?.route==='projects')renderProjects();else if(currentTab()?.route==='operations')renderOperations();if($('#bottomDrawer')?.dataset.state==='open')renderDrawer();},180);}
function qa6StartEventStream(){if(!onepaneWorkspace||typeof EventSource==='undefined')return;if(qa6EventSource&&qa6StreamWorkspace===onepaneWorkspace)return;try{qa6EventSource?.close()}catch{}qa6StreamWorkspace=onepaneWorkspace;const after=Math.max(0,...liveOps.events.map(e=>Number(e.sequence||0)));const es=new EventSource(`/v1/events/stream?workspace_id=${encodeURIComponent(onepaneWorkspace)}&after=${after}&generic=1`);qa6EventSource=es;es.addEventListener('onepane',ev=>{try{const item=JSON.parse(ev.data);if(!item||!item.sequence)return;if(!liveOps.events.some(x=>Number(x.sequence)===Number(item.sequence)))liveOps.events.push(item);liveOps.events=liveOps.events.slice(-500);liveOps.reported.events=true;liveOps.lastRefresh=Date.now();qa6ScheduleLiveRender();}catch{}});es.onerror=()=>{};}

const qa6RenderProjectsBase=renderProjects;
renderProjects=async function(){await qa6RenderProjectsBase();const p=qa4ActiveProject?.(),ws=p?qa4ActiveWorkspace?.():null;if(p&&ws)qa6BindProjectComponents(p,ws);qa6StartEventStream();};

const qa6LegacyAddInspectorTab=qa4AddInspectorTab;
function qa6WorkspaceInspectorContext(){const d=qa4Inspector.data||{};return qa4Inspector.kind==='workspace'&&d.project&&d.workspace?d:null;}
function qa6InspectorTabs(){const d=qa6WorkspaceInspectorContext();if(!d)return qa4InspectorTabs();const cfg=qa6InspectorConfig(d.workspace);return ['overview','configuration',...cfg.tabs.filter(x=>QA6_INSPECTOR_COMPONENTS.includes(x)),...(cfg.tiles.length?['panels']:[])].filter((x,i,a)=>a.indexOf(x)===i);}
function qa6RenderInspectorPanels(project,workspace){const cfg=qa6InspectorConfig(workspace);return `<div class="qa6-inspector-panel-head"><span class="page-subtitle">Tiled Inspector components</span><button class="btn tiny" id="qa6AddInspectorTile">Add tile</button></div><div class="qa6-inspector-panel-grid">${cfg.tiles.length?cfg.tiles.map(t=>`<section class="qa6-inspector-tile" data-qa6-inspector-tile="${escapeHtml(t.id)}"><div class="qa6-inspector-tile-head"><strong>${escapeHtml(t.title||qa6Meta(t.type).title)}</strong><span><button class="tiny" data-qa6-promote-tile="${escapeHtml(t.id)}" title="Tile in workspace">↗</button><button class="tiny danger" data-qa6-remove-tile="${escapeHtml(t.id)}">×</button></span></div>${qa6ComponentContent(t,project,workspace)}</section>`).join(''):'<div class="empty-state compact">No tiled Inspector components yet.</div>'}</div>`;}
function qa6InspectorPicker(project,workspace,mode='tab'){const choices=QA6_INSPECTOR_COMPONENTS.filter(x=>x!=='notes'||true);openModal(mode==='tile'?'Add Inspector tile':'Add Inspector tab',`<div class="component-picker-grid">${choices.map(type=>{const m=qa6Meta(type);return `<button class="component-choice" data-qa6-inspector-choice="${type}"><strong>${escapeHtml(m.title)}</strong><span>${escapeHtml(m.description)}</span></button>`}).join('')}</div>`);$$('[data-qa6-inspector-choice]',$('#overlayRoot')).forEach(b=>b.onclick=async()=>{const type=b.dataset.qa6InspectorChoice,cfg=qa6InspectorConfig(workspace);if(mode==='tile'){cfg.tiles.push({id:`ip-${type}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,5)}`,type,title:qa6Meta(type).title,config:type==='follow'?{task_id:'auto'}:{}});qa4InspectorTab='panels';}else{if(!cfg.tabs.includes(type))cfg.tabs.push(type);qa4InspectorTab=type;}await qa6SaveInspector(project,workspace);closeModal();renderInspector();});}
qa4AddInspectorTab=function(){const d=qa6WorkspaceInspectorContext();if(!d)return qa6LegacyAddInspectorTab();qa6InspectorPicker(d.project,d.workspace,'tab');};

renderInspector=function(){const d=qa6WorkspaceInspectorContext();if(!d){const base=qa5RenderInspectorBase;/* QA5 preserved base for non-workspace contexts. */base();if(qa4Inspector.kind==='model'&&qa4InspectorTab==='overview')$('#qa5InspectorAgentCheck')?.addEventListener('click',()=>qa5AgentCheck(qa4Inspector.data));return;}const {project,workspace}=d,tabs=qa6InspectorTabs();if(!tabs.includes(qa4InspectorTab))qa4InspectorTab='overview';const cfg=qa6InspectorConfig(workspace),optional=t=>!['overview','configuration','panels'].includes(t);$('#inspector').innerHTML=`<div class="inspector-header">Inspector <button class="inspector-close" id="closeInspector">×</button></div><div class="inspector-section"><div class="inspector-title">${escapeHtml(qa4Inspector.title||workspace.name)}</div><div class="page-subtitle">Workspace</div></div><div class="inspector-tabs qa4-inspector-tabs">${tabs.map(t=>`<span class="qa4-inspector-tab-wrap"><button class="${t===qa4InspectorTab?'active':''}" data-qa6-inspector-tab="${t}">${titleCase(t)}</button>${optional(t)?`<span class="qa4-inspector-tab-tools"><button title="Move left" data-qa6-tab-move="${t}:-1">‹</button><button title="Move right" data-qa6-tab-move="${t}:1">›</button><button title="Remove" data-qa6-tab-remove="${t}">×</button></span>`:''}</span>`).join('')}<button class="inspector-tab-add" id="qa4InspectorAddTab" title="Add Inspector tab">+</button></div><div class="inspector-section" id="qa4InspectorContent"></div>`;$('#closeInspector').onclick=()=>setInspectorOpen(false);$$('[data-qa6-inspector-tab]').forEach(b=>b.onclick=()=>{qa4InspectorTab=b.dataset.qa6InspectorTab;renderInspector();});$('#qa4InspectorAddTab').onclick=qa4AddInspectorTab;$$('[data-qa6-tab-remove]').forEach(b=>b.onclick=async e=>{e.stopPropagation();cfg.tabs=cfg.tabs.filter(x=>x!==b.dataset.qa6TabRemove);if(qa4InspectorTab===b.dataset.qa6TabRemove)qa4InspectorTab='overview';await qa6SaveInspector(project,workspace);renderInspector();});$$('[data-qa6-tab-move]').forEach(b=>b.onclick=async e=>{e.stopPropagation();const [type,raw]=b.dataset.qa6TabMove.split(':'),i=cfg.tabs.indexOf(type),j=i+Number(raw);if(i<0||j<0||j>=cfg.tabs.length)return;[cfg.tabs[i],cfg.tabs[j]]=[cfg.tabs[j],cfg.tabs[i]];await qa6SaveInspector(project,workspace);renderInspector();});const c=$('#qa4InspectorContent');if(qa4InspectorTab==='overview')c.innerHTML=qa4InspectorOverview();else if(qa4InspectorTab==='configuration')c.innerHTML=qa4InspectorConfiguration();else if(qa4InspectorTab==='panels')c.innerHTML=qa6RenderInspectorPanels(project,workspace);else{const m=qa6Meta(qa4InspectorTab),tabCfg=(workspace.inspector.tab_config||{})[qa4InspectorTab]||{},virtual={id:`inspector-${qa4InspectorTab}`,type:qa4InspectorTab,title:m.title,config:qa4InspectorTab==='follow'?{task_id:tabCfg.task_id||'auto'}:tabCfg};c.innerHTML=`<div class="qa6-inspector-component-toolbar"><button class="btn tiny" id="qa6PromoteInspector">Tile in workspace</button><button class="btn tiny" id="qa6TileInspector">Add to Panels</button></div>${qa6ComponentContent(virtual,project,workspace)}`;$('#qa6PromoteInspector').onclick=()=>qa6PromoteInspectorComponent(project,workspace,qa4InspectorTab);$('#qa6TileInspector').onclick=()=>qa6AddInspectorTile(project,workspace,qa4InspectorTab);}$('#qa6AddInspectorTile')?.addEventListener('click',()=>qa6InspectorPicker(project,workspace,'tile'));$$('[data-qa6-remove-tile]').forEach(b=>b.onclick=async()=>{cfg.tiles=cfg.tiles.filter(x=>x.id!==b.dataset.qa6RemoveTile);await qa6SaveInspector(project,workspace);renderInspector();});$$('[data-qa6-promote-tile]').forEach(b=>b.onclick=()=>{const t=cfg.tiles.find(x=>x.id===b.dataset.qa6PromoteTile);if(t)qa6PromoteInspectorComponent(project,workspace,t.type);});$$('[data-project-note]',$('#inspector')).forEach(x=>x.oninput=()=>qa4SetContextNote(x.dataset.projectNote,x.value));$$('[data-qa6-open-chat]',$('#inspector')).forEach(b=>b.onclick=()=>{setInspectorOpen(false);$('#qa4ChatInput')?.focus();});$$('[data-qa6-command-chip]',$('#inspector')).forEach(b=>b.onclick=()=>{setInspectorOpen(false);const input=$('#qa4ChatInput');if(input){input.value=b.dataset.qa6CommandChip+' ';input.focus();qa6DrawProjectSlashSuggestions(input);}});$$('[data-qa6-follow-task]',$('#inspector')).forEach(sel=>sel.onchange=()=>{const active=qa4InspectorTab==='panels'?cfg.tiles.find(x=>x.id===sel.dataset.qa6FollowTask):null;if(active){active.config=active.config||{};active.config.task_id=sel.value;qa6SaveInspector(project,workspace).then(renderInspector);}else{const tabs=workspace.inspector.tab_config=workspace.inspector.tab_config||{};tabs.follow=tabs.follow||{};tabs.follow.task_id=sel.value;qa6SaveInspector(project,workspace).then(renderInspector);}});qa4BindInspectorConfig();syncPanelRestoreButtons();};

/* === QA7 workspace-owned component policy, routing controls and brokered remote access === */
QA6_COMPONENTS.settings={title:'Workspace settings',description:'Workspace-owned routing, team, fallback, sandbox and remote-access policy.',multiple:false,size:'large'};
if(!QA6_INSPECTOR_COMPONENTS.includes('settings'))QA6_INSPECTOR_COMPONENTS.push('settings');

function qa7WorkspaceDefaults(){
  const d=qa5Prefs().workspace_defaults||{},external=!!d.external_network;
  return {sandbox:{network:external,internet:external,lan:false,browser:!!d.browser,computer:!!d.computer,host_files:'workspace-only',secrets:'selected'},routing:{enabled:d.model_routing!==false},remote:{enabled:d.remote_models!==false,access_mode:'brokered'}};
}
function qa7NormalizeRole(x={},fallbackAgent='onepane-default'){return {model:x.model||'auto',agent:x.agent||fallbackAgent,fallback_model:x.fallback_model||'auto',fallback_agent:x.fallback_agent||'',count:Number(x.count||2)};}
function qa7NormalizeWorkspace(project,workspace){
  if(!workspace||typeof workspace!=='object')return workspace;
  const defaults=qa7WorkspaceDefaults(),legacy=qa4ProjectUI(project).sandbox||{};
  workspace.sandbox={...defaults.sandbox,...legacy,...(workspace.sandbox||{})};
  workspace.routing={enabled:true,...(workspace.routing||{})};
  workspace.remote={enabled:true,access_mode:'brokered',...(workspace.remote||{})};
  workspace.orchestration=workspace.orchestration||{};
  workspace.orchestration.mode=['direct','supervisor','workers','team','council'].includes(workspace.orchestration.mode)?workspace.orchestration.mode:'supervisor';
  workspace.orchestration.supervisor=qa7NormalizeRole(workspace.orchestration.supervisor,'agent.md');
  workspace.orchestration.workers=qa7NormalizeRole(workspace.orchestration.workers,'onepane-default');
  workspace.orchestration.team=qa7NormalizeRole(workspace.orchestration.team,'onepane-default');
  workspace.orchestration.council=qa7NormalizeRole(workspace.orchestration.council,'onepane-default');
  workspace.inspector=workspace.inspector||{tabs:[],tiles:[],tab_config:{}};
  workspace.inspector.tab_config=workspace.inspector.tab_config||{};
  return workspace;
}
const qa7WorkspacesBase=qa4Workspaces;
qa4Workspaces=function(project){return qa7WorkspacesBase(project).map(w=>qa7NormalizeWorkspace(project,w));};

const qa7DefaultWorkspaceBase=qa4DefaultWorkspace;
qa4DefaultWorkspace=function(project,name='Main workspace'){
  const ws=qa7NormalizeWorkspace(project,qa7DefaultWorkspaceBase(project,name)),d=qa5Prefs().workspace_defaults||{};
  ws.routing.enabled=d.model_routing!==false;ws.remote.enabled=d.remote_models!==false;ws.sandbox={...qa7WorkspaceDefaults().sandbox};
  if(!ws.widgets.some(x=>x.type==='settings'))ws.widgets.push({id:`pw-settings-${Date.now()+3}`,type:'settings',title:'Workspace settings',col:6,row:5});
  return ws;
};

function qa7WorkspaceSandbox(project,workspace){return qa7NormalizeWorkspace(project,workspace).sandbox;}
qa4ProjectSandbox=function(project,workspace){const ws=workspace||qa4ActiveWorkspace?.();return ws?qa7WorkspaceSandbox(project,ws):{...qa7WorkspaceDefaults().sandbox,...(qa4ProjectUI(project).sandbox||{})};};

function qa7RouteFallback(workspace,role='supervisor'){
  const r=workspace?.orchestration?.[role]||{},ids=[],profiles=[];
  for(const raw of [r.fallback_model,r.fallback_agent]){const v=String(raw||'').trim();if(v.startsWith('candidate:'))ids.push(v.slice('candidate:'.length));else if(v&&v!=='auto'&&!profiles.includes(v))profiles.push(v);}
  return {candidate_ids:[...new Set(ids)],agent_profiles:profiles};
}
function qa7WorkspaceAccess(workspace){
  const s=workspace.sandbox||qa7WorkspaceDefaults().sandbox;
  return {mode:'brokered',project_workspace_id:workspace.id,remote_models:workspace.remote?.enabled!==false,filesystem:s.host_files||'workspace-only',internet:!!s.internet,lan:!!s.lan,browser:!!s.browser,computer:!!s.computer,secrets:s.secrets||'selected'};
}
function qa7EffectiveChatMode(workspace,requested='default'){let mode=requested==='default'?(workspace.orchestration?.mode||'supervisor'):requested;if(workspace.routing?.enabled===false&&['workers','team','council'].includes(mode))mode='supervisor';return mode;}
function qa7RoutingEnvelope(workspace,requested='default'){
  const enabled=workspace.routing?.enabled!==false,mode=qa7EffectiveChatMode(workspace,requested),role=mode==='direct'?'supervisor':mode,selected=qa4RouteSelection(workspace,role),fallback=qa7RouteFallback(workspace,role);
  return {enabled,mode,role,candidate_id:selected.candidate_id,agent_profile:selected.agent_profile,fallback_candidate_ids:enabled?fallback.candidate_ids:[],fallback_agent_profiles:enabled?fallback.agent_profiles:[],project_workspace_id:workspace.id,workspace_access:qa7WorkspaceAccess(workspace)};
}

function qa7SettingsRole(key,label,o){
  const x=o[key]||{},models=qa4ModelOptions(),agents=qa4AgentOptions();
  return `<fieldset class="inspector-fieldset qa7-role"><legend>${escapeHtml(label)}</legend><label>Primary model<select data-qa7-role-model="${key}">${qa4OptionRows(models,x.model||'auto')}</select></label><label>Primary agent<select data-qa7-role-agent="${key}">${qa4OptionRows(agents,x.agent||(key==='supervisor'?'agent.md':'onepane-default'))}</select></label><label>Fallback model<select data-qa7-role-fallback-model="${key}">${qa4OptionRows(models,x.fallback_model||'auto')}</select></label><label>Fallback agent<select data-qa7-role-fallback-agent="${key}"><option value="">No separate fallback agent</option>${qa4OptionRows(agents,x.fallback_agent||'')}</select></label>${key==='workers'?`<label>Worker count<input data-qa7-worker-count type="number" min="1" max="32" value="${Number(x.count||2)}"></label>`:''}</fieldset>`;
}
function qa7WorkspaceSettingsContent(w,project,workspace){
  qa7NormalizeWorkspace(project,workspace);const s=workspace.sandbox,o=workspace.orchestration||{},routing=workspace.routing?.enabled!==false,remote=workspace.remote?.enabled!==false;
  return `<form class="qa7-workspace-settings settings-stack" data-qa7-settings-root="${escapeHtml(w.id)}"><div class="qa7-settings-heading"><div><strong>Workspace-owned settings</strong><div class="list-meta">Defaults were copied when this workspace was created. Changes here affect this workspace only.</div></div><span class="pill ${routing?'good':''}">${routing?'Routing enabled':'Single-path'}</span></div><label>Workspace name<input data-qa7-workspace-name value="${escapeHtml(workspace.name||'Workspace')}"></label><label>Default chat mode<select data-qa7-default-mode>${['direct','team','council'].map(x=>`<option value="${x}" ${o.mode===x?'selected':''}>${titleCase(x)}</option>`).join('')}</select></label><label class="inline-check"><input type="checkbox" data-qa7-routing ${routing?'checked':''}> Enable model routing, delegation and automatic fallback</label><div class="page-subtitle">When disabled, OnePane keeps each task on a single reasoning path, blocks delegated child work, and does not automatically escalate to another model.</div>${qa7SettingsRole('supervisor','Direct',o)}${qa7SettingsRole('team','Team',o)}${qa7SettingsRole('council','Council',o)}<fieldset class="inspector-fieldset"><legend>Workspace sandbox</legend><label class="inline-check"><input type="checkbox" data-qa7-sandbox="internet" ${s.internet?'checked':''}> Internet access</label><label class="inline-check"><input type="checkbox" data-qa7-sandbox="lan" ${s.lan?'checked':''}> LAN access</label><label class="inline-check"><input type="checkbox" data-qa7-sandbox="browser" ${s.browser?'checked':''}> Browser capability</label><label class="inline-check"><input type="checkbox" data-qa7-sandbox="computer" ${s.computer?'checked':''}> Computer capability</label><label>Host files<select data-qa7-host-files><option value="workspace-only" ${s.host_files!=='none'?'selected':''}>This workspace only</option><option value="none" ${s.host_files==='none'?'selected':''}>No filesystem access</option></select></label><label>Secrets<select data-qa7-secrets><option value="selected" ${s.secrets!=='none'?'selected':''}>Selected Vault handles</option><option value="none" ${s.secrets==='none'?'selected':''}>None</option></select></label></fieldset><fieldset class="inspector-fieldset"><legend>Remote execution</legend><label class="inline-check"><input type="checkbox" data-qa7-remote ${remote?'checked':''}> Allow qualified enrolled-node / cloud reasoning workers</label><label>Workspace access<input value="Brokered OnePane capabilities only" disabled></label><div class="page-subtitle">Remote models never receive a host filesystem mount. They receive compiled context and can request only capabilities allowed by this workspace policy; OnePane executes and records those operations.</div></fieldset><button class="btn primary" type="submit">Save workspace settings</button><div class="page-subtitle" data-qa7-settings-status></div></form>`;
}

function qa7ChatKey(project,workspace,w){return `onepane.chat.v3:${project.id}:${workspace.id}:${w.id}`;}
function qa7ChatHistory(project,workspace,w){try{const rows=JSON.parse(sessionStorage.getItem(qa7ChatKey(project,workspace,w))||'[]');return Array.isArray(rows)?rows.slice(-30):[]}catch{return []}}
function qa7AppendChat(project,workspace,w,role,text){const rows=qa7ChatHistory(project,workspace,w);rows.push({role,text:String(text||''),at:Date.now()});sessionStorage.setItem(qa7ChatKey(project,workspace,w),JSON.stringify(rows.slice(-30)));}
function qa7ChatContent(w,project,workspace){
  const cfg=w.config=w.config||{},routing=workspace.routing?.enabled!==false,requested=cfg.chat_mode||'default',rows=qa7ChatHistory(project,workspace,w),modeOptions=[['default',`Default (${titleCase((workspace.orchestration?.mode==='team'||workspace.orchestration?.mode==='council')?workspace.orchestration.mode:'direct')})`],['direct','Direct'],['team','Team'],['council','Council']];
  return `<div class="qa7-chat" data-qa7-chat-root="${escapeHtml(w.id)}"><div class="qa7-chat-toolbar"><label>Mode<select data-qa7-chat-mode="${escapeHtml(w.id)}">${modeOptions.map(([v,l])=>`<option value="${v}" ${requested===v?'selected':''} ${!routing&&['team','council'].includes(v)?'disabled':''}>${escapeHtml(l)}</option>`).join('')}</select></label><span class="pill ${routing?'good':''}">${routing?'Routing on':'Single path'}</span></div><div class="qa7-chat-history" data-qa7-chat-history="${escapeHtml(w.id)}">${rows.length?rows.map(x=>`<div class="workspace-chat-message ${x.role==='user'?'user':'system'}"><strong>${x.role==='user'?'You':'OnePane'}</strong><span>${escapeHtml(x.text)}</span></div>`).join(''):'<div class="empty-state compact">Chat creates governed tasks. Type / for deterministic workspace commands.</div>'}</div><form class="qa7-chat-form" data-qa7-chat-form="${escapeHtml(w.id)}"><textarea rows="2" data-qa7-chat-input="${escapeHtml(w.id)}" placeholder="Ask OnePane… Type / for commands"></textarea><button class="btn primary">Send</button><div class="slash-suggestions qa7-slash" hidden></div></form></div>`;
}
function qa7DrawSlashSuggestions(input){
  const box=input.parentElement.querySelector('.qa7-slash'),items=suggestChatCommands(input.value);if(!box)return;if(!items.length){box.hidden=true;box.innerHTML='';return;}box.hidden=false;box.innerHTML=items.map(c=>`<button type="button" class="slash-item" data-qa7-slash="${escapeHtml(c.usage)}"><strong>/${escapeHtml(c.name)}</strong><span>${escapeHtml(c.description)}</span><small>${escapeHtml(c.category)}</small></button>`).join('');$$('[data-qa7-slash]',box).forEach(b=>b.onclick=()=>{input.value=b.dataset.qa7Slash.replace(/\s*[<\[].*$/,' ');input.focus();qa7DrawSlashSuggestions(input);});
}
async function qa7SendWorkspaceChat(project,workspace,w,text,requestedMode,history){
  if(!text)return;qa7AppendChat(project,workspace,w,'user',text);history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message user"><strong>You</strong><span>${escapeHtml(text)}</span></div>`);
  if(text.startsWith('/')){try{const out=await api.chatCommand(qa6ProjectChatSession(project,workspace),text),message=out?.message||`${text} accepted by OnePane.`;qa7AppendChat(project,workspace,w,'system',message);history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system"><strong>OnePane</strong><span>${escapeHtml(message)}</span></div>`);await refreshOperationalDataQA(true);}catch(ex){qa7AppendChat(project,workspace,w,'system',ex.message);history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system error"><strong>OnePane</strong><span>${escapeHtml(ex.message)}</span></div>`);}return;}
  const routing=qa7RoutingEnvelope(workspace,requestedMode),payload={workspace_id:onepaneWorkspace,project_id:project.id,objective:text,priority:10,scheduling_class:'user_interactive',completion:{type:'operator_review',onepane_routing:routing}};
  try{const task=await apiRequest('/v1/tasks',{method:'POST',body:JSON.stringify(payload)}),message=`Queued ${task.id||'task'} · ${titleCase(routing.mode)} · ${routing.enabled?(routing.candidate_id?'selected model':'qualified routing'):'single reasoning path'}.`;qa7AppendChat(project,workspace,w,'system',message);history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system"><strong>OnePane</strong><span>${escapeHtml(message)}</span></div>`);liveOps.lastRefresh=0;}catch(ex){qa7AppendChat(project,workspace,w,'system',ex.message);history.insertAdjacentHTML('beforeend',`<div class="workspace-chat-message system error"><strong>OnePane</strong><span>${escapeHtml(ex.message)}</span></div>`);}
}

const qa7ComponentContentBase=qa6ComponentContent;
qa6ComponentContent=function(w,project,workspace){qa7NormalizeWorkspace(project,workspace);if(w.type==='settings')return qa7WorkspaceSettingsContent(w,project,workspace);if(w.type==='chat')return qa7ChatContent(w,project,workspace);return qa7ComponentContentBase(w,project,workspace);};

function qa7FindComponent(workspace,id){const widget=(workspace.widgets||[]).find(x=>x.id===id);if(widget)return widget;const tile=(qa6InspectorConfig(workspace).tiles||[]).find(x=>x.id===id);if(tile)return tile;if(String(id).startsWith('inspector-')){const type=String(id).slice('inspector-'.length),cfg=workspace.inspector.tab_config=workspace.inspector.tab_config||{};cfg[type]=cfg[type]||{};return {id,type,config:cfg[type]};}return null;}
async function qa7SaveWorkspaceSettings(project,workspace,root){
  workspace.name=$('[data-qa7-workspace-name]',root)?.value.trim()||workspace.name;workspace.routing={enabled:!!$('[data-qa7-routing]',root)?.checked};workspace.remote={enabled:!!$('[data-qa7-remote]',root)?.checked,access_mode:'brokered'};workspace.sandbox=workspace.sandbox||qa7WorkspaceDefaults().sandbox;
  $$('[data-qa7-sandbox]',root).forEach(x=>workspace.sandbox[x.dataset.qa7Sandbox]=x.checked);workspace.sandbox.network=workspace.sandbox.internet||workspace.sandbox.lan;workspace.sandbox.host_files=$('[data-qa7-host-files]',root)?.value||'workspace-only';workspace.sandbox.secrets=$('[data-qa7-secrets]',root)?.value||'selected';workspace.orchestration=workspace.orchestration||{};workspace.orchestration.mode=$('[data-qa7-default-mode]',root)?.value||'supervisor';
  for(const key of ['supervisor','workers','team','council']){const role=workspace.orchestration[key]=workspace.orchestration[key]||{};role.model=$(`[data-qa7-role-model="${key}"]`,root)?.value||'auto';role.agent=$(`[data-qa7-role-agent="${key}"]`,root)?.value||(key==='supervisor'?'agent.md':'onepane-default');role.fallback_model=$(`[data-qa7-role-fallback-model="${key}"]`,root)?.value||'auto';role.fallback_agent=$(`[data-qa7-role-fallback-agent="${key}"]`,root)?.value||'';}
  workspace.orchestration.workers.count=Number($('[data-qa7-worker-count]',root)?.value||2);const status=$('[data-qa7-settings-status]',root);
  try{const updated=await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));qa4Inspector.data.project=updated;if(status)status.innerHTML='<span class="good">Saved for this workspace only.</span>';notice('Workspace settings saved.');}catch(ex){if(status)status.innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`;}
}
function qa7BindWorkspaceControls(project,workspace,root=document){
  $$('[data-qa7-settings-root]',root).forEach(form=>{form.onsubmit=e=>{e.preventDefault();qa7SaveWorkspaceSettings(project,workspace,form);};});
  $$('[data-qa7-chat-mode]',root).forEach(sel=>sel.onchange=async()=>{const w=qa7FindComponent(workspace,sel.dataset.qa7ChatMode);if(!w)return;w.config=w.config||{};w.config.chat_mode=sel.value;await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));});
  $$('[data-qa7-chat-form]',root).forEach(form=>{const id=form.dataset.qa7ChatForm,w=qa7FindComponent(workspace,id);if(!w)return;const input=$(`[data-qa7-chat-input="${id}"]`,form),history=$(`[data-qa7-chat-history="${id}"]`,root),mode=$(`[data-qa7-chat-mode="${id}"]`,root);if(!input||!history)return;input.oninput=()=>qa7DrawSlashSuggestions(input);input.onkeydown=e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();form.requestSubmit();}};form.onsubmit=e=>{e.preventDefault();const text=input.value.trim();if(!text)return;input.value='';form.querySelector('.qa7-slash')?.setAttribute('hidden','');qa7SendWorkspaceChat(project,workspace,w,text,mode?.value||'default',history);};});
}

const qa7BindProjectComponentsBase=qa6BindProjectComponents;
qa6BindProjectComponents=function(project,workspace){qa7BindProjectComponentsBase(project,workspace);qa7BindWorkspaceControls(project,workspace,$('#qa4WorkspaceGrid')||document);};

const qa7RenderProjectsBase=renderProjects;
renderProjects=async function(){await qa7RenderProjectsBase();const project=qa4ActiveProject?.(),workspace=project?qa4ActiveWorkspace?.():null;if(!project||!workspace)return;qa7NormalizeWorkspace(project,workspace);$('.workspace-chat-panel')?.remove();const bar=$('.workspace-context-bar .list-meta');if(bar)bar.textContent=` · workspace sandbox ${workspace.sandbox.internet?'internet allowed':'internet blocked'} · ${workspace.routing.enabled!==false?'routing enabled':'single-path'} · ${titleCase(workspace.orchestration?.mode||'supervisor')}`;const button=$('#qa4WorkspaceSettings');if(button){button.textContent='Workspace settings';button.onclick=()=>qa6OpenInInspector(project,workspace,'settings');}qa7BindWorkspaceControls(project,workspace,$('#qa4WorkspaceGrid')||document);};

const qa7InspectorTabsBase=qa6InspectorTabs;
qa6InspectorTabs=function(){const d=qa6WorkspaceInspectorContext();if(!d)return qa7InspectorTabsBase();const cfg=qa6InspectorConfig(d.workspace);return ['overview',...cfg.tabs.filter(x=>QA6_INSPECTOR_COMPONENTS.includes(x)),...(cfg.tiles.length?['panels']:[])].filter((x,i,a)=>a.indexOf(x)===i);};

const qa7InspectorOverviewBase=qa4InspectorOverview;
qa4InspectorOverview=function(){if(qa4Inspector.kind!=='workspace')return qa7InspectorOverviewBase();const d=qa4Inspector.data||{},w=qa7NormalizeWorkspace(d.project,d.workspace),s=w.sandbox||{};return `<dl class="definition-grid"><dt>Project</dt><dd>${escapeHtml(d.project?.name||d.project?.id||'')}</dd><dt>Workspace</dt><dd>${escapeHtml(w.name||w.id||'')}</dd><dt>Routing</dt><dd>${w.routing?.enabled!==false?'Enabled':'Single path'}</dd><dt>Default mode</dt><dd>${escapeHtml(titleCase(w.orchestration?.mode||'supervisor'))}</dd><dt>Remote models</dt><dd>${w.remote?.enabled!==false?'Allowed through brokered capabilities':'Local execution only'}</dd><dt>Sandbox</dt><dd>${s.internet?'Internet allowed':'Internet blocked'} · ${s.lan?'LAN allowed':'LAN blocked'} · ${s.browser?'Browser allowed':'Browser blocked'} · ${s.computer?'Computer allowed':'Computer blocked'}</dd></dl>`;};

const qa7RenderInspectorBase=renderInspector;
renderInspector=function(){qa7RenderInspectorBase();const d=qa6WorkspaceInspectorContext();if(d)qa7BindWorkspaceControls(d.project,d.workspace,$('#inspector')||document);};

const qa7FollowContentBase=qa6FollowContent;
qa6FollowContent=function(w,project){const html=qa7FollowContentBase(w,project),task=qa6ActiveTask(project,w);if(!task)return html;const events=[...liveOps.events].filter(e=>qa6EventMatchesTask(e,task.id)),route=[...events].reverse().find(e=>{const p=qa6Payload(e);return p.candidate_id||p.candidate_kind;});if(!route)return html;const p=qa6Payload(route),target=[p.candidate_kind,p.candidate_id].filter(Boolean).join(' · ');return html.replace('<div class="qa6-follow-events">',`<div class="qa7-follow-target"><span>Execution target</span><strong>${escapeHtml(target||'OnePane-managed')}</strong></div><div class="qa6-follow-events">`);};

pages.settings.title='Settings';


/* === Alpha 3.1 canonical product layer === */
const A31_LAYOUT_COLUMNS=12;
const A31_LAYOUT_ROW_PX=44;
let a31LayoutSaveInFlight=0;
let a31LayoutGeneration=0;
let a31ModelView="local";
let a31AgentView="profiles";
let a31SkillsView="installed";
let a31SettingsView="general";
let a31OperationsView="overview";
let a31ControlTab="assistant";
let a31AssistantThreadID="";
let a31InstallPoll=null;

pages.skills={title:"Skills",icon:"✦"};
pages.nodes={title:"Nodes",icon:"⬡"};
navItems.splice(0,navItems.length,
  ["operations","▣","Operations"],["tasks","☑","Tasks"],["projects","▢","Projects"],
  ["models","◇","Models"],["nodes","⬡","Nodes"],["agents","♙","Agents"],["skills","✦","Skills"]
);

function a31Array(v){return Array.isArray(v)?v:[]}
function a31Object(v){return v&&typeof v==="object"&&!Array.isArray(v)?v:{}}
function a31JSON(v,fallback={}){if(v&&typeof v==="object")return v;try{return JSON.parse(v||"{}")}catch{return fallback}}
function a31RouteIs(route){return currentTab()?.route===route}
function a31SetModelView(view){a31ModelView=view==="cloud"?"cloud":"local";if(a31RouteIs("models"))renderModels();renderNav()}
function a31SetAgentView(view){a31AgentView=view;if(a31RouteIs("agents"))renderAgents()}
function a31SetSkillsView(view){a31SkillsView=view;if(a31RouteIs("skills"))renderSkills()}
function a31SetOperationsView(view){a31OperationsView=view;if(a31RouteIs("operations"))renderOperations()}
function a31SetSettingsView(view){a31SettingsView=view;if(a31RouteIs("settings"))renderSettings()}
function a31CurrentProject(){return typeof qa4ActiveProject==="function"?qa4ActiveProject():null}
function a31CurrentWorkspace(){return typeof qa4ActiveWorkspace==="function"?qa4ActiveWorkspace():null}

function renderNav(){
  const route=currentTab()?.route||"operations",projects=a31Array(qa4ProjectHub?.projects);
  const projectTree=projects.length?`<div class="project-nav-tree">${projects.map(p=>{const projectActive=route==="projects"&&p.id===qa4ProjectHub.activeProjectID,workspaces=typeof qa4Workspaces==="function"?qa4Workspaces(p):[];return `<div class="project-nav-node ${projectActive&&!qa4ProjectHub.activeWorkspaceID?'active-project':''}"><button class="project-nav-project ${projectActive&&!qa4ProjectHub.activeWorkspaceID?'active':''}" data-a31-project-nav="${escapeHtml(p.id)}"><span>▢</span><span>${escapeHtml(p.name||"Project")}</span></button><div class="project-nav-workspaces">${workspaces.map(w=>`<button class="project-nav-workspace ${projectActive&&w.id===qa4ProjectHub.activeWorkspaceID?'active':''}" data-a31-project-nav="${escapeHtml(p.id)}" data-a31-workspace-nav="${escapeHtml(w.id)}"><span class="project-nav-branch"></span><span>${escapeHtml(w.name||"Workspace")}</span></button>`).join("")}</div></div>`}).join("")}</div>`:"";
  const modelTree=`<div class="model-nav-tree"><button class="model-nav-child ${route==="models"&&a31ModelView==="local"?'active':''}" data-a31-model-view="local"><span>◈</span><span>Local Models</span></button><button class="model-nav-child ${route==="models"&&a31ModelView==="cloud"?'active':''}" data-a31-model-view="cloud"><span>☁</span><span>Cloud Models</span></button></div>`;
  const html=navItems.map(([r,icon,label])=>{const translated=qa5T(r,label),hasLeaf=(r==="models"||r==="projects"),parentActive=route===r&&!hasLeaf;const row=`<button class="nav-item ${parentActive?'active':''}" data-route="${r}" title="${escapeHtml(translated)}"><span class="nav-icon">${icon}</span><span class="nav-label">${escapeHtml(translated)}</span></button>`;if(r==="projects")return row+projectTree;if(r==="models")return row+modelTree;return row}).join("");
  $("#primaryNav").innerHTML=html;
}

/* Shared deterministic component layout. */
function a31Constraints(item){
  const type=String(item.type||"");
  const minW=["chat","modelstack","settings"].includes(type)?4:3;
  const minH=type==="chat"?4:3;
  return {minW,minH,maxW:12,maxH:12};
}
function a31Overlap(a,b){return !(a.x+a.width<=b.x||b.x+b.width<=a.x||a.y+a.height<=b.y||b.y+b.height<=a.y)}
function a31NormalizeLayout(items){
  let cursorX=0,cursorY=0,rowH=0;
  for(const item of items){
    const c=a31Constraints(item);
    item.width=Math.max(c.minW,Math.min(c.maxW,Number(item.width||item.col||4)));
    item.height=Math.max(c.minH,Math.min(c.maxH,Number(item.height||item.row||4)));
    if(!Number.isFinite(Number(item.x))||!Number.isFinite(Number(item.y))){
      if(cursorX+item.width>A31_LAYOUT_COLUMNS){cursorX=0;cursorY+=Math.max(1,rowH);rowH=0}
      item.x=cursorX;item.y=cursorY;cursorX+=item.width;rowH=Math.max(rowH,item.height);
    }else{item.x=Math.max(0,Math.min(A31_LAYOUT_COLUMNS-item.width,Number(item.x)));item.y=Math.max(0,Number(item.y))}
    item.col=item.width;item.row=item.height;
  }
  return items;
}
function a31FirstFree(items,item,ignore){
  for(let y=0;y<120;y++)for(let x=0;x<=A31_LAYOUT_COLUMNS-item.width;x++){
    const probe={x,y,width:item.width,height:item.height};
    if(!items.some(o=>o!==item&&o.id!==ignore&&a31Overlap(probe,o)))return {x,y};
  }
  return {x:0,y:Math.max(0,...items.filter(x=>x!==item).map(x=>x.y+x.height),0)};
}
function a31ResolveLayout(items,anchorID){
  const anchor=items.find(x=>x.id===anchorID);
  const ordered=items.filter(x=>x!==anchor).sort((a,b)=>(a.y-b.y)||(a.x-b.x));
  const placed=anchor?[anchor]:[];
  for(const item of ordered){
    if(placed.some(p=>a31Overlap(item,p))){const slot=a31FirstFree(placed,item,null);item.x=slot.x;item.y=slot.y}
    placed.push(item);
  }
}
function a31GridStyle(item){return `grid-column:${Number(item.x)+1} / span ${item.width};grid-row:${Number(item.y)+1} / span ${item.height}`}
function a31ApplyLayout(root,items,attr,animate=false){
  const before=new Map();if(animate)$$(`[${attr}]`,root).forEach(el=>before.set(el.getAttribute(attr),el.getBoundingClientRect()));
  for(const item of items){const el=$(`[${attr}="${CSS.escape(item.id)}"]`,root);if(!el)continue;el.style.gridColumn=`${item.x+1} / span ${item.width}`;el.style.gridRow=`${item.y+1} / span ${item.height}`}
  if(animate&&Element.prototype.animate)requestAnimationFrame(()=>$$(`[${attr}]`,root).forEach(el=>{const old=before.get(el.getAttribute(attr));if(!old)return;const now=el.getBoundingClientRect(),dx=old.left-now.left,dy=old.top-now.top;if(Math.abs(dx)>1||Math.abs(dy)>1)el.animate([{transform:`translate(${dx}px,${dy}px)`},{transform:"translate(0,0)"}],{duration:170,easing:"ease-out"})}));
}
function a31ApplyItemLayout(root,item,attr){
  const el=$(`[${attr}="${CSS.escape(item.id)}"]`,root);if(!el)return;
  el.style.gridColumn=`${item.x+1} / span ${item.width}`;el.style.gridRow=`${item.y+1} / span ${item.height}`;
}
const A31_RESIZE_EDGES=['n','ne','e','se','s','sw','w','nw'];
function a31ResizeHandles(id,attr,label='component'){
  return A31_RESIZE_EDGES.map(edge=>`<button class="layout-resize-handle resize-${edge}" ${attr}="${escapeHtml(id)}" data-resize-edge="${edge}" title="Resize ${escapeHtml(label)}" aria-label="Resize ${escapeHtml(label)} ${edge}"></button>`).join('');
}
function a31ResizeRect(start,edge,dx,dy,c){
  let x=start.itemX,y=start.itemY,w=start.w,h=start.h;
  if(edge.includes('e'))w=Math.max(c.minW,Math.min(c.maxW,A31_LAYOUT_COLUMNS-x,start.w+dx));
  if(edge.includes('s'))h=Math.max(c.minH,Math.min(c.maxH,start.h+dy));
  if(edge.includes('w')){const shift=Math.max(-start.itemX,Math.min(start.w-c.minW,dx));x=start.itemX+shift;w=start.w-shift;if(w>c.maxW){x+=w-c.maxW;w=c.maxW}}
  if(edge.includes('n')){const shift=Math.max(-start.itemY,Math.min(start.h-c.minH,dy));y=start.itemY+shift;h=start.h-shift;if(h>c.maxH){y+=h-c.maxH;h=c.maxH}}
  return {x:Math.max(0,x),y:Math.max(0,y),width:w,height:h};
}
function a31BindLayout(root,items,{attr,dragAttr,resizeAttr,persist:save}){
  if(!root||isPhoneLayout())return;
  const begin=(e,id,kind)=>{
    if(e.button!==0)return;const item=items.find(x=>x.id===id);if(!item)return;
    e.preventDefault();e.stopPropagation();
    const snapshot=items.map(x=>({...x})),generation=++a31LayoutGeneration;
    const rect=root.getBoundingClientRect(),style=getComputedStyle(root),columnGap=parseFloat(style.columnGap)||0,rowGap=parseFloat(style.rowGap)||0;
    const colW=Math.max(1,(rect.width-columnGap*(A31_LAYOUT_COLUMNS-1))/A31_LAYOUT_COLUMNS),colStep=colW+columnGap,rowStep=A31_LAYOUT_ROW_PX+rowGap;
    const start={x:e.clientX,y:e.clientY,itemX:item.x,itemY:item.y,w:item.width,h:item.height};
    const target=e.currentTarget,card=target.closest(`[${attr}]`),edge=target.dataset.resizeEdge||'se';let raf=0,pending=null,finished=false;
    target.setPointerCapture?.(e.pointerId);root.classList.add('layout-interacting');root.dataset.layoutGeneration=String(generation);if(card)card.dataset.layoutActive='true';
    const render=()=>{raf=0;if(!pending)return;Object.assign(item,pending);item.col=item.width;item.row=item.height;pending=null;a31ApplyItemLayout(root,item,attr)};
    const queue=next=>{pending=next;if(!raf)raf=requestAnimationFrame(render)};
    const move=ev=>{const dx=Math.round((ev.clientX-start.x)/colStep),dy=Math.round((ev.clientY-start.y)/rowStep),c=a31Constraints(item);if(kind==='drag')queue({x:Math.max(0,Math.min(A31_LAYOUT_COLUMNS-item.width,start.itemX+dx)),y:Math.max(0,start.itemY+dy),width:item.width,height:item.height});else queue(a31ResizeRect(start,edge,dx,dy,c))};
    const restore=()=>{for(const old of snapshot){const current=items.find(x=>x.id===old.id);if(current)Object.assign(current,old)}a31ApplyLayout(root,items,attr,true)};
    const finish=async cancelled=>{if(finished)return;finished=true;if(raf){cancelAnimationFrame(raf);raf=0}if(cancelled)pending={x:start.itemX,y:start.itemY,width:start.w,height:start.h};if(pending)render();try{if(target.hasPointerCapture?.(e.pointerId))target.releasePointerCapture(e.pointerId)}catch{}target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',up);target.removeEventListener('pointercancel',cancel);root.classList.remove('layout-interacting');if(card)delete card.dataset.layoutActive;
      if(cancelled){restore();return}
      a31ResolveLayout(items,item.id);a31ApplyLayout(root,items,attr,true);root.dataset.layoutSaving='true';a31LayoutSaveInFlight++;
      try{await save?.();root.dataset.layoutSaved='true';setTimeout(()=>{if(root.isConnected)delete root.dataset.layoutSaved},900)}
      catch(ex){restore();notice('Layout save failed: '+ex.message,'bad')}
      finally{a31LayoutSaveInFlight=Math.max(0,a31LayoutSaveInFlight-1);delete root.dataset.layoutSaving}
    };
    const up=()=>finish(false),cancel=()=>finish(true);
    target.addEventListener('pointermove',move);target.addEventListener('pointerup',up);target.addEventListener('pointercancel',cancel);
  };
  $$('['+dragAttr+']',root).forEach(h=>h.onpointerdown=e=>begin(e,h.getAttribute(dragAttr),'drag'));
  $$('['+resizeAttr+']',root).forEach(h=>h.onpointerdown=e=>begin(e,h.getAttribute(resizeAttr),'resize'));
}

/* Operations */

/* Operations */
function a31OperationsTabs(){
  const tabs=[["overview","Overview"],["activity","Activity"],["logs","Logs"],["health","Health"],["nodes","Nodes"],["providers","Providers"],["recovery","Recovery"]];
  return `<div class="subtabs operations-subtabs">${tabs.map(([v,l])=>`<button class="subtab ${a31OperationsView===v?'active':''}" data-a31-ops="${v}">${l}</button>`).join("")}</div>`;
}
function a31BindOperationsTabs(){
  $$("[data-a31-ops]").forEach(b=>b.onclick=()=>{const v=b.dataset.a31Ops;if(v==="logs"){a31ToggleLogs();return}if(v==="nodes"){openRoute("nodes");return}if(v==="providers"){a31SetModelView("cloud");openRoute("models");return}a31SetOperationsView(v)});
}
function a31ToggleLogs(){
  const drawer=$("#bottomDrawer");if(!drawer)return;
  if(drawer.dataset.state==="open"&&activeDrawerTab==="logs"){setDrawerOpen(false);return}
  activeDrawerTab="logs";setDrawerOpen(true);renderDrawer();if(a31RouteIs("operations"))renderOperations();
}
function a31OpsWidget(w){
  return `<section class="dashboard-widget a31-layout-item ${state.operationsEdit?'editable':''}" data-op-widget="${escapeHtml(w.id)}" style="${a31GridStyle(w)}"><div class="dashboard-edit-bar ${state.operationsEdit?'':'hidden'}"><button class="dashboard-drag" data-op-drag="${escapeHtml(w.id)}" title="Drag component">⋮⋮</button><strong>${escapeHtml(w.title)}</strong><span class="dashboard-edit-spacer"></span><select class="dashboard-size" data-op-preset="${escapeHtml(w.id)}"><option value="">Size…</option><option value="small">Small</option><option value="medium">Medium</option><option value="large">Large</option><option value="wide">Wide</option><option value="full">Full</option></select><button class="tiny danger" data-op-remove="${escapeHtml(w.id)}">×</button></div><div class="dashboard-widget-content">${operationsComponentContent(w.type)}</div>${state.operationsEdit?a31ResizeHandles(w.id,"data-op-resize",w.title):""}</section>`
}
function a31RenderOperationsGrid(){
  const root=$("#operationsLayout");if(!root)return;a31NormalizeLayout(state.operationsWidgets);root.innerHTML=state.operationsWidgets.map(a31OpsWidget).join("");
  if(state.operationsEdit){
    a31BindLayout(root,state.operationsWidgets,{attr:"data-op-widget",dragAttr:"data-op-drag",resizeAttr:"data-op-resize",persist:async()=>persist()});
    $$("[data-op-remove]",root).forEach(b=>b.onclick=async()=>{const el=b.closest("[data-op-widget]");if(el?.animate)await el.animate([{opacity:1,transform:"scale(1)"},{opacity:0,transform:"scale(.96)"}],{duration:130}).finished.catch(()=>{});state.operationsWidgets=state.operationsWidgets.filter(x=>x.id!==b.dataset.opRemove);a31NormalizeLayout(state.operationsWidgets);persist();a31RenderOperationsGrid()});
    $$("[data-op-preset]",root).forEach(sel=>sel.onchange=()=>{if(!sel.value)return;const item=state.operationsWidgets.find(x=>x.id===sel.dataset.opPreset);if(!item)return;const p=operationsSizePreset(sel.value,item);item.width=p.col;item.height=p.row;item.col=p.col;item.row=p.row;a31ResolveLayout(state.operationsWidgets,item.id);a31ApplyLayout(root,state.operationsWidgets,"data-op-widget",true);persist()});
  }
  bindViewActions(root);
}
openOperationsComponentPicker=function(){
  const catalogue=[["metrics","System metrics"],["tasks","Task list"],["scheduled","Scheduled tasks"],["nodes","Nodes"],["resources","Resource Utilisation"],["activity","Recent Activity"],["attention","Attention"],["providers","Cloud Provider Health"]],used=new Set(state.operationsWidgets.map(x=>x.type)),available=catalogue.filter(([t])=>!used.has(t));
  openModal("Add Operations component",available.length?`<div class="component-picker-grid">${available.map(([type,title])=>`<button class="component-choice" data-a31-add-op="${type}"><strong>${escapeHtml(title)}</strong><span>Add ${escapeHtml(title.toLowerCase())} to Operations</span></button>`).join("")}</div>`:'<div class="empty-state compact">All Operations components are already present.</div>');
  $$("[data-a31-add-op]").forEach(b=>b.onclick=()=>{const [type,title]=catalogue.find(x=>x[0]===b.dataset.a31AddOp),item={id:`op-${type}-${Date.now().toString(36)}`,title,type,width:type==="metrics"?12:4,height:type==="metrics"?3:5};a31NormalizeLayout(state.operationsWidgets);const slot=a31FirstFree(state.operationsWidgets,item,null);item.x=slot.x;item.y=slot.y;item.col=item.width;item.row=item.height;state.operationsWidgets.push(item);persist();closeModal();a31RenderOperationsGrid()});
};
function a31OperationsActivity(){const rows=a31Array(liveOps.events).slice(-100).reverse();return `<section class="panel-card"><div class="card-header"><div><div class="card-title">Activity</div><div class="list-meta">Recent control-plane, scheduler, model, node and provider events.</div></div></div><div class="activity-stream">${rows.length?rows.map(e=>`<div class="activity-row"><span class="mono-cell">${escapeHtml(String(e.sequence||""))}</span><div><strong>${escapeHtml(eventLabel(e))}</strong><div class="list-meta">${escapeHtml([e.aggregate_type,e.aggregate_id].filter(Boolean).join(" · "))}</div></div><span class="list-meta">${e.occurred_at?new Date(Number(e.occurred_at)).toLocaleTimeString():""}</span></div>`).join(""):'<div class="empty-state compact">No activity recorded yet.</div>'}</div></section>`}
function a31OperationsHealth(){
  const active=a31Array(liveOps.tasks).filter(t=>!["complete","cancelled","failed"].includes(String(t.state||"").toLowerCase()));
  const nodes=a31Array(liveOps.nodes),providers=a31Array(liveOps.providers);
  const badNodes=nodes.filter(n=>["offline","failed","unavailable","stale"].includes(String(n.status||n.state||"").toLowerCase()));
  const badProviders=providers.filter(p=>!["connected","ready"].includes(String(p.status||"").toLowerCase()));
  const controlState=!liveOpsReported("health")||liveOps.health==="unknown"?"Unknown":liveOps.health==="ok"?"Healthy":"Degraded";
  const taskValue=liveOpsReported("tasks")?String(active.length):"Not reported";
  const nodeValue=!liveOpsReported("nodes")?"Not reported":nodes.length?`${nodes.length-badNodes.length}/${nodes.length} healthy`:"No nodes";
  const providerValue=!liveOpsReported("providers")?"Not reported":providers.length?`${providers.length-badProviders.length}/${providers.length} healthy`:"No providers";
  return `<div class="health-grid">${metric("Control plane",controlState,controlState==="Healthy"?"API responding":controlState==="Degraded"?`Reported status: ${liveOps.health}`:"Health endpoint not reported",controlState==="Healthy"?"good":controlState==="Degraded"?"bad":"")}${metric("Active tasks",taskValue,liveOpsReported("tasks")?"Scheduler workload":"Task feed unavailable")}${metric("Nodes",nodeValue,!liveOpsReported("nodes")?"Node feed unavailable":badNodes.length?"Review degraded nodes":nodes.length?"All observed nodes healthy":"No node health entries returned",liveOpsReported("nodes")&&nodes.length&&!badNodes.length?"good":badNodes.length?"warn":"")}${metric("Providers",providerValue,!liveOpsReported("providers")?"Provider feed unavailable":badProviders.length?"Provider attention required":providers.length?"Connections healthy":"No provider health entries returned",liveOpsReported("providers")&&providers.length&&!badProviders.length?"good":badProviders.length?"warn":"")}</div><section class="panel-card"><div class="widget-body"><strong>Health boundaries</strong><p class="page-subtitle">Managed component failures, unavailable nodes and provider problems remain isolated from the OnePane control plane. Use Recovery for actionable degraded items.</p></div></section>`;
}
async function a31RecoveryContent(){
  let comps={};try{comps=await qa5ModelComponents()}catch{}
  const failed=a31Array(liveOps.tasks).filter(t=>["failed","blocked"].includes(String(t.state||"").toLowerCase()));
  const compRows=Object.values(a31Object(comps)).filter(x=>["failed","degraded","interrupted"].includes(String(x.state||"").toLowerCase()));
  return `<section class="panel-card"><div class="card-header recovery-card-header"><div><div class="card-title">Recovery</div><div class="list-meta">Actionable degraded state only; OnePane does not reset healthy components.</div></div><button class="btn" id="a31RecoveryRefresh">Refresh health</button></div><div class="widget-body"><div class="recovery-list">${compRows.map(c=>`<div class="recovery-row"><div><strong>${escapeHtml(c.display_name||c.id)}</strong><div class="list-meta">${escapeHtml(c.last_error||c.state||"degraded")}</div></div><button class="btn" data-a31-repair-component="${escapeHtml(c.id)}">Repair</button></div>`).join("")}${failed.map(t=>`<div class="recovery-row"><div><strong>Task ${escapeHtml(t.id||"")}</strong><div class="list-meta">${escapeHtml(t.objective||t.state||"")}</div></div><button class="btn" data-route="tasks">Open Tasks</button></div>`).join("")}${!compRows.length&&!failed.length?'<div class="empty-state compact">No degraded components or failed/blocked tasks require recovery.</div>':""}</div></div></section>`
}
renderOperations=async function(){
  const actions=`<button class="btn ${state.operationsEdit?'primary':''}" id="editOperations">${state.operationsEdit?'Done':'Edit layout'}</button>${state.operationsEdit?'<button class="btn" id="addOperationsComponent">Add component</button><button class="btn" id="resetOperationsLayout">Reset layout</button>':""}`;
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Operations","System overview, activity, health and recovery",actions)}${a31OperationsTabs()}<div id="a31OperationsBody"></div></section>`;
  a31BindOperationsTabs();
  const body=$("#a31OperationsBody");
  if(a31OperationsView==="overview"){body.innerHTML=`<div class="operations-layout-grid ${state.operationsEdit?'editing':''}" id="operationsLayout"></div>`;a31RenderOperationsGrid()}
  else if(a31OperationsView==="activity")body.innerHTML=a31OperationsActivity();
  else if(a31OperationsView==="health")body.innerHTML=a31OperationsHealth();
  else if(a31OperationsView==="recovery"){body.innerHTML=await a31RecoveryContent();$("#a31RecoveryRefresh")?.addEventListener("click",async()=>{await refreshOperationalDataQA(true);renderOperations()});$$("[data-a31-repair-component]").forEach(b=>b.onclick=()=>a31ComponentAction(b.dataset.a31RepairComponent,"repair"))}
  $("#editOperations")?.addEventListener("click",()=>{state.operationsEdit=!state.operationsEdit;persist();renderOperations()});
  $("#resetOperationsLayout")?.addEventListener("click",()=>{state.operationsWidgets=defaultState().operationsWidgets;a31NormalizeLayout(state.operationsWidgets);persist();renderOperations()});
  $("#addOperationsComponent")?.addEventListener("click",openOperationsComponentPicker);
  bindViewActions($("#viewHost"));
}

/* Projects: preserve durable project/workspace services, replace layout interaction. */
qa4RenderWorkspaceWidget=function(w,project,workspace){
  a31NormalizeLayout(workspace.widgets||[]);
  const edit=!!state.projectWorkspaceEdit;
  const controls=edit?`<div class="dashboard-edit-bar"><button class="dashboard-drag" data-pw-drag="${escapeHtml(w.id)}" title="Drag component">⋮⋮</button><strong>${escapeHtml(w.title||w.type)}</strong><span class="dashboard-edit-spacer"></span><select class="dashboard-size" data-pw-preset="${escapeHtml(w.id)}"><option value="">Size…</option><option value="small">Small</option><option value="medium">Medium</option><option value="large">Large</option><option value="wide">Wide</option><option value="full">Full</option></select><button class="tiny danger" data-pw-remove="${escapeHtml(w.id)}">×</button></div>`:"";
  return `<section class="workspace-widget dashboard-widget a31-layout-item ${edit?'editable':''}" data-pw-widget="${escapeHtml(w.id)}" style="${a31GridStyle(w)}">${controls}<div class="dashboard-widget-content"><div class="widget-handle"><strong>${escapeHtml(w.title||w.type)}</strong></div>${qa6ComponentContent(w,project,workspace)}</div>${edit?a31ResizeHandles(w.id,"data-pw-resize",w.title||w.type):""}</section>`
};
async function a31RefreshProjectGrid(project,workspace,animate=true){
  const root=$("#qa4WorkspaceGrid");if(!root)return;a31NormalizeLayout(workspace.widgets||[]);
  const before=new Map();if(animate)$$("[data-pw-widget]",root).forEach(el=>before.set(el.dataset.pwWidget,el.getBoundingClientRect()));
  root.innerHTML=(workspace.widgets||[]).map(w=>qa4RenderWorkspaceWidget(w,project,workspace)).join("");
  if(animate&&Element.prototype.animate)requestAnimationFrame(()=>$$("[data-pw-widget]",root).forEach(el=>{const old=before.get(el.dataset.pwWidget);if(!old){el.animate([{opacity:0,transform:"scale(.97)"},{opacity:1,transform:"scale(1)"}],{duration:150});return}const n=el.getBoundingClientRect(),dx=old.left-n.left,dy=old.top-n.top;if(Math.abs(dx)>1||Math.abs(dy)>1)el.animate([{transform:`translate(${dx}px,${dy}px)`},{transform:"translate(0,0)"}],{duration:170,easing:"ease-out"})}));
  qa4BindWorkspaceEdit(project,workspace);qa6BindProjectComponents(project,workspace);qa7BindWorkspaceControls(project,workspace,root);qa4BindProjectNotes(root);
}
qa4BindWorkspaceEdit=function(project,workspace){
  if(!state.projectWorkspaceEdit)return;const root=$("#qa4WorkspaceGrid");if(!root)return;a31NormalizeLayout(workspace.widgets||[]);
  a31BindLayout(root,workspace.widgets||[],{attr:"data-pw-widget",dragAttr:"data-pw-drag",resizeAttr:"data-pw-resize",persist:async()=>{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project))}});
  $$("[data-pw-preset]",root).forEach(sel=>sel.onchange=async()=>{if(!sel.value)return;const w=workspace.widgets.find(x=>x.id===sel.dataset.pwPreset);if(!w)return;const p=workspacePreset(sel.value,w);w.width=p.col;w.height=p.row;w.col=p.col;w.row=p.row;a31ResolveLayout(workspace.widgets,w.id);a31ApplyLayout(root,workspace.widgets,"data-pw-widget",true);a31LayoutSaveInFlight++;try{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project))}finally{a31LayoutSaveInFlight=Math.max(0,a31LayoutSaveInFlight-1)}});
  $$("[data-pw-remove]",root).forEach(b=>b.onclick=async()=>{const el=b.closest("[data-pw-widget]");if(el?.animate)await el.animate([{opacity:1},{opacity:0,transform:"scale(.96)"}],{duration:130}).finished.catch(()=>{});workspace.widgets=workspace.widgets.filter(x=>x.id!==b.dataset.pwRemove);a31NormalizeLayout(workspace.widgets);await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));a31RefreshProjectGrid(project,workspace,true)});
};
qa4AddWorkspaceComponent=function(project,workspace){
  const used=new Set((workspace.widgets||[]).map(x=>x.type)),available=qa4WorkspaceCatalogue().filter(([t])=>!used.has(t)||["notes","chat"].includes(t));
  openModal("Add workspace component",`<div class="component-picker-grid">${available.map(([t,title])=>`<button class="component-choice" data-a31-add-project-component="${t}"><strong>${escapeHtml(title)}</strong><span>Add to ${escapeHtml(workspace.name)}</span></button>`).join("")}</div>`);
  $$("[data-a31-add-project-component]").forEach(b=>b.onclick=async()=>{const [type,title]=qa4WorkspaceCatalogue().find(x=>x[0]===b.dataset.a31AddProjectComponent),item={id:`pw-${type}-${Date.now().toString(36)}`,type,title,width:type==="tasks"||type==="scheduled"?6:4,height:type==="chat"?5:4};workspace.widgets=workspace.widgets||[];a31NormalizeLayout(workspace.widgets);const slot=a31FirstFree(workspace.widgets,item,null);item.x=slot.x;item.y=slot.y;item.col=item.width;item.row=item.height;workspace.widgets.push(item);await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));closeModal();await a31RefreshProjectGrid(project,workspace,true)});
};
const a31ProjectRenderBase=qa6RenderProjectsBase;
renderProjects=async function(){
  const epoch=qa31ViewEpoch;await a31ProjectRenderBase();if(epoch!==qa31ViewEpoch||!a31RouteIs("projects"))return;
  const project=a31CurrentProject(),workspace=project?a31CurrentWorkspace():null;if(!project||!workspace)return;qa7NormalizeWorkspace(project,workspace);a31NormalizeLayout(workspace.widgets||[]);
  $(".workspace-chat-panel")?.remove();const bar=$(".workspace-context-bar .list-meta");if(bar)bar.textContent=` · workspace sandbox ${workspace.sandbox.internet?'internet allowed':'internet blocked'} · ${workspace.routing.enabled!==false?'routing enabled':'single-path'} · ${titleCase(workspace.orchestration?.mode||'direct')}`;
  const settings=$("#qa4WorkspaceSettings");if(settings){settings.textContent="Workspace settings";settings.onclick=()=>qa6OpenInInspector(project,workspace,"settings")}
  qa4BindWorkspaceEdit(project,workspace);qa6BindProjectComponents(project,workspace);qa7BindWorkspaceControls(project,workspace,$("#qa4WorkspaceGrid")||document);qa6StartEventStream();renderNav();
};

/* Managed components */
async function a31ComponentAction(id,action,statusSelector){
  const status=statusSelector?$(statusSelector):null;if(status)status.textContent=`${titleCase(action)} queued…`;
  try{const job=await apiRequest(`/v1/local-ai/components/${encodeURIComponent(id)}/${encodeURIComponent(action)}`,{method:"POST",body:"{}"});for(let i=0;i<180;i++){const j=await apiRequest(`/v1/local-ai/component-jobs/${encodeURIComponent(job.id)}`);if(status)status.textContent=`${titleCase(j.stage||j.status||action)}…`;if(["succeeded","failed","interrupted"].includes(j.status)){if(j.status!=="succeeded")throw new Error(j.failure_reason||`${id} ${action} failed`);notice(`${titleCase(id)} ${action} complete.`);if(a31RouteIs("models"))renderModels();return}await new Promise(r=>setTimeout(r,600))}throw new Error("component job timed out")}catch(ex){if(status)status.innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`;notice(ex.message,"bad")}
}
function a31ComponentButtons(id,c){
  const st=String(c?.state||"not_installed"),installed=!!c?.installed;
  if(!installed)return `<button class="btn primary" data-a31-component="${id}:install">Install</button>`;
  const running=st==="running";
  return `<button class="btn" data-a31-component="${id}:${running?'disable':'enable'}">${running?'Disable':'Enable'}</button><button class="btn" data-a31-component="${id}:update">Update</button><button class="btn" data-a31-component="${id}:repair">Repair</button><button class="btn danger" data-a31-component="${id}:remove">Remove</button>`
}
function a31BindComponentButtons(root=document){$$("[data-a31-component]",root).forEach(b=>b.onclick=()=>{const [id,action]=b.dataset.a31Component.split(":");a31ComponentAction(id,action,`#a31-${id}-status`)})}

/* Models */
function a31ModelTabs(){return `<div class="models-section-tabs"><button class="subtab ${a31ModelView==='local'?'active':''}" data-a31-model-tab="local">Local Models</button><button class="subtab ${a31ModelView==='cloud'?'active':''}" data-a31-model-tab="cloud">Cloud Models</button></div>`}
function a31PlacementLabel(d){const p=d?.placement||{},m=String(p.mode||"").toLowerCase();if(m==="cpu_only")return "CPU";if(m==="cpu_offload")return "HYBRID";if(a31Array(p.devices).some(x=>x.kind==="accelerator"))return "GPU";return "AUTO"}
async function a31OpenCompute(dep){
  if(!localProfileQA){try{localProfileQA=await apiRequest("/v1/local-ai/detect",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})})}catch(ex){notice(ex.message,"bad");return}}
  const gpus=a31Array(localProfileQA?.gpus);
  let current={preference:"auto",placement_mode:"auto",required_device_ids:[]};try{current=await apiRequest(`/v1/local-ai/deployments/${encodeURIComponent(dep.deployment_id)}/compute-policy?workspace_id=${encodeURIComponent(onepaneWorkspace)}`)}catch{}
  openModal("Compute placement",`<form id="a31ComputeForm" class="qa-form"><p class="page-subtitle">Change placement without redownloading the model. A resident runtime is stopped and restarted automatically.</p><label>Compute placement<select name="preference"><option value="auto">Auto</option><option value="prefer_gpu">Prefer GPU</option><option value="prefer_cpu">Prefer CPU</option><option value="require_gpu">Require GPU</option><option value="require_cpu">Require CPU</option><option value="hybrid">Hybrid / CPU + GPU</option></select></label><label>GPU device<select name="device"><option value="">Automatic</option>${gpus.map((g,i)=>`<option value="${escapeHtml(g.device_id||`gpu-${g.device_index??i}`)}">${escapeHtml(g.name||`GPU ${i}`)} · ${bytesQA(g.vram_bytes||0)}</option>`).join("")}</select></label><label>Advanced placement<select name="mode"><option value="auto">Automatic</option><option value="single_device">Single device</option><option value="layer_sharded">Layer split</option><option value="row_sharded">Row split</option><option value="tensor_sharded">Tensor split</option><option value="cpu_offload">CPU offload</option><option value="cpu_only">CPU only</option></select></label><button class="btn primary">Apply placement</button><div class="page-subtitle" id="a31ComputeStatus"></div></form>`);
  const form=$("#a31ComputeForm");form.elements.preference.value=current.preference||"auto";form.elements.mode.value=current.placement_mode||"auto";if(a31Array(current.required_device_ids).length)form.elements.device.value=current.required_device_ids[0];
  form.onsubmit=async e=>{e.preventDefault();const pref=form.elements.preference.value,device=form.elements.device.value,required=pref==="require_gpu"&&device?[device]:[],preferred=pref==="prefer_gpu"&&device?[device]:[];$("#a31ComputeStatus").textContent="Applying…";try{await apiRequest(`/v1/local-ai/deployments/${encodeURIComponent(dep.deployment_id)}/compute-policy`,{method:"PATCH",body:JSON.stringify({workspace_id:onepaneWorkspace,preference:pref,placement_mode:form.elements.mode.value,preferred_device_ids:preferred,required_device_ids:required})});notice("Compute placement updated.");closeModal();renderModels()}catch(ex){$("#a31ComputeStatus").innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`}}
}
async function a31InstallModel(model){
  if(!model.installable){notice(model.install_reason||"This model is advisory only; no verified artifact is available.","bad");return}
  if(!localProfileQA){try{localProfileQA=await apiRequest("/v1/local-ai/detect",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})})}catch(ex){notice(ex.message,"bad");return}}
  const qs=a31Array(model.installable_quantizations),defaultQ=qs[0]||a31Array(model.quantizations)[0]||"";
  openModal("Download & Install",`<form id="a31InstallModelForm" class="qa-form"><strong>${escapeHtml(model.display_name||model.model_ref)}</strong><p class="page-subtitle">OnePane will resolve and verify the managed llama.cpp runtime and model artifact automatically.</p><label>Quantization<select name="quantization">${qs.map(q=>`<option>${escapeHtml(q)}</option>`).join("")}</select></label><label>Compute<select name="compute"><option value="auto">Auto</option><option value="prefer_gpu">Prefer GPU</option><option value="prefer_cpu">Prefer CPU</option><option value="require_gpu">Require GPU</option><option value="require_cpu">Require CPU</option><option value="hybrid">Hybrid / CPU + GPU</option></select></label><button class="btn primary">Download & Install</button><div class="install-progress" id="a31InstallProgress"></div></form>`);
  const form=$("#a31InstallModelForm");form.elements.quantization.value=defaultQ;form.onsubmit=async e=>{e.preventDefault();const box=$("#a31InstallProgress");box.innerHTML='<div class="install-step active">Resolving trusted catalogue…</div>';try{const job=await apiRequest("/v1/local-ai/install-jobs",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,profile_id:localProfileQA.id,role_name:"local-managed",use_case:"general",context_tokens:8192,model_ref:model.model_ref,quantization:form.elements.quantization.value,compute_preference:form.elements.compute.value})});for(let i=0;i<1200;i++){const j=await apiRequest(`/v1/local-ai/install-jobs/${encodeURIComponent(job.id)}`),stage=String(j.stage||j.status||"queued").replaceAll("_"," ");box.innerHTML=`<div class="install-step active">${escapeHtml(titleCase(stage))}</div><div class="list-meta">${Math.round(Number(j.progress_pct||0))}%</div>`;if(["succeeded","failed","cancelled"].includes(String(j.status))){if(j.status!=="succeeded")throw new Error(j.failure_reason||"Install failed");notice("Model installed and registered.");closeModal();renderModels();return}await new Promise(r=>setTimeout(r,800))}}catch(ex){box.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}}
}
async function a31RenderLocalModels(){
  const root=$("#a31ModelsRoot");let catalog=[],deployments=[],components={};
  try{[catalog,deployments,components]=await Promise.all([apiRequest("/v1/local-ai/catalog"),qa5LoadManagedDeployments(),qa5ModelComponents().catch(()=>({}))])}catch(ex){root.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
  const colibri=components.colibri||{};
  root.innerHTML=`<div class="models-single-column"><section class="panel-card local-models-main"><div class="card-header models-card-header"><div><div class="card-title">Local Models</div><div class="list-meta">Hardware-aware trusted model installation and per-deployment CPU/GPU placement.</div></div><button class="btn primary a31-detect-large" id="detectLocal">⚙ Detect Hardware</button></div><div class="a31-local-content"><div id="localResult" class="model-result">${localProfileQA?`<div class="hardware-summary"><strong>${escapeHtml(localProfileQA.cpu?.name||"CPU")}</strong><br>${a31Array(localProfileQA.gpus).map(g=>`${escapeHtml(g.name||"GPU")} · ${bytesQA(g.vram_bytes||0)} VRAM`).join("<br>")||"No GPU detected"}<br>${bytesQA(localProfileQA.memory?.total_bytes||0)} RAM</div>`:'<div class="page-subtitle">Detect this machine to calculate safe placements.</div>'}</div><div class="subsection-title">Installed / registered</div><div class="model-tile-scroll">${deployments.length?deployments.map(d=>`<article class="model-tile"><div><strong>${escapeHtml(d.display_name||d.model_ref)}</strong><div class="list-meta">${escapeHtml(d.quantization||"")} · ${escapeHtml(d.runtime_name||d.runtime_backend||"managed")} · ${escapeHtml(d.status||"unknown")}</div><div class="list-meta"><span class="pill">${a31PlacementLabel(d)}</span> · admission ${escapeHtml(d.admission_status||"pending")}</div></div><div class="toolbar"><button class="btn" data-a31-compute="${escapeHtml(d.deployment_id)}">Compute</button><button class="btn" data-a31-model-spec="${escapeHtml(d.deployment_id)}">Spec sheet</button><button class="btn primary" data-a31-agent-check="${escapeHtml(d.deployment_id)}">Agent Check</button></div></article>`).join(""):'<div class="empty-state compact">No managed local models yet.</div>'}</div><div class="subsection-title">Available catalogue</div><input id="a31LocalFilter" class="catalogue-filter" placeholder="Filter local models…"><div id="a31CatalogRows" class="model-tile-scroll"></div></div></section><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Managed Local Runtime</div><div class="list-meta">OnePane installs the verified llama.cpp CPU/CUDA/Vulkan runtime automatically when required.</div></div><span class="pill good">Managed automatically</span></div><div class="widget-body"><p>No separate llama.cpp installation is required. CUDA runtime libraries are downloaded and verified automatically. Runtime artifacts are selected from the trusted catalogue to match placement and hardware.</p></div></section><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Colibri Large Model</div><div class="list-meta">Optional large-model backend; lifecycle is managed independently.</div></div><span class="pill ${["running","installed_disabled"].includes(String(colibri.state))?'good':''}">${escapeHtml(titleCase(String(colibri.state||"not installed").replaceAll("_"," ")))}</span></div><div class="widget-body"><div class="toolbar">${a31ComponentButtons("colibri",colibri)}${colibri.installed?'<button class="btn" id="a31ColibriRegister">Register model folder</button>':""}</div><div id="a31-colibri-status" class="page-subtitle">${escapeHtml(colibri.last_error||"")}</div></div></section></div>`;
  $("#detectLocal").onclick=async()=>{const b=$("#localResult");b.textContent="Detecting…";try{localProfileQA=await apiRequest("/v1/local-ai/detect",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});renderModels()}catch(ex){b.innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`}};
  const drawCatalog=()=>{const q=($("#a31LocalFilter").value||"").toLowerCase(),rows=catalog.filter(m=>JSON.stringify(m).toLowerCase().includes(q));$("#a31CatalogRows").innerHTML=rows.map((m,i)=>`<article class="model-tile"><div><strong>${escapeHtml(m.display_name||m.model_ref)}</strong><div class="list-meta">${escapeHtml(String(m.parameter_count||m.parameter_scale||"—"))} · ${formatContextQA(m.max_context_tokens||m.context_tokens)} context</div><div class="list-meta">${m.installable?`Trusted · ${escapeHtml(a31Array(m.installable_quantizations).join(", "))}`:`Advisory · ${escapeHtml(m.install_reason||"No trusted artifact")}`}</div></div><button class="btn ${m.installable?'primary':''}" data-a31-install-model="${i}" ${m.installable?'':'disabled'}>${m.installable?'Download & Install':'Unavailable'}</button></article>`).join("")||'<div class="empty-state compact">No models match.</div>';$$("[data-a31-install-model]").forEach(b=>b.onclick=()=>a31InstallModel(rows[Number(b.dataset.a31InstallModel)]))};
  $("#a31LocalFilter").oninput=drawCatalog;drawCatalog();a31BindComponentButtons();$("#a31ColibriRegister")?.addEventListener("click",qa5RegisterColibri);
  $$("[data-a31-compute]").forEach(b=>b.onclick=()=>a31OpenCompute(deployments.find(d=>d.deployment_id===b.dataset.a31Compute)));
  $$("[data-a31-model-spec]").forEach(b=>b.onclick=()=>qa5InspectModel(deployments.find(d=>d.deployment_id===b.dataset.a31ModelSpec)));
  $$("[data-a31-agent-check]").forEach(b=>b.onclick=()=>qa5AgentCheck(deployments.find(d=>d.deployment_id===b.dataset.a31AgentCheck)));
}
async function a31StartOAuth(preset){
  const callback=`${location.origin}/v1/provider-oauth/callback`;
  try{const out=await apiRequest(`/v1/provider-oauth/${encodeURIComponent(preset)}/start`,{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,redirect_uri:callback})});const win=window.open(out.authorization_url,"onepane-oauth","width=720,height=780");if(!win)throw new Error("Browser blocked the OAuth window.");const listener=e=>{if(e.origin!==location.origin||e.data?.type!=="onepane-oauth-complete")return;window.removeEventListener("message",listener);notice("OAuth account connected.");renderModels()};window.addEventListener("message",listener)}catch(ex){notice(ex.message,"bad")}
}
async function a31RenderCloudModels(){
  const root=$("#a31ModelsRoot"),qs=encodeURIComponent(onepaneWorkspace);let presets=[],providers=[],components={},oauthCfg=[],oauthConn=[];
  try{[presets,providers,components,oauthCfg,oauthConn]=await Promise.all([providerPresetsQA(),apiRequest(`/v1/providers?workspace_id=${qs}`),qa5ModelComponents().catch(()=>({})),apiRequest(`/v1/provider-oauth/configs?workspace_id=${qs}`).catch(()=>[]),apiRequest(`/v1/provider-oauth/connections?workspace_id=${qs}`).catch(()=>[])])}catch(ex){root.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
  const omni=components.omniroute||{},cloud=a31Array(presets).filter(p=>p.id!=="omniroute"),providerRows=a31Array(providers);
  root.innerHTML=`<div class="models-single-column"><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Cloud Providers</div><div class="list-meta">Direct provider connections. OAuth tokens and API keys remain Vault-backed.</div></div><button class="btn" id="a31ApiKeys">API Keys</button></div><div class="provider-tile-grid">${cloud.map(p=>{const connected=providerRows.find(x=>String(x.provider).toLowerCase()===String(p.id).toLowerCase()&&x.status!=="revoked"),oauth=String(p.auth_type||"").includes("oauth"),cfg=a31Array(oauthCfg).find(x=>x.preset_id===p.id&&x.enabled),oc=a31Array(oauthConn).find(x=>x.preset_id===p.id&&x.status==="connected");return `<article class="provider-tile"><div class="provider-tile-head"><strong>${escapeHtml(p.display_name||p.id)}</strong><span class="pill ${connected||oc?'good':''}">${oc?'OAuth connected':connected?'Connected':oauth?(cfg?'OAuth ready':'OAuth not configured'):'Available'}</span></div><p>${escapeHtml(p.description||"")}</p><div class="provider-meta"><span>${oauth?'OAuth2 / PKCE':escapeHtml(p.auth_type||"API key")}</span><span>${escapeHtml(p.cost_hint||"")}</span></div><div class="toolbar">${oc?`<button class="btn danger" data-a31-oauth-revoke="${escapeHtml(oc.id)}">Revoke</button>`:connected?`<button class="btn danger" data-a31-provider-revoke="${escapeHtml(connected.id)}">Revoke</button>`:oauth?(cfg?`<button class="btn primary" data-a31-oauth-start="${escapeHtml(p.id)}">Connect account</button>`:'<button class="btn" data-route="settings">Configure OAuth</button>'):`<button class="btn primary" data-a31-provider-connect="${escapeHtml(p.id)}">Connect</button>`}</div></article>`}).join("")}</div></section><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">OmniRoute · Managed Local</div><div class="list-meta">OnePane-owned headless gateway on 127.0.0.1:20128. Independent of external OmniRoute connections.</div></div><span class="pill ${omni.state==="running"?'good':''}">${escapeHtml(titleCase(String(omni.state||"not installed").replaceAll("_"," ")))}</span></div><div class="widget-body"><div class="toolbar">${a31ComponentButtons("omniroute",omni)}</div><div id="a31-omniroute-status" class="page-subtitle">${escapeHtml(omni.last_error||"")}</div></div></section><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">OmniRoute · External Gateway</div><div class="list-meta">Connect an existing local or trusted remote OmniRoute instance.</div></div></div><div class="widget-body omni-provider-layout"><label>Gateway URL<input id="omniUrl" value="${escapeHtml(localStorage.getItem('onepane:omniroute-url')||'http://127.0.0.1:20128/v1')}"></label><label>Gateway credential<select id="omniCredential"><option value="">No gateway credential</option></select></label><label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label><div class="toolbar"><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" disabled>Connect / Reconnect</button></div><div id="omniResult" class="page-subtitle">Probe before connecting.</div></div></section></div>`;
  $("#a31ApiKeys").onclick=()=>openRoute("secrets");a31BindComponentButtons();$("#omniProbe").onclick=()=>omniQA(false);$("#omniConnect").onclick=()=>omniQA(true);populateOmniCredentials();
  $$("[data-a31-provider-connect]").forEach(b=>b.onclick=()=>qa4ConnectCloudProvider(b.dataset.a31ProviderConnect));$$("[data-a31-provider-revoke]").forEach(b=>b.onclick=()=>qa4RevokeProvider(b.dataset.a31ProviderRevoke));$$("[data-a31-oauth-start]").forEach(b=>b.onclick=()=>a31StartOAuth(b.dataset.a31OauthStart));$$("[data-a31-oauth-revoke]").forEach(b=>b.onclick=async()=>{try{await apiRequest(`/v1/provider-oauth/connections/${encodeURIComponent(b.dataset.a31OauthRevoke)}/revoke`,{method:"POST",body:"{}"});renderModels()}catch(ex){notice(ex.message,"bad")}});
}
renderModels=async function(){
  const epoch=qa31ViewEpoch;$("#viewHost").innerHTML=`<section class="page models-page">${pageHeader("Models",a31ModelView==="local"?"Local model installation, qualification and compute placement.":"Cloud providers, OAuth and OmniRoute routing.",'<button class="btn" id="a31ModelSettings">Settings</button>')}${a31ModelTabs()}<div id="a31ModelsRoot"><div class="widget-body">Loading…</div></div></section>`;
  $("#a31ModelSettings").onclick=()=>openRoute("settings");$$("[data-a31-model-tab]").forEach(b=>b.onclick=()=>a31SetModelView(b.dataset.a31ModelTab));
  if(a31ModelView==="local")await a31RenderLocalModels();else await a31RenderCloudModels();if(epoch!==qa31ViewEpoch||!a31RouteIs("models"))return;bindViewActions($("#viewHost"));renderNav();
};

/* Nodes */
renderNodes=async function(){
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Nodes","Enrolled machines are schedulable CPU/GPU resource pools.",'<button class="btn primary" id="a31AddNode">Add Node</button>')}<div id="a31Nodes"><div class="widget-body">Loading nodes…</div></div></section>`;
  try{const rows=await apiRequest("/v1/nodes");$("#a31Nodes").innerHTML=`<div class="node-grid">${a31Array(rows).map(n=>{const id=n.id||n.node_id,name=n.display_name||id,state=n.status||n.state||"unknown";return `<article class="panel-card node-card" data-a31-node="${escapeHtml(id)}"><div class="card-header"><div><div class="card-title">${escapeHtml(name)}</div><div class="list-meta">${escapeHtml(n.os_name||n.os||"")} ${escapeHtml(n.architecture||"")}</div></div><span class="pill ${["online","ready","active"].includes(String(state).toLowerCase())?'good':''}">${escapeHtml(titleCase(state))}</span></div><div class="widget-body"><dl class="definition-grid"><dt>CPU</dt><dd>${escapeHtml(n.cpu_name||n.cpu||"Detected by node")}</dd><dt>GPU</dt><dd>${escapeHtml(n.gpu_name||n.compute||"See capabilities")}</dd><dt>Last seen</dt><dd>${escapeHtml(String(n.last_seen_at||n.last_seen||"—"))}</dd></dl><div class="toolbar"><button class="btn" data-a31-node-cap="${escapeHtml(id)}">Capabilities</button><button class="btn" data-a31-node-models="${escapeHtml(id)}">Model management</button><button class="btn danger" data-a31-node-revoke="${escapeHtml(id)}">Revoke</button></div></div></article>`}).join("")||'<div class="empty-state">No enrolled nodes yet.</div>'}</div>`;
    $$("[data-a31-node-cap]").forEach(b=>b.onclick=async()=>{try{const x=await apiRequest(`/v1/nodes/${encodeURIComponent(b.dataset.a31NodeCap)}/capabilities`);openModal("Node capabilities",`<pre class="json-preview">${escapeHtml(JSON.stringify(x,null,2))}</pre>`)}catch(ex){notice(ex.message,"bad")}});
    $$("[data-a31-node-models]").forEach(b=>b.onclick=async()=>{try{const x=await apiRequest(`/v1/nodes/${encodeURIComponent(b.dataset.a31NodeModels)}/model-management`);openModal("Node model management",`<pre class="json-preview">${escapeHtml(JSON.stringify(x,null,2))}</pre>`)}catch(ex){notice(ex.message,"bad")}});
    $$("[data-a31-node-revoke]").forEach(b=>b.onclick=async()=>{if(!confirm("Revoke this node?"))return;try{await apiRequest(`/v1/nodes/${encodeURIComponent(b.dataset.a31NodeRevoke)}/revoke`,{method:"POST",body:"{}"});renderNodes()}catch(ex){notice(ex.message,"bad")}});
  }catch(ex){$("#a31Nodes").innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}
  $("#a31AddNode").onclick=()=>openModal("Add Node",'<div class="widget-body"><p>Start OnePane on the target Windows or Ubuntu node, then pair its node ID here.</p><button class="btn" data-route="nodes">Refresh discovered nodes</button></div>');bindViewActions($("#viewHost"));renderNav();
};

/* Agents, Teams, Research */
function a31ProfileMeta(p){return a31JSON(p.metadata,{})}
async function a31DuplicateProfile(p){
  openModal("Duplicate Agent Profile",`<form id="a31DuplicateProfile" class="qa-form"><label>Name<input name="name" value="${escapeHtml((p.name||"Profile")+" - Custom")}"></label><label>Instructions<textarea name="instructions" rows="9">${escapeHtml(p.instructions_md||"")}</textarea></label><button class="btn primary">Create custom profile</button></form>`);
  $("#a31DuplicateProfile").onsubmit=async e=>{e.preventDefault();const fd=new FormData(e.currentTarget),meta=a31ProfileMeta(p);try{await apiRequest("/v1/agent-profiles",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,name:fd.get("name"),description:p.description||"",instructions_md:fd.get("instructions"),default_role:p.default_role||"",capability_id:p.capability_id||"inference.general",protocol_level:p.protocol_level||"L1",metadata:{...meta,based_on:p.id}})});closeModal();renderAgents()}catch(ex){notice(ex.message,"bad")}}
}
async function a31CreateTeamPreset(preset){
  const cfg=a31JSON(preset.configuration??preset.Configuration,{}),presetName=preset.name||preset.Name,presetDescription=preset.description||preset.Description||"";try{const t=await apiRequest("/v1/teams",{method:"POST",body:JSON.stringify({WorkspaceID:onepaneWorkspace,Name:presetName,Purpose:presetDescription})});const teamID=t.id||t.ID,teamRevision=t.revision||t.Revision;for(const [i,seat] of a31Array(cfg.seats).entries()){const [name,profile]=seat;await apiRequest(`/v1/teams/${encodeURIComponent(teamID)}/members`,{method:"POST",body:JSON.stringify({member_kind:"agent",display_name:name,role_name:name,capability_id:"inference.general",protocol_level:"L2",route_policy:{},config:{agent_profile_id:profile},ordinal:i})})}await apiRequest(`/v1/teams/${encodeURIComponent(teamID)}/configuration`,{method:"PATCH",body:JSON.stringify({expected_revision:teamRevision,configuration:cfg})});notice(`${presetName} team created.`);renderAgents()}catch(ex){notice(ex.message,"bad")}
}
async function a31EditTeam(team){
  const teamID=team.id||team.ID,teamRevision=team.revision||team.Revision,cfg=a31JSON(team.configuration??team.Configuration,{}),r={pin_models:true,disable_model_substitution:true,same_model_retries:true,preserve_failed_seats:true,independent_first_pass:true,scoped_evidence:true,record_raw_outputs:true,full_provenance:true,require_all_seats:true,anonymized_cross_critique:true,synthesis_pass:true,critique_rounds:2,...a31Object(cfg.research)},critiqueRounds=Math.max(1,Math.min(5,Number(r.critique_rounds||2))),researchFlags=Object.entries(r).filter(([k])=>!["critique_rounds","synthesis_member_id"].includes(k));
  let members=[];try{members=await apiRequest(`/v1/teams/${encodeURIComponent(teamID)}/members`)}catch{}
  openModal("Team Configuration",`<form id="a31TeamConfig" class="qa-form"><div class="team-member-list">${a31Array(members).map(m=>`<div class="team-member-row"><strong>${escapeHtml(m.display_name||m.DisplayName||"Agent")}</strong><span>${escapeHtml(a31JSON(m.config??m.Config,{}).agent_profile_id||m.role_name||m.RoleName||"Agent")}</span></div>`).join("")}</div><label class="inline-check research-toggle"><input type="checkbox" name="research_mode" ${cfg.research_mode?'checked':''}> Research mode</label><div class="research-options">${researchFlags.map(([k,v])=>`<label class="inline-check"><input type="checkbox" data-research-key="${k}" ${v?'checked':''}> ${escapeHtml(titleCase(k.replaceAll("_"," ")))}</label>`).join("")}<label>Critique rounds<input type="number" id="a32CritiqueRounds" min="1" max="5" step="1" value="${critiqueRounds}"></label></div><p class="page-subtitle">Automatic Research flow: independent pass → configured anonymised critique rounds → final synthesis. Provider/rate-limit failures pause the current round without substitution; successful seat outputs are preserved and the same round can be retried manually.</p><button class="btn primary">Save Team configuration</button></form>`);
  $("#a31TeamConfig").onsubmit=async e=>{e.preventDefault();const research={};$$("[data-research-key]",e.currentTarget).forEach(x=>research[x.dataset.researchKey]=x.checked);research.critique_rounds=Math.max(1,Math.min(5,Number($("#a32CritiqueRounds")?.value||2)));const next={...cfg,research_mode:e.currentTarget.elements.research_mode.checked,research};try{await apiRequest(`/v1/teams/${encodeURIComponent(teamID)}/configuration`,{method:"PATCH",body:JSON.stringify({expected_revision:teamRevision,configuration:next})});closeModal();renderAgents()}catch(ex){notice(ex.message,"bad")}}
}
renderAgents=async function(){
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Agents","Reusable Profiles, Sessions, Teams and Councils with policy-scoped capabilities.")}<div class="subtabs"><button class="subtab ${a31AgentView==='profiles'?'active':''}" data-a31-agent-tab="profiles">Profiles</button><button class="subtab ${a31AgentView==='teams'?'active':''}" data-a31-agent-tab="teams">Teams</button><button class="subtab ${a31AgentView==='sessions'?'active':''}" data-a31-agent-tab="sessions">Sessions</button><button class="subtab ${a31AgentView==='councils'?'active':''}" data-a31-agent-tab="councils">Councils</button></div><div id="a31AgentsBody"><div class="widget-body">Loading…</div></div></section>`;$$("[data-a31-agent-tab]").forEach(b=>b.onclick=()=>a31SetAgentView(b.dataset.a31AgentTab));const body=$("#a31AgentsBody");
  try{
    if(a31AgentView==="profiles"){const [profiles,bundles]=await Promise.all([apiRequest(`/v1/agent-profiles?workspace_id=${encodeURIComponent(onepaneWorkspace)}`),apiRequest(`/v1/skills/tool-bundles?workspace_id=${encodeURIComponent(onepaneWorkspace)}`).catch(()=>[])]);body.innerHTML=`<div class="agent-profile-grid">${a31Array(profiles).map((p,i)=>{const m=a31ProfileMeta(p),pbs=a31Array(m.recommended_tool_bundles);return `<article class="panel-card agent-profile-card"><div class="card-header"><div><div class="card-title">${escapeHtml(p.name)}</div><div class="list-meta">${escapeHtml(titleCase(m.category||p.default_role||"general"))} · ${escapeHtml(p.protocol_level||"L1")}</div></div><span class="pill ${p.source_kind==='builtin'?'good':''}">${p.source_kind==='builtin'?'DigiLogic Core':'Custom'}</span></div><div class="widget-body"><p>${escapeHtml(p.description||"")}</p><div class="tool-chip-row">${pbs.map(x=>`<span class="tool-chip">${escapeHtml(x)}</span>`).join("")}</div><div class="toolbar"><button class="btn" data-a31-profile-view="${i}">View</button>${p.source_kind==='builtin'?`<button class="btn primary" data-a31-profile-duplicate="${i}">Duplicate & customise</button>`:""}</div></div></article>`}).join("")}</div>`;$$("[data-a31-profile-view]").forEach(b=>b.onclick=()=>{const p=profiles[Number(b.dataset.a31ProfileView)];openModal(p.name,`<div class="widget-body"><p>${escapeHtml(p.description||"")}</p><pre class="text-preview">${escapeHtml(p.instructions_md||"")}</pre></div>`)});$$("[data-a31-profile-duplicate]").forEach(b=>b.onclick=()=>a31DuplicateProfile(profiles[Number(b.dataset.a31ProfileDuplicate)]))}
    else if(a31AgentView==="teams"){const [teams,presets]=await Promise.all([apiRequest(`/v1/teams?workspace_id=${encodeURIComponent(onepaneWorkspace)}`),apiRequest(`/v1/team-presets?workspace_id=${encodeURIComponent(onepaneWorkspace)}`)]);body.innerHTML=`<section class="panel-card"><div class="card-header"><div><div class="card-title">Team templates</div><div class="list-meta">Create a legitimate Team with profile-backed agent seats.</div></div></div><div class="team-preset-grid">${a31Array(presets).map((p,i)=>`<article class="team-preset"><strong>${escapeHtml(p.name||p.Name)}</strong><span>${escapeHtml(p.description||p.Description||"")}</span><button class="btn" data-a31-team-preset="${i}">Create Team</button></article>`).join("")}</div></section><section class="panel-card"><div class="card-header"><div class="card-title">Configured Teams</div></div><div class="team-list">${a31Array(teams).map((t,i)=>{const cfg=a31JSON(t.configuration??t.Configuration,{}),name=t.name||t.Name,purpose=t.purpose||t.Purpose||"";return `<div class="team-row"><div><strong>${escapeHtml(name)}</strong><div class="list-meta">${escapeHtml(purpose)} ${cfg.research_mode?'· Research mode':''}</div></div><span class="pill ${cfg.research_mode?'good':''}">${cfg.research_mode?'Research':'Standard'}</span><button class="btn" data-a31-team-edit="${i}">Configure</button></div>`}).join("")||'<div class="empty-state compact">No Teams configured yet.</div>'}</div></section>`;$$("[data-a31-team-preset]").forEach(b=>b.onclick=()=>a31CreateTeamPreset(presets[Number(b.dataset.a31TeamPreset)]));$$("[data-a31-team-edit]").forEach(b=>b.onclick=()=>a31EditTeam(teams[Number(b.dataset.a31TeamEdit)]))}
    else if(a31AgentView==="sessions"){const rows=await apiRequest(`/v1/agent-sessions?workspace_id=${encodeURIComponent(onepaneWorkspace)}`);body.innerHTML=`<div class="table-shell"><table class="data-table"><thead><tr><th>Task</th><th>Mode</th><th>Profile</th><th>State</th></tr></thead><tbody>${a31Array(rows).map(x=>`<tr><td>${escapeHtml(x.task_id||"")}</td><td>${escapeHtml(x.execution_mode||"")}</td><td>${escapeHtml(x.profile_id||"")}</td><td>${escapeHtml(x.state||"")}</td></tr>`).join("")}</tbody></table></div>`}
    else body.innerHTML='<section class="panel-card"><div class="widget-body"><strong>Councils</strong><p>Council execution remains Workspace-owned. Research mode adds pinned-model, independent-pass and provenance constraints when selected from a Research-capable Team/Council configuration.</p><button class="btn" data-route="projects">Configure in Workspace</button></div></section>';
  }catch(ex){body.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}bindViewActions($("#viewHost"));renderNav();
};

/* Skills */
async function a31UploadSkill(){
  const input=document.createElement("input");input.type="file";input.accept=".opskill,.zip";input.onchange=async()=>{const file=input.files?.[0];if(!file)return;const fd=new FormData();fd.append("workspace_id",onepaneWorkspace);fd.append("package",file,file.name);try{const res=await fetch("/v1/skills/packages/upload",{method:"POST",body:fd,credentials:"same-origin"});if(!res.ok){let e;try{e=await res.json()}catch{}throw new Error(e?.error||`Upload failed (${res.status})`)}notice("Skill package quarantined for review.");renderSkills()}catch(ex){notice(ex.message,"bad")}};input.click();
}
renderSkills=async function(){
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Skills","Reusable capability packages, tool dependencies and assignments.",'<button class="btn primary" id="a31UploadSkill">Upload Skill</button>')}<div class="subtabs"><button class="subtab ${a31SkillsView==='installed'?'active':''}" data-a31-skills-tab="installed">Installed</button><button class="subtab ${a31SkillsView==='catalogue'?'active':''}" data-a31-skills-tab="catalogue">Catalogue</button><button class="subtab ${a31SkillsView==='assignments'?'active':''}" data-a31-skills-tab="assignments">Assignments</button><button class="subtab ${a31SkillsView==='matrix'?'active':''}" data-a31-skills-tab="matrix">Capability Matrix</button><button class="subtab ${a31SkillsView==='packages'?'active':''}" data-a31-skills-tab="packages">Packages</button></div><div id="a31SkillsBody"><div class="widget-body">Loading…</div></div></section>`;$("#a31UploadSkill").onclick=a31UploadSkill;$$("[data-a31-skills-tab]").forEach(b=>b.onclick=()=>a31SetSkillsView(b.dataset.a31SkillsTab));const body=$("#a31SkillsBody"),qs=encodeURIComponent(onepaneWorkspace);
  try{const [packages,bundles,assignments,profiles,deployments]=await Promise.all([apiRequest(`/v1/skills/packages?workspace_id=${qs}`),apiRequest(`/v1/skills/tool-bundles?workspace_id=${qs}`),apiRequest(`/v1/skills/assignments?workspace_id=${qs}`),apiRequest(`/v1/agent-profiles?workspace_id=${qs}`).catch(()=>[]),qa5LoadManagedDeployments().catch(()=>[])]),pkgs=a31Array(packages);
    if(a31SkillsView==="installed"||a31SkillsView==="catalogue"){const rows=a31SkillsView==="installed"?pkgs.filter(p=>p.status==="installed"):pkgs;body.innerHTML=`<div class="skill-grid">${rows.map((p,i)=>{const m=a31JSON(p.manifest,{});return `<article class="panel-card skill-card"><div class="card-header"><div><div class="card-title">${escapeHtml(p.name)}</div><div class="list-meta">${escapeHtml(p.version)} · ${escapeHtml(p.publisher||"Unknown publisher")}</div></div><span class="pill ${p.trust_state==='builtin'||p.trust_state==='verified'?'good':''}">${escapeHtml(titleCase(p.trust_state||"unverified"))}</span></div><div class="widget-body"><p>${escapeHtml(m.description||m.type||"Capability pack")}</p><div class="tool-chip-row">${a31Array(m.tool_bundles).map(x=>`<span class="tool-chip">${escapeHtml(x)}</span>`).join("")}</div><div class="toolbar">${p.status==="quarantined"?`<button class="btn primary" data-a31-skill-install="${i}">Install</button>`:""}${p.status==="installed"&&p.source_kind!=="builtin"?`<button class="btn" data-a31-skill-status="${i}:disabled">Disable</button>`:""}${p.status==="disabled"?`<button class="btn" data-a31-skill-status="${i}:installed">Enable</button>`:""}</div></div></article>`}).join("")||'<div class="empty-state">No Skills in this view.</div>'}</div>`;$$("[data-a31-skill-install]").forEach(b=>b.onclick=async()=>{const p=rows[Number(b.dataset.a31SkillInstall)];try{await apiRequest(`/v1/skills/packages/${encodeURIComponent(p.id)}/install?workspace_id=${qs}`,{method:"POST",body:"{}"});renderSkills()}catch(ex){notice(ex.message,"bad")}});$$("[data-a31-skill-status]").forEach(b=>b.onclick=async()=>{const [i,status]=b.dataset.a31SkillStatus.split(":"),p=rows[Number(i)];try{await apiRequest(`/v1/skills/packages/${encodeURIComponent(p.id)}?workspace_id=${qs}`,{method:"PATCH",body:JSON.stringify({status})});renderSkills()}catch(ex){notice(ex.message,"bad")}})}
    else if(a31SkillsView==="assignments"){body.innerHTML=`<section class="panel-card"><div class="card-header"><div><div class="card-title">Assignments</div><div class="list-meta">Assigned does not imply permitted; Workspace policy remains authoritative.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Skill package</th><th>Subject</th><th>State</th></tr></thead><tbody>${a31Array(assignments).map(a=>`<tr><td>${escapeHtml(a.skill_package_id)}</td><td>${escapeHtml(a.subject_kind)} · ${escapeHtml(a.subject_id)}</td><td>${a.enabled?'Assigned':'Disabled'}</td></tr>`).join("")}</tbody></table></div></section>`}
    else if(a31SkillsView==="matrix"){
      const packageMap=new Map(pkgs.map(p=>[String(p.id),p])),profileMap=new Map(a31Array(profiles).map(p=>[String(p.id),p])),bundleMap=new Map(a31Array(bundles).map(b=>[String(b.ID||b.id||b.Name||b.name),b]));
      const rows=a31Array(assignments).filter(a=>a.enabled).map(a=>{const pkg=packageMap.get(String(a.skill_package_id)),manifest=a31JSON(pkg?.manifest,{}),bundleIDs=a31Array(manifest.tool_bundles),tools=bundleIDs.flatMap(id=>a31Array(bundleMap.get(String(id))?.Tools||bundleMap.get(String(id))?.tools)),profile=String(a.subject_kind)==="agent_profile"?profileMap.get(String(a.subject_id)):null,meta=profile?a31ProfileMeta(profile):{},model=meta.model_ref||meta.model||meta.default_model||profile?.model_ref||(String(a.subject_kind)==="workspace"?"Workspace routing":String(a.subject_kind).startsWith("team")?"Team/Council pinned at run":"Auto / policy-routed");return {subject:`${a.subject_kind} · ${a.subject_id}`,model,skill:pkg?`${pkg.name} ${pkg.version}`:a.skill_package_id,bundles:bundleIDs.join(", ")||"—",tools:[...new Set(tools)].join(", ")||"Resolved by bundle/policy"}});
      body.innerHTML=`<section class="panel-card capability-matrix"><div class="card-header"><div><div class="card-title">Effective Capability Matrix</div><div class="list-meta">Skills are assigned to governed scopes; models inherit capability through Agent/Team/Workspace routing rather than owning authority directly.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Subject</th><th>Model / routing</th><th>Skill</th><th>Tool bundles</th><th>Effective tools</th></tr></thead><tbody>${rows.length?rows.map(r=>`<tr><td>${escapeHtml(r.subject)}</td><td>${escapeHtml(r.model)}</td><td>${escapeHtml(r.skill)}</td><td>${escapeHtml(r.bundles)}</td><td>${escapeHtml(r.tools)}</td></tr>`).join(""):'<tr><td colspan="5" class="muted-cell">No enabled Skill assignments yet.</td></tr>'}</tbody></table></div></section><section class="panel-card"><div class="card-header"><div><div class="card-title">Known model deployments</div><div class="list-meta">Use this alongside the matrix to see which runtime/model candidates are currently available to routed Agents.</div></div></div><div class="widget-body">${a31Array(deployments).length?a31Array(deployments).map(d=>`<div class="bundle-row"><strong>${escapeHtml(d.display_name||d.model_ref||d.deployment_id)}</strong><span>${escapeHtml(d.runtime_name||d.runtime_backend||"managed")} · ${escapeHtml(a31PlacementLabel(d))} · ${escapeHtml(d.status||"unknown")}</span></div>`).join(""):'No managed local model deployments are currently registered.'}</div></section>`;
    }
    else body.innerHTML=`<section class="panel-card"><div class="card-header"><div><div class="card-title">Packages</div><div class="list-meta">Uploaded packages remain quarantined until validation and explicit installation.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Name</th><th>Source</th><th>Trust</th><th>Status</th><th>SHA-256</th></tr></thead><tbody>${pkgs.map(p=>`<tr><td>${escapeHtml(p.name)} ${escapeHtml(p.version)}</td><td>${escapeHtml(p.source_kind)}</td><td>${escapeHtml(p.trust_state)}</td><td>${escapeHtml(p.status)}</td><td class="mono-cell">${escapeHtml(String(p.package_sha256).slice(0,16))}…</td></tr>`).join("")}</tbody></table></div></section><section class="panel-card"><div class="card-header"><div class="card-title">Tool Bundles</div></div><div class="widget-body">${a31Array(bundles).map(b=>`<div class="bundle-row"><strong>${escapeHtml(b.Name||b.name||b.ID||b.id)}</strong><span>${escapeHtml(a31Array(b.Tools||b.tools).join(", "))}</span></div>`).join("")}</div></section>`;
  }catch(ex){body.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}
};

/* Persistent Control Chat */
function a31SetControlChatCollapsed(collapsed){
  state.controlChatCollapsed=!!collapsed;persist();const panel=$("#controlChatPanel"),toggle=$("#controlChatToggle");if(panel)panel.dataset.collapsed=state.controlChatCollapsed?"true":"false";if(toggle){toggle.setAttribute("aria-expanded",state.controlChatCollapsed?"false":"true");toggle.title=state.controlChatCollapsed?"Expand OnePane Chat":"Collapse OnePane Chat"}
}
function a31OpenControlChat(tab=a31ControlTab){
  a31ControlTab=tab;const panel=$("#controlChatPanel");if(!panel)return;panel.dataset.state="open";panel.removeAttribute("hidden");a31SetControlChatCollapsed(!!state.controlChatCollapsed);a31RenderControlChat();
}
function a31CloseControlChat(){const p=$("#controlChatPanel");if(p){p.dataset.state="closed";p.setAttribute("hidden","")}}
async function a31EnsureAssistantThread(){
  if(a31AssistantThreadID)return a31AssistantThreadID;const rows=await apiRequest(`/v1/assistant/threads?workspace_id=${encodeURIComponent(onepaneWorkspace)}&limit=20`);const t=a31Array(rows)[0]||await apiRequest("/v1/assistant/threads",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,title:"OnePane Control Chat"})});a31AssistantThreadID=t.id;return t.id;
}
async function a31RenderControlChat(){
  const body=$("#controlChatBody");if(!body)return;const projects=a31Array(qa4ProjectHub?.projects),current=a31CurrentProject();
  $("#controlChatAssistantTab")?.classList.toggle("active",a31ControlTab==="assistant");$("#controlChatOrchestratorTab")?.classList.toggle("active",a31ControlTab==="orchestrator");
  if(a31ControlTab==="assistant"){body.innerHTML='<div class="control-chat-loading">Loading Assistant…</div>';try{const id=await a31EnsureAssistantThread(),turns=await apiRequest(`/v1/assistant/threads/${encodeURIComponent(id)}/turns?limit=100`);body.innerHTML=`<div class="control-chat-scope">Global OnePane Assistant</div><div class="control-chat-history">${a31Array(turns).map(t=>`<div class="control-chat-message ${t.role||t.author_kind||''}"><strong>${escapeHtml(t.role||t.author_kind||"OnePane")}</strong><span>${escapeHtml(t.content||t.text||"")}</span></div>`).join("")}</div><form id="a31ControlChatForm" class="control-chat-form"><textarea rows="3" placeholder="Ask OnePane…"></textarea><div><button class="btn">Ask</button><button class="btn primary" name="run" value="1">Run</button></div></form>`;$("#a31ControlChatForm").onsubmit=async e=>{e.preventDefault();const text=e.currentTarget.querySelector("textarea").value.trim();if(!text)return;const run=e.submitter?.value==="1";try{await apiRequest(`/v1/assistant/threads/${encodeURIComponent(id)}/turns`,{method:"POST",body:JSON.stringify({content:text,allow_task_creation:run,force_task:false})});a31RenderControlChat()}catch(ex){notice(ex.message,"bad")}}}catch(ex){body.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}}
  else{const pid=$("#controlChatProject")?.value||current?.id||projects[0]?.id;if(!pid){body.innerHTML='<div class="empty-state compact">Create a Project before using Project Orchestrator.</div>';return}const p=projects.find(x=>x.id===pid),workspaces=p?qa4Workspaces(p):[];body.innerHTML=`<div class="control-chat-selectors"><label>Project<select id="controlChatProject">${projects.map(x=>`<option value="${escapeHtml(x.id)}" ${x.id===pid?'selected':''}>${escapeHtml(x.name)}</option>`).join("")}</select></label><label>Workspace<select id="controlChatWorkspace"><option value="">Project-wide</option>${workspaces.map(w=>`<option value="${escapeHtml(w.id)}">${escapeHtml(w.name)}</option>`).join("")}</select></label></div><div id="a31OrchestratorTurns" class="control-chat-history">Loading…</div><form id="a31OrchestratorForm" class="control-chat-form"><textarea rows="3" placeholder="Ask the Project Orchestrator…"></textarea><div><button class="btn">Ask</button><button class="btn primary" name="run" value="1">Run</button></div></form>`;$("#controlChatProject").onchange=()=>a31RenderControlChat();try{const turns=await apiRequest(`/v1/projects/${encodeURIComponent(pid)}/orchestrator/turns?limit=100`);$("#a31OrchestratorTurns").innerHTML=a31Array(turns).map(t=>`<div class="control-chat-message ${t.role||''}"><strong>${escapeHtml(t.role||"OnePane")}</strong><span>${escapeHtml(t.content||t.objective||t.response||"")}</span></div>`).join("");$("#a31OrchestratorForm").onsubmit=async e=>{e.preventDefault();const objective=e.currentTarget.querySelector("textarea").value.trim(),run=e.submitter?.value==="1",ws=$("#controlChatWorkspace").value;if(!objective)return;try{await apiRequest(`/v1/projects/${encodeURIComponent(pid)}/orchestrator/turns`,{method:"POST",body:JSON.stringify({objective,project_workspace_id:ws,allow_task_creation:run,force_task:false})});a31RenderControlChat()}catch(ex){notice(ex.message,"bad")}}}catch(ex){$("#a31OrchestratorTurns").innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}}
}

/* Settings */
const A31_SETTINGS=[["general","General"],["appearance","Appearance"],["defaults","Defaults"],["models","Models & Compute"],["providers","Providers & Auth"],["nodes","Nodes & Federation"],["agents","Agents & Research"],["skills","Skills & Tools"],["security","Security & Approvals"],["updates","Updates & Diagnostics"]];
function a31SettingsNav(){return `<div class="settings-nav"><input id="a31SettingsSearch" placeholder="Search settings…">${A31_SETTINGS.map(([id,label])=>`<button class="${a31SettingsView===id?'active':''}" data-a31-settings="${id}" data-settings-search="${escapeHtml(label.toLowerCase())}">${escapeHtml(label)}</button>`).join("")}</div>`}
async function a31SettingsSection(){
  const p=qa5Prefs(),d=p.workspace_defaults||{};
  if(a31SettingsView==="general")return `<section class="settings-section"><h2>General</h2><label>Language<select id="a31Language">${Object.entries(qa5AllLanguages()).map(([id,x])=>`<option value="${escapeHtml(id)}" ${p.language===id?'selected':''}>${escapeHtml(x.name)}</option>`).join("")}</select></label><label>Landing page<select id="a31Landing"><option value="operations">Operations</option><option value="projects">Projects</option><option value="tasks">Tasks</option></select></label><button class="btn" data-action="product-tour">Restart product tour</button></section>`;
  if(a31SettingsView==="appearance")return `<section class="settings-section"><h2>Appearance</h2><div class="theme-grid">${qa5ThemeButtons()}</div><div class="toolbar"><button class="btn" id="a31InstallTheme">Install theme package</button></div></section>`;
  if(a31SettingsView==="defaults")return `<section class="settings-section"><h2>Defaults for new Workspaces</h2><p class="page-subtitle">Copied at Workspace creation only. Existing Workspaces are never changed here.</p><label>Default orchestration<select id="a31DefaultMode"><option value="direct">Direct</option><option value="team">Team</option><option value="council">Council</option></select></label><label>Default seats<input id="a31DefaultSeats" type="number" min="1" max="8" value="${Number(d.seats||2)}"></label><label class="inline-check"><input id="a31DefaultRouting" type="checkbox" ${d.model_routing!==false?'checked':''}> Enable model routing</label><label class="inline-check"><input id="a31DefaultRemote" type="checkbox" ${d.remote_models!==false?'checked':''}> Allow qualified remote models/nodes</label><label class="inline-check"><input id="a31DefaultBrowser" type="checkbox" ${d.browser?'checked':''}> Browser capability</label><label class="inline-check"><input id="a31DefaultComputer" type="checkbox" ${d.computer?'checked':''}> Computer capability</label><button class="btn primary" id="a31SaveDefaults">Save defaults</button></section>`;
  if(a31SettingsView==="models"){let cfg={};try{cfg=await apiRequest("/v1/settings/local-ai")}catch{}return `<section class="settings-section"><h2>Models & Compute</h2><label>Model pool path<input id="a31ModelPool" value="${escapeHtml(cfg.model_pool_path||"")}"></label><label>Default runtime strategy<select id="a31Runtime"><option value="auto">Auto</option><option value="hot-swap">Managed Hot Swap</option><option value="colibri">Colibri Large Model</option></select></label><label>Default compute<select id="a31ComputeDefault"><option value="auto">Auto</option><option value="prefer-gpu">Prefer GPU</option><option value="prefer-cpu">Prefer CPU</option></select></label><p class="page-subtitle">Per-deployment Require CPU/GPU/Hybrid settings are configured on Local Models and override these preferences.</p><button class="btn primary" id="a31SaveModelSettings">Save</button></section>`}
  if(a31SettingsView==="providers"){const cfg=await apiRequest(`/v1/provider-oauth/configs?workspace_id=${encodeURIComponent(onepaneWorkspace)}`).catch(()=>[]);return `<section class="settings-section"><h2>Providers & Auth</h2><p class="page-subtitle">OAuth is enabled only when a provider has a configured authorization endpoint, token endpoint, public client ID and scopes.</p><div class="oauth-config-list">${a31Array(cfg).map(x=>`<div class="oauth-config-row"><strong>${escapeHtml(x.preset_id)}</strong><span class="pill ${x.enabled?'good':''}">${x.enabled?'Enabled':'Disabled'}</span><span>${escapeHtml(x.authorization_url)}</span></div>`).join("")||'<div class="empty-state compact">No OAuth providers configured. API-key providers remain available from Cloud Models.</div>'}</div><button class="btn" id="a31AddOAuthConfig">Configure OAuth provider</button></section>`}
  if(a31SettingsView==="nodes")return '<section class="settings-section"><h2>Nodes & Federation</h2><label class="inline-check"><input type="checkbox" checked disabled> Node discovery enabled</label><label class="inline-check"><input type="checkbox" checked disabled> Remote inference enabled</label><p class="page-subtitle">Manage actual enrolled devices, scheduling pools and model caches on the Nodes page.</p><button class="btn" data-route="nodes">Manage Nodes</button></section>';
  if(a31SettingsView==="agents")return `<section class="settings-section"><h2>Agents & Research</h2><label>Assistant compute preference<select id="a31AssistantCompute"><option value="auto">Auto</option><option value="prefer-gpu">Prefer GPU</option><option value="prefer-cpu">Prefer CPU</option></select></label><label class="inline-check"><input id="a31ResearchDefault" type="checkbox" ${p.research_default?'checked':''}> Recommend Research mode for new Research Team templates</label><p class="page-subtitle">Research integrity is stored per Team/Council. Defaults never silently change existing Teams.</p><button class="btn primary" id="a31SaveAgentSettings">Save</button></section>`;
  if(a31SettingsView==="skills")return '<section class="settings-section"><h2>Skills & Tools</h2><p>Uploaded .opskill packages are quarantined, hashed and validated before explicit installation. Tool bundles request capability; Workspace policy remains authority.</p><button class="btn" data-route="skills">Manage Skills</button></section>';
  if(a31SettingsView==="security")return `<section class="settings-section"><h2>Security & Approvals</h2><div class="approval-profile-grid">${["high","medium","low"].map(v=>`<button class="approval-profile ${state.approvalLevel===v?'selected':''}" data-approval-default="${v}"><strong>${titleCase(v)}</strong><span>${v==="high"?"Prompt for every approval-class operation.":v==="medium"?"Auto-approve low risk; prompt for medium/high/critical.":"Auto-approve low and medium risk; prompt for high/critical."}</span></button>`).join("")}</div></section>`;
  return `<section class="settings-section"><h2>Updates & Diagnostics</h2><label>Release channel<select id="a31UpdateChannel"><option value="alpha">Alpha</option><option value="stable">Stable</option></select></label><div class="toolbar"><button class="btn" id="a31OpenLogs">Open Logs</button><button class="btn" data-action="product-tour">Restart product tour</button></div><p class="page-subtitle">Recovery actions that mutate runtimes live in Operations → Recovery.</p></section>`
}
renderSettings=async function(){
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Settings","Application preferences and defaults. Workspace-owned policy remains in each Workspace.")}<div class="settings-shell">${a31SettingsNav()}<div id="a31SettingsContent"><div class="widget-body">Loading…</div></div></div></section>`;
  $("#a31SettingsContent").innerHTML=await a31SettingsSection();$$("[data-a31-settings]").forEach(b=>b.onclick=()=>a31SetSettingsView(b.dataset.a31Settings));$("#a31SettingsSearch").oninput=e=>{const q=e.target.value.toLowerCase();$$("[data-settings-search]").forEach(b=>b.hidden=q&&!b.dataset.settingsSearch.includes(q))};
  $$("[data-settings-theme]").forEach(b=>b.onclick=()=>{applyTheme(b.dataset.settingsTheme);renderSettings()});$$("[data-approval-default]").forEach(b=>b.onclick=()=>{state.approvalLevel=b.dataset.approvalDefault;persist();renderSettings()});
  const p=qa5Prefs(),d=p.workspace_defaults||{};if($("#a31DefaultMode"))$("#a31DefaultMode").value=d.orchestration||"direct";if($("#a31Landing"))$("#a31Landing").value=p.landing||"operations";if($("#a31Runtime"))$("#a31Runtime").value=p.default_runtime||"auto";if($("#a31ComputeDefault"))$("#a31ComputeDefault").value=p.default_compute||"auto";if($("#a31AssistantCompute"))$("#a31AssistantCompute").value=p.assistant_defaults?.compute_preference||"auto";if($("#a31UpdateChannel"))$("#a31UpdateChannel").value=p.update_channel||"alpha";
  $("#a31SaveDefaults")?.addEventListener("click",()=>{qa5SavePrefs({workspace_defaults:{orchestration:$("#a31DefaultMode").value,seats:Number($("#a31DefaultSeats").value||2),model_routing:$("#a31DefaultRouting").checked,remote_models:$("#a31DefaultRemote").checked,browser:$("#a31DefaultBrowser").checked,computer:$("#a31DefaultComputer").checked}});notice("Defaults saved for newly created Workspaces.")});
  $("#a31SaveModelSettings")?.addEventListener("click",async()=>{try{await apiRequest("/v1/settings/local-ai",{method:"POST",body:JSON.stringify({model_pool_path:$("#a31ModelPool").value})});qa5SavePrefs({default_runtime:$("#a31Runtime").value,default_compute:$("#a31ComputeDefault").value});notice("Model defaults saved.")}catch(ex){notice(ex.message,"bad")}});
  $("#a31SaveAgentSettings")?.addEventListener("click",()=>{qa5SavePrefs({assistant_defaults:{...p.assistant_defaults,compute_preference:$("#a31AssistantCompute").value},research_default:$("#a31ResearchDefault").checked});notice("Agent and Research defaults saved.")});
  $("#a31Language")?.addEventListener("change",e=>{qa5SavePrefs({language:e.target.value});qa5ApplyAuthLanguage(e.target.value);qa5RefreshShellLanguage();renderSettings()});
  $("#a31InstallTheme")?.addEventListener("click",()=>{const i=document.createElement("input");i.type="file";i.accept=".json";i.onchange=async()=>{try{await qa5InstallThemePack(i.files[0]);renderSettings()}catch(ex){notice(ex.message,"bad")}};i.click()});
  $("#a31OpenLogs")?.addEventListener("click",a31ToggleLogs);$("#a31AddOAuthConfig")?.addEventListener("click",()=>openModal("Configure OAuth provider",`<form id="a31OAuthConfigForm" class="qa-form"><label>Provider preset ID<input name="preset" required placeholder="provider-id"></label><label>Authorization URL<input name="auth" required placeholder="https://…/authorize"></label><label>Token URL<input name="token" required placeholder="https://…/token"></label><label>Public client ID<input name="client" required></label><label>Scopes<input name="scopes" placeholder="openid profile"></label><label class="inline-check"><input name="enabled" type="checkbox" checked> Enabled</label><button class="btn primary">Save OAuth configuration</button></form>`));setTimeout(()=>{const form=$("#a31OAuthConfigForm");if(form)form.onsubmit=async e=>{e.preventDefault();const fd=new FormData(form),preset=String(fd.get("preset")||"").trim();try{await apiRequest(`/v1/provider-oauth/configs/${encodeURIComponent(preset)}`,{method:"PUT",body:JSON.stringify({workspace_id:onepaneWorkspace,authorization_url:fd.get("auth"),token_url:fd.get("token"),client_id:fd.get("client"),scopes:String(fd.get("scopes")||"").split(/\s+/).filter(Boolean),enabled:!!fd.get("enabled")})});closeModal();renderSettings()}catch(ex){notice(ex.message,"bad")}}},0);
  bindViewActions($("#viewHost"));
}

/* Product tour: crisp target, four-pane focus mask and stable anchored card. */
startProductTour=function({replay=false,welcome=false}={}){
  if(replay)localStorage.removeItem(TOUR_KEY);const root=$("#overlayRoot");if(!root)return;document.documentElement.dataset.productTour="active";
  const originalInspector=state.inspector,originalDrawer=state.drawer;
  const steps=[
    {title:"Welcome to OnePane",body:"OnePane coordinates Projects, Workspaces, models, Agents, Skills, Nodes and governed execution.",target:null},
    {title:"Navigation",body:"Primary management surfaces stay identifiable here, including Nodes and Skills.",target:".sidebar"},
    {title:"OnePane Control Chat",body:"Open persistent Assistant or Project Orchestrator chat without leaving your work.",target:"#controlChatLauncher"},
    {title:"Projects & Workspaces",body:"Projects contain independently configurable Workspaces and resizable components.",target:'[data-route="projects"]',prepare:()=>openRoute("projects")},
    {title:"Local Models",body:"Detect hardware, install trusted artifacts and pin each deployment to CPU, GPU or hybrid compute.",target:'[data-a31-model-view="local"]',prepare:()=>{openRoute("models");a31ModelView="local";renderNav()}},
    {title:"Cloud Models",body:"Connect API-key or OAuth providers and manage local/external OmniRoute.",target:'[data-a31-model-view="cloud"]',prepare:()=>{openRoute("models");a31ModelView="cloud";renderNav()}},
    {title:"Nodes",body:"Enrolled Windows and Ubuntu devices expose schedulable CPU/GPU resource pools.",target:'[data-route="nodes"]',prepare:()=>openRoute("nodes")},
    {title:"Agents & Research",body:"Core Profiles, Teams and Research integrity configuration live under Agents.",target:'[data-route="agents"]',prepare:()=>openRoute("agents")},
    {title:"Skills",body:"Install capability packages, inspect tool bundles and assignments.",target:'[data-route="skills"]',prepare:()=>openRoute("skills")},
    {title:"Inspector",body:"Selected Tasks, Models, Nodes and Workspaces expose contextual details here.",target:"#inspector",prepare:()=>setInspectorOpen(true)},
    {title:"Observability",body:"Logs, Events, Watchdog, Metrics, Evidence and Terminal remain in the bottom drawer.",target:"#bottomDrawer",prepare:()=>setDrawerOpen(true)},
    {title:"Settings",body:"Application defaults are grouped here; Workspace-owned settings remain with each Workspace.",target:'[data-route="settings"]'},
    {title:"Ready",body:"The control plane is ready. You can replay this tour from Settings or Help / Tour.",target:null}
  ];
  let i=0,lastAnchor="bottom-center",ended=false;
  const cleanup=()=>{
    if(ended)return;ended=true;
    delete document.documentElement.dataset.productTour;
    document.querySelectorAll(".tour-target").forEach(x=>x.classList.remove("tour-target"));
    root.innerHTML="";
    window.removeEventListener("resize",position);
    document.removeEventListener("keydown",onKeyDown);
    if(state.inspector!==originalInspector)setInspectorOpen(originalInspector==="open");
    if(state.drawer!==originalDrawer)setDrawerOpen(originalDrawer==="open");
  };
  const finish=()=>{localStorage.setItem(TOUR_KEY,TOUR_COMPLETE_VALUE);cleanup()};
  const onKeyDown=e=>{if(e.key==="Escape"){e.preventDefault();cleanup()}};
  function panes(rect,pad=8){const vw=innerWidth,vh=innerHeight,r=rect?{l:Math.max(0,rect.left-pad),t:Math.max(0,rect.top-pad),r:Math.min(vw,rect.right+pad),b:Math.min(vh,rect.bottom+pad)}:{l:vw/2,t:vh/2,r:vw/2,b:vh/2};return `<div class="tour-pane tour-pane-top" style="left:0;top:0;width:100%;height:${r.t}px"></div><div class="tour-pane tour-pane-left" style="left:0;top:${r.t}px;width:${r.l}px;height:${Math.max(0,r.b-r.t)}px"></div><div class="tour-pane tour-pane-right" style="left:${r.r}px;top:${r.t}px;width:${Math.max(0,vw-r.r)}px;height:${Math.max(0,r.b-r.t)}px"></div><div class="tour-pane tour-pane-bottom" style="left:0;top:${r.b}px;width:100%;height:${Math.max(0,vh-r.b)}px"></div>`}
  function position(){
    const s=steps[i],card=$("#tourCard",root),mask=$("#tourMask",root),target=s.target?$(s.target):null;if(!card||!mask)return;const tr=target?.getBoundingClientRect();mask.innerHTML=panes(tr,s.padding||8);if(target){target.classList.add("tour-target");const spot=$("#tourSpotlight",root);Object.assign(spot.style,{left:`${tr.left-7}px`,top:`${tr.top-7}px`,width:`${tr.width+14}px`,height:`${tr.height+14}px`});spot.hidden=false}else $("#tourSpotlight",root).hidden=true;
    const cr=card.getBoundingClientRect(),margin=18,targetRect=tr?{left:tr.left-20,right:tr.right+20,top:tr.top-20,bottom:tr.bottom+20}:null;
    const candidates={"bottom-center":{left:(innerWidth-cr.width)/2,top:innerHeight-cr.height-margin},"top-center":{left:(innerWidth-cr.width)/2,top:margin},"lower-right":{left:innerWidth-cr.width-margin,top:innerHeight-cr.height-margin},"lower-left":{left:margin,top:innerHeight-cr.height-margin},"upper-right":{left:innerWidth-cr.width-margin,top:margin},"upper-left":{left:margin,top:margin}};
    const collides=p=>targetRect&&!(p.left+cr.width<targetRect.left||p.left>targetRect.right||p.top+cr.height<targetRect.top||p.top>targetRect.bottom);
    let pos=candidates[lastAnchor];if(!pos||collides(pos)){const found=Object.entries(candidates).find(([,p])=>!collides(p));if(found){lastAnchor=found[0];pos=found[1]}}Object.assign(card.style,{left:`${Math.max(margin,Math.min(innerWidth-cr.width-margin,pos.left))}px`,top:`${Math.max(margin,Math.min(innerHeight-cr.height-margin,pos.top))}px`});
  }
  async function draw(){
    document.querySelectorAll(".tour-target").forEach(x=>x.classList.remove("tour-target"));const s=steps[i];if(s.prepare)await s.prepare();root.innerHTML=`<div class="tour-overlay"><div id="tourMask" class="tour-focus-mask"></div><div id="tourSpotlight" class="tour-spotlight" hidden></div><section id="tourCard" class="tour-card a31-tour-card"><div class="tour-progress"><span>${i+1} / ${steps.length}</span><span>${Math.round((i+1)/steps.length*100)}%</span></div><h2>${escapeHtml(s.title)}</h2><p>${escapeHtml(s.body)}</p><div class="tour-actions"><button class="btn" id="tourSkip">${i===steps.length-1?'Close':'Skip tour'}</button><span class="tour-spacer"></span>${i?'<button class="btn" id="tourBack">Back</button>':""}<button class="btn primary" id="tourNext">${i===steps.length-1?'Finish':'Next'}</button></div></section></div>`;$("#tourSkip").onclick=finish;$("#tourBack")?.addEventListener("click",()=>{i--;draw()});$("#tourNext").onclick=()=>{if(i===steps.length-1)return finish();i++;draw()};requestAnimationFrame(()=>requestAnimationFrame(position));
  }
  window.addEventListener("resize",position);document.addEventListener("keydown",onKeyDown);draw();
};

/* Shell bindings and final routing */
const a31BindShellBase=bindShell;
bindShell=function(){
  a31BindShellBase();
  const nav=$("#primaryNav");
  if(nav&&!nav.dataset.delegatedNav){
    nav.dataset.delegatedNav="1";
    nav.addEventListener("click",e=>{
      const model=e.target.closest("[data-a31-model-view]");if(model){e.preventDefault();e.stopPropagation();openRoute("models");a31SetModelView(model.dataset.a31ModelView);return}
      const project=e.target.closest("[data-a31-project-nav]");if(project){e.preventDefault();e.stopPropagation();qa4ProjectHub.activeProjectID=project.dataset.a31ProjectNav;qa4ProjectHub.activeWorkspaceID=project.dataset.a31WorkspaceNav||"";openRoute("projects");renderNav();return}
      const route=e.target.closest("[data-route]");if(route){e.preventDefault();openRoute(route.dataset.route)}
    });
  }
  $("#controlChatLauncher")?.addEventListener("click",()=>a31OpenControlChat());$("#controlChatClose")?.addEventListener("click",e=>{e.stopPropagation();a31CloseControlChat()});$("#controlChatToggle")?.addEventListener("click",()=>a31SetControlChatCollapsed(!state.controlChatCollapsed));$("#controlChatAssistantTab")?.addEventListener("click",()=>a31OpenControlChat("assistant"));$("#controlChatOrchestratorTab")?.addEventListener("click",()=>a31OpenControlChat("orchestrator"));
};
renderActiveView=async function(){
  const epoch=++qa31ViewEpoch,t=currentTab();if(!t)return;if(t.state==="suspended")t.state="active";const route=t.route;
  const renderers={operations:renderOperations,tasks:renderTasks,projects:renderProjects,models:renderModels,nodes:renderNodes,agents:renderAgents,skills:renderSkills,settings:renderSettings,secrets:renderSecrets,evidence:()=>renderPlaceholder("Evidence / Audit","Event Ledger, Artifacts, Observations and Verifications.")};
  try{await Promise.resolve((renderers[route]||renderOperations)())}finally{const host=$("#viewHost");if(epoch===qa31ViewEpoch){if(host)host.dataset.renderedRoute=route;renderNav()}}
};
bootOnePane();
