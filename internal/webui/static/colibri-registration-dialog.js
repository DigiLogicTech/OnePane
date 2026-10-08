/* Colibri registration from the configured managed model pool.
 * Initial context is a model-declared provisional ceiling (capped by the
 * backend), never a verified context until Agent Check records evidence.
 */
qa5RegisterColibri=async function(){
 openModal("Register Colibri model",`<form id="a44ColibriRegister" class="qa-form">
  <p class="page-subtitle">Select a compatible model folder already in your OnePane model pool. Only folders containing config.json are shown.</p>
  <label>Available models
    <select name="model_path" id="a44ColibriModel" required><option value="">Scanning model pool…</option></select>
  </label>
  <div class="list-meta" id="a44ColibriModelDetails"></div>
  <label>Display name <span class="list-meta">(optional)</span>
    <input name="display_name" type="text" maxlength="180" placeholder="Use model folder name" autocomplete="off">
  </label>
  <p class="page-subtitle">Context: Automatic. OnePane reads the model's declared maximum, applies a safe initial cap and marks it unverified until Agent Check. The model is not qualified for use simply by registering it.</p>
  <div class="toolbar">
    <button class="btn primary" type="submit" id="a44ColibriRegisterSubmit" disabled>Register model</button>
    <button class="btn" type="button" id="a44ColibriRegisterRefresh">Refresh list</button>
    <button class="btn" type="button" id="a44ColibriRegisterCancel">Cancel</button>
  </div>
  <div id="a44ColibriRegisterStatus" role="status" aria-live="polite"></div>
 </form>`);
 const form=$("#a44ColibriRegister");if(!form)return;
 const select=form.elements.model_path,submit=$("#a44ColibriRegisterSubmit"),
       detail=$("#a44ColibriModelDetails"),feedback=$("#a44ColibriRegisterStatus");
 let candidates=[];
 $("#a44ColibriRegisterCancel").onclick=closeModal;
 const selected=()=>candidates.find(x=>x.model_path===select.value);
 const formatTokens=n=>Number(n||0)>0?Number(n).toLocaleString()+" tokens":"Not declared";
 const sync=()=>{
  const candidate=selected(), valid=!!candidate&&!candidate.registered;
  submit.disabled=!valid;
  if(!candidate){detail.textContent="Select an available model to preview its settings.";return}
  detail.textContent=candidate.registered?"Already registered. Open it from Installed Models.":
    "Declared context: "+formatTokens(candidate.reported_context_tokens)+
    " · Provisional initial limit: "+formatTokens(candidate.initial_context_tokens)+
    " · Agent Check required for qualification.";
 };
 select.onchange=sync;
 const load=async()=>{
  submit.disabled=true;
  select.disabled=true;
  detail.textContent="Inspecting model directories…";
  feedback.textContent="";
  try{
   const data=await apiRequest("/v1/local-ai/colibri/pool-models?workspace_id="+encodeURIComponent(onepaneWorkspace));
   candidates=a31Array(data);
   select.innerHTML='<option value="">Select a model in the pool</option>'+
    candidates.map((x,i)=>`<option value="${escapeHtml(x.model_path)}" ${x.registered?"disabled":""}>${escapeHtml(x.display_name||x.model_ref||"Model "+(i+1))}${x.registered?" — registered":""}</option>`).join("");
   if(!candidates.length)feedback.textContent="No compatible model folder found. A Colibri model must be placed in a subdirectory of your configured model pool and contain a valid config.json.";
   select.disabled=!candidates.some(x=>!x.registered);
   sync();
  }catch(ex){
   candidates=[];select.innerHTML='<option value="">Unable to read model pool</option>';
   feedback.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;
   detail.textContent="";
  }
 };
 $("#a44ColibriRegisterRefresh").onclick=load;
 form.onsubmit=async event=>{
  event.preventDefault();
  const candidate=selected();
  if(!candidate||candidate.registered){feedback.textContent="Select a model that has not already been registered.";return}
  submit.disabled=true;feedback.textContent="Registering selected model with automatic provisional context…";
  try{
   if(!localProfileQA)await a31DetectHardware();
   const modelRef=String(candidate.model_ref||candidate.display_name||"").trim();
   const display=String(form.elements.display_name.value||"").trim()||String(candidate.display_name||modelRef);
   const dep=await apiRequest("/v1/local-ai/colibri/register",{
     method:"POST",body:JSON.stringify({
       workspace_id:onepaneWorkspace,model_path:candidate.model_path,model_ref:modelRef,
       display_name:display,context_tokens:0
     })
   });
   closeModal();
   notice("Colibri model registered as unqualified. Run Agent Check before using it.");
   const deployments=await qa5LoadManagedDeployments();
   const id=String(dep.deployment_id||dep.id||"");
   const found=deployments.find(x=>x.deployment_id===id||(x.model_ref===modelRef&&String(x.runtime_name).toLowerCase()==="colibri"));
   if(found)await qa5InspectModel(found);
   if(typeof renderModels==="function"&&currentTab()?.route==="models")await renderModels();
  }catch(ex){feedback.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;submit.disabled=false}
 };
 await load();
};
