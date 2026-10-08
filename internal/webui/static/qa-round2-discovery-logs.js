/* QA Round 2: source-authoritative download availability and persistent managed-component history. */
const a40DownloadCache = new Map();
let a40InspectQueue = [], a40ActiveInspect = 0, a40Observer = null;
const a40InspectPending=new Set(),a40InspectCallbacks=new Map();
function a40QueueAvailability(source,id,done){
 const key=source+":"+id;if(!source||!id||a40DownloadCache.has(key))return;
 if(typeof done==="function"){
  if(!a40InspectCallbacks.has(key))a40InspectCallbacks.set(key,new Set());
  a40InspectCallbacks.get(key).add(done);
 }
 if(a40InspectPending.has(key))return;
 if(source==="llmfit"){
  a40DownloadCache.set(key,{state:"no",label:"No direct artifact",message:"llmfit supplies hardware estimates, not a downloadable model artifact."});
  const callbacks=a40InspectCallbacks.get(key);a40InspectCallbacks.delete(key);
  for(const cb of callbacks||[])cb();return;
 }
 a40InspectPending.add(key);a40InspectQueue.push({source,id,key});a40DrainInspectionQueue();
}
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
  a40ActiveInspect++;
  (async()=>{
   let result;
   try{
    const inspected=await apiRequest(`/v1/local-ai/discovery/inspect?source=${encodeURIComponent(task.source)}&id=${encodeURIComponent(task.id)}`);
    const downloadable=!!inspected.can_adopt&&a31Array(inspected.artifacts).length>0;
    result={state:downloadable?"yes":"no",label:downloadable?"Downloadable · verify":"Unavailable",message:inspected.message||"No verified GGUF artifact available from this source."};
   }catch(e){result={state:"error",label:"Check unavailable",message:e.message||"Artifact lookup failed."}}
   a40DownloadCache.set(task.key,result);a40InspectPending.delete(task.key);
   const callbacks=a40InspectCallbacks.get(task.key);a40InspectCallbacks.delete(task.key);
   for(const tile of document.querySelectorAll(".external-model-tile[data-a40-source]")){
    if(tile.dataset.a40Source===task.source&&tile.dataset.a40Id===task.id)a40DisplayAvailability(tile,result);
   }
   for(const cb of callbacks||[])cb();
  })().finally(()=>{a40ActiveInspect--;a40DrainInspectionQueue()});
 }
}
function a40ScheduleAvailabilityChecks(){
 const root=document.querySelector("#a31DiscoverCatalog");if(!root)return;
 if(a40Observer){a40Observer.disconnect();a40Observer=null}
 const elements=[...root.querySelectorAll(".external-model-tile[data-a40-source]")];
 const queue=tile=>{
  const source=tile.dataset.a40Source,id=tile.dataset.a40Id,key=source+":"+id;
  if(!source||!id)return;
  const old=a40DownloadCache.get(key);if(old){a40DisplayAvailability(tile,old);return}
  a40QueueAvailability(source,id);
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
 if(!a40JobFetching&&Date.now()-a40JobFetchAt>10000){
  a40JobFetching=true;a40JobFetchAt=Date.now();
  apiRequest("/v1/local-ai/component-jobs").then(jobs=>{
   a40ComponentJobs=a31Array(jobs);
   if(activeDrawerTab==="logs")renderDrawer();
  }).catch(e=>{const c=document.querySelector("#drawerContent");if(c)c.insertAdjacentHTML("beforeend",`<div class="error">${escapeHtml(e.message)}</div>`)}).finally(()=>{a40JobFetching=false});
 }
};
