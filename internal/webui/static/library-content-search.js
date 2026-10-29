/* One explicit verified search across raw text and extracted Word paragraphs.
 * The server revalidates both formats after all source I/O. Format-native
 * citations remain separate; this is neither semantic search nor attachment. */
function createWorkspaceContentSearchController({api,selection,current=()=>true,change=()=>{}}){
 const scope=Object.freeze({...selection}),path="/v1/projects/"+encodeURIComponent(scope.projectID)+"/workspaces/"+encodeURIComponent(scope.workspaceID)+"/library/search-content";
 const cloneText=m=>({index:{...m.index},line:{...m.line}}),cloneWord=m=>({index:{...m.index,parts:m.index.parts.map(p=>({...p}))},unit:{...m.unit}});
 let pending=false,disposed=false,query="",cursor="",textMatches=[],wordMatches=[],hasMore=false,error="",started=false,sequence=0;
 const alive=()=>!disposed&&current();
 const snapshot=()=>({pending,query,textMatches:textMatches.map(cloneText),wordMatches:wordMatches.map(cloneWord),hasMore,error,started});
 const publish=()=>{if(alive())change(snapshot())};
 async function run(next,value){
  if(!alive()||pending||(next&&!hasMore))return;
  if(!next){query=String(value);cursor="";hasMore=false}
  textMatches=[];wordMatches=[];error="";
  if(!query.trim()||new TextEncoder().encode(query).length>256||query.includes("\0")){cursor="";hasMore=false;error="Enter a phrase of at most 256 UTF-8 bytes.";publish();return}
  pending=true;const ticket=++sequence;publish();
  try{
   const result=await api(path+"?q="+encodeURIComponent(query)+(next?"&cursor="+encodeURIComponent(cursor):""));
   if(!alive()||ticket!==sequence)return;
   if(result?.query!==query||!Array.isArray(result.text_matches)||result.text_matches.length>8||!Array.isArray(result.word_sources)||result.word_sources.length>2||!Array.isArray(result.word_matches)||result.word_matches.length>8||typeof result.has_more!=="boolean"||typeof result.next_cursor!=="string"||(result.has_more?!/^[A-Za-z0-9_-]{43}$/.test(result.next_cursor):result.next_cursor!==""))throw Error("Content search identity or bounded navigation changed; start a new search.");
   // Reuse the existing format validators without another network request or
   // independently fetched excerpt. Both validate this same combined response.
   const common={query,has_more:result.has_more,next_cursor:result.next_cursor};
   const text=createWorkspaceTextSearchController({selection:scope,current:alive,api:async()=>({...common,matches:result.text_matches})});
   const word=createWorkspaceDocumentSearchController({selection:scope,current:alive,api:async()=>({...common,sources:result.word_sources,matches:result.word_matches})});
   try{
    await Promise.all([text.start(query),word.start(query)]);
    if(!alive()||ticket!==sequence)return;
    const t=text.snapshot(),d=word.snapshot();
    if(t.error||d.error)throw Error("Content search citations are inconsistent; start a new search.");
    textMatches=t.matches.map(cloneText);wordMatches=d.matches.map(cloneWord);
   }finally{text.dispose();word.dispose()}
   cursor=result.next_cursor;hasMore=result.has_more;started=true;
  }catch(e){if(!alive()||ticket!==sequence)return;textMatches=[];wordMatches=[];cursor="";hasMore=false;error=String(e?.message||"Content search unavailable")}
  finally{if(alive()&&ticket===sequence){pending=false;publish()}}
 }
 return {snapshot,start:q=>run(false,q),next:()=>run(true),dispose(){disposed=true;sequence++;pending=false;query="";cursor="";textMatches=[];wordMatches=[];hasMore=false;error="";started=false}};
}

function mountWorkspaceContentSearch(host,selection,current=()=>true){
 const make=(tag,text,className)=>{const e=document.createElement(tag);if(text)e.textContent=text;if(className)e.className=className;return e};
 const title=make("strong","Search document contents"),note=make("p","Search indexed UTF-8 files and extracted Word paragraphs in this Workspace with one exact phrase. Index or extract each file on its card first. Raw-file lines and Word paragraphs keep their own source citations. Nothing is sent to a model.","list-meta");
 const form=make("form","","toolbar"),label=make("label","Phrase (case sensitive)","a64-search-label"),input=make("input");input.type="search";input.maxLength=256;input.setAttribute("aria-label","Search indexed text and Word paragraphs in this Workspace");label.append(input);
 const search=make("button","Search contents","btn"),next=make("button","Continue content search","btn");search.type="submit";next.type="button";next.disabled=true;form.append(label,search,next);
 const status=make("p","Document text is loaded only when you search.","list-meta");status.setAttribute("role","status");const output=make("div");host.append(title,note,form,status,output);
 let state=null;const updateNext=()=>{next.disabled=!state||state.pending||!state.hasMore||input.value!==state.query};
 const controller=createWorkspaceContentSearchController({api:apiRequest,selection,current:()=>host.isConnected&&current(),change:s=>{
  state=s;input.disabled=s.pending;search.disabled=s.pending;updateNext();output.replaceChildren();
  const count=s.textMatches.length+s.wordMatches.length;
  status.textContent=s.pending?"Checking permitted text and Word sources…":s.error||(!s.started?"Document text is loaded only when you search.":s.hasMore?(count?"Matches on this page; continue to check more indexed content.":"No matches on this page. Continue content search to check more files."):"Finished the current forward scan of indexed text and Word paragraphs.");
  if(s.textMatches.length)output.append(make("h4","Raw-file text lines"));
  for(const m of s.textMatches){const row=make("article","","a46-library-version");row.append(make("strong",m.index.asset_id+" · v"+m.index.version+" · line "+m.line.line_number),make("pre",m.line.text,"a46-library-preview"),make("p",m.index.content_hash+" · bytes "+m.line.byte_start+"–"+m.line.byte_end+" (end exclusive) · "+m.line.line_hash,"list-meta"));output.append(row)}
  if(s.wordMatches.length)output.append(make("h4","Extracted Word paragraphs"));
  for(const m of s.wordMatches){const row=make("article","","a46-library-version");row.append(make("strong",m.index.asset_id+" · v"+m.index.version+" · "+m.unit.part_name+" · paragraph "+m.unit.paragraph_number),make("pre",m.unit.text,"a46-library-preview"),make("p","Source: "+m.index.raw_source_sha+" · Extracted text: "+m.index.derived_sha+" · Paragraph: "+m.unit.text_hash,"list-meta"));output.append(row)}
  if(s.started&&!s.pending&&!s.error&&!count&&!s.hasMore)output.append(make("p","No matching indexed content on this page.","list-meta"));
 }});
 form.addEventListener("submit",e=>{e.preventDefault();controller.start(input.value)});next.addEventListener("click",()=>controller.next());input.addEventListener("input",updateNext);
 return {...controller,dispose(){controller.dispose();host.replaceChildren()}};
}
