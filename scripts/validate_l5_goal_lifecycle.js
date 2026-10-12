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
 "Goal health remains unobserved", "Existing evidence linked","data-goal-evidence-results"]){
 assert(ui.includes(token), "L5 evidence UI contract missing "+token);
}
assert(server.includes("/continuous-goals/{goalID}/observations"),"goal evidence HTTP route missing");
assert(server.includes("/continuous-goals/{goalID}/coverage"),"goal evidence coverage route missing");
for(const token of ["data-goal-coverage=","Evidence coverage only — not verified health.","No monitoring is activated by this check."]){
 assert(ui.includes(token),"L5 read-only coverage UI missing: "+token);
}
// Receipt-only review grants are independent, expiring and human-approved.
// A mere source identifier must never be treated as active monitoring.
const grants=fs.readFileSync("internal/projectworkspace/continuous_goal_review_permits.go","utf8");
const grantAPI=fs.readFileSync("internal/api/project_goal_evidence_review_permits.go","utf8");
for(const token of ["data-goal-review-source","data-goal-review-duration",
 "data-goal-review-approve=","data-goal-review-list=","data-goal-review-revoke=",
 "expected_goal_revision:Number(grant.dataset.goalRevision)",
 "expected_permit_revision:Number(revoke.dataset.permitRevision)",
 "Monitoring probes and fixes remain disabled."]){
 assert(ui.includes(token),"L5 scoped review UI missing "+token);
}
for(const token of ["humanGoalReviewer","expires_at>?","GoalEvidenceReviewAllowed",
 "project_workspace_id=?","plan_hash=?","existing_receipt_metadata",
 "status='revoked'","ExpectedPermitRevision"]){
 assert(grants.includes(token),"L5 grant enforcement missing "+token);
}
for(const token of ["project.write","project.read","ActorPrincipalID:identity.PrincipalID",
 "ApproveGoalEvidenceReview","RevokeGoalEvidenceReview"]){
 assert(grantAPI.includes(token),"L5 grant API authority missing "+token);
}
for(const token of ["/evidence-review-permits", "/evidence-review-permits/{permitID}/revoke"]){
 assert(server.includes(token),"L5 review permit route missing "+token);
}
const reviewService=fs.readFileSync("internal/projectworkspace/continuous_goal_evidence_reviews.go","utf8");
const reviewAPI=fs.readFileSync("internal/api/project_goal_evidence_reviews.go","utf8");
for(const token of ["data-goal-review-run=","data-goal-review-history=",
 "expected_goal_revision:Number(runReview.dataset.goalRevision)",
 "expected_permit_revision:Number(runReview.dataset.permitRevision)",
 "Historical receipt assessments — not verified device health.",
 "No probes, device health verification, or fixes were performed."]){
 assert(ui.includes(token),"Level 5 evidence review UI missing "+token);
}
for(const token of ["ReviewGoalEvidence","GoalEvidenceReviewRun",
 "status!=\"configured\"&&status!=\"active\"","cadence_window",
 "existing_receipt_metadata","expires_at"]){
 // expiry is held in the permit object; this checks the review service
 // uses that field before emitting an immutable receipt assessment.
 const actual=token==="expires_at"?"permit.ExpiresAt<=now":token;
 assert(reviewService.includes(actual),"Level 5 review admission missing "+actual);
}
for(const token of ["ReviewGoalEvidence","ListGoalEvidenceReviewRuns",
 "project.read","project.write"]){
 assert(reviewAPI.includes(token),"Level 5 evidence review API missing "+token);
}
for(const token of ["/evidence-reviews","/evidence-review-permits/{permitID}/review"]){
 assert(server.includes(token),"Level 5 evidence review route missing "+token);
}
console.log("L5 pause/retire UI and fail-closed goal lifecycle: PASS");
