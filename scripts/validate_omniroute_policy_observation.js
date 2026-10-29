#!/usr/bin/env node
"use strict";
const assert=require("node:assert/strict"),fs=require("node:fs"),path=require("node:path"),vm=require("node:vm");
const source=fs.readFileSync(path.join(__dirname,"../internal/webui/static/app-foundation.js"),"utf8");
const start=source.indexOf("function omniRoutePolicyStatusQA("),end=source.indexOf("async function omniQA(",start);
assert.ok(start>=0&&end>start,"Missing truthful OmniRoute policy renderer");
const describe=vm.runInNewContext(source.slice(start,end)+"\nomniRoutePolicyStatusQA;");
for(const value of [null,{}, {strict_zero_cost_verified:true},{strict_policy_observed:true},
 {strict_policy_observed:"true",strict_policy_source:"settings.freeAccessPolicy"},
 {strict_policy_observed:true,strict_policy_source:"routing.freeAccessPolicy"},
 {strict_policy_observed:false,strict_policy_source:"settings.freeAccessPolicy",strict_zero_cost_verified:true},
 {strict_policy_observed:true,strict_policy_source:'<img src=x onerror=alert(1)>'}]) {
 assert.equal(describe(value)," · Strict routing policy unobserved; request cost unverified");
}
assert.equal(describe({strict_policy_observed:true,strict_policy_source:"settings.freeAccessPolicy",strict_zero_cost_verified:true}),
 " · Strict routing policy observed; request cost unverified");
assert.ok(!source.includes("Require verified strict zero-cost"));
assert.ok(!source.includes("strict $0 verified"));
assert.ok(!source.includes("payload.require_strict_zero_cost"),"Do not weaken the legacy economic guarantee request");
assert.equal((source.match(/payload\.require_strict_policy=/g)||[]).length,2,"Both handlers must use additive observed-policy consent");
assert.equal((source.match(/escapeHtml\(omniRoutePolicyStatusQA\(p\)\)/g)||[]).length,2,"Both probe/connect handlers must use the truthful renderer");
assert.equal((source.match(/escapeHtml\(omniRoutePolicyStatusQA\(omni.connection\)\)/g)||[]).length,2,"Both saved gateway views must treat legacy records as unobserved");
console.log("PASS: OmniRoute exact observed-policy metadata, legacy ambiguity, no cost guarantee and both active/saved gateway views");
