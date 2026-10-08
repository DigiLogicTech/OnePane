// Explicit activation warms the selected Colibri model. Subsequent chat
// requests also activate it on demand via the shared supervisor.
// This does not change which model OnePane Chat routes to.
async function a42SwapColibri(deployment,button=null) {
  const id=String(deployment?.deployment_id||"");
  if(!id)return notice("Select a registered Colibri model.","bad");
  const old=button?.textContent;
  if(button){button.disabled=true;button.textContent="Switching…"}
  try {
    const data=await apiRequest(`/v1/local-ai/deployments/${encodeURIComponent(id)}/colibri-swap`,{
      method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})
    });
    notice(`Colibri model resident · ${String(deployment.display_name||deployment.model_ref||"model")}. Other idle Colibri models were released.`);
    if(button){button.textContent="Resident"}
    if(typeof renderModels==="function")await renderModels();
    return data;
  }catch(ex){
    notice(ex.message,"bad");
    if(button){button.disabled=false;button.textContent=old}
    return null;
  }
}

// Colibri model-specific hot/warm/cold memory controls.
// Registered only for Colibri deployments; llama.cpp Compute remains separate.
async function a42OpenColibriTier(deployment) {
  const id=String(deployment?.deployment_id||"");
  if(!id)return notice("Select a registered Colibri model first.","bad");
  const url=`/v1/local-ai/deployments/${encodeURIComponent(id)}`;
  const ws=encodeURIComponent(onepaneWorkspace);
  let state;
  try { state=await apiRequest(`${url}/colibri-tier?workspace_id=${ws}`) }
  catch(ex){return notice(ex.message,"bad")}
  const tier=state?.settings||{};
  openModal("Colibri · Tiered inference",`
    <form id="a42ColibriTierForm" class="qa-form">
      <p class="page-subtitle">Colibri manages hot VRAM, warm RAM and disk-backed experts. Settings apply to this model only. An active request will not be interrupted.</p>
      <label>Tier placement mode
        <select name="mode">
          <option value="automatic">Automatic — Colibri defaults</option>
          <option value="balanced">Balanced — adaptive expert cache</option>
          <option value="manual">Manual — override cache budgets</option>
        </select>
      </label>
      <label>GPU backend
        <select name="backend">
          <option value="auto">Auto — use installed backend defaults</option>
          <option value="cpu">CPU only</option>
          <option value="vulkan">Vulkan</option>
          <option value="cuda">CUDA</option>
        </select>
      </label>
      <div class="page-subtitle">Vulkan and CUDA require Colibri binaries and drivers with that backend. Selecting an unsupported backend fails visibly; there is no silent fallback.</div>
      <div id="a42ColibriManual">
        <label>RAM budget in GB (0 = auto)<input name="ram_gb" type="number" min="0" max="512" step="1" value="0"></label>
        <label>RAM expert cache cap (0 = auto)<input name="expert_cap" type="number" min="0" max="4096" step="1" value="0"></label>
        <label>Adaptive re-pin interval in tokens (0 = disabled)<input name="repin_tokens" type="number" min="0" max="65536" step="1" value="0"></label>
      </div>
      <label>GPU index<input name="gpu_index" type="number" min="0" max="15" step="1" value="0"></label>
      <label>Vulkan VRAM experts (0 = disable in manual mode)<input name="vulkan_experts" type="number" min="0" max="2048" step="1" value="96"></label>
      <p class="page-subtitle">Start conservatively on 6 GB GPUs. Tiered caching can enable a larger model, but storage I/O may still limit tokens/sec.</p>
      <div class="toolbar">
        <button class="btn primary" type="submit" id="a42ColibriSave">Save tier settings</button>
        <button class="btn" type="button" id="a42ColibriPlan" ${state?.plan_available?"":"disabled"}>View residency plan</button>
        <button class="btn" type="button" id="a42ColibriSwap">Hot swap now</button>
      </div>
      <div class="page-subtitle" id="a42ColibriTierStatus" role="status">${escapeHtml(state?.plan_note||"Placement can be inspected without loading a model into inference.")}</div>
      <pre id="a42ColibriPlanOutput" style="max-height:320px;overflow:auto;white-space:pre-wrap;" hidden></pre>
    </form>`);
  const form=$("#a42ColibriTierForm");
  if(!form)return;
  for(const key of ["mode","backend","ram_gb","expert_cap","repin_tokens","gpu_index","vulkan_experts"]){
    if(tier[key]!==undefined && form.elements[key]) form.elements[key].value=String(tier[key]);
  }
  const manual=form.querySelector("#a42ColibriManual");
  const refresh=()=>{
    manual.hidden=form.elements.mode.value!=="manual";
    form.elements.vulkan_experts.disabled=form.elements.backend.value!=="vulkan";
    form.elements.gpu_index.disabled=!["vulkan","cuda"].includes(form.elements.backend.value);
  };
  form.elements.mode.onchange=refresh;
  form.elements.backend.onchange=refresh;
  refresh();
  const status=form.querySelector("#a42ColibriTierStatus");
  // This status is the *whole Colibri model*, not the internal expert cache.
  try {
    const residency=await apiRequest(`${url}/colibri-swap?workspace_id=${ws}`);
    const active=Array.isArray(residency.active_deployment_ids)?residency.active_deployment_ids:[];
    const message=active.includes(id)?"This model is resident.":"This model is unloaded; Hot swap now will activate it.";
    const note=form.querySelector("#a42ColibriTierStatus");
    if(note)note.textContent=message+(active.length>0?` · ${active.length} Colibri runtime(s) currently resident on node.`:"");
  }catch{}

  form.onsubmit=async event=>{
    event.preventDefault();
    const btn=form.querySelector("#a42ColibriSave");
    btn.disabled=true;status.textContent="Saving tier configuration…";
    const number=k=>Number(form.elements[k].value||0);
    const settings={
      mode:form.elements.mode.value,backend:form.elements.backend.value,
      ram_gb:number("ram_gb"),expert_cap:number("expert_cap"),
      repin_tokens:number("repin_tokens"),gpu_index:number("gpu_index"),
      vulkan_experts:number("vulkan_experts")
    };
    try {
      await apiRequest(`${url}/colibri-tier`,{
        method:"PATCH",body:JSON.stringify({workspace_id:onepaneWorkspace,settings})
      });
      closeModal();notice("Colibri tier configuration saved. It will apply at the next model launch.");
      renderModels();
    }catch(ex){status.textContent=ex.message;btn.disabled=false}
  };
  form.querySelector("#a42ColibriSwap").onclick=async ()=>{
    const btn=form.querySelector("#a42ColibriSwap");
    const data=await a42SwapColibri(deployment,btn);
    if(data){const note=form.querySelector("#a42ColibriTierStatus");if(note)note.textContent="Resident model activated. Chat routing has not been changed."}
  };
  form.querySelector("#a42ColibriPlan").onclick=async ()=>{
    const button=form.querySelector("#a42ColibriPlan");
    const output=form.querySelector("#a42ColibriPlanOutput");
    button.disabled=true;status.textContent="Reading Colibri residency plan…";
    try {
      const plan=await apiRequest(`${url}/colibri-plan?workspace_id=${ws}`);
      output.hidden=false;output.textContent=JSON.stringify(plan,null,2);
      status.textContent="Read-only Colibri plan. No model was launched.";
    }catch(ex){status.textContent=ex.message}
    finally{button.disabled=false}
  };
}
