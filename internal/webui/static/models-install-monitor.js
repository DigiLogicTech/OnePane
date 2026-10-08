/* Isolated durable install/download monitor for OnePane Models. */
let a31InstallMonitorTimer=null;
let a31ActiveInstallJobs=[];
async function a31LoadActiveInstallJobs(){
  if(!onepaneWorkspace)return [];
  return a31Array(await apiRequest(`/v1/local-ai/install-jobs?workspace_id=${encodeURIComponent(onepaneWorkspace)}`).catch(()=>[]))
}
function a31InstallJobTitle(j){return j.model_ref||"Local model install"}
function a31InstallJobProgressMarkup(j){
  const pct=Math.max(0,Math.min(100,Math.round(Number(j.progress_pct||0)))),done=Number(j.bytes_downloaded||0),total=Number(j.bytes_total||0);
  return `<div class="install-step active">${escapeHtml(j.current_artifact||titleCase(String(j.status||"queued").replaceAll("_"," ")))}</div><div class="install-progress-track"><span style="width:${pct}%"></span></div><div class="install-progress-meta"><span>${pct}%</span><span>${total>0?`${bytesQA(done)} / ${bytesQA(total)}`:""}</span></div>`
}
function a31DrawDownloadIndicator(){
  const n=a31ActiveInstallJobs.length,button=$("#modelDownloadButton"),mobile=$("#mobileModelDownloadButton");
  if(button){button.classList.toggle("hidden",n===0);$("#modelDownloadBadge").textContent=String(n);$("#modelDownloadLabel").textContent=n===1?"1 download":`${n} downloads`}
  if(mobile){mobile.classList.toggle("hidden",n===0);$("#mobileModelDownloadBadge").textContent=String(n)}
}
function a31OpenDownloadManager(){
  const rows=a31ActiveInstallJobs;
  openModal("Background model downloads",rows.length?`<div class="model-download-list">${rows.map(j=>`<div class="model-download-row"><div class="model-download-head"><div><strong>${escapeHtml(a31InstallJobTitle(j))}</strong><div class="list-meta">${escapeHtml(j.quantization||"")} · ${escapeHtml(titleCase(String(j.status||"queued")))}</div></div><button class="btn" data-a31-view-install="${escapeHtml(j.id)}">View progress</button></div><div class="model-download-progress"><span style="width:${Math.max(0,Math.min(100,Number(j.progress_pct||0)))}%"></span></div><div class="list-meta">${escapeHtml(j.current_artifact||"Queued")}</div></div>`).join("")}</div>`:'<div class="empty-state compact">No active model downloads.</div>');
  $$("[data-a31-view-install]").forEach(b=>b.onclick=()=>a31OpenInstallProgress(b.dataset.a31ViewInstall))
}
async function a31OpenInstallProgress(jobID){
  let first;
  try{first=await apiRequest(`/v1/local-ai/install-jobs/${encodeURIComponent(jobID)}`)}catch(ex){return notice(ex.message,"bad")}
  openModal("Model install progress",`<div class="widget-body"><strong>${escapeHtml(a31InstallJobTitle(first))}</strong><div class="list-meta">${escapeHtml(first.quantization||"")}</div><div id="a31BackgroundJobProgress">${a31InstallJobProgressMarkup(first)}</div></div>`);
  const poll=async()=>{
    const box=$("#a31BackgroundJobProgress");if(!box)return;
    try{
      const j=await apiRequest(`/v1/local-ai/install-jobs/${encodeURIComponent(jobID)}`);box.innerHTML=a31InstallJobProgressMarkup(j);
      if(["ready","failed","cancelled","interrupted"].includes(String(j.status)))return
    }catch(ex){box.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
    setTimeout(poll,900)
  };setTimeout(poll,900)
}
async function a31RefreshDownloadMonitor(){
  if(!onepaneWorkspace)return;
  const prev=new Map(a31ActiveInstallJobs.map(j=>[j.id,j]));
  const next=await a31LoadActiveInstallJobs();a31ActiveInstallJobs=next;a31DrawDownloadIndicator();
  const active=new Set(next.map(j=>j.id));
  for(const [id,old] of prev){
    if(active.has(id))continue;
    try{
      const done=await apiRequest(`/v1/local-ai/install-jobs/${encodeURIComponent(id)}`);
      if(done.status==="ready")notice(`${a31InstallJobTitle(done)} installed and ready.`);
      else if(done.status==="failed")notice(`${a31InstallJobTitle(done)} install failed: ${done.failure_reason||"unknown error"}`,"bad");
    }catch{}
  }
  if(a31RouteIs("models")&&a31ModelView==="local"&&prev.size!==next.length)setTimeout(()=>renderModels(),0)
}
function a31StartDownloadMonitor(){
  if(a31InstallMonitorTimer)return;
  $("#modelDownloadButton")?.addEventListener("click",a31OpenDownloadManager);
  $("#mobileModelDownloadButton")?.addEventListener("click",a31OpenDownloadManager);
  a31RefreshDownloadMonitor().catch(()=>{});
  a31InstallMonitorTimer=setInterval(()=>a31RefreshDownloadMonitor().catch(()=>{}),3000)
}

