const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const files=['library-text-index.js','library-document-search.js','library-content-search.js'];
const load=context=>files.forEach(name=>vm.runInContext(fs.readFileSync('internal/webui/static/'+name,'utf8'),context));
const context=vm.createContext({TextEncoder});load(context);
const selection={projectID:'project',workspaceID:'workspace'},hash='sha256:'+'a'.repeat(64),derived='sha256:'+'b'.repeat(64),token='A'.repeat(43),phrase='<script>needle</script>';
const page=(more=false)=>({query:'needle',text_matches:[{index:{asset_id:'raw',version:1,parser_version:1,status:'ready',content_hash:hash,size_bytes:1000,line_count:10},line:{line_number:1,text:phrase,line_hash:hash,byte_start:0,byte_end:phrase.length}}],word_sources:[{asset_id:'word',version:1,parser_id:'docx-wordml-paragraphs-v2',status:'ready',raw_source_sha:hash,raw_source_size:2048,derived_sha:derived,derived_utf8_bytes:512,coverage:'stored paragraphs, no rendered pages',unit_count:10,parts:[{part_name:'word/document.xml',source_sha:hash,paragraph_count:10}]}],word_matches:[{source:0,unit:{ordinal:1,part_name:'word/document.xml',paragraph_number:1,text:phrase,text_hash:derived}}],has_more:more,next_cursor:more?token:''});
const empty=()=>({query:'needle',text_matches:[],word_sources:[],word_matches:[],has_more:true,next_cursor:token});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return{promise,resolve}};
const settle=async()=>{for(let i=0;i<30;i++)await Promise.resolve()};
class Element{
 constructor(tag){this.tagName=tag;this.children=[];this.parent=null;this._text='';this.connected=false;this.events={};this.attributes={};this.value='';this.disabled=false}
 get isConnected(){return this.connected||!!this.parent?.isConnected}
 set innerHTML(_){throw Error('Unified search text reached HTML renderer')}
 get textContent(){return this._text+this.children.map(x=>x.textContent).join(' ')}
 set textContent(v){this.replaceChildren();this._text=String(v)}
 append(...items){for(const x of items){x.parent=this;this.children.push(x)}}
 replaceChildren(...items){for(const x of this.children)x.parent=null;this.children=[];this._text='';this.append(...items)}
 setAttribute(k,v){this.attributes[k]=v}
 addEventListener(k,f){this.events[k]=f}
}
const nodes=n=>[n,...n.children.flatMap(nodes)];
(async()=>{
 let response=empty(),calls=[];const scope={...selection};
 const c=context.createWorkspaceContentSearchController({selection:scope,api:async path=>{calls.push(path);return response}});
 scope.workspaceID='changed';assert.equal(calls.length,0);await c.start('needle');assert.equal(calls.length,1);assert.match(calls[0],/workspaces\/workspace\/library\/search-content/);assert.equal(c.snapshot().hasMore,true);assert.equal(c.snapshot().textMatches.length,0);
 response=page();await c.next();assert.equal(calls.length,2);assert.match(calls[1],/cursor=A{43}$/);assert.equal(c.snapshot().textMatches.length,1);assert.equal(c.snapshot().wordMatches.length,1);assert.equal(c.snapshot().hasMore,false);
 const copied=c.snapshot();copied.textMatches[0].line.text='changed';copied.wordMatches[0].index.parts[0].part_name='changed';assert.equal(c.snapshot().textMatches[0].line.text,phrase);assert.equal(c.snapshot().wordMatches[0].index.parts[0].part_name,'word/document.xml');
 const before=calls.length;await c.start('😀'.repeat(65));assert.equal(calls.length,before);assert.equal(c.snapshot().textMatches.length,0);assert.equal(c.snapshot().wordMatches.length,0);
 for(const mutate of [p=>p.query='foreign',p=>p.text_matches[0].line.byte_end=100,p=>p.text_matches[0].index.content_hash='bad',p=>p.text_matches.push(p.text_matches[0]),p=>p.word_sources[0].parser_id='docx-wordml-paragraphs-v1',p=>p.word_matches[0].source=1,p=>p.word_matches[0].unit.part_name='hidden/path',p=>p.word_matches.push(p.word_matches[0]),p=>p.next_cursor='PRIVATE_RAW_POSITION']){
  response=page(true);mutate(response);await c.start('needle');assert.ok(c.snapshot().error);assert.equal(c.snapshot().textMatches.length,0);assert.equal(c.snapshot().wordMatches.length,0);assert.equal(c.snapshot().hasMore,false);
 }
 const waiting=deferred();let alive=true,changes=0,requests=0;
 const stale=context.createWorkspaceContentSearchController({selection,api:()=>{requests++;return waiting.promise},current:()=>alive,change:()=>changes++});const pending=stale.start('needle');await stale.start('other');assert.equal(requests,1);alive=false;const changed=changes;waiting.resolve(page());await pending;assert.equal(changes,changed);assert.equal(stale.snapshot().textMatches.length,0);assert.equal(stale.snapshot().wordMatches.length,0);
 const document={createElement:tag=>new Element(tag)};let handler=async()=>empty();calls=[];
 const mounted=vm.createContext({TextEncoder,document,apiRequest:async path=>{calls.push(path);return handler(path)}});load(mounted);
 const host=new Element('section');host.connected=true;const mount=mounted.mountWorkspaceContentSearch(host,selection);assert.equal(calls.length,0);
 const buttons=nodes(host).filter(n=>n.tagName==='button'),input=nodes(host).find(n=>n.tagName==='input'),form=nodes(host).find(n=>n.tagName==='form');assert.deepEqual(buttons.map(b=>b.textContent),['Search contents','Continue content search']);
 input.value='needle';form.events.submit({preventDefault(){}});await settle();assert.match(host.textContent,/No matches on this page. Continue content search/);assert.equal(buttons[1].disabled,false);
 input.value='changed';input.events.input();assert.equal(buttons[1].disabled,true);input.value='needle';input.events.input();handler=async()=>page();await buttons[1].events.click();await settle();assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,2);assert.match(host.textContent,/Raw-file text lines/);assert.match(host.textContent,/Extracted Word paragraphs/);assert.match(host.textContent,/<script>needle<\/script>/);assert.match(host.textContent,/bytes 0–23/);assert.match(host.textContent,/word\/document.xml · paragraph 1/);assert.match(host.textContent,/Source: sha256:/);assert.match(host.textContent,/Extracted text: sha256:/);
 handler=async()=>{throw Error('Source access revoked')};form.events.submit({preventDefault(){}});await settle();assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,0);assert.match(host.textContent,/Source access revoked/);
 const late=deferred();handler=()=>late.promise;form.events.submit({preventDefault(){}});await settle();assert.ok(buttons.every(b=>b.disabled));mount.dispose();late.resolve(page());await settle();assert.equal(host.children.length,0);
 const html=fs.readFileSync('internal/webui/static/index.html','utf8'),parent=fs.readFileSync('internal/webui/static/workspace-library.js','utf8');assert.ok(html.indexOf('/library-content-search.js')>html.indexOf('/library-document-search.js'));assert.ok(html.indexOf('/library-content-search.js')<html.indexOf('/workspace-library.js'));assert.ok(parent.includes('mountWorkspaceContentSearch(searchHost,{projectID,workspaceID:String(source.id)},current)'));assert.ok(!parent.includes('mountWorkspaceDocumentSearch(documentSearchHost'));
 console.log('Unified content search PASS: one request, two native citation formats, compound paging, inherited byte/identity checks, empty pages, inert rendering, denial, duplicates and stale scopes');
})().catch(e=>{console.error(e);process.exitCode=1});
