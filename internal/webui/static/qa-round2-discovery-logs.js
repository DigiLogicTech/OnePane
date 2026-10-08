/* QA Round 2: source-authoritative download availability and persistent managed-component history. */
const a40DownloadCache = new Map();
let a40InspectQueue = [], a40ActiveInspect = 0, a40Observer = null;
function a40DisplayAvailability(card, record) {
 if (!card?.isConnected) return;
 const badge=card.querySelector(".a40-download-status"), action=card.querySelector("[data-a31-verify-source]");
 if (!badge) return;
 badge.classList.remove("good","warn","bad");
 badge.textContent=record.label;
 if(record.state==="yes")badge.classList.add("good");
 if(record.state==="no"||record.state==="error")badge.classList.add("warn");
 badge.title=record.message||record.label;
 if(action&&record.state==="yes")action.textContent="Verify & install";
 if(action&&record.state==="no")action.textContent="View details";
}
async function a40DrainInspectionQueue() {
 while(a40ActiveInspect<3&&a40InspectQueue.length){
  const task=a40InspectQueue.shift();
  if(!task.card.isConnected)continue;
  a40ActiveInspect++;
  (async()=>{
   let result;
   try{
    const inspected=await apiRequest(`/v1/local-ai/discovery/inspect?source=${encodeURIComponent(task.source)}&id=${encodeURIComponent(task.id)}`);
    const downloadable=!!inspected.can_adopt&&a31Array(inspected.artifacts).length>0;
    result={state:downloadable?"yes":"no",label:downloadable?"Downloadable · verify":"Unavailable",message:inspected.message||"No verified GGUF artifact available from this source."};
   }catch(e){result={state:"error",label:"Check unavailable",message:e.message||"Artifact lookup failed."}}
   a40DownloadCache.set(task.key,result);
   for(const tile of document.querySelectorAll(".external-model-tile[data-a40-source]")){
    if(tile.dataset.a40Source===task.source&&tile.dataset.a40Id===task.id)a40DisplayAvailability(tile,result);
   }
  })().finally(()=>{a40ActiveInspect--;a40DrainInspectionQueue()});
 }
}
function a40ScheduleAvailabilityChecks(){
 const root=document.querySelector("#a31DiscoverCatalog");if(!root)return;
 if(a40Observer){a40Observer.disconnect();a40Observer=null}
 a40InspectQueue=[];
 const elements=[...root.querySelectorAll(".external-model-tile[data-a40-source]")];
 const queue=tile=>{
   const source=tile.dataset.a40Source,id=tile.dataset.a40Id,key=source+":"+id;
   if(!id||!source)return;
   const old=a40DownloadCache.get(key);
   if(old){a40DisplayAvailability(tile,old);return}
   if(source==="llmfit"){
    const advisory={state:"no",label:"No direct artifact",message:"llmfit is a hardware advisory, not a downloadable model source."};
    a40DownloadCache.set(key,advisory);a40DisplayAvailability(tile,advisory);return;
   }
   if(tile.dataset.a40Queued==="1")return;
   tile.dataset.a40Queued="1";
   a40InspectQueue.push({source,id,key,card:tile});
   a40DrainInspectionQueue();
 };
 if(typeof IntersectionObserver!=="undefined"){
  a40Observer=new IntersectionObserver(entries=>{for(const entry of entries)if(entry.isIntersecting){a40Observer?.unobserve(entry.target);queue(entry.target)}},{root,rootMargin:"120px 0px",threshold:0});
  for(const tile of elements){const record=a40DownloadCache.get(tile.dataset.a40Source+":"+tile.dataset.a40Id);if(record)a40DisplayAvailability(tile,record);else a40Observer.observe(tile)}
 }else elements.slice(0,12).forEach(queue);
}
let a40ComponentJobs=[],a40JobFetchAt=0,a40JobFetching=false;
const a40BaseRenderDrawer=renderDrawer;
renderDrawer=function(){
 a40BaseRenderDrawer();
 if(activeDrawerTab!=="logs")return;
 const host=document.querySelector("#drawerContent");if(!host)return;
 const selectedLevel=document.querySelector("#logLevel")?.value?.toLowerCase()||"all levels";
 const rows=a40ComponentJobs.filter(j=>selectedLevel==="all levels"||(selectedLevel==="error"&&j.status==="failed")||(selectedLevel==="info"&&j.status!=="failed")||(selectedLevel==="warn"&&j.status==="interrupted"));
 const html=rows.map(j=>{
  const bad=j.status==="failed",warn=j.status==="interrupted";
  const when=j.updated_at?new Date(Number(j.updated_at)).toLocaleString():"";
  const desc=j.failure_reason||(`Operation ${j.action} · ${j.stage||j.status}`);
  return `<tr><td class="log-time">${escapeHtml(when)}</td><td class="log-level"><span class="pill ${bad?"bad":warn?"warn":""}">${bad?"ERROR":warn?"WARN":"INFO"}</span></td><td class="log-component">${escapeHtml(j.component_id||"component")}</td><td>${escapeHtml(desc)}<div class="list-meta">Job ${escapeHtml(j.id)} · ${escapeHtml(j.status)}</div></td></tr>`;
 }).join("");
 host.insertAdjacentHTML("afterbegin",`<section class="a40-component-history"><div class="list-meta"><strong>Managed component history</strong> · retained after notifications disappear</div>${rows.length?`<table class="log-table"><tbody>${html}</tbody></table>`:'<div class="list-meta">No recent component operations recorded.</div>'}</section>`);
 if(!a40JobFetching&&Date.now()-a40JobFetchAt>10000){
  a40JobFetching=true;a40JobFetchAt=Date.now();
  apiRequest("/v1/local-ai/component-jobs").then(jobs=>{a40ComponentJobs=a31Array(jobs);if(activeDrawerTab==="logs")renderDrawer()}).catch(e=>{const h=document.querySelector(".a40-component-history");if(h)h.insertAdjacentHTML("beforeend",`<div class="error">${escapeHtml(e.message)}</div>`)}).finally(()=>{a40JobFetching=false});
 }
};
