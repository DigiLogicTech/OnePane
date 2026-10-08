/* Global OnePane Assistant model selection, scoped to one Assistant thread.
 * This component cannot modify Project Orchestrator routing. */
async function a31AssistantModelPicker(threadID) {
 const qs=encodeURIComponent(onepaneWorkspace),modelsPath="/v1/scheduler/candidates?workspace_id="+qs+"&capability_id=inference.general&role_name=onepane-assistant";
 const [threadResult,candidatesResult]=await Promise.allSettled([
  apiRequest("/v1/assistant/threads?workspace_id="+qs+"&limit=100"),
  apiRequest(modelsPath)
 ]);
 const threads=threadResult.status==="fulfilled"?a31Array(threadResult.value):[];
 const thread=threads.find(t=>String(t.id)===String(threadID));
 const preference=String(thread?.preferred_model_deployment_id||"");
 const loaded=candidatesResult.status==="fulfilled"&&Array.isArray(candidatesResult.value);
 const candidates=loaded?candidatesResult.value.filter(c=>c.kind==="model_deployment"):[];
 const known=candidates.some(c=>String(c.id)===preference);
 // No model selection is allowed if current eligibility could not be read.
 const opts=candidates.map(c=>{
  const ready=!!c.schedulable&&["ready","degraded"].includes(String(c.status||""));
  const costly=!["local","free"].includes(String(c.cost_class||"unknown"))||
   c.cost_class==="free"&&!c.local;
  const available=ready&&!costly;
  const label=[c.display_name||c.id,c.provider|| (c.local?"Local":"Remote"),!ready?"Not admitted":costly?"Usage policy required":""].filter(Boolean).join(" · ");
  return '<option value="'+escapeHtml(c.id)+'" '+(preference===String(c.id)?'selected ':'')+(available?'':'disabled ')+'>'+escapeHtml(label)+'</option>';
 }).join("");
 const missing=preference&&!known?'<option value="'+escapeHtml(preference)+'" selected disabled>Previously selected model unavailable</option>':"";
 const warning=!loaded?"Model inventory unavailable; automatic routing remains available.":!candidates.length?"No eligible model deployments are registered. Configure a model under Models.":"Selecting a model pins Assistant responses to that deployment (no automatic substitution).";
 const ui='<div class="control-chat-model-row"><label for="a31AssistantModel">Assistant model</label><select id="a31AssistantModel" aria-label="Assistant model" '+(!loaded?'disabled':'')+'><option value="" '+(!preference?'selected':'')+'>Auto (model routing)</option>'+missing+opts+'</select></div><div class="control-chat-model-status" role="status">'+escapeHtml(warning)+'</div>';
 return {ui,selected:preference,loaded};
}
function a31BindAssistantModelPicker(threadID,picker){
 const select=document.querySelector("#a31AssistantModel");if(!select)return;
 select.onchange=async()=>{
  const target=String(select.value||""),previous=picker.selected;
  select.disabled=true;
  const status=document.querySelector(".control-chat-model-status");
  if(status)status.textContent="Saving Assistant model selection…";
  try{
   await apiRequest("/v1/assistant/threads/"+encodeURIComponent(threadID)+"/model",{
    method:"PUT",body:JSON.stringify({deployment_id:target||null})
   });
   notice(target?"Assistant model selected.":"Assistant returned to automatic model routing.");
   await a31RenderControlChat();
  }catch(err){
   select.value=previous;
   select.disabled=false;
   if(status)status.textContent=err.message;
   notice("Assistant model selection: "+err.message,"bad");
  }
 };
}
