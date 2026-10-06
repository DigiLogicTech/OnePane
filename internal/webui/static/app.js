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

const A31_OPERATIONS_LAYOUT_VERSION=3;
function a31OperationsLayoutBroken(rows){
  if(!Array.isArray(rows)||rows.length<4)return true;
  const normalized=rows.map(x=>({...x}));a31NormalizeLayout(normalized);
  const oneRow=normalized.every(w=>Number(w.y||0)===0),rowWidth=normalized.reduce((n,w)=>n+Number(w.width||0),0);
  const overlaps=normalized.some((a,i)=>normalized.slice(i+1).some(b=>a31Overlap(a,b)));
  const invalid=normalized.some(w=>!Number.isFinite(w.x)||!Number.isFinite(w.y)||w.x<0||w.y<0||w.width<3||w.width>12||w.x+w.width>12);
  return overlaps||invalid||(oneRow&&rowWidth>A31_LAYOUT_COLUMNS);
}
function a31RepairPersistedUIState(){
  let changed=false;
  if(a31OperationsLayoutBroken(state.operationsWidgets)){
    state.operationsWidgets=defaultState().operationsWidgets.map(x=>({...x}));
    changed=true;
  }
  if(Number(state.operationsLayoutVersion||0)!==A31_OPERATIONS_LAYOUT_VERSION){
    state.operationsLayoutVersion=A31_OPERATIONS_LAYOUT_VERSION;changed=true;
  }
  if(changed)persist();
  return changed;
}
a31RepairPersistedUIState();

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
  const route=currentTab()?.route||"operations",projects=a31Array(qa4ProjectHub?.projects),nav=$("#primaryNav");if(!nav)return;
  const projectTree=projects.length?`<div class="project-nav-tree">${projects.map(p=>{const projectActive=route==="projects"&&p.id===qa4ProjectHub.activeProjectID,workspaces=typeof qa4Workspaces==="function"?qa4Workspaces(p):[];return `<div class="project-nav-node ${projectActive&&!qa4ProjectHub.activeWorkspaceID?'active-project':''}"><button class="project-nav-project ${projectActive&&!qa4ProjectHub.activeWorkspaceID?'active':''}" data-a31-project-nav="${escapeHtml(p.id)}"><span>▢</span><span>${escapeHtml(p.name||"Project")}</span></button><div class="project-nav-workspaces">${workspaces.map(w=>`<button class="project-nav-workspace ${projectActive&&w.id===qa4ProjectHub.activeWorkspaceID?'active':''}" data-a31-project-nav="${escapeHtml(p.id)}" data-a31-workspace-nav="${escapeHtml(w.id)}"><span class="project-nav-branch"></span><span>${escapeHtml(w.name||"Workspace")}</span></button>`).join("")}</div></div>`}).join("")}</div>`:"";
  const modelTree=`<div class="model-nav-tree"><button class="model-nav-child ${route==="models"&&a31ModelView==="local"?'active':''}" data-a31-model-view="local"><span>◈</span><span>Local Models</span></button><button class="model-nav-child ${route==="models"&&a31ModelView==="cloud"?'active':''}" data-a31-model-view="cloud"><span>☁</span><span>Cloud Models</span></button></div>`;
  const html=navItems.map(([r,icon,label])=>{const translated=qa5T(r,label),hasLeaf=(r==="models"||r==="projects"),parentActive=route===r&&!hasLeaf;const row=`<button class="nav-item ${parentActive?'active':''}" data-route="${r}" title="${escapeHtml(translated)}"><span class="nav-icon">${icon}</span><span class="nav-label">${escapeHtml(translated)}</span></button>`;if(r==="projects")return row+projectTree;if(r==="models")return row+modelTree;return row}).join("");
  nav.innerHTML=html;
  $$("[data-a31-model-view]",nav).forEach(b=>b.onclick=e=>{e.preventDefault();e.stopPropagation();openRoute("models");a31SetModelView(b.dataset.a31ModelView)});
  $$("[data-a31-project-nav]",nav).forEach(b=>b.onclick=e=>{e.preventDefault();e.stopPropagation();qa4ProjectHub.activeProjectID=b.dataset.a31ProjectNav;qa4ProjectHub.activeWorkspaceID=b.dataset.a31WorkspaceNav||"";openRoute("projects")});
  $$(":scope > [data-route]",nav).forEach(b=>b.onclick=e=>{e.preventDefault();openRoute(b.dataset.route)});
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
function a31ApplyLayout(root,items,attr,animate=false,beforeOverride=null){
  const before=new Map();if(animate)$$(`[${attr}]`,root).forEach(el=>before.set(el.getAttribute(attr),el.getBoundingClientRect()));
  if(beforeOverride)for(const [id,rect] of beforeOverride)before.set(id,rect);
  for(const item of items){const el=$(`[${attr}="${CSS.escape(item.id)}"]`,root);if(!el)continue;el.style.gridColumn=`${item.x+1} / span ${item.width}`;el.style.gridRow=`${item.y+1} / span ${item.height}`}
  if(animate&&Element.prototype.animate)requestAnimationFrame(()=>$$(`[${attr}]`,root).forEach(el=>{const old=before.get(el.getAttribute(attr));if(!old)return;const now=el.getBoundingClientRect(),dx=old.left-now.left,dy=old.top-now.top,sx=old.width&&now.width?old.width/now.width:1,sy=old.height&&now.height?old.height/now.height:1;if(Math.abs(dx)>1||Math.abs(dy)>1||Math.abs(sx-1)>.015||Math.abs(sy-1)>.015)el.animate([{transform:`translate(${dx}px,${dy}px) scale(${sx},${sy})`,transformOrigin:"top left"},{transform:"translate(0,0) scale(1)",transformOrigin:"top left"}],{duration:180,easing:"cubic-bezier(.2,.7,.2,1)"})}));
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
  const interactiveSelector='button,select,input,textarea,a,label,[contenteditable="true"]';
  const spanPx=(cells,unit,gap)=>Math.max(1,cells*unit+Math.max(0,cells-1)*gap);
  const begin=(e,id,kind)=>{
    if(e.button!==0||root.dataset.layoutSaving==='true')return;
    if(kind==='drag'&&e.target.closest(interactiveSelector))return;
    const item=items.find(x=>x.id===id);if(!item)return;
    const card=e.currentTarget.closest(`[${attr}]`)||e.currentTarget;if(!card)return;
    e.preventDefault();e.stopPropagation();

    const snapshot=items.map(x=>({...x})),generation=++a31LayoutGeneration;
    const rootRect=root.getBoundingClientRect(),cardRect=card.getBoundingClientRect(),style=getComputedStyle(root),columnGap=parseFloat(style.columnGap)||0,rowGap=parseFloat(style.rowGap)||0;
    const colW=Math.max(1,(rootRect.width-columnGap*(A31_LAYOUT_COLUMNS-1))/A31_LAYOUT_COLUMNS),colStep=colW+columnGap,rowStep=A31_LAYOUT_ROW_PX+rowGap;
    const start={x:e.clientX,y:e.clientY,itemX:item.x,itemY:item.y,w:item.width,h:item.height};
    const target=e.currentTarget,edge=target.dataset.resizeEdge||'se',constraints=a31Constraints(item);
    let raf=0,pending=null,finished=false,previewRect={x:item.x,y:item.y,width:item.width,height:item.height};

    const clamp=(v,min,max)=>Math.max(min,Math.min(max,v));
    const clearPreview=()=>{
      card.style.removeProperty("transform");card.style.removeProperty("width");card.style.removeProperty("height");card.style.removeProperty("will-change");
      delete card.dataset.layoutActive;delete card.dataset.layoutCollision;delete card.dataset.layoutKind;delete card.dataset.layoutPreview;
    };
    const updateCollision=()=>{card.dataset.layoutCollision=items.some(other=>other!==item&&a31Overlap(previewRect,other))?"true":"false"};
    const previewDrag=(dxPx,dyPx)=>{
      const pxX=clamp(dxPx,rootRect.left-cardRect.left,rootRect.right-cardRect.right),pxY=Math.max(rootRect.top-cardRect.top,dyPx);
      const dx=Math.round(pxX/colStep),dy=Math.round(pxY/rowStep);
      previewRect={x:clamp(start.itemX+dx,0,A31_LAYOUT_COLUMNS-start.w),y:Math.max(0,start.itemY+dy),width:start.w,height:start.h};
      card.style.transform=`translate3d(${pxX}px,${pxY}px,0)`;card.style.removeProperty("width");card.style.removeProperty("height");
    };
    const previewResize=(dxPx,dyPx)=>{
      const dx=Math.round(dxPx/colStep),dy=Math.round(dyPx/rowStep);previewRect=a31ResizeRect(start,edge,dx,dy,constraints);
      const minW=spanPx(constraints.minW,colW,columnGap),maxWCells=edge.includes("w")?Math.min(constraints.maxW,start.itemX+start.w):Math.min(constraints.maxW,A31_LAYOUT_COLUMNS-start.itemX),maxW=spanPx(maxWCells,colW,columnGap);
      const minH=spanPx(constraints.minH,A31_LAYOUT_ROW_PX,rowGap),maxHCells=edge.includes("n")?Math.min(constraints.maxH,start.itemY+start.h):constraints.maxH,maxH=spanPx(maxHCells,A31_LAYOUT_ROW_PX,rowGap);
      let left=cardRect.left,right=cardRect.right,top=cardRect.top,bottom=cardRect.bottom;
      if(edge.includes("e"))right=clamp(cardRect.right+dxPx,cardRect.left+minW,Math.min(rootRect.right,cardRect.left+maxW));
      if(edge.includes("w"))left=clamp(cardRect.left+dxPx,Math.max(rootRect.left,cardRect.right-maxW),cardRect.right-minW);
      if(edge.includes("s"))bottom=clamp(cardRect.bottom+dyPx,cardRect.top+minH,cardRect.top+maxH);
      if(edge.includes("n"))top=clamp(cardRect.top+dyPx,Math.max(rootRect.top,cardRect.bottom-maxH),cardRect.bottom-minH);
      card.style.transform=`translate3d(${left-cardRect.left}px,${top-cardRect.top}px,0)`;card.style.width=`${Math.max(1,right-left)}px`;card.style.height=`${Math.max(1,bottom-top)}px`;
    };
    const renderPreview=()=>{raf=0;if(!pending)return;const {dx,dy}=pending;pending=null;if(kind==="drag")previewDrag(dx,dy);else previewResize(dx,dy);card.dataset.layoutPreview=`${previewRect.x},${previewRect.y},${previewRect.width},${previewRect.height}`;updateCollision()};
    const queue=(dx,dy)=>{pending={dx,dy};if(!raf)raf=requestAnimationFrame(renderPreview)};
    const move=ev=>queue(ev.clientX-start.x,ev.clientY-start.y);
    const restore=()=>{if(String(root.dataset.layoutGeneration||'')!==String(generation))return false;for(const old of snapshot){const current=items.find(x=>x.id===old.id);if(current)Object.assign(current,old)}clearPreview();a31ApplyLayout(root,items,attr,true);return true};
    const finish=async cancelled=>{
      if(finished)return;finished=true;if(raf){cancelAnimationFrame(raf);raf=0}if(pending)renderPreview();const livePreview=card.getBoundingClientRect();
      try{if(target.hasPointerCapture?.(e.pointerId))target.releasePointerCapture(e.pointerId)}catch{}
      target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',up);target.removeEventListener('pointercancel',cancel);root.classList.remove('layout-interacting');delete root.dataset.layoutKind;
      if(cancelled){restore();return}
      clearPreview();Object.assign(item,previewRect);item.col=item.width;item.row=item.height;a31ResolveLayout(items,item.id);a31ApplyLayout(root,items,attr,true,new Map([[item.id,livePreview]]));
      root.dataset.layoutSaving='true';a31LayoutSaveInFlight++;
      try{await save?.();root.dataset.layoutSaved='true';setTimeout(()=>{if(root.isConnected)delete root.dataset.layoutSaved},900)}
      catch(ex){restore();notice('Layout save failed: '+ex.message,'bad')}
      finally{a31LayoutSaveInFlight=Math.max(0,a31LayoutSaveInFlight-1);delete root.dataset.layoutSaving}
    };
    const up=()=>finish(false),cancel=()=>finish(true);
    try{target.setPointerCapture?.(e.pointerId)}catch{}
    root.classList.add('layout-interacting');root.dataset.layoutGeneration=String(generation);root.dataset.layoutKind=kind;card.dataset.layoutActive='true';card.dataset.layoutKind=kind;card.style.willChange="transform,width,height";
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
  return `<section class="dashboard-widget a31-layout-item ${state.operationsEdit?'editable':''}" data-op-widget="${escapeHtml(w.id)}" style="${a31GridStyle(w)}"><div class="dashboard-edit-bar ${state.operationsEdit?'':'hidden'}" data-op-drag="${escapeHtml(w.id)}" title="Drag component"><span class="dashboard-drag" aria-hidden="true">⋮⋮</span><strong>${escapeHtml(w.title)}</strong><span class="dashboard-edit-spacer"></span><select class="dashboard-size" data-op-preset="${escapeHtml(w.id)}"><option value="">Size…</option><option value="small">Small</option><option value="medium">Medium</option><option value="large">Large</option><option value="wide">Wide</option><option value="full">Full</option></select><button class="tiny danger" data-op-remove="${escapeHtml(w.id)}">×</button></div><div class="dashboard-widget-content">${operationsComponentContent(w.type)}</div>${state.operationsEdit?a31ResizeHandles(w.id,"data-op-resize",w.title):""}</section>`
}
function a31RenderOperationsGrid(){
  const root=$("#operationsLayout");if(!root)return;
  if(a31OperationsLayoutBroken(state.operationsWidgets)){state.operationsWidgets=defaultState().operationsWidgets.map(x=>({...x}));state.operationsLayoutVersion=A31_OPERATIONS_LAYOUT_VERSION;persist()}
  a31NormalizeLayout(state.operationsWidgets);root.innerHTML=state.operationsWidgets.map(a31OpsWidget).join("");
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
function a31OperationsActivity(){if(!liveOpsReported("events"))return `<section class="panel-card"><div class="card-header"><div><div class="card-title">Activity</div><div class="list-meta">Recent control-plane, scheduler, model, node and provider events.</div></div></div><div class="empty-state compact">Event feed not reported.</div></section>`;const rows=a31Array(liveOps.events).slice(-100).reverse();return `<section class="panel-card"><div class="card-header"><div><div class="card-title">Activity</div><div class="list-meta">Recent control-plane, scheduler, model, node and provider events.</div></div></div><div class="activity-stream">${rows.length?rows.map(e=>`<div class="activity-row"><span class="mono-cell">${escapeHtml(String(e.sequence||""))}</span><div><strong>${escapeHtml(eventLabel(e))}</strong><div class="list-meta">${escapeHtml([e.aggregate_type,e.aggregate_id].filter(Boolean).join(" · "))}</div></div><span class="list-meta">${e.occurred_at?new Date(Number(e.occurred_at)).toLocaleTimeString():""}</span></div>`).join(""):'<div class="empty-state compact">No activity recorded yet.</div>'}</div></section>`}
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
  let comps={},componentsReported=false;try{comps=await qa5ModelComponents();componentsReported=true}catch{}
  const tasksReported=liveOpsReported("tasks"),failed=tasksReported?a31Array(liveOps.tasks).filter(t=>["failed","blocked"].includes(String(t.state||"").toLowerCase())):[],compRows=componentsReported?Object.values(a31Object(comps)).filter(x=>["failed","degraded","interrupted"].includes(String(x.state||"").toLowerCase())):[];
  const unavailable=`${componentsReported?"":'<div class="recovery-row"><div><strong>Managed component health not reported</strong><div class="list-meta">The component health feed is unavailable.</div></div></div>'}${tasksReported?"":'<div class="recovery-row"><div><strong>Task recovery status not reported</strong><div class="list-meta">The task feed is unavailable.</div></div></div>'}`;
  const empty=componentsReported&&tasksReported&&!compRows.length&&!failed.length?'<div class="empty-state compact">No degraded components or failed/blocked tasks require recovery.</div>':"";
  return `<section class="panel-card"><div class="card-header recovery-card-header"><div><div class="card-title">Recovery</div><div class="list-meta">Actionable degraded state only; OnePane does not reset healthy components.</div></div><button class="btn" id="a31RecoveryRefresh">Refresh health</button></div><div class="widget-body"><div class="recovery-list">${unavailable}${compRows.map(c=>`<div class="recovery-row"><div><strong>${escapeHtml(c.display_name||c.id)}</strong><div class="list-meta">${escapeHtml(c.last_error||c.state||"degraded")}</div></div><button class="btn" data-a31-repair-component="${escapeHtml(c.id)}">Repair</button></div>`).join("")}${failed.map(t=>`<div class="recovery-row"><div><strong>Task ${escapeHtml(t.id||"")}</strong><div class="list-meta">${escapeHtml(t.objective||t.state||"")}</div></div><button class="btn" data-route="tasks">Open Tasks</button></div>`).join("")}${empty}</div></div></section>`
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
  const controls=edit?`<div class="dashboard-edit-bar" data-pw-drag="${escapeHtml(w.id)}" title="Drag component"><span class="dashboard-drag" aria-hidden="true">⋮⋮</span><strong>${escapeHtml(w.title||w.type)}</strong><span class="dashboard-edit-spacer"></span><select class="dashboard-size" data-pw-preset="${escapeHtml(w.id)}"><option value="">Size…</option><option value="small">Small</option><option value="medium">Medium</option><option value="large">Large</option><option value="wide">Wide</option><option value="full">Full</option></select><button class="tiny danger" data-pw-remove="${escapeHtml(w.id)}">×</button></div>`:"";
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
  const refreshSaved=async()=>{const freshProject=qa4ProjectHub.projects.find(x=>x.id===project.id),freshWorkspace=freshProject?qa4Workspaces(freshProject).find(x=>x.id===workspace.id):null;if(freshProject&&freshWorkspace&&root.isConnected)await a31RefreshProjectGrid(freshProject,freshWorkspace,true)};
  a31BindLayout(root,workspace.widgets||[],{attr:"data-pw-widget",dragAttr:"data-pw-drag",resizeAttr:"data-pw-resize",persist:async()=>{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));await refreshSaved()}});
  $$("[data-pw-preset]",root).forEach(sel=>sel.onchange=async()=>{if(!sel.value||root.dataset.layoutSaving==="true")return;const w=workspace.widgets.find(x=>x.id===sel.dataset.pwPreset);if(!w)return;const snapshot=(workspace.widgets||[]).map(x=>({...x})),p=workspacePreset(sel.value,w);w.width=p.col;w.height=p.row;w.col=p.col;w.row=p.row;a31ResolveLayout(workspace.widgets,w.id);a31ApplyLayout(root,workspace.widgets,"data-pw-widget",true);root.dataset.layoutSaving="true";a31LayoutSaveInFlight++;try{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));await refreshSaved()}catch(ex){workspace.widgets.splice(0,workspace.widgets.length,...snapshot);a31ApplyLayout(root,workspace.widgets,"data-pw-widget",true);notice("Layout save failed: "+ex.message,"bad")}finally{a31LayoutSaveInFlight=Math.max(0,a31LayoutSaveInFlight-1);delete root.dataset.layoutSaving}});
  $$("[data-pw-remove]",root).forEach(b=>b.onclick=async()=>{if(root.dataset.layoutSaving==="true")return;const snapshot=(workspace.widgets||[]).map(x=>({...x})),el=b.closest("[data-pw-widget]");root.dataset.layoutSaving="true";a31LayoutSaveInFlight++;try{if(el?.animate)await el.animate([{opacity:1},{opacity:0,transform:"scale(.96)"}],{duration:130}).finished.catch(()=>{});workspace.widgets=workspace.widgets.filter(x=>x.id!==b.dataset.pwRemove);a31NormalizeLayout(workspace.widgets);await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));await refreshSaved()}catch(ex){workspace.widgets=snapshot;await a31RefreshProjectGrid(project,workspace,true);notice("Layout save failed: "+ex.message,"bad")}finally{a31LayoutSaveInFlight=Math.max(0,a31LayoutSaveInFlight-1);delete root.dataset.layoutSaving}});
};
qa4AddWorkspaceComponent=function(project,workspace){
  const used=new Set((workspace.widgets||[]).map(x=>x.type)),available=qa4WorkspaceCatalogue().filter(([t])=>!used.has(t)||["notes","chat"].includes(t));
  openModal("Add workspace component",`<div class="component-picker-grid">${available.map(([t,title])=>`<button class="component-choice" data-a31-add-project-component="${t}"><strong>${escapeHtml(title)}</strong><span>Add to ${escapeHtml(workspace.name)}</span></button>`).join("")}</div>`);
  $$("[data-a31-add-project-component]").forEach(b=>b.onclick=async()=>{const root=$("#qa4WorkspaceGrid");if(root?.dataset.layoutSaving==="true")return;const [type,title]=qa4WorkspaceCatalogue().find(x=>x[0]===b.dataset.a31AddProjectComponent),snapshot=(workspace.widgets||[]).map(x=>({...x})),item={id:`pw-${type}-${Date.now().toString(36)}`,type,title,width:type==="tasks"||type==="scheduled"?6:4,height:type==="chat"?5:4};workspace.widgets=workspace.widgets||[];a31NormalizeLayout(workspace.widgets);const slot=a31FirstFree(workspace.widgets,item,null);item.x=slot.x;item.y=slot.y;item.col=item.width;item.row=item.height;workspace.widgets.push(item);if(root)root.dataset.layoutSaving="true";a31LayoutSaveInFlight++;try{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));closeModal();const freshProject=qa4ProjectHub.projects.find(x=>x.id===project.id),freshWorkspace=freshProject?qa4Workspaces(freshProject).find(x=>x.id===workspace.id):null;if(freshProject&&freshWorkspace)await a31RefreshProjectGrid(freshProject,freshWorkspace,true)}catch(ex){workspace.widgets.splice(0,workspace.widgets.length,...snapshot);notice("Component add failed: "+ex.message,"bad")}finally{a31LayoutSaveInFlight=Math.max(0,a31LayoutSaveInFlight-1);if(root)delete root.dataset.layoutSaving}});
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
  liveOps.providers=providerRows;liveOps.reported.providers=true;syncLiveNotifications();
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
  try{const out=await apiRequest("/v1/nodes"),nodeRows=Array.isArray(out)?out:a31Array(out?.nodes);liveOps.nodes=nodeRows;liveOps.reported.nodes=true;syncLiveNotifications();$("#a31Nodes").innerHTML=`<div class="node-grid">${nodeRows.map(n=>{const id=n.id||n.node_id,name=n.display_name||id,state=n.status||n.state||"unknown";return `<article class="panel-card node-card" data-a31-node="${escapeHtml(id)}"><div class="card-header"><div><div class="card-title">${escapeHtml(name)}</div><div class="list-meta">${escapeHtml(n.os_name||n.os||"")} ${escapeHtml(n.architecture||"")}</div></div><span class="pill ${["online","ready","active"].includes(String(state).toLowerCase())?'good':''}">${escapeHtml(titleCase(state))}</span></div><div class="widget-body"><dl class="definition-grid"><dt>CPU</dt><dd>${escapeHtml(n.cpu_name||n.cpu||"Detected by node")}</dd><dt>GPU</dt><dd>${escapeHtml(n.gpu_name||n.compute||"See capabilities")}</dd><dt>Last seen</dt><dd>${escapeHtml(String(n.last_seen_at||n.last_seen||"—"))}</dd></dl><div class="toolbar"><button class="btn" data-a31-node-cap="${escapeHtml(id)}">Capabilities</button><button class="btn" data-a31-node-models="${escapeHtml(id)}">Model management</button><button class="btn danger" data-a31-node-revoke="${escapeHtml(id)}">Revoke</button></div></div></article>`}).join("")||'<div class="empty-state">No enrolled nodes yet.</div>'}</div>`;
    $$("[data-a31-node-cap]").forEach(b=>b.onclick=async()=>{try{const x=await apiRequest(`/v1/nodes/${encodeURIComponent(b.dataset.a31NodeCap)}/capabilities`);openModal("Node capabilities",`<pre class="json-preview">${escapeHtml(JSON.stringify(x,null,2))}</pre>`)}catch(ex){notice(ex.message,"bad")}});
    $$("[data-a31-node-models]").forEach(b=>b.onclick=async()=>{try{const x=await apiRequest(`/v1/nodes/${encodeURIComponent(b.dataset.a31NodeModels)}/model-management`);openModal("Node model management",`<pre class="json-preview">${escapeHtml(JSON.stringify(x,null,2))}</pre>`)}catch(ex){notice(ex.message,"bad")}});
    $$("[data-a31-node-revoke]").forEach(b=>b.onclick=async()=>{if(!confirm("Revoke this node?"))return;try{await apiRequest(`/v1/nodes/${encodeURIComponent(b.dataset.a31NodeRevoke)}/revoke`,{method:"POST",body:"{}"});renderNodes()}catch(ex){notice(ex.message,"bad")}});
  }catch(ex){$("#a31Nodes").innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}
  $("#a31AddNode").onclick=openPairNode;bindViewActions($("#viewHost"));renderNav();
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
  $("#viewHost").innerHTML=`<section class="page">${pageHeader("Skills","Reusable capability packages, tool dependencies and assignments.",'<button class="btn primary" id="a31UploadSkill">Upload Skill</button>')}<div class="subtabs"><button class="subtab ${a31SkillsView==='installed'?'active':''}" data-a31-skills-tab="installed">Installed</button><button class="subtab ${a31SkillsView==='catalogue'?'active':''}" data-a31-skills-tab="catalogue">Catalogue</button><button class="subtab ${a31SkillsView==='assignments'?'active':''}" data-a31-skills-tab="assignments">Assignments</button><button class="subtab ${a31SkillsView==='matrix'?'active':''}" data-a31-skills-tab="matrix">Capability Matrix</button><button class="subtab ${a31SkillsView==='packages'?'active':''}" data-a31-skills-tab="packages">Packages</button><button class="subtab ${a31SkillsView==='bundles'?'active':''}" data-a31-skills-tab="bundles">Tool Bundles</button></div><div id="a31SkillsBody"><div class="widget-body">Loading…</div></div></section>`;$("#a31UploadSkill").onclick=a31UploadSkill;$$("[data-a31-skills-tab]").forEach(b=>b.onclick=()=>a31SetSkillsView(b.dataset.a31SkillsTab));const body=$("#a31SkillsBody"),qs=encodeURIComponent(onepaneWorkspace);
  try{const [packages,bundles,assignments,profiles,deployments]=await Promise.all([apiRequest(`/v1/skills/packages?workspace_id=${qs}`),apiRequest(`/v1/skills/tool-bundles?workspace_id=${qs}`),apiRequest(`/v1/skills/assignments?workspace_id=${qs}`),apiRequest(`/v1/agent-profiles?workspace_id=${qs}`).catch(()=>[]),qa5LoadManagedDeployments().catch(()=>[])]),pkgs=a31Array(packages);
    if(a31SkillsView==="installed"||a31SkillsView==="catalogue"){const rows=a31SkillsView==="installed"?pkgs.filter(p=>p.status==="installed"):pkgs;body.innerHTML=`<div class="skill-grid">${rows.map((p,i)=>{const m=a31JSON(p.manifest,{});return `<article class="panel-card skill-card"><div class="card-header"><div><div class="card-title">${escapeHtml(p.name)}</div><div class="list-meta">${escapeHtml(p.version)} · ${escapeHtml(p.publisher||"Unknown publisher")}</div></div><span class="pill ${p.trust_state==='builtin'||p.trust_state==='verified'?'good':''}">${escapeHtml(titleCase(p.trust_state||"unverified"))}</span></div><div class="widget-body"><p>${escapeHtml(m.description||m.type||"Capability pack")}</p><div class="tool-chip-row">${a31Array(m.tool_bundles).map(x=>`<span class="tool-chip">${escapeHtml(x)}</span>`).join("")}</div><div class="toolbar">${p.status==="quarantined"?`<button class="btn primary" data-a31-skill-install="${i}">Install</button>`:""}${p.status==="installed"&&p.source_kind!=="builtin"?`<button class="btn" data-a31-skill-status="${i}:disabled">Disable</button>`:""}${p.status==="disabled"?`<button class="btn" data-a31-skill-status="${i}:installed">Enable</button>`:""}</div></div></article>`}).join("")||'<div class="empty-state">No Skills in this view.</div>'}</div>`;$$("[data-a31-skill-install]").forEach(b=>b.onclick=async()=>{const p=rows[Number(b.dataset.a31SkillInstall)];try{await apiRequest(`/v1/skills/packages/${encodeURIComponent(p.id)}/install?workspace_id=${qs}`,{method:"POST",body:"{}"});renderSkills()}catch(ex){notice(ex.message,"bad")}});$$("[data-a31-skill-status]").forEach(b=>b.onclick=async()=>{const [i,status]=b.dataset.a31SkillStatus.split(":"),p=rows[Number(i)];try{await apiRequest(`/v1/skills/packages/${encodeURIComponent(p.id)}?workspace_id=${qs}`,{method:"PATCH",body:JSON.stringify({status})});renderSkills()}catch(ex){notice(ex.message,"bad")}})}
    else if(a31SkillsView==="assignments"){body.innerHTML=`<section class="panel-card"><div class="card-header"><div><div class="card-title">Assignments</div><div class="list-meta">Assigned does not imply permitted; Workspace policy remains authoritative.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Skill package</th><th>Subject</th><th>State</th></tr></thead><tbody>${a31Array(assignments).map(a=>`<tr><td>${escapeHtml(a.skill_package_id)}</td><td>${escapeHtml(a.subject_kind)} · ${escapeHtml(a.subject_id)}</td><td>${a.enabled?'Assigned':'Disabled'}</td></tr>`).join("")}</tbody></table></div></section>`}
    else if(a31SkillsView==="matrix"){
      const packageMap=new Map(pkgs.map(p=>[String(p.id),p])),profileMap=new Map(a31Array(profiles).map(p=>[String(p.id),p])),bundleMap=new Map(a31Array(bundles).map(b=>[String(b.ID||b.id||b.Name||b.name),b]));
      const rows=a31Array(assignments).filter(a=>a.enabled).map(a=>{const pkg=packageMap.get(String(a.skill_package_id)),manifest=a31JSON(pkg?.manifest,{}),bundleIDs=a31Array(manifest.tool_bundles),tools=bundleIDs.flatMap(id=>a31Array(bundleMap.get(String(id))?.Tools||bundleMap.get(String(id))?.tools)),profile=String(a.subject_kind)==="agent_profile"?profileMap.get(String(a.subject_id)):null,meta=profile?a31ProfileMeta(profile):{},model=meta.model_ref||meta.model||meta.default_model||profile?.model_ref||(String(a.subject_kind)==="workspace"?"Workspace routing":String(a.subject_kind).startsWith("team")?"Team/Council pinned at run":"Auto / policy-routed");return {subject:`${a.subject_kind} · ${a.subject_id}`,model,skill:pkg?`${pkg.name} ${pkg.version}`:a.skill_package_id,bundles:bundleIDs.join(", ")||"—",tools:[...new Set(tools)].join(", ")||"Resolved by bundle/policy"}});
      body.innerHTML=`<section class="panel-card capability-matrix"><div class="card-header"><div><div class="card-title">Effective Capability Matrix</div><div class="list-meta">Skills are assigned to governed scopes; models inherit capability through Agent/Team/Workspace routing rather than owning authority directly.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Subject</th><th>Model / routing</th><th>Skill</th><th>Tool bundles</th><th>Effective tools</th></tr></thead><tbody>${rows.length?rows.map(r=>`<tr><td>${escapeHtml(r.subject)}</td><td>${escapeHtml(r.model)}</td><td>${escapeHtml(r.skill)}</td><td>${escapeHtml(r.bundles)}</td><td>${escapeHtml(r.tools)}</td></tr>`).join(""):'<tr><td colspan="5" class="muted-cell">No enabled Skill assignments yet.</td></tr>'}</tbody></table></div></section><section class="panel-card"><div class="card-header"><div><div class="card-title">Known model deployments</div><div class="list-meta">Use this alongside the matrix to see which runtime/model candidates are currently available to routed Agents.</div></div></div><div class="widget-body">${a31Array(deployments).length?a31Array(deployments).map(d=>`<div class="bundle-row"><strong>${escapeHtml(d.display_name||d.model_ref||d.deployment_id)}</strong><span>${escapeHtml(d.runtime_name||d.runtime_backend||"managed")} · ${escapeHtml(a31PlacementLabel(d))} · ${escapeHtml(d.status||"unknown")}</span></div>`).join(""):'No managed local model deployments are currently registered.'}</div></section>`;
    }
    else if(a31SkillsView==="packages")body.innerHTML=`<section class="panel-card"><div class="card-header"><div><div class="card-title">Packages</div><div class="list-meta">Uploaded packages remain quarantined until validation and explicit installation.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Name</th><th>Source</th><th>Trust</th><th>Status</th><th>SHA-256</th></tr></thead><tbody>${pkgs.length?pkgs.map(p=>`<tr><td>${escapeHtml(p.name)} ${escapeHtml(p.version)}</td><td>${escapeHtml(p.source_kind)}</td><td>${escapeHtml(p.trust_state)}</td><td>${escapeHtml(p.status)}</td><td class="mono-cell">${escapeHtml(String(p.package_sha256).slice(0,16))}…</td></tr>`).join(""):'<tr><td colspan="5" class="muted-cell">No Skill packages are registered.</td></tr>'}</tbody></table></div></section>`;
    else {const bundleRows=a31Array(bundles);body.innerHTML=`<section class="panel-card"><div class="card-header"><div><div class="card-title">Tool Bundles</div><div class="list-meta">Role-scoped tool groupings used by Skills, Agent Profiles, Teams and Workspace routing. Bundles request capability; policy remains authoritative.</div></div></div><div class="table-shell"><table class="data-table"><thead><tr><th>Bundle</th><th>ID</th><th>Tools</th><th>Count</th></tr></thead><tbody>${bundleRows.length?bundleRows.map(b=>{const tools=a31Array(b.Tools||b.tools),id=b.ID||b.id||b.Name||b.name||"";return `<tr><td><strong>${escapeHtml(b.Name||b.name||id||"Tool bundle")}</strong></td><td class="mono-cell">${escapeHtml(id)}</td><td>${escapeHtml(tools.join(", ")||"No tools declared")}</td><td>${tools.length}</td></tr>`}).join(""):'<tr><td colspan="4" class="muted-cell">No Tool Bundles are registered.</td></tr>'}</tbody></table></div></section>`;}
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
  let i=0,ended=false;
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
    const s=steps[i],card=$("#tourCard",root),overlay=$(".tour-overlay",root),target=s.target?$(s.target):null,spot=$("#tourSpotlight",root);if(!card||!overlay||!spot)return;
    const tr=target?.getBoundingClientRect(),pad=s.padding||8;
    if(target&&tr){
      overlay.dataset.focus="target";target.classList.add("tour-target");
      Object.assign(spot.style,{left:`${Math.max(0,tr.left-pad)}px`,top:`${Math.max(0,tr.top-pad)}px`,width:`${Math.max(8,tr.width+pad*2)}px`,height:`${Math.max(8,tr.height+pad*2)}px`,borderRadius:getComputedStyle(target).borderRadius||"12px"});
      spot.hidden=false;
    }else{overlay.dataset.focus="none";spot.hidden=true}
    const cr=card.getBoundingClientRect(),margin=18,host=$("#viewHost"),hr=host?.getBoundingClientRect();
    const usable={
      left:Math.max(margin,hr?.left??margin),
      top:Math.max(margin,hr?.top??margin),
      right:Math.min(innerWidth-margin,hr?.right??(innerWidth-margin)),
      bottom:Math.min(innerHeight-margin,hr?.bottom??(innerHeight-margin))
    };
    const clamp=p=>({left:Math.max(usable.left,Math.min(usable.right-cr.width,p.left)),top:Math.max(usable.top,Math.min(usable.bottom-cr.height,p.top))});
    const center=clamp({left:(usable.left+usable.right-cr.width)/2,top:(usable.top+usable.bottom-cr.height)/2});
    const targetRect=tr?{left:tr.left-pad-18,right:tr.right+pad+18,top:tr.top-pad-18,bottom:tr.bottom+pad+18}:null;
    const collides=p=>targetRect&&!(p.left+cr.width<=targetRect.left||p.left>=targetRect.right||p.top+cr.height<=targetRect.top||p.top>=targetRect.bottom);
    const fits=p=>p.left>=usable.left&&p.top>=usable.top&&p.left+cr.width<=usable.right&&p.top+cr.height<=usable.bottom;
    let pos=center;
    if(collides(pos)&&targetRect){
      const around=[
        {left:(usable.left+usable.right-cr.width)/2,top:targetRect.top-cr.height-margin},
        {left:(usable.left+usable.right-cr.width)/2,top:targetRect.bottom+margin},
        {left:targetRect.right+margin,top:(usable.top+usable.bottom-cr.height)/2},
        {left:targetRect.left-cr.width-margin,top:(usable.top+usable.bottom-cr.height)/2},
        {left:usable.left,top:usable.top},
        {left:usable.right-cr.width,top:usable.top},
        {left:usable.left,top:usable.bottom-cr.height},
        {left:usable.right-cr.width,top:usable.bottom-cr.height}
      ];
      pos=around.find(p=>fits(p)&&!collides(p))||clamp(around.find(p=>!collides(clamp(p)))||center);
    }
    Object.assign(card.style,{left:`${pos.left}px`,top:`${pos.top}px`});
    card.dataset.positioned="true";
  }
  async function draw(){
    document.querySelectorAll(".tour-target").forEach(x=>x.classList.remove("tour-target"));const s=steps[i];if(s.prepare)await s.prepare();root.innerHTML=`<div class="tour-overlay" data-focus="${s.target?'target':'none'}"><div id="tourSpotlight" class="tour-spotlight" hidden></div><section id="tourCard" class="tour-card a31-tour-card"><div class="tour-progress"><span>${i+1} / ${steps.length}</span><span>${Math.round((i+1)/steps.length*100)}%</span></div><h2>${escapeHtml(s.title)}</h2><p>${escapeHtml(s.body)}</p><div class="tour-actions"><button class="btn" id="tourSkip">${i===steps.length-1?'Close':'Skip tour'}</button><span class="tour-spacer"></span>${i?'<button class="btn" id="tourBack">Back</button>':""}<button class="btn primary" id="tourNext">${i===steps.length-1?'Finish':'Next'}</button></div></section></div>`;$("#tourSkip").onclick=finish;$("#tourBack")?.addEventListener("click",()=>{i--;draw()});$("#tourNext").onclick=()=>{if(i===steps.length-1)return finish();i++;draw()};requestAnimationFrame(()=>position());
  }
  window.addEventListener("resize",position);document.addEventListener("keydown",onKeyDown);
  Promise.resolve(draw()).catch(ex=>{console.error("Product tour failed",ex);cleanup();notice("Product tour could not start.","bad")});
};

