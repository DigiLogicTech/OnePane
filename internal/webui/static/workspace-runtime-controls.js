/* Canonical Workspace runtime management.
 * No direct host commands: all writes go through the existing Project runtime
 * operation coordinator, capability leases, observation & verification.
 */
async function a48MountWorkspaceRuntime(project,workspace,container){
 const section=document.createElement("section");
 section.className="a44-environment-subsection a48-runtime";
 section.dataset.workspaceId=String(workspace.id);
 section.innerHTML='<h3>Workspace sandbox</h3><p class="list-meta">Resolving canonical execution environment…</p>';
 container.prepend(section);
 const prefix="/v1/projects/"+encodeURIComponent(project.id);
 let views=[];
 try{const result=await apiRequest(prefix+"/workspaces");views=Array.isArray(result)?result:[]}
 catch(e){section.innerHTML='<h3>Workspace sandbox</h3><div class="error" role="alert">'+escapeHtml(e.message)+'</div>';return}
 const canonical=views.find(w=>a45BackendLegacyID(w)===String(workspace.id));
 if(!section.isConnected)return;
 if(!canonical){
  section.innerHTML='<h3>Workspace sandbox</h3><p class="list-meta">Register this Workspace in Workspace connections before provisioning an isolated environment.</p>';
  return;
 }
 const runtimeURI=prefix+"/workspaces/"+encodeURIComponent(canonical.id)+"/runtime";
 let runtime=null,apps=[],err="";
 try{runtime=await apiRequest(runtimeURI)}
 catch(e){if(!/404|not found|no rows/i.test(String(e.message)))err=e.message}
 if(runtime){
  try{const list=await apiRequest("/v1/project-runtimes/"+encodeURIComponent(runtime.id)+"/applications");apps=Array.isArray(list)?list:[]}
  catch(e){err="Application inventory unavailable: "+e.message}
 }
 if(!section.isConnected||section.dataset.workspaceId!==String(workspace.id))return;
 const verified=runtime?.status==="running";
 const status=runtime?.status||"not provisioned",desired=runtime?.desired_state||"stopped";
 const tools=apps.map(a=>'<div class="a48-app-row"><strong>'+escapeHtml(a.name)+'</strong><span class="pill">'+escapeHtml(a.status||"declared")+'</span><span class="list-meta">'+escapeHtml(a.source_ref||"")+'</span></div>').join("");
 const buttons=runtime?
  '<button type="button" class="btn" id="a48Start" '+(desired==="running"?"disabled":"")+'>Start</button> <button type="button" class="btn" id="a48Stop" '+(desired==="stopped"?"disabled":"")+'>Stop</button>':
  '<button type="button" class="btn primary" id="a48Create">Create isolated sandbox</button>';
 section.innerHTML='<div class="card-header"><div><h3>Workspace sandbox</h3><p class="list-meta">Dedicated execution identity. Actual execution requires a compatible rootless Podman/Docker host; remote Node placement is not yet verified.</p></div>'+
 '<span class="pill '+(verified?"good":"")+'">'+escapeHtml(status)+'</span></div>'+
 '<div class="a48-runtime-grid">'+
 a44DevelopmentSection("Requested state",desired,"Operations may remain queued until capacity is available")+
 a44DevelopmentSection("Observed state",status,verified?"Verified running status from backend":"Not confirmed running")+
 a44DevelopmentSection("Runtime scope",canonical.name,"Dedicated to this Workspace, not the legacy shared Project runtime")+
 a44DevelopmentSection("Installed applications",String(apps.length),"Only observed application states are reported")+'</div>'+
 '<div class="toolbar a48-runtime-actions">'+buttons+'<button type="button" class="btn" id="a48Refresh">Refresh</button></div>'+
 (runtime?'<form id="a48Install" class="a48-install"><h4>Install a toolchain / OCI application</h4>'+
 '<p class="list-meta">Supply a reviewed image pinned to its sha256 digest. Installs are governed and executed only on a compatible Sandbox Node, never on the Windows desktop host.</p>'+
 '<label>Application name<input name="name" required maxlength="120" placeholder="Godot headless"></label>'+
 '<label>OCI image digest<input name="source_ref" required placeholder="registry.example/engine@sha256:…" pattern=".+@sha256:[0-9a-fA-F]{64}"></label>'+
 '<button type="submit" class="btn" '+(runtime.status==="running"?"":"disabled")+' >Declare pinned tool</button></form>':'')+
 (tools?'<div class="a48-app-list"><h4>Workspace tools</h4>'+tools+'</div>':'')+
 (err?'<p class="error" role="alert">'+escapeHtml(err)+'</p>':'');
 const refresh=()=>a48MountWorkspaceRuntime(project,workspace,container).then(()=>{
  if(section.isConnected)section.remove();
 });
 const mutate=async(uri,body)=>{
  try{await apiRequest(uri,{method:"POST",body:JSON.stringify(body)});notice("Workspace runtime change requested.");await refresh()}
  catch(e){notice("Workspace runtime: "+e.message,"bad")}
 };
 section.querySelector("#a48Create")?.addEventListener("click",()=>mutate(runtimeURI,{
  desired_state:"stopped",isolation_mode:"sandboxed_container"
 }));
 section.querySelector("#a48Start")?.addEventListener("click",()=>mutate("/v1/project-runtimes/"+encodeURIComponent(runtime.id)+"/desired-state",{expected_revision:runtime.revision,desired_state:"running"}));
 section.querySelector("#a48Stop")?.addEventListener("click",()=>mutate("/v1/project-runtimes/"+encodeURIComponent(runtime.id)+"/desired-state",{expected_revision:runtime.revision,desired_state:"stopped"}));
 section.querySelector("#a48Refresh")?.addEventListener("click",refresh);
 section.querySelector("#a48Install")?.addEventListener("submit",async e=>{
  e.preventDefault();
  const form=e.currentTarget;const btn=form.querySelector('button[type="submit"]');btn.disabled=true;
  const data=Object.fromEntries(new FormData(form));
  if(!/^.+@sha256:[0-9a-fA-F]{64}$/.test(data.source_ref)){notice("A pinned OCI image SHA-256 digest is required.","bad");btn.disabled=false;return}
  try{
   await apiRequest("/v1/project-runtimes/"+encodeURIComponent(runtime.id)+"/applications",{
    method:"POST",body:JSON.stringify({name:data.name,source_kind:"oci_image",source_ref:data.source_ref,desired_state:"installed",install_spec:{},runtime_spec:{},environment_bindings:{}})
   });
   notice("Pinned toolchain registered. Runtime reconciliation will verify installation.");
   await refresh();
  }catch(e){btn.disabled=false;notice("Tool registration failed: "+e.message,"bad")}
 });
}
