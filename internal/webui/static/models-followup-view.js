function a37Obj(v){if(v==null)return{};if(typeof v==="object")return v;try{return JSON.parse(v)}catch{return{}}}
function a37Field(label,v){return `<div class="spec-field"><span class="spec-field-label">${escapeHtml(label)}</span><div class="spec-field-value">${escapeHtml(v==null||v===""?"Not reported":String(v))}</div></div>`}
function a37Section(label,content){return `<section class="spec-readable-section"><h3>${escapeHtml(label)}</h3><div class="spec-card-data">${content}</div></section>`}
function a37Check(label,result){const state=result===true?"Pass":result===false?"Fail":"Not tested";return `<div class="spec-check"><span>${escapeHtml(label)}</span><span class="pill ${result===true?"good":result===false?"warn":""}">${escapeHtml(state)}</span></div>`}
const a37OldInspectorOverview=qa4InspectorOverview;
qa4InspectorOverview=function(){
 if(qa4Inspector.kind!=="model")return a37OldInspectorOverview();
 const d=qa4Inspector.data||{},s=a37Obj(d.spec),place=a37Obj(s.placement),q=a37Obj(s.qualification);
 const e=a37Obj(q.evidence||q.evidence_json||s.agent_check),metrics=a37Obj(e.metrics||q.metrics);
 const devs=Array.isArray(place.devices)?place.devices:[],dev=devs.find(x=>x.kind==="accelerator")||devs[0];
 const requested=Number(s.requested_context_tokens||q.requested_context_tokens||0),verified=Number(s.verified_context_tokens||metrics.verified_context||d.context_max_verified||0);
 const speed=s.measured_tps??metrics.tokens_per_second??metrics.completion_tokens_per_second??q.tokens_per_second;
 const errors=Array.isArray(e.errors)?e.errors:[],probes=Array.isArray(e.context_probes)?e.context_probes:[];
 const name=String(dev?.name||dev?.device_name||"Not reported").replaceAll("&amp;","&");
 const layout=a37Section("Runtime and placement",[
  a37Field("Runtime",d.runtime_name||s.runtime_name||"llama.cpp"),
  a37Field("Backend",d.runtime_backend||place.backend||s.runtime_backend||"Automatic"),
  a37Field("Mode",String(place.mode||"Automatic").replaceAll("_"," ")),
  a37Field("Device",name),
  a37Field("Device allocation",dev?.allocated_bytes?bytesQA(dev.allocated_bytes):"Not reported"),
  a37Field("Admission",d.admission_status||s.admission_status||"Pending")
 ].join(""));
 const context=a37Section("Context and performance",[
  a37Field("Requested context",requested?`${requested.toLocaleString()} tokens`:"Not reported"),
  a37Field("Verified context",verified?`${verified.toLocaleString()} tokens${requested?" of "+requested.toLocaleString():""}`:"Not yet verified"),
  a37Field("Generation speed",speed!=null?`${Number(speed).toFixed(2)} tokens/s (${s.measured_tps!=null?"OnePane benchmark":"Agent Check"})`:"Not measured"),
  a37Field("Time to first token",s.measured_ttft_ms!=null?`${Math.round(Number(s.measured_ttft_ms))} ms`:"Not measured")
 ].join(""));
 const allErrors=[...errors,...probes.filter(x=>x.error).map(x=>x.error)];
 const warning=allErrors.length?`<div class="spec-warning">${allErrors.map(x=>escapeHtml(String(x))).join("<hr>")}</div>`:"";
 const qual=a37Section("Agent Check",`<div class="spec-check-grid">${a37Check("Plain-text response",e.plain_ok)}${a37Check("JSON response",e.json_ok)}${a37Check("Schema validation",e.schema_ok)}${a37Check("Tool calling",e.tools_ok)}</div>${a37Field("Qualification",q.status||e.status||"Not tested")}${a37Field("Protocol level",metrics.protocol_level||q.protocol_level||"Not reported")}`);
 if(s.error)return `<div class="error">${escapeHtml(s.error)}</div>`;
 return `<div class="model-spec-inspector readable-model-spec"><div class="spec-readable-head"><strong>${escapeHtml(d.display_name||d.model_ref||"Local model")}</strong><span class="pill">${escapeHtml(d.status||"Unknown")}</span></div>${layout}${context}${warning}${qual}<details class="spec-technical-details"><summary>Advanced technical details and raw evidence</summary><pre class="spec-sheet-json">${escapeHtml(JSON.stringify(s,null,2))}</pre></details><button class="btn primary" id="qa5InspectorAgentCheck">Run Agent Check</button></div>`;
};
const a37LocalBase=a31RenderLocalModels;
a31RenderLocalModels=async function(){
 await a37LocalBase();
 const grid=$("#a31ModelsRoot .local-runtime-grid");if(!grid)return;
 const colibri=grid.querySelector(":scope > .managed-runtime-card"),llama=colibri?.nextElementSibling,hardware=grid.querySelector(".hardware-card"),llmfit=grid.querySelector("#a36LLMFitCard");
 if(!colibri||!llama||!hardware||!llmfit)return;
 const combined=document.createElement("section");combined.className="panel-card managed-runtimes-combined";
 combined.innerHTML='<div class="card-header models-card-header"><div><div class="card-title">Managed Runtimes</div><div class="list-meta">Colibri, llama.cpp and llmfit on this node.</div></div></div><div class="runtime-tabs" role="tablist" aria-label="Managed runtimes"></div><div class="runtime-tab-panels"></div>';
 const tabs=[["colibri","Colibri",colibri],["llamacpp","llama.cpp",llama],["llmfit","llmfit",llmfit]],nav=combined.querySelector(".runtime-tabs"),panels=combined.querySelector(".runtime-tab-panels");
 for(const [id,title,node] of tabs){
  const pane=document.createElement("div");pane.className="runtime-panel";pane.dataset.runtimePane=id;pane.appendChild(node);panels.appendChild(pane);
  const status=node.querySelector(".pill")?.textContent||"";
  nav.insertAdjacentHTML("beforeend",`<button type="button" role="tab" data-runtime-tab="${id}" title="${escapeHtml(title)} · ${escapeHtml(status)}">${escapeHtml(title)} <span class="pill ${/installed|running/i.test(status)?"good":""}">${escapeHtml(status)}</span></button>`);
 }
 grid.insertBefore(combined,hardware);
 const choose=id=>{
  nav.querySelectorAll("[data-runtime-tab]").forEach(b=>{const active=b.dataset.runtimeTab===id;b.classList.toggle("active",active);b.setAttribute("aria-selected",String(active))});
  panels.querySelectorAll("[data-runtime-pane]").forEach(p=>p.hidden=p.dataset.runtimePane!==id);
  localStorage.setItem("onepane-managed-runtime-tab",id)
 };
 nav.querySelectorAll("[data-runtime-tab]").forEach(b=>b.onclick=()=>choose(b.dataset.runtimeTab));
 const saved=localStorage.getItem("onepane-managed-runtime-tab");choose(tabs.some(t=>t[0]===saved)?saved:"llamacpp")
};
