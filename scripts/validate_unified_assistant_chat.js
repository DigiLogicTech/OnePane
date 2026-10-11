"use strict";
// RC11 unified Assistant acceptance: context is visible, canonical and not
// a second privileged chat endpoint. Go integration tests cover DB ownership.
const assert=require("node:assert/strict");
const fs=require("node:fs");
const app=fs.readFileSync("internal/webui/static/app.js","utf8");
const html=fs.readFileSync("internal/webui/static/index.html","utf8");
const assistant=fs.readFileSync("internal/assistant/service.go","utf8");
const api=fs.readFileSync("internal/api/assistant_orchestrator.go","utf8");
const schedulerAPI=fs.readFileSync("internal/api/server.go","utf8");
const picker=fs.readFileSync("internal/webui/static/assistant-model-picker.js","utf8");
const render=app.split("async function a31RenderControlChat(){")[1]?.split("/* Settings */")[0]||"";
assert(render,"unified Assistant chat is missing");
assert(!html.includes("controlChatOrchestratorTab"),"duplicate Orchestrator tab resurrected");
assert(!html.includes("controlChatAssistantTab"),"duplicate Assistant tab resurrected");
for(const token of [
  'id="controlChatProject"','id="controlChatWorkspace"',
  "/v1/assistant/threads/","/scope","project_workspace_id:selectedWorkspace?.id||\"\"",
  'project_id:project?.id||""',"allow_task_creation:run","force_task:run",
  "Select an active Project Workspace to run tasks.","a31AssistantModelPicker(id)"
])assert(render.includes(token),`unified chat contract missing: ${token}`);
assert(!render.includes("allow_task_creation:run,force_task:false"),"Run still depends on a model choosing the Task action");
assert(render.includes("const canRun=!!project&&workspaces.length>0"),"Run enabled without canonical active Workspace");
assert(render.includes("Multiple isolated Workspaces exist."),"automatic placement ambiguity must be explained");
assert(render.includes("const picker=project?null:await a31AssistantModelPicker(id)"),"Project chat may needlessly probe Assistant models");
assert(assistant.includes("ProjectWorkspaceID:selectedWorkspace"),"Assistant did not forward Workspace scope");
assert(assistant.includes("project_id=? AND status='active'"),"canonical Workspace membership check missing");
assert(api.includes('json:"project_workspace_id"'),"Assistant API omitted explicit Workspace");
assert(api.includes("ProjectWorkspaceID:in.ProjectWorkspaceID"),"Assistant API did not forward Workspace");
assert(schedulerAPI.includes('"hard_zero_incremental_cost": c.HardZeroIncrementalCost'),"candidate API does not surface verified zero-cost classification");
assert(picker.includes("!!c.local&&!!c.hard_zero_incremental_cost"),"Assistant picker offers unverified provider models");
assert(picker.includes("const available=ready&&verifiedLocal"),"Assistant picker availability contradicts backend policy");
console.log("Unified Assistant context, no implicit tabs and task-scope contracts: PASS");

const agentScreen=fs.readFileSync("internal/webui/static/app.js","utf8");
for(const token of ["data-a31-profile-restrictions", "a31EditAgentRestrictions(profile)",
 "existing[key]==='disabled'", "policy[key]=checked?'disabled':'inherited'",
 "Removing an existing restriction requires human approval.",
 "expected_revision:Number(current.revision||0)"]){
 assert(agentScreen.includes(token),"versioned Agent restrictions UI missing "+token);
}
