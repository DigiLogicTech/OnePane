/* First functional Project Library slice: safe upload, immutable versions and
 * explicit Workspace grants. Content is kept in the configured artifact store.
 * No model obtains broad Library access from rendering this page.
 */
async function a46MountWorkspaceLibrary(project,workspace,container){
 if(!container.isConnected)return;
 const slot=document.createElement("section");
 slot.className="a46-library a44-environment-subsection";
 slot.dataset.workspaceId=String(workspace.id);
 slot.innerHTML="<h3>Project Library</h3><p class='list-meta'>Loading versioned assets…</p>";
 container.append(slot);
 await a46RenderLibrary(project,workspace,slot);
}
async function a46RenderLibrary(project,workspace,root){
 if(!root?.isConnected)return;
 const prefix=`/v1/projects/${encodeURIComponent(project.id)}`;
 let workspaces=[],assets=[];
 try{
  [workspaces,assets]=await Promise.all([apiRequest(prefix+"/workspaces"),apiRequest(prefix+"/library")]);
 }catch(e){if(root.isConnected)root.innerHTML=`<h3>Project Library</h3><div class="error" role="alert">${escapeHtml(e.message)}</div>`;return}
 if(!root.isConnected)return;
 workspaces=Array.isArray(workspaces)?workspaces:[];
 assets=Array.isArray(assets)?assets:[];
 const globalLibrary=root.dataset.globalLibrary==="true";
 const source=globalLibrary?{id:"",name:"Project Library"}:workspaces.find(w=>a45BackendLegacyID(w)===String(workspace.id));
 const labelOf=w=>escapeHtml(w.name||w.id||"Workspace");
 const targets=workspaces.filter(w=>w.status==="active");
 const options=targets.map(w=>`<option value="${escapeHtml(w.id)}" ${w.id===source?.id?"selected":""}>${labelOf(w)}</option>`).join("");
 const cards=assets.map(a=>`<article class="a46-library-asset" data-a46-asset="${escapeHtml(a.id)}">
  <div class="a45-link-header"><div><strong>${escapeHtml(a.name)}</strong>
    <div class="list-meta">${escapeHtml(a.asset_type)} · v${Number(a.current_version)} · ${escapeHtml(a.id)}</div></div>
    <span class="pill">Versioned</span></div>
  <div class="toolbar a45-link-actions">
   <button type="button" class="btn" data-a46-versions="${escapeHtml(a.id)}">Versions</button>
   <button type="button" class="btn" data-a46-grant="${escapeHtml(a.id)}">Grant Workspace access</button>
   ${source?.id?`<a class="btn" href="${prefix}/library/${encodeURIComponent(a.id)}/versions/${Number(a.current_version)}/content?workspace_id=${encodeURIComponent(source.id)}" title="Requires an approved Workspace read grant">Download if granted</a>`:""}
  </div><div class="a46-versions" data-a46-version-list="${escapeHtml(a.id)}" hidden></div>
 </article>`).join("");
 root.innerHTML=`<h3>Project Library</h3>
 <p class="list-meta">Shared Project storage, versioned and permissioned per Workspace. Uploading an asset to this Workspace does not give other sandboxes permission to read it.</p>
 <form class="a46-upload" id="a46UploadForm">
  <label>New asset<input type="file" name="file" required></label>
  <button class="btn primary" type="submit" ${source?"":"disabled"}>${globalLibrary?"Upload to Project Library":"Upload into "+escapeHtml(workspace.name||"Workspace")+" Library"}</button>
 </form>
 ${!source?'<p class="list-meta">Register this Workspace under Workspace connections to enable uploads.</p>':""}
 <div class="a46-library-items">${cards||'<div class="empty-state compact">No Project Library assets. Upload a document, source artifact or asset to begin.</div>'}</div>`;
 const form=root.querySelector("#a46UploadForm");
 form?.addEventListener("submit",async e=>{
  e.preventDefault();
  if(!source)return;
  const file=form.querySelector('[name="file"]').files?.[0];
  if(!file)return;
  if(file.size>32*1024*1024){notice("This Library uploader currently supports up to 32 MiB per asset.","bad");return}
  const body=new FormData();body.append("file",file,file.name);
  const b=form.querySelector('[type="submit"]');b.disabled=true;
  try{
   const result=await fetch(prefix+"/library?workspace_id="+encodeURIComponent(source.id),{
    method:"POST",body,credentials:"same-origin",headers:{"X-OnePane-CSRF":csrfCookie()}
   });
   const data=await result.json();
   if(!result.ok)throw new Error(data.error||"Upload failed");
   notice("Library asset added: "+data.name);
   await a46RenderLibrary(project,workspace,root);
  }catch(err){b.disabled=false;notice("Library upload failed: "+err.message,"bad")}
 });
 root.querySelectorAll("[data-a46-versions]").forEach(b=>b.addEventListener("click",async()=>{
  const id=b.dataset.a46Versions;const section=root.querySelector(`[data-a46-version-list="${CSS.escape(id)}"]`);
  if(!section)return;
  if(!section.hidden){section.hidden=true;return}
  b.disabled=true;section.hidden=false;section.textContent="Loading versions…";
  try{
   const versions=await apiRequest(prefix+"/library/"+encodeURIComponent(id)+"/versions");
   section.innerHTML=Array.isArray(versions)&&versions.length?versions.map(v=>`<div class="a46-library-version">
    <strong>v${Number(v.version)}</strong><span class="list-meta">${escapeHtml(v.content_hash)} · ${Number(v.size_bytes)} bytes</span>
   </div>`).join(""):'<span class="list-meta">No versions.</span>';
  }catch(err){section.textContent="Version history unavailable: "+err.message}finally{b.disabled=false}
 }));
 root.querySelectorAll("[data-a46-grant]").forEach(b=>b.addEventListener("click",()=>{
  const asset=assets.find(a=>a.id===b.dataset.a46Grant);if(!asset)return;
  openModal("Grant Library access",`<form id="a46GrantForm" class="qa-form">
    <p class="list-meta">Grant one Workspace read access. This does not grant tool execution, deletion or modification rights.</p>
    <label>Workspace<select name="workspace_id">${options}</select></label>
    <label>Version selection<select name="version_policy">
      <option value="pinned">Pinned version (recommended)</option><option value="latest">Latest version (moves with updates)</option></select></label>
    <label>Pinned version<input type="number" name="pinned_version" min="1" value="${Number(asset.current_version)}"></label>
    <div id="a46GrantError" class="error"></div><button class="btn primary" type="submit">Grant read access</button>
   </form>`);
  const gf=document.querySelector("#a46GrantForm");
  gf.onsubmit=async e=>{
   e.preventDefault();const params=Object.fromEntries(new FormData(gf));
   const submit=gf.querySelector('[type="submit"]');submit.disabled=true;
   try{
    await apiRequest(prefix+"/library/"+encodeURIComponent(asset.id)+"/grants",{
     method:"POST",body:JSON.stringify({workspace_id:params.workspace_id,version_policy:params.version_policy,pinned_version:Number(params.pinned_version)})
    });
    closeModal();notice("Workspace Library read grant saved.");
   }catch(err){submit.disabled=false;gf.querySelector("#a46GrantError").textContent=err.message}
  };
 }));
}
