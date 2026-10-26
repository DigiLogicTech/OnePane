/* Explicit, scoped indexing and literal search of one immutable text version. */
function createLibraryTextIndexController({api,selection,current=()=>true,change=()=>{}}){
 const binding=Object.freeze({...selection});
 const path="/v1/projects/"+encodeURIComponent(binding.projectID)+"/workspaces/"+
  encodeURIComponent(binding.workspaceID)+"/library/"+encodeURIComponent(binding.assetID)+
  "/versions/"+binding.version;
 let disposed=false,pending=false,index=null,lines=[],hasMore=false,searched=false,error="",sequence=0;
 const live=()=>!disposed&&current();
 const snapshot=()=>({pending,index:index?{...index}:null,lines:lines.map(x=>({...x})),hasMore,searched,error});
 const publish=()=>{if(live())change(snapshot())};
 const validIndex=x=>x&&x.asset_id===binding.assetID&&x.version===binding.version&&
  x.parser_version===1&&/^sha256:[a-f0-9]{64}$/.test(x.content_hash)&&
  ["ready","not_indexed","unsupported","too_large","invalid_text"].includes(x.status)&&
  Number.isSafeInteger(x.line_count)&&x.line_count>=0&&x.line_count<=16384&&
  Number.isSafeInteger(x.size_bytes)&&x.size_bytes>=0&&(x.status!=="ready"||x.size_bytes<=1048576);
 async function act(kind,query=""){
  if(!live()||pending)return;
  if(kind==="search"&&(!query.trim()||new TextEncoder().encode(query).length>256||query.includes("\0"))){
   lines=[];hasMore=false;searched=false;error="Enter a literal phrase of at most 256 UTF-8 bytes.";publish();return;
  }
  const ticket=++sequence;
  pending=true;lines=[];hasMore=false;searched=false;error="";publish();
  try{
   const response=await api(path+(kind==="search"?"/search?q="+encodeURIComponent(query):"/index"),
    kind==="index"?{method:"POST"}:undefined);
   if(!live()||ticket!==sequence)return;
   const metadata=kind==="search"?response?.index:response;
   if(!validIndex(metadata))throw new Error("Library text identity changed; reload the Library.");
   if(kind==="search"){
    if(metadata.status!=="ready"||response.query!==query||!Array.isArray(response.lines)||response.lines.length>8||
     typeof response.has_more!=="boolean"||response.lines.some((x,i)=>!Number.isSafeInteger(x.line_number)||
      x.line_number<1||x.line_number>metadata.line_count||typeof x.text!=="string"||
      (i>0&&x.line_number<=response.lines[i-1].line_number)||x.text.includes("\0")||x.text.includes("\n")||!x.text.includes(query)||
      new TextEncoder().encode(x.text).length>16384||!/^sha256:[a-f0-9]{64}$/.test(x.line_hash)||
      !Number.isSafeInteger(x.byte_start)||!Number.isSafeInteger(x.byte_end)||x.byte_start<0||
      x.byte_end-x.byte_start!==new TextEncoder().encode(x.text).length||x.byte_end>metadata.size_bytes)){
     throw new Error("Library text citation unavailable; reload the Library.");
    }
    lines=response.lines.map(x=>({...x}));hasMore=response.has_more;searched=true;
   }
   index={...metadata};
  }catch(e){
   if(!live()||ticket!==sequence)return;
   index=null;lines=[];hasMore=false;searched=false;error=String(e?.message||"Library text unavailable");
  }finally{
   if(live()&&ticket===sequence){pending=false;publish()}
  }
 }
 return {snapshot,status:()=>act("status"),index:()=>act("index"),search:q=>act("search",String(q)),
  dispose(){disposed=true;sequence++;index=null;lines=[];error="";pending=false;searched=false;}};
}

