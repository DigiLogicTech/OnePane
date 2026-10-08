/* Start a Web-only Research Council from Web Chat without API inference.
 * Uses the canonical Team -> Task -> Council session interfaces, not a
 * separate unofficial model or browser transport. Setup retries are staged
 * to avoid duplicating already confirmed resources after a failed request.
 */
const A41_MAX_WEB_SEATS=8;
let a41WebCouncilLaunch=null;

function a41DefaultWebCouncilDraft(){
  return {name:"Web Research Council",objective:"",critique_rounds:2,synthesis_pass:true,chair_mode:"manual",chair_provider_id:"chatgpt",chair_model_label:"",chair_require_approval:true,synthesis_index:0,seats:[
    {provider_id:"chatgpt",model_label:"",role_name:"Independent researcher"},
    {provider_id:"claude",model_label:"",role_name:"Critical analyst"},
    {provider_id:"gemini",model_label:"",role_name:"Alternative researcher"}
  ]};
}
function a41ValidateWebCouncilDraft(source){
  if(!source||typeof source!=="object")throw Error("Council configuration is missing.");
  const name=String(source.name||"").trim();
  const objective=String(source.objective||"").trim();
  if(name.length<3||name.length>120)throw Error("Council name must contain 3–120 characters.");
  if(objective.length<5||objective.length>12000)throw Error("Provide a Council objective (5–12,000 characters).");
  const critique=Number(source.critique_rounds);
  if(!Number.isInteger(critique)||critique<1||critique>5)throw Error("Choose between 1 and 5 critique rounds.");
  if(!Array.isArray(source.seats)||source.seats.length<2||source.seats.length>A41_MAX_WEB_SEATS)
    throw Error("A Web-only Council requires between 2 and 8 manual seats.");
  const providers=new Set(A39_WEB_PROVIDERS.map(p=>p.id));
  const chair_mode=String(source.chair_mode||"none").trim();
  if(chair_mode!=="manual" && chair_mode!=="none")throw Error("This Web-only Council supports a manual Web Chat Chair or deterministic no-Chair mode. Local/API chairs require a hybrid Council configuration.");
  if(chair_mode==="manual"&&source.seats.length>A41_MAX_WEB_SEATS-1)
    throw Error("A manual AI Chair occupies one Team seat. Select 2–7 research participants.");
  const synthesis_index=Number(source.synthesis_index??0);
  if(!Number.isInteger(synthesis_index)||synthesis_index<0||synthesis_index>=source.seats.length)
    throw Error("Select a participating model for final synthesis.");
  const chair_provider_id=String(source.chair_provider_id||"").trim();
  const chair_model_label=String(source.chair_model_label||"").trim();
  if(chair_mode==="manual"&&(!A39_WEB_PROVIDERS.some(p=>p.id===chair_provider_id)
     ||!chair_model_label||chair_model_label.length>128||/[\r\n]/.test(chair_model_label)))
    throw Error("Select the Chair provider and enter its exact selected web model.");
  const seats=source.seats.map((raw,index)=>{
    const provider_id=String(raw.provider_id||"").trim().toLowerCase();
    const model_label=String(raw.model_label||"").trim();
    const role_name=String(raw.role_name||"").trim();
    if(!providers.has(provider_id))throw Error(`Seat ${index+1}: select a supported cloud provider.`);
    if(!model_label||model_label.length>128||/[\r\n]/.test(model_label))
      throw Error(`Seat ${index+1}: enter the exact selected web model name (up to 128 characters).`);
    if(role_name.length<3||role_name.length>120)
      throw Error(`Seat ${index+1}: enter a role (3–120 characters).`);
    return {provider_id,model_label,role_name};
  });
  return {name,objective,critique_rounds:critique,synthesis_pass:source.synthesis_pass!==false,chair_mode,chair_provider_id,chair_model_label,chair_require_approval:source.chair_require_approval!==false,synthesis_index,seats};
}
function a41WebCouncilResearch(draft,memberIDs=[]){
  return {research_mode:true,research:{
    pin_models:true,disable_model_substitution:true,same_model_retries:true,
    preserve_failed_seats:true,independent_first_pass:true,scoped_evidence:true,
    record_raw_outputs:true,full_provenance:true,require_all_seats:true,
    anonymized_cross_critique:true,critique_rounds:draft.critique_rounds,
    synthesis_pass:draft.synthesis_pass,
    synthesis_member_id:memberIDs[draft.synthesis_index]||"",
    chair_mode:draft.chair_mode,
    chair_member_id:draft.chair_mode==="manual"?(memberIDs[draft.seats.length]||""):"",
    chair_require_approval:draft.chair_mode==="manual"&&draft.chair_require_approval
  }};
}
function a41CouncilIdentifier(entry){
  return String(entry?.id||entry?.ID||"").trim();
}
function a41CouncilRevision(entry){
  return Number(entry?.revision??entry?.Revision??1);
}


