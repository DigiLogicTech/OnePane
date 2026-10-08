let a10Search="",a10Level="all",a10Source="all",a10Paused=false,a10Snapshot=null,a10Following=true;
const a10DrawBase=renderDrawer;
function a10Events(){
 if(a10Paused){if(!a10Snapshot)a10Snapshot=[...liveOps.events];return a10Snapshot}
 return [...liveOps.events]
}
function a10LevelOf(e){const t=String(e.event_type||"");return /fail|error|blocked|unavailable|recovery/i.test(t)?"ERROR":/warn|approval|required|degraded|rate_limited/i.test(t)?"WARN":"INFO"}
function a10AllEntries(){
 const events=a10Events();
 if(activeDrawerTab!=="logs")return events;
 const jobs=(typeof a40ComponentJobs!=="undefined"?a40ComponentJobs:[]).map(j=>({
  _componentJob:j,event_type:"component."+j.action+"."+j.status,
  aggregate_type:j.component_id||"component",aggregate_id:j.id,
  sequence:"componentjob:"+j.id,occurred_at:j.updated_at,
  _summary:j.failure_reason||("Operation "+j.action+" · "+j.stage+" · "+j.status)
 }));
 const ids=new Set(jobs.map(j=>String(j.aggregate_id)));
 return [...events.filter(e=>!ids.has(String(e.aggregate_id))),...jobs];
}
function a10Filtered(){
 const q=a10Search.toLowerCase(),all=a10AllEntries().slice().sort((a,b)=>Number(b.occurred_at||0)-Number(a.occurred_at||0)).slice(0,500);
 return all.filter(e=>(a10Level==="all"||a10LevelOf(e)===a10Level)&&(a10Source==="all"||String(e.aggregate_type||"event")===a10Source)&&(!q||`${e.event_type||""} ${e.aggregate_type||""} ${e.aggregate_id||""} ${e.sequence||""} ${e._summary||""}`.toLowerCase().includes(q)))
}
function a10Text(e){return e._summary||`${e.event_type||"Event"} · ${e.aggregate_type||"event"} · ${e.aggregate_id||""}`}
function a10ListMarkup(){
 const events=a10Filtered(),mode=activeDrawerTab;
 if(!liveOpsReported("events")&&!(activeDrawerTab==="logs"&&events.length))return '<div class="empty-state compact">Operational Event Ledger unavailable. These entries have not been confirmed.</div>';
 if(!events.length)return '<div class="empty-state compact">No events match the selected filters.</div>';
 return `<table class="log-table"><tbody>${events.map(e=>{
  const ts=Number(e.occurred_at||0),time=ts?new Date(ts).toLocaleTimeString([],{hour:"2-digit",minute:"2-digit",second:"2-digit"}):"";
  const level=a10LevelOf(e),id=String(e.sequence||e.id||"");
  const detail=mode==="events"?`${e.aggregate_type||"event"} · ${e.aggregate_id||""}`:a10Text(e);
  return `<tr><td class="log-time">${escapeHtml(time)}</td><td class="log-level"><span class="pill ${level==="ERROR"?"bad":level==="WARN"?"warn":""}">${level}</span></td>
   <td class="log-component">${escapeHtml(e.aggregate_type||"event")}</td><td>${escapeHtml(mode==="events"?e.event_type||"Event":eventLabel(e))}<div class="list-meta">${escapeHtml(detail)}</div></td>
   <td><button class="btn tiny" data-a10-event="${escapeHtml(id)}" title="Inspect event">Details</button></td></tr>`;
 }).join("")}</tbody></table>`;
}
function a10CsvCell(v){const raw=String(v??"");const safe=/^[=+\-@]/.test(raw)?"'"+raw:raw;return '"'+safe.replaceAll('"','""')+'"'}
function a10ExportRows(){return [["timestamp","level","source","event_type","entity_id","sequence"],...a10Filtered().map(e=>[e.occurred_at||"",a10LevelOf(e),e.aggregate_type||"",e.event_type||"",e.aggregate_id||"",e.sequence||e.id||""])].map(row=>row.map(a10CsvCell).join(",")).join("\r\n")}
function a10Copy(){
 const text=a10Filtered().map(e=>`${new Date(Number(e.occurred_at||Date.now())).toLocaleString()} [${a10LevelOf(e)}] ${a10Text(e)}`).join("\n");
 navigator.clipboard?.writeText(text).then(()=>notice("Visible entries copied.")).catch(e=>notice(e.message,"bad"))
}
function a10Export(){
 const blob=new Blob([a10ExportRows()],{type:"text/csv;charset=utf-8"}),url=URL.createObjectURL(blob);
 const a=document.createElement("a");a.href=url;a.download="onepane-"+activeDrawerTab+"-"+new Date().toISOString().slice(0,10)+".csv";a.click();URL.revokeObjectURL(url)
}
function a10Bind(){
 const root=$("#drawerContent");if(!root)return;
 const query=$("#a10Search");
 if(query)query.oninput=()=>{a10Search=query.value;a10Fill()};
 const level=$("#a10Level");if(level)level.onchange=()=>{a10Level=level.value;a10Fill()};
 const src=$("#a10Source");if(src)src.onchange=()=>{a10Source=src.value;a10Fill()};
 $("#a10Pause")?.addEventListener("click",()=>{a10Paused=!a10Paused;a10Snapshot=a10Paused?[...liveOps.events]:null;renderDrawer()});
 $("#a10Follow")?.addEventListener("click",()=>{a10Following=!a10Following;renderDrawer()});
 $("#a10Copy")?.addEventListener("click",a10Copy);
 $("#a10Export")?.addEventListener("click",a10Export);
 a10BindRows()
}
function a10BindRows(){
 $$("[data-a10-event]",$("#drawerContent")||document).forEach(b=>b.onclick=()=>{
  const id=b.dataset.a10Event,e=a10AllEntries().find(x=>String(x.sequence||x.id||"")===id);
  if(e)qa4Inspect(e._componentJob?"component_job":"event",id,e._summary||eventLabel(e),e._componentJob||e)
 })
}
function a10Fill(){
 const host=$("#a10Rows");if(host){host.innerHTML=a10ListMarkup();a10BindRows();if(a10Following&&!a10Paused)host.scrollTop=0}
}
renderDrawer=function(){
 const hadSearch=document.activeElement?.id==="a10Search",selection=hadSearch?document.activeElement.selectionStart:null;
 a10DrawBase();
 const c=$("#drawerContent");if(!c)return;
 if(["logs","events"].includes(activeDrawerTab)){
  const sources=[...new Set(a10AllEntries().map(e=>String(e.aggregate_type||"event")))].sort();
  const title=activeDrawerTab==="logs"?"Unified operational logs":"Structured Event Ledger";
  c.innerHTML=`<div class="drawer-log-shell"><div class="drawer-tools">
    <div class="drawer-feed-label"><strong>${title}</strong><span class="list-meta">Event Ledger + managed component history · ${a10Paused?"Paused":"Live feed"}</span></div>
    <input id="a10Search" aria-label="Search log or event" placeholder="Search message, ID, source…" value="${escapeHtml(a10Search)}">
    <select id="a10Level" aria-label="Severity filter">${["all","INFO","WARN","ERROR"].map(x=>`<option value="${x}" ${a10Level===x?"selected":""}>${x==="all"?"All levels":x}</option>`).join("")}</select>
    <select id="a10Source" aria-label="Component filter"><option value="all">All components</option>${sources.map(x=>`<option value="${escapeHtml(x)}" ${a10Source===x?"selected":""}>${escapeHtml(x)}</option>`).join("")}</select>
    <button class="btn tiny" id="a10Pause">${a10Paused?"Resume":"Pause"}</button>
    <button class="btn tiny" id="a10Follow" aria-pressed="${a10Following}">${a10Following?"Follow: On":"Follow: Off"}</button>
    <button class="btn tiny" id="a10Copy">Copy</button><button class="btn tiny" id="a10Export">Export CSV</button>
   </div><div id="a10Rows" class="drawer-log-results">${a10ListMarkup()}</div></div>`;
  a10Bind();if(hadSearch){const input=$("#a10Search");input?.focus({preventScroll:true});if(selection!==null)input?.setSelectionRange(selection,selection)}return;
 }
 if(activeDrawerTab==="watchdog"){
  const updated=liveOps.lastRefresh?new Date(liveOps.lastRefresh).toLocaleString():"Not yet checked";
  c.insertAdjacentHTML("afterbegin",`<div class="drawer-health-note"><strong>Live operational health</strong><span class="page-subtitle">Last checked: ${escapeHtml(updated)} · Derived from the current control-plane feeds; not a separate Watchdog telemetry stream.</span></div>`)
 }
 if(activeDrawerTab==="metrics"){
  const updated=liveOps.lastRefresh?new Date(liveOps.lastRefresh).toLocaleString():"Not yet checked";
  c.insertAdjacentHTML("afterbegin",`<div class="drawer-health-note"><strong>Hardware snapshot</strong><span class="page-subtitle">Last refreshed: ${escapeHtml(updated)} · Run Detect hardware for updated device details.</span></div>`)
 }
 if(activeDrawerTab==="evidence"){
  const rows=a10Events().filter(e=>/evidence|audit|verification|assurance|receipt/i.test(String(e.event_type||"")+" "+String(e.aggregate_type||""))).slice(-50).reverse();
  c.innerHTML=`<div class="widget-body"><strong>Evidence & audit references</strong><p class="page-subtitle">Entries identified in the current Event Ledger. The complete assurance record remains in its owning service.</p>${rows.length?`<ul class="list">${rows.map(e=>`<li class="list-row"><button class="btn tiny" data-a10-event="${escapeHtml(e.sequence||e.id||"")}">Inspect</button><span>${escapeHtml(eventLabel(e))}</span></li>`).join("")}</ul>`:'<div class="empty-state compact">No evidence-related entries in the current Event Ledger window.</div>'}</div>`;
  a10BindRows()
 }
};
