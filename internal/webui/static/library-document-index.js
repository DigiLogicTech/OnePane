/* Source-bound Word document extraction. Stored paragraph coordinates are
 * distinct from rendered page numbers and raw ZIP byte offsets. */
function createLibraryDocumentController({api,selection,current=()=>true,change=()=>{}}){
 const scope=Object.freeze({...selection}),base="/v1/projects/"+encodeURIComponent(scope.projectID)+"/workspaces/"+encodeURIComponent(scope.workspaceID)+"/library/"+encodeURIComponent(scope.assetID)+"/versions/"+scope.version;
 const hash=x=>typeof x==="string"&&/^sha256:[a-f0-9]{64}$/.test(x);
 let index=null,units=[],pending=false,error="",query="",hasMore=false,after=0,read=false,disposed=false,sequence=0;
 const alive=()=>!disposed&&current();
 const snapshot=()=>({index:index?{...index,parts:index.parts.map(p=>({...p}))}:null,units:units.map(u=>({...u})),pending,error,query,hasMore,read});
 const publish=()=>{if(alive())change(snapshot())};
 const validIndex=x=>x&&x.asset_id===scope.assetID&&x.version===scope.version&&x.parser_id==="docx-wordml-paragraphs-v2"&&hash(x.raw_source_sha)&&
  Number.isSafeInteger(x.raw_source_size)&&x.raw_source_size>=0&&["ready","not_indexed","unsupported","too_large","invalid_document"].includes(x.status)&&
  Number.isSafeInteger(x.unit_count)&&x.unit_count>=0&&x.unit_count<=8192&&Number.isSafeInteger(x.derived_utf8_bytes)&&x.derived_utf8_bytes>=0&&x.derived_utf8_bytes<=1048576&&
  typeof x.coverage==="string"&&new TextEncoder().encode(x.coverage).length<=512&&Array.isArray(x.parts)&&x.parts.length<=64&&
  x.parts.every(p=>typeof p.part_name==="string"&&p.part_name.startsWith("word/")&&new TextEncoder().encode(p.part_name).length<=256&&hash(p.source_sha)&&Number.isSafeInteger(p.paragraph_count)&&p.paragraph_count>=0&&p.paragraph_count<=8192)&&
  (x.status==="ready"?(x.raw_source_size<=8388608&&hash(x.derived_sha)&&x.coverage.length>0&&x.parts.length>0&&
   new Set(x.parts.map(p=>p.part_name)).size===x.parts.length&&x.parts.reduce((n,p)=>n+p.paragraph_count,0)===x.unit_count):
   (x.unit_count===0&&x.derived_utf8_bytes===0&&x.derived_sha===""&&x.coverage===""&&x.parts.length===0));
 async function act(kind,value=""){
  if(!alive()||pending||(kind==="next"&&!hasMore))return;
  const reading=kind==="read"||kind==="next";
  if(kind==="read"){query=String(value);after=0}
  units=[];hasMore=false;error="";read=false;
  if(reading&&(new TextEncoder().encode(query).length>256||query.includes("\0")||(query!==""&&!query.trim()))){error="Use an optional literal phrase of at most 256 UTF-8 bytes.";publish();return}
  pending=true;const ticket=++sequence;publish();
  try{
   const response=await api(base+(reading?"/document-text?after="+after+"&q="+encodeURIComponent(query):"/document-index"),kind==="extract"?{method:"POST"}:undefined);
   if(!alive()||sequence!==ticket)return;
   const receipt=reading?response?.index:response;
   if(!validIndex(receipt))throw Error("Document source identity changed; reload the Library.");
   if(reading){
    if(receipt.status!=="ready"||response.query!==query||!Array.isArray(response.units)||response.units.length>8||typeof response.has_more!=="boolean"||!Number.isSafeInteger(response.next_after)||response.next_after<after||response.next_after>receipt.unit_count||
     response.units.some((u,i)=>!Number.isSafeInteger(u.ordinal)||u.ordinal<=after||u.ordinal>receipt.unit_count||(i>0&&u.ordinal<=response.units[i-1].ordinal)||
      !Number.isSafeInteger(u.paragraph_number)||u.paragraph_number<1||!receipt.parts.some(p=>p.part_name===u.part_name&&u.paragraph_number<=p.paragraph_count)||
      typeof u.text!=="string"||u.text.includes("\0")||!u.text.includes(query)||new TextEncoder().encode(u.text).length>16384||!hash(u.text_hash))||
     (response.units.length?response.next_after!==response.units.at(-1).ordinal:response.has_more))throw Error("Document paragraph receipt is unavailable; reload the extraction.");
    units=response.units.map(u=>({...u}));after=response.next_after;hasMore=response.has_more;read=true;
   }else{after=0}
   index={...receipt,parts:receipt.parts.map(p=>({...p}))};
  }catch(e){if(!alive()||sequence!==ticket)return;index=null;units=[];hasMore=false;error=String(e?.message||"Document extraction unavailable")}
  finally{if(alive()&&sequence===ticket){pending=false;publish()}}
 }
 return {snapshot,status:()=>act("status"),extract:()=>act("extract"),read:q=>act("read",q),next:()=>act("next"),dispose(){disposed=true;sequence++;index=null;units=[];query="";hasMore=false;pending=false}};
}

