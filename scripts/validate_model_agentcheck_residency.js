"use strict";
const fs=require("node:fs");
const assert=require("node:assert/strict");
const read=(p)=>fs.readFileSync(p,"utf8");
const qualifier=read("internal/localai/qualification.go");
const testbed=read("internal/localai/testbed.go");
const supervisor=read("internal/localai/supervisor.go");
const summary=read("internal/localai/managed_deployments.go");
const models=read("internal/webui/static/models-page.js");
const agentCheck=read("internal/webui/static/model-agentcheck-diagnostics.js");
const helper=read("internal/localai/qualification_residency.go");
assert.ok(qualifier.includes("q.supervisor.Acquire(ctx, req.DeploymentID)"),
  "automatic qualification must protect active model residency");
assert.ok(qualifier.includes("cleanupQualificationResidency("),
  "automatic qualification must release and drain even on failure");
assert.ok(testbed.includes("s.supervisor.Acquire(ctx,sess.DeploymentID)"),
  "manual Agent Check turns must also hold an activity lease");
assert.ok(testbed.includes("s.supervisor.Release(cleanupCtx,sess.DeploymentID)"),
  "manual Agent Check turns must release their activity lease");
assert.ok(helper.includes("runtime.Release(ctx,deploymentID)")&&
  helper.includes("runtime.StopIfIdle(ctx,deploymentID)"),
  "qualification must release before draining an idle model");
assert.ok(supervisor.includes("residency_state='stopped'"),
  "verified managed process stop must persist unloaded residency");
assert.ok(summary.includes('json:"residency_state"')&&
  summary.includes("COALESCE(d.residency_state,'stopped')"),
  "managed inventory must expose current deployment residency");
assert.ok(models.includes("d.residency_state||\"unknown\""),
  "model tiles must display residency separately from compute placement");
assert.ok(agentCheck.includes('residency==="stopped"'),
  "Agent Check completion must inspect real backend residency");
assert.ok(!agentCheck.includes("Spec Sheet updated and idle model unloaded."),
  "Agent Check may not unconditionally claim CPU/GPU unload");
console.log("PASS: Agent Check runtime leases, verified unload state and honest model residency UI");