// Exact session/member binding ensures that two tabs for the same provider
// do not accidentally consume each other's responses. A successful prior
// round remains visible until this member's next pending round is queued.
function a41AutoBindWebCouncilHandoffs(rows){
  let changed=false;
  for(const tab of a40WebTabs()){
    if(!tab.session_id||!tab.member_id)continue;
    const owned=rows.filter(t=>t.session_id===tab.session_id && t.member_id===tab.member_id
      && t.provider_id===tab.provider_id);
    const pending=owned.filter(t=>t.status==="awaiting_input").sort((a,b)=>
      (Number(b.created_at)||0)-(Number(a.created_at)||0));
    const current=owned.find(t=>t.turn_id===tab.turn_id);
    if(current?.status==="awaiting_input")continue; // Preserve active pending answer.
    if(!pending.length)continue;
    const next=pending[0];
    if(next.turn_id===tab.turn_id)continue;
    if(a40WebDrafts.get(tab.id)?.response?.trim())continue; // Do not discard draft.
    tab.turn_id=next.turn_id;
    tab.conversation_generation=Number(next.conversation_generation)||1;
    changed=true;
  }
  if(changed)persist();
  return changed;
}

// Poll only queue metadata and rerender if queue records change, never every
// interval tick: continuously rebuilding the DOM would erase text selection
// and interfere with an operator copying/pasting prompts or responses.
let a41QueueMonitorStarted=false;
let a41QueueMonitorBusy=false;
let a41QueueFingerprint="";
function a41WebQueueDigest(workspace,rows){
  return workspace+"|"+rows.map(t=>[
    t.turn_id,t.status,t.conversation_generation,t.submitted_at
  ].join(":")).sort().join("|");
}
function a41RememberWebQueue(workspace,rows){
  a41QueueFingerprint=a41WebQueueDigest(workspace,rows);
  a41MonitorWebQueue();
}
function a41MonitorWebQueue(){
  if(a41QueueMonitorStarted)return;
  a41QueueMonitorStarted=true;
  setInterval(async()=>{
    if(a41QueueMonitorBusy||document.hidden||currentTab()?.route!=="webchat"||!a40WebWorkspace())return;
    a41QueueMonitorBusy=true;
    try{
      const ws=a40WebWorkspace();
      const data=await apiRequest(`/v1/manual-web/turns?workspace_id=${encodeURIComponent(ws)}`);
      if(ws!==a40WebWorkspace()||currentTab()?.route!=="webchat")return;
      const rows=a31Array(data);
      const digest=a41WebQueueDigest(ws,rows);
      if(digest!==a41QueueFingerprint){
        a41QueueFingerprint=digest;
        renderWebChat();
      }
    }catch{
      // Transient errors leave the last good queue visible. Explicit Refresh
      // surfaces API failures in the current workspace.
    }finally{a41QueueMonitorBusy=false}
  },8000);
}

