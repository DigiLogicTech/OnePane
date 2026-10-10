/* Workspace artifact channels: explicit, revocable, version-pinned transfers.
 * Workspaces remain sandbox-isolated. This is not a shared directory or
 * permission to run arbitrary tools or receive ungranted Library bytes.
 */
async function a45MountCollaboration(project,workspace,container){
 if(!container.isConnected)return;
 const section=document.createElement("section");
 section.className="a45-collaboration a44-environment-subsection";
 section.dataset.workspaceId=String(workspace.id);
 section.innerHTML='<h3>Workspace connections</h3><p class="list-meta">Loading approved Project artifact channels…</p>';
 container.append(section);
 await a45RenderCollaboration(project,workspace,section);
}
function a45BackendLegacyID(view){
 let state=view?.state||{};
 if(typeof state==="string"){try{state=JSON.parse(state)}catch{state={}}}
 return String(state?.legacy_workspace_id||"");
}
function a45WorkspaceName(view){return String(view?.name||view?.id||"Workspace")}
function a45HTML(v){return escapeHtml(String(v??""))}
async function a45RenderCollaboration(project,workspace,root){
 if(!root?.isConnected)return;
 const endpoint=`/v1/projects/${encodeURIComponent(project.id)}`;
 let canonical=[],links=[];
 try{
  const all=await Promise.all([apiRequest(endpoint+"/workspaces"),apiRequest(endpoint+"/workspace-links")]);
  canonical=Array.isArray(all[0])?all[0]:[];
  links=Array.isArray(all[1])?all[1]:[];
 }catch(err){
  if(root.isConnected)root.innerHTML=`<h3>Workspace connections</h3><div class="error" role="alert">${a45HTML(err.message)}</div>`;
  return;
 }
 if(!root.isConnected||root.dataset.workspaceId!==String(workspace.id))return;
 const current=canonical.find(v=>a45BackendLegacyID(v)===String(workspace.id));
 const persisted=Array.isArray(qa4ProjectUI(project)?.workspaces)&&qa4ProjectUI(project).workspaces.some(w=>w.id===workspace.id);
 const heading=`<h3>Workspace connections</h3>
 <p class="list-meta">Send immutable Project Library versions to another Workspace. No live directory mounts, code execution privileges, credentials or unrestricted browsing are shared.</p>`;
 if(!current){
  root.innerHTML=heading+`<div class="a44-environment-note">This dashboard Workspace is not yet linked to a canonical backend environment.
    ${persisted?"Register it to enable controlled sharing.":"Save the Workspace layout before registering a persistent connection."}
    ${persisted?'<button type="button" class="btn primary" id="a45RegisterWorkspace">Register this Workspace</button>':""}
   </div>`;
  root.querySelector("#a45RegisterWorkspace")?.addEventListener("click",async e=>{
   e.currentTarget.disabled=true;
   try{
    await apiRequest(endpoint+"/workspaces",{method:"POST",body:JSON.stringify({name:workspace.name||"Workspace",legacy_workspace_id:workspace.id})});
    await a45RenderCollaboration(project,workspace,root);
   }catch(err){if(root.isConnected)root.querySelector(".a44-environment-note").insertAdjacentHTML("beforeend",`<div class="error">${a45HTML(err.message)}</div>`)}
  });
  return;
 }
 const nameByID=new Map(canonical.map(v=>[String(v.id),a45WorkspaceName(v)]));
 const currentLinks=links.filter(l=>l.source_workspace_id===current.id||l.target_workspace_id===current.id);
 const targets=canonical.filter(v=>v.id!==current.id&&v.status==="active");
 const options=targets.map(w=>`<option value="${a45HTML(w.id)}">${a45HTML(w.name)}</option>`).join("");
 const hasTargets=targets.length>0;
 const form=`<form id="a45ConnectionForm" class="a45-connection-form">
   <label>Share from <input disabled value="${a45HTML(workspace.name||current.name)}"></label>
   <label>To Workspace <select name="target_workspace_id" required ${hasTargets?"":"disabled"}>${options||'<option>No other canonical Workspaces yet</option>'}</select></label>
   <label>Connection name<input name="name" maxlength="120" required value="${a45HTML(workspace.name||"Workspace")} artifacts"></label>
   <label>Grant duration<select name="duration"><option value="none">Until revoked</option><option value="day">24 hours</option><option value="week">7 days</option><option value="month">30 days</option></select></label>
   <button type="submit" class="btn primary" ${hasTargets?"":"disabled"}>Enable connection</button>
 </form>`;
 const rows=currentLinks.length?currentLinks.map(l=>{
  const expired=Boolean(l.expired);
  const usable=Boolean(l.enabled)&&!expired;
  const expires=Number(l.expires_at_ms);
  const expiryText=l.expires_at_ms&&Number.isFinite(expires)&&expires>0
   ?`Expires ${new Date(expires).toLocaleString()}`:"No expiry";
  const from=nameByID.get(l.source_workspace_id)||l.source_workspace_id;
  const to=nameByID.get(l.target_workspace_id)||l.target_workspace_id;
  const sending=l.source_workspace_id===current.id;
  return `<article class="a45-link-card" data-a45-link-id="${a45HTML(l.id)}">
   <div class="a45-link-header"><div><strong>${a45HTML(l.name)}</strong>
    <div class="list-meta">${a45HTML(from)} → ${a45HTML(to)} · ${sending?"Outgoing":"Incoming"} · Artifact versions</div>
    <div class="list-meta">${a45HTML(expiryText)}</div></div>
    <span class="pill ${usable?"good":""}">${expired?"Expired":l.enabled?"Enabled":"Disabled"}</span></div>
   <div class="toolbar a45-link-actions">
    <button type="button" class="btn" data-a45-toggle="${a45HTML(l.id)}" data-a45-revision="${Number(l.revision)}" data-a45-enabled="${l.enabled?"1":"0"}">${l.enabled?"Disable":"Enable"} link</button>
    ${l.expires_at_ms?`<button type="button" class="btn" data-a45-renew="${a45HTML(l.id)}" data-a45-revision="${Number(l.revision)}">Renew 7 days</button>
    <button type="button" class="btn" data-a45-clear-expiry="${a45HTML(l.id)}" data-a45-revision="${Number(l.revision)}">Remove expiry</button>`:""}
    ${sending&&usable?`<button type="button" class="btn" data-a45-publish="${a45HTML(l.id)}">Publish Library asset</button>`:""}
   </div>
   <div class="a45-publications" data-a45-pubs="${a45HTML(l.id)}">
    <span class="list-meta">${usable?"Loading published versions…":expired?"Expired links cannot expose publications.":"Disabled links cannot expose publications."}</span>
   </div>
  </article>`;
 }).join(""):'<div class="empty-state compact">No connections involving this Workspace. Other sandboxes remain isolated.</div>';
 root.innerHTML=heading+form+`<div class="a45-links">${rows}</div>`;
 const refresh=()=>a45RenderCollaboration(project,workspace,root);
 root.querySelector("#a45ConnectionForm")?.addEventListener("submit",async e=>{
  e.preventDefault();const button=e.currentTarget.querySelector('[type="submit"]');button.disabled=true;
  const data=Object.fromEntries(new FormData(e.currentTarget));
  const durationMs={day:86400000,week:7*86400000,month:30*86400000}[data.duration];
  try{
   await apiRequest(endpoint+"/workspace-links",{method:"POST",body:JSON.stringify({
    source_workspace_id:current.id,target_workspace_id:data.target_workspace_id,
    name:data.name,enable:true,
    ...(durationMs?{expires_at_ms:Date.now()+durationMs}:{})
   })});await refresh();
  }catch(err){button.disabled=false;notice("Connection failed: "+err.message,"bad")}
 });
 root.querySelectorAll("[data-a45-toggle]").forEach(b=>b.addEventListener("click",async()=>{
  b.disabled=true;
  try{await apiRequest(`/v1/workspace-links/${encodeURIComponent(b.dataset.a45Toggle)}`,{
   method:"PATCH",body:JSON.stringify({expected_revision:Number(b.dataset.a45Revision),enabled:b.dataset.a45Enabled!=="1"})
  });await refresh()}catch(err){b.disabled=false;notice("Link update failed: "+err.message,"bad")}
 }));
 root.querySelectorAll("[data-a45-renew]").forEach(b=>b.addEventListener("click",async()=>{
  b.disabled=true;
  try{
   await apiRequest(`/v1/workspace-links/${encodeURIComponent(b.dataset.a45Renew)}`,{
    method:"PATCH",body:JSON.stringify({
     expected_revision:Number(b.dataset.a45Revision),enabled:true,
     expires_at_ms:Date.now()+7*86400000
    })
   });
   notice("Workspace link renewed for seven days.");await refresh();
  }catch(err){b.disabled=false;notice("Link renewal failed: "+err.message,"bad")}
 }));
 root.querySelectorAll("[data-a45-clear-expiry]").forEach(b=>b.addEventListener("click",async()=>{
  b.disabled=true;
  try{
   await apiRequest(`/v1/workspace-links/${encodeURIComponent(b.dataset.a45ClearExpiry)}`,{
    method:"PATCH",body:JSON.stringify({
     expected_revision:Number(b.dataset.a45Revision),enabled:true,clear_expiry:true
    })
   });
   notice("Workspace link expiry removed.");await refresh();
  }catch(err){b.disabled=false;notice("Expiry update failed: "+err.message,"bad")}
 }));
 root.querySelectorAll("[data-a45-publish]").forEach(b=>b.addEventListener("click",async()=>{
  const id=b.dataset.a45Publish;
  let assets=[];
  try{
   const result=await apiRequest(endpoint+"/workspaces/"+encodeURIComponent(current.id)+"/library");
   assets=Array.isArray(result)?result:[];
  }catch(err){notice("Project Library unavailable: "+err.message,"bad");return}
  if(!assets.length){notice("Upload an asset into this Workspace Library before publishing it.","bad");return}
  const suggestions=assets.map(a=>`<option value="${a45HTML(a.id)}">${a45HTML(a.name)} · v${Number(a.current_version)}</option>`).join("");
  openModal("Publish to Workspace",`<form id="a45PublishForm" class="qa-form">
   <p class="list-meta">Publish one immutable Project Library version. The source Workspace must have an active read-and-derivative grant. The target receives only this exact published version.</p>
   <label>Library asset<select name="asset_id" required>${suggestions}</select></label>
   <label>Exact version<input name="version" type="number" min="1" value="${Number(assets[0].current_version)}" required></label>
   <div class="error" id="a45PublishError"></div><button class="btn primary" type="submit">Publish pinned version</button>
  </form>`);
  const form=document.querySelector("#a45PublishForm");
  form.querySelector('[name="asset_id"]').onchange=e=>{
   const item=assets.find(a=>a.id===e.target.value);
   if(item)form.querySelector('[name="version"]').value=String(item.current_version);
  };
  form.onsubmit=async e=>{
   e.preventDefault();const submit=form.querySelector('[type="submit"]');submit.disabled=true;
   const fields=Object.fromEntries(new FormData(form));
   try{
    await apiRequest(`/v1/workspace-links/${encodeURIComponent(id)}/publications`,{method:"POST",body:JSON.stringify({asset_id:fields.asset_id,version:Number(fields.version)})});
    closeModal();notice("Version published to connected Workspace.");await refresh();
   }catch(err){submit.disabled=false;form.querySelector("#a45PublishError").textContent=err.message}
  };
 }));
 const enabled=currentLinks.filter(l=>l.enabled&&!l.expired);
 await Promise.all(enabled.map(async link=>{
  const cell=root.querySelector(`[data-a45-pubs="${CSS.escape(link.id)}"]`);
  if(!cell)return;
  try{
   const pubs=await apiRequest(`/v1/workspace-links/${encodeURIComponent(link.id)}/publications`);
   if(!cell.isConnected)return;
   cell.innerHTML=Array.isArray(pubs)&&pubs.length?pubs.map(p=>`<div class="a45-publication"><strong>${a45HTML(p.asset_name)}</strong><span class="list-meta">v${Number(p.asset_version)} · ${a45HTML(p.content_hash)} · ${a45HTML(p.asset_id)}</span></div>`).join(""):'<span class="list-meta">No versions published.</span>';
  }catch(err){if(cell.isConnected)cell.textContent="Publication list unavailable: "+err.message}
 }));
}
