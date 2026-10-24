// Exercise the shipped read-only controller and mounted lifecycle. No browser,
// model execution or installed-platform acceptance is implied by these checks.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/webui/static/task-recovery.js','utf8');
const context=vm.createContext({AbortController,Date,Number,String,Array,Error,Promise});
vm.runInContext(source,context);
const {controller,content}=context.OnePaneTaskRecovery;
const row=i=>({id:'case-'+String(i).padStart(3,'0'),task_id:'task /one',attempt_id:'original-'+i,phase:'awaiting_inventory',task_revision_at_capture:7,created_at:12345,reason:'SECRET_REASON',effects:{prompt:'SECRET_PROMPT'}});
async function main(){
 let records=Array.from({length:65},(_,i)=>row(i)),calls=[],fail=false,hold=null,alive=true,scope='tenant',detailOverride=null,pageOverride=null;
 const api=async(path,options)=>{
  calls.push({path,options});assert.equal(options.method,'GET');assert.equal(options.cache,'no-store');assert.ok(options.signal);
  if(hold)await hold;if(fail)throw Error('PRIVATE_DATABASE_ERROR SECRET_REASON');
  if(path==='/v1/tasks/task%20%2Fone')return {id:'task /one',workspace_id:scope};
  const url=new URL(path,'http://onepane.invalid');
  if(url.pathname.endsWith('/recovery-cases')){
   if(pageOverride)return pageOverride;
   const after=url.searchParams.get('after')||'',rows=records.filter(r=>r.id>after);
   return {cases:rows.slice(0,32),next_after:rows.length>32?rows[31].id:''};
  }
  if(url.pathname.includes('/recovery-cases/'))return detailOverride||records.find(r=>r.id===decodeURIComponent(url.pathname.split('/').at(-1)));
  throw Error('Unexpected request '+path);
 };
 let emissions=0;const c=controller({api,taskID:'task /one',workspaceID:'tenant',alive:()=>alive,onChange:()=>emissions++});
 await c.first();assert.equal(c.state.cases.length,32);assert.equal(c.state.nextAfter,'case-031');assert.equal(c.state.after,'');assert.ok(c.state.observedAt);
 assert.ok(!JSON.stringify(c.state).includes('SECRET_'),'private fields copied into client state');
 let html=content(c.state);assert.match(html,/Awaiting effect inventory/);assert.match(html,/data-recovery-next/);assert.ok(!html.includes('SECRET_'));
 await c.next();assert.equal(c.state.cases.length,32);assert.equal(c.state.cases[0].id,'case-032');assert.equal(c.state.cases.at(-1).id,'case-063');assert.equal(c.state.after,'case-031');
 await c.next();assert.equal(c.state.cases.length,1);assert.equal(c.state.cases[0].id,'case-064');assert.equal(c.state.nextAfter,'');
 const noNext=calls.length;await c.next();await c.inspect('foreign');assert.equal(calls.length,noNext,'unlisted case queried or pagination ran past end');
 await c.inspect('case-064');assert.equal(c.state.selected.id,'case-064');assert.match(content(c.state),/Back to records/);
 await c.refresh();assert.equal(c.state.selected.id,'case-064','detail refresh silently changed the viewed record');
 await c.list();assert.equal(c.state.selected,null);assert.equal(c.state.cases[0].id,'case-064');
 await c.first();assert.equal(c.state.after,'');assert.equal(c.state.cases[0].id,'case-000');
 let release;hold=new Promise(r=>release=r);const reading=c.refresh(),before=calls.length;await c.next();await c.inspect('case-000');await c.refresh();assert.equal(calls.length,before,'overlapping requests duplicated reads');release();await reading;hold=null;
 fail=true;await c.refresh();assert.equal(c.state.cases.length,0);assert.equal(c.state.nextAfter,'');assert.equal(c.state.observedAt,0);assert.match(c.state.error,/unavailable/);assert.ok(!JSON.stringify(c.state).includes('PRIVATE_'),'raw API error displayed');
 const failedReads=calls.length;await Promise.resolve();assert.equal(calls.length,failedReads,'read error retried itself');fail=false;await c.first();assert.equal(c.state.error,'');
 // Reject corrupt/foreign pages as a whole; never show an accepted prefix.
 for(const page of [
  {cases:[{...row(0),task_id:'foreign'}]}, {cases:[{...row(0),attempt_id:''}]},
  {cases:[{...row(0),phase:'verified'}]}, {cases:[{...row(0),task_revision_at_capture:0}]},
  {cases:[{...row(0),created_at:'123'}]}, {cases:[{...row(0),id:'x'.repeat(161)}]},
  {cases:[row(1),row(0)]}, {cases:[row(0),row(0)]}, {cases:records.slice(0,33)},
  {cases:[row(0)],next_after:'case-000'}, {cases:records.slice(0,32),next_after:'case-000'},
  {cases:[],next_after:{}}, {cases:null}
 ]){pageOverride=page;await c.first();assert.equal(c.state.cases.length,0);assert.equal(c.state.selected,null);assert.ok(c.state.error)}
 pageOverride=null;await c.first();
 for(const bad of [{...row(0),id:'foreign'},{...row(0),task_id:'foreign'},{...row(0),attempt_id:null}]){
  detailOverride=bad;await c.inspect('case-000');assert.equal(c.state.selected,null);assert.equal(c.state.cases.length,0);assert.ok(c.state.error);detailOverride=null;await c.first();
 }
 scope='foreign';const beforeScope=calls.length;await c.refresh();assert.equal(calls.length,beforeScope+1,'wrong tenant Task queried for recovery cases');assert.equal(c.state.cases.length,0);scope='tenant';await c.first();
 // Workspace changes and disposal fence even an API implementation ignoring abort.
 hold=new Promise(r=>release=r);const late=c.refresh(),beforeEmissions=emissions;alive=false;release();await late;hold=null;
 assert.equal(c.state.cases.length,0);assert.equal(c.state.selected,null);assert.equal(emissions,beforeEmissions,'late response emitted into another Workspace');
 const beforeStop=calls.length;c.stop();await c.first();assert.equal(calls.length,beforeStop);assert.ok(calls.at(-1).options.signal.aborted);
 const lateController=controller({api,taskID:'task /one',workspaceID:'tenant'});hold=new Promise(r=>release=r);const lateRead=lateController.first();lateController.stop();release();await lateRead;hold=null;assert.equal(lateController.state.cases.length,0);
 // Injected identity characters cannot become markup or executable controls.
 const escaped=content({cases:[{...row(0),id:'"><img src=x onerror=evil()>',attempt_id:'<script>evil()</script>'}],busy:false,observedAt:1,after:'',nextAfter:'',error:'',selected:null});
 assert.ok(!escaped.includes('<img'));assert.ok(!escaped.includes('<script>'));assert.match(escaped,/&lt;script&gt;/);assert.match(content({cases:[],selected:null,after:'',nextAfter:'',busy:false,error:'',observedAt:1}),/does not establish that the Task is safe to retry/);
 // Run the actual mount with a minimal DOM to verify teardown and tenant fences.
 let observerCallback,timerCallback,disconnected=false,cleared=false,scopeAtMount='tenant',clickListener;
 const host={isConnected:true,innerHTML:'',textContent:'',classList:{add(){}},contains:()=>true,querySelectorAll:()=>[]};
 const mountScope=vm.createContext({AbortController,Date,Number,String,Array,Error,Promise,
  apiRequest:api,onepaneWorkspace:'tenant',qa4InspectorOverview:()=>'<p>Task details</p>',qa4Inspector:{kind:'task',id:'task /one'},
  document:{body:{},hidden:false,addEventListener:(_kind,fn)=>{clickListener=fn}},
  MutationObserver:class{constructor(fn){observerCallback=fn}observe(){}disconnect(){disconnected=true}},
  setTimeout:fn=>{timerCallback=fn;return 42},clearTimeout:()=>{cleared=true}
 });
 vm.runInContext(source,mountScope);assert.match(vm.runInContext('qa4InspectorOverview()',mountScope),/data-task-recovery-open="task \/one"/);assert.equal(typeof clickListener,'function');
 const mounted=mountScope.OnePaneTaskRecovery.mountInto(host,'task /one',scopeAtMount);
 for(let i=0;i<8;i++)await Promise.resolve();assert.match(host.innerHTML,/Awaiting effect inventory/);
 fail=true;host.onclick({target:{closest:()=>({hasAttribute:key=>key==='data-recovery-refresh'})}});
 for(let i=0;i<8;i++)await Promise.resolve();assert.ok(!host.innerHTML.includes('original-0'),'mounted denied read retained recovery data');fail=false;
 mountScope.onepaneWorkspace='other';timerCallback();assert.equal(host.textContent,'Recovery inspection closed.');assert.equal(host.onclick,null);assert.ok(disconnected&&cleared);assert.equal(mounted.controller.state.cases.length,0);
 host.isConnected=false;observerCallback();
 const index=fs.readFileSync('internal/webui/static/index.html','utf8');assert.match(index,/src="\/task-recovery.js"/);assert.match(index,/href="\/task-recovery.css"/);
 console.log('PASS: scoped recovery inspection, bounded pages, explicit detail, no mutations, revocation, malformed evidence, late responses and mounted disposal');
}
main().catch(error=>{console.error(error);process.exitCode=1});