function mountLibraryTextIndex(host,selection,current=()=>true){
 const make=(tag,text,className)=>{const e=document.createElement(tag);if(text)e.textContent=text;if(className)e.className=className;return e};
 const title=make("strong","Document text · v"+selection.version);
 const note=make("p","Save a local text index, then search this exact version. UTF-8 text up to 1 MiB; each read checks current Workspace access. Source text is not sent to a model.","list-meta");
 const controls=make("div","","toolbar");
 const statusButton=make("button","Check indexing","btn"),indexButton=make("button","Index text","btn");
 for(const b of [statusButton,indexButton])b.type="button";
 const form=make("form","","toolbar"),label=make("label","Literal phrase (case sensitive)","a64-search-label");
 const query=make("input");query.type="search";query.maxLength=256;query.setAttribute("aria-label","Search this exact Library document");
 label.append(query);
 const searchButton=make("button","Search document","btn");searchButton.type="submit";
 form.append(label,searchButton);controls.append(statusButton,indexButton);
 const status=make("p","Indexing status has not been checked.","list-meta");status.setAttribute("role","status");
 const output=make("div");output.setAttribute("aria-label","Untrusted source text and immutable line citations");
 host.append(title,note,controls,form,status,output);
 const messages={ready:"Text indexed",not_indexed:"Text has not been indexed",unsupported:"This file type or encoding is not supported by the text index",too_large:"Document exceeds the text, line length or line count limit",invalid_text:"Content is not valid UTF-8 text"};
 const controller=createLibraryTextIndexController({api:apiRequest,selection,current:()=>host.isConnected&&current(),change:s=>{
  for(const b of [statusButton,indexButton,searchButton,query])b.disabled=s.pending;
  searchButton.disabled=s.pending||s.index?.status!=="ready";
  status.textContent=s.pending?"Checking authorised document text…":s.error||
   (s.index?messages[s.index.status]+(s.index.status==="ready"?" · "+s.index.line_count+" lines":""):"Indexing status has not been checked.");
  output.replaceChildren();
  for(const line of s.lines){
   const entry=make("article","","a46-library-version");
   entry.append(make("strong","Line "+line.line_number),make("pre",line.text,"a46-library-preview"),
    make("p","v"+selection.version+" · bytes "+line.byte_start+"–"+line.byte_end+" (end exclusive) · "+line.line_hash,"list-meta"));
   output.append(entry);
  }
  if(s.lines.length)output.prepend(make("p","Source "+s.index.content_hash,"list-meta"));
  if(s.searched&&!s.lines.length)output.append(make("p","No matching lines in this exact version.","list-meta"));
  if(s.hasMore)output.append(make("p","More matching lines exist. Refine the phrase to narrow the results.","list-meta"));
 }});
 searchButton.disabled=true;
 statusButton.addEventListener("click",()=>controller.status());
 indexButton.addEventListener("click",()=>controller.index());
 form.addEventListener("submit",e=>{e.preventDefault();controller.search(query.value)});
 return {...controller,dispose(){controller.dispose();host.replaceChildren()}};
}

function createWorkspaceTextSearchController({api,selection,current=()=>true,change=()=>{}}){
 const binding=Object.freeze({...selection});
 const path="/v1/projects/"+encodeURIComponent(binding.projectID)+"/workspaces/"+encodeURIComponent(binding.workspaceID)+"/library/search-text";
 let pending=false,disposed=false,query="",cursor="",matches=[],hasMore=false,error="",started=false,ticket=0;
 const live=()=>!disposed&&current();
 const snapshot=()=>({pending,query,matches:matches.map(x=>({index:{...x.index},line:{...x.line}})),hasMore,error,started});
 const publish=()=>{if(live())change(snapshot())};
 const validMatch=m=>m&&m.index&&m.line&&typeof m.index.asset_id==="string"&&m.index.asset_id.length>0&&m.index.asset_id.length<=256&&
  Number.isSafeInteger(m.index.version)&&m.index.version>0&&m.index.parser_version===1&&m.index.status==="ready"&&
  /^sha256:[a-f0-9]{64}$/.test(m.index.content_hash)&&Number.isSafeInteger(m.index.size_bytes)&&m.index.size_bytes>=0&&m.index.size_bytes<=1048576&&
  Number.isSafeInteger(m.index.line_count)&&m.index.line_count>=1&&m.index.line_count<=16384&&
  Number.isSafeInteger(m.line.line_number)&&m.line.line_number>=1&&m.line.line_number<=m.index.line_count&&
  typeof m.line.text==="string"&&!m.line.text.includes("\0")&&!m.line.text.includes("\n")&&m.line.text.includes(query)&&
  new TextEncoder().encode(m.line.text).length<=16384&&/^sha256:[a-f0-9]{64}$/.test(m.line.line_hash)&&
  Number.isSafeInteger(m.line.byte_start)&&Number.isSafeInteger(m.line.byte_end)&&m.line.byte_start>=0&&m.line.byte_end<=m.index.size_bytes&&
  m.line.byte_end-m.line.byte_start===new TextEncoder().encode(m.line.text).length;
 async function run(next,newQuery){
  if(!live()||pending||(next&&!hasMore))return;
  if(!next){query=String(newQuery);cursor="";hasMore=false}
  matches=[];error="";
  if(!query.trim()||new TextEncoder().encode(query).length>256||query.includes("\0")){cursor="";hasMore=false;error="Enter a literal phrase of at most 256 UTF-8 bytes.";publish();return}
  pending=true;const request=++ticket;publish();
  try{
   const result=await api(path+"?q="+encodeURIComponent(query)+(next?"&cursor="+encodeURIComponent(cursor):""));
   if(!live()||request!==ticket)return;
   if(result?.query!==query||!Array.isArray(result.matches)||result.matches.length>8||typeof result.has_more!=="boolean"||
    typeof result.next_cursor!=="string"||(result.has_more?!/^[A-Za-z0-9_-]{43}$/.test(result.next_cursor):result.next_cursor!=="")||
    result.matches.some(m=>!validMatch(m))||new Set(result.matches.map(m=>JSON.stringify([m.index.asset_id,m.index.version,m.line.line_number]))).size!==result.matches.length){throw new Error("Workspace search identity or citations changed; start a new search.")}
   matches=result.matches.map(m=>({index:{...m.index},line:{...m.line}}));cursor=result.next_cursor;hasMore=result.has_more;started=true;
  }catch(e){if(!live()||request!==ticket)return;matches=[];cursor="";hasMore=false;error=String(e?.message||"Workspace text search unavailable")}
  finally{if(live()&&request===ticket){pending=false;publish()}}
 }
 return {snapshot,start:q=>run(false,q),next:()=>run(true),dispose(){disposed=true;ticket++;matches=[];cursor="";query="";hasMore=false;pending=false}};
}

