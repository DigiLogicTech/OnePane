#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict");
const fs=require("node:fs"),path=require("node:path");
const b=require("../internal/webui/static/qa-api-capture.js");
const canary="VERY_PRIVATE_API_KEY_PASSWORD_SECRETS_QUERY_BODY";
const entry={at_utc:"2026-10-10T03:00:00Z",subsystem:"tasks",
 method:"POST",status_code:500,duration_ms:4,
 uri:"/v1/tasks/"+canary,path:canary,error_text:canary,
 headers:{"Authorization":canary},body:canary,trace_id:canary};
const view={
 schema_version:1,source:"operator_tagged_local_http",status:"recording",level:"verbose",
 capture_id:"1".repeat(48),started_utc:"2026-10-10T03:00:00Z",
 ends_utc:"2026-10-10T03:01:00Z",principal_id:canary,raw_log:canary,
 observed_requests:2,dropped_observations:0,instrumentation_update_us:123,
 counters:[{subsystem:"tasks",outcome:"5xx",count:2,secret:canary}],
 observations:[entry,{...entry,subsystem:"hidden-"+canary,method:"MALICIOUS"}]
};
const projected=b.project(view);
assert.equal(projected.observations.length,1);
assert.equal(projected.observations[0].subsystem,"tasks");
assert.equal(projected.counters.length,1);
assert.equal(projected.instrumentation_update_us,123);
const output=b.format(view);
assert.ok(output.includes("Collector update time only"));
assert.ok(output.includes("status=500"));
assert.ok(!JSON.stringify(projected).includes(canary));
assert.ok(!output.includes(canary));
assert.ok(!output.includes("capture_id"));
assert.throws(()=>b.project({...view,scope:"bad",source:"something-else"}));
assert.throws(()=>b.project({...view,observations:Array(65).fill(entry)}));
assert.throws(()=>b.project({...view,counters:Array(41).fill(view.counters[0])}));
assert.equal(b.project({...view,level:"SECRET-"+canary}).level,"unavailable");
assert.ok(b.markup().includes('id="a61Start"'));
assert.ok(b.markup().includes('id="a61Refresh"'));
assert.ok(b.markup().includes('id="a61Stop"'));
assert.ok(b.markup().includes('id="a61Preview"'));
assert.ok(b.markup().includes("Node administrators only"));

const base=path.resolve(__dirname,"..");
const read=p=>fs.readFileSync(path.join(base,p),"utf8");
const helper=read("internal/webui/static/qa-api-capture.js");
const frontend=read("internal/webui/static/app-foundation.js");
const ui=read("internal/webui/static/debug-centre.js");
const html=read("internal/webui/static/index.html");
const server=read("internal/api/server.go");
const backend=read("internal/api/qa_api_capture.go");
for(const snippet of [
 'captureID=typeof a61APICapture!=="undefined"?a61APICapture.id():""',
 '!path.startsWith("/v1/qa/")','headers["X-OnePane-QA-Capture"]=captureID',
 'path.startsWith("/v1/")','headers.Accept="application/json"'
])assert.ok(frontend.includes(snippet),"API tagging safety missing "+snippet);
for(const snippet of ['a61APICapture.markup()','a61APICapture.bind(host,root.apiRequest)']){
 assert.ok(ui.includes(snippet),"central UI missing "+snippet);
}
assert.ok(html.indexOf('/qa-api-capture.js')<html.indexOf('/debug-centre.js'));
assert.ok(html.indexOf('/app-foundation.js')<html.indexOf('/qa-api-capture.js'));
assert.ok(server.includes('s.securityHeaders(s.qaCaptureMiddleware(s.mux))'));
for(const route of ["POST /v1/qa/api-capture/start","POST /v1/qa/api-capture/stop","GET /v1/qa/api-capture"]){
 assert.ok(server.includes(route),"missing route "+route);
}
for(const important of [
 's.auth.Authenticate(r)', 's.federation.CanOperate(r.Context(),i.PrincipalID)',
 's.apiCapture.accepts(', 'strings.HasPrefix(r.URL.Path,"/v1/qa/")',
 'strings.Contains(strings.ToLower(r.Header.Get("Accept")),"application/json")',
 'qaAPIRetention=2*time.Minute','qaAPIMaxEvents=64'
])assert.ok(backend.includes(important),"missing security check: "+important);
assert.ok(!helper.includes("localStorage")&&!helper.includes("sessionStorage"));
assert.ok(!helper.includes('window.setInterval'));
assert.ok(!helper.includes('innerHTML='));
assert.ok(!helper.includes('fetch('));
assert.ok(!backend.includes("r.URL.RawQuery"));
assert.ok(!backend.includes("r.Header.Get(\"Authorization\")"));
assert.ok(!backend.includes("r.RemoteAddr"));
console.log("PASS: bounded operator-tagged backend capture client, projection allowlist, admin gates, time-limit, no automatic network usage");
