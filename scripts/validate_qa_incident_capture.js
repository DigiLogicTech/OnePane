#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const {createRecorder,classifyRequest}=require("../internal/webui/static/qa-incident-capture.js");
async function main(){
const secret="SECRET_TOKEN_ALPHA_qwerty_987654";
let time=1710000000000;
const active=new Map(),timers=new Map(),calls=[];
let nextTimer=0;
const originalFetch=async(input,init)=>{
 calls.push({input,init});
 if(String(input).includes("reject"))throw Error(secret);
 return {status:String(input).includes("forbidden")?403:200,
  json:async()=>({token:secret}),text:async()=>secret};
};
const win={
 location:{origin:"http://127.0.0.1:4567"},
 fetch:originalFetch,
 addEventListener(type,fn){if(!active.has(type))active.set(type,new Set());active.get(type).add(fn)},
 removeEventListener(type,fn){active.get(type)?.delete(fn)}
};
const dispatch=(type,data)=>{for(const fn of [...(active.get(type)||[])])fn(data)};
const route=()=>secret; // untrusted route must be reduced to "other"
const controller=createRecorder({
 win,now:()=>time,route,
 schedule:(fn,ms)=>{const id=++nextTimer;timers.set(id,{fn,at:time+ms});return id},
 cancel:id=>timers.delete(id)
});
const emitClick=()=>dispatch("click",{target:{closest:()=>({tagName:"BUTTON",textContent:secret,value:secret})}});
assert.deepEqual(classifyRequest("https://somewhere.com/v1/tasks",{},"http://127.0.0.1:4567"),null);
assert.deepEqual(classifyRequest("/v1/tasks/private-"+secret+"?access_token="+secret,
 {method:"POST",body:secret},"http://127.0.0.1:4567"),
 {subsystem:"tasks",method:"POST"});
assert.equal(classifyRequest("javascript:alert(1)",{},"http://127.0.0.1:4567"),null);
assert.equal(controller.start(),true);
assert.equal(controller.start(),false,"capture must not double-hook fetch");
assert.notEqual(win.fetch,originalFetch);
assert.equal(active.get("click").size,1);
emitClick();
dispatch("error",{message:secret,filename:secret,error:{stack:secret}});
dispatch("unhandledrejection",{reason:secret});
dispatch("submit",{target:{value:secret}});
await win.fetch("/v1/tasks/private-"+secret+"/forbidden?api_key="+secret,
 {method:"POST",body:JSON.stringify({password:secret}),headers:{Authorization:"Bearer "+secret}});
await win.fetch("/v1/agent-sessions/abc?apikey="+secret,{method:"GET"});
await win.fetch("https://external.test/v1/tasks?password="+secret,{method:"POST"});
try{await win.fetch("/v1/projects/reject-"+secret)}catch(e){assert.equal(e.message,secret)}
assert.equal(controller.mark(),true);
let report=controller.snapshot();
assert.equal(report.status,"recording");
assert.equal(report.max_events,120);
assert.equal(report.max_duration_seconds,600);
assert.ok(report.events.some(e=>e.action==="marked_issue"));
assert.ok(report.events.some(e=>e.action==="response"&&e.subsystem==="tasks"&&e.status===403));
assert.ok(report.events.some(e=>e.action==="transport_failure"&&e.subsystem==="projects"));
assert.ok(report.events.some(e=>e.action==="uncaught_error"));
assert.ok(report.events.some(e=>e.action==="unhandled_rejection"));
assert.ok(report.events.some(e=>e.kind==="ui"&&e.route==="other"));
assert.ok(!report.events.some(e=>e.subsystem==="api_other"&&e.method==="POST"));
const encoded=JSON.stringify(report);
for(const value of [secret,"Bearer "+secret,"/v1/tasks/private","Authorization:","api_key="+secret,"filename:"+secret]){
 assert.ok(!encoded.includes(value),"capture leaked sensitive marker "+value);
}
assert.equal(controller.stop(),true);
assert.equal(controller.stop(),false);
assert.equal(win.fetch,originalFetch,"original fetch must be restored");
assert.equal(active.get("click").size,0,"DOM listeners must be detached");
assert.equal(timers.size,0,"timer must be cancelled when stopped");
assert.deepEqual(controller.snapshot().events.at(-1).action,"stopped");
controller.clear();
assert.deepEqual(controller.snapshot().events,[]);
assert.equal(controller.snapshot().status,"idle");

// Ring buffer never grows unbounded and must truthfully report dropped events.
assert.equal(controller.start(),true);
for(let i=0;i<145;i++)emitClick();
assert.equal(controller.stop(),true);
report=controller.snapshot();
assert.equal(report.events.length,120);
assert.ok(report.dropped_events>=26);
assert.equal(win.fetch,originalFetch);

// Expiry is a hard deadline, not a UI-only flag. It must not recurse or
// prolong the capture when delayed callbacks execute.
controller.clear();
controller.start();
assert.equal(timers.size,1);
time+=600001;
const tick=[...timers.values()][0];
tick.fn();
report=controller.snapshot();
assert.equal(report.status,"expired");
assert.equal(win.fetch,originalFetch);
assert.equal(active.get("click").size,0);
assert.equal(timers.size,0);
assert.ok(report.events.some(e=>e.kind==="capture"&&e.outcome==="expired"));
assert.equal(controller.mark(),false);
assert.equal(controller.start(),true,"expired capture may be restarted only by explicit action");
controller.clear();
assert.equal(controller.snapshot().status,"idle");
console.log("PASS: incident capture is bounded, local, opt-in, redacted and auto-expiring");

}
main().catch(error=>{console.error(error);process.exitCode=1});
