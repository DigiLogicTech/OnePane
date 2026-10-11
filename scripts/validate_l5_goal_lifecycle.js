"use strict";
const assert=require("node:assert/strict"),fs=require("node:fs");
const ui=fs.readFileSync("internal/webui/static/app-foundation.js","utf8");
const service=fs.readFileSync("internal/projectworkspace/continuous_goal.go","utf8");
const api=fs.readFileSync("internal/api/project_continuous_goals.go","utf8");
const server=fs.readFileSync("internal/api/server.go","utf8");
const fragment=ui.split("async function qa4BindL5GoalConfigurator(project){")[1]||"";
for(const token of ["data-goal-transition=\"paused\"","data-goal-transition=\"retired\"",
 "Confirm retirement","expected_revision:Number(button.dataset.goalRevision)",
 "await refresh()"])assert(fragment.includes(token),"L5 goal UI contract missing "+token);
assert(service.includes('cmd.TargetStatus!="paused"&&cmd.TargetStatus!="retired"'),"unverified goal activation permitted");
assert(service.includes("ExpectedRevision"),"revision precondition missing");
assert(service.includes("next_due_at=0"),"scheduled due time not cleared on pause/retire");
assert(service.includes('principalType!="human"'),"retirement not gated on human");
assert(api.includes("TransitionContinuousGoal"),"goal transition handler missing");
assert(server.includes("/continuous-goals/{goalID}/{transition}"),"transition route missing");
for(const token of ["monitoring_plan:{version:1,source_ids:sourceIDs}","data-goal-attach=","data-goal-list-evidence=",
 "goal health remains unobserved", "Existing evidence linked","data-goal-evidence-results"]){
 assert(ui.includes(token), "L5 evidence UI contract missing "+token);
}
assert(server.includes("/continuous-goals/{goalID}/observations"),"goal evidence HTTP route missing");
console.log("L5 pause/retire UI and fail-closed goal lifecycle: PASS");
