const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync('internal/webui/static/library-text-index.js','utf8');
const context=vm.createContext({TextEncoder});vm.runInContext(source,context);
const selection={projectID:'project',workspaceID:'workspace',assetID:'asset',version:1};
const hash='sha256:'+'a'.repeat(64);
const metadata=()=>({asset_id:'asset',version:1,parser_version:1,content_hash:hash,size_bytes:40,status:'ready',line_count:2});
const citation=()=>({line_number:2,byte_start:10,byte_end:23,text:'<b>source</b>',line_hash:hash});
const result=q=>({index:metadata(),query:q,lines:[citation()],has_more:false});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {resolve,promise}};
const settle=async()=>{for(let i=0;i<8;i++)await Promise.resolve()};

class Element{
 constructor(tag){this.tagName=tag;this.children=[];this.parent=null;this.events={};this.attributes={};this._text='';this.connected=false;this.disabled=false;this.value='';}
 get isConnected(){return this.connected||!!this.parent?.isConnected}
 set innerHTML(_){throw Error('Untrusted text renderer used HTML')}
 get textContent(){return this._text+this.children.map(x=>x.textContent).join(' ')}
 set textContent(value){this.replaceChildren();this._text=String(value)}
 append(...items){for(const child of items){child.parent=this;this.children.push(child)}}
 prepend(child){child.parent=this;this.children.unshift(child)}
 replaceChildren(...items){for(const child of this.children)child.parent=null;this.children=[];this._text='';this.append(...items)}
 setAttribute(key,value){this.attributes[key]=value}
 addEventListener(key,fn){this.events[key]=fn}
}
function descendants(root){return [root,...root.children.flatMap(descendants)]}

