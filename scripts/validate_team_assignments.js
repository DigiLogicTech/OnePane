// Exercise the shipped controller against plan/Task APIs, without a browser,
// model, tool execution or installer. Product UI review remains separate.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const scope=vm.createContext({AbortController,Map,Array,JSON,Number,String,Promise,Error});
vm.runInContext(fs.readFileSync('internal/webui/static/team-room.js','utf8'),scope);
new vm.Script(fs.readFileSync('internal/webui/static/team-assignments.js','utf8'));
const {controller}=scope.OnePaneTeamRoom;
const foundation=fs.readFileSync('internal/webui/static/app-foundation.js','utf8');
const finder=foundation.slice(foundation.indexOf('function qa4FindEntity('),foundation.indexOf('function qa4InspectorOverview('));
const inspectorScope=vm.createContext({liveOps:{tasks:[]},qa4Inspector:{kind:'task',id:'new-child',data:{id:'new-child',objective:'Fetched assignment'}}});
vm.runInContext(finder,inspectorScope);assert.equal(vm.runInContext('qa4FindEntity("task","new-child").objective',inspectorScope),'Fetched assignment','uncached assignment inspection was empty');
assert.equal(vm.runInContext('Object.keys(qa4FindEntity("task","foreign")).length',inspectorScope),0,'inspector leaked another Task fallback');
async function main(){
 let room={id:'session',task_id:'root',workspace_id:'tenant',task_state:'created',status:'deliberating',members:[{id:'a',kind:'agent',name:'Builder'},{id:'b',kind:'agent',name:'Reviewer'}],assignments:[]};
 let calls=[],posts=[],hold=null,fail=false;
 const api=async(path,options={})=>{
  calls.push(path);
  if(options.method==='POST'){
   const body=JSON.parse(options.body);posts.push({path,body});if(hold)await hold;if(fail)throw Error('unknown network outcome');
   if(path.endsWith('/plans')){room={...room,status:'plan_proposed'};return {ID:'reviewed-plan',SessionID:'session'}}
   if(path.endsWith('/accept')){room={...room,status:'plan_accepted',task_state:'ready',assignments:[{key:'build',member_id:'a',task_id:'child'}]};return {ID:'session'}}
   throw Error('unexpected mutation');
  }
  if(path==='/v1/tasks/root/team-session')return {ID:'session',TaskID:'root',WorkspaceID:'tenant'};
  if(path==='/v1/tasks/child')return {ID:'child',WorkspaceID:'tenant',ParentTaskID:'root',Objective:'Build'};
  if(path.endsWith('/live'))return room;
  if(path.includes('/messages/page'))return {messages:[],has_more:false};
  throw Error('unexpected read: '+path);
 };
 const c=controller({api,taskID:'root',workspaceID:'tenant'});await c.refresh();
 const suggest=assignments=>({id:'suggestion',kind:'agent',author_member_id:'a',content:{proposal_type:'replan',proposal:{assignments}}});
 const draft=[{key:'review',member_id:'b',objective:'Review',depends_on:['build']},{key:'build',member_id:'a',objective:'Build',depends_on:[],required_verification:'V2',verification_spec:{result_checks:[{pointer:'/summary',operator:'not_empty'}],artifacts:[{media_type:'text/plain'}]}}];
 c.state.messages=[suggest(draft)];
 const adopted=c.assignmentDraft('suggestion');
 assert.equal(JSON.stringify(adopted.map(a=>a.key)),JSON.stringify(['build','review']),'draft dependency order was lost');
 assert.equal(adopted[0].required_verification,'V2');assert.equal(JSON.stringify(adopted[0].verification_spec),JSON.stringify(draft[1].verification_spec),'adoption dropped verification requirements');
 adopted[0].objective='Edited';assert.equal(draft[1].objective,'Build','adoption mutated the recorded suggestion');
 assert.equal(posts.length,0,'reading a suggestion executed work');
 for(const bad of [
  [{...draft[0],depends_on:['missing']},draft[1]],
  [draft[0],{...draft[1],depends_on:['review']}],
  [draft[1],{...draft[0],key:'build'}],
  [{...draft[1],member_id:'foreign'}],
  [{...draft[1],depends_on:['build']}],
  [{...draft[1],objective:''}],
  [{...draft[1],required_verification:'V99'}],
  [{...draft[1],verification_spec:[]}],
 ]){c.state.messages=[suggest(bad)];assert.equal(c.assignmentDraft('suggestion'),null,'unsafe assignment draft adopted')}
 for(const patch of [{kind:'human'},{author_member_id:'foreign'},{content:{proposal_type:'complete',proposal:{assignments:draft}}}]){
  c.state.messages=[{...suggest(draft),...patch}];assert.equal(c.assignmentDraft('suggestion'),null,'unscoped suggestion adopted');
 }
 c.state.messages=[];
 const assignments=[{key:'build',member_id:'a',objective:'Build',depends_on:[]},{key:'review',member_id:'b',objective:'Review',depends_on:['build']}];
 assert.equal(await c.acceptAssignments(),false,'unsaved plan executed');assert.equal(posts.length,0);
 assert.equal(await c.proposeAssignments([{...assignments[0],member_id:'foreign'}]),false);assert.equal(posts.length,0);
 assert.equal(await c.proposeAssignments(assignments),true);assert.equal(posts.length,1,'saving a plan executed it');
 assert.deepEqual(posts[0].body,{plan:{assignments}});assert.equal(c.state.proposedPlan.id,'reviewed-plan');
 c.state.draft='Guidance survives plan review';c.switchView('agents');c.switchView('conversation');assert.equal(posts.length,1);assert.equal(c.state.draft,'Guidance survives plan review');
 let release;hold=new Promise(r=>release=r);const accepting=c.acceptAssignments();assert.equal(await c.acceptAssignments(),false,'duplicate approval executed');release();assert.equal(await accepting,true);hold=null;
 assert.equal(posts.length,2);assert.ok(posts[1].path.endsWith('/plans/reviewed-plan/accept'));assert.equal(posts[1].body.accept_open_risk,false);assert.equal(c.state.proposedPlan,null);
 const before=calls.length;assert.equal(await c.readAssignment('foreign-task'),null);assert.equal(calls.length,before,'foreign Task was queried');
 assert.equal((await c.readAssignment('child')).ID,'child');assert.equal(await c.proposeAssignments(assignments),false,'active run was silently replaced');
 room={...room,status:'deliberating',task_state:'created',assignments:[]};await c.refresh();fail=true;
 const mutations=posts.length;assert.equal(await c.proposeAssignments(assignments),false);assert.equal(posts.length,mutations+1,'failed save retried itself');assert.equal(c.state.proposedPlan,null);
 fail=false;room={...room,assignments:[{member_id:'foreign',task_id:'private'}]};await c.refresh();assert.equal(c.state.room,null,'foreign assignment projection retained');
 room={...room,assignments:[]};await c.refresh();let releaseSave;hold=new Promise(r=>releaseSave=r);const saving=c.proposeAssignments(assignments);c.invalidatePlanReview();releaseSave();assert.equal(await saving,false,'changed draft retained stale approval');hold=null;assert.equal(c.state.proposedPlan,null);
 let releaseSnapshot;hold=new Promise(r=>releaseSnapshot=r);const mutable=JSON.parse(JSON.stringify(assignments));const snapshotSave=c.proposeAssignments(mutable);mutable[0].objective='Changed after request';releaseSnapshot();assert.equal(await snapshotSave,true);hold=null;
 assert.equal(c.state.proposedPlan.assignments[0].objective,posts.at(-1).body.plan.assignments[0].objective,'review differs from saved request');
 // An authorized Task response arriving after view replacement must not reopen
 // stale contribution data. Exercise the actual asynchronous controller path.
 for(const scenario of ['allowed','removed','plan','member','key','session','scope','foreign_parent','foreign_workspace','closed','revoked']){
  let releaseTask;
  const snapshot={id:'session',task_id:'root',workspace_id:'tenant',accepted_plan_id:'plan',members:[{id:'a',kind:'agent'}],assignments:[{key:'build',member_id:'a',task_id:'child'}]};
  const reader=controller({taskID:'root',workspaceID:'tenant',api:async path=>{
   if(path==='/v1/tasks/root/team-session')return {ID:'session',TaskID:'root',WorkspaceID:'tenant'};
   if(path.endsWith('/live'))return snapshot;
   if(path.includes('/messages/page'))return {messages:[]};
   if(path==='/v1/tasks/child')return new Promise(resolve=>{releaseTask=resolve});
   throw Error('unexpected read');
  }});
  await reader.refresh();const pending=reader.readAssignment('child');
  const updated={...snapshot,assignments:snapshot.assignments.map(a=>({...a}))};
  if(scenario==='removed')updated.assignments=[];
  if(scenario==='plan')updated.accepted_plan_id='replacement';
  if(scenario==='member')updated.assignments[0].member_id='b';
  if(scenario==='key')updated.assignments[0].key='replacement';
  if(scenario==='session')updated.id='other-session';
  if(scenario==='scope')updated.workspace_id='other-tenant';
  reader.state.room=scenario==='revoked'?null:updated;
  if(scenario==='closed')reader.stop();
  releaseTask({ID:'child',ParentTaskID:scenario==='foreign_parent'?'foreign':'root',WorkspaceID:scenario==='foreign_workspace'?'foreign':'tenant',Result:{private:'result'}});
  const inspected=await pending;
  if(scenario==='allowed')assert.equal(inspected.ID,'child');else assert.equal(inspected,null,'stale/foreign assignment opened: '+scenario);
  reader.stop();
 }
 // Exercise shipped form handlers with controlled DOM fields, including the
 // criteria capture and approval markup that controller-only checks omit.
 const elements=new Map(),element=selector=>{if(!elements.has(selector))elements.set(selector,{innerHTML:'',disabled:false,hidden:false});return elements.get(selector)};
 const criteria={value:'',error:'',setCustomValidity(message){this.error=message},reportValidity(){return !this.error},matches:s=>s==='[data-plan-criteria]'};
 const fields={
  'select[data-plan-member]':{value:'a'},'textarea':{value:'Build'},
  '[data-plan-verification]':{value:'V2'},'[data-plan-criteria]':criteria,
 };
 const row={querySelector:s=>fields[s],querySelectorAll:()=>[]};
 let previews=[],resultReads=0;
 const resultButton={dataset:{teamContributionRead:'child'},parentElement:{append:preview=>previews.push(preview)}};
 const host={isConnected:true,querySelector:element,querySelectorAll:s=>s==='[data-plan-row]'?[row]:s==='[data-team-contribution-read]'?[resultButton]:[],className:'',innerHTML:''};
 const statusNode=element('[data-team-assignment-status]');let statusHTML='';
 Object.defineProperty(statusNode,'innerHTML',{get:()=>statusHTML,set:value=>{statusHTML=value;previews=[]}});
 scope.document={createElement:()=>host};vm.runInContext(fs.readFileSync('internal/webui/static/team-assignments.js','utf8'),scope);
 let formSaved=null;
 const formState={room:{...room,task_state:'created',assignments:[]},messages:[],busy:false,proposedPlan:null};
 let readResult=async()=>({ID:'child',State:'complete',Result:{summary:'<script>untrusted</script>',artifact_id:'foreign-artifact'}});
 const formController={state:formState,invalidatePlanReview(){formState.proposedPlan=null},assignmentDraft:()=>null,proposeAssignments:async value=>{formSaved=value},readAssignment:async()=>{resultReads++;return readResult()}};
 const editor=scope.OnePaneTeamAssignments.mount({append(){}},formController);editor.draw(formState);
 const completed={key:'build',member_id:'a',task_id:'child',objective:'Build',state:'complete',checkpoint_id:'checkpoint<unsafe>',verification_id:'verification<unsafe>',verification_level:'V0'};
 formState.room={...formState.room,assignments:[completed]};editor.draw(formState);
 let evidenceHTML=element('[data-team-assignment-status]').innerHTML;
 assert.ok(evidenceHTML.includes('V0 (declaration only)'));assert.ok(evidenceHTML.includes('checkpoint&lt;unsafe&gt;'));assert.ok(!evidenceHTML.includes('checkpoint<unsafe>'));
 assert.ok(evidenceHTML.includes('verification&lt;unsafe&gt;'));assert.ok(evidenceHTML.includes('data-team-assignment-open="child"'));
 scope.document.createElement=tag=>{assert.equal(tag,'pre');return {textContent:'',className:''}};
 const resultClick={target:{closest:selector=>selector==='[data-team-contribution-read]'?resultButton:null}};
 assert.equal(resultReads,0,'result fetched without explicit action');
 await statusNode.onclick(resultClick);assert.equal(resultReads,1);assert.equal(previews.length,1);
 assert.ok(previews[0].textContent.includes('<script>untrusted</script>'),'result was not rendered as text');assert.equal(previews[0].innerHTML,undefined);
 assert.ok(!statusHTML.includes('href='),'untrusted artifact ID became a link');
 formState.room={...formState.room,assignments:[{...completed,checkpoint_id:'replacement'}]};editor.draw(formState);assert.equal(previews.length,0,'invalidated result remained visible');
 formState.room={...formState.room,assignments:[completed]};editor.draw(formState);
 readResult=async()=>({State:'complete',Result:'x'.repeat(65537)});await statusNode.onclick(resultClick);assert.match(previews[0].textContent,/too large/);assert.ok(previews[0].textContent.length<256);
 readResult=async()=>({State:'complete',Result:Array.from({length:4097},()=>1)});await statusNode.onclick(resultClick);assert.match(previews[0].textContent,/too large/,'wide result bypassed preview work limit');
 readResult=async()=>({State:'complete',Result:{nested:'x'.repeat(65537)}});await statusNode.onclick(resultClick);assert.match(previews[0].textContent,/too large/,'nested string bypassed preview work limit');
 let releaseResult;readResult=()=>new Promise(resolve=>releaseResult=resolve);
 const staleRead=statusNode.onclick(resultClick);formState.room={...formState.room,assignments:[]};editor.draw(formState);releaseResult({State:'complete',Result:'PRIVATE_STALE_RESULT'});await staleRead;assert.equal(previews.length,0,'late result repopulated removed assignment');
 for(const patch of [{state:'running'},{checkpoint_id:''},{verification_id:''},{verification_level:'V99'}]){
  formState.room={...formState.room,assignments:[{...completed,...patch}]};editor.draw(formState);
  assert.ok(!element('[data-team-assignment-status]').innerHTML.includes('team-contribution-evidence'),'incomplete or unsupported evidence was rendered');
 }
 formState.room={...formState.room,assignments:[]};editor.draw(formState);
 assert.ok(!element('[data-team-assignment-status]').innerHTML.includes('checkpoint&lt;unsafe&gt;'),'removed evidence remained visible');
 assert.match(element('[data-team-plan-rows]').innerHTML,/data-plan-criteria/);
 criteria.value=JSON.stringify(draft[1].verification_spec);
 await element('[data-team-plan-form]').onsubmit({preventDefault(){}});
 assert.equal(JSON.stringify(formSaved[0].verification_spec),criteria.value,'form capture replaced additional checks');
 formState.proposedPlan={assignments:[{...formSaved[0],verification_spec:{artifacts:[{media_type:'<script>unsafe</script>'}]}}]};editor.draw(formState);
 const reviewHTML=element('[data-team-plan-review]').innerHTML;assert.ok(reviewHTML.includes('&lt;script&gt;unsafe&lt;/script&gt;'));assert.ok(!reviewHTML.includes('<script>'));
 formSaved=null;criteria.value='[]';await element('[data-team-plan-form]').onsubmit({preventDefault(){}});assert.equal(formSaved,null,'invalid criteria were submitted');assert.ok(criteria.error);
 criteria.value='{}';element('[data-team-plan-form]').oninput({target:criteria});assert.equal(criteria.error,'','corrected criteria remained blocked by form validity');
 formState.room={...formState.room,assignments:[completed]};editor.draw(formState);
 const closingRead=statusNode.onclick(resultClick);editor.dispose();releaseResult({State:'complete',Result:'PRIVATE_CLOSED_RESULT'});await closingRead;assert.equal(host.textContent,'');const closedHTML=statusHTML;editor.draw(formState);assert.equal(statusHTML,closedHTML,'disposed editor redrew private data');
 c.stop();const afterStop=posts.length;assert.equal(await c.proposeAssignments(assignments),false);assert.equal(posts.length,afterStop);
 assert.ok(fs.readFileSync('internal/webui/static/index.html','utf8').includes('/team-assignments.js'));
 console.log('PASS: Team assignment review, explicit approval, shared views, scoped inspection, no duplicate execution or automatic retries');
}
main().catch(e=>{console.error(e);process.exitCode=1});
