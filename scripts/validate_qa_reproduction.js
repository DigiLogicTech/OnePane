#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const fs=require("node:fs"),path=require("node:path");
const note=require("../internal/webui/static/qa-reproduction-note.js");
const canary="SECRET_PASSWORD_TOKEN_API_KEY_IN_OPERATOR_NOTES";
const clean={
 category:"task",impact:"high",title:" Task fails after model swap ",
 steps:"1. Switch runtime\r\n2. Run Task\n3. Observe failure",
 expected:"Task resumes using selected model",
 actual:"Task remains blocked",vault_material:canary,private_path:canary
};
const r=note.prepare(clean,1000);
assert.ok(r.text.includes("Subsystem: task"));
assert.ok(r.text.includes("Impact: high"));
assert.ok(r.text.includes("Steps to reproduce:"));
assert.ok(r.text.includes("Browser/backend trace correlation: not asserted"));
assert.ok(!r.text.includes(canary),"unselected source fields copied into report");
assert.ok(note.stillValid(r,clean,r.text,1001,true));
assert.equal(note.stillValid(r,clean,r.text,1001,false),false);
assert.equal(note.stillValid(r,clean,r.text,121001,true),false);
assert.equal(note.stillValid(r,clean,r.text,999,true),false);
assert.equal(note.stillValid(r,{...clean,actual:"different"},r.text,1001,true),false);
assert.equal(note.stillValid(r,clean,r.text+"modified",1001,true),false);
assert.equal(note.stillValid(r,clean,r.text,1001,undefined),false);
assert.throws(()=>note.prepare({...clean,title:" "},1000),/required/);
assert.throws(()=>note.prepare({...clean,title:"x".repeat(121)},1000),/limit/);
assert.throws(()=>note.prepare({...clean,steps:"x".repeat(1601)},1000),/limit/);
assert.throws(()=>note.prepare({...clean,actual:""},1000),/required/);
assert.throws(()=>note.prepare(clean,-1),/time/);
const unknown=note.prepare({...clean,category:canary,impact:canary},1000);
assert.ok(unknown.text.includes("Subsystem: other"));
assert.ok(unknown.text.includes("Impact: medium"));
assert.ok(!unknown.text.includes(canary));
const authored=note.prepare({...clean,steps:"I deliberately typed "+canary},1000);
assert.ok(authored.text.includes(canary),"Never pretend operator notes were redacted");
assert.ok(authored.text.includes("NOT automatically redacted"));
assert.ok(note.stillValid(authored,{...clean,steps:"I deliberately typed "+canary},authored.text,1001,true));
const root=path.resolve(__dirname,".."),read=p=>fs.readFileSync(path.join(root,p),"utf8");
const ui=read("internal/webui/static/debug-centre.js");
const html=read("internal/webui/static/index.html");
const css=read("internal/webui/static/debug-centre.css");
for(const needle of [
 'id="a60Category"','id="a60Impact"','id="a60Title"','id="a60Steps"',
 'id="a60Expected"','id="a60Actual"','id="a60Consent"',
 'id="a60Review"','id="a60Copy"','id="a60Download"','id="a60ReportPreview"',
 'notePreview.textContent=noteReviewed.text','note.stillValid(',
 'noteConsent.checked','URL.createObjectURL('
])assert.ok(ui.includes(needle),"missing UI gate "+needle);
assert.ok(ui.includes('I reviewed the exact text'));
assert.ok(ui.includes('not automatically redacted data'));
assert.ok(html.includes('/qa-reproduction-note.js'));
assert.ok(html.indexOf('/qa-reproduction-note.js')<html.indexOf('/debug-centre.js'));
assert.ok(css.includes('.a60-reproduction-fields'));
const helper=read("internal/webui/static/qa-reproduction-note.js");
assert.ok(!helper.includes("fetch("));
assert.ok(!helper.includes("localStorage")&&!helper.includes("sessionStorage"));
assert.ok(!helper.includes("apiRequest("));
assert.ok(!helper.includes("innerHTML"));
console.log("PASS: local operator authored QA notes, reviewed exact text, consent, limits, mutation and expiry denial");
