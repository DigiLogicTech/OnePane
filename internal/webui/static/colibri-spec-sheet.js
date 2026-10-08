/* Readable Colibri extension for the Model Spec Sheet.
 * Configured policy, projected placement and observed model residency are
 * intentionally separate; do not fabricate live per-tier byte counters.
 */
const a43ColibriSpecCache=new Map();
function a43ColibriDeployment(dep,spec={}){
 const names=[dep?.runtime_name,dep?.runtime_backend,spec?.runtime_name,spec?.runtime_backend];
 return names.some(v=>String(v||"").toLowerCase().includes("colibri"));
}
function a43ColibriValue(v,label0="Automatic"){
 return v==null||v===""?label0:String(v);
}
function a43ColibriMemory(v){return Number(v)>0?Number(v).toLocaleString()+" GB":"Automatic / not reserved";}
function a43ColibriNumber(v,unit,zero){return Number(v)>0?Number(v).toLocaleString()+" "+unit:zero;}
function a43ColibriSpecMarkup(dep){
 const id=String(dep?.deployment_id||"");
 const status=a43ColibriSpecCache.get(id);
 if(!status)return a37Section("Colibri: VRAM · RAM · SSD",
   '<p class="page-subtitle">Loading configured placement and whole-model residency from the active node…</p>');
 const tier=status.tier?.settings||{},swap=status.swap||{},loaded=Array.isArray(swap.active_deployment_ids)?swap.active_deployment_ids:[];
 const mode=String(tier.mode||"automatic");
 const backend=String(tier.backend||"auto");
 const assigned=loaded.includes(id);
 const residency=status.swapError?"Unavailable — "+status.swapError:
   assigned?("Resident · "+String(swap.status||"active")):
   "Not resident · "+String(swap.status||"stopped");
 const settings=status.tierError?
   a37Field("Tier configuration","Unavailable — "+status.tierError):
   [
    a37Field("Placement strategy",mode==="automatic"?"Automatic":mode==="balanced"?"Balanced · adaptive expert cache":mode==="manual"?"Manual · operator-set cache budgets":mode),
    a37Field("Selected backend",backend==="auto"?"Automatic (resolved at launch)":backend.toUpperCase()),
    a37Field("GPU index",["cuda","vulkan"].includes(backend)?String(tier.gpu_index??0):"Not explicitly selected"),
    a37Field("Warm RAM expert budget",a43ColibriMemory(tier.ram_gb)),
    a37Field("Warm RAM expert cap",a43ColibriNumber(tier.expert_cap,"experts","Automatic / not capped")),
    a37Field("Adaptive re-pin interval",a43ColibriNumber(tier.repin_tokens,"tokens","Disabled")),
    a37Field("Vulkan VRAM expert target",backend==="vulkan"?a43ColibriNumber(tier.vulkan_experts,"experts","Disabled"):"Not applicable to selected backend"),
    a37Field("Placement-plan command",status.tier?.plan_available?"Available (read-only)":"Unavailable · "+String(status.tier?.plan_note||"Colibri launcher not detected"))
   ].join("");
 const observed=[
  a37Field("Whole-model residency",residency),
  a37Field("Resident Colibri models on node",status.swapError?"Unknown":String(loaded.length)),
  a37Field("VRAM/RAM/SSD expert cache bytes","Not measured by OnePane"),
  a37Field("Measured speed and context","See Agent Check / performance above — not estimated from placement")
 ].join("");
 return a37Section("Colibri: tiered memory and hot swap",
  '<p class="page-subtitle">Hot VRAM / warm RAM / disk-backed experts. Configured limits are not measurements; a placement plan is an estimate, not proof of active residency.</p>'+
  '<h4>Configured placement</h4>'+settings+'<h4>Runtime residency</h4>'+observed+
  '<div class="toolbar">'+
   '<button type="button" class="btn" data-a43-tier="'+escapeHtml(id)+'">Configure tiering</button>'+
   '<button type="button" class="btn" data-a43-plan="'+escapeHtml(id)+'" '+(status.tier?.plan_available?"":"disabled")+'>View read-only plan</button>'+
   '<button type="button" class="btn" data-a43-swap="'+escapeHtml(id)+'">Hot swap model</button>'+
  '</div><p class="page-subtitle">OnePane permits one resident Colibri model per node in this release. Busy requests are protected; failed swaps attempt rollback. Hot swap does not change chat model routing.</p>');
}
const a43PreviousModelSpec=qa4InspectorOverview;
qa4InspectorOverview=function(){
 const html=a43PreviousModelSpec();
 if(qa4Inspector.kind!=="model")return html;
 const dep=qa4Inspector.data||{};
 if(!a43ColibriDeployment(dep,dep.spec||{}))return html;
 const section=a43ColibriSpecMarkup(dep);
 const anchor='<details class="spec-technical-details"';
 const idx=html.indexOf(anchor);
 if(idx>=0)return html.slice(0,idx)+section+html.slice(idx);
 const agentButton='<button class="btn primary" id="qa5InspectorAgentCheck"';
 const pos=html.indexOf(agentButton);
 if(pos>=0)return html.slice(0,pos)+section+html.slice(pos);
 return html+section;
};
const a43PreviousInspectModel=qa5InspectModel;
qa5InspectModel=async function(dep){
 if(!dep?.deployment_id)return;
 await a43PreviousInspectModel(dep);
 const targetID=String(dep.deployment_id);
 if(!a43ColibriDeployment(dep,qa4Inspector.data?.spec||{}))return;
 const url='/v1/local-ai/deployments/'+encodeURIComponent(targetID),ws=encodeURIComponent(onepaneWorkspace);
 const [tier,swap]=await Promise.allSettled([
  apiRequest(url+'/colibri-tier?workspace_id='+ws),
  apiRequest(url+'/colibri-swap?workspace_id='+ws)
 ]);
 a43ColibriSpecCache.set(targetID,{
  tier:tier.status==="fulfilled"?tier.value:null,
  tierError:tier.status==="rejected"?String(tier.reason?.message||tier.reason):"",
  swap:swap.status==="fulfilled"?swap.value:null,
  swapError:swap.status==="rejected"?String(swap.reason?.message||swap.reason):""
 });
 if(qa4Inspector.kind==="model"&&String(qa4Inspector.id)===targetID&&typeof renderInspector==="function"){
  renderInspector();
 }
};
document.addEventListener("click",async ev=>{
 const control=ev.target.closest("[data-a43-tier],[data-a43-plan],[data-a43-swap]");
 if(!control)return;
 const id=control.dataset.a43Tier||control.dataset.a43Plan||control.dataset.a43Swap;
 const dep=qa4Inspector.kind==="model"&&String(qa4Inspector.id)===String(id)?qa4Inspector.data:null;
 if(!dep)return;
 if(control.dataset.a43Tier){await a42OpenColibriTier(dep);return}
 if(control.dataset.a43Swap){await a42SwapColibri(dep,control);return}
 if(control.dataset.a43Plan){
  control.disabled=true;
  const old=control.textContent;control.textContent="Reading plan…";
  try{
   const plan=await apiRequest("/v1/local-ai/deployments/"+encodeURIComponent(id)+"/colibri-plan?workspace_id="+encodeURIComponent(onepaneWorkspace));
   openModal("Colibri read-only placement plan",
    '<div class="widget-body"><p class="page-subtitle">Colibri planner estimate only. Model is not launched and no VRAM/RAM usage is being measured.</p><pre class="spec-sheet-json">'+escapeHtml(JSON.stringify(plan,null,2))+'</pre></div>');
  }catch(ex){notice("Placement plan unavailable: "+ex.message,"bad")}
  finally{control.disabled=false;control.textContent=old}
 }
});
