const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");
const html=fs.readFileSync("internal/webui/static/index.html","utf8");
const main=fs.readFileSync("internal/webui/static/app.js","utf8");
const spec=fs.readFileSync("internal/webui/static/colibri-spec-sheet.js","utf8");
const tourAnchor=main.slice(main.indexOf("/* Product tour: crisp target"),main.indexOf("/* Inspector: Overview"));
for(const title of ["Web Chat is a separate workspace","Start Web-only Research Council","AI Chair and human approvals","Colibri tiered inference","Colibri whole-model hot swap","Model spec sheet and Agent Check","Cloud Models and OmniRoute","Projects and Workspaces","Inspector and observability","Settings, Help and replay"]){
 assert.ok(tourAnchor.includes(title),"Tour must cover "+title);
}
for(const tag of ["web-chat-council-launch.js","web-chat-chair.js","colibri-tiered-ui.js","colibri-spec-sheet.js"]){
 assert.ok(html.includes('src="/'+tag+'"'),"HTML must load "+tag);
}
assert.ok(html.indexOf('src="/colibri-spec-sheet.js"')>html.indexOf('src="/models-followup-view.js"'));
let observedPlanCalls=0;
const model={deployment_id:"dep-123",display_name:"MoE test",runtime_backend:"colibri"};
const context=vm.createContext({
 qa4Inspector:{kind:"model",id:"dep-123",data:model},
 qa4InspectorOverview:()=>'<section class="model-spec-inspector"><button class="btn primary" id="qa5InspectorAgentCheck">Agent Check</button></section>',
 qa5InspectModel:async()=>{},
 a37Section:(title,html)=>'<section><h3>'+title+'</h3>'+html+'</section>',
 a37Field:(label,v)=>'<div>'+label+': '+v+'</div>',
 escapeHtml:s=>String(s).replaceAll("<","&lt;").replaceAll(">","&gt;").replaceAll('"',"&quot;"),
 onepaneWorkspace:"ws-1",
 apiRequest:async(url)=>{if(url.includes("/colibri-plan"))observedPlanCalls++;return {}},
 document:{addEventListener:()=>{}},
 Map,Array,Set,Number,String,JSON,Promise,
 renderInspector:()=>{},
 notice:()=>{},
 openModal:()=>{}
});
vm.runInContext(spec,context,{filename:"colibri-spec-sheet.js"});
const before=vm.runInContext("a43ColibriSpecMarkup",context)(model);
assert.match(before,/Loading configured placement/);
const cache=vm.runInContext("a43ColibriSpecCache",context);
cache.set("dep-123",{tier:{settings:{
 mode:"balanced",backend:"vulkan",ram_gb:16,expert_cap:128,repin_tokens:4096,
 gpu_index:0,vulkan_experts:96},plan_available:true},
 swap:{active_deployment_ids:["dep-123"],status:"healthy"}});
const result=vm.runInContext("qa4InspectorOverview()",context);
for(const expected of [
 "Colibri: tiered memory and hot swap","VRAM","RAM","SSD","Balanced",
 "VULKAN","16 GB","128 experts","4,096 tokens","96 experts","Resident",
 "View read-only plan","Configure tiering","Hot swap model",
 "Not measured by OnePane","estimate"
]) assert.ok(result.includes(expected),"Missing Colibri spec information: "+expected);
assert.equal(observedPlanCalls,0,"reading Spec Sheet must not run Colibri planner");
console.log("Colibri spec distinctions, model residency, no implicit plan execution, loaded assets and feature tour: PASS");
