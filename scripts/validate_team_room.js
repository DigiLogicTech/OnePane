// Behaviour tests for the actual Team-room controller and its public API calls.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const context=vm.createContext({AbortController,Map,Array,JSON,Number,String,Promise,Error});
vm.runInContext(fs.readFileSync('internal/webui/static/team-room.js','utf8'),context);
const foundation=fs.readFileSync('internal/webui/static/app-foundation.js','utf8');
new vm.Script(foundation,{filename:'app-foundation.js'});
const inspector=foundation.slice(foundation.indexOf('function qa4InspectorOverview(){'),foundation.indexOf('function qa4InspectorConfiguration(){'));
const inspectorContext=vm.createContext({qa4Inspector:{kind:'task',id:'task'},qa4FindEntity:()=>({state:'CREATED'}),escapeHtml:String,titleCase:String});
vm.runInContext(inspector,inspectorContext);
assert.match(vm.runInContext('qa4InspectorOverview()',inspectorContext),/data-team-open-task="task"/,'canonical Task Inspector must open the live room');
const {controller}=context.OnePaneTeamRoom;
assert.equal(context.OnePaneTeamRoom.guidanceRecipient({Kind:'human',Content:'{"text":"Check","target_member_id":"reviewer"}'}),'reviewer');
assert.equal(context.OnePaneTeamRoom.guidanceRecipient({Kind:'agent',Content:{target_member_id:'reviewer'}}),'');
const message=i=>({ID:'m'+String(i).padStart(4,'0'),sequence:i+1,SessionID:'room',Kind:'agent',Content:{text:'Contribution '+i},CreatedAt:100+Math.floor(i/60),RoundNumber:1,AuthorMemberID:'builder'});
async function main(){
 let calls=[],mutations=[],failReads=false,alive=true,room={id:'room',task_id:'task',status:'deliberating',execution_mode:'team',task_state:'CREATED',members:[{id:'builder',name:'Builder',kind:'agent',status:'running'},{id:'reviewer',name:'Reviewer',kind:'agent',status:'pending'}]},messages=Array.from({length:261},(_,i)=>message(i));
 let holdMutation=null;
 const api=async(path,opts={})=>{
  calls.push(path);
  if(opts.method==='POST'){mutations.push({path,body:JSON.parse(opts.body)});if(holdMutation)await holdMutation;return {}}
  if(failReads)throw Error('Access revoked');
  if(path.includes('/tasks/'))return {ID:'room'};
  if(path.endsWith('/live'))return {...room};
  const url=new URL(path,'http://onepane.invalid');let rows=messages;const before=url.searchParams.get('before'),after=url.searchParams.get('after'),limit=Number(url.searchParams.get('limit'));
  if(before)rows=rows.filter(m=>m.ID<before);if(after)rows=rows.filter(m=>m.ID>after);
  const has_more=rows.length>limit;rows=after?rows.slice(0,limit):rows.slice(-limit);
  return {messages:rows,has_more,first_id:rows[0]?.ID,last_id:rows.at(-1)?.ID};
 };
 let notifications=0;const c=controller({api,taskID:'task',alive:()=>alive,onChange:()=>notifications++});
 await c.refresh();assert.equal(c.state.messages[0].ID,'m0231');assert.equal(c.state.messages.at(-1).ID,'m0260');
 c.state.draft='Keep this draft';c.state.selectedAgent='reviewer';const beforeCalls=calls.length;
 c.switchView('agents');c.switchView('conversation');assert.equal(calls.length,beforeCalls,'view switch must not dispatch or read');assert.equal(c.state.draft,'Keep this draft');assert.equal(c.state.selectedAgent,'reviewer');
 await c.older();assert.equal(c.state.history.first_id,'m0201');const historyID=c.state.history.first_id;
 messages.push(message(261));await c.refresh();assert.equal(c.state.messages.at(-1).ID,'m0261');assert.equal(c.state.history.first_id,historyID,'live update replaced the reader history');
 await c.refresh();assert.equal(new Set(c.state.messages.map(m=>m.ID)).size,c.state.messages.length,'reconnect duplicated messages');
 c.live();assert.equal(c.state.history,null);
 await c.ask();assert.deepEqual(mutations.at(-1).body,{member_ids:['reviewer']});
 c.state.selectedAgent='foreign-agent';const beforeMutation=mutations.length;assert.equal(await c.ask(),false);assert.equal(mutations.length,beforeMutation,'forged target dispatched');
 c.state.selectedAgent='reviewer';let release;holdMutation=new Promise(r=>release=r);c.state.draft='First message';const sending=c.send();
 c.state.draft='New text written during send';assert.equal(await c.send(),false,'duplicate submission admitted');release();await sending;holdMutation=null;assert.equal(c.state.draft,'New text written during send','async completion erased a newer draft');
 room={...room,status:'plan_accepted'};await c.refresh();assert.equal(await c.ask(),false,'accepted execution silently reopened');
 c.state.guidanceTarget='reviewer';c.state.draft='Check the accepted contribution';
 assert.equal(await c.send(),true,'accepted Team rejected human guidance');
 assert.deepEqual(mutations.at(-1).body,{content:{text:'Check the accepted contribution',target_member_id:'reviewer'}});
 c.switchView('agents');c.switchView('conversation');assert.equal(c.state.guidanceTarget,'reviewer','view switch lost guidance recipient');
 c.state.guidanceTarget='foreign';c.state.draft='Keep this text';const guidanceCount=mutations.length;
 assert.equal(await c.send(),false);assert.equal(mutations.length,guidanceCount,'foreign guidance target dispatched');
 c.state.guidanceTarget='';room={...room,status:'executing'};await c.refresh();assert.equal(await c.send(),true,'executing Team rejected shared guidance');
 assert.deepEqual(mutations.at(-1).body,{content:{text:'Keep this text'}});
 room={...room,status:'complete'};await c.refresh();c.state.draft='New text written during send';const completedCount=mutations.length;
 assert.equal(await c.send(),false);assert.equal(mutations.length,completedCount,'completed Team admitted guidance');
 failReads=true;await c.refresh();assert.equal(c.state.room,null);assert.equal(c.state.messages.length,0);assert.equal(c.state.history,null);assert.ok(c.state.error);assert.equal(c.state.draft,'New text written during send');
 failReads=false;room={...room,status:'deliberating'};await c.refresh();assert.equal(c.state.messages.at(-1).ID,'m0261','recovery lost newest window');
 const beforeStop=calls.length;c.stop();await c.refresh();assert.equal(calls.length,beforeStop,'closed room polled');
 let resolveRead;const slow=controller({api:()=>new Promise(r=>resolveRead=r),taskID:'task',alive:()=>alive,onChange:()=>{throw Error('detached update')}});const pending=slow.refresh();alive=false;resolveRead({ID:'room'});await pending;slow.stop();
 const index=fs.readFileSync('internal/webui/static/index.html','utf8');assert.ok(index.includes('/team-room.js'));assert.ok(index.includes('/team-room.css'));
 console.log('PASS: live Team views, long history, targeted replies, draft retention, no duplicate dispatch, revocation and disposal');
}
main().catch(e=>{console.error(e);process.exitCode=1});
