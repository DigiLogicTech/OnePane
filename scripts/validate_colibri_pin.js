#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const fs=require("node:fs"),path=require("node:path"),vm=require("node:vm");
const root=path.join(__dirname,"..");
const read=x=>fs.readFileSync(path.join(root,x),"utf8");
const modelJS=read("internal/webui/static/models-page.js");
const name="function a31InstalledModelsMarkup(";
const start=modelJS.indexOf(name),end=modelJS.indexOf("function a31LlamaRuntimeCard(",start);
assert.ok(start>=0&&end>start,"Local Models markup renderer missing");
const renderer=vm.runInNewContext(modelJS.slice(start,end)+"\na31InstalledModelsMarkup;",{
 escapeHtml:s=>String(s).replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll('"',"&quot;")
});
const native={deployment_id:"colibri-id",runtime_name:"colibri",runtime_backend:"colibri",
 display_name:"Large Local Model",status:"ready",colibri_pinned:true};
const nativeHTML=renderer([native]);
assert.ok(nativeHTML.includes('data-a31-colibri-pin="colibri-id"'));
assert.ok(nativeHTML.includes("checked"));
assert.ok(nativeHTML.includes("Pin to Colibri"));
assert.ok(!/<input[^>]*data-a31-colibri-pin="colibri-id"[^>]*disabled/.test(nativeHTML));
assert.ok(nativeHTML.includes("Tiering"));
const gguf={...native,deployment_id:"gguf-id",runtime_name:"llamacpp",runtime_backend:"cuda",colibri_pinned:false};
const ggufHTML=renderer([gguf]);
assert.ok(/<input[^>]*data-a31-colibri-pin="gguf-id"[^>]*disabled/.test(ggufHTML));
assert.ok(ggufHTML.includes("GGUF/llama.cpp"));
assert.ok(!ggufHTML.includes("checked"));
const fakeRuntime={...native,deployment_id:"bad-id",runtime_backend:"cpu",colibri_pinned:false};
assert.ok(/<input[^>]*data-a31-colibri-pin="bad-id"[^>]*disabled/.test(renderer([fakeRuntime])));
const empty=renderer([]);
assert.ok(empty.includes("No managed local models yet"));
assert.ok(modelJS.includes('box.checked=!desired'));
assert.ok(modelJS.includes('method:"PATCH",body:JSON.stringify({workspace_id:onepaneWorkspace,pinned:desired})'));
assert.ok(modelJS.includes("await renderModels()"));
const api=read("internal/api/local_ai_colibri_tier.go");
const server=read("internal/api/server.go");
for(const str of ['s.authenticate(w,r)','s.authorizeColibriDeployment(w,r,ws,dep,"model.write")',
 's.authorize(w,r,i,ws,"model.write")','if in.Pinned==nil']){
 assert.ok(api.includes(str),"missing backend authorization/validation: "+str);
}
assert.ok(server.includes('PATCH /v1/local-ai/deployments/{deploymentID}/colibri-pin'));
const pin=read("internal/localai/colibri_pin.go");
assert.ok(pin.includes("m.status<>'removed'"));
assert.ok(pin.includes("LOWER(runtime_name)='colibri'"));
assert.ok(pin.includes("json_set(runtime_config_json,'$.colibri_pinned'"));
const swap=read("internal/localai/colibri_hotswap.go");
assert.ok(swap.includes("if c.Pinned"));
const supervisor=read("internal/localai/supervisor.go");
assert.ok(supervisor.includes("json_extract(d.runtime_config_json,'$.colibri_pinned'),0)<>1"));
const migration=read("migrations/0043_colibri_model_pin.sql");
assert.ok(migration.includes("CREATE UNIQUE INDEX"));
console.log("PASS: Colibri pin checkbox, ineligible GGUF denial, optimistic UI rollback, scoped APIs, durable pin and pin-aware residency");