// Network execution is separately testable. State never includes API keys,
// cookies or subscription sessions; only exact resource IDs and form settings.
async function a41CreateWebOnlyCouncil(workspace,draft,existing=null,options={}){
  const normalized=a41ValidateWebCouncilDraft(draft);
  workspace=String(workspace||"").trim();
  if(!workspace)throw Error("Select a OnePane Workspace before launching the Council.");
  const send=options.request||apiRequest;
  const progress=options.progress||(()=>{});
  const state0=existing||{
    workspace_id:workspace,draft:normalized,team_id:"",team_revision:1,
    member_ids:[],configured:false,task_id:"",session_id:"",tabs_created:false
  };
  if(state0.workspace_id!==workspace)throw Error("Cannot resume this Council in another Workspace.");
  // Preserve the original launch manifest if the operator edited the form
  // after a partially successful request.
  if(existing && JSON.stringify(state0.draft)!==JSON.stringify(normalized))
    throw Error("A partial Council launch must be resumed with its original configuration.");
  // Save the launch checkpoint before awaiting any network operation. On
  // failure the caller can retry from this exact resource stage.
  a41WebCouncilLaunch=state0;
  if(!state0.team_id){
    progress("Creating dedicated Web Research Team…");
    const record=await send("/v1/teams",{method:"POST",body:JSON.stringify({
      WorkspaceID:workspace,Name:normalized.name,Purpose:"Manual Web-only Research Council (no API inference or tools)"
    })});
    state0.team_id=a41CouncilIdentifier(record);
    if(!state0.team_id)throw Error("Team creation did not return an identifier; inspect Teams before retrying.");
    state0.team_revision=a41CouncilRevision(record);
  }
  const allSeats=[...normalized.seats,...(normalized.chair_mode==="manual"?[{
    provider_id:normalized.chair_provider_id,
    model_label:normalized.chair_model_label,
    role_name:"Council Chair",
    council_chair_only:true
  }]:[])];
  for(let i=state0.member_ids.length;i<allSeats.length;i++){
    const seat=allSeats[i];
    progress(`Adding manual Web Council seat ${i+1} of ${allSeats.length}…`);
    const provider=A39_WEB_PROVIDERS.find(x=>x.id===seat.provider_id);
    const record=await send(`/v1/teams/${encodeURIComponent(state0.team_id)}/members`,{
      method:"POST",body:JSON.stringify({
        member_kind:"agent",
        display_name:`${provider.name} — ${seat.role_name}`,
        role_name:seat.role_name,
        capability_id:"inference.general",protocol_level:"L0",
        route_policy:{},
        config:{manual_web:{enabled:true,provider_id:seat.provider_id,model_label:seat.model_label},...(seat.council_chair_only?{council_chair_only:true}:{})},
        ordinal:i
      })
    });
    const memberID=a41CouncilIdentifier(record);
    if(!memberID)throw Error(`Seat ${i+1} was created but its identifier was not returned; inspect the Team before retrying.`);
    state0.member_ids.push(memberID);
  }
  if(!state0.configured){
    progress("Saving strict Research integrity and multi-round settings…");
    const record=await send(`/v1/teams/${encodeURIComponent(state0.team_id)}/configuration`,{
      method:"PATCH",body:JSON.stringify({
        expected_revision:state0.team_revision,
        configuration:a41WebCouncilResearch(normalized,state0.member_ids)
      })
    });
    state0.team_revision=a41CouncilRevision(record);
    state0.configured=true;
  }
  if(!state0.task_id){
    progress("Creating consultation-only Task (no task execution)…");
    const record=await send("/v1/tasks",{method:"POST",body:JSON.stringify({
      workspace_id:workspace,objective:normalized.objective,
      scheduling_class:"user_interactive",priority:0,
      completion:{type:"operator_review"}
    })});
    state0.task_id=a41CouncilIdentifier(record);
    if(!state0.task_id)throw Error("Task was created but its identifier was not returned; inspect Tasks before retrying.");
  }
  if(!state0.session_id){
    progress("Starting the Web-only Research Council session…");
    const record=await send(`/v1/tasks/${encodeURIComponent(state0.task_id)}/team-session`,{
      method:"POST",body:JSON.stringify({
        team_id:state0.team_id,execution_mode:"council",
        config:{origin:"webchat",manual_web_only:true,task_execution:false}
      })
    });
    state0.session_id=a41CouncilIdentifier(record);
    if(!state0.session_id)throw Error("Session was created but its identifier was not returned; inspect the Task before retrying.");
  }
  progress("Council launched. Preparing separate Web Chat seats…");
  return state0;
}
function a41ProvisionWebCouncilTabs(result){
  if(result.tabs_created)return 0;
  let created=0;
  for(let i=0;i<result.draft.seats.length;i++){
    const seat=result.draft.seats[i];
    const memberID=result.member_ids[i];
    const matching=a40WebTabs(result.workspace_id).find(s=>s.session_id===result.session_id && s.member_id===memberID);
    if(matching)continue;
    if(a40WebTabs(result.workspace_id).length>=A40_WEB_MAX_TABS)break;
    const provider=A39_WEB_PROVIDERS.find(p=>p.id===seat.provider_id);
    const s=a40WebNewSession(seat.provider_id,`${provider.name} · ${seat.role_name}`,true);
    if(!s)break;
    s.session_id=result.session_id;
    s.member_id=memberID;
    s.turn_id="";
    created++;
  }
  if(result.draft.chair_mode==="manual"){
    const idx=result.draft.seats.length,chairID=result.member_ids[idx];
    if(!a40WebTabs(result.workspace_id).some(t=>t.session_id===result.session_id&&t.member_id===chairID)
        &&a40WebTabs(result.workspace_id).length<A40_WEB_MAX_TABS){
      const s=a40WebNewSession(result.draft.chair_provider_id,"Council Chair",true);
      if(s){s.session_id=result.session_id;s.member_id=chairID;s.council_chair=true;s.chair_turn_id="";created++;}
    }
  }
  const everySeat=result.member_ids.every(memberID=>a40WebTabs(result.workspace_id)
      .some(t=>t.session_id===result.session_id&&t.member_id===memberID));
  if(everySeat)result.tabs_created=true;
  persist();
  return created;
}
function a41WebSeatRow(seat){
  return `<div class="a41-council-seat" data-a41-seat>
    <div class="a41-council-seat-head"><strong>Web provider seat</strong><button type="button" class="btn" data-a41-remove-seat title="Remove this seat">Remove</button></div>
    <div class="a41-council-seat-fields">
      <label>Provider<select name="provider_id" required>
        ${A39_WEB_PROVIDERS.map(p=>`<option value="${escapeHtml(p.id)}" ${seat.provider_id===p.id?"selected":""}>${escapeHtml(p.name)}</option>`).join("")}
      </select></label>
      <label>Selected web model<input name="model_label" required maxlength="128" value="${escapeHtml(seat.model_label||"")}" placeholder="e.g. the model selected in your browser"></label>
      <label>Role<input name="role_name" required maxlength="120" value="${escapeHtml(seat.role_name||"Independent researcher")}"></label>
    </div>
  </div>`;
}
function a41CouncilWizardSnapshot(form){
  return {
    name:form.elements.namedItem("name").value,
    objective:form.elements.namedItem("objective").value,
    critique_rounds:Number(form.elements.namedItem("critique_rounds").value),
    synthesis_pass:form.elements.namedItem("synthesis_pass").checked,
    synthesis_index:Number(form.elements.namedItem("synthesis_index").value),
    chair_mode:form.elements.namedItem("chair_mode").value,
    chair_provider_id:form.elements.namedItem("chair_provider_id").value,
    chair_model_label:form.elements.namedItem("chair_model_label").value,
    chair_require_approval:form.elements.namedItem("chair_require_approval").checked,
    seats:$$("[data-a41-seat]",form).map(row=>({
      provider_id:row.querySelector('[name="provider_id"]').value,
      model_label:row.querySelector('[name="model_label"]').value,
      role_name:row.querySelector('[name="role_name"]').value
    }))
  };
}
function a41OpenWebOnlyCouncilWizard(){
  const ws=a40WebWorkspace();
  if(!ws)return notice("Select a Workspace before starting a Web Council.","bad");
  const draft=a41DefaultWebCouncilDraft();
  openModal("Start Web-only Council",`
    <form id="a41CouncilWizard" class="qa-form a41-council-form">
      <p class="page-subtitle">OnePane creates an isolated, manual Research Council. You submit each prompt using your existing cloud-provider web subscription and paste the answers back. No cloud API inference or task execution.</p>
      <label>Council name<input name="name" maxlength="120" value="${escapeHtml(draft.name)}" required></label>
      <label>Research objective<textarea name="objective" required minlength="5" maxlength="12000" rows="4" placeholder="What should the Council investigate, assess or decide?"></textarea></label>
      <div class="a41-council-settings">
        <label>Cross-critique rounds<select name="critique_rounds">
          ${[1,2,3,4,5].map(n=>`<option value="${n}" ${n===2?"selected":""}>${n}</option>`).join("")}
        </select></label>
        <label class="inline-check"><input type="checkbox" name="synthesis_pass" checked> Include final synthesis</label>
      </div>
      <div class="a41-council-seats-title"><strong>Council leadership</strong><span class="list-meta">Chair advises; OnePane enforces approval and isolation</span></div>
      <div class="a41-council-settings">
        <label>Chair mode<select name="chair_mode" id="a41ChairMode">
          <option value="manual" selected>Manual Web Chat AI Chair</option>
          <option value="none">No AI Chair — structured workflow</option>
          <option value="local" disabled>OnePane local model — hybrid mode (not yet integrated)</option>
          <option value="api" disabled>Connected cloud API — hybrid mode (not yet integrated)</option>
        </select></label>
        <label>Final synthesis seat<select name="synthesis_index" id="a41SynthesisSeat"></select></label>
      </div>
      <div class="a41-council-settings" id="a41ChairFields">
        <label>Chair Web provider<select name="chair_provider_id">
          ${A39_WEB_PROVIDERS.map(p=>`<option value="${escapeHtml(p.id)}">${escapeHtml(p.name)}</option>`).join("")}
        </select></label>
        <label>Chair selected model<input name="chair_model_label" required maxlength="128" placeholder="Exact model selected on the provider website"></label>
      </div>
      <label class="inline-check" id="a41ChairApproval"><input type="checkbox" name="chair_require_approval" checked> Require my approval for the Chair agenda and follow-up questions</label>
      <div class="a41-council-seats-title"><strong>Manual web consultation seats (2–7 with Chair, 2–8 without)</strong><button class="btn" id="a41AddSeat" type="button">+ Add provider seat</button></div>
      <div class="a41-council-seats" id="a41CouncilSeats">${draft.seats.map(a41WebSeatRow).join("")}</div>
      <p class="list-meta">You can choose the same provider more than once. Specify the model you will select on each website. All first-round seats remain independent, with no automatic model substitutions.</p>
      <div class="error" id="a41CouncilError" role="alert"></div>
      <div class="list-meta" id="a41CouncilProgress" role="status"></div>
      <div class="toolbar"><button class="btn primary" type="submit" id="a41LaunchCouncil">Start Council</button></div>
    </form>`);
  const form=$("#a41CouncilWizard");
  if(!form)return;
  const bindRemove=()=>{
    $$("[data-a41-remove-seat]",form).forEach(b=>b.onclick=()=>{
      const rows=$$("[data-a41-seat]",form);
      if(rows.length<=2)return notice("Keep at least two Council seats.","bad");
      b.closest("[data-a41-seat]").remove();
      syncChair();syncSynthesis();
    });
  };
  bindRemove();
  const syncChair=()=>{
    const manual=$("#a41ChairMode").value==="manual";
    $("#a41ChairFields").hidden=!manual;
    $("#a41ChairApproval").hidden=!manual;
    form.elements.namedItem("chair_model_label").required=manual;
    const count=$("[data-a41-seat]",form).length;
    $("#a41AddSeat").disabled=count>=A41_MAX_WEB_SEATS-(manual?1:0);
  };
  const syncSynthesis=()=>{
    const control=$("#a41SynthesisSeat");
    const selected=Number(control.value);
    control.innerHTML=$("[data-a41-seat]",form).map((el,i)=>{
      const role=el.querySelector('[name="role_name"]')?.value||("Research seat "+(i+1));
      const provider=el.querySelector('[name="provider_id"]')?.value||"";
      return `<option value="${i}">${escapeHtml(provider+" · "+role)}</option>`;
    }).join("");
    control.value=String(selected>=0&&selected<control.options.length?selected:0);
  };
  $("#a41ChairMode").onchange=()=>{syncChair();syncSynthesis()};
  $("#a41CouncilSeats").addEventListener("change",syncSynthesis);
  syncChair();syncSynthesis();
  $("#a41AddSeat").onclick=()=>{
    const rows=$$("[data-a41-seat]",form);
    if(rows.length>=A41_MAX_WEB_SEATS-($("#a41ChairMode").value==="manual"?1:0))return;
    const host=$("#a41CouncilSeats");
    host.insertAdjacentHTML("beforeend",a41WebSeatRow({
      provider_id:"chatgpt",model_label:"",role_name:"Independent reviewer"
    }));
    bindRemove();
    syncChair();syncSynthesis();
  };
  form.onsubmit=async e=>{
    e.preventDefault();
    const error=$("#a41CouncilError"),status=$("#a41CouncilProgress"),button=$("#a41LaunchCouncil");
    error.textContent="";
    let normalized;
    try{normalized=a41ValidateWebCouncilDraft(a41CouncilWizardSnapshot(form))}
    catch(ex){error.textContent=ex.message;return}
    if(a41WebCouncilLaunch &&
       a41WebCouncilLaunch.workspace_id===ws && !a41WebCouncilLaunch.session_id &&
       JSON.stringify(a41WebCouncilLaunch.draft)!==JSON.stringify(normalized)){
      error.textContent="An earlier launch partially completed. Keep its settings unchanged and press Resume Council launch, or reload and inspect the partially created resources.";
      return;
    }
    button.disabled=true;
    const progress=text=>{if(status)status.textContent=text};
    try{
      a41WebCouncilLaunch=await a41CreateWebOnlyCouncil(ws,normalized,a41WebCouncilLaunch,{progress});
      const count=a41ProvisionWebCouncilTabs(a41WebCouncilLaunch);
      const result=a41WebCouncilLaunch;
      progress(`Launched Council ${result.session_id}. Opening ${count} Web Chat tabs.`);
      a41WebCouncilLaunch=null;
      closeModal();
      notice("Web-only Research Council started. Manual seat prompts will appear in the queue.");
      await renderWebChat();
    }catch(ex){
      const partial=a41WebCouncilLaunch;
      error.textContent=`Council setup interrupted: ${ex.message}. ${partial?.team_id?`Team ${partial.team_id}. `:""}${partial?.task_id?`Task ${partial.task_id}. `:""}Retry resumes confirmed steps, not a fresh Council.`;
      button.textContent="Resume Council launch";
      button.disabled=false;
    }
  };
}
