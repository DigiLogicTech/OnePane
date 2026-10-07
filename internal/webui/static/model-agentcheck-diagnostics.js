qa5AgentCheck=async function(dep){
 const id=String(dep?.deployment_id||"");
 if(!id)return notice("Agent Check requires a registered local deployment.","bad");
 let stage="validating deployment",sessionID="",failures=[];
 try{
  const deployments=await qa5LoadManagedDeployments();
  const current=deployments.find(x=>String(x.deployment_id)===id);
  if(!current){notice("This deployment is missing from the active inventory. Rescan Installed Models before running Agent Check.","bad");return}
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
  for(const item of probes){
   stage=item.label;
   try{await apiRequest(`/v1/model-testbed/${encodeURIComponent(sessionID)}/turns`,{method:"POST",body:JSON.stringify(item.payload)})}
   catch(ex){failures.push(item.label+": "+ex.message)}
  }
  stage="saving qualification";
  await apiRequest(`/v1/model-testbed/${encodeURIComponent(sessionID)}/complete`,{method:"POST",body:"{}"});
  await qa5LoadManagedDeployments();
  const fresh=qa5ManagedDeployments.find(x=>String(x.deployment_id)===id)||current;
  await qa5InspectModel(fresh);
  if(failures.length)notice("Agent Check completed with limitations: "+failures.join(" · "),"bad");
  else notice("Agent Check completed; Spec Sheet updated.");
  if(currentTab()?.route==="models")renderModels();
 }catch(ex){
  const prefix=`Agent Check ${stage} failed: ${ex.message}`;
  const msg=/not found/i.test(ex.message)?" Verify this deployment still exists and its runtime is available.":""; 
  notice(prefix+msg+(sessionID?" · Session: "+sessionID:""),"bad");
 }
};
