/* Observes durable recovery intake only. No execution or decision endpoint. */
(function(root){
 "use strict";
 const escape=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
 const validID=value=>typeof value==="string"&&value.length>0&&value.length<=160;
 function controller({api,taskID,workspaceID,onChange=()=>{},alive=()=>true}){
  const state={cases:[],selected:null,after:"",nextAfter:"",busy:false,error:"",observedAt:0};
  const abort=new AbortController();let stopped=false;
  const clear=()=>{state.cases=[];state.selected=null;state.after="";state.nextAfter="";state.observedAt=0};
  const active=()=>!stopped&&alive();
  const emit=()=>{if(active())onChange(state)};
  const request=path=>api(path,{method:"GET",signal:abort.signal,cache:"no-store"});
  const path="/v1/tasks/"+encodeURIComponent(taskID);
  function metadata(row){
   if(!row||!validID(row.id)||row.task_id!==taskID||!validID(row.attempt_id)||row.phase!=="awaiting_inventory"||!Number.isSafeInteger(row.task_revision_at_capture)||row.task_revision_at_capture<1||!Number.isSafeInteger(row.created_at)||row.created_at<1)throw Error("Invalid recovery observation");
   return {id:row.id,task_id:taskID,attempt_id:row.attempt_id,phase:row.phase,task_revision_at_capture:row.task_revision_at_capture,created_at:row.created_at};
  }
  async function read(after,caseID=""){
   if(!active()||state.busy||!validID(taskID)||!validID(workspaceID))return;
   state.busy=true;state.error="";emit();
   try{
    // Independently bind the selected Task to the active tenant Workspace.
    const task=await request(path);if(!active())return;
    if((task?.id??task?.ID)!==taskID||(task?.workspace_id??task?.WorkspaceID)!==workspaceID)throw Error("Task scope changed");
    const result=await request(path+"/recovery-cases"+(caseID?"/"+encodeURIComponent(caseID):after?"?after="+encodeURIComponent(after):""));
    if(!active())return;
    if(caseID){const selected=metadata(result);if(selected.id!==caseID)throw Error("Foreign case");state.selected=selected}
    else{
     if(!Array.isArray(result?.cases)||result.cases.length>32)throw Error("Unbounded recovery page");
     const rows=result.cases.map(metadata),next=result.next_after??"";
     if(rows.some((row,i)=>row.id<=(i?rows[i-1].id:after))||typeof next!=="string"||next&&(rows.length!==32||next!==rows[31].id||!validID(next)))throw Error("Invalid recovery cursor");
     state.cases=rows;state.after=after;state.nextAfter=next;state.selected=null;
    }
    state.observedAt=Date.now();
   }catch{
    if(active()){clear();state.error="Recovery records unavailable. Refresh to check access again. No work was restarted."}
   }finally{state.busy=false;if(!active())clear();emit()}
  }
  return {state,
   refresh(){return read(state.after,state.selected?.id||"")},
   first(){return read("")},
   next(){if(state.nextAfter&&!state.selected)return read(state.nextAfter)},
   inspect(caseID){if(state.cases.some(row=>row.id===caseID))return read(state.after,caseID)},
   list(){return read(state.after)},
   stop(){stopped=true;abort.abort();clear();state.busy=false;state.error=""}
  };
 }
 function content(state){
  const explanation='<p>These records identify interrupted execution. Effects have not been inventoried or independently verified. Recovery decisions are not available yet; viewing a record cannot resume or cancel work.</p>';
  const fields=row=>'<dl><dt>Task</dt><dd>'+escape(row.task_id)+'</dd><dt>Record</dt><dd>'+escape(row.id)+'</dd><dt>Original attempt</dt><dd>'+escape(row.attempt_id)+'</dd><dt>Recovery status</dt><dd>Awaiting effect inventory</dd><dt>Task revision at intake</dt><dd>'+escape(row.task_revision_at_capture)+'</dd><dt>Recorded</dt><dd>'+escape(new Date(row.created_at).toLocaleString())+'</dd></dl>';
  return explanation+'<div class="toolbar"><button class="btn" type="button" data-recovery-refresh'+(state.busy?' disabled':'')+'>Refresh</button>'+(state.selected?'<button class="btn" type="button" data-recovery-list'+(state.busy?' disabled':'')+'>Back to records</button>':(state.after?'<button class="btn" type="button" data-recovery-first'+(state.busy?' disabled':'')+'>First page</button>':'')+(state.nextAfter?'<button class="btn" type="button" data-recovery-next'+(state.busy?' disabled':'')+'>Next page</button>':''))+'</div><p role="status" aria-live="polite">'+escape(state.error||(state.busy?'Checking recovery records…':state.observedAt?'Observed '+new Date(state.observedAt).toLocaleTimeString():''))+'</p><div class="task-recovery-records">'+(state.error?'':state.selected?'<article class="panel-card">'+fields(state.selected)+'</article>':state.cases.length?state.cases.map(row=>'<article class="panel-card">'+fields(row)+'<button class="btn" type="button" data-recovery-case="'+escape(row.id)+'"'+(state.busy?' disabled':'')+'>Inspect record</button></article>').join(''):state.observedAt?'<p>No recovery records were returned for this page. This does not establish that the Task is safe to retry or that cancellation is complete.</p>':'')+'</div>';
 }
 function mountInto(host,taskID,workspaceID){
  if(!host||typeof apiRequest!=="function")return;
  let timer,observer,elapsed=0,focusAttribute="",focusValue="";
  const alive=()=>host.isConnected&&workspaceID===onepaneWorkspace;
  const focusAttributes=["data-recovery-case","data-recovery-refresh","data-recovery-list","data-recovery-first","data-recovery-next"];
  function rememberFocus(button){if(!button?.hasAttribute)return;const attribute=focusAttributes.find(name=>button.hasAttribute(name));if(attribute){focusAttribute=attribute;focusValue=button.getAttribute?.(attribute)||""}}
  const c=controller({api:apiRequest,taskID,workspaceID,alive,onChange:state=>{
   if(document.activeElement&&host.contains(document.activeElement))rememberFocus(document.activeElement);
   host.innerHTML=content(state);
   if(!state.busy&&focusAttribute){const buttons=Array.from(host.querySelectorAll("button"));const target=buttons.find(b=>b.hasAttribute(focusAttribute)&&b.getAttribute(focusAttribute)===focusValue)||buttons.find(b=>b.hasAttribute("data-recovery-list"))||buttons.find(b=>b.hasAttribute("data-recovery-refresh"));target?.focus();focusAttribute=""}
  }});
  host.classList.add("task-recovery");
  host.onclick=e=>{
   const button=e.target.closest?.("button");if(!button||!host.contains(button))return;
   rememberFocus(button);
   if(button.hasAttribute("data-recovery-refresh"))c.refresh();
   else if(button.hasAttribute("data-recovery-next"))c.next();
   else if(button.hasAttribute("data-recovery-first"))c.first();
   else if(button.hasAttribute("data-recovery-list"))c.list();
   else if(button.hasAttribute("data-recovery-case"))c.inspect(button.dataset.recoveryCase);
  };
  function stop(){clearTimeout(timer);observer?.disconnect();c.stop();host.textContent="Recovery inspection closed.";host.onclick=null}
  observer=new MutationObserver(()=>{if(!alive())stop()});observer.observe(document.body,{childList:true,subtree:true});
  function tick(){if(!alive()){stop();return}if(!document.hidden&&++elapsed>=15){elapsed=0;c.refresh()}timer=setTimeout(tick,1000)}
  c.first();tick();return {stop,controller:c};
 }
 function mount(taskID){
  if(!validID(taskID)||typeof openModal!=="function")return;
  const scope=onepaneWorkspace;
  openModal("Interrupted execution",'<section id="onepaneTaskRecovery" aria-label="Task recovery records"></section>');
  return mountInto(document.getElementById("onepaneTaskRecovery"),taskID,scope);
 }
 root.OnePaneTaskRecovery={controller,content,mount,mountInto};
 if(typeof document!=="undefined"){
  const overview=qa4InspectorOverview;
  qa4InspectorOverview=function(){const html=overview();return qa4Inspector.kind==="task"&&validID(String(qa4Inspector.id||""))?html+'<section class="spec-readable-section"><h3>Interrupted execution</h3><p>Inspect recorded recovery intake and the original interrupted attempts.</p><button class="btn" type="button" data-task-recovery-open="'+escape(qa4Inspector.id)+'">Recovery records</button></section>':html};
  document.addEventListener("click",e=>{const button=e.target.closest?.("[data-task-recovery-open]");if(button){e.preventDefault();mount(button.dataset.taskRecoveryOpen)}});
 }
})(globalThis);
