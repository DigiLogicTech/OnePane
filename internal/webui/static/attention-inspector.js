let a9All=[],a9Statuses=new Map(),a9Filter="active",a9Workspace="",a9LastRead=0,a9Loading=null;
function a9Status(id){return a9Statuses.get(String(id))||"active"}
function a9RefreshState(){
 a9All=attentionFromLiveData();
 ATTENTION_ITEMS=a9All.filter(x=>a9Status(x.id)==="active");
 let read=[];try{read=JSON.parse(localStorage.getItem("onepane:read-notifications")||"[]")}catch{}
 NOTIFICATIONS=ATTENTION_ITEMS.map(n=>({...n,unread:!read.includes(n.id)}));
 syncNotificationBadges();
}
const a9SyncOld=syncLiveNotifications;
syncLiveNotifications=function(){a9SyncOld();a9RefreshState()};
async function a9Load(force=false){
 const ws=onepaneWorkspace;if(!ws)return;
 if(a9Workspace!==ws){a9Workspace=ws;a9Statuses=new Map();a9LastRead=0}
 if(!force&&Date.now()-a9LastRead<15000)return;
 if(a9Loading)return a9Loading;
 a9Loading=(async()=>{try{
  const result=await apiRequest("/v1/ui/attention?workspace_id="+encodeURIComponent(ws));
  if(a9Workspace!==ws)return;
  a9Statuses=new Map(a31Array(result.items).map(x=>[String(x.alert_id),x.disposition]));
  a9LastRead=Date.now();a9RefreshState()
 }catch(ex){console.warn("Attention state sync",ex)}})();
 try{await a9Loading}finally{a9Loading=null}
}
const a9RefreshOld=refreshOperationalDataQA;
refreshOperationalDataQA=async function(...args){const r=await a9RefreshOld(...args);await a9Load();return r};
function a9RenderAgain(){
 const route=currentTab()?.route;
 if(route==="operations")renderOperations();
 else if(route==="projects")renderProjects();
}
async function a9Change(id,status){
 if(!a9All.some(x=>x.id===id))return notice("Attention source no longer available.","bad");
 try{
  await apiRequest("/v1/ui/attention?workspace_id="+encodeURIComponent(onepaneWorkspace),{method:"POST",body:JSON.stringify({alert_id:id,disposition:status})});
  a9Statuses.set(id,status);a9RefreshState();a9RenderAgain();a9RefreshOpenPopover();
  if(qa4Inspector.kind==="attention"&&String(qa4Inspector.id)===id)renderInspector();
  notice(status==="archived"?"Attention archived; source event retained.":status==="acknowledged"?"Attention acknowledged.":"Attention restored.")
 }catch(ex){notice("Attention update failed: "+ex.message,"bad")}
}
// Refresh the list in place; preserve the current filter and scroll offset.
function a9RefreshOpenPopover(){
 const root=document.querySelector("#overlayRoot .notification-center");
 const panel=root?.querySelector(".attention-panel");if(!panel)return;
 const scroll=panel.scrollTop;
 panel.outerHTML=a9Panel();
 const fresh=root.querySelector(".attention-panel");
 if(fresh)fresh.scrollTop=scroll;
 a9Bind(root);
}
function a9Rows(rows){
 return rows.map(n=>{const status=a9Status(n.id);return `<li class="attention-item">
  <span class="pill ${escapeHtml(n.severity||"warn")}">!</span><div class="attention-copy">
  <strong>${escapeHtml(n.title)}</strong><div class="list-meta">${escapeHtml(n.detail||"")}</div>
  <div class="attention-row-actions"><button class="btn tiny" data-a9-inspect="${escapeHtml(n.id)}">Details</button>
  ${status==="active"?`<button class="btn tiny" data-a9-state="${escapeHtml(n.id)}:acknowledged">Acknowledge</button>`:""}
  <button class="btn tiny" data-a9-state="${escapeHtml(n.id)}:${status==="archived"?"active":"archived"}">${status==="archived"?"Restore":"Archive"}</button></div>
  </div></li>`}).join("")
}
function a9Panel(compact=false){
 const rows=a9All.filter(x=>a9Status(x.id)===a9Filter),counts=Object.fromEntries(["active","acknowledged","archived"].map(k=>[k,a9All.filter(x=>a9Status(x.id)===k).length]));
 return `<div class="attention-panel"><div class="attention-toolbar">${["active","acknowledged","archived"].map(k=>`<button class="btn tiny ${a9Filter===k?"attention-selected":""}" data-a9-filter="${k}">${titleCase(k)} ${counts[k]}</button>`).join("")}</div>
  ${a9Filter==="acknowledged"&&counts.acknowledged?'<button class="btn tiny" data-a9-bulk="archived">Archive acknowledged</button>':""}
  ${!liveOpsAttentionReported()?'<p class="page-subtitle">Operational feeds incomplete; some attention may be missing.</p>':""}
  ${rows.length?`<ul class="attention-list">${a9Rows(compact?rows.slice(0,6):rows)}</ul>`:`<div class="empty-state compact">No ${escapeHtml(a9Filter)} attention items.</div>`}
  <p class="page-subtitle">Archiving never deletes source events or audit history.</p></div>`
}
attentionCard=function(){return card(`Attention (${a9All.filter(x=>a9Status(x.id)==="active").length})`,a9Panel())};
const a9WorkspaceOld=qa6ComponentContent;
qa6ComponentContent=function(w,p,ws){return w.type==="attention"?a9Panel(true):a9WorkspaceOld(w,p,ws)};
const a9EntityOld=qa4FindEntity;
qa4FindEntity=function(kind,id){return kind==="attention"?(a9All.find(x=>x.id===String(id))||qa4Inspector.data||{}):a9EntityOld(kind,id)};
function a9Inspect(id){const n=a9All.find(x=>x.id===id);if(n)qa4Inspect("attention",id,n.title,n);else notice("Attention item no longer in feed.","bad")}
function a9Bind(root=document){
 $$("[data-a9-filter]",root).forEach(b=>b.onclick=e=>{e.stopPropagation();a9Filter=b.dataset.a9Filter;a9RenderAgain();a9RefreshOpenPopover()});
 $$("[data-a9-inspect]",root).forEach(b=>b.onclick=e=>{e.stopPropagation();a9Inspect(b.dataset.a9Inspect)});
 $$("[data-a9-state]",root).forEach(b=>b.onclick=e=>{e.stopPropagation();const v=b.dataset.a9State,i=v.lastIndexOf(":");if(i>=0)a9Change(v.slice(0,i),v.slice(i+1))});
 $$("[data-a9-bulk]",root).forEach(b=>b.onclick=async e=>{e.stopPropagation();for(const n of a9All.filter(x=>a9Status(x.id)==="acknowledged"))await a9Change(n.id,"archived")})
}
const a9BindOld=bindViewActions;
bindViewActions=function(root=document){a9BindOld(root);a9Bind(root)};
openAttentionPopover=async function(anchor){
 await refreshOperationalDataQA(true).catch(()=>{});
 popoverFor(anchor,`<div class="popover notification-center"><div class="popover-title-row"><h3>Attention</h3></div>${a9Panel()}</div>`);
 a9Bind($("#overlayRoot"))
};
function a9Field(k,v){return v==null||v===""?"":`<div class="inspector-info-row"><span>${escapeHtml(k)}</span><strong>${escapeHtml(String(v))}</strong></div>`}
const a9OverviewOld=qa4InspectorOverview;
qa4InspectorOverview=function(){
 const kind=qa4Inspector.kind;
 if(!["attention","task","node","provider","event"].includes(kind))return a9OverviewOld();
 const d=qa4FindEntity(kind,qa4Inspector.id)||{},attention=kind==="attention",ev=kind==="event";
 const title=attention?d.title:kind==="task"?(d.objective||d.name):kind==="node"?(d.hostname||d.display_name):kind==="provider"?(d.display_name||d.provider):eventLabel(d);
 const status=attention?a9Status(d.id):d.status||d.state||d.event_type||"Recorded";
 const props=attention?[["Source",d.route],["Alert ID",d.id],["Severity",d.severity]]:
  ev?[["Source",d.aggregate_type],["Entity ID",d.aggregate_id],["Time",d.occurred_at?new Date(Number(d.occurred_at)).toLocaleString():""],["Event ID",d.id||d.sequence]]:
  kind==="node"?[["Hostname",d.hostname||d.display_name],["Node ID",d.id||d.node_id],["Last contact",d.last_seen_at]]:
  kind==="provider"?[["Provider",d.provider],["Authentication",d.auth_type],["Last probe",d.last_probe_at]]:
  [["Task ID",d.id],["Assigned model",d.model_ref||d.model_id],["Priority",d.priority],["Updated",d.updated_at]];
 return `<div class="inspector-context"><div class="inspector-context-top"><strong>${escapeHtml(title||titleCase(kind))}</strong><span class="pill">${escapeHtml(String(status))}</span></div>
  ${attention?`<p class="page-subtitle">${escapeHtml(d.detail||"")}</p>`:""}
  <section class="spec-readable-section"><h3>Key information</h3><div class="inspector-info">${props.map(([k,v])=>a9Field(k,v)).join("")}</div></section>
  ${attention?`<div class="toolbar"><button class="btn" data-a9-state="${escapeHtml(d.id)}:acknowledged">Acknowledge</button><button class="btn" data-a9-state="${escapeHtml(d.id)}:${a9Status(d.id)==="archived"?"active":"archived"}">${a9Status(d.id)==="archived"?"Restore":"Archive"}</button><button class="btn" data-a9-source="${escapeHtml(d.route||"operations")}">Open source</button></div>`:""}
  <details class="spec-technical-details"><summary>Advanced technical details</summary><pre class="spec-sheet-json">${escapeHtml(JSON.stringify(d,null,2))}</pre></details></div>`
};
const a9InspectorOld=renderInspector;
renderInspector=function(){
 a9InspectorOld();const root=$("#inspector");if(!root)return;a9Bind(root);
 $$("[data-a9-source]",root).forEach(b=>b.onclick=()=>openRoute(b.dataset.a9Source))
};
