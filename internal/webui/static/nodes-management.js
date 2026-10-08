/* OnePane federated Nodes control plane — backed by node-scoped APIs.
 * A selected node is never inferred from the operator's browser location. */
var nextNodeUI={nodes:[],selected:"",tab:"overview",manifest:null,grant:null,policy:null,capError:"",jobs:[]};
function nextNodeEsc(v){return escapeHtml(String(v==null?"":v));}
function nextNodeID(n){return String(n.id||n.node_id||"");}
function nextNodeName(n){return n.name||n.hostname||n.display_name||nextNodeID(n)||"Node";}
function nextNodeState(n){return n.local?"local":(n.status||n.state||n.trust_state||"unknown");}
function nextNodeArray(v){return Array.isArray(v)?v:[];}
function nextNodeRows(){
 var rows=nextNodeUI.nodes,online=rows.filter(function(n){return n.local||n.trust_state==="paired";}).length;
 var gpu=nextNodeArray(nextNodeUI.manifest&&nextNodeUI.manifest.compute&&nextNodeUI.manifest.compute.pools).filter(function(p){return p.kind==="gpu";}).length;
 return '<div class="host-metrics-grid node-summary-grid">'+
  [['Nodes',rows.length],['Trusted / local',online],['Selected GPU pools',gpu],['Remote models',nextNodeArray(nextNodeUI.manifest&&nextNodeUI.manifest.models).length]]
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
 host.innerHTML='<section class="page">'+pageHeader("Nodes","Central management for local and paired Windows/Ubuntu compute nodes.",'<button class="btn primary" id="a31AddNode">Add Node</button>')+'<div id="a31Nodes"><div id="nextNodeRoot"><div class="widget-body">Loading registered nodes…</div></div></div></section>';
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
 var checks=await Promise.allSettled([
  apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/capabilities"),
  apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/model-management"),
  apiRequest("/v1/nodes/"+encodeURIComponent(id)+"/compute-policy")
 ]);
 if(checks[0].status==="fulfilled")nextNodeUI.manifest=checks[0].value;
 else nextNodeUI.capError=String(checks[0].reason&&checks[0].reason.message||"Capabilities unavailable");
 if(checks[1].status==="fulfilled")nextNodeUI.grant=checks[1].value;
 if(checks[2].status==="fulfilled")nextNodeUI.policy=checks[2].value;
}
function nextNodeDraw(){
 var root=$("#nextNodeRoot");if(!root)return;
 var node=nextNodeUI.nodes.find(function(n){return nextNodeID(n)===nextNodeUI.selected;});
 var top=nextNodeRows();
 var list='<div class="node-grid">'+nextNodeUI.nodes.map(function(n){
  var id=nextNodeID(n),selected=id===nextNodeUI.selected,st=nextNodeState(n);
  return '<article class="panel-card node-card'+(selected?' next-node-selected':'')+'" data-a31-node="'+nextNodeEsc(id)+'"><div class="card-header"><div><div class="card-title">'+nextNodeEsc(nextNodeName(n))+(n.local?' (Local)':'')+'</div><div class="list-meta">'+nextNodeEsc(id)+'</div></div><span class="pill">'+nextNodeEsc(st)+'</span></div><div class="widget-body"><div class="list-meta">'+nextNodeEsc(n.last_seen_at||"No heartbeat reported")+'</div><button class="btn'+(selected?' primary':'')+'" data-next-node="'+nextNodeEsc(id)+'">Manage node</button></div></article>';
 }).join("")+'</div>';
 var detail=node?nextNodeDetail(node):'<section class="panel-card"><div class="widget-body">No enrolled nodes. Select Add Node to begin a mutually confirmed pairing.</div></section>';
 root.innerHTML=top+list+detail;
 $$("[data-next-node]",root).forEach(function(b){b.onclick=async function(){nextNodeUI.selected=b.dataset.nextNode;nextNodeUI.tab="overview";await nextNodeLoadDetail(nextNodeUI.selected);nextNodeDraw();};});
 $$("[data-next-node-tab]",root).forEach(function(b){b.onclick=function(){nextNodeUI.tab=b.dataset.nextNodeTab;nextNodeDraw();};});
 $("#nextNodeRefresh")?.addEventListener("click",function(){nextNodeLoadDetail(nextNodeUI.selected).then(nextNodeDraw).catch(function(e){notice(e.message,"bad");});});
 $("#nextNodePairAgain")?.addEventListener("click",openPairNode);
 $("#nextNodeOpenModels")?.addEventListener("click",function(){openRoute("models");});
 $("#nextNodeRecommendations")?.addEventListener("click",nextNodeRecommendations);
 $("#nextNodeInstall")?.addEventListener("click",nextNodeInstallDialog);
 $("#nextNodePolicySave")?.addEventListener("click",nextNodeSavePolicy);
 $("#nextNodeGrant")?.addEventListener("click",nextNodeToggleGrant);
 $("#nextNodeRevoke")?.addEventListener("click",nextNodeRevoke);
 $$("[data-next-node-job]",root).forEach(function(b){b.onclick=function(){nextNodeInspectJob(b.dataset.nextNodeJob);};});
 $$("[data-next-node-spec]",root).forEach(function(b){b.onclick=function(){nextNodeShowSpec(b.dataset.nextNodeSpec);};});
 $$("[data-next-node-check]",root).forEach(function(b){b.onclick=function(){nextNodeAgentCheck(b.dataset.nextNodeCheck);};});
}
function nextNodeDetail(n){
 var tabs=[["overview","Overview"],["models","Models"],["compute","Compute"],["access","Access & Policy"],["activity","Activity"]];
 var nav='<div class="subtabs">'+tabs.map(function(t){return '<button class="subtab'+(nextNodeUI.tab===t[0]?' active':'')+'" data-next-node-tab="'+t[0]+'">'+t[1]+'</button>';}).join("")+'</div>';
 var header='<div class="card-header"><div><div class="card-title">'+nextNodeEsc(nextNodeName(n))+' · Management</div><div class="list-meta">'+nextNodeEsc(n.trust_state||"Local node")+' · '+nextNodeEsc(n.id)+'</div></div><button class="btn" id="nextNodeRefresh">Refresh</button></div>';
 var body=nextNodeUI.tab==="models"?nextNodeModels(n):nextNodeUI.tab==="compute"?nextNodeCompute(n):nextNodeUI.tab==="access"?nextNodeAccess(n):nextNodeUI.tab==="activity"?nextNodeActivity(n):nextNodeOverview(n);
 return '<section class="panel-card next-node-manager">'+header+nav+'<div class="widget-body">'+body+'</div></section>';
}
function nextNodeOverview(n){
 var manifest=nextNodeUI.manifest||{},compute=manifest.compute||{},pools=nextNodeArray(compute.pools);
 var rows=[["Name",nextNodeName(n)],["Node identity",nextNodeID(n)],["Trust",n.trust_state],["Endpoint",JSON.stringify(n.endpoint||{})],["Compute state",compute.status],["CPU usage",compute.cpu_usage_pct==null?"Unavailable":compute.cpu_usage_pct+"%"],["Available RAM",compute.memory_available_bytes?bytesQA(compute.memory_available_bytes):"Not reported"],["Available storage",compute.storage_available_bytes?bytesQA(compute.storage_available_bytes):"Not reported"],["Last seen",n.last_seen_at||"Not reported"]];
 return (nextNodeUI.capError?'<p class="error">'+nextNodeEsc(nextNodeUI.capError)+'</p>':'')+
  '<dl class="definition-grid">'+rows.map(function(r){return '<dt>'+nextNodeEsc(r[0])+'</dt><dd>'+nextNodeEsc(r[1]||"—")+'</dd>';}).join("")+'</dl>'+
  '<h3>Compute pools</h3>'+(pools.length?pools.map(function(p){return '<div class="list-row"><div class="list-main"><strong>'+nextNodeEsc(p.name||p.kind)+'</strong><div class="list-meta">'+nextNodeEsc(p.backend||p.kind)+' · '+nextNodeEsc(p.available_bytes?bytesQA(p.available_bytes)+" available":"Capacity not reported")+'</div></div></div>';}).join(""):'<p class="page-subtitle">No active compute pools advertised.</p>');
}
function nextNodeModels(n){
 var models=nextNodeArray(nextNodeUI.manifest&&nextNodeUI.manifest.models);
 var toolbar=n.local?'<button class="btn primary" id="nextNodeOpenModels">Manage local models</button>':'<button class="btn" id="nextNodeRecommendations">Recommendations</button> <button class="btn primary" id="nextNodeInstall">Install on this node</button>';
 return '<p class="page-subtitle">Model weights, runtimes and inference are hosted on the selected node. Local and remote model installations use the same trusted catalogue.</p><div class="toolbar">'+toolbar+'</div>'+
 (models.length?'<div class="table-shell"><table class="data-table"><thead><tr><th>Model</th><th>Runtime</th><th>Qualification</th><th>Actions</th></tr></thead><tbody>'+models.map(function(m){var id=m.deployment_id||"";return '<tr><td><strong>'+nextNodeEsc(m.model_ref)+'</strong><div class="list-meta">'+nextNodeEsc(m.quantization||"")+'</div></td><td>'+nextNodeEsc(m.runtime_name||"—")+'</td><td>'+nextNodeEsc(m.qualification||"Unknown")+'</td><td><button class="btn tiny" data-next-node-spec="'+nextNodeEsc(id)+'">Spec Sheet</button> <button class="btn tiny" data-next-node-check="'+nextNodeEsc(id)+'">Agent Check</button></td></tr>';}).join("")+'</tbody></table></div>':'<div class="empty-state compact">No remote deployments advertised by this node.</div>');
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
  notice("Remote Agent Check completed; review qualification.");
  nextNodeShowSpec(dep);
 }catch(e){notice("Remote Agent Check failed: "+e.message+(session?" · Session "+session:""),"bad");}
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
