/* Real Evidence / Audit read surface for the authorised tenancy Workspace.
 * Events are returned by /v1/events only after an events.read policy check.
 * This is a bounded recent ledger, NOT a replacement for immutable evidence,
 * artifact content or the independent verification APIs.
 */
let a51AuditRows=[];

function a51AuditText(value){
 return escapeHtml(value===null||value===undefined?"—":String(value));
}
function a51PaintAudit(){
 const host=$("#viewHost");
 if(!host||currentTab()?.route!=="evidence")return;
 const type=host.querySelector("#a51AuditType")?.value||"";
 const query=(host.querySelector("#a51AuditFilter")?.value||"").trim().toLowerCase();
 const items=a51AuditRows.filter(row=>{
  if(type&&row.event_type!==type)return false;
  if(!query)return true;
  return [row.event_type,row.aggregate_type,row.aggregate_id,row.actor_principal_id,row.id,row.trace_id]
   .some(value=>String(value||"").toLowerCase().includes(query));
 });
 const count=host.querySelector("#a51AuditCount");
 if(count)count.textContent=items.length+" of "+a51AuditRows.length+" recent records";
 const list=host.querySelector("#a51AuditList");
 if(!list)return;
 if(!items.length){
  list.innerHTML='<div class="empty-state compact">No events match the current filters.</div>';
  return;
 }
 list.innerHTML=items.map(row=>{
  const time=Number(row.occurred_at)>0?
   new Date(Number(row.occurred_at)).toLocaleString():"Unknown time";
  const payload=JSON.stringify(row.payload===undefined?null:row.payload,null,2);
  const shown=payload.length>6000?payload.slice(0,6000)+"\n…truncated in this view":payload;
  return '<article class="panel-card" data-a51-sequence="'+Number(row.sequence||0)+'">'+
   '<div class="card-header"><div><strong>'+a51AuditText(row.event_type)+'</strong>'+
   '<div class="list-meta">'+a51AuditText(time)+' · #'+Number(row.sequence||0)+'</div></div>'+
   '<span class="pill">'+a51AuditText(row.aggregate_type)+'</span></div>'+
   '<div class="list-meta">Object: '+a51AuditText(row.aggregate_id)+
   ' · Actor: '+a51AuditText(row.actor_principal_id)+'</div>'+
   '<details><summary>Recorded event metadata and payload</summary>'+
   '<div class="list-meta">Event: '+a51AuditText(row.id)+
   ' · Trace: '+a51AuditText(row.trace_id)+
   ' · Request: '+a51AuditText(row.request_id)+'</div>'+
   '<pre class="log-entry">'+a51AuditText(shown)+'</pre></details></article>';
 }).join("");
}
async function a51RenderAudit(){
 const host=$("#viewHost");
 if(!host)return;
 const workspaceID=String(onepaneWorkspace||"");
 host.innerHTML='<section class="page a51-evidence-page">'+
  pageHeader("Evidence / Audit","Recent, permission-checked backend event records for the current OnePane tenancy Workspace.")+
  '<p class="list-meta">This is a read-only event ledger, not the complete artifact/evidence/verification viewer. Only the latest 100 records are shown; historical search and linked evidence inspection are still being developed.</p>'+
  '<div class="toolbar"><button class="btn" type="button" id="a51AuditRefresh">Refresh</button>'+
  '<label>Event type <select id="a51AuditType"><option value="">All types</option></select></label>'+
  '<label>Filter records <input id="a51AuditFilter" type="search" placeholder="Type, object, actor or trace" maxlength="256"></label></div>'+
  '<p class="list-meta" id="a51AuditCount" aria-live="polite">Loading verified event records…</p>'+
  '<section id="a51AuditList" class="a51-audit-list"></section></section>';
 host.querySelector("#a51AuditRefresh").onclick=()=>a51RenderAudit();
 if(!workspaceID){
  host.querySelector("#a51AuditCount").textContent="No active tenancy Workspace";
  host.querySelector("#a51AuditList").innerHTML='<div class="empty-state">Select an active OnePane Workspace to view its event ledger.</div>';
  return;
 }
 let rows;
 try{
  rows=await apiRequest("/v1/events?workspace_id="+encodeURIComponent(workspaceID)+"&latest=1&limit=100");
 }catch(error){
  if(host.isConnected&&currentTab()?.route==="evidence"){
   host.querySelector("#a51AuditCount").textContent="Event ledger unavailable";
   host.querySelector("#a51AuditList").innerHTML='<div class="error" role="alert">'+
    a51AuditText(error.message||"The current user lacks events.read permission.")+'</div>';
  }
  return;
 }
 if(!host.isConnected||currentTab()?.route!=="evidence")return;
 a51AuditRows=(Array.isArray(rows)?rows:[]).slice(0,100).sort((a,b)=>Number(b.sequence||0)-Number(a.sequence||0));
 const types=[...new Set(a51AuditRows.map(x=>x.event_type).filter(Boolean))].sort();
 host.querySelector("#a51AuditType").insertAdjacentHTML("beforeend",
  types.map(type=>'<option value="'+a51AuditText(type)+'">'+a51AuditText(type)+'</option>').join(""));
 host.querySelector("#a51AuditType").onchange=a51PaintAudit;
 host.querySelector("#a51AuditFilter").oninput=a51PaintAudit;
 a51PaintAudit();
}
const a51PreviousRenderActiveView=renderActiveView;
renderActiveView=async function(){
 const tab=currentTab();
 if(tab?.route!=="evidence")return a51PreviousRenderActiveView();
 const epoch=++qa31ViewEpoch;
 if(tab.state==="suspended")tab.state="active";
 try{await a51RenderAudit()}
 finally{
  if(epoch===qa31ViewEpoch){
   const host=$("#viewHost");
   if(host)host.dataset.renderedRoute="evidence";
   renderNav();
  }
 }
};
