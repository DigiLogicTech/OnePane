const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/webui/static/library-document-search.js','utf8');
const context=vm.createContext({TextEncoder});vm.runInContext(source,context);
const selection={projectID:'project',workspaceID:'workspace'},hash='sha256:'+'a'.repeat(64),derived='sha256:'+'b'.repeat(64),token='A'.repeat(43);
const index=()=>({asset_id:'asset',version:1,parser_id:'docx-wordml-paragraphs-v2',status:'ready',raw_source_sha:hash,raw_source_size:2048,derived_sha:derived,derived_utf8_bytes:512,coverage:'stored paragraphs, no rendered pages',unit_count:10,parts:[{part_name:'word/document.xml',source_sha:hash,paragraph_count:10}]});
const unit=n=>({ordinal:n,part_name:'word/document.xml',paragraph_number:n,text:'<script>needle</script>',text_hash:derived});
const page=(more=false,n=1)=>({query:'needle',sources:[index()],matches:[{source:0,unit:unit(n)}],has_more:more,next_cursor:more?token:''});
const empty=()=>({query:'needle',sources:[],matches:[],has_more:true,next_cursor:token});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return{promise,resolve}};
const settle=async()=>{for(let i=0;i<12;i++)await Promise.resolve()};
class Element{
 constructor(tag){this.tagName=tag;this.children=[];this.parent=null;this._text='';this.connected=false;this.events={};this.attributes={};this.value='';this.disabled=false}
 get isConnected(){return this.connected||!!this.parent?.isConnected}
 set innerHTML(_){throw Error('Word discovery text reached HTML renderer')}
 get textContent(){return this._text+this.children.map(x=>x.textContent).join(' ')}
 set textContent(v){this.replaceChildren();this._text=String(v)}
 append(...items){for(const x of items){x.parent=this;this.children.push(x)}}
 replaceChildren(...items){for(const x of this.children)x.parent=null;this.children=[];this._text='';this.append(...items)}
 setAttribute(k,v){this.attributes[k]=v}
 addEventListener(k,f){this.events[k]=f}
}
const nodes=n=>[n,...n.children.flatMap(nodes)];
(async()=>{
 let response=empty(),calls=[];
 const scope={...selection};const c=context.createWorkspaceDocumentSearchController({selection:scope,api:async path=>{calls.push(path);return response}});
 scope.workspaceID='changed';assert.equal(calls.length,0);await c.start('needle');assert.match(calls[0],/workspaces\/workspace\/library\/search-documents/);assert.equal(c.snapshot().matches.length,0);assert.equal(c.snapshot().hasMore,true);
 response=page();await c.next();assert.match(calls.at(-1),/cursor=A{43}$/);assert.equal(c.snapshot().matches.length,1);assert.equal(c.snapshot().hasMore,false);
 const copied=c.snapshot();copied.matches[0].index.parts[0].part_name='changed';copied.matches[0].unit.text='changed';assert.equal(c.snapshot().matches[0].unit.text,'<script>needle</script>');assert.equal(c.snapshot().matches[0].index.parts[0].part_name,'word/document.xml');
 const before=calls.length;await c.start('😀'.repeat(65));assert.equal(calls.length,before);assert.equal(c.snapshot().matches.length,0);
 for(const mutate of [p=>p.query='foreign',p=>p.sources[0].parser_id='docx-wordml-paragraphs-v1',p=>p.sources[0].raw_source_sha='bad',p=>p.sources[0].raw_source_size=8388609,p=>p.sources[0].derived_utf8_bytes=1048577,p=>p.sources[0].unit_count=12,p=>p.sources.push(index()),p=>p.matches[0].source=1,p=>p.matches[0].unit.part_name='private/path',p=>p.matches[0].unit.text_hash='bad',p=>p.matches[0].unit.text='not matching',p=>p.matches.push(p.matches[0]),p=>p.next_cursor='PRIVATE_RAW_SOURCE_POSITION']){
  response=page(true);mutate(response);await c.start('needle');assert.ok(c.snapshot().error);assert.equal(c.snapshot().matches.length,0);assert.equal(c.snapshot().hasMore,false);
 }
 const waiting=deferred();let alive=true,changes=0,requests=0;
 const stale=context.createWorkspaceDocumentSearchController({selection,api:()=>{requests++;return waiting.promise},current:()=>alive,change:()=>changes++});
 const pending=stale.start('needle');await stale.start('other');assert.equal(requests,1);alive=false;const changed=changes;waiting.resolve(page());await pending;assert.equal(changes,changed);assert.equal(stale.snapshot().matches.length,0);
 const document={createElement:tag=>new Element(tag)};let handler=async()=>empty();calls=[];
 const mounted=vm.createContext({TextEncoder,document,apiRequest:async path=>{calls.push(path);return handler(path)}});vm.runInContext(source,mounted);
 const host=new Element('section');host.connected=true;const mount=mounted.mountWorkspaceDocumentSearch(host,selection);
 assert.equal(calls.length,0);const buttons=nodes(host).filter(n=>n.tagName==='button'),input=nodes(host).find(n=>n.tagName==='input'),form=nodes(host).find(n=>n.tagName==='form');
 assert.deepEqual(buttons.map(b=>b.textContent),['Search Word text','Continue Word search']);assert.equal(buttons[1].disabled,true);
 input.value='needle';form.events.submit({preventDefault(){}});await settle();assert.match(host.textContent,/No matches on this page. Continue Word search/);assert.equal(buttons[1].disabled,false);
 input.value='changed';input.events.input();assert.equal(buttons[1].disabled,true);input.value='needle';input.events.input();
 handler=async()=>page();await buttons[1].events.click();assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,1);assert.match(host.textContent,/<script>needle<\/script>/);assert.match(host.textContent,/word\/document.xml · paragraph 1/);assert.match(host.textContent,/Source: sha256:/);assert.match(host.textContent,/Extracted text: sha256:/);assert.ok(!host.textContent.includes('bytes 0'));
 handler=async()=>{throw Error('Source access revoked')};form.events.submit({preventDefault(){}});await settle();assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,0);assert.match(host.textContent,/Source access revoked/);
 const late=deferred();handler=()=>late.promise;form.events.submit({preventDefault(){}});await settle();assert.ok(buttons.every(b=>b.disabled));mount.dispose();late.resolve(page());await settle();assert.equal(host.children.length,0);
 const html=fs.readFileSync('internal/webui/static/index.html','utf8'),parent=fs.readFileSync('internal/webui/static/workspace-library.js','utf8'),scriptPosition=html.indexOf('/library-document-search.js');assert.ok(scriptPosition>=0&&scriptPosition<html.indexOf('/library-content-search.js'));assert.ok(parent.includes('mountWorkspaceContentSearch(searchHost,{projectID,workspaceID:String(source.id)},current)'));
 console.log('Workspace Word discovery controller/mounted renderer PASS: explicit paging, compact source receipts, paragraph provenance, byte/identity limits, empty pages, safe text, denial, duplicates and stale scope');
})().catch(e=>{console.error(e);process.exitCode=1});
