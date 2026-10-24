/* A single observed Team session, with conversation and member views.
 * Polls only authorized read APIs. No model call is caused by viewing,
 * switching views, reconnecting, or paging history.
 */
(function(root){
 "use strict";
 const field=(v,lower,upper)=>v?.[lower]??v?.[upper];
 const id=v=>String(field(v,"id","ID")||"");
 const text=v=>String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
 function content(m){let v=field(m,"content","Content");if(typeof v==="string"){try{v=JSON.parse(v)}catch{return v}}return typeof v?.text==="string"?v.text:"Structured contribution (inspect its durable record for details)."}
 function guidanceRecipient(m){if(field(m,"kind","Kind")!=="human")return "";let v=field(m,"content","Content");if(typeof v==="string"){try{v=JSON.parse(v)}catch{return ""}}return typeof v?.target_member_id==="string"?v.target_member_id:""}
 function controller({api,taskID,workspaceID,onChange=()=>{},alive=()=>true}){
  const state={sessionID:"",room:null,messages:[],history:null,hasOlder:false,view:"conversation",selectedAgent:"",guidanceTarget:"",draft:"",busy:false,error:"",liveAfter:"",proposedPlan:null};
  const abort=new AbortController();let reading=false,stopped=false,planDraftRevision=0;
  const active=()=>!stopped&&alive();const emit=()=>{if(active())onChange(state)};
  const request=(path,options={})=>api(path,{...options,signal:abort.signal});
  const path=suffix=>"/v1/team-sessions/"+encodeURIComponent(state.sessionID)+suffix;
  async function refresh(){
   if(reading||!active())return;reading=true;
   try{
    if(typeof workspaceID!=="string"||!workspaceID)throw Error("No Workspace scope");
    if(!state.sessionID){const ss=await request("/v1/tasks/"+encodeURIComponent(taskID)+"/team-session");if(!active())return;if(field(ss,"workspace_id","WorkspaceID")!==workspaceID||field(ss,"task_id","TaskID")!==taskID)throw Error("Foreign Team session");state.sessionID=id(ss);if(!state.sessionID)throw Error("No Team session")}
    const [room,page]=await Promise.all([request(path("/live")),request(path("/messages/page?limit=30")+(state.liveAfter?"&after="+encodeURIComponent(state.liveAfter):""))]);
    if(!active())return;
    if(room.id!==state.sessionID||room.task_id!==taskID||room.workspace_id!==workspaceID||!Array.isArray(room.members)||room.members.length>8||!Array.isArray(page.messages)||page.messages.length>50)throw Error("Invalid Team projection");
    if(room.assignments!==undefined&&(!Array.isArray(room.assignments)||room.assignments.length>8||room.assignments.some(a=>!room.members.some(m=>m.id===a.member_id))))throw Error("Invalid Team assignments");
    state.room=room;
    const merged=new Map(state.messages.map(m=>[id(m),m]));for(const m of page.messages){if(field(m,"session_id","SessionID")!==state.sessionID)throw Error("Foreign message");merged.set(id(m),m)}
    state.messages=Array.from(merged.values()).sort((a,b)=>Number(a.sequence||0)-Number(b.sequence||0)||(id(a)<id(b)?-1:id(a)>id(b)?1:0)).slice(-100);
    if(!state.liveAfter)state.hasOlder=!!page.has_more;
    if(page.last_id)state.liveAfter=page.last_id;
    state.error="";emit();
   }catch(e){if(active()){state.room=null;state.messages=[];state.history=null;state.liveAfter="";state.error="Live Team data unavailable. Check access or reconnect; no work has been repeated.";emit()}}
   finally{reading=false}
  }
  async function older(){
   if(state.busy||!state.room||!active())return;
   const before=state.history?.first_id||id(state.messages[0]);if(!before)return;
   state.busy=true;emit();
   try{const page=await request(path("/messages/page?limit=30&before=")+encodeURIComponent(before));if(!active())return;
    if(!Array.isArray(page.messages)||page.messages.length>50||page.messages.some(m=>field(m,"session_id","SessionID")!==state.sessionID))throw Error("Invalid history");state.history=page;state.error="";
   }catch{if(active()){state.history=null;state.room=null;state.messages=[];state.liveAfter="";state.error="History unavailable. Access will be checked again before reconnecting."}}
   finally{state.busy=false;emit()}
  }
  async function mutate(suffix,body){
   const allowed=["deliberating","plan_proposed","paused_for_deliberation","team_review",...(suffix==="/messages"||suffix.startsWith("/execution/")?["plan_accepted","executing"]:[])];
   if(state.busy||!state.room||!allowed.includes(state.room.status)||!active())return false;
   state.busy=true;emit();try{await request(path(suffix),{method:"POST",body:JSON.stringify(body)});if(!active())return false;state.error="";return true}
   catch{if(active()){state.error="The action could not be confirmed. Refresh and inspect the room before submitting again."}return false}
   finally{state.busy=false;emit()}
  }
  return {state,refresh,older,
   async controlExecution(action,reason=""){
    const room=state.room;
    if(!["pause","resume"].includes(action)||!room||room.execution_mode!=="team"||!room.assignments?.length||!room.accepted_plan_id||!Number.isSafeInteger(room.task_revision)||room.task_revision<1||typeof reason!=="string"||reason.length>2048)return false;
    if(action==="pause"&&(room.execution_paused||!["ready","running"].includes(room.task_state)))return false;
    if(action==="resume"&&(!room.execution_paused||room.pause_draining||room.pause_reconciliation_required||room.task_state!=="paused"))return false;
    const ok=await mutate("/execution/"+action,{plan_id:room.accepted_plan_id,expected_task_revision:room.task_revision,reason:reason.trim()});await refresh();return ok;
   },
   async cancelExecution(reason=""){
    const room=state.room;
    if(!room||room.execution_mode!=="team"||!room.assignments?.length||!room.accepted_plan_id||!Number.isSafeInteger(room.task_revision)||room.task_revision<1||!["created","ready","running","waiting_dependency","waiting_approval","paused","blocked","completion_requested","verifying"].includes(room.task_state)||typeof reason!=="string"||reason.length>2048)return false;
    const ok=await mutate("/execution/cancel",{plan_id:room.accepted_plan_id,expected_task_revision:room.task_revision,reason:reason.trim()});await refresh();return ok;
   },
	assignmentDraft(messageID){
	 if(!state.room||state.room.task_state!=="created"||state.room.assignments?.length)return null;
	 const message=state.messages.find(m=>id(m)===messageID&&field(m,"kind","Kind")==="agent"&&state.room.members.some(member=>member.id===field(m,"author_member_id","AuthorMemberID")&&member.kind==="agent"));if(!message)return null;
	 let body=field(message,"content","Content");try{if(typeof body==="string")body=JSON.parse(body);if(body?.proposal_type!=="replan")return null;let proposal=body.proposal;if(typeof proposal==="string")proposal=JSON.parse(proposal);
	 const assignments=proposal?.assignments;if(!Array.isArray(assignments)||assignments.length<1||assignments.length>8||JSON.stringify(assignments).length>64000)return null;
	 if(assignments.some(a=>a.required_verification!==undefined&&!['','V0','V1','V2','V3','V4','V5'].includes(a.required_verification)||a.verification_spec!==undefined&&(!a.verification_spec||Array.isArray(a.verification_spec)||typeof a.verification_spec!=="object"||JSON.stringify(a.verification_spec).length>16384)))return null;
	 const seen=new Set(),ordered=[],remaining=assignments.map(a=>({key:a.key,member_id:a.member_id,objective:a.objective,depends_on:a.depends_on||[],...(a.required_verification!==undefined?{required_verification:a.required_verification}:{}),...(a.verification_spec!==undefined?{verification_spec:a.verification_spec}:{})}));
	 if(remaining.some(a=>typeof a.key!=="string"||!(/^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$/).test(a.key)||typeof a.objective!=="string"||!a.objective.trim()||a.objective.length>4096||!state.room.members.some(m=>m.id===a.member_id&&m.kind==="agent")||!Array.isArray(a.depends_on)||a.depends_on.length>7||new Set(a.depends_on).size!==a.depends_on.length||seen.has(a.key)||(seen.add(a.key),false)))return null;
	 while(remaining.length){const next=remaining.findIndex(a=>a.depends_on.every(key=>ordered.some(done=>done.key===key)));if(next<0)return null;ordered.push(remaining.splice(next,1)[0])}
	 return JSON.parse(JSON.stringify(ordered));
	 }catch{return null}
	},
   invalidatePlanReview(){planDraftRevision++;state.proposedPlan=null;emit()},
   async readAssignment(taskID){if(!state.room?.assignments?.some(a=>a.task_id===taskID)||!active())return null;try{const task=await request("/v1/tasks/"+encodeURIComponent(taskID));if(!active()||id(task)!==taskID||field(task,"workspace_id","WorkspaceID")!==state.room?.workspace_id)return null;return task}catch{return null}},
   async proposeAssignments(assignments){
    if(state.busy||!state.room||state.room.task_state!=="created"||!Array.isArray(assignments)||assignments.length<1||assignments.length>8||!active())return false;
    if(assignments.some(a=>!state.room.members.some(m=>m.id===a.member_id&&m.kind==="agent")))return false;
    const savedAssignments=JSON.parse(JSON.stringify(assignments));
    const reviewRevision=planDraftRevision;state.busy=true;state.proposedPlan=null;emit();
    try{const out=await request(path("/plans"),{method:"POST",body:JSON.stringify({plan:{assignments:savedAssignments}})});if(!active())return false;
     if(!id(out)||field(out,"session_id","SessionID")!==state.sessionID)throw Error("Invalid plan scope");
     if(reviewRevision!==planDraftRevision){state.error="The draft changed while saving. Save the updated plan for a new review.";return false}
     state.proposedPlan={id:id(out),assignments:savedAssignments};state.error="";return true;
    }catch{if(active())state.error="The plan could not be confirmed. Refresh and inspect saved plans before submitting again.";return false}
    finally{state.busy=false;emit();await refresh()}
   },
   async acceptAssignments(){const plan=state.proposedPlan;if(!plan||state.room?.task_state!=="created")return false;const ok=await mutate("/plans/"+encodeURIComponent(plan.id)+"/accept",{accept_open_risk:false,decision:{reviewed_named_assignments:true}});if(ok){state.proposedPlan=null;await refresh()}return ok},
   stop(){stopped=true;abort.abort();state.sessionID="";state.room=null;state.messages=[];state.history=null;state.hasOlder=false;state.liveAfter="";state.proposedPlan=null;state.draft="";state.selectedAgent="";state.guidanceTarget="";state.error="";state.busy=false},
   switchView(view){if(view!=="conversation"&&view!=="agents")return;state.view=view;emit()},
   live(){state.history=null;emit()},
   async send(){const original=state.draft,draft=original.trim(),target=state.guidanceTarget;if(!draft||draft.length>8000||target&&(state.room?.execution_mode!=="team"||!state.room.members.some(m=>m.id===target&&m.kind==="agent")))return false;const ok=await mutate("/messages",{content:{text:draft,...(target?{target_member_id:target}:{})}});if(ok){if(state.draft===original)state.draft="";emit();await refresh()}return ok},
   async ask(){const member=state.selectedAgent;if(member&&!state.room?.members.some(m=>m.id===member&&m.kind!=="human"))return false;const ok=await mutate("/rounds",{member_ids:member?[member]:[]});if(ok)await refresh();return ok}
  };
 }
 function mount(taskID){
  if(typeof openModal!=="function"||typeof apiRequest!=="function")return;
  const scope=onepaneWorkspace;
  openModal("Live Team",'<section class="team-room" id="onepaneTeamRoom"><p class="page-subtitle">One shared session. Watch contributions and observed turns, send guidance, then request replies. Tool execution still requires the accepted Task plan.</p><div class="toolbar" role="group" aria-label="Team view"><button class="btn" data-team-view="conversation" aria-pressed="true">Conversation</button><button class="btn" data-team-view="agents" aria-pressed="false">Agents</button><button class="btn" data-team-refresh>Refresh</button></div><p data-team-status role="status">Connecting to the Task’s Team…</p><div data-team-execution-controls hidden><button class="btn" type="button" data-team-pause>Pause Team</button><button class="btn" type="button" data-team-resume>Resume Team</button><p data-team-hold-status role="status"></p></div><details data-team-cancel-controls hidden><summary>Cancel Team work</summary><p>Request cancellation of this Task, its contributions and their descendant Tasks. Completed results remain available. This records a request; already-started external actions may still need reconciliation.</p><label>Reason (optional)<input data-team-cancel-reason maxlength="2048"></label><button class="btn" type="button" data-team-cancel>Request cancellation</button></details><div data-team-conversation><div class="toolbar"><button class="btn" data-team-older>Earlier messages</button><button class="btn" data-team-live hidden>Return to live</button></div><div class="team-room-feed" data-team-feed tabindex="0" aria-label="Team conversation"></div></div><div class="team-room-agents" data-team-agents hidden></div><form class="qa-form" data-team-form><label>Guidance recipient<select aria-label="Guidance recipient" data-team-guidance-target><option value="">Whole team</option></select></label><p class="list-meta">Addressed guidance stays visible in the shared room and reaches the selected agent on its next reasoning step. It does not change the accepted plan or grant permissions.</p><label>Message to the room<textarea rows="3" maxlength="8000" data-team-draft required></textarea></label><div class="toolbar"><button class="btn" type="submit" data-team-send>Send message</button><label>Request replies from<select aria-label="Request replies from" data-team-target><option value="">All agents</option></select></label><button class="btn primary" type="button" data-team-ask>Request replies</button></div><p class="list-meta">Sending saves your message. Request replies queues reasoning turns. Waiting does not mean that an agent or model is running.</p></form></section>');
  const el=document.getElementById("onepaneTeamRoom");if(!el)return;
  const get=s=>el.querySelector(s);let roster="",timer=null,watchTimer=null,planEditor=null;
  const alive=()=>el.isConnected&&scope===onepaneWorkspace;
  const c=controller({api:apiRequest,taskID,workspaceID:scope,alive,onChange:draw});
  function draw(s){
   if(!alive())return;
   get("[data-team-status]").textContent=s.error||(s.room?"Task: "+s.room.task_state+" · Team: "+s.room.status+" · Round "+s.room.round+" · observed "+new Date(s.room.observed_at).toLocaleTimeString():"Connecting…");
   const feed=get("[data-team-feed]"),top=feed.scrollTop,atEnd=feed.scrollHeight-feed.clientHeight-top<32;
   const members=s.room?.members||[],names=new Map(members.map(m=>[m.id,m.name]));
   const msgs=s.history?.messages||s.messages;
   feed.innerHTML=msgs.length?msgs.map(m=>'<article class="team-room-message"><strong>'+text(names.get(field(m,"author_member_id","AuthorMemberID"))||(field(m,"kind","Kind")==="human"?"You":"Team"))+'</strong><small>'+(field(m,"kind","Kind")==="system"?"System update · ":"")+'Round '+text(field(m,"round_number","RoundNumber"))+(guidanceRecipient(m)?' · Guidance for '+text(names.get(guidanceRecipient(m))||'session member')+' (shared)':'')+'</small><p>'+text(content(m))+'</p></article>').join(""):'<p class="list-meta">No messages in this window.</p>';
   feed.scrollTop=atEnd&&!s.history?feed.scrollHeight:top;
   get("[data-team-conversation]").hidden=s.view!=="conversation";get("[data-team-agents]").hidden=s.view!=="agents";
   el.querySelectorAll("[data-team-view]").forEach(b=>b.setAttribute("aria-pressed",String(b.dataset.teamView===s.view)));
   get("[data-team-agents]").innerHTML=members.map(m=>'<article class="panel-card"><h3>'+text(m.name)+'</h3><p>'+text(m.role)+' · Turn: '+text(m.status)+'</p><dl><dt>Model/runtime</dt><dd>'+text(m.model_id||"Not selected")+'</dd><dt>Shared compute</dt><dd>'+text(m.compute_state||"No compute admission observed")+'</dd><dt>Backend / placement</dt><dd>'+text([m.backend,m.compute_mode].filter(Boolean).join(" / ")||"Not observed")+'</dd><dt>Turn</dt><dd>'+text(m.turn_id||"Not queued")+'</dd><dt>Round</dt><dd>'+text(m.round)+'</dd></dl><p class="list-meta">Observed deliberation status. No tool execution is implied.</p></article>').join("");
   const key=JSON.stringify(members.map(m=>[m.id,m.name,m.kind]));if(key!==roster){roster=key;get("[data-team-target]").innerHTML='<option value="">All agents</option>'+members.filter(m=>m.kind!=="human").map(m=>'<option value="'+text(m.id)+'">'+text(m.name)+'</option>').join("");get("[data-team-target]").value=s.selectedAgent}
   get("[data-team-guidance-target]").innerHTML='<option value="">Whole team</option>'+members.filter(m=>m.kind==="agent").map(m=>'<option value="'+text(m.id)+'">'+text(m.name)+'</option>').join("");get("[data-team-guidance-target]").value=s.guidanceTarget;
   get("[data-team-draft]").value=s.draft;
   const enabled=!!s.room&&["deliberating","plan_proposed","paused_for_deliberation","team_review"].includes(s.room.status)&&!s.busy;
   const guidanceEnabled=!!s.room&&["deliberating","plan_proposed","paused_for_deliberation","team_review","plan_accepted","executing"].includes(s.room.status)&&!s.busy;
   get("[data-team-send]").disabled=!guidanceEnabled;get("[data-team-guidance-target]").disabled=!guidanceEnabled||s.room.execution_mode!=="team";get("[data-team-guidance-target]").parentElement.hidden=s.room?.execution_mode!=="team";get("[data-team-ask]").disabled=!enabled;
   get("[data-team-older]").disabled=s.busy||!(s.history?s.history.has_more:s.hasOlder||s.messages.length>=100);get("[data-team-live]").hidden=!s.history;
   const executionControls=!!s.room&&s.room.execution_mode==="team"&&s.room.assignments?.length&&s.room.accepted_plan_id;
   get("[data-team-execution-controls]").hidden=!executionControls;get("[data-team-pause]").disabled=!executionControls||s.busy||s.room.execution_paused||!["ready","running"].includes(s.room.task_state);get("[data-team-resume]").disabled=!executionControls||s.busy||!s.room.execution_paused||s.room.pause_draining||s.room.pause_reconciliation_required||s.room.task_state!=="paused";
   get("[data-team-hold-status]").textContent=s.room?.execution_paused?(s.room.pause_reconciliation_required?"Team execution requires reconciliation before resume. Inspect blocked assignments and their recovery evidence.":s.room.pause_draining?"Paused for new work; waiting for an in-flight reasoning call to finish before resume.":"Team execution paused. Resume continues the accepted plan and original Attempts."):"";
   const cancellable=!!s.room&&s.room.execution_mode==="team"&&s.room.assignments?.length&&s.room.accepted_plan_id&&["created","ready","running","waiting_dependency","waiting_approval","paused","blocked","completion_requested","verifying"].includes(s.room.task_state);
   get("[data-team-cancel-controls]").hidden=!cancellable;get("[data-team-cancel]").disabled=!cancellable||s.busy;
   planEditor?.draw(s);
  }
  el.querySelectorAll("[data-team-view]").forEach(b=>b.onclick=()=>c.switchView(b.dataset.teamView));
  get("[data-team-guidance-target]").onchange=e=>{c.state.guidanceTarget=e.target.value};
  get("[data-team-draft]").oninput=e=>{c.state.draft=e.target.value};get("[data-team-target]").onchange=e=>{c.state.selectedAgent=e.target.value};
  get("[data-team-pause]").onclick=()=>c.controlExecution("pause");get("[data-team-resume]").onclick=()=>c.controlExecution("resume");
  get("[data-team-cancel]").onclick=()=>c.cancelExecution(get("[data-team-cancel-reason]").value);
  get("[data-team-form]").onsubmit=e=>{e.preventDefault();c.send()};get("[data-team-ask]").onclick=()=>c.ask();get("[data-team-refresh]").onclick=()=>c.refresh();get("[data-team-older]").onclick=()=>c.older();get("[data-team-live]").onclick=()=>c.live();
  if(root.OnePaneTeamAssignments)planEditor=root.OnePaneTeamAssignments.mount(el,c);
  if(root.OnePaneTaskRecovery){
   const button=document.createElement("button"),panel=document.createElement("section");let recovery=null;
   button.type="button";button.className="btn";button.textContent="Recovery records";button.setAttribute("aria-expanded","false");
   panel.hidden=true;panel.setAttribute("aria-label","Team Task recovery records");el.append(panel);el.querySelector(".toolbar").append(button);
   button.onclick=()=>{if(!alive())return;const opening=panel.hidden;recovery?.stop();recovery=null;panel.hidden=!opening;button.setAttribute("aria-expanded",String(opening));if(opening)recovery=root.OnePaneTaskRecovery.mountInto(panel,taskID,scope)};
  }
  const observer=new MutationObserver(()=>{if(!alive())stop()});observer.observe(document.body,{childList:true,subtree:true});
  function stop(){clearTimeout(timer);clearTimeout(watchTimer);observer.disconnect();c.stop();el.textContent="Team view closed."}
  // Scope changes must clear the room even while an HTTP read is still waiting.
  function watchScope(){if(!alive()){stop();return}watchTimer=setTimeout(watchScope,1000)}
  async function tick(){if(!alive()){stop();return}if(!document.hidden)await c.refresh();if(alive())timer=setTimeout(tick,2000);else stop()}
  watchScope();tick();return c;
 }
 root.OnePaneTeamRoom={controller,mount,content,guidanceRecipient};
 if(typeof document!=="undefined"){
  // Later canonical Inspector renderers replace the compatibility overview.
  // Install this entry point after those renderers, where it is actually used.
  const overview=qa4InspectorOverview;
  qa4InspectorOverview=function(){const html=overview();return qa4Inspector.kind==="task"&&!html.includes("data-team-open-task=")?html+'<section class="spec-readable-section"><h3>Team collaboration</h3><p>Open this Task’s Team conversation and named assignments, when a Team session is available.</p><button class="btn" type="button" data-team-open-task="'+text(qa4Inspector.id)+'">Open live Team</button></section>':html};
  document.addEventListener("click",e=>{const b=e.target.closest?.("[data-team-open-task]");if(b){e.preventDefault();mount(b.dataset.teamOpenTask)}});
 }
})(globalThis);
