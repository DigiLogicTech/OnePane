// Regression coverage for the installed QA findings and follow-up controls.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const read=p=>fs.readFileSync('internal/webui/static/'+p,'utf8');
const app=read('app.js'),models=read('models-page.js'),inspector=read('attention-inspector.js');
const nav={innerHTML:''};let route='projects',persisted=0;
const ctx=vm.createContext({state:{},currentTab:()=>({route}),a31Array:v=>v||[],
 qa4ProjectHub:{projects:[{id:'a',name:'Alpha'},{id:'b',name:'Beta'}]},
 qa4Workspaces:p=>[{id:p.id+'-ws',name:p.name+' workspace'}],
 $:()=>nav,$$:()=>[],escapeHtml:String,qa5T:(_,s)=>s,persist:()=>persisted++,
 navItems:[['projects','P','Projects'],['models','M','Models']],a31ModelView:'local'});
vm.runInContext(app.slice(app.indexOf('function a34NavTreeState('),app.indexOf('/* Shared deterministic component layout. */')),ctx);
for(route of ['projects','workspaces','models','operations']){
 ctx.renderNav();assert.match(nav.innerHTML,/data-a34-nav-toggle="projects"/);
 assert.match(nav.innerHTML,/Alpha workspace/);assert.match(nav.innerHTML,/Beta workspace/);
}
ctx.a34SetNavExpanded('projects',false);assert.doesNotMatch(nav.innerHTML,/project-nav-tree/);
assert.match(nav.innerHTML,/title="Expand Projects"/);assert.ok(persisted);
ctx.a34SetNavExpanded('projects',true);assert.match(nav.innerHTML,/title="Collapse Projects"/);

const date=vm.runInNewContext(inspector.slice(inspector.indexOf('function a9Date('),inspector.indexOf('function a9Field('))+'a9Date;');
assert.ok(date(1793218203361).includes('2026'));assert.equal(date(null),'');assert.equal(date('bad'),'Not reported');

// Exercise the real OAuth callbacks: setup must bind save after opening its
// modal, and completion must work through polling without a browser opener.
let html='',saved=null,poll=null,connected=false,closed=0,opened='';
const controls=new Map(),node=key=>{if(!controls.has(key))controls.set(key,{isConnected:true,textContent:'',querySelector:()=>({disabled:false})});return controls.get(key)};
class FormDataMock{get(key){return {preset:'provider',auth:'https://id.example/authorize',token:'https://id.example/token',client:'public-client',scopes:'openid profile',enabled:'on'}[key]}}
const oauth=vm.createContext({URL,onepaneWorkspace:'ws',location:{origin:'http://127.0.0.1:18183'},a31Array:v=>v||[],escapeHtml:String,
 openModal:(_,value)=>{html=value},$:node,FormData:FormDataMock,closeModal:()=>closed++,notice:()=>{},
 currentTab:()=>({route:'models'}),renderModels:()=>{},a39WebOpen:url=>opened=url,
 setTimeout:fn=>{poll=fn},apiRequest:async(url,opt)=>{
  if(opt?.method==='PUT'){saved=JSON.parse(opt.body);return {}}
  if(url.endsWith('/start'))return {authorization_url:'https://id.example/authorize?state=opaque',expires_at:Date.now()+60000};
  return connected?[{id:'new-connection',preset_id:'provider',status:'connected'}]:[];
 }});
vm.runInContext(models.slice(models.indexOf('let a31OAuthAttempt='),models.indexOf('function a31OpenSecrets(')),oauth);
(async()=>{
 oauth.a31OpenOAuthConfig('provider');await node('#a31OAuthConfigForm').onsubmit({preventDefault(){}});
 assert.equal(saved.client_id,'public-client');assert.deepEqual(saved.scopes,['openid','profile']);assert.equal(closed,1);
 await oauth.a31StartOAuth('provider');assert.match(html,/Continue to sign-in/);
 node('#a31OAuthOpen').onclick();assert.match(opened,/^https:\/\/id.example/);
 connected=true;await poll();assert.equal(closed,2,'OAuth completion should close its modal without window.opener');
 console.log('PASS: Projects disclosure on every route, timestamp formatting, OAuth setup save and external-browser completion');
})().catch(e=>{console.error(e);process.exitCode=1});
