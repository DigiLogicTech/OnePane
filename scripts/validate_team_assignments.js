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
 // Publication reads are explicit, bound to the accepted assignment and an
 // independently authorized Task revision/scope, and validate every returned row.
 const outputCompletion={session_id:'session',workspace_id:'tenant',parent_task_id:'root',task_id:'child',plan_id:'plan',assignment_key:'build',member_id:'a',project_id:'project',project_workspace_id:'named',artifact_session_id:'',task_revision:7,checkpoint_id:'cp',verification_id:'ver',verification_level:'V2'};
 const outputRow={task_id:'child',relative_path:'reports/result.txt',asset_id:'asset/<unsafe>',version:2,content_hash:'a'.repeat(64),size_bytes:12,mime_type:'text/plain',published_at:10};
 const outputSnapshot=()=>({id:'session',task_id:'root',workspace_id:'tenant',accepted_plan_id:'plan',members:[{id:'a',kind:'agent'}],assignments:[{key:'build',member_id:'a',task_id:'child',state:'complete',checkpoint_id:'cp',verification_id:'ver',verification_level:'V2'}]});
 const outputTask=()=>({ID:'child',WorkspaceID:'tenant',ParentTaskID:'root',State:'complete',Revision:7,ProjectID:'project',ProjectWorkspaceID:'named',ArtifactSessionID:null});
 async function outputReader({mutateValue=()=>{},mutateTask=()=>{},throwAt='',defer=false}={}){
  const snapshot=outputSnapshot(),task=outputTask(),value={completion:{...outputCompletion},outputs:[{...outputRow}]},requests=[];let releaseOutput;
  mutateValue(value);mutateTask(task);
  const reader=controller({taskID:'root',workspaceID:'tenant',api:async path=>{
   requests.push(path);if(throwAt&&path.includes(throwAt))throw Error('access revoked');
   if(path==='/v1/tasks/root/team-session')return {ID:'session',TaskID:'root',WorkspaceID:'tenant'};
   if(path.endsWith('/live'))return snapshot;if(path.includes('/messages/page'))return {messages:[]};
   if(path==='/v1/tasks/child')return task;
   if(path==='/v1/team-sessions/session/assignments/child/outputs')return defer?new Promise(resolve=>releaseOutput=()=>resolve(value)):value;
   throw Error('unexpected publication read '+path);
  }});
  await reader.refresh();assert.equal(requests.filter(p=>p.endsWith('/outputs')).length,0,'polling fetched publications');
  return {reader,requests,release:()=>releaseOutput(),value};
 }
 let published=await outputReader();const output=await published.reader.readAssignmentOutputs('child');
 assert.equal(output.outputs[0].asset_id,outputRow.asset_id);assert.equal(output.completion.artifact_session_id,'','nullable Task scope did not match');
 assert.equal(published.requests.filter(p=>p.endsWith('/outputs')).length,1);published.reader.stop();
 published=await outputReader({mutateValue:v=>delete v.completion.artifact_session_id});assert.equal((await published.reader.readAssignmentOutputs('child')).completion.artifact_session_id,'','omitted empty Go artifact scope rejected');published.reader.stop();
 published=await outputReader({mutateTask:t=>t.ArtifactSessionID='artifact-session',mutateValue:v=>v.completion.artifact_session_id='artifact-session'});assert.equal((await published.reader.readAssignmentOutputs('child')).completion.artifact_session_id,'artifact-session');published.reader.stop();
 for(const key of Object.keys(outputCompletion)){
  published=await outputReader({mutateValue:v=>v.completion[key]=key==='task_revision'?8:'foreign'});
  assert.equal(await published.reader.readAssignmentOutputs('child'),null,'foreign completion accepted: '+key);published.reader.stop();
 }
 const badOutputCases={
  '33 receipts':v=>v.outputs=Array.from({length:33},(_,i)=>({...outputRow,relative_path:'file'+i})),
  'extra response property':v=>v.storage_uri='file:///private',
  'extra receipt property':v=>v.outputs[0].url='javascript:alert(1)',
  'extra completion property':v=>v.completion.raw_provenance='private',
  'foreign Task':v=>v.outputs[0].task_id='foreign',
  'unsafe version':v=>v.outputs[0].version=Number.MAX_SAFE_INTEGER+1,
  'zero version':v=>v.outputs[0].version=0,
  'negative size':v=>v.outputs[0].size_bytes=-1,
  'fractional size':v=>v.outputs[0].size_bytes=1.5,
  'invalid date':v=>v.outputs[0].published_at=-1,
  'prefixed hash':v=>v.outputs[0].content_hash='sha256:'+'a'.repeat(64),
  'uppercase hash':v=>v.outputs[0].content_hash='A'.repeat(64),
  'oversize ID':v=>v.outputs[0].asset_id='x'.repeat(257),
  'oversize UTF8 ID':v=>v.outputs[0].asset_id='🙂'.repeat(65),
  'oversize path':v=>v.outputs[0].relative_path='x'.repeat(4097),
  'oversize UTF8 path':v=>v.outputs[0].relative_path='🙂'.repeat(1025),
  'path traversal':v=>v.outputs[0].relative_path='../private',
  'absolute path':v=>v.outputs[0].relative_path='/private',
  'drive path':v=>v.outputs[0].relative_path='C:/private',
  'Windows path':v=>v.outputs[0].relative_path='reports\\private',
  'control path':v=>v.outputs[0].relative_path='private\nfile',
  'invalid surrogate':v=>v.outputs[0].relative_path='\ud800',
  'oversize MIME':v=>v.outputs[0].mime_type='x'.repeat(257),
  'header MIME':v=>v.outputs[0].mime_type='text/plain\r\nX-Private:yes',
  'duplicate receipt':v=>v.outputs.push({...v.outputs[0]}),
  'whole response bound':v=>v.outputs=Array.from({length:32},(_,i)=>({...outputRow,relative_path:'"'.repeat(4090)+i})),
  'huge untrusted payload':v=>v.secret='x'.repeat(262145),
 };
 for(const [name,mutateValue] of Object.entries(badOutputCases)){
  published=await outputReader({mutateValue});assert.equal(await published.reader.readAssignmentOutputs('child'),null,'unsafe publication accepted: '+name);published.reader.stop();
 }
 for(const mutateTask of [t=>t.ProjectID=null,t=>t.ProjectWorkspaceID=null,t=>t.Revision=8,t=>t.State='running',t=>t.ArtifactSessionID='foreign']){
  published=await outputReader({mutateTask});assert.equal(await published.reader.readAssignmentOutputs('child'),null,'foreign authorized Task metadata accepted');published.reader.stop();
 }
 for(const throwAt of ['/v1/tasks/child','/outputs']){published=await outputReader({throwAt});assert.equal(await published.reader.readAssignmentOutputs('child'),null,'revoked publication access retained');published.reader.stop()}
 for(const scenario of ['removed','plan','member','key','session','workspace','parent','checkpoint','verification','level','running','revoked','closed']){
  published=await outputReader({defer:true});const pending=published.reader.readAssignmentOutputs('child');
  // Await the explicit Task read before changing the view during output collection.
  await new Promise(resolve=>setImmediate(resolve));
  const next=outputSnapshot();if(scenario==='removed')next.assignments=[];
  if(scenario==='plan')next.accepted_plan_id='foreign';if(scenario==='member')next.assignments[0].member_id='foreign';if(scenario==='key')next.assignments[0].key='foreign';
  if(scenario==='session')next.id='foreign';if(scenario==='workspace')next.workspace_id='foreign';if(scenario==='parent')next.task_id='foreign';
  if(scenario==='checkpoint')next.assignments[0].checkpoint_id='foreign';if(scenario==='verification')next.assignments[0].verification_id='foreign';if(scenario==='level')next.assignments[0].verification_level='V3';if(scenario==='running')next.assignments[0].state='running';
  published.reader.state.room=scenario==='revoked'?null:next;if(scenario==='closed')published.reader.stop();published.release();
  assert.equal(await pending,null,'late publication response reopened evidence: '+scenario);published.reader.stop();
 }
 published=await outputReader();published.reader.state.room.assignments[0].state='running';const noRead=published.requests.length;
 assert.equal(await published.reader.readAssignmentOutputs('child'),null);assert.equal(await published.reader.readAssignmentOutputs('foreign'),null);assert.equal(published.requests.length,noRead,'ineligible publication dispatched');published.reader.stop();
 published=await outputReader({mutateValue:v=>v.outputs=[]});assert.equal((await published.reader.readAssignmentOutputs('child')).outputs.length,0);published.reader.stop();
 published=await outputReader({mutateValue:v=>v.outputs=Array.from({length:32},(_,i)=>({...outputRow,relative_path:'file'+i}))});assert.equal((await published.reader.readAssignmentOutputs('child')).outputs.length,32);published.reader.stop();
 // Exercise shipped form handlers with controlled DOM fields, including the
 // criteria capture and approval markup that controller-only checks omit.
 const elements=new Map(),element=selector=>{if(!elements.has(selector))elements.set(selector,{innerHTML:'',disabled:false,hidden:false});return elements.get(selector)};
 const criteria={value:'',error:'',setCustomValidity(message){this.error=message},reportValidity(){return !this.error},matches:s=>s==='[data-plan-criteria]'};
 const fields={
  'select[data-plan-member]':{value:'a'},'textarea':{value:'Build'},
  '[data-plan-verification]':{value:'V2'},'[data-plan-criteria]':criteria,
 };
 const row={querySelector:s=>fields[s],querySelectorAll:()=>[]};
 let previews=[],resultReads=0,publicationReads=0;
 const resultButton={dataset:{teamContributionRead:'child'},parentElement:{append:preview=>previews.push(preview)}};
 const publicationButton={dataset:{teamPublicationsRead:'child'},parentElement:resultButton.parentElement};
 const host={isConnected:true,querySelector:element,querySelectorAll:s=>s==='[data-plan-row]'?[row]:s==='[data-team-contribution-read]'?[resultButton]:s==='[data-team-publications-read]'?[publicationButton]:[],className:'',innerHTML:''};
 const statusNode=element('[data-team-assignment-status]');let statusHTML='';
 Object.defineProperty(statusNode,'innerHTML',{get:()=>statusHTML,set:value=>{statusHTML=value;previews=[]}});
 scope.document={createElement:()=>host};vm.runInContext(fs.readFileSync('internal/webui/static/team-assignments.js','utf8'),scope);
 let formSaved=null;
 const formState={room:{...room,task_state:'created',assignments:[]},messages:[],busy:false,proposedPlan:null};
 let readResult=async()=>({ID:'child',State:'complete',Result:{summary:'<script>untrusted</script>',artifact_id:'foreign-artifact'}});
 let readPublications=async()=>({completion:{...outputCompletion},outputs:[{...outputRow,relative_path:'reports/<img src=x>.txt'}]});
 const formController={state:formState,invalidatePlanReview(){formState.proposedPlan=null},assignmentDraft:()=>null,proposeAssignments:async value=>{formSaved=value},readAssignment:async()=>{resultReads++;return readResult()},readAssignmentOutputs:async()=>{publicationReads++;return readPublications()}};
 const editor=scope.OnePaneTeamAssignments.mount({append(){}},formController);editor.draw(formState);
 const completed={key:'build',member_id:'a',task_id:'child',objective:'Build',state:'complete',checkpoint_id:'checkpoint<unsafe>',verification_id:'verification<unsafe>',verification_level:'V0'};
 formState.room={...formState.room,assignments:[completed]};editor.draw(formState);
 let evidenceHTML=element('[data-team-assignment-status]').innerHTML;
 assert.ok(evidenceHTML.includes('V0 (declaration only)'));assert.ok(evidenceHTML.includes('checkpoint&lt;unsafe&gt;'));assert.ok(!evidenceHTML.includes('checkpoint<unsafe>'));
 assert.ok(evidenceHTML.includes('verification&lt;unsafe&gt;'));assert.ok(evidenceHTML.includes('data-team-assignment-open="child"'));
 scope.document.createElement=tag=>{assert.ok(['pre','section'].includes(tag));return {textContent:'',className:'',innerHTML:undefined}};
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
 // The UI receives only controller-validated records and derives fixed scoped
 // links. Caller-supplied artifact IDs, URLs and storage never become targets.
 formState.room={...formState.room,assignments:[completed]};editor.draw(formState);
 const publicationClick={target:{closest:selector=>selector==='[data-team-publications-read]'?publicationButton:null}};
 assert.equal(publicationReads,0,'UI fetched publications without explicit action');
 await statusNode.onclick(publicationClick);assert.equal(publicationReads,1);
 let publicationPanel=previews.find(p=>p.className==='team-recorded-publications');
 assert.ok(publicationPanel.innerHTML.includes('Task-associated recorded publications'));assert.ok(publicationPanel.innerHTML.includes('reports/&lt;img src=x&gt;.txt'));assert.ok(!publicationPanel.innerHTML.includes('<img'));
 assert.ok(publicationPanel.innerHTML.includes('/v1/projects/project/workspaces/named/library/asset%2F%3Cunsafe%3E/versions/2/preview'));
 assert.ok(publicationPanel.innerHTML.includes('/v1/projects/project/library/asset%2F%3Cunsafe%3E/versions/2/content?workspace_id=named'));assert.ok(publicationPanel.innerHTML.includes('rel="noopener"'));
 assert.ok(!publicationPanel.innerHTML.includes('artifact:'));assert.ok(!publicationPanel.innerHTML.includes('Attempt'));
 for(const [mime_type,size_bytes,preview] of [['application/octet-stream',12,false],['image/png',12,false],['text/plain',262145,false],['text/plain',262144,true],['Application/JSON; charset=utf-8',12,true],['text/html',12,true],['text/',12,false],['application/jsonp',12,false]]){
  readPublications=async()=>({completion:{...outputCompletion},outputs:[{...outputRow,mime_type,size_bytes}]});await statusNode.onclick(publicationClick);
  publicationPanel=previews.find(p=>p.className==='team-recorded-publications');
  assert.equal(publicationPanel.innerHTML.includes('>Preview</a>'),preview,'preview eligibility differs for '+mime_type+' '+size_bytes);
  assert.ok(publicationPanel.innerHTML.includes('>Download</a>'),'non-previewable publication lost authorized download');
 }
 readPublications=async()=>({completion:{...outputCompletion},outputs:[]});await statusNode.onclick(publicationClick);
 publicationPanel=previews.find(p=>p.className==='team-recorded-publications');assert.ok(publicationPanel.innerHTML.includes('No Task-associated publications were recorded.'));
 readPublications=async()=>null;await statusNode.onclick(publicationClick);publicationPanel=previews.find(p=>p.className==='team-recorded-publications');assert.ok(publicationPanel.innerHTML.includes('unavailable'));assert.ok(!publicationPanel.innerHTML.includes('href='),'revoked publications retained links');
 let releasePublications;readPublications=()=>new Promise(resolve=>releasePublications=resolve);
 const beforePendingPublications=publicationReads,latePublications=statusNode.onclick(publicationClick);await statusNode.onclick(publicationClick);assert.equal(publicationReads,beforePendingPublications+1,'duplicate publication click dispatched twice');
 formState.room={...formState.room,assignments:[{...completed,verification_id:'replacement'}]};editor.draw(formState);
 releasePublications({completion:{...outputCompletion},outputs:[{...outputRow}]});await latePublications;
 assert.equal(previews.filter(p=>p.className==='team-recorded-publications').length,0,'late publications reopened changed evidence');
 formState.room=null;editor.draw(formState);assert.equal(previews.length,0,'revoked Team view retained publication data');
 formState.room={...room,assignments:[]};editor.draw(formState);
 assert.match(element('[data-team-plan-rows]').innerHTML,/data-plan-criteria/);
 criteria.value=JSON.stringify(draft[1].verification_spec);
 await element('[data-team-plan-form]').onsubmit({preventDefault(){}});
 assert.equal(JSON.stringify(formSaved[0].verification_spec),criteria.value,'form capture replaced additional checks');
 formState.proposedPlan={assignments:[{...formSaved[0],verification_spec:{artifacts:[{media_type:'<script>unsafe</script>'}]}}]};editor.draw(formState);
 const reviewHTML=element('[data-team-plan-review]').innerHTML;assert.ok(reviewHTML.includes('&lt;script&gt;unsafe&lt;/script&gt;'));assert.ok(!reviewHTML.includes('<script>'));
 formSaved=null;criteria.value='[]';await element('[data-team-plan-form]').onsubmit({preventDefault(){}});assert.equal(formSaved,null,'invalid criteria were submitted');assert.ok(criteria.error);
 criteria.value='{}';element('[data-team-plan-form]').oninput({target:criteria});assert.equal(criteria.error,'','corrected criteria remained blocked by form validity');
 formState.room={...formState.room,assignments:[completed]};editor.draw(formState);
 const closingRead=statusNode.onclick(resultClick),closingPublications=statusNode.onclick(publicationClick);editor.dispose();releaseResult({State:'complete',Result:'PRIVATE_CLOSED_RESULT'});releasePublications({completion:{...outputCompletion},outputs:[{...outputRow}]});await Promise.all([closingRead,closingPublications]);assert.equal(host.textContent,'');const closedHTML=statusHTML;editor.draw(formState);assert.equal(statusHTML,closedHTML,'disposed editor redrew private data');
 c.stop();const afterStop=posts.length;assert.equal(await c.proposeAssignments(assignments),false);assert.equal(posts.length,afterStop);
 assert.ok(fs.readFileSync('internal/webui/static/index.html','utf8').includes('/team-assignments.js'));
 console.log('PASS: Team assignments, bounded explicit publication navigation, exact completion/scope, late reply and revoked access fencing, safe derived links, no execution or automatic retries');
}
main().catch(e=>{console.error(e);process.exitCode=1});
