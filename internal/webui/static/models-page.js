/* === Alpha 3.2 Models feature layer === */
// Managed components
async function a31ComponentAction(id,action,statusSelector){
  const status=statusSelector?$(statusSelector):null;if(status)status.textContent=`${titleCase(action)} queued…`;
  try{
    const job=await apiRequest(`/v1/local-ai/components/${encodeURIComponent(id)}/${encodeURIComponent(action)}`,{method:"POST",body:"{}"});
    for(let i=0;i<180;i++){
      const j=await apiRequest(`/v1/local-ai/component-jobs/${encodeURIComponent(job.id)}`);
      if(status)status.textContent=`${titleCase(j.stage||j.status||action)}…`;
      if(["succeeded","failed","interrupted"].includes(j.status)){
        if(j.status!=="succeeded")throw new Error(j.failure_reason||`${id} ${action} failed`);
        notice(`${titleCase(id)} ${action==="remove"?"uninstall":action} complete.`);
        if(a31RouteIs("models"))renderModels();
        return
      }
      await new Promise(r=>setTimeout(r,600))
    }
    throw new Error("component job timed out")
  }catch(ex){
    if(status)status.innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`;
    notice(ex.message,"bad")
  }
}
function a31ComponentName(id){return id==="colibri"?"Colibri":id==="omniroute"?"OmniRoute":id==="llamacpp"?"llama.cpp":titleCase(id)}
function a31ConfirmAction(title,message,confirmLabel,onConfirm){
  openModal(title,`<div class="widget-body confirm-stack"><p>${escapeHtml(message)}</p><div class="toolbar confirm-actions"><button class="btn" id="a31ConfirmCancel">Cancel</button><button class="btn danger" id="a31ConfirmAccept">${escapeHtml(confirmLabel)}</button></div></div>`);
  $("#a31ConfirmCancel").onclick=closeModal;
  $("#a31ConfirmAccept").onclick=async()=>{const b=$("#a31ConfirmAccept");b.disabled=true;try{await onConfirm();closeModal()}catch(ex){b.disabled=false;notice(ex.message,"bad")}}
}
function a31ComponentButtons(id,c){
  const st=String(c?.state||"not_installed"),installed=!!c?.installed;
  if(!installed)return `<button class="btn primary" data-a31-component="${id}:install">Install</button>`;
  if(id==="llamacpp")return `<button class="btn" data-a31-component="${id}:update">Update</button><button class="btn danger" data-a31-component="${id}:remove">Uninstall</button><button class="btn" data-a31-component-settings="${id}">Settings</button>`;
  const running=st==="running";
  return `<button class="btn" data-a31-component="${id}:${running?'disable':'enable'}">${running?'Stop':'Start'}</button><button class="btn" data-a31-component="${id}:update">Update</button><button class="btn danger" data-a31-component="${id}:remove">Uninstall</button><button class="btn" data-a31-component-settings="${id}">Settings</button>`
}
async function a31OpenComponentSettings(id,c){
  const installed=c?.installed_version||"Not installed",available=c?.available_version||"Unknown",state=titleCase(String(c?.state||"not installed").replaceAll("_"," "));
  let extra="";
  if(id==="llamacpp"){
    try{
      const rows=a31Array(await apiRequest("/v1/local-ai/llama-runtimes"));
      extra=`<div class="runtime-backend-list">${rows.map(x=>`<div class="runtime-backend-row"><div><strong>${escapeHtml(String(x.backend||"").toUpperCase())}</strong><div class="list-meta">${escapeHtml(x.reason||"Optional backend")}${x.driver_version?` · NVIDIA driver ${escapeHtml(x.driver_version)}`:""}</div></div><span class="pill ${x.installed?'good':''}">${x.installed?'Installed':x.recommended?'Recommended':'Optional'}</span></div>`).join("")}</div>`
    }catch(ex){extra=`<div class="page-subtitle">Detect hardware to calculate the recommended llama.cpp backend stack.</div>`}
  }
  openModal(`${a31ComponentName(id)} settings`,`<div class="widget-body managed-runtime-settings"><dl class="definition-grid"><dt>Installed version</dt><dd>${escapeHtml(installed)}</dd><dt>Available version</dt><dd>${escapeHtml(available)}</dd><dt>State</dt><dd>${escapeHtml(state)}</dd><dt>Runtime scope</dt><dd>This OnePane node</dd></dl>${extra}${c?.last_error?`<div class="error">${escapeHtml(c.last_error)}</div>`:""}<div class="toolbar">${c?.installed?`<button class="btn" data-a31-settings-repair="${id}">Repair runtime</button>`:""}${id==="colibri"&&c?.installed?'<button class="btn" id="a31SettingsRegisterColibri">Register model folder</button>':""}${id==="omniroute"?'<button class="btn" id="a31SettingsOmniCredentials">Manage credentials</button>':""}</div></div>`);
  $("[data-a31-settings-repair]")?.addEventListener("click",()=>{closeModal();a31ComponentAction(id,"repair",`#a31-${id}-status`)});
  $("#a31SettingsRegisterColibri")?.addEventListener("click",()=>{closeModal();qa5RegisterColibri()});
  $("#a31SettingsOmniCredentials")?.addEventListener("click",()=>{closeModal();a31CloudConsumerFilter="omniroute";a31SetModelView("cloud")});
}
function a31BindComponentButtons(root=document,components={}){
  $$("[data-a31-component]",root).forEach(b=>b.onclick=()=>{const [id,action]=b.dataset.a31Component.split(":");if(action==="remove"){a31ConfirmAction(`Uninstall ${a31ComponentName(id)}?`,`Remove ${a31ComponentName(id)} from this OnePane node? Existing data/configuration is preserved where supported.`,"Uninstall",()=>a31ComponentAction(id,action,`#a31-${id}-status`));return}a31ComponentAction(id,action,`#a31-${id}-status`)});
  $$("[data-a31-component-settings]",root).forEach(b=>b.onclick=()=>a31OpenComponentSettings(b.dataset.a31ComponentSettings,components[b.dataset.a31ComponentSettings]||{}))
}

// Models
function a31ModelTabs(){return `<div class="models-section-tabs"><button class="subtab ${a31ModelView==='local'?'active':''}" data-a31-model-tab="local">Local</button><button class="subtab ${a31ModelView==='cloud'?'active':''}" data-a31-model-tab="cloud">Cloud</button><button class="subtab ${a31ModelView==='routing'?'active':''}" data-a31-model-tab="routing">Model Routing</button><button class="subtab ${a31ModelView==='discover'?'active':''}" data-a31-model-tab="discover">Discover</button></div>`}
function a31PlacementLabel(d){const p=d?.placement||{},m=String(p.mode||"").toLowerCase();if(m==="cpu_only")return "CPU";if(m==="cpu_offload")return "HYBRID";if(a31Array(p.devices).some(x=>x.kind==="accelerator"))return "GPU";return "AUTO"}
function a31ComponentState(c){const s=String(c?.state||"not_installed");if(s==="installed_disabled")return "Installed · Stopped";if(s==="not_installed")return "Not installed";if(s==="failed")return "Failed";return titleCase(s.replaceAll("_"," "))}
function a31RuntimeErrorSummary(error){
 const raw=String(error||"").trim();if(!raw)return "";
 const reason=/unknown option ['"‘]?(--[\w-]+)/i.exec(raw);
 const short=reason?`Unsupported runtime option: ${reason[1]}. The managed launcher needs an update.`:raw.split(/\r?\n/)[0].slice(0,240);
 return `<span class="error">${escapeHtml(short)}</span>${raw.length>short.length?` <details class="runtime-error-details"><summary>Technical details</summary><pre>${escapeHtml(raw)}</pre></details>`:""}`;
}
function a31RuntimeCard(id,title,c,description){
  return `<section class="panel-card managed-runtime-card"><div class="card-header models-card-header"><div><div class="card-title">${escapeHtml(title)}</div><div class="list-meta">${escapeHtml(description)}</div></div><span class="pill ${["running","installed_disabled"].includes(String(c?.state))?'good':''}">${escapeHtml(a31ComponentState(c))}</span></div><div class="widget-body"><div class="runtime-version">${c?.installed?`v${escapeHtml(c.installed_version||"installed")}`:`Available ${escapeHtml(c?.available_version||"")}`}</div><div class="toolbar runtime-actions">${a31ComponentButtons(id,c)}</div><div id="a31-${id}-status" class="page-subtitle">${a31RuntimeErrorSummary(c?.last_error||"")}</div></div></section>`
}
async function a31DetectHardware(){
  localProfileQA=await apiRequest("/v1/local-ai/detect",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});
  return localProfileQA
}
async function a31RecommendedModels(limit=6){
  if(!localProfileQA)return [];
  const rows=await apiRequest("/v1/local-ai/recommendations",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,profile_id:localProfileQA.id,use_case:"general",context_tokens:8192,limit,storage_headroom_pct:20,prefer_gpu:true})});
  return a31Array(rows)
}
async function a31OpenCompute(dep){
  if(!localProfileQA){try{await a31DetectHardware()}catch(ex){notice(ex.message,"bad");return}}
  const gpus=a31Array(localProfileQA?.gpus);
  let current={preference:"auto",placement_mode:"auto",required_device_ids:[]};try{current=await apiRequest(`/v1/local-ai/deployments/${encodeURIComponent(dep.deployment_id)}/compute-policy?workspace_id=${encodeURIComponent(onepaneWorkspace)}`)}catch{}
  openModal("Compute placement",`<form id="a31ComputeForm" class="qa-form"><p class="page-subtitle">Change placement without redownloading the model. A resident runtime is stopped and restarted automatically.</p><label>Compute placement<select name="preference"><option value="auto">Auto</option><option value="prefer_gpu">Prefer GPU</option><option value="prefer_cpu">Prefer CPU</option><option value="require_gpu">Require GPU</option><option value="require_cpu">Require CPU</option><option value="hybrid">Hybrid / CPU + GPU</option></select></label><label>GPU device<select name="device"><option value="">Automatic</option>${gpus.map((g,i)=>`<option value="${escapeHtml(g.device_id||`gpu-${g.device_index??i}`)}">${escapeHtml(g.name||`GPU ${i}`)} · ${bytesQA(g.vram_bytes||0)}</option>`).join("")}</select></label><label>Advanced placement<select name="mode"><option value="auto">Automatic</option><option value="single_device">Single device</option><option value="layer_sharded">Layer split</option><option value="row_sharded">Row split</option><option value="tensor_sharded">Tensor split</option><option value="cpu_offload">CPU offload</option><option value="cpu_only">CPU only</option></select></label><button class="btn primary">Apply placement</button><div class="page-subtitle" id="a31ComputeStatus"></div></form>`);
  const form=$("#a31ComputeForm");form.elements.preference.value=current.preference||"auto";form.elements.mode.value=current.placement_mode||"auto";if(a31Array(current.required_device_ids).length)form.elements.device.value=current.required_device_ids[0];

  form.onsubmit=async e=>{e.preventDefault();const pref=form.elements.preference.value,device=form.elements.device.value,required=pref==="require_gpu"&&device?[device]:[],preferred=pref==="prefer_gpu"&&device?[device]:[];$("#a31ComputeStatus").textContent="Applying…";try{await apiRequest(`/v1/local-ai/deployments/${encodeURIComponent(dep.deployment_id)}/compute-policy`,{method:"PATCH",body:JSON.stringify({workspace_id:onepaneWorkspace,preference:pref,placement_mode:form.elements.mode.value,preferred_device_ids:preferred,required_device_ids:required})});notice("Compute placement updated.");closeModal();renderModels()}catch(ex){$("#a31ComputeStatus").innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`}}
}
async function a31InstallModel(model){
  // Reopen an active durable job instead of creating a second runtime install.
  const active=(await a31LoadActiveInstallJobs().catch(()=>[])).find(j=>String(j.status||"")!=="interrupted"&&String(j.model_ref||"").toLowerCase()===String(model?.model_ref||"").toLowerCase());
  if(active){a31OpenInstallProgress(active.id);return}
  if(!model?.installable){notice(model?.install_reason||"This model is advisory only; no verified artifact is available.","bad");return}
  if(!localProfileQA){try{await a31DetectHardware()}catch(ex){notice(ex.message,"bad");return}}
  const qs=a31Array(model.installable_quantizations),defaultQ=qs[0]||a31Array(model.quantizations)[0]||"";
  let peers=[];try{const inventory=await apiRequest("/v1/nodes");peers=a31Array(inventory?.nodes||inventory).filter(n=>!n.local&&["paired","trusted"].includes(String(n.trust_state||"").toLowerCase()))}catch{ /* Local installation still works if node inventory is unavailable. */ }
  openModal("Download & Install",`<form id="a31InstallModelForm" class="qa-form"><strong>${escapeHtml(model.display_name||model.model_ref)}</strong><p class="page-subtitle">OnePane will resolve and verify the managed llama.cpp runtime and model artifact automatically. Partial trusted downloads are resumable.</p><label>Install on<select name="target"><option value="local">This node</option>${peers.length?'<option value="remote">Another managed node</option>':""}</select></label><label id="a31TargetNodeRow" hidden>Managed node<select name="node">${peers.map(n=>`<option value="${escapeHtml(n.id||n.node_id)}">${escapeHtml(n.name||n.id||n.node_id)}</option>`).join("")}</select></label><label>Quantization<select name="quantization">${qs.map(q=>`<option>${escapeHtml(q)}</option>`).join("")}</select></label><label>Compute<select name="compute"><option value="auto">Auto</option><option value="prefer_gpu">Prefer GPU</option><option value="prefer_cpu">Prefer CPU</option><option value="require_gpu">Require GPU</option><option value="require_cpu">Require CPU</option><option value="hybrid">Hybrid / CPU + GPU</option></select></label><label class="install-override-option"><input type="checkbox" name="resource_override" value="1"> Proceed despite estimated RAM/VRAM limits (experimental; not a trust override)</label><div class="list-meta" id="a31InstallNotice">OnePane recommends a placement but honours the exact quantization you choose. The RAM/VRAM override is only needed if the placement estimate fails; verified artifacts and physical storage are never overridden.</div><button class="btn primary" id="a31InstallSubmit">Download & Install</button><div class="install-progress" id="a31InstallProgress"></div></form>`);
  const form=$("#a31InstallModelForm");form.elements.quantization.value=defaultQ;
  form.elements.target.onchange=()=>{
    const remote=form.elements.target.value==="remote";
    const targetRow=$("#a31TargetNodeRow");if(targetRow)targetRow.hidden=!remote;
    const note=$("#a31InstallNotice");
    if(note)note.textContent=remote?"Remote installs use the managed node's verified automatic planning. Compute and experimental resource override apply to this node only.":"Placement is calculated for the exact selected quantization; the override bypasses estimated RAM/VRAM limits, never artifact verification or disk limits.";
    form.elements.compute.disabled=remote;
    form.elements.resource_override.disabled=remote;
  };
  form.elements.target.onchange();
  form.onsubmit=async e=>{
    e.preventDefault();const box=$("#a31InstallProgress"),submit=$("#a31InstallSubmit");submit.disabled=true;
    const active=(await a31LoadActiveInstallJobs().catch(()=>[])).find(j=>String(j.status||"")!=="interrupted"&&String(j.model_ref||"").toLowerCase()===String(model.model_ref||"").toLowerCase()&&String(j.quantization||"").toLowerCase()===String(form.elements.quantization.value||"").toLowerCase());
    if(active){a31OpenInstallProgress(active.id);return}

    if(form.elements.target.value==="remote"){
      const nodeID=String(form.elements.node?.value||"");
      if(!nodeID){submit.disabled=false;box.innerHTML='<div class="error">Select a trusted node.</div>';return}
      box.textContent="Validating target node and trusted artifact…";
      try{
        const remote=await apiRequest(`/v1/nodes/${encodeURIComponent(nodeID)}/models/install`,{method:"POST",body:JSON.stringify({model_ref:model.model_ref,quantization:form.elements.quantization.value,use_case:"general",context_tokens:8192,role_name:"local-managed",prefer_gpu:true})});
        a38RemoteJobs.push({node_id:nodeID,job_id:remote.id,model_ref:model.model_ref,status:remote.status||"queued"});a38SaveRemoteJobs();closeModal();notice("Remote model install queued on "+nodeID);a38OpenAllInstallProgress();return
      }catch(ex){submit.disabled=false;box.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
    }
    box.innerHTML='<div class="install-step active">Resolving trusted catalogue…</div>';
    try{
      const job=await apiRequest("/v1/local-ai/install-jobs",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,profile_id:localProfileQA.id,role_name:"local-managed",use_case:"general",context_tokens:8192,model_ref:model.model_ref,quantization:form.elements.quantization.value,compute_preference:form.elements.compute.value,allow_resource_override:form.elements.resource_override.checked})});
      for(let i=0;i<2400;i++){
        const j=await apiRequest(`/v1/local-ai/install-jobs/${encodeURIComponent(job.id)}`),status=String(j.status||"queued"),pct=Math.max(0,Math.min(100,Math.round(Number(j.progress_pct||0)))),done=Number(j.bytes_downloaded||0),total=Number(j.bytes_total||0);
        box.innerHTML=`<div class="install-step active">${escapeHtml(j.current_artifact||titleCase(status.replaceAll("_"," ")))}</div><div class="install-progress-track"><span style="width:${pct}%"></span></div><div class="install-progress-meta"><span>${pct}%</span><span>${total>0?`${bytesQA(done)} / ${bytesQA(total)}`:""}</span></div>`;
        if(status==="ready"){notice("Model installed, started and qualified.");closeModal();renderModels();return}
        if(status==="failed"||status==="cancelled")throw new Error(j.failure_reason||`Install ${status}`);
        if(status==="interrupted"){box.insertAdjacentHTML("beforeend",`<div class="error">${escapeHtml(j.failure_reason||"Download interrupted.")}</div><div class="page-subtitle">Verified partial data was preserved. Run Install again to resume the download.</div>`);submit.disabled=false;return}
        await new Promise(r=>setTimeout(r,800))
      }
      throw new Error("Install job did not reach a terminal state.")
    }catch(ex){submit.disabled=false;box.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}
  }
}
function a31HardwareMarkup(){
  if(!localProfileQA)return '<div class="empty-state compact">Hardware has not been detected for this node yet.</div>';
  const gpus=a31Array(localProfileQA.gpus);
  return `<div class="hardware-summary"><strong>${escapeHtml(localProfileQA.cpu?.name||"CPU")}</strong><div>${bytesQA(localProfileQA.memory?.total_bytes||0)} RAM${localProfileQA.storage?.available_bytes?` · ${bytesQA(localProfileQA.storage.available_bytes)} free`:""}</div>${gpus.length?gpus.map((g,i)=>`<div>${escapeHtml(g.name||`GPU ${i}`)} · ${bytesQA(g.vram_bytes||0)} VRAM</div>`).join(""):"<div>No GPU detected</div>"}</div>`
}
function a31InstalledModelsMarkup(deployments){
  return deployments.length?deployments.map(d=>`<article class="model-tile"><div><strong>${escapeHtml(d.display_name||d.model_ref)}</strong><div class="list-meta">${escapeHtml(d.quantization||"")} · ${escapeHtml(d.runtime_name||d.runtime_backend||"managed")} · ${escapeHtml(d.status||"unknown")}</div><div class="list-meta"><span class="pill">${escapeHtml(String(d.compute_mode||"auto").toUpperCase())}</span> · Agent Check: <strong>${escapeHtml(String(d.agent_check_status||"not_run").replaceAll("_"," "))}</strong> · Admission: ${escapeHtml(d.admission_status||"pending")}</div></div><div class="toolbar">${String(d.runtime_name||"").toLowerCase()==="colibri"?`<button class="btn" data-a42-colibri-tier="${escapeHtml(d.deployment_id)}">Tiering</button><button class="btn" data-a42-colibri-swap="${escapeHtml(d.deployment_id)}">Hot swap</button>`:`<button class="btn" data-a31-compute="${escapeHtml(d.deployment_id)}">Compute</button>`}<button class="btn" data-a31-model-spec="${escapeHtml(d.deployment_id)}">Spec sheet</button><button class="btn primary" data-a31-agent-check="${escapeHtml(d.deployment_id)}">Agent Check</button></div></article>`).join(""):'<div class="empty-state compact">No managed local models yet.</div>'
}
function a31LlamaRuntimeCard(c,rows){
  const recommended=a31Array(rows).filter(x=>x.recommended),installed=a31Array(rows).filter(x=>x.installed);
  const summary=recommended.length?recommended.map(x=>String(x.backend||"").toUpperCase()).join(" + "):(installed.length?"Detect hardware for recommendations":"Detect hardware");
  return `<section class="panel-card managed-runtime-card"><div class="card-header models-card-header"><div><div class="card-title">llama.cpp</div><div class="list-meta">Managed inference runtime backends selected from detected hardware.</div></div><span class="pill ${installed.length?'good':''}">${installed.length?`${installed.length} backend${installed.length===1?"":"s"} installed`:"Not installed"}</span></div><div class="widget-body"><div class="runtime-version">Recommended: ${escapeHtml(summary)}</div><div class="runtime-backend-pills">${a31Array(rows).map(x=>`<span class="pill ${x.installed?'good':''}">${escapeHtml(String(x.backend||"").toUpperCase())} · ${x.installed?'installed':x.recommended?'recommended':'optional'}</span>`).join("")}</div><div class="toolbar runtime-actions">${a31ComponentButtons("llamacpp",c)}</div><div id="a31-llamacpp-status" class="page-subtitle">${escapeHtml(c?.last_error||"")}</div></div></section>`
}
async function a31RenderLocalModels(){
  const root=$("#a31ModelsRoot");let deployments=[],components={},recommendations=[],catalog=[],llamaRows=[];
  try{
    [deployments,components,catalog]=await Promise.all([qa5LoadManagedDeployments(),qa5ModelComponents().catch(()=>({})),apiRequest("/v1/local-ai/catalog").catch(()=>[])]);
    llamaRows=await apiRequest("/v1/local-ai/llama-runtimes").catch(()=>[])
  }catch(ex){root.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
  const installByRef=new Map(a31Array(catalog).map(m=>[String(m.model_ref||"").toLowerCase(),m]));

  const installedRefs=new Set(deployments.map(d=>String(d.model_ref||"").toLowerCase())),activeRefs=new Set(a31ActiveInstallJobs.map(j=>String(j.model_ref||"").toLowerCase()));
  const colibri=components.colibri||{},llama=components.llamacpp||{};
  root.innerHTML=`<div class="models-single-column"><div class="models-runtime-grid local-runtime-grid">${a31RuntimeCard("colibri","Colibri",colibri,"Managed large-model runtime.")}${a31LlamaRuntimeCard(llama,llamaRows)}<section class="panel-card hardware-card"><div class="card-header models-card-header"><div><div class="card-title">Hardware</div><div class="list-meta">Detected compute resources used for recommendations and runtime selection.</div></div><button class="btn primary a31-detect-large models-header-action" id="detectLocal">${localProfileQA?"Refresh":"⚙ Detect Hardware"}</button></div><div class="widget-body" id="localResult">${a31HardwareMarkup()}</div></section></div><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Installed Models</div><div class="list-meta">Registered local deployments reconciled against the managed model cache.</div></div><button class="btn models-header-action" id="a31RescanInstalledModels">Rescan models</button></div><div class="model-tile-scroll">${a31InstalledModelsMarkup(deployments)}</div></section></div>`;
  $("#detectLocal").onclick=async()=>{const b=$("#localResult");b.textContent="Detecting…";try{await a31DetectHardware();renderModels()}catch(ex){b.innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`}};

  $("#a31RescanInstalledModels").onclick=async()=>{const b=$("#a31RescanInstalledModels");b.disabled=true;b.textContent="Scanning…";try{const r=await apiRequest("/v1/local-ai/deployments/reconcile",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});notice(`Model rescan complete · ${r.kept||0} kept · ${r.removed_stale||0} stale removed · ${r.removed_duplicates||0} duplicate${Number(r.removed_duplicates||0)===1?"":"s"} removed.`);await renderModels()}catch(ex){b.disabled=false;b.textContent="Rescan models";notice(ex.message,"bad")}};

  a31BindComponentButtons(root,components);
  $$("[data-a31-compute]").forEach(b=>b.onclick=()=>a31OpenCompute(deployments.find(d=>d.deployment_id===b.dataset.a31Compute)));
  $$("[data-a42-colibri-tier]").forEach(b=>b.onclick=()=>a42OpenColibriTier(deployments.find(d=>d.deployment_id===b.dataset.a42ColibriTier)));
  $$("[data-a42-colibri-swap]").forEach(b=>b.onclick=()=>a42SwapColibri(deployments.find(d=>d.deployment_id===b.dataset.a42ColibriSwap),b));
  $$("[data-a31-model-spec]").forEach(b=>b.onclick=()=>qa5InspectModel(deployments.find(d=>d.deployment_id===b.dataset.a31ModelSpec)));
  $$("[data-a31-agent-check]").forEach(b=>b.onclick=()=>qa5AgentCheck(deployments.find(d=>d.deployment_id===b.dataset.a31AgentCheck)))
}
async function a31StartOAuth(preset){
  const callback=`${location.origin}/v1/provider-oauth/callback`;
  try{const out=await apiRequest(`/v1/provider-oauth/${encodeURIComponent(preset)}/start`,{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,redirect_uri:callback})});const win=window.open(out.authorization_url,"onepane-oauth","width=720,height=780");if(!win)throw new Error("Browser blocked the OAuth window.");const listener=e=>{if(e.origin!==location.origin||e.data?.type!=="onepane-oauth-complete")return;window.removeEventListener("message",listener);notice("OAuth account connected.");renderModels()};window.addEventListener("message",listener)}catch(ex){notice(ex.message,"bad")}
}
function a31OpenSecrets(scope="all"){sessionStorage.setItem("onepane:secrets-scope",scope);openRoute("secrets")}
async function a31EditOmniCredential(provider,label,has=false){
  openModal(`${has?"Update":"Add"} ${label} · OmniRoute`,`<form id="a31OmniCredentialForm" class="qa-form"><p class="page-subtitle">This credential is stored in Secrets and bound to the OmniRoute consumer scope. It remains distinct from a Direct OnePane credential for the same provider.</p><label>Credential type<select name="kind"><option value="api-key">API key</option><option value="access-token">Access token</option></select></label><label>Credential<input type="password" name="value" autocomplete="off" required placeholder="${has?"Paste replacement credential":"Paste credential"}"></label><button class="btn primary">${has?"Update":"Store"} credential</button></form>`);
  $("#a31OmniCredentialForm").onsubmit=async e=>{e.preventDefault();const fd=new FormData(e.currentTarget);try{await apiRequest("/v1/vault/provider-credentials",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,scope:"omniroute",upstream_provider:provider,kind:fd.get("kind"),value:fd.get("value"),display_label:`${label} · OmniRoute`})});closeModal();notice(`${label} OmniRoute credential stored.`);renderModels()}catch(ex){notice(ex.message,"bad")}}
}
function a31CloudModelsMarkup(){
  const rows=a31Array(qa4ProjectHub?.candidates).filter(x=>x.kind==="model_deployment"&&!x.local&&x.schedulable!==false);
  return rows.length?rows.map(x=>`<article class="model-tile"><div><strong>${escapeHtml(x.display_name||x.model_ref||x.id)}</strong><div class="list-meta">${escapeHtml(x.provider||"Cloud")} · ${escapeHtml(x.qualification||"available")}</div></div><span class="pill ${x.schedulable?'good':''}">${x.schedulable?'Routing eligible':'Unavailable'}</span></article>`).join(""):'<div class="empty-state compact">No cloud model deployments are currently registered. Connect a provider to populate routing candidates.</div>'
}
function a31ApplyCloudFilters(){
  const consumer=$("#a31CloudConsumer")?.value||a31CloudConsumerFilter||"all",auth=$("#a31CloudAuth")?.value||"all",status=$("#a31CloudStatus")?.value||"all";
  a31CloudConsumerFilter=consumer;
  $$("[data-cloud-provider-row]").forEach(row=>{
    const matchConsumer=consumer==="all"||row.dataset.consumer===consumer,matchAuth=auth==="all"||row.dataset.auth===auth,matchStatus=status==="all"||row.dataset.status===status;
    row.hidden=!(matchConsumer&&matchAuth&&matchStatus)
  })
}
async function a31RenderCloudModels(){
  const root=$("#a31ModelsRoot"),qs=encodeURIComponent(onepaneWorkspace);let presets=[],providers=[],components={},oauthCfg=[],oauthConn=[],vaultRows=[];
  try{
    await qa4LoadProjectHub(false).catch(()=>{});
    [presets,providers,components,oauthCfg,oauthConn,vaultRows]=await Promise.all([providerPresetsQA(),apiRequest(`/v1/providers?workspace_id=${qs}`),qa5ModelComponents().catch(()=>({})),apiRequest(`/v1/provider-oauth/configs?workspace_id=${qs}`).catch(()=>[]),apiRequest(`/v1/provider-oauth/connections?workspace_id=${qs}`).catch(()=>[]),loadVaultRecords().catch(()=>[])])
  }catch(ex){root.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
  const omni=components.omniroute||{},cloud=a31Array(presets).filter(p=>p.id!=="omniroute"),providerRows=a31Array(providers);
  liveOps.providers=providerRows;liveOps.reported.providers=true;syncLiveNotifications();
  const directCards=cloud.map(p=>{
    const connected=providerRows.find(x=>(String(x.provider).toLowerCase()===String(p.id).toLowerCase()||String(x.provider).toLowerCase()===String(p.credential_provider||"").toLowerCase())&&x.status!=="revoked"),oauth=String(p.auth_type||"").includes("oauth"),cfg=a31Array(oauthCfg).find(x=>x.preset_id===p.id&&x.enabled),oc=a31Array(oauthConn).find(x=>x.preset_id===p.id&&x.status==="connected"),auth=oauth?"oauth":"api",status=(connected||oc)?"connected":"setup";
    const oauthAction=cfg?`<button class="btn primary" data-a31-oauth-start="${escapeHtml(p.id)}">Connect OAuth</button>`:`<button class="btn" data-a31-oauth-setup="${escapeHtml(p.id)}">OAuth setup</button>`;
    return `<article class="provider-tile" data-cloud-provider-row data-consumer="direct" data-auth="${auth}" data-status="${status}"><div class="provider-tile-head"><strong>${escapeHtml(p.display_name||p.id)}</strong><span class="pill ${connected||oc?'good':''}">${oc?'OAuth connected':connected?'Connected':oauth?(cfg?'OAuth ready':'OAuth setup required'):'Needs setup'}</span></div><p>${escapeHtml(p.description||"")}</p><div class="provider-meta"><span>OnePane Direct</span><span>${oauth?'OAuth2 / PKCE':escapeHtml(p.auth_type||"API key")}</span><span>${escapeHtml(p.cost_hint||"")}</span></div><div class="toolbar">${oc?`<button class="btn danger" data-a31-oauth-revoke="${escapeHtml(oc.id)}">Revoke</button>`:connected?`<button class="btn danger" data-a31-provider-revoke="${escapeHtml(connected.id)}">Revoke</button>`:oauth?oauthAction:`<button class="btn primary" data-a31-provider-connect="${escapeHtml(p.id)}">Connect</button>`}</div></article>`
  }).join("");
  const omniCards=cloud.filter(p=>!String(p.auth_type||"").includes("oauth")&&String(p.auth_type||"")!=="none").map(p=>{const provider=String(p.credential_provider||p.id).toLowerCase(),stored=vaultRows.find(r=>secretMatches(r,{scope:"omniroute",provider}));return `<article class="provider-tile" data-cloud-provider-row data-consumer="omniroute" data-auth="api" data-status="${stored?'connected':'setup'}"><div class="provider-tile-head"><strong>${escapeHtml(p.display_name||p.id)}</strong><span class="pill ${stored?'good':''}">${stored?'Stored':'Needs setup'}</span></div><p>Credential used by OmniRoute. This can differ from the Direct OnePane credential.</p><div class="provider-meta"><span>OmniRoute</span><span>API key / token</span></div><div class="toolbar"><button class="btn ${stored?'':'primary'}" data-a31-omni-key-provider="${escapeHtml(provider)}" data-a31-omni-key-label="${escapeHtml(p.display_name||p.id)}" data-a31-omni-key-stored="${stored?'1':'0'}">${stored?'Update':'Add API key'}</button></div></article>`}).join("");
  const mode=localStorage.getItem("onepane:omniroute-mode")||"managed";
  root.innerHTML=`<div class="models-single-column"><div class="models-runtime-grid"><section class="panel-card omni-unified-card"><div class="card-header models-card-header"><div><div class="card-title">OmniRoute <span class="pill ${omni.state==="running"?"good":""}">${escapeHtml(a31ComponentState(omni))}</span></div><div class="list-meta">Managed provider/model routing gateway · ${escapeHtml(omni.installed?`v${omni.installed_version||"installed"}`:"Not installed")}</div></div><button class="btn models-header-action" id="a31OmniCredentials">Manage Credentials</button></div><div class="widget-body omni-runtime-overview"><div class="toolbar runtime-actions">${a31ComponentButtons("omniroute",omni)}</div><div id="a31-omniroute-status" class="page-subtitle">${a31RuntimeErrorSummary(omni?.last_error||"")}</div></div><div class="widget-body omni-provider-layout"><label>Gateway mode<select id="omniMode"><option value="managed">Managed local</option><option value="external">External gateway</option></select></label><label>Gateway URL<input id="omniUrl" value="${escapeHtml(mode==="managed"?'http://127.0.0.1:20128/v1':localStorage.getItem('onepane:omniroute-url')||'')}"></label><label>Gateway credential<select id="omniCredential"><option value="">No gateway credential</option></select></label><label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label><div class="toolbar"><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" disabled>Connect / Reconnect</button></div><div id="omniResult" class="page-subtitle">${mode==="managed"&&omni.state!=="running"?"Managed OmniRoute is installed but stopped. Start it before probing.":"Probe before connecting."}</div></div></section></div><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Providers & Credentials</div><div class="list-meta">OAuth and API credentials are shown even before configuration; filter by consumer without losing available providers.</div></div><button class="btn models-header-action" id="a31OpenSecrets">Open Secrets</button></div><div class="models-filter-row"><select id="a31CloudConsumer"><option value="all">All consumers</option><option value="direct">OnePane Direct</option><option value="omniroute">OmniRoute</option></select><select id="a31CloudAuth"><option value="all">All authentication</option><option value="oauth">OAuth</option><option value="api">API key / token</option></select><select id="a31CloudStatus"><option value="all">All statuses</option><option value="connected">Connected / stored</option><option value="setup">Needs setup</option></select></div><div class="provider-tile-grid cloud-provider-grid">${directCards}${omniCards}</div></section><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Cloud Models</div><div class="list-meta">Registered cloud deployments eligible for OnePane routing.</div></div></div><div class="model-tile-scroll">${a31CloudModelsMarkup()}</div></section></div>`;
  $("#a31CloudConsumer").value=a31CloudConsumerFilter;$("#omniMode").value=mode;
  const syncMode=()=>{const managed=$("#omniMode").value==="managed",url=$("#omniUrl");url.disabled=managed;if(managed)url.value="http://127.0.0.1:20128/v1";localStorage.setItem("onepane:omniroute-mode",managed?"managed":"external")};
  $("#omniMode").onchange=syncMode;syncMode();
  ["#a31CloudConsumer","#a31CloudAuth","#a31CloudStatus"].forEach(sel=>$(sel).onchange=a31ApplyCloudFilters);a31ApplyCloudFilters();
  $("#a31OpenSecrets").onclick=()=>a31OpenSecrets($("#a31CloudConsumer").value==="omniroute"?"omniroute":$("#a31CloudConsumer").value==="direct"?"provider":"all");
  $("#a31OmniCredentials").onclick=()=>{$("#a31CloudConsumer").value="omniroute";a31ApplyCloudFilters();$("#a31CloudConsumer").scrollIntoView({block:"center",behavior:"smooth"})};
  a31BindComponentButtons(root,components);populateOmniCredentials();
  $("#omniProbe").onclick=()=>{if($("#omniMode").value==="managed"&&omni.state!=="running"){const result=$("#omniResult");result.innerHTML='<span class="warn">Managed OmniRoute is stopped. Start it from the OmniRoute card, then probe again.</span>';return}omniQA(false)};
  $("#omniConnect").onclick=()=>omniQA(true);
  $$("[data-a31-provider-connect]").forEach(b=>b.onclick=()=>qa4ConnectCloudProvider(b.dataset.a31ProviderConnect));
  $$("[data-a31-provider-revoke]").forEach(b=>b.onclick=()=>qa4RevokeProvider(b.dataset.a31ProviderRevoke));
  $$("[data-a31-oauth-start]").forEach(b=>b.onclick=()=>a31StartOAuth(b.dataset.a31OauthStart));
  $$("[data-a31-oauth-setup]").forEach(b=>b.onclick=()=>openModal("OAuth setup",`<div class="widget-body"><strong>${escapeHtml(cloud.find(p=>String(p.id)===b.dataset.a31OauthSetup)?.display_name||"OAuth provider")}</strong><p>OnePane supports the OAuth connection lifecycle for this provider, but this installation does not yet have an enabled OAuth client configuration.</p><p class="page-subtitle">Configure an authorised OAuth client in Settings before starting the consent flow. OnePane will not fabricate provider credentials.</p><button class="btn primary" id="a31GoOAuthSettings">Open Settings</button></div>`));
  $$("[data-a31-oauth-setup]").forEach(b=>b.addEventListener("click",()=>setTimeout(()=>{$("#a31GoOAuthSettings")?.addEventListener("click",()=>{closeModal();openRoute("settings")})},0)));
  $$("[data-a31-oauth-revoke]").forEach(b=>b.onclick=async()=>{try{await apiRequest(`/v1/provider-oauth/connections/${encodeURIComponent(b.dataset.a31OauthRevoke)}/revoke`,{method:"POST",body:"{}"});renderModels()}catch(ex){notice(ex.message,"bad")}});
  $$("[data-a31-omni-key-provider]").forEach(b=>b.onclick=()=>a31EditOmniCredential(b.dataset.a31OmniKeyProvider,b.dataset.a31OmniKeyLabel,b.dataset.a31OmniKeyStored==="1"))
}
function a31RoutingProfileRows(project){return a31Array(qa4ProjectUI(project).model_routing_profiles)}
function a31ModelOptionLabel(value){return qa4ModelOptions().find(([v])=>v===value)?.[1]||value||"Automatic"}
async function a31EditRoutingProfile(index=-1){
  const project=a31CurrentProject();if(!project)return notice("Create or open a Project before saving routing profiles.","bad");
  const rows=a31RoutingProfileRows(project),existing=index>=0?rows[index]:null,options=qa4ModelOptions();
  const fallback=existing?.fallback_models||[];
  openModal(existing?"Edit routing profile":"New routing profile",`<form id="a31RoutingProfileForm" class="qa-form"><label>Name<input name="name" required value="${escapeHtml(existing?.name||"")}"></label><label>Primary model<select name="primary">${qa4OptionRows(options,existing?.primary_model||"auto")}</select></label><label>Fallback 1<select name="fallback1"><option value="">None</option>${qa4OptionRows(options.filter(([v])=>v!=="auto"),fallback[0]||"")}</select></label><label>Fallback 2<select name="fallback2"><option value="">None</option>${qa4OptionRows(options.filter(([v])=>v!=="auto"),fallback[1]||"")}</select></label><label>Fallback 3<select name="fallback3"><option value="">None</option>${qa4OptionRows(options.filter(([v])=>v!=="auto"),fallback[2]||"")}</select></label><p class="page-subtitle">Fallback order is preserved in the real task routing envelope. Research mode can still pin models and disable substitution.</p><button class="btn primary">Save profile</button></form>`);
  $("#a31RoutingProfileForm").onsubmit=async e=>{e.preventDefault();const fd=new FormData(e.currentTarget),next={id:existing?.id||`route-${Date.now().toString(36)}`,name:String(fd.get("name")||"").trim(),primary_model:String(fd.get("primary")||"auto"),fallback_models:[fd.get("fallback1"),fd.get("fallback2"),fd.get("fallback3")].map(String).filter(Boolean)};const updated=[...rows];if(index>=0)updated[index]=next;else updated.push(next);try{await qa4SaveProjectUI(project,{model_routing_profiles:updated});closeModal();renderModels()}catch(ex){notice(ex.message,"bad")}}
}
async function a31CreateWorkerRoutingProfile(){
  const project=a31CurrentProject();if(!project)return notice("Create or open a Project before saving routing profiles.","bad");
  const rows=a31RoutingProfileRows(project),existing=rows.findIndex(x=>String(x.name||"").toLowerCase()==="worker fallback chain");
  if(existing>=0){notice("Worker fallback profile already exists.");a31EditRoutingProfile(existing);return}
  const available=qa4ModelOptions().filter(([v])=>v!=="auto").slice(0,3).map(([v])=>v),next={id:`route-${Date.now().toString(36)}`,name:"Worker fallback chain",primary_model:"auto",fallback_models:available};
  try{await qa4SaveProjectUI(project,{model_routing_profiles:[...rows,next]});notice("Worker fallback profile created.");renderModels()}catch(ex){notice(ex.message,"bad")}
}
async function a31ApplyRoutingProfile(profile,role){
  const project=a31CurrentProject(),workspace=a31CurrentWorkspace();if(!project||!workspace)return notice("Open a Project Workspace before applying a routing profile.","bad");
  qa7NormalizeWorkspace(project,workspace);const target=workspace.orchestration[role]||qa7NormalizeRole({},role==="supervisor"?"agent.md":"onepane-default");
  target.model=profile.primary_model||"auto";target.fallback_models=a31Array(profile.fallback_models).filter(Boolean);target.fallback_model=target.fallback_models[0]||"auto";workspace.orchestration[role]=target;
  try{await qa4SaveProjectWorkspaces(project,qa4Workspaces(project));notice(`${profile.name} applied to ${workspace.name} · ${titleCase(role)}.`);renderModels()}catch(ex){notice(ex.message,"bad")}
}
async function a31RenderRoutingModels(){
  const root=$("#a31ModelsRoot");try{await qa4LoadProjectHub(false)}catch{}
  const project=a31CurrentProject(),workspace=a31CurrentWorkspace();
  if(!project){root.innerHTML='<div class="empty-state">Create or open a Project before configuring Model Routing profiles.</div>';return}
  const rows=a31RoutingProfileRows(project);
  root.innerHTML=`<div class="models-single-column"><section class="panel-card"><div class="card-header models-card-header models-routing-header"><div><div class="card-title">Model Routing Profiles</div><div class="list-meta">Reusable ordered model chains for Direct, Workers, Teams and Councils.</div></div><div class="toolbar models-routing-actions"><button class="btn" id="a31StarterRoute">Create worker fallback profile</button><button class="btn primary" id="a31NewRoute">New profile</button></div></div><div class="widget-body routing-profile-grid" id="a31RoutingProfiles">${rows.length?rows.map((p,i)=>`<article class="routing-profile-card"><div class="provider-tile-head"><strong>${escapeHtml(p.name||"Routing profile")}</strong><div class="toolbar"><button class="btn tiny" data-a31-route-edit="${i}">Edit</button><button class="btn tiny danger" data-a31-route-delete="${i}" aria-label="Delete routing profile">×</button></div></div><div class="routing-chain"><span>${escapeHtml(a31ModelOptionLabel(p.primary_model||"auto"))}</span>${a31Array(p.fallback_models).map(v=>`<span class="routing-arrow">→</span><span>${escapeHtml(a31ModelOptionLabel(v))}</span>`).join("")}</div><div class="routing-apply"><select data-a31-route-role="${i}"><option value="supervisor">Direct</option><option value="workers" selected>Workers</option><option value="team">Team</option><option value="council">Council</option></select><button class="btn primary" data-a31-route-apply="${i}" ${workspace?"":"disabled"}>Apply to ${escapeHtml(workspace?.name||"Workspace")}</button></div></article>`).join(""):'<div class="empty-state compact">No routing profiles yet. Create a reusable multi-fallback chain for worker agents or other roles.</div>'}</div></section><section class="panel-card"><div class="widget-body"><strong>Routing integrity</strong><p class="page-subtitle">Profiles write into Workspace routing policy and therefore affect actual task routing. Research mode remains authoritative when a Team/Council requires pinned models or disables substitution.</p></div></section></div>`;
  $("#a31NewRoute").onclick=()=>a31EditRoutingProfile(-1);$("#a31StarterRoute").onclick=a31CreateWorkerRoutingProfile;
  $$("[data-a31-route-edit]").forEach(b=>b.onclick=()=>a31EditRoutingProfile(Number(b.dataset.a31RouteEdit)));
  $$("[data-a31-route-delete]").forEach(b=>b.onclick=()=>{const i=Number(b.dataset.a31RouteDelete),profile=rows[i];a31ConfirmAction("Delete routing profile?",`Delete "${profile?.name||"profile"}"? Existing Workspaces keep the routing policy already applied from this profile.`,"Delete profile",async()=>{await qa4SaveProjectUI(project,{model_routing_profiles:rows.filter((_,n)=>n!==i)});renderModels()})});
  $$("[data-a31-route-apply]").forEach(b=>b.onclick=()=>{const i=Number(b.dataset.a31RouteApply),role=$(`[data-a31-route-role="${i}"]`)?.value||"workers";a31ApplyRoutingProfile(rows[i],role)})
}
function a31ExternalSourceLabel(source){return source==="huggingface"?"Hugging Face":source==="huggingbay"?"Hugging Bay":source==="llmfit"?"llmfit":"External"}
function a31OpenModelSource(url){try{const u=new URL(url);if(u.protocol!=="https:")throw new Error("Only HTTPS model sources can be opened.");window.open(u.toString(),"_blank","noopener,noreferrer")}catch(ex){notice(ex.message,"bad")}}
async function a31VerifyExternalModel(model){
  const source=model?._source||model?.source||"",id=model?.id||model?.model_ref||"";
  if(!source||!id)return notice("External model identity is incomplete.","bad");
  openModal("Verify external model",`<div class="widget-body"><strong>${escapeHtml(model.display_name||id)}</strong><p class="page-subtitle">OnePane is checking the source and resolving a digest-pinned local artifact. No browser-supplied digest is trusted.</p><div id="a31VerifyExternalStatus">Inspecting source…</div></div>`);
  const status=$("#a31VerifyExternalStatus");
  try{
    const inspected=await apiRequest(`/v1/local-ai/discovery/inspect?source=${encodeURIComponent(source)}&id=${encodeURIComponent(id)}`);
    const artifacts=a31Array(inspected.artifacts);
    if(!inspected.can_adopt||!artifacts.length){
      status.innerHTML=`<div class="warn">${escapeHtml(inspected.message||"No locally installable verified artifact was found.")}</div><div class="definition-grid"><dt>Source</dt><dd>${escapeHtml(a31ExternalSourceLabel(source))}</dd><dt>Trust</dt><dd>${escapeHtml(inspected.trust||"advisory")}</dd></div>`;
      return
    }
    status.innerHTML=`<form id="a31AdoptExternalForm" class="qa-form"><div class="good">${escapeHtml(inspected.message||"A digest-pinned GGUF artifact is available.")}</div><label>Verified artifact<select name="filename">${artifacts.map(a=>`<option value="${escapeHtml(a.filename)}">${escapeHtml(a.quantization||"GGUF")} · ${escapeHtml(a.filename)} · ${bytesQA(a.size_bytes||0)}</option>`).join("")}</select></label><div class="definition-grid"><dt>Trust</dt><dd>${escapeHtml(inspected.trust||"verified metadata")}</dd><dt>Verification</dt><dd>SHA-256 is re-resolved by OnePane and rechecked during download.</dd></div><button class="btn primary">Verify, adopt & install</button></form>`;
    $("#a31AdoptExternalForm").onsubmit=async e=>{
      e.preventDefault();const button=e.currentTarget.querySelector("button");button.disabled=true;button.textContent="Adopting…";
      try{
        const fd=new FormData(e.currentTarget);
        const adopted=await apiRequest("/v1/local-ai/discovery/adopt",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,source,model_id:id,filename:String(fd.get("filename")||"")})});
        const catalog=await apiRequest("/v1/local-ai/catalog"),spec=a31Array(catalog).find(x=>String(x.model_ref||"").toLowerCase()===String(adopted?.model?.model_ref||id).toLowerCase());
        closeModal();notice("External artifact verified and added to the local trusted catalogue.");
        if(spec?.installable)a31InstallModel(spec);else notice("Artifact was adopted but is not yet installable from the active catalogue.","bad")
      }catch(ex){button.disabled=false;button.textContent="Verify, adopt & install";notice(ex.message,"bad")}
    }
  }catch(ex){status.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`}
}

async function a31RenderDiscoverModels(){
  const root=$("#a31ModelsRoot");let catalog=[],recommendations=[],external=[],sourceErrors={},nextCursor="",loadingExternal=false,requestEpoch=0,autoPages=0,installedRefs=null;
  const deploymentRefs=rows=>{
    const list=Array.isArray(rows)?rows:rows?.deployments;
    return Array.isArray(list)?new Set(list.map(x=>String(x.model_ref||"").trim().toLowerCase()).filter(Boolean)):null
  };
  try{
    const [cat,deployments]=await Promise.all([apiRequest("/v1/local-ai/catalog"),apiRequest("/v1/local-ai/deployments?workspace_id="+encodeURIComponent(onepaneWorkspace)).catch(()=>null)]);
    catalog=cat;installedRefs=deploymentRefs(deployments);
    if(localProfileQA)recommendations=await a31RecommendedModels(50).catch(()=>[])
  }catch(ex){root.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;return}
  const fits=new Map(recommendations.map(x=>[String(x.model?.model_ref||"").toLowerCase(),{fit:x.fit_level,mode:x.run_mode}]));
  root.innerHTML=`<div class="models-single-column"><section class="panel-card"><div class="card-header models-card-header"><div><div class="card-title">Discover Models</div><div class="list-meta">OnePane Verified installs remain digest-pinned; external catalogues expand discovery without weakening the trust boundary.</div></div><button class="btn models-header-action" id="a31DiscoverDetect">${localProfileQA?"Refresh hardware fit":"Detect hardware"}</button></div><div class="models-discover-controls"><input id="a31DiscoverFilter" class="catalogue-filter" placeholder="Search model, capability, quantization…"><select id="a31DiscoverSource"><option value="all">All sources</option><option value="onepane">OnePane Verified</option><option value="huggingface">Hugging Face</option><option value="huggingbay">Hugging Bay</option><option value="llmfit">llmfit</option></select><select id="a31DiscoverAvailability"><option value="all">All models</option><option value="downloadable">Downloadable only</option><option value="verification">Verification required</option><option value="unavailable">Unavailable</option><option value="recommended">Recommended for this hardware</option></select><select id="a31DiscoverSort" aria-label="Sort models"><option value="popular">Most popular</option></select><button class="btn primary" id="a31DiscoverSearch">Search catalogues</button></div><div id="a31DiscoverSourceStatus" class="page-subtitle discover-source-status"></div><div id="a31DiscoverCatalog" class="model-tile-scroll"></div><div class="discover-pagination"><span id="a31DiscoverCount" class="page-subtitle"></span><button class="btn" id="a31DiscoverMore" hidden>Load more models</button></div></section></div>`;
  const isInstalled=m=>installedRefs?.has(String(m.model_ref||m.id||"").trim().toLowerCase())===true;
  const localRows=()=>a31Array(catalog).map(m=>{const f=fits.get(String(m.model_ref||"").toLowerCase());return {...m,_source:"onepane",_fit:f}});
  const externalRows=()=>external.map(x=>({...x,_source:x.source||"external",_fit:x.fit_level?{fit:x.fit_level,mode:x.run_mode}:null}));
  const availabilityChanged=()=>{if($("#a31DiscoverCatalog"))draw()};const draw=()=>{
    const q=($("#a31DiscoverFilter").value||"").toLowerCase(),source=$("#a31DiscoverSource").value,mode=$("#a31DiscoverAvailability").value;
    let rows=[...localRows(),...externalRows()];
    rows=rows.filter(m=>{
      const hit=!q||JSON.stringify(m).toLowerCase().includes(q),sourceHit=source==="all"||(source==="onepane"?m._source==="onepane":m._source===source),fit=!!m._fit;
      const isLocal=m._source==="onepane",availability=isInstalled(m)?"no":isLocal?(m.installable&&installedRefs?"yes":"no"):a40DownloadCache.get(m._source+":"+(m.id||m.model_ref||""))?.state;
      return hit&&sourceHit&&(mode==="all"||mode==="downloadable"&&availability==="yes"||mode==="verification"&&!isLocal&&(!availability||availability==="error")||mode==="unavailable"&&(availability==="no"||availability==="error")||mode==="recommended"&&fit)
    });
    $("#a31DiscoverCatalog").innerHTML=rows.length?rows.map((m,i)=>{
      if(m._source==="onepane"){
        const f=m._fit,installed=isInstalled(m),canInstall=!!m.installable&&installedRefs!==null&&!installed;
        const label=installed?"Installed":installedRefs===null?"Inventory unavailable":m.installable?"Ready to install":"Advisory only";
        return `<article class="model-tile"><div><div class="model-source-line"><span class="pill good">OnePane Verified</span><span class="pill ${installed||canInstall?'good':''}">${label}</span></div><strong>${escapeHtml(m.display_name||m.model_ref)}</strong><div class="list-meta">${escapeHtml(String(m.params_b||m.parameter_count||m.parameter_scale||"—"))} · ${formatContextQA(m.context_length||m.max_context_tokens||m.context_tokens)} context</div><div class="list-meta">${m.installable?`Digest-pinned · ${escapeHtml(a31Array(m.installable_quantizations).join(", "))}`:`Advisory · ${escapeHtml(m.install_reason||"No verified artifact")}`}${f?` · ${escapeHtml(f.fit||"fit")} · ${escapeHtml(f.mode||"")}`:""}</div></div><div class="toolbar">${f?`<span class="pill good">${escapeHtml(f.fit||"Recommended")}</span>`:""}<button class="btn ${canInstall?'primary':''}" data-a31-discover-install-ref="${escapeHtml(m.model_ref||"")}" ${canInstall?'':'disabled'}>${installed?'Installed':canInstall?'Install':installedRefs===null?'Unavailable':'Unavailable'}</button></div></article>`
      }
      const availability=m._source==="llmfit"?"Hardware advisory · artifact not verified":m._source==="huggingbay"?"Manifest verification required":"Artifact verification required",action=m._source==="llmfit"?"Find compatible GGUF":m._source==="huggingbay"?"Verify manifest":"Verify artifact";
      const src=a31ExternalSourceLabel(m._source),meta=[m.quantization,m.runtime,m.license,m.downloads?`${Number(m.downloads).toLocaleString()} downloads`:"",m.seeds?`${Number(m.seeds).toLocaleString()} seeds`:""].filter(Boolean).join(" · ");
      return `<article class="model-tile external-model-tile" data-a40-source="${escapeHtml(m._source)}" data-a40-id="${escapeHtml(m.id||m.model_ref||"")}"><div><div class="model-source-line"><span class="pill">${escapeHtml(src)}</span><span class="pill ${m.verified?'good':''}">${escapeHtml(m.trust||"advisory")}</span><span class="pill">${escapeHtml(availability)}</span><span class="pill a40-download-status" aria-live="polite">Checking download…</span></div><strong>${escapeHtml(m.display_name||m.id||"Model")}</strong><div class="list-meta">${escapeHtml(meta||m.category||"External catalogue entry")}</div><div class="list-meta">${m._fit?`${escapeHtml(m._fit.fit||"fit")} · ${escapeHtml(m._fit.mode||"")}`:`${escapeHtml(m.install_reason||"External source metadata")}`}</div></div><div class="toolbar">${m._fit?`<span class="pill good">${escapeHtml(m._fit.fit||"Recommended")}</span>`:""}${m.source_url?`<button class="btn" data-a31-source-url="${escapeHtml(m.source_url)}">View source</button>`:""}<button class="btn ${isInstalled(m)?'':'primary'}" data-a31-verify-source="${escapeHtml(m._source)}" data-a31-verify-id="${escapeHtml(m.id||m.model_ref||"")}" ${isInstalled(m)?'disabled':''}>${isInstalled(m)?'Installed':escapeHtml(action)}</button></div></article>`
    }).join(""):'<div class="empty-state compact">No models match these filters.</div>';
    const checked=external.filter(m=>a40DownloadCache.has((m.source||"external")+":"+(m.id||m.model_ref||""))).length;
    const confirmed=external.filter(m=>a40DownloadCache.get((m.source||"external")+":"+(m.id||m.model_ref||""))?.state==="yes").length;
    $("#a31DiscoverCount").textContent=`${rows.length} visible · ${confirmed} external downloadable confirmed · ${checked}/${external.length} checked`;
    if(["downloadable","verification","unavailable"].includes(mode))for(const m of external){const s=m.source||"external",id=m.id||m.model_ref||"";if(id)a40QueueAvailability(s,id,availabilityChanged)}
    if(mode==="downloadable"&&nextCursor&&checked===external.length&&rows.length<8&&!loadingExternal&&autoPages<5){autoPages++;Promise.resolve().then(()=>fetchExternal(true))}
    $$("[data-a31-discover-install-ref]:not(:disabled)").forEach(b=>b.onclick=async()=>{
      const model=catalog.find(x=>String(x.model_ref)===b.dataset.a31DiscoverInstallRef);
      if(!model)return;
      const current=await apiRequest("/v1/local-ai/deployments?workspace_id="+encodeURIComponent(onepaneWorkspace)).catch(()=>null);
      installedRefs=deploymentRefs(current);draw();
      if(installedRefs===null)return notice("Cannot verify existing installations; retry after refreshing the inventory.","bad");
      if(isInstalled(model))return notice("Model is already installed.");
      a31InstallModel(model)
    });
    $$("[data-a31-source-url]").forEach(b=>b.onclick=()=>a31OpenModelSource(b.dataset.a31SourceUrl));
    $$("[data-a31-verify-source]:not(:disabled)").forEach(b=>b.onclick=()=>{const m=external.find(x=>String(x.source||"")===b.dataset.a31VerifySource&&String(x.id||x.model_ref||"")===b.dataset.a31VerifyId);if(m&&!isInstalled(m))a31VerifyExternalModel({...m,_source:m.source})})
    if(typeof a40ScheduleAvailabilityChecks==="function")a40ScheduleAvailabilityChecks();
  };

  const fetchExternal=async(more=false)=>{
    if(loadingExternal)return;
    const source=$("#a31DiscoverSource").value,q=$("#a31DiscoverFilter").value.trim(),status=$("#a31DiscoverSourceStatus"),button=$("#a31DiscoverSearch"),moreButton=$("#a31DiscoverMore"),epoch=more?requestEpoch:++requestEpoch;
    if(source==="onepane"){external=[];nextCursor="";sourceErrors={};status.textContent="Showing OnePane's verified local catalogue.";moreButton.hidden=true;draw();return}
    if(more&&!nextCursor)return;
    loadingExternal=true;button.disabled=true;moreButton.disabled=true;
    status.textContent=more?"Loading next catalogue page…":"Querying model catalogues…";
    try{
      const cursor=more?`&cursor=${encodeURIComponent(nextCursor)}`:"",order=$("#a31DiscoverSort").value;
      const out=await apiRequest(`/v1/local-ai/discovery?source=${encodeURIComponent(source)}&q=${encodeURIComponent(q)}&sort=${encodeURIComponent(order)}&limit=40${cursor}`);
      if(epoch!==requestEpoch||!$("#a31DiscoverCatalog"))return;
      const incoming=a31Array(out?.models);
      const keys=new Set((more?external:[]).map(m=>`${m.source}:${m.id}`));
      external=more?[...external]:[];
      for(const m of incoming){const key=`${m.source}:${m.id}`;if(!keys.has(key)){keys.add(key);external.push(m)}}
      nextCursor=out?.has_more?String(out?.next_cursor||""):"";
      sourceErrors=out?.source_errors||{};
      const states=out?.sources||{},messages=Object.entries(states).map(([k,v])=>`${a31ExternalSourceLabel(k)}: ${v.count} returned (${v.message||v.status})`);
      const errors=Object.entries(sourceErrors),summary=messages.length?messages.join(" · "):`${incoming.length} entries returned`;
      status.innerHTML=errors.length?`<span class="warn">${escapeHtml(summary)} · ${escapeHtml(errors.map(([k,v])=>`${a31ExternalSourceLabel(k)}: ${v}`).join(" · "))}</span>`:escapeHtml(summary);
      moreButton.hidden=!nextCursor;draw();
    }catch(ex){if(!more)external=[];status.innerHTML=`<span class="error">${escapeHtml(ex.message)}</span>`;draw()}
    finally{loadingExternal=false;button.disabled=false;moreButton.disabled=false}
  };
  $("#a31DiscoverFilter").oninput=draw;
  $("#a31DiscoverAvailability").onchange=draw;
  const updateSort=()=>{
    const source=$("#a31DiscoverSource").value,sort=$("#a31DiscoverSort"),prior=sort.value;
    const choices=source==="huggingface"?[["popular","Most popular"],["trending","Trending this week"],["downloads","Most downloads"],["likes","Most liked"],["newest","Newest"],["updated","Recently updated"]]:
      source==="huggingbay"?[["popular","Most popular (seeds)"],["seeds","Most seeded"]]:
      source==="llmfit"?[["popular","Best hardware fit"],["fit","Best fit"],["speed","Fastest estimate"],["memory","Lowest memory"],["name","Name"]]:
      [["popular","Most popular per source"]];
    sort.innerHTML=choices.map(([id,title])=>`<option value="${id}">${title}</option>`).join("");
    if(choices.some(([id])=>id===prior))sort.value=prior;
  };
  $("#a31DiscoverSource").onchange=()=>{nextCursor="";autoPages=0;updateSort();fetchExternal(false)};
  $("#a31DiscoverSort").onchange=()=>{nextCursor="";fetchExternal(false)};
  $("#a31DiscoverSearch").onclick=()=>{nextCursor="";autoPages=0;fetchExternal(false)};
  $("#a31DiscoverMore").onclick=()=>fetchExternal(true);
  updateSort();draw();fetchExternal(false);
  $("#a31DiscoverDetect").onclick=async()=>{try{await a31DetectHardware();renderModels()}catch(ex){notice(ex.message,"bad")}}
}
renderModels=async function(){
  const descriptions={local:"Installed local models, managed runtimes and hardware-aware recommendations.",cloud:"OmniRoute, provider authentication and registered cloud models.",routing:"Reusable ordered model routes with multi-fallback policy.",discover:"Find and install models from the trusted catalogue."};
  const epoch=qa31ViewEpoch;$("#viewHost").innerHTML=`<section class="page models-page">${pageHeader("Models",descriptions[a31ModelView]||descriptions.local,'<button class="btn" id="a31ModelSettings">Settings</button>')}${a31ModelTabs()}<div id="a31ModelsRoot"><div class="widget-body">Loading…</div></div></section>`;
  $("#a31ModelSettings").onclick=()=>openRoute("settings");$$("[data-a31-model-tab]").forEach(b=>b.onclick=()=>a31SetModelView(b.dataset.a31ModelTab));
  if(a31ModelView==="local")await a31RenderLocalModels();else if(a31ModelView==="cloud")await a31RenderCloudModels();else if(a31ModelView==="routing")await a31RenderRoutingModels();else await a31RenderDiscoverModels();
  if(epoch!==qa31ViewEpoch||!a31RouteIs("models"))return;bindViewActions($("#viewHost"));renderNav()
};
