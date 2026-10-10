/* Opt-in backend HTTP diagnostic controls, no automatic network requests.
 * Capture ID is held only in JS memory, and used solely to tag subsequent
 * same-origin JSON API calls by the existing apiRequest helper.
 */
(function(root){
 "use strict";
 const subsystem=new Set(["projects","tasks","models","nodes","agents","providers","system","other"]);
 const outcome=new Set(["2xx","3xx","4xx","5xx","other"]);
 const method=new Set(["GET","POST","PUT","PATCH","DELETE","HEAD","OPTIONS","OTHER"]);
 const idPattern=/^[0-9a-f]{48}$/;
 let captureID="",endAt=0;
 function activeID(){
  if(captureID&&Date.now()>=endAt){captureID="";endAt=0}
  return captureID;
 }
 function arm(response){
  const id=response?.capture_id,ends=Date.parse(response?.ends_utc||"");
  if(response?.status!=="recording"||!idPattern.test(id||"")||
   !Number.isFinite(ends)||ends<=Date.now()||ends-Date.now()>301000){
   captureID="";endAt=0;return false;
  }
  captureID=id;endAt=ends;return true;
 }
 function disarm(){captureID="";endAt=0}
 const safeInt=(v,max)=>Number.isSafeInteger(v)&&v>=0&&v<=max?v:0;
 const knownLevel=x=>["normal","verbose"].includes(x)?x:"unavailable";
 const knownStatus=x=>["recording","stopped","expired"].includes(x)?x:"unavailable";
 function project(raw){
  if(!raw||raw.schema_version!==1||raw.source!=="operator_tagged_local_http"||
   !Array.isArray(raw.counters)||raw.counters.length>40||
   !Array.isArray(raw.observations)||raw.observations.length>64)
   throw Error("Unrecognised backend capture schema");
  const rows=[];
  for(const item of raw.observations){
   if(!item||!subsystem.has(item.subsystem)||!method.has(item.method))continue;
   if(!Number.isInteger(item.status_code)||item.status_code<100||item.status_code>599)continue;
   const timestamp=typeof item.at_utc==="string"&&item.at_utc.length<=35&&
    /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z$/.test(item.at_utc)?item.at_utc:"unavailable";
   rows.push({subsystem:item.subsystem,method:item.method,
    status_code:item.status_code,duration_ms:safeInt(item.duration_ms,60000),
    at_utc:timestamp});
  }
  const counts=[];
  for(const item of raw.counters){
   if(item&&subsystem.has(item.subsystem)&&outcome.has(item.outcome)){
    counts.push({subsystem:item.subsystem,outcome:item.outcome,count:safeInt(item.count,65535)});
   }
  }
  return {schema_version:1,source:"operator_tagged_local_http",status:knownStatus(raw.status),
   level:knownLevel(raw.level),observed_requests:safeInt(raw.observed_requests,65535),
   dropped_observations:safeInt(raw.dropped_observations,65535),
   instrumentation_update_us:safeInt(raw.instrumentation_update_us,100000000),
   counters:counts,observations:rows,
   exclusions:"No URL, path, query, request/response body, credentials, cookies, IP addresses or cross-operator capture. No external Tool Gateway or model proof."};
 }
 function format(raw){
  const v=project(raw);
  return ["OnePane RC11 — locally owned tagged HTTP diagnostics",
   "Mode: "+v.level+" · state: "+v.status,
   "Observed tagged JSON requests: "+v.observed_requests,
   "Oldest detailed observations dropped: "+v.dropped_observations,
   "Collector update time only: "+v.instrumentation_update_us+" microseconds (not total instrumentation overhead)",
   "Counts by fixed subsystem/HTTP outcome:",
   ...v.counters.map(c=>"  "+c.subsystem+" / "+c.outcome+": "+c.count),
   "Recent per-request observations (verbose only, max 64):",
   ...v.observations.map(e=>"  "+e.at_utc+" "+e.subsystem+" "+e.method+" status="+e.status_code+" duration_ms="+e.duration_ms),
   "Unobserved: Task/Worker internals, independent tool success, model outputs, requests not tagged by this operator",
   "Excluded: paths, URLs, bodies, headers, credentials and other administrator sessions"
  ].join("\n");
 }
 function markup(){
  return `<article class="panel-card a58-debug-tile a61-api-panel" aria-label="Time limited backend HTTP diagnostics">
   <h3>Backend HTTP capture</h3>
   <p class="list-meta">Node administrators only. Start a 30–300 second capture of JSON API requests explicitly tagged by this browser's OnePane API client. Normal mode saves counts only; Verbose mode also retains the last 64 coarse HTTP observations. This does not enable server-wide logging or collect other users' traffic.</p>
   <div class="a61-api-controls">
    <label>Detail <select id="a61Mode"><option value="normal">Normal — outcome counters only</option><option value="verbose">Verbose — bounded HTTP event metadata</option></select></label>
    <label>Duration <select id="a61Seconds"><option value="60">60 seconds</option><option value="120">120 seconds</option><option value="300">300 seconds</option></select></label>
    <button class="btn primary" type="button" id="a61Start">Start backend capture</button>
    <button class="btn" type="button" id="a61Refresh">Refresh capture status</button>
    <button class="btn" type="button" id="a61Stop">Stop capture</button>
   </div>
   <p class="list-meta" id="a61Message" role="status">Inactive until explicitly started; requires Node Admin privileges. Results are available only to the credential that started the session.</p>
   <details id="a61Details"><summary>Reviewed, sanitised HTTP observations</summary>
    <pre id="a61Preview" class="a49-qa-snapshot-preview a58-debug-preview" aria-label="Backend HTTP diagnostic projection"></pre>
   </details>
   <p class="list-meta">Capture is memory-only and automatically expires, with at most two further minutes for inspection. The collector measures only its own counter-update time, not full middleware overhead. These observations are not included in a QA ZIP or uploaded.</p>
  </article>`;
 }
 function bind(host,request){
  if(!host)return;
  const get=id=>host.querySelector("#"+id);
  const mode=get("a61Mode"),seconds=get("a61Seconds");
  const start=get("a61Start"),refresh=get("a61Refresh"),stop=get("a61Stop");
  const message=get("a61Message"),preview=get("a61Preview"),details=get("a61Details");
  let busy=false,owned=false;
  function setButtons(){start.disabled=busy||owned;stop.disabled=busy||!owned;refresh.disabled=busy}
  function receive(data){
   const view=project(data);
   preview.textContent=format(data);details.open=true;
   owned=view.status==="recording"&&arm(data);
   if(!owned)disarm();
   message.textContent="Owned capture: "+view.status+" · "+view.level+
    " · "+view.observed_requests+" tagged requests"+
    (view.status==="expired"?" (recording automatically expired)":"");
   setButtons();
  }
  function run(task){
   busy=true;setButtons();
   Promise.resolve().then(task).catch(()=>{
    disarm();owned=false;preview.textContent="";
    message.textContent="Backend diagnostics unavailable or Node Admin authorisation denied. No evidence shown.";
   }).finally(()=>{busy=false;setButtons()});
  }
  start.onclick=()=>run(async()=>{
   const data=await request("/v1/qa/api-capture/start",{
    method:"POST",body:JSON.stringify({level:mode.value,seconds:Number(seconds.value)})});
   receive(data);
  });
  refresh.onclick=()=>run(async()=>receive(await request("/v1/qa/api-capture",{cache:"no-store"})));
  stop.onclick=()=>run(async()=>{
   const data=await request("/v1/qa/api-capture/stop",{method:"POST"});
   receive(data);
  });
  setButtons();
 }
 root.a61APICapture={id:activeID,arm,disarm,project,format,markup,bind};
 if(typeof module==="object"&&module.exports)module.exports={project,format,markup};
})(typeof globalThis!=="undefined"?globalThis:this);
