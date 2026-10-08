/* Manual Chair workbench actions and exact session/seat routing.
 * Operator approval is a separate audited API action from model submission.
 */
let a42ChairTurns=[];
function a42ChairPending(t){return t.status==="awaiting_input"||t.status==="awaiting_approval"}
function a42BindChairQueue(rows){
 let changed=false;
 for(const tab of a40WebTabs()){
  if(!tab.council_chair||!tab.session_id||!tab.member_id)continue;
  const matching=rows.filter(t=>t.session_id===tab.session_id&&t.member_id===tab.member_id)
    .sort((a,b)=>(Number(b.created_at)||0)-(Number(a.created_at)||0));
  const current=matching.find(t=>t.id===tab.chair_turn_id);
  if(current&&a42ChairPending(current))continue;
  const next=matching.find(a42ChairPending);
  if(!next||next.id===tab.chair_turn_id||a40WebDrafts.get(tab.id)?.response?.trim())continue;
  tab.chair_turn_id=next.id;changed=true;
 }
 if(changed)persist();
 return changed;
}
function a42ChairActive(tab){
 if(!tab?.council_chair)return null;
 return a42ChairTurns.find(t=>t.id===tab.chair_turn_id&&t.session_id===tab.session_id&&t.member_id===tab.member_id)||null;
}
async function a42ChairSubmit(tab,t){
 const response=String(a40WebDrafts.get(tab.id)?.response||"").trim();
 if(!response)return notice("Paste the Chair model's proposed agenda or follow-up questions.","bad");
 if(!confirm("Submit this proposal as the Chair model's response?"))return;
 try{
  await apiRequest(`/v1/manual-web/chair-turns/${encodeURIComponent(t.id)}/submit`,{method:"POST",body:JSON.stringify({response_text:response})});
  a40WebDrafts.delete(tab.id);
  notice("Chair proposal recorded. Approval is required before the Council can advance if the approval gate is enabled.");
  renderWebChat();
 }catch(ex){notice(ex.message,"bad")}
}
async function a42ChairApprove(tab,t){
 const text=String($("#a40Response")?.value||t.response_text||"").trim();
 if(!text)return notice("The approved Chair guidance cannot be empty.","bad");
 if(!confirm("Approve this Chair agenda/questions for the next Council round? You may edit the proposal before approval."))return;
 try{
  await apiRequest(`/v1/manual-web/chair-turns/${encodeURIComponent(t.id)}/approve`,{method:"POST",body:JSON.stringify({approved_text:text})});
  a40WebDrafts.delete(tab.id);
  notice("Chair guidance approved. OnePane will schedule the next Research Council round.");
  renderWebChat();
 }catch(ex){notice(ex.message,"bad")}
}