(async()=>{
 let calls=[],answer=metadata(),alive=true,changes=0;
 const c=context.createLibraryTextIndexController({selection,api:async(path,options)=>{calls.push({path,options});return answer},current:()=>alive,change:()=>changes++});
 assert.equal(calls.length,0,'mount must not read source automatically');
 await c.status();assert.equal(c.snapshot().index.status,'ready');
 await c.index();assert.equal(calls.at(-1).options.method,'POST');assert.ok(!('body' in calls.at(-1).options),'client supplies no replacement bytes');
 answer=result('source');await c.search('source');assert.equal(c.snapshot().lines[0].text,'<b>source</b>');
 assert.match(calls.at(-1).path,/\/versions\/1\/search\?q=source$/);
 let before=calls.length;await c.search('😀'.repeat(65));assert.equal(calls.length,before,'UTF-8 byte bound bypassed');assert.equal(c.snapshot().lines.length,0);
 await c.search('\0');assert.equal(calls.length,before);
 for(const mutate of [x=>x.index.asset_id='foreign',x=>x.index.version=2,x=>x.index.parser_version=2,x=>x.index.size_bytes=-1,x=>x.index.size_bytes=1048577,x=>x.query='different',x=>x.lines[0].byte_end=100,x=>x.lines[0].line_hash='bad',x=>x.lines=[citation(),citation()],x=>x.lines=Array.from({length:9},citation)]){
  answer=result('source');mutate(answer);await c.search('source');assert.equal(c.snapshot().lines.length,0);assert.ok(c.snapshot().error);
 }
 answer=result('source');await c.search('source');const copy=c.snapshot();copy.lines[0].text='mutated';assert.equal(c.snapshot().lines[0].text,'<b>source</b>');
 const gate=deferred();let pendingCalls=0;
 const pending=context.createLibraryTextIndexController({selection,api:()=>{pendingCalls++;return gate.promise},change:()=>{}});
 const reading=pending.status();await pending.index();await pending.search('source');assert.equal(pendingCalls,1,'duplicate actions dispatched');
 pending.dispose();gate.resolve(metadata());await reading;assert.equal(pending.snapshot().index,null);
 const late=deferred();const stale=context.createLibraryTextIndexController({selection,api:()=>late.promise,current:()=>alive,change:()=>changes++});
 const loading=stale.status();alive=false;before=changes;late.resolve(metadata());await loading;assert.equal(changes,before);assert.equal(stale.snapshot().index,null);alive=true;

 const document={createElement:tag=>new Element(tag)};let handler=async path=>path.includes('/search?')?result('source'):metadata();calls=[];
 const mounted=vm.createContext({TextEncoder,document,apiRequest:async(path,options)=>{calls.push({path,options});return handler(path,options)}});
 vm.runInContext(source,mounted);
 const host=new Element('section');host.connected=true;
 const mount=mounted.mountLibraryTextIndex(host,selection);
 assert.equal(calls.length,0);
 const buttons=descendants(host).filter(x=>x.tagName==='button');
 assert.deepEqual(buttons.map(x=>x.textContent),['Check indexing','Index text','Search document']);
 assert.equal(buttons[2].disabled,true);
 await buttons[0].events.click();assert.equal(buttons[2].disabled,false);assert.match(host.textContent,/Text indexed/);
 const input=descendants(host).find(x=>x.tagName==='input');input.value='source';
 const form=descendants(host).find(x=>x.tagName==='form');form.events.submit({preventDefault(){}});await settle();
 const preview=descendants(host).find(x=>x.tagName==='pre');assert.equal(preview.textContent,'<b>source</b>');assert.match(host.textContent,/Line 2/);assert.match(host.textContent,/bytes 10–23/);
 handler=async()=>{throw Error('Access revoked')};await buttons[0].events.click();assert.ok(!host.textContent.includes('<b>source</b>'),'denial retained private excerpts');assert.match(host.textContent,/Access revoked/);
 const waiting=deferred();handler=()=>waiting.promise;const action=buttons[1].events.click();await settle();assert.ok(buttons.every(b=>b.disabled));
 host.connected=false;waiting.resolve(metadata());await action;assert.ok(!host.textContent.includes('Text indexed'),'late detached response rendered');mount.dispose();
 const workspace=fs.readFileSync('internal/webui/static/workspace-library.js','utf8');
 assert.ok(workspace.includes('root.textContent="Loading authorised Library…"'),'new scope retained private old content');
 assert.ok(workspace.includes('if(!current())return;'),'late replacement scope not fenced');
 const html=fs.readFileSync('internal/webui/static/index.html','utf8');assert.ok(html.indexOf('/library-text-index.js')<html.indexOf('/workspace-library.js'));
 const token='A'.repeat(43),workspacePage=(more=false)=>({query:'source',matches:[{index:metadata(),line:citation()}],has_more:more,next_cursor:more?token:''});
 let pages=[],response={query:'source',matches:[],has_more:true,next_cursor:token};
 const searchController=context.createWorkspaceTextSearchController({selection,api:async path=>{pages.push(path);return response}});
 assert.equal(pages.length,0);await searchController.start('source');assert.equal(searchController.snapshot().hasMore,true);assert.equal(searchController.snapshot().matches.length,0);
 response=workspacePage();await searchController.next();assert.match(pages[1],/cursor=A{43}$/);assert.equal(searchController.snapshot().matches.length,1);assert.equal(searchController.snapshot().hasMore,false);
 const copied=searchController.snapshot();copied.matches[0].line.text='changed';assert.equal(searchController.snapshot().matches[0].line.text,'<b>source</b>');
 before=pages.length;await searchController.start('😀'.repeat(65));assert.equal(pages.length,before);assert.equal(searchController.snapshot().matches.length,0);
 for(const mutate of [p=>p.query='foreign',p=>p.next_cursor='PRIVATE_RAW_ASSET_POSITION',p=>p.matches[0].index.content_hash='bad',p=>p.matches[0].line.byte_end=999,p=>p.matches.push(p.matches[0])]){
  response=workspacePage(true);mutate(response);await searchController.start('source');assert.ok(searchController.snapshot().error);assert.equal(searchController.snapshot().matches.length,0);assert.equal(searchController.snapshot().hasMore,false);
 }
 const latePage=deferred();let workspaceAlive=true,workspaceChanges=0,workspaceCalls=0;
 const lateSearch=context.createWorkspaceTextSearchController({selection,api:()=>{workspaceCalls++;return latePage.promise},current:()=>workspaceAlive,change:()=>workspaceChanges++});
 const firstPage=lateSearch.start('source');await lateSearch.start('other');assert.equal(workspaceCalls,1);workspaceAlive=false;before=workspaceChanges;latePage.resolve(workspacePage());await firstPage;assert.equal(workspaceChanges,before);assert.equal(lateSearch.snapshot().matches.length,0);
 const workspaceHost=new Element('section');workspaceHost.connected=true;
 handler=async()=>({query:'source',matches:[],has_more:true,next_cursor:token});
 const workspaceMount=mounted.mountWorkspaceTextSearch(workspaceHost,selection);
 const workspaceButtons=descendants(workspaceHost).filter(x=>x.tagName==='button'),workspaceInput=descendants(workspaceHost).find(x=>x.tagName==='input'),workspaceForm=descendants(workspaceHost).find(x=>x.tagName==='form');
 assert.deepEqual(workspaceButtons.map(x=>x.textContent),['Search text','Continue search']);assert.equal(workspaceButtons[1].disabled,true);
 workspaceInput.value='source';workspaceForm.events.submit({preventDefault(){}});await settle();assert.match(workspaceHost.textContent,/No matches on this page. Continue search/);assert.equal(workspaceButtons[1].disabled,false);
 workspaceInput.value='changed';workspaceInput.events.input();assert.equal(workspaceButtons[1].disabled,true);workspaceInput.value='source';workspaceInput.events.input();assert.equal(workspaceButtons[1].disabled,false);
 handler=async()=>workspacePage();await workspaceButtons[1].events.click();assert.match(workspaceHost.textContent,/<b>source<\/b>/);assert.equal(workspaceButtons[1].disabled,true);
 handler=async()=>{throw Error('Workspace access revoked')};workspaceForm.events.submit({preventDefault(){}});await settle();assert.ok(!workspaceHost.textContent.includes('<b>source</b>'));assert.match(workspaceHost.textContent,/Workspace access revoked/);
 workspaceMount.dispose();assert.equal(workspaceHost.children.length,0);
 // Exercise the actual parent renderer, including old-success/old-error races.
 for(const fail of [false,true]){
  let complete,deny;
  const gate=new Promise((resolve,reject)=>{complete=resolve;deny=reject});let requests=0,writes=0;
  const root={isConnected:true,dataset:{},textContent:'PRIVATE old citation',_html:'',
   set innerHTML(value){this._html=value;writes++},get innerHTML(){return this._html},querySelector:()=>null,querySelectorAll:()=>[]};
  const parent=vm.createContext({apiRequest:()=>++requests===1?gate:Promise.resolve([]),escapeHtml:String});
  vm.runInContext(workspace,parent);
  const old=parent.a46RenderLibrary({id:'old'},{id:'old-workspace'},root);
  assert.equal(root.textContent,'Loading authorised Library…','private excerpts survived replacement loading');
  await parent.a46RenderLibrary({id:'new'},{id:'new-workspace'},root);const rendered=root.innerHTML,before=writes;
  if(fail)deny(Error('OLD PRIVATE error'));else complete([]);
  await old;assert.equal(writes,before,'old scope repainted parent renderer');assert.equal(root.innerHTML,rendered);
 }
 console.log('Library text index controller and mounted renderer PASS: explicit actions, immutable scope/citations, UTF-8 bounds, duplicate/late response fences, inert source text and denial cleanup');
})().catch(err=>{console.error(err);process.exitCode=1});
