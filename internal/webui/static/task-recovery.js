/* Observes durable recovery intake only. No execution or decision endpoint. */
(function(root){
 "use strict";
 const escape=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
 const validID=value=>typeof value==="string"&&value.length>0&&value.length<=160;
 function controller({api,taskID,workspaceID,onChange=()=>{},alive=()=>true}){
  const state={cases:[],selected:null,journal:null,journalObservedAt:0,after:"",nextAfter:"",busy:false,error:"",observedAt:0};
  const abort=new AbortController();let stopped=false;
  const clear=()=>{state.cases=[];state.selected=null;state.journal=null;state.journalObservedAt=0;state.after="";state.nextAfter="";state.observedAt=0};
  const active=()=>!stopped&&alive();
  const emit=()=>{if(active())onChange(state)};
  const request=path=>api(path,{method:"GET",signal:abort.signal,cache:"no-store"});
  const path="/v1/tasks/"+encodeURIComponent(taskID);
  function metadata(row){
   if(!row||!validID(row.id)||row.task_id!==taskID||!validID(row.attempt_id)||row.phase!=="awaiting_inventory"||!Number.isSafeInteger(row.task_revision_at_capture)||row.task_revision_at_capture<1||!Number.isSafeInteger(row.created_at)||row.created_at<1)throw Error("Invalid recovery observation");
   return {id:row.id,task_id:taskID,attempt_id:row.attempt_id,phase:row.phase,task_revision_at_capture:row.task_revision_at_capture,created_at:row.created_at};
  }
  function journalMetadata(value,selected,taskRevision){
   const hash=v=>typeof v==='string'&&/^sha256:[a-f0-9]{64}$/.test(v);
   const kinds=['route','model','agent_runtime','tool','operation','delegate','replan','complete','human','escalate','wait','fail','verification'],statuses=['started','succeeded','failed','waiting','unknown'];
   if(!selected||value?.case_id!==selected.id||value.task_id!==taskID||value.attempt_id!==selected.attempt_id||value.phase!=='awaiting_inventory'||!Number.isSafeInteger(value.task_revision)||value.task_revision<selected.task_revision_at_capture||!Number.isSafeInteger(taskRevision)||value.task_revision<taskRevision||!['recorded','not_recorded'].includes(value.availability)||!Array.isArray(value.steps)||value.steps.length>256)throw Error('Invalid stored journal');
   if(value.availability==='not_recorded'){
    if(value.steps.length||value.run_id||value.snapshot_sha256||value.observation_version)throw Error('Contradictory missing journal');
    return {case_id:value.case_id,task_id:taskID,attempt_id:value.attempt_id,task_revision:value.task_revision,availability:'not_recorded',steps:[]};
   }
   if(!validID(value.run_id)||value.observation_version!=='onepane.worker-journal-slice/v1'||!hash(value.snapshot_sha256))throw Error('Invalid journal identity');
   const steps=value.steps.map((step,i)=>{
    if(!validID(step.id)||step.number!==i+1||!kinds.includes(step.kind)||!statuses.includes(step.status)||!hash(step.metadata_sha256)||typeof step.reference_recognized!=='boolean')throw Error('Invalid journal step');
    return {id:step.id,number:step.number,kind:step.kind,status:step.status,metadata_sha256:step.metadata_sha256,reference_recognized:step.reference_recognized};
   });
   if(new Set(steps.map(step=>step.id)).size!==steps.length)throw Error('Duplicate journal identity');
   return {case_id:value.case_id,task_id:taskID,attempt_id:value.attempt_id,task_revision:value.task_revision,availability:'recorded',run_id:value.run_id,observation_version:value.observation_version,snapshot_sha256:value.snapshot_sha256,steps};
  }
  async function read(after,caseID="",withJournal=false){
   if(!active()||state.busy||!validID(taskID)||!validID(workspaceID))return;
   const expectedCase=withJournal&&state.selected?{...state.selected}:null;
   if(withJournal&&(!expectedCase||expectedCase.id!==caseID))return;
   state.busy=true;state.error="";emit();
   try{
    // Independently bind the selected Task to the active tenant Workspace.
    const task=await request(path);if(!active())return;
    if((task?.id??task?.ID)!==taskID||(task?.workspace_id??task?.WorkspaceID)!==workspaceID)throw Error("Task scope changed");
    const result=await request(path+"/recovery-cases"+(caseID?"/"+encodeURIComponent(caseID)+(withJournal?'/journal':''):after?"?after="+encodeURIComponent(after):""));
    if(!active())return;
    const taskRevision=task?.revision??task?.Revision;
    if(withJournal){
     if(state.selected?.id!==expectedCase.id||state.selected?.attempt_id!==expectedCase.attempt_id)throw Error('Recovery selection changed');
     state.journal=journalMetadata(result,expectedCase,taskRevision);state.journalObservedAt=Date.now();
    }
    else if(caseID){const selected=metadata(result);if(selected.id!==caseID)throw Error("Foreign case");state.selected=selected;
     if(state.journal&&(state.journal.case_id!==selected.id||state.journal.attempt_id!==selected.attempt_id||state.journal.task_revision!==taskRevision)){state.journal=null;state.journalObservedAt=0}
    }
    else{
     if(!Array.isArray(result?.cases)||result.cases.length>32)throw Error("Unbounded recovery page");
     const rows=result.cases.map(metadata),next=result.next_after??"";
     if(rows.some((row,i)=>row.id<=(i?rows[i-1].id:after))||typeof next!=="string"||next&&(rows.length!==32||next!==rows[31].id||!validID(next)))throw Error("Invalid recovery cursor");
     state.cases=rows;state.after=after;state.nextAfter=next;state.selected=null;state.journal=null;state.journalObservedAt=0;
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
   journal(){if(state.selected)return read(state.after,state.selected.id,true)},
   list(){return read(state.after)},
   stop(){stopped=true;abort.abort();clear();state.busy=false;state.error=""}
  };
 }
 function content(state){
  const explanation='<p>These records identify interrupted execution. Effects have not been inventoried or independently verified. Recovery decisions are not available yet; viewing a record cannot resume or cancel work.</p>';
  const fields=row=>'<dl><dt>Task</dt><dd>'+escape(row.task_id)+'</dd><dt>Record</dt><dd>'+escape(row.id)+'</dd><dt>Original attempt</dt><dd>'+escape(row.attempt_id)+'</dd><dt>Recovery status</dt><dd>Awaiting effect inventory</dd><dt>Task revision at intake</dt><dd>'+escape(row.task_revision_at_capture)+'</dd><dt>Recorded</dt><dd>'+escape(new Date(row.created_at).toLocaleString())+'</dd></dl>';
  const journal=state.journal;
  const journalView=journal?'<section class="task-recovery-journal"><h4>Stored execution journal</h4><p>Observed '+escape(new Date(state.journalObservedAt).toLocaleTimeString())+'. These stored records do not establish actual external outcomes, independent verification or permission to retry.</p>'+(journal.availability==='not_recorded'?'<p>No Worker journal is recorded for this original attempt. This does not establish that no effects occurred.</p>':'<dl><dt>Worker</dt><dd>'+escape(journal.run_id)+'</dd><dt>Snapshot hash</dt><dd>'+escape(journal.snapshot_sha256)+'</dd></dl>'+(journal.steps.length?'<ol>'+journal.steps.map(step=>'<li><strong>Step '+escape(step.number)+' · '+escape(({model:'Model inference',agent_runtime:'Agent runtime',route:'Model routing',tool:'Tool call',operation:'Governed operation',delegate:'Delegation',verification:'Verification'})[step.kind]||step.kind)+'</strong><div>Recorded status: '+escape(step.status==='unknown'?'Unknown outcome':step.status)+'</div><div>Record: '+escape(step.id)+'</div><div>Reference format: '+(step.reference_recognized?'Recognized; target ownership remains unverified':'Not interpreted')+'</div></li>').join('')+'</ol>':'<p>No steps were returned in this journal. This is not proof of no effects.</p>'))+'</section>':'';
  return explanation+'<div class="toolbar"><button class="btn" type="button" data-recovery-refresh'+(state.busy?' disabled':'')+'>Refresh</button>'+(state.selected?'<button class="btn" type="button" data-recovery-list'+(state.busy?' disabled':'')+'>Back to records</button><button class="btn" type="button" data-recovery-journal'+(state.busy?' disabled':'')+'>'+(journal?'Refresh stored journal':'Inspect stored journal')+'</button>':(state.after?'<button class="btn" type="button" data-recovery-first'+(state.busy?' disabled':'')+'>First page</button>':'')+(state.nextAfter?'<button class="btn" type="button" data-recovery-next'+(state.busy?' disabled':'')+'>Next page</button>':''))+'</div><p role="status" aria-live="polite">'+escape(state.error||(state.busy?'Checking recovery records…':state.observedAt?'Observed '+new Date(state.observedAt).toLocaleTimeString():''))+'</p><div class="task-recovery-records">'+(state.error?'':state.selected?'<article class="panel-card">'+fields(state.selected)+journalView+'</article>':state.cases.length?state.cases.map(row=>'<article class="panel-card">'+fields(row)+'<button class="btn" type="button" data-recovery-case="'+escape(row.id)+'"'+(state.busy?' disabled':'')+'>Inspect record</button></article>').join(''):state.observedAt?'<p>No recovery records were returned for this page. This does not establish that the Task is safe to retry or that cancellation is complete.</p>':'')+'</div>';
 }
 function mountInto(host,taskID,workspaceID){
  if(!host||typeof apiRequest!=="function")return;
  let timer,observer,elapsed=0,focusAttribute="",focusValue="";
  const alive=()=>host.isConnected&&workspaceID===onepaneWorkspace;
  const focusAttributes=["data-recovery-case","data-recovery-refresh","data-recovery-list","data-recovery-first","data-recovery-next","data-recovery-journal"];
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
   else if(button.hasAttribute("data-recovery-journal"))c.journal();
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
