/* OnePane federated Nodes control plane — backed by node-scoped APIs.
 * A selected node is never inferred from the operator's browser location. */
var nextNodeUI={nodes:[],selected:"",tab:"overview",manifest:null,grant:null,policy:null,capError:"",jobs:[],localMetrics:null,localHardware:null,localModels:[],localModelsAvailable:false};
function nextNodeEsc(v){return escapeHtml(String(v==null?"":v));}
function nextNodeID(n){return String(n.id||n.node_id||"");}
function nextNodeName(n){return n.name||n.hostname||n.display_name||nextNodeID(n)||"Node";}
function nextNodeState(n){return n.local?"local":(n.status||n.state||n.trust_state||"unknown");}
function nextNodeArray(v){return Array.isArray(v)?v:[];}
function nextNodeWhen(value){
 if(value==null||value==="")return "Not reported";
 const n=Number(value);if(!Number.isFinite(n)||n<=0)return "Not reported";
 const ms=n<1e11?n*1000:n;
 return new Date(ms).toLocaleString();
}
function nextNodeNumber(v){return v==null||!Number.isFinite(Number(v))?null:Number(v);}
function nextNodeCount(v){return v==null?"—":String(v);}
function nextNodeMetricBytes(value){return nextNodeNumber(value)==null?"Not reported":bytesQA(Number(value));}
function nextNodeLocalManifest(hardware,metrics,models,modelsAvailable){
 const total=nextNodeNumber(metrics?.memory_total_bytes)??nextNodeNumber(hardware?.memory?.total_bytes);
 const available=nextNodeNumber(metrics?.memory_available_bytes)??nextNodeNumber(hardware?.memory?.available_bytes);
 const free=nextNodeNumber(metrics?.storage_volumes?.find(v=>nextNodeArray(v.roles).includes("Models"))?.free_bytes)
  ??nextNodeNumber(hardware?.storage?.available_bytes)??nextNodeNumber(metrics?.disk_free_bytes);
 const gpus=nextNodeArray(hardware?.gpus).length?hardware.gpus:nextNodeArray(metrics?.gpus);
 const pools=nextNodeArray(hardware?.gpus).length||nextNodeArray(metrics?.gpus).length?
  gpus.map((g,i)=>({id:g.device_id||String(i),kind:"gpu",name:g.name||"GPU "+i,backend:g.backend||nextNodeArray(g.backends).join(", ")||"Auto",capacity_bytes:g.vram_bytes??g.vram_total_bytes??0,available_bytes:g.free_vram_bytes??null,utilization_pct:g.usage_percent??null})):null;
 return {protocol:"local",node_id:hardware?.node_id||"",models:modelsAvailable?models:null,
  compute:{status:"local",cpu_usage_pct:metrics?.cpu_percent??null,
   memory_total_bytes:total,memory_available_bytes:available,
   storage_available_bytes:free,pools:pools,cpu_name:hardware?.cpu?.name||null},
  source:"local Metrics and Local Models; no federation manifest required"};
}
async function nextNodeLoadLocal(id){
 const node=nextNodeUI.nodes.find(n=>nextNodeID(n)===id);
 if(!node?.local)return;
 const tasks=[
  apiRequest("/v1/system/metrics?workspace_id="+encodeURIComponent(onepaneWorkspace||"")),
  onepaneWorkspace?apiRequest("/v1/local-ai/deployments?workspace_id="+encodeURIComponent(onepaneWorkspace)):Promise.reject(Error("No active workspace")),
  (typeof localProfileQA!=="undefined"&&localProfileQA)?Promise.resolve(localProfileQA):
   nextNodeUI.localHardware?Promise.resolve(nextNodeUI.localHardware):
   onepaneWorkspace?apiRequest("/v1/local-ai/detect",{method:"POST",body:JSON.stringify({workspace_id:onepaneWorkspace})}):
   Promise.reject(Error("Select a Workspace to detect hardware"))
 ];
 const results=await Promise.allSettled(tasks);
 if(nextNodeUI.selected!==id)return;
 const metrics=results[0].status==="fulfilled"?results[0].value:null;
 const dep=results[1].status==="fulfilled"?results[1].value:null;
 const hardware=results[2].status==="fulfilled"?results[2].value:null;
 nextNodeUI.localMetrics=metrics;
 if(hardware?.cpu||nextNodeArray(hardware?.gpus).length)nextNodeUI.localHardware=hardware;
 const modelRows=Array.isArray(dep)?dep:dep?.deployments;
 nextNodeUI.localModelsAvailable=Array.isArray(modelRows);
 nextNodeUI.localModels=nextNodeArray(modelRows);
 nextNodeUI.manifest=nextNodeLocalManifest(nextNodeUI.localHardware,metrics,nextNodeUI.localModels,nextNodeUI.localModelsAvailable);
 nextNodeUI.capError=results[0].status==="rejected"&&results[1].status==="rejected"?
   "Local telemetry and model inventory unavailable; check your session permissions.":"";
}
function nextNodeRows(){
 var rows=nextNodeUI.nodes,online=rows.filter(function(n){return n.local||n.trust_state==="paired";}).length;
 var manifest=nextNodeUI.manifest,pools=manifest?.compute?.pools,models=manifest?.models;
 var gpu=Array.isArray(pools)?pools.filter(p=>p.kind==="gpu").length:null;
 return '<div class="host-metrics-grid node-summary-grid">'+
  [['Nodes',rows.length],['Trusted / local',online],['Selected GPU pools',nextNodeCount(gpu)],['Selected models',nextNodeCount(Array.isArray(models)?models.length:null)]]
  .map(function(row){return '<section class="host-metric-tile"><strong>'+nextNodeEsc(row[0])+'</strong><div class="host-metric-value">'+nextNodeEsc(row[1])+'</div></section>';}).join("")+'</div>';
}
function nextNodeJobKey(id){return "onepane:nodes:jobs:"+id;}
function nextNodeReadJobs(id){try{return nextNodeArray(JSON.parse(localStorage.getItem(nextNodeJobKey(id))||"[]"));}catch{return [];}}
function nextNodeAddJob(id,job){
 var jobs=nextNodeReadJobs(id).filter(function(x){return x.id!==job.id;});
 jobs.unshift({id:job.id,model_ref:job.model_ref||"Model",quantization:job.quantization||""});
 localStorage.setItem(nextNodeJobKey(id),JSON.stringify(jobs.slice(0,40)));
 nextNodeUI.jobs=jobs;
}
async function renderNodes(){
 var host=$("#viewHost");
 host.innerHTML='<section class="page nodes-page">'+pageHeader("Nodes","Central management for local and paired Windows/Ubuntu compute nodes.",'<button class="btn primary" id="a31AddNode">Add Node</button>')+'<div id="a31Nodes"><div id="nextNodeRoot"><div class="widget-body">Loading registered nodes…</div></div></div></section>';
 $("#a31AddNode").onclick=openPairNode;
 try{
  var result=await apiRequest("/v1/nodes"),rows=nextNodeArray(Array.isArray(result)?result:result.nodes);
  nextNodeUI.nodes=rows;liveOps.nodes=rows;liveOps.reported.nodes=true;
  if(!rows.some(function(n){return nextNodeID(n)===nextNodeUI.selected;}))nextNodeUI.selected=rows.length?nextNodeID(rows[0]):"";
  if(nextNodeUI.selected)await nextNodeLoadDetail(nextNodeUI.selected);
  nextNodeDraw();
 }catch(ex){$("#nextNodeRoot").innerHTML='<div class="error">'+nextNodeEsc(ex.message)+'</div>';}
 bindViewActions(host);
}
async function nextNodeLoadDetail(id){
 nextNodeUI.manifest=null;nextNodeUI.grant=null;nextNodeUI.policy=null;nextNodeUI.capError="";
 nextNodeUI.jobs=nextNodeReadJobs(id);
 const node=nextNodeUI.nodes.find(n=>nextNodeID(n)===id);
 if(node?.local){await nextNodeLoadLocal(id);return}
 var checks=await Promise.allSettled([
  apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/capabilities"),
  apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-management"),
  apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/compute-policy")
 ]);
 if(nextNodeUI.selected!==id)return;
 if(checks[0].status==="fulfilled")nextNodeUI.manifest=checks[0].value;
 else nextNodeUI.capError=String(checks[0].reason&&checks[0].reason.message||"Capabilities unavailable");
 if(checks[1].status==="fulfilled")nextNodeUI.grant=checks[1].value;
 if(checks[2].status==="fulfilled")nextNodeUI.policy=checks[2].value;
}
function nextNodeDraw(){
 var root=$("#nextNodeRoot");if(!root)return;
 // Metrics updates rerender this list every five seconds. Do not reset the
 // operator's scroll position while Logs changes the available viewport.
 const previousScroll=root.scrollTop;
 var node=nextNodeUI.nodes.find(function(n){return nextNodeID(n)===nextNodeUI.selected;});
 var top=nextNodeRows();
 var list='<div class="node-grid">'+nextNodeUI.nodes.map(function(n){
  var id=nextNodeID(n),selected=id===nextNodeUI.selected,st=nextNodeState(n);
  return '<article class="panel-card node-card'+(selected?' next-node-selected':'')+'" data-a31-node="'+nextNodeEsc(id)+'"><div class="card-header"><div><div class="card-title">'+nextNodeEsc(nextNodeName(n))+(n.local?' (Local)':'')+'</div><div class="list-meta">'+nextNodeEsc(id)+'</div></div><span class="pill">'+nextNodeEsc(st)+'</span></div><div class="widget-body"><div class="list-meta">'+(n.local?'Local machine · live metrics':"Last seen: "+nextNodeEsc(nextNodeWhen(n.last_seen_at)))+'</div><button class="btn'+(selected?' primary':'')+'" data-next-node="'+nextNodeEsc(id)+'">Manage node</button></div></article>';
 }).join("")+'</div>';
 var detail=node?nextNodeDetail(node):'<section class="panel-card"><div class="widget-body">No enrolled nodes. Select Add Node to begin a mutually confirmed pairing.</div></section>';
 root.innerHTML=top+list+detail;
 root.scrollTop=previousScroll;
 $$("[data-next-node]",root).forEach(function(b){b.onclick=async function(){nextNodeUI.selected=b.dataset.nextNode;nextNodeUI.tab="overview";await nextNodeLoadDetail(nextNodeUI.selected);nextNodeDraw();};});
 $$("[data-next-node-tab]",root).forEach(function(b){b.onclick=function(){nextNodeUI.tab=b.dataset.nextNodeTab;nextNodeDraw();};});
 $("#nextNodeQAEvidence")?.addEventListener("click",()=>nextNodeShowQAEvidence(nextNodeUI.selected));
 $("#nextNodeRefresh")?.addEventListener("click",function(){nextNodeLoadDetail(nextNodeUI.selected).then(nextNodeDraw).catch(function(e){notice(e.message,"bad");});});
 $("#nextNodePairAgain")?.addEventListener("click",openPairNode);
 $("#nextNodeOpenModels")?.addEventListener("click",function(){openRoute("models");});
 $("#nextNodeRecommendations")?.addEventListener("click",nextNodeRecommendations);
 $("#nextNodeInstall")?.addEventListener("click",nextNodeInstallDialog);
 $("#nextNodePolicySave")?.addEventListener("click",nextNodeSavePolicy);
 $("#nextNodeGrant")?.addEventListener("click",nextNodeToggleGrant);
 $("#nextNodeRevoke")?.addEventListener("click",nextNodeRevoke);
 $$("[data-next-node-job]",root).forEach(function(b){b.onclick=function(){nextNodeInspectJob(b.dataset.nextNodeJob);};});
 $$("[data-next-node-spec]",root).forEach(function(b){b.onclick=function(){
  const dep=b.dataset.nextNodeSpec,local=nextNodeUI.nodes.find(n=>nextNodeID(n)===nextNodeUI.selected)?.local;
  if(local){const model=nextNodeUI.localModels.find(m=>m.deployment_id===dep);if(model)qa5InspectModel(model);else notice("Local deployment unavailable","bad");}
  else nextNodeShowSpec(dep);
 };});
 $$("[data-next-node-check]",root).forEach(function(b){b.onclick=function(){
  const dep=b.dataset.nextNodeCheck,local=nextNodeUI.nodes.find(n=>nextNodeID(n)===nextNodeUI.selected)?.local;
  if(local){const model=nextNodeUI.localModels.find(m=>m.deployment_id===dep);if(model)qa5AgentCheck(model);else notice("Local deployment unavailable","bad");}
  else nextNodeAgentCheck(dep);
 };});
}
function nextNodeDetail(n){
 var tabs=[["overview","Overview"],["models","Models"],["compute","Compute"],["access","Access & Policy"],["activity","Activity"]];
 var nav='<div class="subtabs next-node-tabs">'+tabs.map(function(t){return '<button class="subtab'+(nextNodeUI.tab===t[0]?' active':'')+'" data-next-node-tab="'+t[0]+'">'+t[1]+'</button>';}).join("")+(n.local?'<button class="btn next-node-local-models" id="nextNodeOpenModels" type="button">Manage local models</button>':'')+'</div>';
 var header='<div class="card-header"><div><div class="card-title">'+nextNodeEsc(nextNodeName(n))+' · Management</div><div class="list-meta">'+nextNodeEsc(n.trust_state||"Local node")+' · '+nextNodeEsc(n.id)+'</div></div><div class="toolbar"><button class="btn" type="button" id="nextNodeQAEvidence">QA evidence</button><button class="btn" id="nextNodeRefresh">Refresh</button></div></div>';
 var body=nextNodeUI.tab==="models"?nextNodeModels(n):nextNodeUI.tab==="compute"?nextNodeCompute(n):nextNodeUI.tab==="access"?nextNodeAccess(n):nextNodeUI.tab==="activity"?nextNodeActivity(n):nextNodeOverview(n);
 return '<section class="panel-card next-node-manager">'+header+nav+'<div class="widget-body">'+body+'</div></section>';
}
function nextNodeOverview(n){
 var manifest=nextNodeUI.manifest||{},compute=manifest.compute||{},pools=compute.pools;
 var rows=[["Name",nextNodeName(n)],["Node identity",nextNodeID(n)],["Trust",n.trust_state],
 ["Endpoint",n.local?"Local management; no remote endpoint":(n.endpoint?JSON.stringify(n.endpoint):"Not advertised")],
 ["Compute state",compute.status||"Not reported"],["CPU",compute.cpu_name||"Not reported"],
 ["CPU usage",compute.cpu_usage_pct==null?"Collecting samples…":Number(compute.cpu_usage_pct).toFixed(1)+"%"],
 ["Available RAM",nextNodeMetricBytes(compute.memory_available_bytes)],
 ["Available storage",nextNodeMetricBytes(compute.storage_available_bytes)],
 ["Last seen",n.local?"Local (active session)":nextNodeWhen(n.last_seen_at)]];
 return (nextNodeUI.capError?'<p class="error">'+nextNodeEsc(nextNodeUI.capError)+'</p>':'')+
  '<dl class="definition-grid">'+rows.map(function(r){return '<dt>'+nextNodeEsc(r[0])+'</dt><dd>'+nextNodeEsc(r[1]??"—")+'</dd>';}).join("")+'</dl>'+
  '<h3>Compute pools</h3>'+(Array.isArray(pools)?(pools.length?pools.map(function(p){return '<div class="list-row"><div class="list-main"><strong>'+nextNodeEsc(p.name||p.kind)+'</strong><div class="list-meta">'+nextNodeEsc(p.backend||p.kind)+' · '+(p.capacity_bytes?nextNodeEsc(bytesQA(p.capacity_bytes))+" capacity":"Capacity not reported")+(p.utilization_pct==null?"":" · "+Number(p.utilization_pct).toFixed(1)+"% utilisation")+'</div></div></div>';}).join(""):'<p class="page-subtitle">No accelerator pools detected.</p>'):'<p class="page-subtitle">Compute inventory not yet reported.</p>');
}
function nextNodeModels(n){
 var models=nextNodeArray(nextNodeUI.manifest&&nextNodeUI.manifest.models);
 var toolbar=n.local?'':'<button class="btn" id="nextNodeRecommendations">Recommendations</button> <button class="btn primary" id="nextNodeInstall">Install on this node</button>';
 return '<p class="page-subtitle">Model weights, runtimes and inference are hosted on the selected node. Local and remote model installations use the same trusted catalogue.</p><div class="toolbar node-model-actions">'+toolbar+'</div>'+
 (models.length?'<div class="table-shell"><table class="data-table"><thead><tr><th>Model</th><th>Runtime</th><th>Status</th><th>Actions</th></tr></thead><tbody>'+models.map(function(m){var id=m.deployment_id||"";return '<tr><td><strong>'+nextNodeEsc(m.model_ref)+'</strong><div class="list-meta">'+nextNodeEsc(m.quantization||"")+'</div></td><td>'+nextNodeEsc(m.runtime_name||"—")+'</td><td>'+nextNodeEsc(m.qualification||m.status||"Unknown")+'</td><td><button class="btn tiny" data-next-node-spec="'+nextNodeEsc(id)+'">Spec Sheet</button> <button class="btn tiny" data-next-node-check="'+nextNodeEsc(id)+'">Agent Check</button></td></tr>';}).join("")+'</tbody></table></div>':(nextNodeUI.manifest?.models==null?'<div class="empty-state compact">Model inventory not reported.</div>':'<div class="empty-state compact">No managed deployments reported on this node.</div>'));
}
function nextNodeCompute(n){
 var p=nextNodeUI.policy||{};
 return '<p class="page-subtitle">Configure scheduling eligibility and compute access independently from model-management trust.</p>'+
 '<div class="qa-form"><label class="inline-check"><input type="checkbox" id="nextNodeEnabled"'+(p.enabled?' checked':'')+'> Participate in federated scheduling</label>'+
 '<label class="inline-check"><input type="checkbox" id="nextNodeIdle"'+(p.idle_only!==false?' checked':'')+'> Schedule only when idle</label>'+
 '<label class="inline-check"><input type="checkbox" id="nextNodeDownloads"'+(p.allow_model_downloads?' checked':'')+'> Allow remote model downloads</label>'+
 '<label>Runtime installation<select id="nextNodeRuntimePolicy">'+["deny","confirm","allow"].map(function(x){return '<option value="'+x+'"'+((p.runtime_installation||"confirm")===x?' selected':'')+'>'+titleCase(x)+'</option>';}).join("")+'</select></label>'+
 '<label>Project scope<select id="nextNodeProjectScope"><option value="selected"'+(p.project_scope!=="all"?' selected':'')+'>Selected projects only</option><option value="all"'+(p.project_scope==="all"?' selected':'')+'>All projects</option></select></label>'+
 '<button class="btn primary" id="nextNodePolicySave">Save compute policy</button></div>'+
 '<p class="page-subtitle">Resource ceilings, schedules and project allowlists remain available in the advanced policy record; this form preserves existing values.</p>';
}
function nextNodeAccess(n){
 var grant=nextNodeUI.grant||{};
 return '<dl class="definition-grid"><dt>Node</dt><dd>'+nextNodeEsc(nextNodeName(n))+'</dd><dt>Trust</dt><dd>'+nextNodeEsc(n.trust_state||"local")+'</dd><dt>Remote model management</dt><dd>'+nextNodeEsc(grant.enabled?"Enabled":"Disabled")+'</dd></dl>'+
 (n.local?'<p class="page-subtitle">The local node is administered directly. Remote management grants apply to paired peers.</p>':
 '<div class="toolbar"><button class="btn" id="nextNodeGrant">'+(grant.enabled?"Disable":"Enable")+' remote model management</button><button class="btn danger" id="nextNodeRevoke">Revoke pairing</button></div>')+
 '<p class="page-subtitle">Pairing and management grants never implicitly grant access to Project or Library content.</p>';
}
function nextNodeActivity(n){
 return '<p class="page-subtitle">Recent remote model jobs are retained on this control-plane installation. Open a job to fetch its current state directly from the target node.</p>'+
 (nextNodeUI.jobs.length?nextNodeUI.jobs.map(function(j){return '<div class="list-row"><div class="list-main"><strong>'+nextNodeEsc(j.model_ref)+'</strong><div class="list-meta">'+nextNodeEsc(j.id)+'</div></div><button class="btn tiny" data-next-node-job="'+nextNodeEsc(j.id)+'">View progress</button></div>';}).join(""):'<div class="empty-state compact">No remote installation jobs recorded for this node.</div>');
}
// Admin-scoped, read-only Node QA evidence. Neither the preview nor export
// calls a remote Node or collects logs/credentials. The authorized API returns
// only fixed enums/timestamps and historical counts for the selected Node.
async function nextNodeShowQAEvidence(nodeID){
 const selected=String(nodeID||"");
 if(!selected)return;
 openModal("Node QA evidence",'<div class="widget-body">'+
  '<p class="page-subtitle">Read-only control-plane report. For the local Node only, a systemd/Windows SCM status may be queried; remote services are not collected. A service marked running does not establish application readiness, and historical Node timestamps do not prove reachability. Pairing material, raw errors, telemetry and remote payloads are excluded.</p>'+
  '<p class="list-meta" id="nextNodeQAStatus" role="status">Checking Node administrator permissions and recorded evidence…</p>'+
  '<pre class="json-preview" id="nextNodeQAPreview" style="max-height:45vh;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere"></pre>'+
  '<div class="toolbar"><button class="btn" type="button" id="nextNodeQAClose">Close</button>'+
  '<button class="btn" type="button" id="nextNodeQADownload" disabled>Download reviewed JSON</button></div></div>');
 const preview=document.querySelector("#nextNodeQAPreview");
 const status=document.querySelector("#nextNodeQAStatus");
 const exportButton=document.querySelector("#nextNodeQADownload");
 document.querySelector("#nextNodeQAClose").onclick=closeModal;
 try{
  const data=await apiRequest("/v1/qa/nodes/"+encodeURIComponent(selected)+"/evidence");
  if(!preview?.isConnected||!exportButton?.isConnected||nextNodeUI.selected!==selected)return;
  const report=JSON.stringify(data,null,2);
  if(report.length>24000)throw Error("Report exceeds safe display size");
  preview.textContent=report;
  status.textContent="Review before export. Local OS service-manager status appears only when successfully observed; drivers, remote services and physical runtime diagnostics are not collected.";
  exportButton.disabled=false;
  exportButton.onclick=()=>{
   if(!preview.isConnected||preview.textContent!==report||nextNodeUI.selected!==selected)return;
   const payload=new Blob([report],{type:"application/json"});
   if(payload.size>25000){status.textContent="Node QA report exceeds the export limit.";return}
   const url=URL.createObjectURL(payload);
   try{
    const link=document.createElement("a");
    link.href=url;link.download="onepane-node-qa-evidence.json";
    link.style.display="none";
    document.body.appendChild(link);link.click();link.remove();
   }finally{URL.revokeObjectURL(url)}
   status.textContent="Reviewed Node evidence saved locally. Inspect before sharing.";
  };
 }catch(_){
  if(!preview?.isConnected)return;
  preview.textContent="";
  status.textContent="Node QA evidence unavailable. Check Admin permissions and the local diagnostic database.";
 }
}

