#!/usr/bin/env node
// Validate local/remote Nodes inventory isolation without a browser or real service.
const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");
const source=fs.readFileSync("internal/webui/static/nodes-management.js","utf8");
const localID="node-local",remoteID="node-peer";
const calls=[];
const mock={
 onepaneWorkspace:"ws-test",
 localProfileQA:{
  node_id:localID,cpu:{name:"Test CPU"},
  memory:{total_bytes:16*2**30,available_bytes:8*2**30},
  storage:{available_bytes:25*2**30},
  gpus:[{name:"NVIDIA Test GPU",vram_bytes:6*2**30,free_vram_bytes:3*2**30,backend:"cuda"}]
 },
 escapeHtml:v=>String(v).replaceAll("&","&amp;").replaceAll("<","&lt;"),
 bytesQA:v=>(Number(v)/2**30).toFixed(1)+" GiB",
 titleCase:v=>String(v),setInterval:()=>0,
 localStorage:{getItem:()=>null,setItem:()=>{}},
 apiRequest:async url=>{
  calls.push(url);
  if(url.startsWith("/v1/system/metrics"))return {
   cpu_percent:12.5,memory_total_bytes:16*2**30,
   memory_available_bytes:7*2**30,disk_free_bytes:9*2**30,
   storage_volumes:[{path:"D:\\",roles:["Models","Projects"],free_bytes:25*2**30}]
  };
  if(url.startsWith("/v1/local-ai/deployments"))return {
   deployments:[{deployment_id:"dep-1",model_ref:"model-one",status:"ready",runtime_name:"llamacpp"}]
  };
  if(url==="/v1/nodes/"+remoteID+"/capabilities")return {
   node_id:remoteID,compute:{status:"available",pools:[{kind:"gpu",name:"REMOTE GPU"}]},models:[]
  };
  if(url.endsWith("/model-management")||url.endsWith("/compute-policy"))return {};
  throw Error("Unexpected request "+url);
 },
 document:{querySelector:()=>null},a31RouteIs:()=>false,
};
vm.createContext(mock);
vm.runInContext(source,mock,{filename:"nodes-management.js"});
(async()=>{
 mock.nextNodeUI.nodes=[
  {id:localID,name:"Local",local:true,trust_state:"local",last_seen_at:1791432448310},
  {id:remoteID,name:"Remote",local:false,trust_state:"paired",last_seen_at:1791432448310},
 ];
 mock.nextNodeUI.selected=localID;
 await mock.nextNodeLoadDetail(localID);
 const m=mock.nextNodeUI.manifest;
 assert.equal(m.protocol,"local");
 assert.equal(m.compute.cpu_usage_pct,12.5);
 assert.equal(m.compute.memory_available_bytes,7*2**30);
 assert.equal(m.compute.storage_available_bytes,25*2**30);
 assert.equal(m.compute.pools.length,1);
 assert.equal(m.compute.pools[0].name,"NVIDIA Test GPU");
 assert.equal(m.models.length,1);
 assert.ok(mock.nextNodeOverview(mock.nextNodeUI.nodes[0]).includes("Local management; no remote endpoint"));
 assert.ok(mock.nextNodeOverview(mock.nextNodeUI.nodes[0]).includes("Test CPU"));
 assert.ok(mock.nextNodeRows().includes("Selected models"));
 assert.ok(!calls.some(x=>x.includes("/v1/nodes/"+localID+"/capabilities")));
 const display=mock.nextNodeWhen(1791432448310);
 assert.ok(display!=="1791432448310" && display.includes("2026"),"timestamp must be human-readable: "+display);
 mock.nextNodeUI.selected=remoteID;
 await mock.nextNodeLoadDetail(remoteID);
 assert.equal(mock.nextNodeUI.manifest.node_id,remoteID);
 assert.equal(mock.nextNodeUI.manifest.compute.pools[0].name,"REMOTE GPU");
 assert.ok(calls.some(x=>x.includes("/v1/nodes/"+remoteID+"/capabilities")));
 console.log("PASS: local telemetry, GPU inventory, deployed models, timestamp and remote isolation");
})().catch(e=>{console.error(e);process.exitCode=1;});
