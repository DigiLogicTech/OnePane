qa5AgentCheck=async function(dep){
 const id=String(dep?.deployment_id||"");
 if(!id)return notice("Agent Check requires a registered local deployment.","bad");
 let stage="validating deployment",sessionID="",failures=[];
 try{
  const deployments=await qa5LoadManagedDeployments();
  const current=deployments.find(x=>String(x.deployment_id)===id);
  if(!current){notice("This deployment is missing from the active inventory. Rescan Installed Models before running Agent Check.","bad");return}
  // Embedding models have no chat-completions endpoint. Do not overwrite a
  // successful vector qualification with misleading plain-text/JSON failures.
  if(/embedding/i.test(String(current.model_ref||""))){
   await qa5InspectModel(current);
   notice("Embedding qualification uses /v1/embeddings. Review vector results in the Spec Sheet.");
   return;
  }
  notice("Starting Agent Check for "+(current.display_name||current.model_ref||id)+"…");
  stage="starting testbed session";
  const session=await apiRequest(`/v1/model-deployments/${encodeURIComponent(id)}/testbed/sessions`,{method:"POST",body:JSON.stringify({notes:"OnePane manual Agent Check"})});
  sessionID=String(session.id||"");
  if(!sessionID)throw Error("testbed did not return a session identifier");
  const probes=[
   {label:"Plain-text response",payload:{prompt:"Reply with exactly: ONEPANE_OK",max_tokens:32}},
   {label:"JSON response",payload:{prompt:"Return a compact JSON object with keys status and number, where status is ok and number is 7.",max_tokens:96}},
   {label:"Tool calling",payload:{prompt:"Call the synthetic onepane_test_probe tool with value agent-check if supported; otherwise state tool calling is unavailable.",synthetic_tool_probe:true,max_tokens:128}}
  ];
  let completedProbes=0;
  for(const item of probes){
   stage=item.label;
   try{await apiRequest(`/v1/model-testbed/${encodeURIComponent(sessionID)}/turns`,{method:"POST",body:JSON.stringify(item.payload)});completedProbes++}
   catch(ex){
    failures.push(item.label+": "+ex.message);
    // A failed first inference commonly means the runtime never started. Avoid
    // three identical failing launches and do not manufacture qualification.
    if(!completedProbes)break;
   }
  }
  if(!completedProbes)throw Error("No inference probe succeeded. "+failures.join(" · ")+"; testbed remains unqualified");
  stage="saving qualification";
  await apiRequest(`/v1/model-testbed/${encodeURIComponent(sessionID)}/complete`,{method:"POST",body:"{}"});
  await qa5LoadManagedDeployments();
  const fresh=qa5ManagedDeployments.find(x=>String(x.deployment_id)===id)||current;
  await qa5InspectModel(fresh);
  if(failures.length)notice("Agent Check completed with limitations: "+failures.join(" · "),"bad");
  else notice("Agent Check completed; Spec Sheet updated and idle model unloaded.");
  if(currentTab()?.route==="models")renderModels();
 }catch(ex){
  // Failed first probes never reach /complete. Explicitly close the testbed
  // and release CPU/GPU allocation; report cleanup failures to the user.
  let cleanupWarning="";
  if(sessionID){
   try{
    await apiRequest(`/v1/model-testbed/${encodeURIComponent(sessionID)}/abort`,{
     method:"POST",body:JSON.stringify({reason:"Agent Check "+stage+" failed: "+ex.message})
    });
   }catch(cleanupError){cleanupWarning=" · Runtime cleanup: "+cleanupError.message}
  }
  const prefix=`Agent Check ${stage} failed: ${ex.message}`;
  const msg=/not found/i.test(ex.message)?" Verify this deployment still exists and its runtime is available.":""; 
  if(sessionID){try{await qa5InspectModel(dep)}catch{}}
  notice(prefix+msg+(sessionID?" · Session: "+sessionID:"")+cleanupWarning,"bad");
  if(/managed model file missing|managed model file inaccessible|managed model path is not a file|managed model unavailable/i.test(ex.message)){
    openModal("Repair local model",`<div class="widget-body"><p>The registered model artifact is missing or inaccessible. Agent Check cannot run until the installation is repaired.</p><p class="page-subtitle">Rescan disables stale registrations but preserves files, projects and settings. You can then reinstall a verified artifact.</p><div class="toolbar"><button class="btn primary" id="qa5RescanReinstall">Rescan & reinstall</button><button class="btn" id="qa5RepairCancel">Cancel</button></div><div id="qa5RepairFeedback" class="error"></div></div>`);
    document.querySelector("#qa5RepairCancel")?.addEventListener("click",closeModal);
    document.querySelector("#qa5RescanReinstall")?.addEventListener("click",async()=>{
      const button=document.querySelector("#qa5RescanReinstall");button.disabled=true;
      try{
        const report=await apiRequest("/v1/local-ai/deployments/reconcile",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});
        const catalog=a31Array(await apiRequest("/v1/local-ai/catalog"));
        const model=catalog.find(m=>String(m.model_ref||"").toLowerCase()===String(current.model_ref||"").toLowerCase());
        closeModal();notice(`Rescan completed: ${report.removed_stale||0} stale registrations disabled.`);
        if(model?.installable)await a31InstallModel(model);
        else notice("Find a verified artifact in Discover Models to reinstall this model.","bad");
      }catch(repairError){const target=document.querySelector("#qa5RepairFeedback");if(target)target.textContent=repairError.message;button.disabled=false}
    });
  }
 }
};

