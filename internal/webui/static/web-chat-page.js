/* Web Chat is a separate OnePane page, not the floating Assistant chat.
   External provider authentication and conversation UI stay on their sites.
   We deliberately never iframe or scrape a provider login/session. */
const A39_WEB_PROVIDERS = Object.freeze([
  {id:"chatgpt",name:"ChatGPT",url:"https://chatgpt.com/"},
  {id:"claude",name:"Claude",url:"https://claude.ai/new"},
  {id:"gemini",name:"Gemini",url:"https://gemini.google.com/app"},
  {id:"perplexity",name:"Perplexity",url:"https://www.perplexity.ai/"},
  {id:"grok",name:"Grok",url:"https://grok.com/"}
]);
let a39WebProvider="chatgpt";
let a39WebTurnID="";
let a39WebTurns=[];
const a39WebDrafts=new Map();

function a39WebProviderInfo(id){
  return A39_WEB_PROVIDERS.find(x=>x.id===id)||null;
}
function a39WebClipboard(text){
  if(navigator.clipboard?.writeText) return navigator.clipboard.writeText(text);
  const node=document.createElement("textarea");
  node.value=text;node.style.position="fixed";node.style.left="-9999px";
  document.body.appendChild(node);node.select();
  try {if(!document.execCommand("copy"))throw Error("Copy is unavailable in this browser")} finally {node.remove()}
  return Promise.resolve();
}
function a39WebOpen(url){
  if(!url)return notice("This provider has no registered website. Open it manually.","bad");
  const windowRef=window.open(url,"_blank","noopener,noreferrer");
  if(!windowRef)notice("Browser blocked the new tab. Use the provider's website link instead.","bad");
}
async function a39WebNewConversation(turn) {
  const site=a39WebProviderInfo(turn?.provider_id||a39WebProvider);
  if(!turn){
    a39WebOpen(site?.url);
    return;
  }
  if(!confirm("Start a new provider conversation? This keeps the Council seat and prompt but discards the current unsent draft."))return;
  try{
    const next=await apiRequest(`/v1/manual-web/turns/${encodeURIComponent(turn.turn_id)}/new-conversation`,{
      method:"POST",body:JSON.stringify({conversation_generation:turn.conversation_generation})
    });
    a39WebDrafts.delete(turn.turn_id);
    a39WebTurnID=turn.turn_id;
    a39WebProvider=turn.provider_id;
    a39WebOpen(site?.url);
    notice("New provider conversation started. Copy the refreshed handoff prompt.");
    await renderWebChat();
    return next;
  }catch(ex){notice(ex.message,"bad")}
}
async function a39WebSubmit(turn,raw){
  const response=String(raw||"").trim();
  if(!response)return notice("Paste the provider's complete response first.","bad");
  if(!confirm("Import this answer into the Council as an operator-attested response?"))return;
  try{
    await apiRequest(`/v1/manual-web/turns/${encodeURIComponent(turn.turn_id)}/submit`,{
      method:"POST",body:JSON.stringify({
        response_text:response,conversation_generation:turn.conversation_generation
      })
    });
    a39WebDrafts.delete(turn.turn_id);
    notice("Council response recorded. OnePane will continue when the round is complete.");
    await renderWebChat();
  }catch(ex){notice(ex.message,"bad")}
}
async function renderWebChat(){
  const root=$("#viewHost");
  if(!root)return;
  const wid=encodeURIComponent(onepaneWorkspace||"");
  root.innerHTML=`<section class="page a39-webchat-page">
    ${pageHeader("Web Chat","Manual consultation with cloud-provider websites. No browser automation, API bridge or local tool access.",'<button class="btn" id="a39WebRefresh">Refresh Council queue</button>')}
    <div id="a39WebChatRoot" class="a39-webchat-loading">Loading Web Chat…</div>
  </section>`;
  $("#a39WebRefresh").onclick=()=>renderWebChat();
  if(!onepaneWorkspace){
    $("#a39WebChatRoot").innerHTML='<div class="empty-state">Select a workspace to view Web Council handoffs.</div>';return;
  }
  try{
    const out=await apiRequest(`/v1/manual-web/turns?workspace_id=${wid}`);
    a39WebTurns=a31Array(out);
  }catch(ex){
    $("#a39WebChatRoot").innerHTML=`<div class="error">Could not load Web Council handoffs: ${escapeHtml(ex.message)}</div>`;
    return;
  }
  // Show the selected provider plus any other provider present in queued work.
  const providers=[...A39_WEB_PROVIDERS];
  for(const t of a39WebTurns){
    if(!providers.some(p=>p.id===t.provider_id)){
      providers.push({id:t.provider_id,name:t.provider_id,url:""});
    }
  }
  if(!providers.some(p=>p.id===a39WebProvider))a39WebProvider=providers[0].id;
  const perProvider=a39WebTurns.filter(t=>t.provider_id===a39WebProvider);
  const awaiting=perProvider.filter(t=>t.status==="awaiting_input");
  const selected=perProvider.find(t=>t.turn_id===a39WebTurnID) || awaiting[0] || perProvider[0] || null;
  if(selected)a39WebTurnID=selected.turn_id;
  const provider=providers.find(p=>p.id===a39WebProvider);
  const queueCount=a39WebTurns.filter(t=>t.status==="awaiting_input").length;
  const pendingText=selected?.status==="awaiting_input"?"Awaiting your response":"Response submitted";
  const prompt=selected?String(selected.prompt_text||""):"";
  const draft=selected?a39WebDrafts.get(selected.turn_id)||"":"";
  const escape=escapeHtml;
  $("#a39WebChatRoot").innerHTML=`
    <div class="a39-webchat-summary">
      <strong>${queueCount} pending Council handoff${queueCount===1?"":"s"}</strong>
      <span class="list-meta">OnePane pauses manual Council seats until their replies are submitted. Other independent seats can continue.</span>
    </div>
    <div class="a39-webchat-provider-tabs" role="tablist" aria-label="Cloud Web Chat providers">
      ${providers.map(p=>{
        const n=a39WebTurns.filter(t=>t.provider_id===p.id&&t.status==="awaiting_input").length;
        return `<button type="button" role="tab" aria-selected="${p.id===a39WebProvider}" class="subtab ${p.id===a39WebProvider?"active":""}" data-a39-web-provider="${escape(p.id)}">${escape(p.name)}${n?` <span class="pill">${n}</span>`:""}</button>`
      }).join("")}
    </div>
    <div class="a39-webchat-layout">
      <section class="panel-card a39-webchat-provider-card">
        <div class="card-header"><div><div class="card-title">${escape(provider.name)} Web</div><div class="list-meta">Subscription-based conversation · manual use</div></div></div>
        <div class="widget-body">
          <p>Open this provider in your normal browser, sign in there, then paste a Council prompt. OnePane doesn't access your session or send requests on your behalf.</p>
          <div class="toolbar">
            ${provider.url?`<a class="btn primary" href="${escape(provider.url)}" target="_blank" rel="noopener noreferrer">Open ${escape(provider.name)}</a>`:""}
            <button class="btn" id="a39WebNewConversation" type="button">New Conversation</button>
          </div>
          <p class="list-meta">Some provider websites prevent embedding in other pages, so Web Chat uses an external browser tab. Your Council queue stays here in OnePane.</p>
          <div class="a39-webchat-queue">
            <h3>Council handoffs</h3>
            ${perProvider.length?perProvider.map(t=>`
              <button class="a39-webchat-turn ${selected?.turn_id===t.turn_id?"selected":""}" type="button" data-a39-web-turn="${escape(t.turn_id)}">
                <strong>${escape(t.model_label)} · ${escape(t.member_id)}</strong>
                <span class="list-meta">${escape(t.status==="awaiting_input"?"Awaiting response":"Submitted")} · Conversation ${Number(t.conversation_generation)||1}</span>
              </button>`).join(""):'<p class="empty-state compact">No Council handoffs for this provider. You can still use its website independently.</p>'}
          </div>
        </div>
      </section>
      <section class="panel-card a39-webchat-handoff-card">
        <div class="card-header"><div><div class="card-title">Council turn handoff</div>
          <div class="list-meta">${selected?escape(pendingText+" · "+selected.session_id):"No turn selected"}</div>
        </div></div>
        ${selected?`
          <div class="widget-body a39-webchat-handoff-content">
            <div class="a39-webchat-meta">Provider: <strong>${escape(provider.name)}</strong> · Model: <strong>${escape(selected.model_label)}</strong>
            · Conversation <strong>${Number(selected.conversation_generation)||1}</strong> · Identity: <strong>Operator attested</strong></div>
            <label class="a39-webchat-label">Prepared Council prompt</label>
            <textarea id="a39WebPrompt" class="a39-webchat-textarea" rows="11" readonly>${escape(prompt)}</textarea>
            <div class="toolbar">
              <button class="btn primary" id="a39WebCopyPrompt" type="button">Copy Prompt</button>
              ${provider.url?`<a class="btn" href="${escape(provider.url)}" target="_blank" rel="noopener noreferrer">Open Provider</a>`:""}
            </div>
            <label class="a39-webchat-label" for="a39WebResponse">Paste provider response</label>
            <textarea id="a39WebResponse" class="a39-webchat-textarea" rows="10" ${selected.status!=="awaiting_input"?"disabled":""} placeholder="Paste the complete cloud model response here. OnePane will store its provenance and pass it to the Research Council.">${escape(draft)}</textarea>
            <div class="toolbar">
              <button class="btn primary" id="a39WebSubmit" type="button" ${selected.status!=="awaiting_input"?"disabled":""}>Submit to Council</button>
              <span class="list-meta">No tools or task actions are granted to this seat.</span>
            </div>
          </div>`:'<div class="widget-body"><div class="empty-state compact">Start a Council with a manual Web seat to receive its prompt here.</div></div>'}
      </section>
    </div>`;
  $$("[data-a39-web-provider]").forEach(b=>b.onclick=()=>{
    a39WebProvider=b.dataset.a39WebProvider;
    a39WebTurnID="";
    renderWebChat();
  });
  $$("[data-a39-web-turn]").forEach(b=>b.onclick=()=>{
    a39WebTurnID=b.dataset.a39WebTurn;
    renderWebChat();
  });
  $("#a39WebNewConversation").onclick=()=>a39WebNewConversation(selected?.status==="awaiting_input"?selected:null);
  if(selected){
    $("#a39WebCopyPrompt").onclick=async()=>{
      try{await a39WebClipboard(prompt);notice("Council prompt copied to clipboard.")}
      catch(ex){notice(ex.message,"bad")}
    };
    const response=$("#a39WebResponse");
    if(response){
      response.oninput=()=>a39WebDrafts.set(selected.turn_id,response.value);
      $("#a39WebSubmit").onclick=()=>a39WebSubmit(selected,response.value);
    }
  }
}
