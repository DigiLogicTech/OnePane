const a39DiscoverBase=a31RenderDiscoverModels;
a31RenderDiscoverModels=async function(){
 await a39DiscoverBase();
 const status=$("#a31DiscoverSourceStatus"),source=$("#a31DiscoverSource");if(!status||!source)return;
 const recovery=document.createElement("div");recovery.className="llmfit-discover-recovery";recovery.hidden=true;
 status.insertAdjacentElement("afterend",recovery);
 const refresh=()=>{
  if(!recovery.isConnected)return;
  const unavailable=source.value==="llmfit"&&/not configured|unavailable|not running|not installed/i.test(status.textContent||"");
  recovery.hidden=!unavailable;
  if(!unavailable)return;
  if(recovery.querySelector("button"))return;
  recovery.innerHTML='<strong>llmfit is unavailable</strong><p class="page-subtitle">Its model catalogue requires a healthy llmfit service on this node.</p><div class="toolbar"><button class="btn primary" id="a39InstallLLMFit">Install & start llmfit</button><button class="btn" id="a39ManageLLMFit">Open runtime settings</button></div><div class="error" id="a39InstallError"></div>';
  $("#a39ManageLLMFit").onclick=()=>{localStorage.setItem("onepane-managed-runtime-tab","llmfit");a31SetModelView("local")};
  $("#a39InstallLLMFit").onclick=async()=>{
   const btn=$("#a39InstallLLMFit");btn.disabled=true;btn.textContent="Installing llmfit…";
   try{
    const s=await apiRequest(`/v1/local-ai/llmfit?workspace_id=${encodeURIComponent(onepaneWorkspace)}`);
    if(!s.installed)await apiRequest("/v1/local-ai/llmfit/install",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});
    btn.textContent="Starting llmfit…";
    await apiRequest("/v1/local-ai/llmfit/start",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})});
    notice("llmfit started. Refreshing model discovery.");$("#a31DiscoverSearch")?.click();
   }catch(ex){btn.disabled=false;btn.textContent="Retry llmfit install";$("#a39InstallError").textContent=ex.message}
  }
 };
 const observer=new MutationObserver(refresh);observer.observe(status,{childList:true,subtree:true,characterData:true});
 source.addEventListener("change",()=>{recovery.innerHTML="";refresh()});refresh()
};
