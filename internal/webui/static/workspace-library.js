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
 const globalLibrary=root.dataset.globalLibrary==="true";
 const query=String(root.dataset.libraryQuery||"").trim().slice(0,256);
 const suffix=query?"?q="+encodeURIComponent(query):"";
 let workspaces=[],assets=[],source=null;
 try{
  const list=await apiRequest(prefix+"/workspaces");
  workspaces=Array.isArray(list)?list:[];
  source=globalLibrary?{id:"",name:"Project Library"}:workspaces.find(w=>a45BackendLegacyID(w)===String(workspace.id));
  // A Workspace may see ONLY its explicit grant/published inventory; the
  // broader Project Library is accessible from the Project-owned Library page.
  if(globalLibrary){const response=await apiRequest(prefix+"/library"+suffix);assets=Array.isArray(response)?response:[]}
  else if(source?.id){const response=await apiRequest(prefix+"/workspaces/"+encodeURIComponent(source.id)+"/library"+suffix);assets=Array.isArray(response)?response:[]}
 }catch(e){if(root.isConnected)root.innerHTML=`<h3>Project Library</h3><div class="error" role="alert">${escapeHtml(e.message)}</div>`;return}
 if(!root.isConnected)return;
 const labelOf=w=>escapeHtml(w.name||w.id||"Workspace");
 const targets=workspaces.filter(w=>w.status==="active");
 const options=targets.map(w=>`<option value="${escapeHtml(w.id)}" ${w.id===source?.id?"selected":""}>${labelOf(w)}</option>`).join("");
 const cards=assets.map(a=>`<article class="a46-library-asset" data-a46-asset="${escapeHtml(a.id)}">
  <div class="a45-link-header"><div><strong>${escapeHtml(a.name)}</strong>
    <div class="list-meta">${escapeHtml(a.asset_type)} · v${Number(globalLibrary?a.current_version:(a.accessible_version||a.current_version))} · ${escapeHtml(a.id)}</div></div>
    <span class="pill">Versioned</span></div>
  ${source?.id?'<label class="inline-check a62-evidence-pick"><input type="checkbox" data-a62-asset="'+escapeHtml(a.id)+
   '" data-a62-version="'+Number(a.accessible_version||a.current_version)+'"> Include this exact version in an evidence manifest</label>':""}
  <div class="toolbar a45-link-actions">
   ${globalLibrary||source?.id?`<button type="button" class="btn" data-a46-versions="${escapeHtml(a.id)}">Versions</button>`:""}
   ${globalLibrary?`<button type="button" class="btn" data-a46-grant="${escapeHtml(a.id)}">Grant Workspace access</button>`:""}
   ${source?.id?`<a class="btn" href="${prefix}/library/${encodeURIComponent(a.id)}/versions/${Number(a.accessible_version||a.current_version)}/content?workspace_id=${encodeURIComponent(source.id)}" title="Rechecked against current Workspace grants when downloaded">Download authorised version</a>`:""}
  </div><div class="a46-versions" data-a46-version-list="${escapeHtml(a.id)}" hidden></div>
 </article>`).join("");
 root.innerHTML=`<h3>Project Library</h3>
 <p class="list-meta">Shared Project storage, versioned and permissioned per Workspace. Uploading an asset to this Workspace does not give other sandboxes permission to read it.</p>
 <form class="a46-upload" id="a46UploadForm">
  <label>New asset<input type="file" name="file" required></label>
  <button class="btn primary" type="submit" ${source?"":"disabled"}>${globalLibrary?"Upload to Project Library":"Upload into "+escapeHtml(workspace.name||"Workspace")+" Library"}</button>
 </form>
 ${!source?'<p class="list-meta">Register this Workspace under Workspace connections to enable uploads.</p>':""}
 <div class="toolbar a46-recovery-toolbar"><button id="a46RecoverArtifact" type="button" class="btn">Adopt existing managed artifact</button></div>
 <form id="a46SearchAssetsForm" class="a46-library-filter toolbar">
   <label>Search filenames / MIME types<input id="a46FilterAssets" name="query" maxlength="256"
    value="${escapeHtml(query)}" placeholder="Filename or file type…" aria-label="Search permitted Library metadata"></label>
   <button class="btn" type="submit">Search Library</button>
   <button class="btn" type="button" id="a46ClearSearch" ${query?"":"disabled"}>Clear</button>
 </form>
 <div class="a46-library-items">${cards||'<div class="empty-state compact">No Project Library assets. Upload a document, source artifact or asset to begin.</div>'}</div>
 ${source?.id?'<div class="a62-evidence-panel"><div class="toolbar"><button type="button" class="btn" data-a62-build disabled>Build scoped evidence manifest</button><button type="button" class="btn" data-a63-read disabled>Read verified text (up to 8)</button></div><p class="list-meta">Metadata: choose up to 16 exact versions. Text retrieval: choose up to 8 UTF-8 documents, 64 KiB each, 256 KiB total. Every read is hash checked and permissions are rechecked. Untrusted text is for operator review only: it is not sent to the Orchestrator or Council.</p><pre class="a62-evidence-receipt" data-a62-result hidden aria-label="Scoped Workspace evidence metadata manifest"></pre><pre class="a62-evidence-receipt" data-a63-result hidden aria-label="Verified untrusted Workspace Library text"></pre><label class="a64-search-label">Search selected verified text (2–64 characters)<input type="search" data-a64-query maxlength="64" placeholder="Phrase to find in approved versions"></label><div class="toolbar"><button type="button" class="btn" data-a64-search disabled>Search selected text</button></div><pre class="a62-evidence-receipt" data-a64-results hidden aria-label="Permission-checked evidence search snippets"></pre></div>':""}`;
 // Evidence selection is deliberate and read-only. Server revalidates each
 // selected immutable version against the current direct grant/publication.
 const build=root.querySelector("[data-a62-build]");
 if(build && source?.id){
  const selected=()=>Array.from(root.querySelectorAll('[data-a62-asset]:checked')).map(el=>({
   asset_id:String(el.dataset.a62Asset||""),
   version:Number(el.dataset.a62Version||0)
  }));
  const read=root.querySelector("[data-a63-read]");
  const search=root.querySelector("[data-a64-search]");
  const searchInput=root.querySelector("[data-a64-query]");
  const update=()=>{
   const count=selected().length;
   build.disabled=count===0;
   if(read)read.disabled=count===0||count>8;
   if(search)search.disabled=count===0||count>8||String(searchInput?.value||"").trim().length<2;
  };
  root.querySelectorAll("[data-a62-asset]").forEach(input=>input.addEventListener("change",update));
  searchInput?.addEventListener("input",update);
  search?.addEventListener("click",async()=>{
   const selections=selected(),query=String(searchInput?.value||"").trim();
   if(!selections.length||selections.length>8||query.length<2||query.length>64){
    notice("Select up to eight authorised text versions and a 2–64 character query.","bad");return;
   }
   search.disabled=true;
   const output=root.querySelector("[data-a64-results]");
   if(output){output.textContent="";output.hidden=true;}
   try{
    const result=await apiRequest(prefix+"/workspaces/"+encodeURIComponent(source.id)+
     "/evidence-packets/search-text",{
     method:"POST",body:JSON.stringify({selections,query})
    });
    if(!root.isConnected)return;
    if(output){output.textContent=JSON.stringify(result,null,2);output.hidden=false;}
    notice("Permission-checked content search complete; no index or model prompt created.");
   }catch(err){notice("Evidence search denied: "+err.message,"bad")}
   finally{update()}
  });
  read?.addEventListener("click",async()=>{
   const selections=selected();
   if(!selections.length||selections.length>8){
    notice("Select one to eight small text Library versions.","bad");return;
   }
   read.disabled=true;
   const output=root.querySelector("[data-a63-result]");
   if(output){output.hidden=true;output.textContent="";}
   try{
    const evidence=await apiRequest(prefix+"/workspaces/"+encodeURIComponent(source.id)+
     "/evidence-packets/verified-text",{
     method:"POST",body:JSON.stringify({selections})
    });
    if(!root.isConnected)return;
    if(output){
     output.hidden=false;
     // Never parse untrusted Library text into DOM markup or execute it.
     output.textContent=JSON.stringify(evidence,null,2);
    }
    notice("Evidence text verified for local operator inspection only.");
   }catch(err){notice("Verified evidence read denied: "+err.message,"bad")}
   finally{update()}
  });
  build.addEventListener("click",async()=>{
   const selections=selected();
   if(selections.length<1||selections.length>16){
    notice("Select between one and sixteen authorised asset versions.","bad");return;
   }
   build.disabled=true;
   try{
    const packet=await apiRequest(prefix+"/workspaces/"+encodeURIComponent(source.id)+"/evidence-packets",{
     method:"POST",body:JSON.stringify({selections})
    });
    const output=root.querySelector("[data-a62-result]");
    if(output&&root.isConnected){
     output.hidden=false;
     output.textContent=JSON.stringify(packet,null,2);
    }
    notice("Scoped evidence metadata assembled; no document content or new access was granted.");
   }catch(err){notice("Evidence selection denied: "+err.message,"bad")}
   finally{update()}
  });
 }
 root.querySelector("#a46RecoverArtifact")?.addEventListener("click",()=>{
  openModal("Adopt existing OnePane artifact",`<form id="a46AdoptForm" class="qa-form">
   <p class="list-meta">Recover a verified managed artifact already stored by OnePane. No file is moved or deleted, and artifacts from other Projects or tenants are not accepted.</p>
   <label>Existing Artifact ID<input name="artifact_id" required placeholder="Managed artifact ID"></label>
   <label>Library display name<input name="name" maxlength="240" placeholder="Recovered research or build artifact"></label>
   <div id="a46AdoptError" class="error"></div><button type="submit" class="btn primary">Verify and adopt</button>
  </form>`);
  const af=document.querySelector("#a46AdoptForm");
  af.onsubmit=async e=>{
   e.preventDefault();const input=Object.fromEntries(new FormData(af));
   const btn=af.querySelector('[type="submit"]');btn.disabled=true;
   try{
    const record=await apiRequest(prefix+"/library/import-managed",{
     method:"POST",body:JSON.stringify({artifact_id:input.artifact_id,name:input.name,
      source_workspace_id:source?.id||""})
    });
    closeModal();notice("Existing artifact adopted: "+(record.name||input.artifact_id));
    await a46RenderLibrary(project,workspace,root);
   }catch(err){btn.disabled=false;af.querySelector("#a46AdoptError").textContent=err.message}
  };
 });
 root.querySelector("#a46SearchAssetsForm")?.addEventListener("submit",e=>{
  e.preventDefault();
  root.dataset.libraryQuery=String(root.querySelector("#a46FilterAssets")?.value||"").trim().slice(0,256);
  a46RenderLibrary(project,workspace,root);
 });
 root.querySelector("#a46ClearSearch")?.addEventListener("click",()=>{
  root.dataset.libraryQuery="";
  a46RenderLibrary(project,workspace,root);
 });
 root.querySelector("#a46FilterAssets")?.addEventListener("input",e=>{
  const query=String(e.target.value||"").toLowerCase().trim();
  root.querySelectorAll(".a46-library-asset").forEach(card=>{
    card.hidden=!!query&&!card.textContent.toLowerCase().includes(query);
  });
 });
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
   const versionsURL=source?.id
    ?prefix+"/workspaces/"+encodeURIComponent(source.id)+"/library/"+encodeURIComponent(id)+"/versions"
    :prefix+"/library/"+encodeURIComponent(id)+"/versions";
   const versions=await apiRequest(versionsURL);
   section.innerHTML=Array.isArray(versions)&&versions.length?versions.map(v=>`<div class="a46-library-version">
    <strong>v${Number(v.version)}</strong>
    <span class="list-meta">${escapeHtml(v.content_hash)} · ${Number(v.size_bytes)} bytes</span>
    ${source?.id?`<a class="btn" href="${prefix}/library/${encodeURIComponent(id)}/versions/${Number(v.version)}/content?workspace_id=${encodeURIComponent(source.id)}" title="Permission rechecked when downloaded">Download v${Number(v.version)}</a>`:""}
    ${source?.id && Number(v.size_bytes)<=262144 && /^(text\/|application\/(json|xml|javascript|x-yaml|yaml|toml))/.test(String(v.mime_type||"").toLowerCase())?`<button type="button" class="btn" data-a46-preview="${Number(v.version)}">Preview v${Number(v.version)}</button>`:""}
   </div>`).join(""):'<span class="list-meta">No versions.</span>';
   // Version preview is Workspace-scoped and rechecked by the server on
   // every read. Untrusted asset bytes must never be inserted as HTML.
   section.querySelectorAll("[data-a46-preview]").forEach(previewButton=>previewButton.addEventListener("click",async()=>{
    const row=previewButton.closest(".a46-library-version");
    if(!row)return;
    const existing=row.querySelector(".a46-library-preview");
    if(existing){existing.remove();return}
    previewButton.disabled=true;
    try{
     const version=Number(previewButton.dataset.a46Preview);
     const uri=prefix+"/workspaces/"+encodeURIComponent(source.id)+
      "/library/"+encodeURIComponent(id)+"/versions/"+version+"/preview";
     const response=await fetch(uri,{method:"GET",credentials:"same-origin",cache:"no-store",
      headers:{"Accept":"text/plain"}});
     if(!response.ok)throw new Error("Preview denied or unavailable (HTTP "+response.status+")");
     const content=await response.text();
     if(!section.isConnected)return;
     const pane=document.createElement("pre");
     pane.className="a46-library-preview";
     pane.setAttribute("aria-label","Plain text preview of Library version "+version);
     pane.textContent=content;
     row.append(pane);
    }catch(err){notice("Library preview: "+err.message,"bad")}
    finally{previewButton.disabled=false}
   }));
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
