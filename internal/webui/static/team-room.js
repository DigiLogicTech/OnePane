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
 function controller({api,taskID,onChange=()=>{},alive=()=>true}){
  const state={sessionID:"",room:null,messages:[],history:null,hasOlder:false,view:"conversation",selectedAgent:"",draft:"",busy:false,error:"",liveAfter:""};
  const abort=new AbortController();let reading=false,stopped=false;
  const active=()=>!stopped&&alive();const emit=()=>{if(active())onChange(state)};
  const request=(path,options={})=>api(path,{...options,signal:abort.signal});
  const path=suffix=>"/v1/team-sessions/"+encodeURIComponent(state.sessionID)+suffix;
  async function refresh(){
   if(reading||!active())return;reading=true;
   try{
    if(!state.sessionID){const ss=await request("/v1/tasks/"+encodeURIComponent(taskID)+"/team-session");if(!active())return;state.sessionID=id(ss);if(!state.sessionID)throw Error("No Team session")}
    const [room,page]=await Promise.all([request(path("/live")),request(path("/messages/page?limit=30")+(state.liveAfter?"&after="+encodeURIComponent(state.liveAfter):""))]);
    if(!active())return;
    if(room.id!==state.sessionID||room.task_id!==taskID||!Array.isArray(room.members)||room.members.length>8||!Array.isArray(page.messages)||page.messages.length>50)throw Error("Invalid Team projection");
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
   if(state.busy||!state.room||!["deliberating","plan_proposed","paused_for_deliberation","team_review"].includes(state.room.status)||!active())return false;
   state.busy=true;emit();try{await request(path(suffix),{method:"POST",body:JSON.stringify(body)});if(!active())return false;state.error="";return true}
   catch{if(active()){state.error="The action could not be confirmed. Refresh and inspect the room before submitting again."}return false}
   finally{state.busy=false;emit()}
  }
  return {state,refresh,older,
   stop(){stopped=true;abort.abort()},
   switchView(view){if(view!=="conversation"&&view!=="agents")return;state.view=view;emit()},
   live(){state.history=null;emit()},
   async send(){const original=state.draft,draft=original.trim();if(!draft||draft.length>8000)return false;const ok=await mutate("/messages",{content:{text:draft}});if(ok){if(state.draft===original)state.draft="";emit();await refresh()}return ok},
   async ask(){const member=state.selectedAgent;if(member&&!state.room?.members.some(m=>m.id===member&&m.kind!=="human"))return false;const ok=await mutate("/rounds",{member_ids:member?[member]:[]});if(ok)await refresh();return ok}
  };
 }
 function mount(taskID){
  if(typeof openModal!=="function"||typeof apiRequest!=="function")return;
  const scope=onepaneWorkspace;
  openModal("Live Team",'<section class="team-room" id="onepaneTeamRoom"><p class="page-subtitle">One shared session. Watch contributions and observed turns, send guidance, then request replies. Tool execution still requires the accepted Task plan.</p><div class="toolbar" role="group" aria-label="Team view"><button class="btn" data-team-view="conversation" aria-pressed="true">Conversation</button><button class="btn" data-team-view="agents" aria-pressed="false">Agents</button><button class="btn" data-team-refresh>Refresh</button></div><p data-team-status role="status">Connecting to the Task’s Team…</p><div data-team-conversation><div class="toolbar"><button class="btn" data-team-older>Earlier messages</button><button class="btn" data-team-live hidden>Return to live</button></div><div class="team-room-feed" data-team-feed tabindex="0" aria-label="Team conversation"></div></div><div class="team-room-agents" data-team-agents hidden></div><form class="qa-form" data-team-form><label>Message to the room<textarea rows="3" maxlength="8000" data-team-draft required></textarea></label><div class="toolbar"><button class="btn" type="submit" data-team-send>Send message</button><label>Request replies from<select aria-label="Request replies from" data-team-target><option value="">All agents</option></select></label><button class="btn primary" type="button" data-team-ask>Request replies</button></div><p class="list-meta">Sending saves your message. Request replies queues reasoning turns. Waiting does not mean that an agent or model is running.</p></form></section>');
  const el=document.getElementById("onepaneTeamRoom");if(!el)return;
  const get=s=>el.querySelector(s);let roster="",timer=null;
  const alive=()=>el.isConnected&&scope===onepaneWorkspace;
  const c=controller({api:apiRequest,taskID,alive,onChange:draw});
  function draw(s){
   if(!alive())return;
   get("[data-team-status]").textContent=s.error||(s.room?"Task: "+s.room.task_state+" · Team: "+s.room.status+" · Round "+s.room.round+" · observed "+new Date(s.room.observed_at).toLocaleTimeString():"Connecting…");
   const feed=get("[data-team-feed]"),top=feed.scrollTop,atEnd=feed.scrollHeight-feed.clientHeight-top<32;
   const members=s.room?.members||[],names=new Map(members.map(m=>[m.id,m.name]));
   const msgs=s.history?.messages||s.messages;
   feed.innerHTML=msgs.length?msgs.map(m=>'<article class="team-room-message"><strong>'+text(names.get(field(m,"author_member_id","AuthorMemberID"))||(field(m,"kind","Kind")==="human"?"You":"Team"))+'</strong><small>Round '+text(field(m,"round_number","RoundNumber"))+'</small><p>'+text(content(m))+'</p></article>').join(""):'<p class="list-meta">No messages in this window.</p>';
   feed.scrollTop=atEnd&&!s.history?feed.scrollHeight:top;
   get("[data-team-conversation]").hidden=s.view!=="conversation";get("[data-team-agents]").hidden=s.view!=="agents";
   el.querySelectorAll("[data-team-view]").forEach(b=>b.setAttribute("aria-pressed",String(b.dataset.teamView===s.view)));
   get("[data-team-agents]").innerHTML=members.map(m=>'<article class="panel-card"><h3>'+text(m.name)+'</h3><p>'+text(m.role)+' · Turn: '+text(m.status)+'</p><dl><dt>Model/runtime</dt><dd>'+text(m.model_id||"Not selected")+'</dd><dt>Shared compute</dt><dd>'+text(m.compute_state||"No compute admission observed")+'</dd><dt>Backend / placement</dt><dd>'+text([m.backend,m.compute_mode].filter(Boolean).join(" / ")||"Not observed")+'</dd><dt>Turn</dt><dd>'+text(m.turn_id||"Not queued")+'</dd><dt>Round</dt><dd>'+text(m.round)+'</dd></dl><p class="list-meta">Observed deliberation status. No tool execution is implied.</p></article>').join("");
   const key=JSON.stringify(members.map(m=>[m.id,m.name,m.kind]));if(key!==roster){roster=key;get("[data-team-target]").innerHTML='<option value="">All agents</option>'+members.filter(m=>m.kind!=="human").map(m=>'<option value="'+text(m.id)+'">'+text(m.name)+'</option>').join("");get("[data-team-target]").value=s.selectedAgent}
   get("[data-team-draft]").value=s.draft;
   const enabled=!!s.room&&["deliberating","plan_proposed","paused_for_deliberation","team_review"].includes(s.room.status)&&!s.busy;
   get("[data-team-send]").disabled=!enabled;get("[data-team-ask]").disabled=!enabled;
   get("[data-team-older]").disabled=s.busy||!(s.history?s.history.has_more:s.hasOlder||s.messages.length>=100);get("[data-team-live]").hidden=!s.history;
  }
  el.querySelectorAll("[data-team-view]").forEach(b=>b.onclick=()=>c.switchView(b.dataset.teamView));
  get("[data-team-draft]").oninput=e=>{c.state.draft=e.target.value};get("[data-team-target]").onchange=e=>{c.state.selectedAgent=e.target.value};
  get("[data-team-form]").onsubmit=e=>{e.preventDefault();c.send()};get("[data-team-ask]").onclick=()=>c.ask();get("[data-team-refresh]").onclick=()=>c.refresh();get("[data-team-older]").onclick=()=>c.older();get("[data-team-live]").onclick=()=>c.live();
  const observer=new MutationObserver(()=>{if(!alive())stop()});observer.observe(document.body,{childList:true,subtree:true});
  function stop(){clearTimeout(timer);observer.disconnect();c.stop()}
  async function tick(){if(!alive()){stop();return}if(!document.hidden)await c.refresh();if(alive())timer=setTimeout(tick,2000);else stop()}
  tick();return c;
 }
 root.OnePaneTeamRoom={controller,mount,content};
 if(typeof document!=="undefined"){
  document.addEventListener("click",e=>{const b=e.target.closest?.("[data-team-open-task]");if(b){e.preventDefault();mount(b.dataset.teamOpenTask)}});
 }
})(globalThis);