function mountWorkspaceTextSearch(host,selection,current=()=>true){
 const make=(tag,text,className)=>{const e=document.createElement(tag);if(text)e.textContent=text;if(className)e.className=className;return e};
 host.append(make("strong","Search indexed text files"),make("p","Search saved UTF-8 text indexes with current Workspace access. Word paragraphs are available from each document's extraction controls. Continue search checks another group of files or lines. Start a new search to include earlier files changed during paging.","list-meta"));
 const form=make("form","","toolbar"),label=make("label","Literal phrase (case sensitive)","a64-search-label"),input=make("input");input.type="search";input.maxLength=256;input.setAttribute("aria-label","Search indexed text in this Workspace");label.append(input);
 const search=make("button","Search text","btn"),next=make("button","Continue search","btn");search.type="submit";next.type="button";next.disabled=true;form.append(label,search,next);host.append(form);
 const status=make("p","No document text is loaded automatically.","list-meta");status.setAttribute("role","status");const output=make("div");output.setAttribute("aria-label","Current page of authorised source citations");host.append(status,output);
 let state=null;
 const updateNext=()=>{next.disabled=!state||state.pending||!state.hasMore||input.value!==state.query};
 const controller=createWorkspaceTextSearchController({api:apiRequest,selection,current:()=>host.isConnected&&current(),change:s=>{
  state=s;input.disabled=s.pending;search.disabled=s.pending;updateNext();output.replaceChildren();
  status.textContent=s.pending?"Checking authorised indexed text…":s.error||(!s.started?"No document text is loaded automatically.":s.hasMore?
   (s.matches.length?"Matches on this page; more indexed text remains to scan.":"No matches on this page. Continue search to check more documents."):
   "Finished the current forward scan of indexed text.");
  if(s.started&&!s.pending&&!s.error)output.append(make("p","Literal phrase: "+s.query,"list-meta"));
  for(const m of s.matches){const row=make("article","","a46-library-version");row.append(make("strong",m.index.asset_id+" · v"+m.index.version+" · line "+m.line.line_number),make("pre",m.line.text,"a46-library-preview"),make("p",m.index.content_hash+" · bytes "+m.line.byte_start+"–"+m.line.byte_end+" (end exclusive) · "+m.line.line_hash,"list-meta"));output.append(row)}
  if(s.started&&!s.matches.length&&!s.hasMore&&!s.error&&!s.pending)output.append(make("p","No matching lines on this page.","list-meta"));
 }});
 form.addEventListener("submit",e=>{e.preventDefault();controller.start(input.value)});next.addEventListener("click",()=>controller.next());input.addEventListener("input",updateNext);
 return {...controller,dispose(){controller.dispose();host.replaceChildren()}};
}