async function nextNodeRecommendations(){
 var id=nextNodeUI.selected;
 try{
  var r=await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/models/recommendations",{method:"POST",body:JSON.stringify({use_case:"general",context_tokens:8192,limit:10,minimum_fit:"",prefer_gpu:true})});
  var rows=nextNodeArray(r.recommendations);
  openModal("Remote model recommendations",'<div class="widget-body"><p>Hardware-qualified recommendations for '+nextNodeEsc(id)+'. Installations run on the remote node.</p>'+
   (rows.length?rows.map(function(x){return '<div class="list-row"><strong>'+nextNodeEsc(x.model_ref||(x.model&&x.model.model_ref)||"Model")+'</strong><div class="list-meta">'+nextNodeEsc(x.fit||x.reason||"")+'</div></div>';}).join(""):'<div class="empty-state">No recommendations returned.</div>')+'</div>');
 }catch(e){notice("Remote recommendations: "+e.message,"bad");}
}
function nextNodeInstallDialog(){
 var id=nextNodeUI.selected;
 openModal("Install model on "+id,'<form class="qa-form" id="nextNodeInstallForm"><p class="page-subtitle">Only digest-verified trusted catalogue models may be installed. The remote node must explicitly permit model management.</p>'+
 '<label>Catalogue model reference<input name="model_ref" required placeholder="organisation/model"></label>'+
 '<label>Quantization<input name="quantization" required value="Q4_K_M"></label>'+
 '<label>Use case<select name="use_case"><option value="general">General</option><option value="embedding">Embedding</option><option value="coding">Coding</option></select></label>'+
 '<label>Requested context (tokens)<input name="context_tokens" type="number" min="512" max="131072" value="8192"></label>'+
 '<label>Compute placement<select name="placement_preference"><option value="">Auto</option><option value="cpu_only">CPU only</option><option value="single_device">GPU</option><option value="cpu_offload">Hybrid</option></select></label>'+
 '<button class="btn primary" type="submit">Queue remote installation</button><div class="error" id="nextNodeInstallError"></div></form>');
 $("#nextNodeInstallForm").onsubmit=async function(e){
  e.preventDefault();
  var f=e.currentTarget,p=Object.fromEntries(new FormData(f));
  p.context_tokens=Number(p.context_tokens);p.role_name="local-managed";p.prefer_gpu=p.placement_preference!=="cpu_only";
  f.querySelector('button[type="submit"]').disabled=true;
  try{
   var job=await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/models/install",{method:"POST",body:JSON.stringify(p)});
   nextNodeAddJob(id,job);closeModal();notice("Remote model install queued.");nextNodeUI.tab="activity";nextNodeDraw();
  }catch(ex){$("#nextNodeInstallError").textContent=ex.message;f.querySelector('button[type="submit"]').disabled=false;}
 };
}
async function nextNodeInspectJob(jobID){
 try{
  var id=nextNodeUI.selected,job=await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-install-jobs/"+encodeURIComponent(jobID));
  openModal("Remote installation status",'<div class="widget-body"><h3>'+nextNodeEsc(job.model_ref||jobID)+'</h3><p>'+nextNodeEsc(job.status||"Unknown")+'</p><p>'+nextNodeEsc(job.failure_reason||job.current_artifact||"")+'</p><p>'+nextNodeEsc(job.progress_pct||0)+'%</p><button class="btn" id="nextNodeReloadJob">Refresh status</button></div>');
  $("#nextNodeReloadJob").onclick=function(){nextNodeInspectJob(jobID);};
 }catch(e){notice(e.message,"bad");}
}
async function nextNodeShowSpec(dep){
 try{var d=await apiRequest("/v1/nodes/"+encodeURIComponent(nextNodeUI.selected)+"/models/"+encodeURIComponent(dep)+"/spec-sheet");
  openModal("Remote model Spec Sheet",'<pre class="json-preview">'+nextNodeEsc(JSON.stringify(d,null,2))+'</pre>');
 }catch(e){notice(e.message,"bad");}
}
async function nextNodeAgentCheck(dep){
 var n=nextNodeUI.manifest&&nextNodeUI.manifest.models&&nextNodeUI.manifest.models.find(function(x){return x.deployment_id===dep;});
 if(n&&nextNodeArray(n.capabilities).some(function(c){return /embedding/i.test(c);})){notice("Embedding models use vector qualification; chat Agent Check is not applicable.","bad");return;}
 var id=nextNodeUI.selected,session="";
 try{
  notice("Starting remote Agent Check…");
  var s=await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/models/"+encodeURIComponent(dep)+"/testbed/sessions",{method:"POST",body:JSON.stringify({notes:"OnePane remote manual Agent Check"})});
  session=s.id;
  var probes=[{prompt:"Reply with exactly: ONEPANE_OK",max_tokens:32},{prompt:"Return JSON with status ok and number 7.",max_tokens:96}];
  var success=0;
  for(var i=0;i<probes.length;i++){try{await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-testbed/"+encodeURIComponent(session)+"/turns",{method:"POST",body:JSON.stringify(probes[i])});success++;}catch(ex){if(!success)throw ex;break;}}
  if(!success)throw Error("No successful inference probes; testbed not qualified");
  await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-testbed/"+encodeURIComponent(session)+"/complete",{method:"POST",body:"{}"});
  notice("Remote Agent Check completed; idle model unloaded on target node.");
  nextNodeShowSpec(dep);
 }catch(e){
  let cleanup="";
  if(session){
   try{await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-testbed/"+encodeURIComponent(session)+"/abort",{method:"POST",body:JSON.stringify({reason:"Remote Agent Check failed: "+e.message})});}
   catch(err){cleanup=" · Cleanup: "+err.message}
  }
  notice("Remote Agent Check failed: "+e.message+(session?" · Session "+session:"")+cleanup,"bad");
 }
}
async function nextNodeToggleGrant(){
 var id=nextNodeUI.selected,enabled=!(nextNodeUI.grant&&nextNodeUI.grant.enabled);
 try{nextNodeUI.grant=await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-management",{method:"POST",body:JSON.stringify({enabled:enabled})});nextNodeDraw();notice("Remote management "+(enabled?"enabled.":"disabled."));}
 catch(e){notice(e.message,"bad");}
}
async function nextNodeSavePolicy(){
 var id=nextNodeUI.selected,p=nextNodeUI.policy||{};
 var payload={enabled:$("#nextNodeEnabled").checked,idle_only:$("#nextNodeIdle").checked,
 allow_model_downloads:$("#nextNodeDownloads").checked,runtime_installation:$("#nextNodeRuntimePolicy").value,
 project_scope:$("#nextNodeProjectScope").value,availability:p.availability||{},
 limits:p.limits||{},allowed_projects:p.allowed_projects||[]};
 try{nextNodeUI.policy=await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/compute-policy",{method:"POST",body:JSON.stringify(payload)});nextNodeDraw();notice("Node compute policy saved.");}
 catch(e){notice(e.message,"bad");}
}
async function nextNodeRevoke(){
 var id=nextNodeUI.selected;
 if(!confirm("Revoke trust for "+id+"? New remote inference and model management will be blocked; model files are preserved."))return;
 try{await apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/revoke",{method:"POST",body:"{}"});nextNodeUI.selected="";await renderNodes();notice("Node pairing revoked.");}
 catch(e){notice(e.message,"bad");}
}

// Live local node telemetry: refresh the active Overview without disturbing
// Compute / Policy forms, preserving user input during edits.
setInterval(async function(){
 if(!a31RouteIs("nodes")||nextNodeUI.tab!=="overview")return;
 const selected=nextNodeUI.selected,node=nextNodeUI.nodes.find(n=>nextNodeID(n)===selected);
 if(!node?.local||!document.querySelector("#nextNodeRoot"))return;
 try{
  const fresh=await apiRequest("/v1/system/metrics?workspace_id="+encodeURIComponent(onepaneWorkspace||""));
  if(nextNodeUI.selected!==selected||nextNodeUI.tab!=="overview"||!a31RouteIs("nodes"))return;
  nextNodeUI.localMetrics=fresh;
  nextNodeUI.manifest=nextNodeLocalManifest(nextNodeUI.localHardware,fresh,nextNodeUI.localModels,nextNodeUI.localModelsAvailable);
  nextNodeDraw();
 }catch{}
},5000);
