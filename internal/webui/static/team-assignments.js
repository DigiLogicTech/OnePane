/* Human review of named assignments. Saving a plan never executes it. */
(function(root){
 "use strict";
 const escape=v=>String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
 function mount(room,controller){
  const host=document.createElement("section");host.className="team-assignment-section";
  host.innerHTML='<h3>Team work</h3><div data-team-assignment-status></div><details data-team-plan-editor><summary>Plan named assignments</summary><p>Define each contribution and which earlier results it needs. The original Task consolidates the result after all contributions pass its verification requirements. Work inherits the Task’s permissions, model restrictions and verification criteria.</p><form class="qa-form" data-team-plan-form><div data-team-agent-drafts></div><div data-team-plan-rows></div><div class="toolbar"><button class="btn" type="button" data-team-plan-add>Add assignment</button><button class="btn" type="submit" data-team-plan-save>Save plan for review</button></div></form><div data-team-plan-review></div></details>';
  room.append(host);
  const get=s=>host.querySelector(s);let drafts=[],initialized=false;
  get("[data-team-assignment-status]").onclick=async e=>{const button=e.target.closest("[data-team-assignment-open]");if(!button)return;const task=await controller.readAssignment(button.dataset.teamAssignmentOpen);if(task&&typeof qa4Inspect==="function"){if(typeof closeModal==="function")closeModal();qa4Inspect("task",button.dataset.teamAssignmentOpen,task.objective||task.Objective,task)}};
  const members=()=>controller.state.room?.members.filter(m=>m.kind==="agent")||[];
  function capture(){
   let valid=true;
   host.querySelectorAll("[data-plan-row]").forEach((row,i)=>{
    drafts[i].member_id=row.querySelector("select[data-plan-member]").value;
    drafts[i].objective=row.querySelector("textarea").value;
    drafts[i].required_verification=row.querySelector("[data-plan-verification]").value;
    const criteria=row.querySelector("[data-plan-criteria]");criteria.setCustomValidity("");
    try{const raw=criteria.value.trim();const value=raw?JSON.parse(raw):undefined;if(value!==undefined&&(!value||Array.isArray(value)||typeof value!=="object"))throw Error();drafts[i].verification_spec=value}
    catch{criteria.setCustomValidity("Enter a JSON object for additional checks, or leave this blank.");criteria.reportValidity();valid=false}
    drafts[i].depends_on=Array.from(row.querySelectorAll("[data-plan-dependency]:checked")).map(input=>input.value);
   });
   return valid;
  }
  function rows(){
   const roster=members();
   get("[data-team-plan-rows]").innerHTML=drafts.map((a,i)=>'<fieldset data-plan-row><legend>Assignment '+(i+1)+'</legend><label>Agent<select data-plan-member required>'+roster.map(m=>'<option value="'+escape(m.id)+'"'+(m.id===a.member_id?' selected':'')+'>'+escape(m.name)+'</option>').join("")+'</select></label><label>Contribution<textarea rows="2" maxlength="4096" required>'+escape(a.objective)+'</textarea></label><label>Minimum verification<select data-plan-verification>'+['','V0','V1','V2','V3','V4','V5'].map(level=>'<option value="'+level+'"'+(a.required_verification===level?' selected':'')+'>'+escape(level||'Use Task requirement')+'</option>').join('')+'</select></label><label>Additional verification checks (JSON)<textarea data-plan-criteria rows="3" maxlength="16384" spellcheck="false">'+escape(a.verification_spec?JSON.stringify(a.verification_spec,null,2):'')+'</textarea></label><p class="list-meta">Optional result, artifact or observation checks. These add to the Task requirements.</p><div role="group" aria-label="Required earlier contributions">'+drafts.slice(0,i).map((prior,j)=>'<label><input type="checkbox" data-plan-dependency value="'+escape(prior.key)+'"'+(a.depends_on.includes(prior.key)?' checked':'')+'> Wait for assignment '+(j+1)+'</label>').join("")+'</div></fieldset>').join("");
  }
  get("[data-team-plan-form]").oninput=e=>{if(e.target.matches("[data-plan-criteria]"))e.target.setCustomValidity("");controller.invalidatePlanReview()};
	get("[data-team-agent-drafts]").onclick=e=>{const button=e.target.closest("[data-team-adopt-draft]");if(!button)return;const proposed=controller.assignmentDraft(button.dataset.teamAdoptDraft);if(!proposed)return;controller.invalidatePlanReview();drafts=proposed;rows()};
  get("[data-team-plan-form]").onchange=()=>controller.invalidatePlanReview();
  get("[data-team-plan-add]").onclick=()=>{if(!capture()||drafts.length>=8)return;controller.invalidatePlanReview();let sequence=drafts.length+1;while(drafts.some(a=>a.key==="work-"+sequence))sequence++;drafts.push({key:"work-"+sequence,member_id:members()[drafts.length%members().length]?.id||"",objective:"",depends_on:[]});rows()};
  get("[data-team-plan-form]").onsubmit=async e=>{e.preventDefault();if(!capture())return;await controller.proposeAssignments(drafts.map(a=>({...a,objective:a.objective.trim(),depends_on:[...a.depends_on]})))};
  get("[data-team-plan-review]").onclick=async e=>{if(e.target.closest("[data-team-plan-accept]"))await controller.acceptAssignments()};
  return {draw(state){
   const roster=members(),names=new Map(roster.map(m=>[m.id,m.name]));
   if(!initialized&&roster.length){initialized=true;drafts=roster.slice(0,2).map((m,i)=>({key:"work-"+(i+1),member_id:m.id,objective:"",depends_on:[]}));rows()}
   const assignments=state.room?.assignments||[];
   get("[data-team-assignment-status]").innerHTML=assignments.length?assignments.map(a=>'<article class="panel-card"><h4>'+escape(names.get(a.member_id)||"Team agent")+' · '+escape(a.key)+'</h4><p>'+escape(a.objective)+'</p><p>Task: '+escape(a.state)+(a.compute_state?' · Compute: '+escape(a.compute_state):'')+'</p>'+(a.depends_on?.length?'<p>Requires: '+a.depends_on.map(escape).join(", ")+'</p>':'')+(a.last_step_kind?'<p>Observed '+escape(a.last_step_kind)+': '+escape(a.last_step_status)+'</p>':'')+'<button class="btn" type="button" data-team-assignment-open="'+escape(a.task_id)+'">Inspect task</button></article>').join(""):'<p class="list-meta">No accepted execution assignments.</p>';
   const canPlan=!!state.room&&roster.length>0&&state.room.task_state==="created"&&!assignments.length;
   get("[data-team-plan-editor]").hidden=!canPlan;
	get("[data-team-agent-drafts]").innerHTML=canPlan?state.messages.filter(m=>controller.assignmentDraft(String(m.id||m.ID))).slice(-4).map(m=>'<button class="btn" type="button" data-team-adopt-draft="'+escape(m.id||m.ID)+'">Review '+escape(names.get(m.author_member_id||m.AuthorMemberID)||'agent')+'’s suggested assignments</button>').join(""):"";
   host.querySelectorAll("[data-team-plan-form] button,[data-team-plan-form] input,[data-team-plan-form] select,[data-team-plan-form] textarea").forEach(b=>b.disabled=!canPlan||state.busy);
   get("[data-team-plan-add]").disabled=!canPlan||state.busy||drafts.length>=8;
   const review=state.proposedPlan;
   get("[data-team-plan-review]").innerHTML=review?'<h4>Review this saved plan</h4><ol>'+review.assignments.map(a=>'<li><strong>'+escape(names.get(a.member_id)||"Team agent")+'</strong>: '+escape(a.objective)+(a.depends_on.length?' (after '+a.depends_on.map(escape).join(", ")+')':' (independent)')+' · Verification: '+escape(a.required_verification||'Task requirement')+(a.verification_spec?'<pre>'+escape(JSON.stringify(a.verification_spec,null,2))+'</pre>':'')+'</li>').join("")+'</ol><p>Approval queues these Tasks under the existing execution and tool controls. Blocking objections require separate resolution.</p><button class="btn primary" type="button" data-team-plan-accept'+(!canPlan||state.busy?' disabled':'')+'>Approve plan and queue work</button>':"";
  }};
 }
 root.OnePaneTeamAssignments={mount};
})(globalThis);
