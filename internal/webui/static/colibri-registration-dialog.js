/* OnePane native Colibri registration — without native browser dialogs.
 * A Colibri model must already exist in the managed pool and contain config.json.
 */
qa5RegisterColibri=async function(){
 const hint="Select the directory containing the actual Colibri model (config.json), not the parent Models folder. Model registration does not download or convert weights.";
 openModal("Register Colibri model",`<form id="a44ColibriRegister" class="qa-form">
  <p class="page-subtitle">${escapeHtml(hint)}</p>
  <label>Colibri model directory
    <div class="toolbar">
      <input name="model_path" type="text" autocomplete="off" required placeholder="e.g. D:\\OnePane\\Models\\MyColibriModel" aria-label="Absolute Colibri model folder path">
      <button type="button" class="btn" id="a44ColibriBrowse">Browse folders</button>
    </div>
  </label>
  <label>Model reference/name
    <input name="model_ref" type="text" maxlength="180" required placeholder="e.g. Qwen-MoE-local" autocomplete="off">
  </label>
  <label>Display name
    <input name="display_name" type="text" maxlength="180" placeholder="Name shown in OnePane">
  </label>
  <label>Requested context tokens
    <select name="context_tokens">
      <option value="4096">4,096</option>
      <option value="8192" selected>8,192</option>
      <option value="16384">16,384</option>
      <option value="32768">32,768</option>
    </select>
  </label>
  <p class="page-subtitle">Requirements: the directory must be inside your configured OnePane model pool, contain config.json, and be compatible with Colibri. The model remains unqualified until Agent Check.</p>
  <div class="toolbar">
    <button class="btn primary" type="submit" id="a44ColibriRegisterSubmit">Register model</button>
    <button class="btn" type="button" id="a44ColibriRegisterCancel">Cancel</button>
  </div>
  <div id="a44ColibriRegisterStatus" role="status" aria-live="polite"></div>
 </form>`);
 const form=$("#a44ColibriRegister");if(!form)return;
 const feedback=$("#a44ColibriRegisterStatus"),submit=$("#a44ColibriRegisterSubmit");
 $("#a44ColibriRegisterCancel").onclick=closeModal;
 const deriveName=()=>{
  const pieces=String(form.elements.model_path.value||"").replace(/[\\/]+$/,"").split(/[\\/]/);
  const name=pieces[pieces.length-1]||"";
  if(name && !["models","managed","model-pool"].includes(name.toLowerCase())){
   if(!form.elements.model_ref.value.trim())form.elements.model_ref.value=name;
   if(!form.elements.display_name.value.trim())form.elements.display_name.value=name;
  }
 };
 form.elements.model_path.addEventListener("change",deriveName);
 $("#a44ColibriBrowse").onclick=async()=>{
  const b=$("#a44ColibriBrowse");b.disabled=true;
  feedback.textContent="Opening the folder picker…";
  try{
   const picked=await apiRequest("/desktop/folder-picker",{method:"POST",body:JSON.stringify({title:"Choose a Colibri model directory containing config.json"})});
   if(picked?.path){
    form.elements.model_path.value=String(picked.path);
    deriveName();feedback.textContent="Folder selected. Confirm it is the model folder containing config.json.";
   }else{feedback.textContent="No folder selected."}
  }catch(ex){feedback.innerHTML=`<div class="error">${escapeHtml(ex.message||"Folder picker unavailable. Enter an absolute path instead.")}</div>`}
  finally{b.disabled=false}
 };
 form.onsubmit=async ev=>{
  ev.preventDefault();
  const path=String(form.elements.model_path.value||"").trim();
  const modelRef=String(form.elements.model_ref.value||"").trim();
  const display=String(form.elements.display_name.value||"").trim()||modelRef;
  const tail=path.replace(/[\\/]+$/,"").split(/[\\/]/).pop()?.toLowerCase();
  if(!path||!modelRef||["models","managed","model-pool"].includes(tail)){
   feedback.innerHTML='<div class="error">Choose a specific Colibri model folder, and provide its model reference. Do not select the Models pool root.</div>';
   return
  }
  submit.disabled=true;feedback.textContent="Validating the model folder and registering the deployment…";
  try{
   if(!localProfileQA)await a31DetectHardware();
   const dep=await apiRequest("/v1/local-ai/colibri/register",{
    method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace,model_path:path,model_ref:modelRef,
      display_name:display,context_tokens:Number(form.elements.context_tokens.value)})
   });
   closeModal();
   notice("Colibri model registered. Run Agent Check to qualify its actual capabilities.");
   const deployments=await qa5LoadManagedDeployments();
   const id=String(dep.deployment_id||dep.id||"");
   const found=deployments.find(d=>d.deployment_id===id||d.model_ref===modelRef);
   if(found)await qa5InspectModel(found);
   if(typeof renderModels==="function"&&currentTab()?.route==="models")await renderModels();
  }catch(ex){feedback.innerHTML=`<div class="error">${escapeHtml(ex.message)}</div>`;submit.disabled=false}
 };
};
