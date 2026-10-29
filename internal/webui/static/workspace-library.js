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
 const projectID=String(project.id),workspaceID=String(workspace.id);
 const generation=String((Number(root.dataset.libraryGeneration)||0)+1);
 root.dataset.libraryGeneration=generation;
 const current=()=>root.isConnected&&root.dataset.libraryGeneration===generation;
 // Clear prior private excerpts immediately when a new scope/render starts.
 // A slow replacement request must not keep the previous Workspace visible.
 root.textContent="Loading authorised Library…";
 const prefix=`/v1/projects/${encodeURIComponent(projectID)}`;
 const globalLibrary=root.dataset.globalLibrary==="true";
 const query=String(root.dataset.libraryQuery||"").trim().slice(0,256);
 const suffix=query?"?q="+encodeURIComponent(query):"";
 let workspaces=[],assets=[],source=null;
 try{
  const list=await apiRequest(prefix+"/workspaces");
  workspaces=Array.isArray(list)?list:[];
  source=globalLibrary?{id:"",name:"Project Library"}:workspaces.find(w=>a45BackendLegacyID(w)===workspaceID);
  // A Workspace may see ONLY its explicit grant/published inventory; the
  // broader Project Library is accessible from the Project-owned Library page.
  if(globalLibrary){const response=await apiRequest(prefix+"/library"+suffix);assets=Array.isArray(response)?response:[]}
  else if(source?.id){const response=await apiRequest(prefix+"/workspaces/"+encodeURIComponent(source.id)+"/library"+suffix);assets=Array.isArray(response)?response:[]}
 }catch(e){if(current())root.innerHTML=`<h3>Project Library</h3><div class="error" role="alert">${escapeHtml(e.message)}</div>`;return}
 if(!current())return;
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
 ${source?.id?'<div class="a62-evidence-panel"><div class="toolbar"><button type="button" class="btn" data-a62-build disabled>Build scoped evidence manifest</button><button type="button" class="btn" data-a63-read disabled>Read verified text (up to 8)</button><button type="button" class="btn" data-a65-freeze disabled>Approve Research evidence metadata</button></div><p class="list-meta">Metadata: choose up to 16 exact versions. Text retrieval: choose up to 8 UTF-8 documents, 64 KiB each, 256 KiB total. Every read is hash checked and permissions are rechecked. Untrusted text is for operator review only: it is not sent to the Orchestrator or Council.</p><pre class="a62-evidence-receipt" data-a62-result hidden aria-label="Scoped Workspace evidence metadata manifest"></pre><pre class="a62-evidence-receipt" data-a63-result hidden aria-label="Verified untrusted Workspace Library text"></pre><label class="a64-search-label">Search selected verified text (2–64 characters)<input type="search" data-a64-query maxlength="64" placeholder="Phrase to find in approved versions"></label><div class="toolbar"><button type="button" class="btn" data-a64-search disabled>Search selected text</button></div><pre class="a62-evidence-receipt" data-a64-results hidden aria-label="Permission-checked evidence search snippets"></pre></div>':""}`;
 if(source?.id){
  const searchHost=document.createElement("section");searchHost.className="a62-evidence-panel";
  root.append(searchHost);
  mountWorkspaceContentSearch(searchHost,{projectID,workspaceID:String(source.id)},current);
  for(const asset of assets){
   const card=root.querySelector(`[data-a46-asset="${CSS.escape(asset.id)}"]`);
   if(!card)continue;
   const textHost=document.createElement("section");textHost.className="a62-evidence-panel";card.append(textHost);
   const selection={projectID,workspaceID:String(source.id),assetID:String(asset.id),version:Number(asset.accessible_version||asset.current_version)};
   if(asset.asset_type==="application/vnd.openxmlformats-officedocument.wordprocessingml.document")mountLibraryDocumentIndex(textHost,selection,current);
   else mountLibraryTextIndex(textHost,selection,current);
  }
 }
 // An operator can reopen previously frozen Research approvals without
 // copying opaque Council IDs, including after leaving the approval dialog.
 if(source?.id){
  const historyPanel=document.createElement("section");
  historyPanel.className="a62-evidence-panel";
  historyPanel.innerHTML='<div class="toolbar"><strong>Frozen Research approvals</strong>'+
   '<button type="button" class="btn" data-a67-load>Show saved approvals</button></div>'+
   '<p class="list-meta">Review exact Research sessions and optionally recheck frozen text integrity. Saved metadata is not a model attachment, and no Library content is loaded automatically.</p>'+
   '<div data-a67-results aria-live="polite"></div>';
  root.appendChild(historyPanel);
  a67BindFrozenResearchHistory(historyPanel,prefix,source.id);
 }
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
  const freeze=root.querySelector("[data-a65-freeze]");
  const update=()=>{
   const count=selected().length;
   build.disabled=count===0;
   if(read)read.disabled=count===0||count>8;
   if(freeze)freeze.disabled=count===0||count>16;
   if(search)search.disabled=count===0||count>8||String(searchInput?.value||"").trim().length<2;
  };
  root.querySelectorAll("[data-a62-asset]").forEach(input=>input.addEventListener("change",update));
  searchInput?.addEventListener("input",update);
  freeze?.addEventListener("click",async()=>{
   const selections=selected();
   if(!selections.length||selections.length>16){
    notice("Select between one and sixteen permitted evidence versions.","bad");return;
   }
   freeze.disabled=true;
   let sessions;
   try{
    sessions=await apiRequest(prefix+"/workspaces/"+encodeURIComponent(source.id)+
     "/research/evidence-sessions");
   }catch(ex){
    notice("Cannot list eligible Research Council sessions: "+ex.message,"bad");
    update();return;
   }
   update();
   if(!Array.isArray(sessions)||!sessions.length){
    notice("No eligible pre-round Research Council sessions for this Project Workspace. Create a Research session from a Workspace Task first.","bad");
    return;
   }
   const options=sessions.map(row=>'<option value="'+escapeHtml(row.id)+'">'+
    escapeHtml((row.team_name||"Research Council")+" — "+(row.task_objective||row.task_id||"Task"))+
    '</option>').join("");
   openModal("Approve Research evidence selection",
    '<form id="a65FreezeEvidenceForm" class="qa-form">'+
    '<p class="list-meta">Select the Research Council Task for this exact Project Workspace. Freeze these '+selections.length+
    ' authorised Library versions before its first round. This stores hash-pinned metadata only; no document content reaches a Council seat.</p>'+
    '<label>Eligible Research session<select name="session_id" required>'+options+'</select></label>'+
    '<label class="inline-check"><input type="checkbox" name="reviewed" required> I reviewed this immutable metadata selection and its destination Council session.</label>'+
    '<div class="error" id="a65FreezeError" role="alert"></div>'+
    '<button type="submit" class="btn primary">Freeze approved metadata</button></form>');
   const form=document.querySelector("#a65FreezeEvidenceForm");
   if(!form)return;
   form.onsubmit=async e=>{
    e.preventDefault();
    if(!form.elements.reviewed?.checked)return;
    const sid=String(form.elements.session_id?.value||"").trim();
    if(!sid||!sessions.some(row=>row.id===sid))return;
    const button=form.querySelector('[type="submit"]');
    button.disabled=true;
    try{
     await apiRequest("/v1/team-sessions/"+encodeURIComponent(sid)+
      "/research/evidence-selection",{
      method:"POST",body:JSON.stringify({selections})
     });
     // Approval does not imply permission to read or forward document text.
     // Offer a distinct, opt-in server-side integrity preflight. The receipt
     // contains hashes, counts and sizes only, never the selected plaintext.
     form.innerHTML='<p class="list-meta">Research evidence metadata is frozen for this session. No document content was sent to a model.</p>'+
      '<p class="list-meta">Optional: verify up to 8 small UTF-8 documents (64 KiB each; 256 KiB total) against the exact frozen selection. Verification reads text only on this OnePane server; it does not save or forward text to any Council seat.</p>'+
      '<button type="button" class="btn" data-a66-preflight>Verify frozen text locally</button> '+
      '<button type="button" class="btn" data-a66-close>Close</button>'+
      '<pre data-a66-result class="a62-evidence-receipt" hidden aria-label="Frozen Research text verification receipt"></pre>';
     form.querySelector("[data-a66-close]")?.addEventListener("click",closeModal);
     form.querySelector("[data-a66-preflight]")?.addEventListener("click",async e=>{
      const verify=e.currentTarget,output=form.querySelector("[data-a66-result]");
      verify.disabled=true;
      if(output){output.hidden=true;output.textContent="";}
      try{
       const receipt=await apiRequest("/v1/team-sessions/"+encodeURIComponent(sid)+
        "/research/evidence-preflight",{method:"POST"});
       if(!form.isConnected)return;
       if(output){output.hidden=false;output.textContent=JSON.stringify(receipt,null,2);}
       notice("Frozen Research text verified for this operator only. No Council attachment or model access was created.");
      }catch(ex){
       if(output){output.hidden=false;output.textContent="Text preflight denied: "+ex.message;}
      }finally{verify.disabled=false;}
     });
     notice("Research metadata frozen. Text verification is optional and requires a separate click.");
    }catch(ex){
     button.disabled=false;
     form.querySelector("#a65FreezeError").textContent=ex.message;
    }
   };
  });

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

function a67BindFrozenResearchHistory(panel,prefix,workspaceID){
 const load=panel.querySelector("[data-a67-load]");
 const output=panel.querySelector("[data-a67-results]");
 if(!load||!output)return;
 load.addEventListener("click",async()=>{
  load.disabled=true;
  output.textContent="Loading scoped Research approval metadata…";
  try{
   const rows=await apiRequest(prefix+"/workspaces/"+
    encodeURIComponent(workspaceID)+"/research/frozen-evidence-sessions");
   if(!panel.isConnected)return;
   if(!Array.isArray(rows))throw Error("Unexpected Research history response");
   output.textContent="";
   if(!rows.length){
    output.textContent="No frozen Research approvals in this Workspace.";
    return;
   }
   for(const item of rows){
    const wrapper=document.createElement("div");
    wrapper.className="a46-library-asset";
    const title=document.createElement("strong");
    title.textContent=String(item.team_name||"Research Council");
    const context=document.createElement("p");
    context.className="list-meta";
    context.textContent=String(item.task_objective||"Research Task")+
     " · "+String(item.status||"unknown")+" · round "+Number(item.round_number||0);
    const verify=document.createElement("button");
    verify.type="button";
    verify.className="btn";
    verify.textContent="Reverify frozen text";
    const receipt=document.createElement("pre");
    receipt.className="a62-evidence-receipt";
    receipt.hidden=true;
    receipt.setAttribute("aria-label","Research evidence re-verification receipt");
    verify.addEventListener("click",async()=>{
     verify.disabled=true;
     receipt.hidden=true;
     receipt.textContent="";
     try{
      const checked=await apiRequest("/v1/team-sessions/"+
       encodeURIComponent(String(item.id||""))+"/research/evidence-preflight",{
       method:"POST"
      });
      if(!panel.isConnected)return;
      receipt.textContent=JSON.stringify(checked,null,2);
      receipt.hidden=false;
      notice("Research text reverified for this operator only. No model access was granted.");
     }catch(ex){
      if(!panel.isConnected)return;
      receipt.textContent="Verification denied: "+ex.message;
      receipt.hidden=false;
     }finally{verify.disabled=false;}
    });
    const share=document.createElement("button");
    share.type="button";
    share.className="btn";
    share.textContent="Review local-only seat consent";
    share.addEventListener("click",()=>a68ReviewResearchSeatConsent(String(item.id||"")));
    wrapper.append(title,context,verify,share,receipt);
    output.appendChild(wrapper);
   }
  }catch(ex){
   if(panel.isConnected)output.textContent="Research approvals unavailable: "+ex.message;
  }finally{load.disabled=false;}
 });
}

async function a68ReviewResearchSeatConsent(sessionID){
 if(!sessionID)return;
 const endpoint="/v1/team-sessions/"+encodeURIComponent(sessionID)+"/research/seat-evidence-approval";
 let manifest;
 try{
  manifest=await apiRequest("/v1/team-sessions/"+encodeURIComponent(sessionID)+"/manifest");
 }catch(ex){notice("Research session access denied: "+ex.message,"bad");return;}
 const snap=manifest?.manifest?.Snapshot||manifest?.manifest?.snapshot||{};
 const members=Array.isArray(snap.members)?snap.members:[];
 const activeParticipants=members.filter(m=>m?.status==="active");
 const hasManualChair=snap?.research?.chair_mode==="manual";
 const candidates=members.filter(m=>{
  let cfg=m?.config||{};
  if(typeof cfg==="string"){try{cfg=JSON.parse(cfg)}catch{return false}}
  return m?.member_kind==="agent"&&m?.status==="active"&&
   !cfg?.manual_web?.enabled&&!cfg?.council_chair_only;
 });
 let prior=null;
 try{prior=await apiRequest(endpoint);}catch{}
 const locked=prior?.state==="approved"||prior?.state==="revoked";
 const fullLocalCouncil=!hasManualChair&&candidates.length>0&&
  candidates.length===activeParticipants.length;
 const rows=candidates.map(m=>{
  const checked=prior?.seat_ids?.includes(m.id)?"checked":"";
  return '<label class="inline-check"><input type="checkbox" data-a68-seat value="'+
   escapeHtml(m.id)+'" '+checked+(locked?' disabled':'')+'> '+
   escapeHtml(m.display_name||m.role_name||"Research seat")+'</label>';
 }).join("");
 openModal("Research evidence — local-only seat intent",
  '<div class="qa-form" id="a68ResearchSeatConsent">'+
  '<p class="list-meta">This is separate human permission for private Workspace Library text to be attached to Research Council local-model prompts. Once approved, OnePane may supply the exact frozen versions to every approved local origin-node seat in subsequent rounds. Only UTF-8 text is eligible, with a strict 32 KiB total delivery cap; no cloud, remote or manual Web sharing.</p>'+
  '<p class="list-meta">ALL active participants must be approved, local eligible agent seats. A mixed or manual-Chair Council cannot receive this evidence because later critique could quote private content. Approval is immutable; permanent revocation halts protected Council dispatch, including subsequent rounds, and a new session is required to restart.</p>'+
  '<div class="team-member-list">'+(rows||'<p>No eligible local-capable Research seats in this frozen session.</p>')+'</div>'+
  '<label class="inline-check"><input type="checkbox" data-a68-confirm '+(locked?'disabled':'')+
  '> I authorize delivery of the selected frozen text to ALL named local-model Council seats in future Research rounds, subject to live permission and hash checks.</label>'+
  '<div class="toolbar">'+
  '<button type="button" class="btn primary" data-a68-approve '+(locked||!fullLocalCouncil?'disabled':'')+
  '>Approve local-only evidence delivery</button>'+
  '<button type="button" class="btn" data-a68-revoke '+(prior?.state==="approved"?'':'disabled')+
  '>Revoke approval</button></div>'+
  '<pre class="a62-evidence-receipt" data-a68-receipt aria-live="polite"></pre></div>');
 const box=document.querySelector("#a68ResearchSeatConsent");
 if(!box)return;
 const report=box.querySelector("[data-a68-receipt]");
 if(prior)report.textContent=JSON.stringify(prior,null,2);
 box.querySelector("[data-a68-approve]")?.addEventListener("click",async e=>{
  const selected=Array.from(box.querySelectorAll("[data-a68-seat]:checked")).map(v=>v.value);
  if(!fullLocalCouncil||selected.length!==candidates.length){
   report.textContent="Protected Research evidence requires every active Council seat to be an approved local agent. Mixed/manual Councils must use a new local-only session.";
   return;
  }
  if(!box.querySelector("[data-a68-confirm]")?.checked||!selected.length){
   report.textContent="Explicit confirmation and at least one selected seat are required.";
   return;
  }
  e.currentTarget.disabled=true;
  try{
   const result=await apiRequest(endpoint,{
    method:"POST",body:JSON.stringify({seat_ids:selected,confirm_local_only:true})
   });
   report.textContent=JSON.stringify(result,null,2);
   box.querySelectorAll("[data-a68-seat], [data-a68-confirm]").forEach(v=>v.disabled=true);
   box.querySelector("[data-a68-revoke]").disabled=false;
   notice("Local-only Research evidence approved for future seat turns; file permissions and hashes are checked again before delivery.");
  }catch(ex){
   report.textContent="Approval denied: "+ex.message;
   e.currentTarget.disabled=false;
  }
 });
 box.querySelector("[data-a68-revoke]")?.addEventListener("click",async e=>{
  if(!confirm("Permanently revoke evidence delivery and halt further protected Council turns? A new Council session will be required."))return;
  e.currentTarget.disabled=true;
  try{
   const result=await apiRequest(endpoint+"/revoke",{method:"POST",body:"{}"});
   report.textContent=JSON.stringify(result,null,2);
   box.querySelector("[data-a68-approve]").disabled=true;
   notice("Research seat evidence intent revoked.");
  }catch(ex){
   report.textContent="Revocation denied: "+ex.message;
   e.currentTarget.disabled=false;
  }
 });
}
