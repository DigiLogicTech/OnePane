/* RC11 Project Orchestrator Task DAG operator console.
 * This is a human approval UI, NOT LLM-generated plans or speculative work.
 * A submitted graph may enqueue runnable Task admission immediately.
 */
const a61TaskGraphDrafts=new Map();
function a61Safe(value){return escapeHtml(String(value??""))}
function a61GraphDraft(projectId,workspaces){
 let draft=a61TaskGraphDrafts.get(projectId);
 if(!draft){
  draft={name:"Development workstream",idempotency_key:"review-"+Date.now().toString(36),
   nodes:[{project_workspace_id:workspaces[0]?.id||"",objective:"",depends:"",priority:0}],approved:false};
  a61TaskGraphDrafts.set(projectId,draft);
 }
 return draft;
}
function a61ReadGraphForm(form,draft){
 if(!form)return;
 const data=new FormData(form);
 draft.name=String(data.get("graph_name")||"");
 draft.idempotency_key=String(data.get("idempotency_key")||"");
 draft.nodes=draft.nodes.map((n,i)=>({
  project_workspace_id:String(data.get("workspace_"+i)||""),
  objective:String(data.get("objective_"+i)||""),
  depends:String(data.get("depends_"+i)||""),
  priority:Number(data.get("priority_"+i)||0)
 }));
 draft.approved=!!form.querySelector('[name="approved"]:checked');
}
function a61NodesFromDraft(draft){
 const known=new Set(draft.nodes.map((_,i)=>"step"+(i+1)));
 return draft.nodes.map((node,i)=>{
  const key="step"+(i+1);
  const dependencies=node.depends.split(",").map(x=>x.trim()).filter(Boolean);
  if(!node.project_workspace_id||!node.objective.trim())throw Error(key+": choose a registered Workspace and enter an objective.");
  if(node.objective.length>4096)throw Error(key+": objective exceeds 4096 characters.");
  if(dependencies.length>8||new Set(dependencies).size!==dependencies.length||
    dependencies.some(d=>!known.has(d)||d===key))
   throw Error(key+": dependencies must be up to eight distinct, other step keys.");
  if(!Number.isInteger(node.priority)||node.priority<0||node.priority>100)
   throw Error(key+": priority must be between 0 and 100.");
  return {key,project_workspace_id:node.project_workspace_id,objective:node.objective.trim(),
   depends_on:dependencies,priority:node.priority};
 });
}
function a61GraphBuilderMarkup(draft,workspaces){
 const options=workspaces.filter(w=>w.status==="active").map(w=>
  '<option value="'+a61Safe(w.id)+'">'+a61Safe(w.name||"Workspace")+'</option>');
 const rows=draft.nodes.map((n,i)=>'<fieldset class="a61-graph-step"><legend>Step '+(i+1)+
  ' <code>step'+(i+1)+'</code></legend><div class="a61-graph-fields">'+
  '<label>Workspace<select required name="workspace_'+i+'">'+
   workspaces.filter(w=>w.status==="active").map(w=>
    '<option value="'+a61Safe(w.id)+'"'+(w.id===n.project_workspace_id?' selected':'')+
    '>'+a61Safe(w.name||"Workspace")+'</option>').join("")+
   '</select></label>'+
  '<label>Priority<input type="number" min="0" max="100" name="priority_'+i+
   '" value="'+a61Safe(n.priority)+'"></label>'+
  '<label class="a61-objective">Objective<textarea required rows="2" maxlength="4096" name="objective_'+i+
   '" placeholder="Describe one independently verifiable task">'+a61Safe(n.objective)+'</textarea></label>'+
  '<label class="a61-depends">Hard prerequisites (step keys, comma-separated)<input name="depends_'+i+
   '" value="'+a61Safe(n.depends)+'" placeholder="'+(i?'step1':'No predecessors')+
   '"><small>Only complete, unarchived prerequisites can unblock this step.</small></label>'+
  '</div></fieldset>').join("");
 return '<form class="a61-graph-form" data-a61-plan>'+
  '<div class="a61-graph-fields"><label>Graph name<input name="graph_name" required maxlength="120" value="'+
   a61Safe(draft.name)+'"></label>'+
  '<label>Idempotency key<input name="idempotency_key" required maxlength="120" value="'+
   a61Safe(draft.idempotency_key)+'"><small>Retrying the same key cannot create duplicate Tasks.</small></label></div>'+
  rows+'<div class="toolbar a61-graph-actions">'+
   '<button type="button" class="btn" data-a61-add '+(draft.nodes.length>=32?"disabled":"")+
   '>Add step</button><button type="button" class="btn" data-a61-remove '+
   (draft.nodes.length<=1?"disabled":"")+
   '>Remove last step</button></div>'+
  '<p class="list-meta">Submitting records a governed Task graph and can enqueue eligible Tasks immediately. It does not grant cross-Workspace files, secrets or networking. Model and toolchain availability must be checked independently.</p>'+
  '<label class="inline-check"><input type="checkbox" name="approved" '+
   (draft.approved?"checked":"")+
   '> I reviewed the Workspace ownership and all Task dependencies and approve scheduling this graph.</label>'+
  '<div class="toolbar a61-graph-actions"><button type="submit" class="btn primary" '+
   (!workspaces.length?"disabled":"")+
   '>Approve and create Task graph</button></div>'+
  '</form>';
}
function a61GraphCard(graph){
 const progress=graph.progress||{},nodes=Array.isArray(graph.nodes)?graph.nodes:[];
 const count=(k)=>Number(progress[k]||0);
 const summary=[count("complete")+" complete",count("admissible")+" admission eligible",
  count("waiting")+" waiting",count("needs_attention")+" needs attention"].join(" · ");
 const rows=nodes.map(n=>'<div class="a61-graph-node">'+
  '<div><strong>'+a61Safe(n.key)+'</strong> <span class="list-meta">'+
    a61Safe(n.project_workspace_id)+'</span></div>'+
  '<span class="pill">'+a61Safe(n.readiness||n.state||"unknown")+'</span>'+
  '<div class="list-meta">'+a61Safe(n.next_action||"Pending Task assessment")+'</div>'+
  (n.blocked_by?.length?'<div class="list-meta">Waiting on: '+
   a61Safe(n.blocked_by.join(", "))+'</div>':"")+
  (n.failed_or_intervened_on?.length?'<div class="list-meta">Review prerequisite: '+
   a61Safe(n.failed_or_intervened_on.join(", "))+'</div>':"")+
  '</div>').join("");
 return '<details class="a61-graph-card"><summary><strong>'+a61Safe(graph.name||"Task graph")+
  '</strong><span class="pill">'+a61Safe(graph.status||"unknown")+'</span>'+
  '<span class="list-meta">'+a61Safe(summary)+'</span></summary>'+
  '<div class="a61-graph-nodes">'+rows+'</div></details>';
}
async function a61MountProjectTaskGraphs(project,workspace,container){
 if(!container?.isConnected)return;
 const section=document.createElement("section");
 section.className="a61-task-graphs a44-environment-subsection";
 section.dataset.projectId=String(project.id);
 section.dataset.workspaceId=String(workspace.id);
 section.innerHTML='<h3>Project Task graphs</h3><p class="list-meta">Loading governed workstreams…</p>';
 container.append(section);
 const base="/v1/projects/"+encodeURIComponent(project.id);
 let workspaces,graphs;
 try{
  [workspaces,graphs]=await Promise.all([
   apiRequest(base+"/workspaces"),apiRequest(base+"/orchestrator/task-graphs?limit=20")
  ]);
  workspaces=Array.isArray(workspaces)?workspaces.filter(x=>x?.status==="active"):[];
  graphs=Array.isArray(graphs)?graphs:[];
 }catch(err){
  if(section.isConnected)section.innerHTML='<h3>Project Task graphs</h3>'+
   '<div class="error" role="alert">'+a61Safe(err.message)+'</div>';
  return;
 }
 if(!section.isConnected||section.dataset.workspaceId!==String(workspace.id))return;
 const draft=a61GraphDraft(project.id,workspaces);
 const intro='<h3>Project Task graphs</h3><p class="list-meta">Review independent Workspace workstreams, hard dependencies and recovery blockers. Work on other branches can continue when one Task is blocked. Recovery does not automatically retry a side-effecting Task.</p>';
 const renderBuilder=()=>{
  const editor=section.querySelector("[data-a61-builder]");
  if(!editor)return;
  editor.innerHTML=a61GraphBuilderMarkup(draft,workspaces);
  const form=editor.querySelector("[data-a61-plan]");
  const capture=()=>{a61ReadGraphForm(form,draft);draft.approved=false;};
  form.querySelector("[data-a61-add]")?.addEventListener("click",()=>{
   capture();
   if(draft.nodes.length>=32)return;
   draft.nodes.push({project_workspace_id:workspaces[0]?.id||"",
    objective:"",depends:"",priority:0});
   renderBuilder();
  });
  form.querySelector("[data-a61-remove]")?.addEventListener("click",()=>{
   capture();
   if(draft.nodes.length>1)draft.nodes.pop();
   renderBuilder();
  });
  form.addEventListener("input",event=>{
   if(event.target?.name==="approved")return;
   draft.approved=false;
   const box=form.querySelector('[name="approved"]');
   if(box)box.checked=false;
  });
  form.addEventListener("submit",async e=>{
   e.preventDefault();
   a61ReadGraphForm(form,draft);
   if(!draft.approved){notice("Review and approve the graph before scheduling.","bad");return}
   const submit=form.querySelector('[type="submit"]');
   submit.disabled=true;
   try{
    const nodes=a61NodesFromDraft(draft);
    const body={name:draft.name.trim(),idempotency_key:draft.idempotency_key.trim(),nodes};
    if(!body.name||!body.idempotency_key)throw Error("Graph name and idempotency key are required.");
    const saved=await apiRequest(base+"/orchestrator/task-graphs",{
     method:"POST",body:JSON.stringify(body)
    });
    // The graph is now durable. A list-refresh error must not imply rollback.
    notice("Operator-approved Task graph recorded; eligible Tasks can be admitted.");
    draft.idempotency_key="review-"+Date.now().toString(36);
    draft.approved=false;
    renderBuilder();
    try{
     const refreshed=await apiRequest(base+"/orchestrator/task-graphs?limit=20");
     graphs=Array.isArray(refreshed)?refreshed:[saved];
    }catch(_refreshError){graphs=[saved,...graphs.filter(g=>g.id!==saved.id)].slice(0,20)}
    renderInventory();
   }catch(err){submit.disabled=false;notice("Graph submission blocked: "+err.message,"bad")}
  });
 };
 const renderInventory=()=>{
  const inventory=section.querySelector("[data-a61-inventory]");
  if(!inventory)return;
  inventory.innerHTML=graphs.length?graphs.map(a61GraphCard).join(""):
   '<div class="empty-state compact">No operator-approved Task graphs in this Project.</div>';
 };
 section.innerHTML=intro+'<div class="toolbar a61-graph-actions">'+
  '<button type="button" class="btn" data-a61-refresh>Refresh statuses</button>'+
  '</div><div data-a61-inventory></div>'+
  '<details class="a61-create-graph"><summary>Create an operator-approved Task graph</summary>'+
  '<div data-a61-builder></div></details>';
 renderInventory();renderBuilder();
 section.querySelector("[data-a61-refresh]")?.addEventListener("click",async e=>{
  const button=e.currentTarget;button.disabled=true;
  try{
   const data=await apiRequest(base+"/orchestrator/task-graphs?limit=20");
   if(section.isConnected){graphs=Array.isArray(data)?data:[];renderInventory();}
  }catch(err){notice("Task graph refresh failed: "+err.message,"bad")}
  finally{button.disabled=false}
 });
}
