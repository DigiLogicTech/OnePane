#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const fs=require("node:fs");
const path=require("node:path");
const root=path.resolve(__dirname,"..");
const reviewer=require("../internal/webui/static/policy-diagnostic-review.js");
const record={
 At:"2026-10-11T00:00:00Z",Component:"featurepolicy",
 CorrelationID:"run:42",Action:"inference.cloud",
 Allowed:false,Category:"policy.denied",
 Password:"PRIVATE_PASSWORD_EXAMPLE",prompt:"PRIVATE_PROMPT_EXAMPLE"
};
const sample=JSON.stringify([record,{...record,CorrelationID:"attempt:1",Allowed:true,Category:"policy.allowed",Action:"files.read"}]);
const projected=reviewer.project(sample);
assert.equal(projected.denied,1);
assert.equal(projected.allowed,1);
assert.equal(projected.total,2);
assert.equal(projected.events[0].correlation_id,"run:42");
const safe=JSON.stringify(projected);
assert.ok(!safe.includes("PRIVATE_PASSWORD_EXAMPLE"));
assert.ok(!safe.includes("PRIVATE_PROMPT_EXAMPLE"));
assert.ok(!safe.includes("Password"));
assert.ok(!safe.includes("prompt"));
assert.ok(safe.includes('"action":"inference.cloud"'));
assert.equal(reviewer.project(JSON.stringify([{
 ...record,CorrelationID:"run:Bearer:credential",At:"sensitive date"
}])).events[0].correlation_id,"redacted");
assert.equal(reviewer.project(JSON.stringify([{
 ...record,CorrelationID:"run:Bearer:credential",At:"sensitive date"
}])).events[0].at_utc,"unavailable");
assert.throws(()=>reviewer.project(""),/at most/);
assert.throws(()=>reviewer.project("{"),/Invalid JSON/);
assert.throws(()=>reviewer.project(JSON.stringify(Array(65).fill(record))),/1–64/);
assert.throws(()=>reviewer.project(JSON.stringify([{...record,Action:"shell.exec"}])),/No valid/);
assert.throws(()=>reviewer.project(JSON.stringify([{...record,Category:"policy.allowed"}])),/No valid/);
assert.throws(()=>reviewer.project(JSON.stringify([{...record,Allowed:"true"}])),/No valid/);
assert.throws(()=>reviewer.project(JSON.stringify([{...record,Component:"other"}])),/No valid/);
assert.throws(()=>reviewer.project(JSON.stringify([{...record,CorrelationID:"trace:abc",prompt:"x".repeat(32768)}])),/32 KiB/);
const js=fs.readFileSync(path.join(root,"internal/webui/static/debug-centre.js"),"utf8");
const html=fs.readFileSync(path.join(root,"internal/webui/static/index.html"),"utf8");
const css=fs.readFileSync(path.join(root,"internal/webui/static/debug-centre.css"),"utf8");
assert.ok(js.includes("root.a62PolicyEvidence.markup()"));
assert.ok(js.includes("root.a62PolicyEvidence.bind(host)"));
assert.ok(html.indexOf("/policy-diagnostic-review.js")<html.indexOf("/debug-centre.js"));
assert.ok(css.includes(".a62-policy-panel"));
for(const term of ["fetch(", "XMLHttpRequest", "localStorage", "sessionStorage", "innerHTML="])
 assert.ok(!fs.readFileSync(path.join(root,"internal/webui/static/policy-diagnostic-review.js"),"utf8").includes(term),"No automatic transmission, persistence or unsafe rendering: "+term);
assert.ok(reviewer.markup().includes("a62PolicyInput"));
assert.ok(reviewer.markup().includes("a62PolicyDownload"));
console.log("PASS: offline policy review sanitization, schema, boundaries and Debug Centre integration");
