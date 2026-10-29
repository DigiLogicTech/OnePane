'use strict';
// Execute the real Tasks renderer across a live transition, preserving filters
// and the archived view. A pure filter-helper test cannot catch stale options.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const root=path.resolve(__dirname,'..');
const source=fs.readFileSync(path.join(root,'internal/webui/static/tasks-page.js'),'utf8');
const code=source.slice(source.indexOf('function qaTaskFilterRows('),source.indexOf('qa4OpenScheduledTask=async function'));
const elements=new Map();
function element(id){
  if(!elements.has(id))elements.set(id,{value:'',innerHTML:'',textContent:'',isConnected:true});
  return elements.get(id);
}
const state=element('#taskStateFilter');
let stateMarkup='';
Object.defineProperty(state,'innerHTML',{get:()=>stateMarkup,set:v=>{stateMarkup=v;state.value='';}});
let route='tasks';
const tabs=['current','scheduled','archived'].map(tab=>({dataset:{taskTab:tab},classList:{toggle(){}}}));
const task={id:'t1',objective:'Build UI',project_id:'p1',state:'created'};
const archived={id:'t2',objective:'Archived fixture',state:'complete'};
const escapeHtml=v=>String(v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const context={
  $:element,$$:selector=>selector==='[data-task-tab]'?tabs:[],
  currentTab:()=>({route}),liveOps:{tasks:[],reported:{}},
  qa4ProjectHub:{projects:[{id:'p1',name:'QA Project'}]},
  onepaneWorkspace:'ws1',pageHeader:()=>'',escapeHtml,
  titleCase:v=>v.replace(/\b\w/g,c=>c.toUpperCase()),
  qaTaskProjectName:()=> 'QA Project',qa4LoadProjectHub:async()=>{},
  apiRequest:async url=>url.includes('/routines')?[]:url.includes('archived=1')?[archived]:[task],
  syncLiveNotifications(){},bindViewActions(){},openNewTask(){},qa4OpenScheduledTask(){},qaTaskNormaliseRoutine:r=>r,
};
(async()=>{
  vm.runInNewContext(code+'\nthis.render=renderTasks;this.refresh=qaTaskRefreshLiveView;',context);
  await context.render();
  assert(stateMarkup.includes('value="created"'));
  element('#taskSearch').value='BUILD';
  element('#taskProjectFilter').value='p1';
  state.value='created';
  context.liveOps.tasks=[{...task,state:'blocked'}];
  context.refresh();
  assert(stateMarkup.includes('value="blocked"'),'live Blocked option must appear without re-entering Tasks');
  assert.equal(state.value,'created','an applied filter must not be silently cleared');
  assert.equal(element('#taskSearch').value,'BUILD');
  assert.equal(element('#taskProjectFilter').value,'p1');
  assert.equal(element('#taskResultCount').textContent,'0 of 1 loaded tasks');
  state.value='blocked';state.onchange();
  assert.equal(element('#taskResultCount').textContent,'1 of 1 loaded tasks');
  assert(element('#tasksBody').innerHTML.includes('Blocked'));
  element('#taskSearch').value='';element('#taskProjectFilter').value='';state.value='';
  tabs[2].onclick();context.refresh();
  assert(element('#tasksBody').innerHTML.includes('Archived fixture'),'live refresh must preserve archived tab');
  const before=element('#tasksBody').innerHTML;
  route='projects';context.refresh();assert.equal(element('#tasksBody').innerHTML,before);
  route='tasks';element('#tasksBody').isConnected=false;
  context.liveOps.tasks=[];context.refresh();assert.equal(element('#tasksBody').innerHTML,before);
  const foundation=fs.readFileSync(path.join(root,'internal/webui/static/app-foundation.js'),'utf8');
  const refreshCode=foundation.slice(foundation.indexOf('async function refreshOperationalDataQA('),foundation.indexOf('function normalizedWorkspace('));
  assert(refreshCode.includes('qaTaskRefreshLiveView()'),'operational polling must notify the Tasks renderer');
  const healthCode=foundation.slice(foundation.indexOf('function applyHealthStatus('),foundation.indexOf('function titleCase('));
  const buttons=[{classes:{},text:{textContent:''}},{classes:{},text:{textContent:''}}];
  for(const button of buttons)button.classList={toggle:(name,on)=>{button.classes[name]=on;}};
  const healthContext={$:(selector,button)=>selector==='#healthButton'?buttons[0]:selector==='#mobileHealthButton'?buttons[1]:selector==='span:last-child'?button.text:null};
  vm.runInNewContext(healthCode+'\nthis.apply=applyHealthStatus;',healthContext);
  for(const [status,label] of [['ok','Healthy'],['degraded','Degraded'],['halted','Halted'],['unknown','Unknown'],['ok','Healthy']]){
    healthContext.apply({status,reason:status==='halted'?'worker halted':''});
    for(const button of buttons){
      assert.equal(button.text.textContent,label);
      assert.equal(button.classes['health-ok'],status==='ok');
      assert.equal(button.classes['health-halted'],status==='halted');
      if(status==='halted')assert.equal(button.title,'Halted: worker halted');
    }
  }
  assert(refreshCode.includes('applyHealthStatus({status:liveOps.health,reason:liveOps.healthReason})'),'live health polling must update both shell badges');
  console.log('Live task transition, state options, retained filters, archived tab and navigation fences PASS');
})().catch(error=>{console.error(error);process.exitCode=1;});