/* Inspector: Overview is implicit; only show navigation when multiple views exist. */
const a31RenderInspectorBase=renderInspector;
renderInspector=function(){
  a31RenderInspectorBase();
  const root=$("#inspector");if(!root)return;
  const tabs=typeof qa4InspectorTabs==="function"?qa4InspectorTabs():["overview"];
  root.dataset.tabMode=tabs.length>1?"multi":"single";
  const add=$("#qa4InspectorAddTab"),header=root.querySelector(".inspector-header");
  if(add&&header){add.classList.add("inspector-header-add");add.title="Add Inspector view";header.appendChild(add)}
};

/* Shell bindings and final routing */
const a31BindShellBase=bindShell;
bindShell=function(){
  a31BindShellBase();
  $("#controlChatLauncher")?.addEventListener("click",()=>a31OpenControlChat());$("#controlChatClose")?.addEventListener("click",e=>{e.stopPropagation();a31CloseControlChat()});$("#controlChatToggle")?.addEventListener("click",()=>a31SetControlChatCollapsed(!state.controlChatCollapsed));$("#controlChatAssistantTab")?.addEventListener("click",()=>a31OpenControlChat("assistant"));$("#controlChatOrchestratorTab")?.addEventListener("click",()=>a31OpenControlChat("orchestrator"));
};
renderActiveView=async function(){
  const epoch=++qa31ViewEpoch,t=currentTab();if(!t)return;if(t.state==="suspended")t.state="active";const route=t.route;
  const renderers={operations:renderOperations,tasks:renderTasks,projects:renderProjects,models:renderModels,nodes:renderNodes,agents:renderAgents,skills:renderSkills,settings:renderSettings,secrets:renderSecrets,evidence:()=>renderPlaceholder("Evidence / Audit","Event Ledger, Artifacts, Observations and Verifications.")};
  try{await Promise.resolve((renderers[route]||renderOperations)())}finally{const host=$("#viewHost");if(epoch===qa31ViewEpoch){if(host)host.dataset.renderedRoute=route;renderNav()}}
};
bootOnePane().then(()=>window.onepaneReleaseSmoke?.()).catch(ex=>{try{window.chrome?.webview?.postMessage(`onepane-ui-e2e|FAIL|boot: ${String(ex?.message||ex)}`)}catch{}});
