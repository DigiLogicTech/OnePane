/* Request-specific human input. Saving a reply never approves execution. */
(function(root){
 "use strict";
 const bytes=s=>{try{return encodeURIComponent(s).replace(/%[0-9a-f]{2}/gi,"x").length}catch{return Infinity}};
 const identifier=s=>typeof s==="string"&&s.trim().length>0&&bytes(s)<=256&&!/[\u0000-\u001f\u007f]/.test(s);
 const positive=n=>Number.isSafeInteger(n)&&n>0;
 const roomIdentity=r=>r?JSON.stringify([r.id,r.workspace_id,r.task_id,r.accepted_plan_id]):"";
 const roomBinding=r=>r?JSON.stringify([roomIdentity(r),r.assignments.map(a=>JSON.stringify([a.task_id,a.key,a.member_id])).sort(),r.members.map(m=>JSON.stringify([m.id,m.kind])).sort()]):"";
 const identity=r=>JSON.stringify([r.request_id,r.workspace_id,r.session_id,r.plan_id,r.parent_task_id,r.assignment_key,r.member_id,r.task_id,r.attempt_id,r.run_id,r.wait_step_id,r.notice_message_id,r.author_principal_id,r.question,r.project_id||"",r.project_workspace_id||"",r.artifact_session_id||""]);
 function validRequest(r,room){
  if(!r||typeof r!=="object"||Array.isArray(r)||Object.keys(r).length>22||!room)return false;
  const fields=["request_id","workspace_id","session_id","plan_id","parent_task_id","assignment_key","member_id","task_id","attempt_id","run_id","wait_step_id","notice_message_id","author_principal_id"];
  const optional=["project_id","project_workspace_id","artifact_session_id"];
  const allowed=new Set([...fields,...optional,"member_name","question","task_state","task_revision","worker_revision","created_at"]);
  if(Object.keys(r).some(k=>!allowed.has(k))||fields.some(k=>!identifier(r[k]))||optional.some(k=>r[k]!==undefined&&r[k]!==""&&!identifier(r[k])))return false;
  const a=room.assignments?.find(a=>a.task_id===r.task_id);
  return r.workspace_id===room.workspace_id&&r.session_id===room.id&&r.parent_task_id===room.task_id&&r.plan_id===room.accepted_plan_id&&!!a&&a.key===r.assignment_key&&a.member_id===r.member_id&&
   room.members?.some(m=>m.id===r.member_id&&m.kind==="agent")&&r.author_principal_id==="system:agent-worker"&&identifier(r.member_name)&&
   typeof r.question==="string"&&!!r.question.trim()&&bytes(r.question)<=512&&(!r.project_workspace_id||!!r.project_id)&&
   ["waiting_approval","paused"].includes(r.task_state)&&positive(r.task_revision)&&positive(r.worker_revision)&&positive(r.created_at);
 }
 function validAnswer(a,r){
  return a&&typeof a==="object"&&!Array.isArray(a)&&Object.keys(a).length===8&&identifier(a.answer_id)&&a.request_id===r.request_id&&identifier(a.author_principal_id)&&
   typeof a.idempotency_key==="string"&&!!a.idempotency_key.trim()&&bytes(a.idempotency_key)<=128&&typeof a.text==="string"&&!!a.text.trim()&&bytes(a.text)<=4096&&
   positive(a.task_revision)&&a.task_revision<=r.task_revision&&positive(a.worker_revision)&&a.worker_revision<=r.worker_revision&&positive(a.created_at);
 }
 const owner=r=>({task_id:r.task_id,attempt_id:r.attempt_id,run_id:r.run_id,wait_step_id:r.wait_step_id,expected_task_revision:r.task_revision,expected_worker_revision:r.worker_revision});
 const older=(incoming,current)=>incoming.task_revision<current.task_revision||incoming.worker_revision<current.worker_revision||(incoming.task_revision===current.task_revision&&incoming.task_state!==current.task_state);
 function controller({api,taskID,workspaceID,alive=()=>true,onChange=()=>{}}){
  const state={room:null,requests:[],error:"",busy:false};
  const abort=new AbortController();let stopped=false,epoch=0,reading=null,writing=null;
  const active=()=>!stopped&&alive();
  const emit=()=>{if(active())onChange(state)};
  const clear=()=>{epoch++;reading=null;writing=null;state.requests=[];state.busy=false};
  const path=(r,suffix="")=>"/v1/team-sessions/"+encodeURIComponent(state.room.id)+"/assistance"+(r?"/"+encodeURIComponent(r.request_id):"")+suffix;
  const request=(url,opts={})=>api(url,{...opts,signal:abort.signal});
  const current=(entry,generation)=>active()&&epoch===generation&&state.requests.includes(entry);
  function updateRoom(room){
   const valid=room&&room.workspace_id===workspaceID&&room.task_id===taskID&&identifier(room.id)&&identifier(room.accepted_plan_id)&&room.execution_mode==="team"&&
    ["plan_accepted","executing"].includes(room.status)&&Array.isArray(room.assignments)&&room.assignments.length<=8&&Array.isArray(room.members)&&room.members.length<=8;
   if(!valid){clear();state.room=null;state.error="";emit();return}
   if(roomBinding(room)!==roomBinding(state.room)){clear();state.error=""}
   state.room={...room,assignments:room.assignments.map(a=>({...a})),members:room.members.map(m=>({...m}))};
  }
  async function refresh(){
   if(!active()||!state.room||reading)return false;
   const ticket={},generation=epoch,room=state.room;reading=ticket;
   try{
    const value=await request(path(null));
    if(!active()||epoch!==generation||reading!==ticket)return false;
    if(!value||Object.keys(value).length!==1||!Array.isArray(value.requests)||value.requests.length>8||value.requests.some(r=>!validRequest(r,room))||bytes(JSON.stringify(value))>131072)throw Error("Invalid request list");
    const seen=new Set();for(const r of value.requests){if(seen.has(r.request_id)||seen.has("task:"+r.task_id))throw Error("Duplicate request");seen.add(r.request_id);seen.add("task:"+r.task_id)}
    for(const r of value.requests){const old=state.requests.find(e=>identity(e.request)===identity(r));if(old&&older(r,old.request)){state.error="Input requests changed during refresh. Refresh again for the current state.";emit();return false}}
    state.requests=value.requests.map(r=>{
     const old=state.requests.find(e=>identity(e.request)===identity(r));
     return old?Object.assign(old,{request:{...r}}):{request:{...r},draft:"",answer:null,error:"",pending:false,submission:null};
    });
    state.error="";emit();return true;
   }catch{
    if(active()&&epoch===generation&&reading===ticket){clear();state.error="Input requests unavailable. Reconnect to check access and current work.";emit()}
    return false;
   }finally{if(reading===ticket)reading=null}
  }
  function setDraft(requestID,text){
   const entry=state.requests.find(e=>e.request.request_id===requestID);
   if(!active()||!entry||entry.answer||typeof text!=="string"||bytes(text)>4096)return false;
   entry.draft=text;return true;
  }
  async function action(requestID,kind){
   const entry=state.requests.find(e=>e.request.request_id===requestID);
   if(!active()||!entry||writing||!state.room)return false;
   const r=entry.request,generation=epoch;let body,url;
   if(!validRequest(r,state.room))return false;
   if(kind==="save"){
    if(entry.answer||!entry.draft.trim()||bytes(entry.draft)>4096)return false;
    if(!entry.submission||entry.submission.text!==entry.draft||entry.submission.expected_task_revision!==r.task_revision||entry.submission.expected_worker_revision!==r.worker_revision){
     if(typeof root.crypto?.randomUUID!=="function"){entry.error="A secure submission identity is unavailable. Reopen this view.";emit();return false}
     entry.submission={...owner(r),text:entry.draft,idempotency_key:root.crypto.randomUUID()};
    }
    body=entry.submission;url=path(r,"/answers");
   }else if(kind==="continue"){
    if(!entry.answer||!validAnswer(entry.answer,r)||r.task_state!=="waiting_approval"||state.room.execution_paused||state.room.pause_draining||state.room.pause_reconciliation_required)return false;
    body={...owner(r),answer_id:entry.answer.answer_id};url=path(r,"/continue");
   }else url=path(r,"/answer");
   const ticket={};writing=ticket;state.busy=true;entry.pending=true;entry.error="";emit();
   try{
    const result=await request(url,body?{method:"POST",body:JSON.stringify(body)}:{});
    if(!current(entry,generation)||writing!==ticket)return false;
    if(kind==="load"){
     if(!result||Object.keys(result).length!==2||!validRequest(result.request,state.room)||identity(result.request)!==identity(r)||!validAnswer(result.answer,result.request))throw Error("Invalid saved reply");
     if(older(result.request,entry.request)){
      entry.error="The Task changed while loading this reply. Check for the saved reply again.";emit();return false;
     }
     entry.answer={...result.answer};entry.request={...result.request};
     epoch++;reading=null;
    }else if(kind==="save"){
     if(!validAnswer(result,r)||result.text!==body.text||result.idempotency_key!==body.idempotency_key)throw Error("Invalid reply receipt");
     entry.answer={...result};if(entry.draft===body.text)entry.draft="";
    }else{
     if(!result||!identifier(result.continuation_id)||result.request_id!==r.request_id||result.answer_id!==entry.answer.answer_id||!identifier(result.continue_step_id)||!identifier(result.actor_principal_id)||result.task_revision!==r.task_revision||result.worker_revision!==r.worker_revision||!positive(result.created_at))throw Error("Invalid continuation receipt");
     epoch++;reading=null;
     state.requests=state.requests.filter(e=>e!==entry);
    }
    emit();return true;
   }catch(error){
    if(current(entry,generation)&&writing===ticket){
     if(kind==="load"&&error?.status===404)entry.error="No saved reply yet. You can save your reply below.";
     else if(kind==="load"){clear();state.error="Saved reply unavailable. Refresh input requests to check current access."}
     else entry.error="The action could not be confirmed. Check for a saved reply or refresh before trying again.";
     emit();
    }
    return false;
   }finally{if(writing===ticket){writing=null;state.busy=false;entry.pending=false;emit()}}
  }
  return {state,updateRoom,refresh,setDraft,save:id=>action(id,"save"),loadAnswer:id=>action(id,"load"),continueRequest:id=>action(id,"continue"),stop(){clear();state.room=null;state.error="";stopped=true;abort.abort()}};
 }
 function mount(host,team,{api,taskID,workspaceID,alive}){
  const section=document.createElement("section");section.className="team-assistance-section";section.setAttribute("aria-label","Team input requests");host.append(section);
  const c=controller({api,taskID,workspaceID,alive,onChange:render});let disposed=false;
  function node(tag,text,className){const el=document.createElement(tag);if(text!==undefined)el.textContent=text;if(className)el.className=className;return el}
  function render(s){
   if(disposed)return;
   // Keep textareas and focus stable during ordinary polls. Drafts stay in
   // controller state when a changed revision requires rebuilding the cards.
   const signature=JSON.stringify([roomIdentity(s.room),s.room?.execution_paused,s.room?.pause_draining,s.room?.pause_reconciliation_required,s.error,s.busy,s.requests.map(e=>[e.request,e.answer,e.error,e.pending])]);
   if(section.dataset.signature===signature)return;section.dataset.signature=signature;
   const focused=section.contains(document.activeElement)?document.activeElement:null;
   const focusID=focused?.dataset.helpDraft,selection=focused?[focused.selectionStart,focused.selectionEnd]:null;
   section.replaceChildren();section.hidden=!s.room&&!s.error;
   section.append(node("h3","Needs your input"));
   if(s.error){const error=node("p",s.error);error.setAttribute("role","status");section.append(error)}
   if(s.room&&!s.requests.length&&!s.error)section.append(node("p","No current input requests.","list-meta"));
   for(const entry of s.requests){
    const r=entry.request,card=node("article",undefined,"panel-card");card.append(node("h4",r.member_name+" · "+r.assignment_key),node("p",r.question));
    if(entry.answer){
     card.append(node("p","Saved human reply","list-meta"),node("pre",entry.answer.text,"team-contribution-result"));
     const button=node("button","Continue original task","btn primary");button.type="button";button.disabled=s.busy||r.task_state!=="waiting_approval"||!!s.room.execution_paused||!!s.room.pause_draining||!!s.room.pause_reconciliation_required;button.onclick=()=>c.continueRequest(r.request_id);card.append(button);
     card.append(node("p","Continue uses this reply under the Task’s current execution controls. Active pauses and recovery holds require separate resolution.","list-meta"));
    }else{
     const label=node("label","Your reply"),input=node("textarea");input.rows=3;input.maxLength=4096;input.value=entry.draft;input.dataset.helpDraft=r.request_id;input.disabled=s.busy;
     input.oninput=()=>{if(!c.setDraft(r.request_id,input.value))input.setCustomValidity("Use at most 4096 UTF-8 bytes.");else input.setCustomValidity("")};label.append(input);card.append(label);
     const save=node("button","Save reply","btn primary");save.type="button";save.disabled=s.busy;save.onclick=()=>{if(input.reportValidity())c.save(r.request_id)};
     const load=node("button","Check for saved reply","btn");load.type="button";load.disabled=s.busy;load.onclick=()=>c.loadAnswer(r.request_id);card.append(save,load,node("p","Saving records one final reply. Continue is a separate action.","list-meta"));
    }
    if(entry.error){const error=node("p",entry.error);error.setAttribute("role","status");card.append(error)}
    section.append(card);
   }
   const refresh=node("button","Refresh input requests","btn");refresh.type="button";refresh.disabled=s.busy;refresh.onclick=()=>c.refresh();section.append(refresh);
   if(focusID){const input=Array.from(section.querySelectorAll("textarea")).find(e=>e.dataset.helpDraft===focusID);if(input&&!input.disabled){input.focus();if(selection)input.setSelectionRange(...selection)}}
  }
  return {draw(s){if(disposed)return;c.updateRoom(s.room);if(!s.room)render(c.state);else c.refresh()},dispose(){disposed=true;c.stop();section.replaceChildren();section.remove()}};
 }
 root.OnePaneTeamAssistance={controller,mount,validRequest,validAnswer};
})(globalThis);
