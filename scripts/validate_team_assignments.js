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
  if(path==='/v1/tasks/root/team-session')return {ID:'session'};
  if(path==='/v1/tasks/child')return {ID:'child',WorkspaceID:'tenant',Objective:'Build'};
  if(path.endsWith('/live'))return room;
  if(path.includes('/messages/page'))return {messages:[],has_more:false};
  throw Error('unexpected read: '+path);
 };
 const c=controller({api,taskID:'root'});await c.refresh();
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
 c.stop();const afterStop=posts.length;assert.equal(await c.proposeAssignments(assignments),false);assert.equal(posts.length,afterStop);
 assert.ok(fs.readFileSync('internal/webui/static/index.html','utf8').includes('/team-assignments.js'));
 console.log('PASS: Team assignment review, explicit approval, shared views, scoped inspection, no duplicate execution or automatic retries');
}
main().catch(e=>{console.error(e);process.exitCode=1});
