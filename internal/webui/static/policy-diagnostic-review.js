/* Explicit, offline review of allowlisted feature-policy decisions.
 * No automatic collection, storage, URLs, or raw-record export.
 */
(function(root){
 "use strict";
 const MAX_INPUT_BYTES=32768,MAX_EVENTS=64;
 const actions=new Set(["inference.local","inference.cloud","inference.fallback","files.read","files.write","projects.share","tasks.autonomous"]);
 const correlation=/^(run|task|attempt|request|trace):[a-zA-Z0-9_.-]{1,96}$/;
 const timestamp=/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/;
 function project(input){
  if(typeof input!=="string"||!input.trim()||new TextEncoder().encode(input).length>MAX_INPUT_BYTES)
   throw Error("Expected at most 32 KiB of local diagnostic JSON");
  let parsed;
  try{parsed=JSON.parse(input)}catch(_){throw Error("Invalid JSON")}
  const source=Array.isArray(parsed)?parsed:parsed?.events;
  if(!Array.isArray(source)||!source.length||source.length>MAX_EVENTS)
   throw Error("Expected 1–64 decision records");
  const events=[];
  for(const record of source){
   if(!record||typeof record!=="object"||Array.isArray(record))continue;
   const allowed=record.Allowed??record.allowed;
   const action=record.Action??record.action;
   const category=record.Category??record.category;
   const component=record.Component??record.component;
   const at=record.At??record.at_utc;
   const id=record.CorrelationID??record.correlation_id;
   if(component!=="featurepolicy"||typeof allowed!=="boolean"||
       category!==(allowed?"policy.allowed":"policy.denied"))continue;
   if(!actions.has(action)&&action!=="redacted")continue;
   const safeAt=typeof at==="string"&&at.length<=40&&timestamp.test(at)&&
    Number.isFinite(Date.parse(at))?at:"unavailable";
   const safeID=typeof id==="string"&&correlation.test(id)?id:"redacted";
   events.push({at_utc:safeAt,action,allowed,category,correlation_id:safeID});
  }
  if(!events.length)throw Error("No valid policy decisions found");
  return {schema_version:1,source:"local_operator_policy_decisions",
   total:events.length,allowed:events.filter(x=>x.allowed).length,
   denied:events.filter(x=>!x.allowed).length,events,
   exclusions:"Offline projection only; no authority check, runtime enforcement or live collection. Unknown fields and raw input are discarded."};
 }
 function markup(){
  return `<article class="panel-card a58-debug-tile a62-policy-panel" aria-label="Local policy decision evidence review">
    <h3>Policy decisions — local review</h3>
    <p class="list-meta">Paste up to 64 diagnostic decision records from an authorised export. This is an offline evidence viewer, not live policy enforcement or automatic telemetry. Unknown fields, prompts and secrets are discarded. Nothing is sent to OnePane or another service.</p>
    <label for="a62PolicyInput">Diagnostic JSON</label>
    <textarea id="a62PolicyInput" rows="3" maxlength="32768" autocomplete="off" spellcheck="false" placeholder='[{"At":"2026-10-11T00:00:00Z","Component":"featurepolicy","CorrelationID":"run:1","Action":"files.read","Allowed":false,"Category":"policy.denied"}]'></textarea>
    <div class="a58-debug-actions">
     <button class="btn primary" id="a62PolicyReview" type="button">Review sanitised decisions</button>
     <button class="btn" id="a62PolicyClear" type="button">Clear</button>
     <button class="btn" id="a62PolicyDownload" type="button" disabled>Download sanitised JSON</button>
    </div>
    <p class="list-meta" id="a62PolicyStatus" role="status">No policy evidence reviewed.</p>
    <details id="a62PolicyDetails"><summary>Allowlisted policy decision evidence</summary>
     <pre id="a62PolicyPreview" class="a49-qa-snapshot-preview a58-debug-preview" aria-label="Sanitised policy decision records"></pre>
    </details>
   </article>`;
 }
 function bind(host){
  if(!host)return;
  const get=id=>host.querySelector("#"+id);
  const input=get("a62PolicyInput"),review=get("a62PolicyReview"),clear=get("a62PolicyClear");
  const download=get("a62PolicyDownload"),status=get("a62PolicyStatus");
  const details=get("a62PolicyDetails"),preview=get("a62PolicyPreview");
  if(![input,review,clear,download,status,details,preview].every(Boolean))return;
  let evidence="";
  const discard=()=>{evidence="";preview.textContent="";download.disabled=true};
  input.addEventListener("input",()=>{discard();status.textContent="Input changed. Review again."});
  clear.onclick=()=>{input.value="";discard();details.open=false;status.textContent="Local policy evidence cleared."};
  review.onclick=()=>{
   discard();
   try{
    const projected=project(input.value);
    evidence=JSON.stringify(projected,null,2);
    input.value=""; // Raw untrusted contents are not retained after review.
    preview.textContent=evidence;details.open=true;download.disabled=false;
    status.textContent=projected.total+" reviewed decisions: "+projected.denied+" denied, "+projected.allowed+" allowed. Only sanitised fields can be exported.";
   }catch(_){
    input.value="";
    status.textContent="Invalid or unsupported policy evidence; nothing retained. Supply 1–64 recognised decision records.";
   }
  };
  download.onclick=()=>{
   if(!evidence||preview.textContent!==evidence){discard();return}
   const url=URL.createObjectURL(new Blob([evidence+"\n"],{type:"application/json"}));
   try{
    const a=document.createElement("a");a.href=url;a.download="onepane-reviewed-policy-decisions.json";
    a.style.display="none";document.body.appendChild(a);a.click();a.remove();
   }finally{URL.revokeObjectURL(url)}
   discard();status.textContent="Sanitised evidence downloaded locally. Review before sharing.";
  };
 }
 root.a62PolicyEvidence={project,markup,bind};
 if(typeof module==="object"&&module.exports)module.exports={project,markup,bind};
})(typeof globalThis!=="undefined"?globalThis:this);
