/* RC11 opt-in browser incident recorder.
 * Never records URL paths/queries, request bodies, response bodies,
 * form values, user text, exception messages, stack traces or credentials.
 * The capture is memory-only, expires, and cannot widen backend permissions.
 */
(function(root){
 "use strict";
 const MAX_EVENTS=120, MAX_DURATION_MS=10*60*1000;
 const routes=new Set([
  "operations","workspaces","projects","tasks","models","nodes",
  "agents","sandboxes","routines","providers","integrations",
  "secrets","evidence","settings","library","assistant","skills",
  "web-chat","chat"
 ]);
 const methods=new Set(["GET","POST","PUT","PATCH","DELETE","HEAD","OPTIONS"]);
 function safeRoute(v){return routes.has(v)?v:"other"}
 function safeMethod(v){
  const method=String(v||"GET").toUpperCase();
  return methods.has(method)?method:"OTHER";
 }
 function subsystem(path){
  if(/^\/v1\/qa(?:\/|$)/.test(path))return "diagnostics";
  if(/^\/v1\/(?:local-ai|model-deployments|model-testbed|models|model-routing)(?:\/|$)/.test(path))return "models";
  if(/^\/v1\/(?:projects|library|project-workspaces)(?:\/|$)/.test(path))return "projects";
  if(/^\/v1\/(?:tasks|routines|plans)(?:\/|$)/.test(path))return "tasks";
  if(/^\/v1\/(?:nodes|node-federation)(?:\/|$)/.test(path))return "nodes";
  if(/^\/v1\/(?:auth|setup)(?:\/|$)/.test(path))return "authentication";
  if(/^\/v1\/(?:providers|provider-onboarding|cloud-models)(?:\/|$)/.test(path))return "providers";
  if(/^\/v1\/(?:agent-profiles|agent-sessions|agent-runtimes|teams)(?:\/|$)/.test(path))return "agents";
  if(/^\/v1\/(?:events|assurance|verifications)(?:\/|$)/.test(path))return "evidence";
  if(/^\/v1\/(?:system|health|about)(?:\/|$)/.test(path))return "system";
  return path.startsWith("/v1/")?"api_other":null;
 }
 function classifyRequest(input,init,origin){
  try{
   const source=typeof input==="string"?input:input&&typeof input.url==="string"?input.url:"";
   if(!source||source.length>8192)return null;
   const u=new URL(source,origin);
   if(u.origin!==origin)return null;
   const area=subsystem(u.pathname);
   if(!area)return null;
   const method=safeMethod(init&&init.method||input&&input.method||"GET");
   return {subsystem:area,method};
  }catch(_){return null}
 }
 function createRecorder(cfg){
  const env=cfg||{};
  const win=env.win||root;
  const now=env.now||(()=>Date.now());
  const schedule=env.schedule||((fn,delay)=>setTimeout(fn,delay));
  const cancel=env.cancel||((id)=>clearTimeout(id));
  const readRoute=env.route||(()=> "other");
  let status="idle",events=[],dropped=0,started=0,ended=0;
  let timer=null,restoreFetch=null,wrappedFetch=null;
  let listeners=[];
  const timestamp=()=>new Date(now()).toISOString();
  function note(event){
   if(status!=="recording")return;
   if(now()-started>=MAX_DURATION_MS){stop("expired");return}
   // Only explicitly constructed enum/number fields can reach this list.
   if(events.length>=MAX_EVENTS){events.shift();dropped++}
   events.push(Object.assign({at_utc:timestamp()},event));
  }
  function on(winEvent,listener){
   win.addEventListener(winEvent,listener);
   listeners.push([winEvent,listener]);
  }
  function detach(){
   for(const [kind,fn] of listeners)win.removeEventListener(kind,fn);
   listeners=[];
   if(wrappedFetch&&win.fetch===wrappedFetch)win.fetch=restoreFetch;
   restoreFetch=null;wrappedFetch=null;
   if(timer!==null){cancel(timer);timer=null}
  }
  function stop(reason){
   if(status!=="recording")return false;
   // Do not copy uncontrolled error reasons.
   const outcome=reason==="expired"?"expired":"stopped";
   // Do not call note() while expiring: note() itself tests the deadline.
   if(events.length>=MAX_EVENTS){events.shift();dropped++}
   events.push({at_utc:timestamp(),kind:"capture",action:"stopped",outcome});
   status=outcome;
   ended=now();
   detach();
   return true;
  }
  function start(){
   if(status==="recording")return false;
   detach();
   events=[];dropped=0;started=now();ended=0;status="recording";
   note({kind:"capture",action:"started",outcome:"recording"});
   on("error",()=>note({kind:"browser",action:"uncaught_error",route:safeRoute(readRoute())}));
   on("unhandledrejection",()=>note({kind:"browser",action:"unhandled_rejection",route:safeRoute(readRoute())}));
   on("click",(e)=>{
    const target=e&&e.target;
    if(!target||typeof target.closest!=="function")return;
    const control=target.closest("button,a,[role=button]");
    if(!control)return;
    const tag=String(control.tagName||"").toUpperCase();
    const action=tag==="A"?"link":tag==="BUTTON"?"button":"control";
    note({kind:"ui",action,route:safeRoute(readRoute())});
   });
   on("submit",()=>note({kind:"ui",action:"form_submit",route:safeRoute(readRoute())}));
   // Wrap ONLY the local browser's fetch while explicitly capturing.
   // Every call is forwarded with identical arguments; only timings/status
   // and a fixed subsystem bucket leave this observer.
   restoreFetch=win.fetch;
   wrappedFetch=function(input,init){
    const identity=classifyRequest(input,init,win.location.origin);
    const before=now();
    let pending;
    try{pending=restoreFetch.apply(win,arguments)}
    catch(err){
     if(identity)note({kind:"api",action:"transport_failure",subsystem:identity.subsystem,
      method:identity.method,duration_ms:Math.min(60000,Math.max(0,now()-before))});
     throw err;
    }
    return Promise.resolve(pending).then((response)=>{
     if(identity)note({kind:"api",action:"response",subsystem:identity.subsystem,
      method:identity.method,status:Number.isInteger(response&&response.status)&&
       response.status>=100&&response.status<=599?response.status:0,
      duration_ms:Math.min(60000,Math.max(0,now()-before))});
     return response;
    },(err)=>{
     if(identity)note({kind:"api",action:"transport_failure",subsystem:identity.subsystem,
      method:identity.method,duration_ms:Math.min(60000,Math.max(0,now()-before))});
     throw err;
    });
   };
   win.fetch=wrappedFetch;
   timer=schedule(()=>stop("expired"),MAX_DURATION_MS);
   return true;
  }
  function mark(){
   if(status!=="recording")return false;
   note({kind:"capture",action:"marked_issue",route:safeRoute(readRoute())});
   return status==="recording";
  }
  function clear(){
   if(status==="recording")stop("manual");
   detach();events=[];dropped=0;started=0;ended=0;status="idle";
  }
  function snapshot(){
   if(status==="recording"&&now()-started>=MAX_DURATION_MS)stop("expired");
   return {
    schema_version:1,source:"onepane_browser_opt_in",
    status,started_utc:started?new Date(started).toISOString():null,
    ended_utc:ended?new Date(ended).toISOString():null,
    max_duration_seconds:MAX_DURATION_MS/1000,max_events:MAX_EVENTS,
    dropped_events:dropped,
    exclusions:["typed inputs","URLs and query strings","request/response bodies",
      "chat prompts and outputs","cookies, headers and secrets",
      "exception text and stack traces","cross-origin fetches","screenshots"],
    events:events.map(e=>Object.assign({},e))
   };
  }
  return {start,stop,mark,clear,snapshot};
 }
 root.a54QACreateRecorder=createRecorder;
 root.a54QACapture=createRecorder({
  win:root,
  route:()=>typeof root.currentTab==="function"?root.currentTab()?.route:"other"
 });
 if(typeof module==="object"&&module&&module.exports){
  module.exports={createRecorder,classifyRequest};
 }
})(typeof globalThis!=="undefined"?globalThis:this);
