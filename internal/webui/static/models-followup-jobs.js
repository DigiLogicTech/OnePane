const A38_REMOTE_STORAGE="onepane_remote_model_jobs_v1";
let a38RemoteJobs=[];
try{a38RemoteJobs=JSON.parse(localStorage.getItem(A38_REMOTE_STORAGE)||"[]");if(!Array.isArray(a38RemoteJobs))a38RemoteJobs=[]}catch{a38RemoteJobs=[]}
function a38SaveRemoteJobs(){localStorage.setItem(A38_REMOTE_STORAGE,JSON.stringify(a38RemoteJobs.slice(-50)))}
// Node selection, quantization and compute now share the primary Download & Install modal.
let a38Components={},a38MonitorRunning=false;
const a38BaseComponentButtons=a31ComponentButtons;
a31ComponentButtons=function(id,c){
 if(c?.active_job_id)return `<button class="btn primary" data-a38-view-component="${escapeHtml(id)}">View progress</button>`;
 return a38BaseComponentButtons(id,c)
};
const a38BaseBindComponents=a31BindComponentButtons;
a31BindComponentButtons=function(root=document,components={}){
 a38BaseBindComponents(root,components);
 $$("[data-a38-view-component]",root).forEach(b=>b.onclick=()=>a38ViewComponent(b.dataset.a38ViewComponent))
};
function a38InstallRows(rows){
 return rows.map(x=>`<div class="model-download-row"><div class="model-download-head"><strong>${escapeHtml(x.name||x.id)}</strong><span class="pill">${escapeHtml(x.stage||x.status||"Queued")}</span></div>${uiProgressMarkup({label:x.name||x.id,stage:x.stage||x.status||"Working",percent:x.progress_pct??null,done:x.bytes_downloaded||0,total:x.bytes_total||0})}${x.error?`<div class="error">${escapeHtml(x.error)}</div>`:""}</div>`).join("")
}
async function a38ViewComponent(id){
 const state=a38Components[id];if(!state?.active_job_id){notice("No active installation for "+id);return}
 const jobID=state.active_job_id;
 openModal(a31ComponentName(id)+" installation",'<div id="a38JobProgress" class="model-download-list">Loading current stage…</div>');
 const poll=async()=>{
  const box=$("#a38JobProgress");if(!box)return;
  try{
   const job=await apiRequest(`/v1/local-ai/component-jobs/${encodeURIComponent(jobID)}`);
   box.innerHTML=a38InstallRows([{id,name:a31ComponentName(id),stage:job.stage||job.status,error:job.failure_reason,progress_pct:job.progress_pct,bytes_downloaded:job.bytes_downloaded,bytes_total:job.bytes_total}]);
   if(["succeeded","failed","interrupted"].includes(job.status))return
  }catch(e){box.innerHTML=`<div class="error">${escapeHtml(e.message)}</div>`;return}
  setTimeout(poll,1200)
 };poll()
}
const a38BaseDownloadManager=a31OpenDownloadManager;
function a38OpenAllInstallProgress(){
 a38BaseDownloadManager();
 const body=$("#overlayRoot .qa-modal-body");if(!body)return;
 const jobs=Object.entries(a38Components).filter(([,c])=>c.active_job_id).map(([id,c])=>({id,name:a31ComponentName(id),stage:c._stage||c.state}));
 const remotes=a38RemoteJobs.filter(j=>!["ready","failed","cancelled"].includes(j.status)).map(j=>({id:j.job_id,name:j.model_ref+" · "+j.node_id,stage:j.status}));
 if(!jobs.length&&!remotes.length)return;
 body.insertAdjacentHTML("beforeend",`<div class="subsection-title">Runtime and remote installations</div><div class="model-download-list">${a38InstallRows([...jobs,...remotes])}</div><div class="toolbar">${jobs.map(j=>`<button class="btn" data-a38-view-component="${escapeHtml(j.id)}">View ${escapeHtml(j.name)}</button>`).join("")}</div>`);
 $$("[data-a38-view-component]",body).forEach(b=>b.onclick=()=>a38ViewComponent(b.dataset.a38ViewComponent))
}
a31OpenDownloadManager=a38OpenAllInstallProgress;
const a38BaseDownloadIndicator=a31DrawDownloadIndicator;
a31DrawDownloadIndicator=function(){
 a38BaseDownloadIndicator();
 const extra=Object.values(a38Components).filter(c=>c.active_job_id).length+a38RemoteJobs.filter(j=>!["ready","failed","cancelled"].includes(j.status)).length;
 if(!extra)return;
 const total=extra+a31ActiveInstallJobs.length,desk=$("#modelDownloadButton"),mobile=$("#mobileModelDownloadButton");
 if(desk){desk.classList.remove("hidden");$("#modelDownloadBadge").textContent=String(total);$("#modelDownloadLabel").textContent=`Installing · ${total}`}
 if(mobile){mobile.classList.remove("hidden");$("#mobileModelDownloadBadge").textContent=String(total)}
};
async function a38PollManagedInstalls(){
 if(a38MonitorRunning)return;a38MonitorRunning=true;
 try{
  const results=await apiRequest("/v1/local-ai/components"),previous=a38Components;
  a38Components={};
  for(const [id,c] of Object.entries(results||{})){
   if(!c||typeof c!=="object")continue;
   a38Components[id]=c;
   if(c.active_job_id){
    try{
     const j=await apiRequest(`/v1/local-ai/component-jobs/${encodeURIComponent(c.active_job_id)}`);
     c._stage=j.stage||j.status;
     const status=$("#a31-"+id+"-status");if(status)status.textContent=titleCase(String(c._stage).replaceAll("_"," "));
     // Keep the current tile accurate immediately, not only after a page
     // revisit. Existing installers remain owned by their durable job ID.
     $('[data-a31-component]').filter(b=>b.dataset.a31Component===id+":install").forEach(b=>{
      b.removeAttribute("data-a31-component");b.dataset.a38ViewComponent=id;
      b.textContent="View progress";b.onclick=()=>a38ViewComponent(id)
     });
    }catch{}
   }else if(previous[id]?.active_job_id){
    if(c.state==="failed")notice(a31ComponentName(id)+" install failed: "+(c.last_error||"unknown error"),"bad");
    else notice(a31ComponentName(id)+" lifecycle completed.");
    if(a31RouteIs("models"))renderModels()
   }
  }
  for(const j of a38RemoteJobs){
   if(["ready","failed","cancelled"].includes(j.status))continue;
   try{
    const job=await apiRequest(`/v1/nodes/${encodeURIComponent(j.node_id)}/model-install-jobs/${encodeURIComponent(j.job_id)}`);
    j.status=job.status||j.status;
    if(["ready","failed","cancelled"].includes(j.status))notice(j.model_ref+" on "+j.node_id+": "+j.status,j.status==="ready"?"good":"bad")
   }catch{}
  }
  a38SaveRemoteJobs();a31DrawDownloadIndicator()
 }catch{}finally{a38MonitorRunning=false}
}
const a38StartDownloadBase=a31StartDownloadMonitor;
a31StartDownloadMonitor=function(){
 a38StartDownloadBase();
 if(!a38PollManagedInstalls.timer){a38PollManagedInstalls.timer=setInterval(a38PollManagedInstalls,3500);a38PollManagedInstalls()}
};
