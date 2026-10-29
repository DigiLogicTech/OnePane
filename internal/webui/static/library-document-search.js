/* Explicit Workspace-wide Word discovery. Paragraph provenance stays separate
 * from raw-file line/byte citations. No extraction, paging or model delivery is automatic. */
function createWorkspaceDocumentSearchController({api,selection,current=()=>true,change=()=>{}}){
 const scope=Object.freeze({...selection});
 const path="/v1/projects/"+encodeURIComponent(scope.projectID)+"/workspaces/"+encodeURIComponent(scope.workspaceID)+"/library/search-documents";
 const hash=x=>typeof x==="string"&&/^sha256:[a-f0-9]{64}$/.test(x);
 const bytes=x=>new TextEncoder().encode(x).length;
 const clone=m=>({index:{...m.index,parts:m.index.parts.map(p=>({...p}))},unit:{...m.unit}});
 let pending=false,disposed=false,query="",cursor="",matches=[],hasMore=false,error="",started=false,sequence=0;
 const alive=()=>!disposed&&current();
 const snapshot=()=>({pending,query,matches:matches.map(clone),hasMore,error,started});
 const publish=()=>{if(alive())change(snapshot())};
 const valid=m=>{
  const x=m?.index,u=m?.unit;
  return x&&u&&typeof x.asset_id==="string"&&bytes(x.asset_id)>0&&bytes(x.asset_id)<=256&&Number.isSafeInteger(x.version)&&x.version>0&&
   x.parser_id==="docx-wordml-paragraphs-v2"&&x.status==="ready"&&hash(x.raw_source_sha)&&hash(x.derived_sha)&&
   Number.isSafeInteger(x.raw_source_size)&&x.raw_source_size>=0&&x.raw_source_size<=8388608&&Number.isSafeInteger(x.derived_utf8_bytes)&&x.derived_utf8_bytes>=0&&x.derived_utf8_bytes<=1048576&&
   Number.isSafeInteger(x.unit_count)&&x.unit_count>0&&x.unit_count<=8192&&typeof x.coverage==="string"&&x.coverage.length>0&&bytes(x.coverage)<=512&&
   Array.isArray(x.parts)&&x.parts.length>0&&x.parts.length<=64&&new Set(x.parts.map(p=>p?.part_name)).size===x.parts.length&&
   x.parts.every(p=>typeof p?.part_name==="string"&&p.part_name.startsWith("word/")&&bytes(p.part_name)<=256&&hash(p.source_sha)&&Number.isSafeInteger(p.paragraph_count)&&p.paragraph_count>=0&&p.paragraph_count<=8192)&&
   x.parts.reduce((sum,p)=>sum+p.paragraph_count,0)===x.unit_count&&Number.isSafeInteger(u.ordinal)&&u.ordinal>0&&u.ordinal<=x.unit_count&&
   Number.isSafeInteger(u.paragraph_number)&&u.paragraph_number>0&&x.parts.some(p=>p.part_name===u.part_name&&u.paragraph_number<=p.paragraph_count)&&
   typeof u.text==="string"&&!u.text.includes("\0")&&bytes(u.text)<=16384&&u.text.includes(query)&&hash(u.text_hash);
 };
 async function run(next,value){
  if(!alive()||pending||(next&&!hasMore))return;
  if(!next){query=String(value);cursor="";hasMore=false}
  matches=[];error="";
  if(!query.trim()||bytes(query)>256||query.includes("\0")){cursor="";hasMore=false;error="Enter a phrase of at most 256 UTF-8 bytes.";publish();return}
  pending=true;const ticket=++sequence;publish();
  try{
   const result=await api(path+"?q="+encodeURIComponent(query)+(next?"&cursor="+encodeURIComponent(cursor):""));
   if(!alive()||ticket!==sequence)return;
   if(result?.query!==query||!Array.isArray(result.sources)||result.sources.length>2||!Array.isArray(result.matches)||result.matches.length>8||typeof result.has_more!=="boolean"||
    typeof result.next_cursor!=="string"||(result.has_more?!/^[A-Za-z0-9_-]{43}$/.test(result.next_cursor):result.next_cursor!=="")||
    result.matches.some(m=>!Number.isSafeInteger(m?.source)||m.source<0||m.source>=result.sources.length)||
    new Set(result.matches.map(m=>m.source)).size!==result.sources.length){
    throw Error("Word search source identity or paragraph citations changed; start a new search.");
   }
   const expanded=result.matches.map(m=>({index:result.sources[m.source],unit:m.unit}));
   if(expanded.some(m=>!valid(m))||new Set(expanded.map(m=>JSON.stringify([m.index.asset_id,m.index.version,m.unit.ordinal]))).size!==expanded.length||
    new Set(result.sources.map(x=>JSON.stringify([x?.asset_id,x?.version]))).size!==result.sources.length){throw Error("Word paragraph/source receipts are inconsistent; start a new search.")}
   matches=expanded.map(clone);hasMore=result.has_more;cursor=result.next_cursor;started=true;
  }catch(e){if(!alive()||ticket!==sequence)return;matches=[];cursor="";hasMore=false;error=String(e?.message||"Word text search unavailable")}
  finally{if(alive()&&ticket===sequence){pending=false;publish()}}
 }
 return {snapshot,start:q=>run(false,q),next:()=>run(true),dispose(){disposed=true;sequence++;matches=[];cursor="";query="";hasMore=false;pending=false;error="";started=false}};
}

