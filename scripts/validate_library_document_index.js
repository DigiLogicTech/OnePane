const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/webui/static/library-document-index.js','utf8');
const context=vm.createContext({TextEncoder});vm.runInContext(source,context);
const scope={projectID:'project',workspaceID:'workspace',assetID:'asset',version:1};
const raw='sha256:'+'a'.repeat(64),derived='sha256:'+'b'.repeat(64);
const index=()=>({asset_id:'asset',version:1,parser_id:'docx-wordml-paragraphs-v2',raw_source_sha:raw,raw_source_size:1024,derived_sha:derived,derived_utf8_bytes:512,status:'ready',coverage:'stored paragraph text; no layout or pages',parts:[{part_name:'word/document.xml',source_sha:raw,paragraph_count:10}],unit_count:10});
const unit=n=>({ordinal:n,part_name:'word/document.xml',paragraph_number:n,text:'<script>needle</script>',text_hash:derived});
const page=(after=0,q='')=>({index:index(),query:q,units:Array.from({length:after?2:8},(_,i)=>unit(after+i+1)),has_more:!after,next_after:after?10:8});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return{resolve,promise}};
const settle=async()=>{for(let i=0;i<10;i++)await Promise.resolve()};
class Element{
 constructor(tag){this.tagName=tag;this.children=[];this.parent=null;this._text='';this.connected=false;this.events={};this.attributes={};this.value='';this.disabled=false}
 get isConnected(){return this.connected||!!this.parent?.isConnected}
 set innerHTML(_){throw Error('Document text entered HTML renderer')}
 get textContent(){return this._text+this.children.map(x=>x.textContent).join(' ')}
 set textContent(v){this.replaceChildren();this._text=String(v)}
 append(...items){for(const x of items){x.parent=this;this.children.push(x)}}
 replaceChildren(...items){for(const x of this.children)x.parent=null;this.children=[];this._text='';this.append(...items)}
 setAttribute(k,v){this.attributes[k]=v}
 addEventListener(k,f){this.events[k]=f}
}
const nodes=n=>[n,...n.children.flatMap(nodes)];
(async()=>{
 let response=index(),calls=[],alive=true,changes=0;
 const c=context.createLibraryDocumentController({selection:scope,api:async(path,options)=>{calls.push({path,options});return response},current:()=>alive,change:()=>changes++});
 assert.equal(calls.length,0);await c.status();assert.equal(c.snapshot().index.raw_source_sha,raw);assert.equal(c.snapshot().units.length,0);
 await c.extract();assert.equal(calls.at(-1).options.method,'POST');assert.ok(!('body'in calls.at(-1).options));
 response=page();await c.read('');assert.equal(c.snapshot().units.length,8);assert.equal(c.snapshot().hasMore,true);assert.match(calls.at(-1).path,/document-text\?after=0&q=$/);
 response=page(8);await c.next();assert.match(calls.at(-1).path,/after=8/);assert.equal(c.snapshot().units[0].ordinal,9);assert.equal(c.snapshot().hasMore,false);
 const copied=c.snapshot();copied.index.parts[0].part_name='foreign';copied.units[0].text='changed';assert.equal(c.snapshot().index.parts[0].part_name,'word/document.xml');assert.equal(c.snapshot().units[0].text,'<script>needle</script>');
 const before=calls.length;await c.read('😀'.repeat(65));assert.equal(calls.length,before);assert.equal(c.snapshot().units.length,0);
 response=index();await c.status();assert.equal(calls.length,before+1,'invalid query blocked metadata recovery');
 for(const mutate of [x=>x.index.asset_id='foreign',x=>x.index.parser_id='raw-text',x=>x.index.raw_source_sha='invalid',x=>x.index.derived_utf8_bytes=1048577,x=>x.query='other',x=>x.units[0].part_name='private/path',x=>x.units[0].paragraph_number=11,x=>x.units[0].text_hash='invalid',x=>x.units[1].ordinal=1,x=>x.next_after=1,x=>x.units.push(unit(9))]){
  response=page(0,'needle');mutate(response);await c.read('needle');assert.ok(c.snapshot().error);assert.equal(c.snapshot().units.length,0);assert.equal(c.snapshot().index,null);
 }
 const wait=deferred();let pendingCalls=0;
 const late=context.createLibraryDocumentController({selection:scope,api:()=>{pendingCalls++;return wait.promise},current:()=>alive,change:()=>changes++});
 const loading=late.extract();await late.extract();await late.read('');assert.equal(pendingCalls,1);alive=false;const changed=changes;wait.resolve(index());await loading;assert.equal(changes,changed);assert.equal(late.snapshot().index,null);alive=true;
 const document={createElement:tag=>new Element(tag)};let handler=async path=>path.includes('document-text')?page():index();calls=[];
 const mounted=vm.createContext({TextEncoder,document,apiRequest:async(path,options)=>{calls.push({path,options});return handler(path,options)}});vm.runInContext(source,mounted);
 const host=new Element('section');host.connected=true;const mount=mounted.mountLibraryDocumentIndex(host,scope);
 assert.equal(calls.length,0);const buttons=nodes(host).filter(n=>n.tagName==='button');assert.deepEqual(buttons.map(b=>b.textContent),['Check extraction','Extract DOCX text','Read extracted paragraphs','Next paragraphs']);
 assert.equal(buttons[2].disabled,true);assert.equal(buttons[3].disabled,true);await buttons[1].events.click();assert.equal(buttons[2].disabled,false);
 const form=nodes(host).find(n=>n.tagName==='form'),input=nodes(host).find(n=>n.tagName==='input');form.events.submit({preventDefault(){}});await settle();
 assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,8);assert.ok(nodes(host).filter(n=>n.tagName==='pre').every(n=>n.textContent==='<script>needle</script>'));assert.match(host.textContent,/word\/document.xml · paragraph 1/);assert.match(host.textContent,/Source document: sha256:/);assert.match(host.textContent,/Extracted text receipt: sha256:/);assert.ok(!host.textContent.includes('bytes 0'),'raw ZIP offsets mislabeled as paragraph provenance');
 assert.equal(buttons[3].disabled,false);input.value='changed';input.events.input();assert.equal(buttons[3].disabled,true);input.value='';input.events.input();
 handler=async()=>page(8);await buttons[3].events.click();assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,2);assert.match(host.textContent,/paragraph 9/);assert.ok(!host.textContent.includes('paragraph 1 '),'old paragraphs accumulated');
 handler=async()=>{throw Error('Source access revoked')};await buttons[0].events.click();assert.equal(nodes(host).filter(n=>n.tagName==='pre').length,0);assert.match(host.textContent,/Source access revoked/);
 const inFlight=deferred();handler=()=>inFlight.promise;const action=buttons[1].events.click();assert.ok(buttons.every(b=>b.disabled));mount.dispose();inFlight.resolve(index());await action;assert.equal(host.children.length,0,'disposed extraction revived');
 const html=fs.readFileSync('internal/webui/static/index.html','utf8'),library=fs.readFileSync('internal/webui/static/workspace-library.js','utf8');assert.ok(html.indexOf('/library-document-index.js')<html.indexOf('/workspace-library.js'));assert.ok(library.includes('mountLibraryDocumentIndex(textHost,selection,current)'));
 console.log('DOCX extraction controller and mounted renderer PASS: separate source/derived provenance, explicit reads, paragraph paging, literal text, identity/byte bounds, denial, duplicates and disposal');
})().catch(e=>{console.error(e);process.exitCode=1});
