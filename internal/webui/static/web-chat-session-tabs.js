/* Conversation tabs on top of the manual Web Council handoff service.
 * Tab metadata is local UI state; provider browser sessions stay external.
 * Prompts and response drafts are never persisted in localStorage.
 */
const A40_WEB_MAX_TABS = 16;
const a40WebDrafts = new Map();
let a40WebRenderingEpoch = 0;

function a40WebWorkspace() { return String(onepaneWorkspace||""); }
function a40WebAllTabs() {
  if (!Array.isArray(state.webChatSessions)) state.webChatSessions = [];
  // The saved view contains only routing/tab metadata, never remote credentials.
  state.webChatSessions = state.webChatSessions.filter(s =>
    s && typeof s.id==="string" && typeof s.workspace_id==="string" &&
    typeof s.provider_id==="string" && typeof s.title==="string" &&
    s.id.length<160 && s.title.length<=80);
  return state.webChatSessions;
}
function a40WebTabs(ws=a40WebWorkspace()) {
  return a40WebAllTabs().filter(s=>s.workspace_id===ws);
}
function a40WebProvider(id) {
  return a39WebProviderInfo(id) || {id,name:id,url:""};
}
function a40WebActive(ws=a40WebWorkspace()) {
  const tabs=a40WebTabs(ws);
  return tabs.find(s=>s.id===state.webChatActiveSession) || tabs[0] || null;
}
function a40WebNewSession(providerID="chatgpt", title="", silent=false) {
  const ws=a40WebWorkspace();
  if(!ws){notice("Select a Workspace before starting a Web Chat.","bad");return null}
  const provider=a40WebProvider(providerID);
  const all=a40WebTabs(ws);
  if(all.length>=A40_WEB_MAX_TABS){notice("Close a Web Chat tab before opening another.","bad");return null}
  let number=1;
  const names=new Set(all.map(s=>s.title));
  while(names.has(provider.name+" "+number))number++;
  const entry={
    id:"webchat-"+Date.now().toString(36)+"-"+Math.random().toString(36).slice(2,8),
    workspace_id:ws,provider_id:providerID,
    title:String(title||provider.name+" "+number).trim().slice(0,80),
    turn_id:"",conversation_generation:1,
    created_at:Date.now()
  };
  a40WebAllTabs().push(entry);
  state.webChatActiveSession=entry.id;
  persist();
  if(!silent)renderWebChat();
  return entry;
}
function a40WebEnsureSession(rows) {
  let entry=a40WebActive();
  if(entry)return entry;
  const first=rows.find(t=>t.status==="awaiting_input");
  entry=a40WebNewSession(first?.provider_id||"chatgpt","",true);
  if(entry && first)entry.turn_id=first.turn_id;
  persist();
  return entry;
}
function a40WebSwitch(id) {
  if(!a40WebTabs().some(s=>s.id===id))return;
  state.webChatActiveSession=id;
  persist();
  renderWebChat();
}
function a40WebClose(id) {
  const entry=a40WebTabs().find(s=>s.id===id);
  if(!entry)return;
  const draft=a40WebDrafts.get(id);
  if(draft && (draft.response||draft.prompt) &&
    !confirm("Close this Web Chat tab? Its unsent draft will be discarded; queued Council turns will be preserved."))return;
  state.webChatSessions=a40WebAllTabs().filter(s=>s.id!==id);
  a40WebDrafts.delete(id);
  const next=a40WebTabs()[0];
  if(state.webChatActiveSession===id)state.webChatActiveSession=next?.id||"";
  persist();
  renderWebChat();
}
function a40WebOpenNewDialog() {
  openModal("New Web Chat",`
    <form id="a40NewChatForm" class="qa-form">
      <label>Cloud provider<select name="provider_id">
        ${A39_WEB_PROVIDERS.map(p=>`<option value="${escapeHtml(p.id)}">${escapeHtml(p.name)}</option>`).join("")}
      </select></label>
      <label>Conversation label (optional)<input name="title" maxlength="80" placeholder="e.g. ChatGPT – independent research"></label>
      <p class="page-subtitle">Each conversation gets its own OnePane tab, saved workspace selection and Council handoff. The provider's web conversation opens in your browser.</p>
      <button class="btn primary" type="submit">Create Web Chat tab</button>
    </form>`);
  const form=$("#a40NewChatForm");
  if(!form)return;
  form.onsubmit=e=>{
    e.preventDefault();
    const data=new FormData(form);
    const providerID=String(data.get("provider_id")||"chatgpt");
    const label=String(data.get("title")||"");
    const created=a40WebNewSession(providerID,label,true);
    if(!created)return;
    closeModal();renderWebChat();
  };
}
function a40WebAssignTurn(session,turnID) {
  if(!session)return;
  const picked=a39WebTurns.find(t=>t.turn_id===turnID && t.provider_id===session.provider_id);
  if(!picked || picked.status!=="awaiting_input")return;
  if(session.turn_id===turnID)return;
  const existing=a40WebDrafts.get(session.id);
  if(existing && existing.response?.trim() &&
    !confirm("Switch to this Council turn? The current unsent response draft will be discarded."))return;
  a40WebDrafts.delete(session.id);
  session.turn_id=turnID;
  session.conversation_generation=Number(picked.conversation_generation)||1;
  persist();renderWebChat();
}
async function a40WebNewConversation(session, turn) {
  if(!session)return;
  const provider=a40WebProvider(session.provider_id);
  const draft=a40WebDrafts.get(session.id);
  if((draft?.response || draft?.prompt) &&
    !confirm("Start a new provider conversation? Unsent text in this tab will be discarded; its Council turn stays queued."))return;
  // Persist the replacement handoff attempt before opening the provider.
  if(turn?.status==="awaiting_input"){
    try{
      const updated=await apiRequest(`/v1/manual-web/turns/${encodeURIComponent(turn.turn_id)}/new-conversation`,{
        method:"POST",
        body:JSON.stringify({conversation_generation:turn.conversation_generation})
      });
      session.conversation_generation=Number(updated.conversation_generation)||session.conversation_generation+1;
    }catch(ex){notice(ex.message,"bad");return}
  }else{
    session.conversation_generation=(Number(session.conversation_generation)||1)+1;
  }
  a40WebDrafts.delete(session.id);
  persist();
  a39WebOpen(provider.url);
  notice("Handoff restarted. Start a new chat on the provider website; the Council turn is unchanged.");
  renderWebChat();
}
async function a40WebSubmit(session,turn) {
  if(!session || !turn || turn.status!=="awaiting_input")return;
  const response=String(a40WebDrafts.get(session.id)?.response||"").trim();
  if(!response)return notice("Paste the provider's complete response first.","bad");
  if(!confirm("Import this answer as an operator-attested Council response?"))return;
  const button=$("[data-a40-submit]");
  if(button)button.disabled=true;
  try{
    await apiRequest(`/v1/manual-web/turns/${encodeURIComponent(turn.turn_id)}/submit`,{
      method:"POST",body:JSON.stringify({
        response_text:response,
        conversation_generation:turn.conversation_generation
      })
    });
    a40WebDrafts.delete(session.id);
    notice("Council response recorded. OnePane will continue when the round is complete.");
    renderWebChat();
  }catch(ex){
    notice(ex.message,"bad");
    if(button)button.disabled=false;
  }
}
async function a40RenderWebChat(){
  const epoch=++a40WebRenderingEpoch;
  const host=$("#viewHost");if(!host)return;
  host.innerHTML=`<section class="page a39-webchat-page a40-webchat-page">
    ${pageHeader("Web Chat","Separate manual provider conversations and Research Council handoffs.",'<button class="btn" id="a40NewChatTop">+ New Web Chat</button><button class="btn" id="a40Refresh">Refresh</button>')}
    <div id="a40WebRoot" class="a39-webchat-loading">Loading Web Chat…</div>
  </section>`;
  $("#a40NewChatTop").onclick=a40WebOpenNewDialog;
  $("#a40Refresh").onclick=()=>renderWebChat();
  const workspace=a40WebWorkspace();
  if(!workspace){
    $("#a40WebRoot").innerHTML='<div class="empty-state compact">Select a Workspace to manage Web Chat conversations.</div>';
    return;
  }
  let rows;
  try{
    const data=await apiRequest(`/v1/manual-web/turns?workspace_id=${encodeURIComponent(workspace)}`);
    rows=a31Array(data);
  }catch(ex){
    const element=$("#a40WebRoot");
    if(element&&epoch===a40WebRenderingEpoch)element.innerHTML=`<div class="error">Council handoffs unavailable: ${escapeHtml(ex.message)}</div>`;
    return;
  }
  if(epoch!==a40WebRenderingEpoch || currentTab()?.route!=="webchat")return;
  a39WebTurns=rows;
  const active=a40WebEnsureSession(rows);
  if(!active)return;
  // Distinct session IDs let ChatGPT #1 and ChatGPT #2 retain independent
  // Council selections and drafts, even when they share a cloud provider.
  const sessions=a40WebTabs(workspace);
  const provider=a40WebProvider(active.provider_id);
  const associated=rows.filter(t=>t.provider_id===active.provider_id);
  const selected=associated.find(t=>t.turn_id===active.turn_id)||null;
  const pendingCount=rows.filter(t=>t.status==="awaiting_input").length;
  const originalDraft=a40WebDrafts.get(active.id)||{prompt:"",response:""};
  const attached=!!selected;
  const draft=selected?selected.prompt_text:originalDraft.prompt;
  const response=originalDraft.response;
  const title=selected?"Council handoff":"Independent Web Chat";
  const status=selected?(selected.status==="awaiting_input"?"Awaiting your response":"Submitted"):"Not linked to a Council turn";
  const safe=escapeHtml;
  $("#a40WebRoot").innerHTML=`
    <div class="a39-webchat-summary">
      <strong>${pendingCount} pending Council handoff${pendingCount===1?"":"s"}</strong>
      <span class="list-meta">Each Web Chat tab holds an independent conversation and Council selection; the provider website remains external.</span>
    </div>
    <div class="a40-chat-tabs" role="tablist" aria-label="Web Chat conversations">
      ${sessions.map(s=>`
        <div class="a40-chat-tab ${s.id===active.id?"active":""}" role="presentation">
          <button type="button" role="tab" aria-selected="${s.id===active.id}" data-a40-switch="${safe(s.id)}" title="${safe(s.title)}">
            <span class="a40-provider-mark" aria-hidden="true">☁</span>
            <span class="a40-tab-label">${safe(s.title)}</span>
            ${rows.some(t=>t.turn_id===s.turn_id && t.status==="awaiting_input")?'<span class="a40-tab-pending" title="Council input required">•</span>':""}
          </button>
          <button class="a40-tab-close" type="button" data-a40-close="${safe(s.id)}" title="Close Web Chat tab" aria-label="Close ${safe(s.title)}">×</button>
        </div>`).join("")}
      <button class="a40-add-tab" type="button" id="a40AddWebChat" title="New Web Chat">+ New Chat</button>
    </div>
    <div class="a39-webchat-layout a40-webchat-layout">
      <section class="panel-card a39-webchat-provider-card">
        <div class="card-header">
          <div><div class="card-title">${safe(active.title)}</div>
          <div class="list-meta">${safe(provider.name)} · Conversation ${Number(selected?.conversation_generation||active.conversation_generation)||1}</div></div>
        </div>
        <div class="widget-body">
          <p>Use your signed-in ${safe(provider.name)} website. OnePane does not embed, automate or read your browser session.</p>
          <div class="toolbar">
            ${provider.url?`<a class="btn primary" href="${safe(provider.url)}" target="_blank" rel="noopener noreferrer">Open ${safe(provider.name)}</a>`:""}
            <button class="btn" id="a40NewConversation">New Conversation</button>
          </div>
          <p class="list-meta">Review prompts before sharing project information with a third-party cloud service.</p>
          <h3>Available Council handoffs</h3>
          <div class="a39-webchat-queue">
            ${associated.filter(t=>t.status==="awaiting_input"||t.turn_id===active.turn_id).map(t=>`
              <button class="a39-webchat-turn ${selected?.turn_id===t.turn_id?"selected":""}" data-a40-assign="${safe(t.turn_id)}" type="button">
                <strong>${safe(t.model_label)} · ${safe(t.member_id)}</strong>
                <span class="list-meta">${safe(t.status==="awaiting_input"?"Awaiting response":"Submitted")} · Conversation ${Number(t.conversation_generation)||1}</span>
              </button>
            `).join("")||'<p class="list-meta">No pending Council handoffs for this provider. This tab can be used independently.</p>'}
          </div>
          <button class="btn" id="a40UnlinkTurn" type="button" ${!selected?"disabled":""}>Show independent chat pad</button>
        </div>
      </section>
      <section class="panel-card a39-webchat-handoff-card">
        <div class="card-header">
          <div><div class="card-title">${title}</div>
            <div class="list-meta">${safe(status)} · ${safe(provider.name)} · ${safe(selected?.model_label||"Operator selected model")}</div>
          </div>
        </div>
        <div class="widget-body a39-webchat-handoff-content">
          <label class="a39-webchat-label" for="a40Prompt">${attached?"Prepared Council prompt":"Prompt scratchpad (local, not submitted to Council)"}</label>
          <textarea class="a39-webchat-textarea" id="a40Prompt" rows="11" ${attached?"readonly":""} placeholder="Write or paste a prompt for this web conversation.">${safe(draft)}</textarea>
          <div class="toolbar">
            <button class="btn primary" id="a40CopyPrompt" type="button">Copy Prompt</button>
            ${provider.url?`<a class="btn" href="${safe(provider.url)}" target="_blank" rel="noopener noreferrer">Open Provider</a>`:""}
          </div>
          <label class="a39-webchat-label" for="a40Response">${attached?"Paste provider response":"Response scratchpad"}</label>
          <textarea class="a39-webchat-textarea" id="a40Response" rows="10" ${selected?.status==="submitted"?"disabled":""} placeholder="Paste the provider's response here.">${safe(response)}</textarea>
          <div class="toolbar">
            ${attached?`<button class="btn primary" data-a40-submit type="button" ${selected.status!=="awaiting_input"?"disabled":""}>Submit to Council</button>`:""}
            <span class="list-meta">${attached?"Manual consultation only · no tools or task authority":"Text in an unlinked scratchpad is not automatically saved or routed to a Council."}</span>
          </div>
        </div>
      </section>
    </div>`;
  $$("[data-a40-switch]").forEach(b=>b.onclick=()=>a40WebSwitch(b.dataset.a40Switch));
  $$("[data-a40-close]").forEach(b=>b.onclick=()=>a40WebClose(b.dataset.a40Close));
  $("#a40AddWebChat").onclick=a40WebOpenNewDialog;
  $("#a40NewConversation").onclick=()=>a40WebNewConversation(active,selected);
  $("#a40UnlinkTurn").onclick=()=>{
    if(a40WebDrafts.get(active.id)?.response?.trim()&&!confirm("Unlink the Council handoff from this view? Its unsent response draft will remain local to this tab."))return;
    active.turn_id="";persist();renderWebChat();
  };
  $$("[data-a40-assign]").forEach(b=>b.onclick=()=>a40WebAssignTurn(active,b.dataset.a40Assign));
  const promptField=$("#a40Prompt"),responseField=$("#a40Response");
  if(!attached && promptField)promptField.oninput=()=>{
    const current=a40WebDrafts.get(active.id)||{};
    a40WebDrafts.set(active.id,{...current,prompt:promptField.value});
  };
  if(responseField)responseField.oninput=()=>{
    const current=a40WebDrafts.get(active.id)||{};
    a40WebDrafts.set(active.id,{...current,response:responseField.value});
  };
  $("#a40CopyPrompt").onclick=async()=>{
    try{
      await a39WebClipboard(promptField?.value||"");
      notice("Web Chat prompt copied to clipboard.");
    }catch(ex){notice(ex.message,"bad")}
  };
  if(attached) $("[data-a40-submit]").onclick=()=>a40WebSubmit(active,selected);
}
// OnePane's route renderer calls this symbol. Keep the original provider
// picker code as a compatibility fallback for older browser caches.
renderWebChat=a40RenderWebChat;