function mountWorkspaceDocumentSearch(host,selection,current=()=>true){
 const make=(tag,text,className)=>{const e=document.createElement(tag);if(text)e.textContent=text;if(className)e.className=className;return e};
 const title=make("strong","Search extracted Word text"),note=make("p","Search Word documents indexed in this Workspace. Extract each document on its card first. Matches cite the source version, document part and paragraph. Nothing is sent to a model.","list-meta");
 const form=make("form","","toolbar"),label=make("label","Phrase (case sensitive)","a64-search-label"),input=make("input");input.type="search";input.maxLength=256;input.setAttribute("aria-label","Search extracted Word paragraphs in this Workspace");label.append(input);
 const search=make("button","Search Word text","btn"),next=make("button","Continue Word search","btn");search.type="submit";next.type="button";next.disabled=true;form.append(label,search,next);
 const status=make("p","Word text is loaded only when you search.","list-meta");status.setAttribute("role","status");const output=make("div");host.append(title,note,form,status,output);
 let state=null;
 const updateNext=()=>{next.disabled=!state||state.pending||!state.hasMore||input.value!==state.query};
 const controller=createWorkspaceDocumentSearchController({api:apiRequest,selection,current:()=>host.isConnected&&current(),change:s=>{
  state=s;input.disabled=s.pending;search.disabled=s.pending;updateNext();output.replaceChildren();
  status.textContent=s.pending?"Checking permitted Word text…":s.error||(!s.started?"Word text is loaded only when you search.":s.hasMore?
   (s.matches.length?"Matches on this page; continue to check more documents.":"No matches on this page. Continue Word search to check more documents."):
   "Finished the current forward scan of extracted Word text.");
  for(const match of s.matches){const row=make("article","","a46-library-version");row.append(make("strong",match.index.asset_id+" · v"+match.index.version+" · "+match.unit.part_name+" · paragraph "+match.unit.paragraph_number),make("pre",match.unit.text,"a46-library-preview"),make("p","Source: "+match.index.raw_source_sha+" · Extracted text: "+match.index.derived_sha+" · Paragraph: "+match.unit.text_hash,"list-meta"));output.append(row)}
  if(s.started&&!s.pending&&!s.error&&!s.matches.length&&!s.hasMore)output.append(make("p","No matching paragraphs on this page.","list-meta"));
 }});
 form.addEventListener("submit",e=>{e.preventDefault();controller.start(input.value)});next.addEventListener("click",()=>controller.next());input.addEventListener("input",updateNext);
 return {...controller,dispose(){controller.dispose();host.replaceChildren()}};
}