function mountLibraryDocumentIndex(host,selection,current=()=>true){
 const node=(tag,text,style)=>{const e=document.createElement(tag);if(text)e.textContent=text;if(style)e.className=style;return e};
 host.append(node("strong","Word document text · v"+selection.version),node("p","Extract stored paragraph text from the document, headers, footers, notes and comments when present. Page layout, images and embedded documents are not extracted. Each read checks original source access.","list-meta"));
 const controls=node("div","","toolbar"),check=node("button","Check extraction","btn"),extract=node("button","Extract DOCX text","btn");check.type=extract.type="button";controls.append(check,extract);host.append(controls);
 const form=node("form","","toolbar"),label=node("label","Optional literal phrase (case sensitive)","a64-search-label"),input=node("input");input.type="search";input.maxLength=256;input.setAttribute("aria-label","Filter stored document paragraphs");label.append(input);
 const read=node("button","Read extracted paragraphs","btn"),next=node("button","Next paragraphs","btn");read.type="submit";next.type="button";read.disabled=next.disabled=true;form.append(label,read,next);host.append(form);
 const status=node("p","Extraction status has not been checked.","list-meta"),output=node("div");status.setAttribute("role","status");output.setAttribute("aria-label","Authorised document paragraphs and source provenance");host.append(status,output);
 const labels={ready:"Stored paragraph text extracted",not_indexed:"Document has not been extracted",unsupported:"This document format or structure is unsupported",too_large:"Document exceeds extraction limits",invalid_document:"Document package or XML could not be validated"};
 let state=null;const update=()=>{read.disabled=!state||state.pending||state.index?.status!=="ready";next.disabled=!state||state.pending||!state.hasMore||input.value!==state.query};
 const controller=createLibraryDocumentController({api:apiRequest,selection,current:()=>host.isConnected&&current(),change:s=>{
  state=s;check.disabled=extract.disabled=input.disabled=s.pending;update();output.replaceChildren();
  status.textContent=s.pending?"Checking authorised document source…":s.error||(s.index?labels[s.index.status]+(s.index.status==="ready"?" · "+s.index.unit_count+" paragraphs":""):"Extraction status has not been checked.");
  if(s.index?.status==="ready"){
   output.append(node("p","Source document: "+s.index.raw_source_sha,"list-meta"),node("p","Extracted text receipt: "+s.index.derived_sha,"list-meta"));
  }
  for(const u of s.units){const row=node("article","","a46-library-version");row.append(node("strong",u.part_name+" · paragraph "+u.paragraph_number),node("pre",u.text,"a46-library-preview"),node("p",u.text_hash,"list-meta"));output.append(row)}
  if(s.read&&!s.units.length)output.append(node("p","No matching paragraphs after this position.","list-meta"));
  if(s.hasMore)output.append(node("p","More matching paragraphs remain in this exact source version.","list-meta"));
 }});
 check.addEventListener("click",()=>controller.status());extract.addEventListener("click",()=>controller.extract());form.addEventListener("submit",e=>{e.preventDefault();controller.read(input.value)});next.addEventListener("click",()=>controller.next());input.addEventListener("input",update);
 return {...controller,dispose(){controller.dispose();host.replaceChildren()}};
}
