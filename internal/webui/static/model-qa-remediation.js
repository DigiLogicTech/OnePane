function a36SpecRow(label,value){
 const text=value===null||value===undefined||value===""?"Not yet available":String(value);
 return `<dt>${escapeHtml(label)}</dt><dd>${escapeHtml(text)}</dd>`
}
function a36SpecSection(title,rows){
 return `<section class="spec-readable-section"><h3>${escapeHtml(title)}</h3><dl class="definition-grid">${rows.join("")}</dl></section>`
}
function a36ObjectText(value){
 if(value===undefined||value===null||value==="")return "Not reported";
 if(Array.isArray(value))return value.length?value.join(", "):"None";
 if(typeof value==="object")return Object.entries(value).filter(([,v])=>v!==null&&v!==""&&v!==undefined).slice(0,15).map(([k,v])=>`${titleCase(k.replaceAll("_"," "))}: ${typeof v==="object"?JSON.stringify(v):v}`).join(" · ")||"Not reported";
 return String(value);
}
const a36InspectorOverviewBase=qa4InspectorOverview;
qa4InspectorOverview=function(){
 if(qa4Inspector.kind!=="model")return a36InspectorOverviewBase();
 const m=qa4Inspector.data||{},s=m.spec||{},model=s.model||{},placement=s.placement||{},qual=s.qualification||{},claims=s.catalog_claims||{},fit=s.llmfit||{},restrictions=s.restrictions||{};
 const context=v=>v?formatContextQA(v):"Not yet verified";
 const tps=s.measured_tps==null?"Not measured":`${Number(s.measured_tps).toFixed(1)} tokens/sec`;
 const ttft=s.measured_ttft_ms==null?"Not measured":`${Number(s.measured_ttft_ms).toFixed(0)} ms`;
 const body=s.error?`<div class="error">${escapeHtml(s.error)}</div>`:
 a36SpecSection("Identity",[
  a36SpecRow("Model",m.display_name||model.display_name||m.model_ref),
  a36SpecRow("Model reference",m.model_ref||model.model_ref),
  a36SpecRow("Quantization",s.quantization||m.quantization),
  a36SpecRow("Licence",model.license||claims.license||"Check source licence"),
  a36SpecRow("Deployment state",m.status||"Unknown")])+
 a36SpecSection("Runtime and placement",[
  a36SpecRow("Runtime",s.runtime_name||m.runtime_name),
  a36SpecRow("Backend",s.runtime_backend||m.runtime_backend||"Automatic"),
  a36SpecRow("Compute plan",a36ObjectText(placement)),
  a36SpecRow("Admission",s.admission_status||m.admission_status||"Pending")])+
 a36SpecSection("Context and measured performance",[
  a36SpecRow("Requested context",context(s.requested_context_tokens)),
  a36SpecRow("Verified context",context(s.verified_context_tokens)),
  a36SpecRow("Generated-token speed",tps),
  a36SpecRow("Time to first token",ttft)])+
 a36SpecSection("Qualification and restrictions",[
  a36SpecRow("Agent Check",a36ObjectText(qual)),
  a36SpecRow("Restrictions",a36ObjectText(restrictions)),
  a36SpecRow("llmfit guidance",a36ObjectText(fit))])+
 `<details class="spec-technical-details"><summary>Advanced details and raw JSON</summary><p class="page-subtitle">Technical provenance for debugging and model assessment.</p><pre class="spec-sheet-json">${escapeHtml(JSON.stringify(s,null,2))}</pre></details>`;
 return `<div class="model-spec-inspector readable-model-spec"><div class="spec-readable-head"><strong>${escapeHtml(m.display_name||model.display_name||m.model_ref||"Local model")}</strong><span class="pill">${escapeHtml(titleCase(String(s.admission_status||"pending")))}</span></div>${body}<button class="btn primary" id="qa5InspectorAgentCheck">Run Agent Check</button></div>`
};
async function a36LlamaBackendManager(){
 let rows=[];
 try{rows=a31Array(await apiRequest("/v1/local-ai/llama-runtimes"))}
 catch(ex){return notice(ex.message,"bad")}
 openModal("Manage llama.cpp backends",`<div class="widget-body qa-form"><p class="page-subtitle">CPU, CUDA and Vulkan are managed independently. Model weights are preserved. Stop and migrate dependent deployments before removing their backend.</p><div class="runtime-backend-list">${rows.map(x=>{const used=Number(x.active_instances||0),deps=Number(x.dependent_models||0),blocked=used>0||deps>0;return `<div class="runtime-backend-row"><div><strong>${escapeHtml(String(x.backend||"").toUpperCase())}</strong><div class="list-meta">${escapeHtml(x.reason||"Optional backend")}</div><div class="list-meta">${deps} dependent model(s) · ${used} active instance(s)</div></div><div class="toolbar"><span class="pill ${x.installed?"good":""}">${x.installed?"Installed":"Not installed"}</span>${x.installed?`<button class="btn danger" data-a36-remove-backend="${escapeHtml(x.backend)}" ${blocked?"disabled":""}>Uninstall</button>`:""}</div></div>`}).join("")}</div><div id="a36BackendStatus" class="page-subtitle"></div></div>`);
 $$("[data-a36-remove-backend]").forEach(button=>button.onclick=async()=>{
  const backend=button.dataset.a36RemoveBackend;
  button.disabled=true;$("#a36BackendStatus").textContent=`Removing ${backend} backend…`;
  try{
   await apiRequest(`/v1/local-ai/llama-runtimes/${encodeURIComponent(backend)}/remove`,{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});
   notice(`${backend.toUpperCase()} backend removed; weights preserved.`);await a36LlamaBackendManager();if(a31RouteIs("models"))renderModels()
  }catch(ex){button.disabled=false;$("#a36BackendStatus").innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`}
 })
}
const a36BindComponentsBase=a31BindComponentButtons;
a31BindComponentButtons=function(root=document,components={}){
 a36BindComponentsBase(root,components);
 $$('[data-a31-component="llamacpp:remove"]',root).forEach(b=>b.onclick=a36LlamaBackendManager)
};
function a36ManagedLLMFitCard(st){
 const installed=!!st.installed,running=!!st.running;
 const controls=!st.supported?'<span class="page-subtitle">Managed Windows x64 runtime only</span>':!installed?'<button class="btn primary" data-a36-llmfit="install">Install llmfit</button>':`<button class="btn" data-a36-llmfit="${running?"stop":"start"}">${running?"Stop":"Start"}</button><button class="btn danger" data-a36-llmfit="remove" ${running?"disabled":""}>Uninstall</button>`;
 return `<section class="panel-card managed-runtime-card"><div class="card-header models-card-header"><div><div class="card-title">llmfit</div><div class="list-meta">Hardware suitability advisor and model catalogue</div></div><span class="pill ${running?"good":""}">${running?"Running":installed?"Installed · Stopped":"Not installed"}</span></div><div class="widget-body"><div class="runtime-version">${installed?`v${escapeHtml(st.version)}`:"Managed llmfit available"}</div><div class="toolbar runtime-actions">${controls}</div><div class="page-subtitle" id="a36LLMFitStatus">${escapeHtml(st.message||"")}</div></div></section>`
}
async function a36BindLLMFitCard(){
 const grid=$(".local-runtime-grid");if(!grid)return;
 try{
  const st=await apiRequest(`/v1/local-ai/llmfit?workspace_id=${encodeURIComponent(onepaneWorkspace)}`);
  if(!grid.isConnected)return;
  $("#a36LLMFitCard")?.remove();
  const slot=document.createElement("div");slot.id="a36LLMFitCard";slot.innerHTML=a36ManagedLLMFitCard(st);grid.appendChild(slot);
  $$("[data-a36-llmfit]",slot).forEach(b=>b.onclick=async()=>{
   const action=b.dataset.a36Llmfit;b.disabled=true;$("#a36LLMFitStatus").textContent=`${titleCase(action)} in progress…`;
   try{
    await apiRequest(`/v1/local-ai/llmfit/${encodeURIComponent(action)}`,{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});
    notice(`llmfit ${action} complete.`);await a36BindLLMFitCard()
   }catch(ex){b.disabled=false;$("#a36LLMFitStatus").innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`}
  })
 }catch(ex){if(grid.isConnected){const slot=document.createElement("div");slot.className="panel-card";slot.innerHTML=`<div class="widget-body error">${escapeHtml("llmfit: "+ex.message)}</div>`;grid.appendChild(slot)}}
}
const a36RenderLocalBase=a31RenderLocalModels;
a31RenderLocalModels=async function(){await a36RenderLocalBase();await a36BindLLMFitCard()};
