/* RC11 Debug Centre: one operator entry, no new privilege path.
 * Reuses the existing tab-scoped, opt-in browser recorder and the strict
 * sanitiser used by the consolidated QA ZIP. It never collects raw logs.
 */
(function(root) {
 "use strict";
 const MAX_REVIEW_AGE_MS=120000;
 const MAX_JSON_BYTES=65536;
 function makeReview(snapshot, project, nowMs) {
  if(!project || typeof project.browser!=="function")throw Error("Sanitiser unavailable");
  if(!snapshot || !["stopped","expired"].includes(snapshot.status) ||
    !Array.isArray(snapshot.events) || !snapshot.events.length)
   throw Error("Stop a nonempty browser capture before reviewing it");
  const safe=project.browser(snapshot);
  const json=JSON.stringify(safe,null,2);
  if(new TextEncoder().encode(json).length>MAX_JSON_BYTES)
   throw Error("Sanitised browser review exceeds the size cap");
  return {json,fingerprint:JSON.stringify(snapshot),reviewedAt:nowMs};
 }
 function validReview(review,snapshot,preview,nowMs) {
  if(!review || !snapshot || !Number.isFinite(nowMs) ||
   !Number.isFinite(review.reviewedAt) || nowMs<review.reviewedAt ||
   nowMs-review.reviewedAt>MAX_REVIEW_AGE_MS)return false;
  if(!["stopped","expired"].includes(snapshot.status))return false;
  return JSON.stringify(snapshot)===review.fingerprint && preview===review.json;
 }
 function centreMarkup() {
  return `<section class="settings-section a58-debug-centre" aria-label="Debug and Diagnostics Centre">
    <p class="list-meta">Operator-controlled diagnostics. Browser capture is off by default, expires after 10 minutes and remains in this browser tab only. No automatic upload or change to model/agent policy.</p>
    <div class="a58-debug-grid">
     <article class="panel-card a58-debug-tile">
      <h3>Browser incident capture</h3>
      <p class="list-meta">Records category-only UI interactions, same-origin API status/duration and browser error counts. Never records typed text, raw URLs, HTTP bodies, prompts or error messages.</p>
      <div id="a58CaptureState" class="a58-debug-state" role="status" aria-live="polite">Capture not started</div>
      <div class="a58-debug-actions">
       <button class="btn primary" type="button" id="a58StartCapture">Start capture</button>
       <button class="btn" type="button" id="a58MarkIssue">Mark issue</button>
       <button class="btn" type="button" id="a58StopCapture">Stop capture</button>
       <button class="btn" type="button" id="a58ClearCapture">Clear</button>
      </div>
      <div class="a58-debug-actions">
       <button class="btn" type="button" id="a58ReviewTrace">Review sanitised trace</button>
       <button class="btn" type="button" id="a58DownloadTrace" disabled>Download reviewed JSON</button>
      </div>
      <p id="a58CaptureMessage" class="list-meta" role="status">No capture is currently being collected by this panel.</p>
      <details id="a58TraceDetails"><summary>Reviewed browser evidence</summary>
       <pre id="a58TracePreview" class="a49-qa-snapshot-preview a58-debug-preview" aria-label="Sanitised browser trace"></pre>
      </details>
     </article>
     <article class="panel-card a58-debug-tile">
      <h3>Scoped backend evidence</h3>
      <p class="list-meta">Use the existing authorised source views. This centre cannot grant access to another Project, Workspace, model or Node.</p>
      <div class="a58-debug-links">
       <button class="btn" type="button" data-a58-destination="workspaces">Workspace: scoped Task/Worker trace correlation and reviewed QA ZIP</button>
       <button class="btn" type="button" data-a58-destination="models">Models: Agent Check and residency evidence</button>
       <button class="btn" type="button" data-a58-destination="nodes">Nodes: admin-only service and backend readiness</button>
       <button class="btn" type="button" data-a58-destination="operations">Operations: Logs, activity and recovery</button>
      </div>
      <p class="list-meta">The consolidated support ZIP is generated inside a selected canonical Workspace after per-source authorisation and a human review. Export rechecks current permissions.</p>
     </article>
    </div>
    <section class="a58-debug-offline" aria-label="Offline diagnostic guidance">
     <h3>If OnePane cannot start</h3>
     <p>The Windows service and Ubuntu backend can retain bounded, typed startup events outside the WebUI. A standalone, opt-in MSI log reader is also available in the source tree. These tools do not run automatically here, and raw installer/service logs are never silently included in a QA ZIP.</p>
     <p class="list-meta">Offline evidence must be read using local filesystem permissions. This online panel cannot retrieve a backend that failed before HTTP startup or prove that a skipped physical Node test passed.</p>
    </section>
   </section>`;
 }
 function bindCentre(host, recorder, projector, nav) {
  if(!host)return;
  const get=id=>host.querySelector("#"+id);
  const controls={
   start:get("a58StartCapture"), mark:get("a58MarkIssue"),stop:get("a58StopCapture"),
   clear:get("a58ClearCapture"),review:get("a58ReviewTrace"),
   download:get("a58DownloadTrace")
  };
  const stateEl=get("a58CaptureState"),message=get("a58CaptureMessage"),
   preview=get("a58TracePreview"),details=get("a58TraceDetails");
  let reviewed=null;
  const clearReview=()=>{reviewed=null;preview.textContent="";controls.download.disabled=true};
  function paint() {
   if(!recorder||typeof recorder.snapshot!=="function") {
    Object.values(controls).forEach(b=>b.disabled=true);
    stateEl.textContent="Browser recorder unavailable";
    message.textContent="This build cannot capture browser diagnostics.";
    return;
   }
   const s=recorder.snapshot(),active=s.status==="recording";
   controls.start.disabled=active;controls.mark.disabled=!active;
   controls.stop.disabled=!active;controls.clear.disabled=s.status==="idle";
   controls.review.disabled=active||!s.events.length;
   controls.download.disabled=!validReview(reviewed,s,preview.textContent,Date.now());
   stateEl.textContent="Capture: "+s.status+" · "+s.events.length+
    " of "+s.max_events+" retained events"+(s.dropped_events?" · "+s.dropped_events+" older events omitted":"");
  }
  if(recorder&&typeof recorder.snapshot==="function") {
   controls.start.onclick=()=>{
    clearReview();recorder.start();message.textContent="Capturing generic events in this browser tab only.";paint();
   };
   controls.mark.onclick=()=>{recorder.mark();message.textContent="Issue marker recorded without any user-entered text.";paint()};
   controls.stop.onclick=()=>{recorder.stop();clearReview();message.textContent="Capture stopped. Review before downloading.";paint()};
   controls.clear.onclick=()=>{recorder.clear();clearReview();message.textContent="In-memory capture discarded.";paint()};
   controls.review.onclick=()=>{
    clearReview();
    try {
     reviewed=makeReview(recorder.snapshot(),projector,Date.now());
     preview.textContent=reviewed.json;details.open=true;
     message.textContent="Review the exact sanitised JSON. Download expires in two minutes or if capture changes.";
    }catch(_) {message.textContent="No valid stopped capture available for review."; }
    paint();
   };
   controls.download.onclick=()=>{
    const snapshot=recorder.snapshot();
    if(!validReview(reviewed,snapshot,preview.textContent,Date.now())) {
     clearReview();message.textContent="Capture or review changed/expired. Review again.";paint();return;
    }
    const bytes=new TextEncoder().encode(reviewed.json+"\n");
    if(bytes.length>MAX_JSON_BYTES){clearReview();message.textContent="Safe export limit exceeded.";paint();return}
    const blob=new Blob([bytes],{type:"application/json"});
    const url=URL.createObjectURL(blob);
    try{
     const a=document.createElement("a");
     a.href=url;a.download="onepane-reviewed-browser-incident.json";
     a.style.display="none";document.body.appendChild(a);a.click();a.remove();
    }finally{URL.revokeObjectURL(url)}
    clearReview();message.textContent="Reviewed browser evidence exported locally. Inspect before sharing.";paint();
   };
  }
  for(const b of host.querySelectorAll("[data-a58-destination]")) {
   b.onclick=()=>nav(b.dataset.a58Destination);
  }
  paint();
 }
 // Only direct, user-triggered navigation into already-existing authorised UIs.
 function navigate(dest) {
  if(!["workspaces","models","nodes","operations","projects"].includes(dest))return;
  if(typeof root.openRoute==="function")root.openRoute(dest);
 }
 function mount() {
  const host=document.querySelector("#a31SettingsContent .a58-debug-centre");
  if(!host)return;
  bindCentre(host,root.a54QACapture,root.a56SupportBundle,navigate);
 }
 // The drawer is an entry point, not a second recorder. All effects are local
 // to the authorised Settings page the operator explicitly opens.
 if(root.document)root.document.addEventListener("click",e=>{
  if(!e.target?.closest?.("#a58OpenDebugCentre"))return;
  if(typeof root.a31SetSettingsView!=="function" || typeof root.openRoute!=="function")return;
  if(root.currentTab?.()?.route==="settings")root.a31SetSettingsView("diagnostics");
  else {
   // a31SetSettingsView only re-renders if already in Settings.
   root.a31SetSettingsView("diagnostics");root.openRoute("settings");
  }
 });
 root.a58DebugCentre={centreMarkup,bindCentre,mount,makeReview,validReview};
 if(typeof module==="object"&&module.exports)module.exports={centreMarkup,makeReview,validReview};
})(typeof globalThis!=="undefined"?globalThis:this);