/* A single explicit configuration/check workflow. It never silently changes
 * the deployment compute policy or treats planned placement as measured GPU
 * residency. Results remain separate from production admission. */
const a43AgentCheckBusy=new Set();
const a43RunAgentCheck=qa5AgentCheck;
qa5AgentCheck=async function(dep){
 const id=String(dep?.deployment_id||"");if(!id)return;
 if(a43AgentCheckBusy.has(id))return notice("Agent Check already running for this model.","bad");
 let current=dep,policy={};
 try{
  const ds=await qa5LoadManagedDeployments();
  current=ds.find(d=>d.deployment_id===id)||dep;
  policy=await apiRequest("/v1/local-ai/deployments/"+encodeURIComponent(id)+"/compute-policy?workspace_id="+encodeURIComponent(onepaneWorkspace));
 }catch(e){/* Configuration read errors do not imply a placement change. */}
 const compute=String(policy.preference||current.compute_mode||"auto");
 const mode=String(policy.placement_mode||"automatic");
 openModal("Agent Check · "+String(current.display_name||current.model_ref||"Model"),`<div class="widget-body a43-agent-check-review"><p class="page-subtitle">Agent Check tests the configured deployment. Select compute separately if it needs changing; GPU Required must not silently fall back to CPU.</p><dl class="definition-grid"><dt>Quantization</dt><dd>${escapeHtml(current.quantization||"Unknown")}</dd><dt>Compute policy</dt><dd>${escapeHtml(compute)}</dd><dt>Planned placement</dt><dd>${escapeHtml(mode)} (not measured residency)</dd><dt>Current Agent Check</dt><dd>${escapeHtml(current.agent_check_status||"Not run")}</dd><dt>Production admission</dt><dd>${escapeHtml(current.admission_status||"pending")}</dd></dl><p class="list-meta">Completing this check tests inference and records evidence; it does not grant production admission or verify the full context window.</p><div class="toolbar"><button class="btn" id="a43AgentCheckCancel">Cancel</button><button class="btn" id="a43AgentCheckCompute">Configure compute</button><button class="btn primary" id="a43AgentCheckRun">Run Agent Check</button></div></div>`);
 $("#a43AgentCheckCancel").onclick=closeModal;
 $("#a43AgentCheckCompute").onclick=()=>{closeModal();a31OpenCompute(current)};
 $("#a43AgentCheckRun").onclick=async()=>{
  if(a43AgentCheckBusy.has(id))return;
  a43AgentCheckBusy.add(id);
  closeModal();
  try{await a43RunAgentCheck(current)}
  finally{
   a43AgentCheckBusy.delete(id);
   if(currentTab()?.route==="models")await renderModels();
  }
 };
};
