#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const fs=require("node:fs");
const path=require("node:path");
const support=require("../internal/webui/static/qa-consolidated-support.js");
const centre=require("../internal/webui/static/debug-centre.js");
const root=path.resolve(__dirname,"..");
const read=p=>fs.readFileSync(path.join(root,p),"utf8");
const canary="PRIVATE_USER_PROMPT_PATH_TOKEN_PASSWORD_1234";
const event={
 kind:"api",action:"response",at_utc:"2026-10-10T01:00:00Z",
 subsystem:"models",method:"GET",status:503,duration_ms:22,
 payload:canary,token:canary,exception:canary,raw_url:"https://"+canary
};
const incident={
 schema_version:1,source:"onepane_browser_opt_in",
 status:"stopped",started_utc:"2026-10-10T00:59:00Z",
 ended_utc:"2026-10-10T01:00:00Z",events:[event],
 dropped_events:0,extra_secret:canary
};
const reviewed=centre.makeReview(incident,support,1000);
assert.ok(reviewed.json.includes('"subsystem": "models"'));
assert.ok(reviewed.json.includes('"status": 503'));
assert.ok(!reviewed.json.includes(canary));
assert.ok(centre.validReview(reviewed,incident,reviewed.json,1200));
assert.ok(centre.validReview(reviewed,incident,reviewed.json,121000));
assert.equal(centre.validReview(reviewed,incident,reviewed.json,121001),false);
assert.equal(centre.validReview(reviewed,incident,reviewed.json,999),false);
assert.equal(centre.validReview(reviewed,{...incident,events:[{...event,status:200}]},reviewed.json,1200),false);
assert.equal(centre.validReview(reviewed,{...incident,status:"recording"},reviewed.json,1200),false);
assert.equal(centre.validReview(reviewed,incident,reviewed.json+" ",1200),false);
assert.throws(()=>centre.makeReview({...incident,status:"recording"},support,1000),/Stop/);
assert.throws(()=>centre.makeReview({...incident,events:[]},support,1000),/Stop/);
assert.throws(()=>centre.makeReview(incident,null,1000),/Sanitiser/);
assert.throws(()=>centre.makeReview({...incident,events:Array(121).fill(event)},support,1000),/limit/);
assert.ok(centre.centreMarkup().includes('id="a58StartCapture"'));
assert.ok(centre.centreMarkup().includes('id="a58ReviewTrace"'));
assert.ok(centre.centreMarkup().includes('id="a58DownloadTrace"'));
assert.ok(centre.centreMarkup().includes('data-a58-destination="workspaces"'));

// Inspect the actual shipped source for orchestration and scope guardrails.
const js=read("internal/webui/static/debug-centre.js");
const settings=read("internal/webui/static/settings-redesign.js");
const app=read("internal/webui/static/app.js");
const html=read("internal/webui/static/index.html");
const workflow=read("internal/webui/static/workspace-ai-workflow.js");
const requisite=[
 [settings,'diagnostics:{label:"Debug & Diagnostics"'],
 [settings,'"skills","diagnostics","updates"'],
 [app,'if(a31SettingsView==="diagnostics")'],
 [app,'a58DebugCentre.mount()'],
 [html,'id="a58OpenDebugCentre"'],
 [html,'/debug-centre.css'],
 [html,'/debug-centre.js'],
 [js,'root.a54QACapture'],
 [js,'root.a56SupportBundle'],
 [js,'root.openRoute(dest)'],
 [js,'validReview(reviewed,snapshot,preview.textContent,Date.now())'],
 [workflow,'id="a56SupportBundle"'],
 [workflow,'await reauthorizeSupportSources(supportOptions());']
];
for(const [src,token] of requisite)assert.ok(src.includes(token),"Missing Debug Centre contract: "+token);
assert.ok(html.indexOf('/qa-consolidated-support.js')<html.indexOf('/debug-centre.js'));
assert.ok(html.indexOf('/qa-incident-capture.js')<html.indexOf('/debug-centre.js'));
assert.ok(html.indexOf('/debug-centre.js')<html.indexOf('/workspace-ai-workflow.js'));
assert.ok(!js.includes("apiRequest("),"Debug Centre does not bypass authorised source panels");
assert.ok(!js.includes("localStorage")&&!js.includes("sessionStorage"),
 "Debug Centre cannot persist incident traces");
assert.ok(!js.includes("XMLHttpRequest")&&!js.includes("fetch("),
 "Debug Centre cannot transmit or auto-retrieve evidence");
assert.ok(!js.includes("innerHTML="),"Debug Centre cannot interpolate captured data as HTML");
console.log("PASS: Settings Debug Centre, permission-scoped links, reused recorder, sanitised review, local download integrity and expiry");
