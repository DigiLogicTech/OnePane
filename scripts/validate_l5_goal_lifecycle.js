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
 "Device probes and fixes remain disabled."]){
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
// Automatic cadence reviews require an active human-granted permit and
// must never load an LLM or invoke sensors, host/network tools or remediation.
const worker=fs.readFileSync("internal/projectworkspace/continuous_goal_review_worker.go","utf8");
const bootstrap=fs.readFileSync("internal/bootstrap/bootstrap.go","utf8");
const harness=fs.readFileSync("cmd/harnessd/main.go","utf8");
for(const token of ["TickDueGoalEvidenceReviews","GoalEvidenceReviewWorkerPrincipal",
 "p.status='approved'","p.expires_at>?","NOT EXISTS","ReviewGoalEvidence"]){
 assert(worker.includes(token),"bounded L5 review worker missing "+token);
}
assert(bootstrap.includes("EnsureGoalEvidenceReviewWorkerPrincipal(ctx)"),"review worker principal not bootstrapped");
assert(harness.includes("TickDueGoalEvidenceReviews(ctx,8)"),"passive L5 review worker not scheduled");
for(const token of ["goal_review_worker_cursors","after_permit_id","ORDER BY p.id LIMIT 64",
 "advanceGoalReviewCursor","dueGoalEvidenceReviewCandidates"]){
 assert(worker.includes(token),"durable fair review cursor missing "+token);
}
assert(ui.includes("Approve recurring receipt review"),"UI does not disclose recurring nature of review consent");
assert(ui.includes("Device monitoring not activated"),"UI falsely presents receipt review as device monitoring");
// Network targets require a completely separate human permit. A plan
// source name or a receipt-review grant never implies TCP/network authority.
const tcpService=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_probe_permits.go","utf8");
const tcpAPI=fs.readFileSync("internal/api/project_goal_tcp_probe_permits.go","utf8");
const tcpMigration=fs.readFileSync("migrations/0060_goal_tcp_probe_permits.sql","utf8");
for(const token of ["netip.ParseAddr(input)","addr.IsPrivate()","addr.IsLoopback()",
 "addr.IsLinkLocalUnicast()","addr.String()!=input",
 "TargetPort<1","TargetPort>65535","TimeoutMS<100","TimeoutMS>1500",
 "humanGoalReviewer","goalReviewPlan","status='revoked'",
 "network_tcp_connect","ExpectedPermitRevision"]){
 assert(tcpService.includes(token),"private TCP grant authorization missing "+token);
}
for(const token of ["ApproveGoalTCPProbe","RevokeGoalTCPProbe",
 "project.read","project.write","ActorPrincipalID:identity.PrincipalID"]){
 assert(tcpAPI.includes(token),"private TCP source API missing "+token);
}
for(const token of ["data-goal-probe-source","data-goal-probe-ipv4",
 "data-goal-probe-port","data-goal-probe-timeout","data-goal-probe-duration",
 "data-goal-probe-approve=","data-goal-probe-list=","data-goal-probe-revoke=",
 "Confirm network target ","No network probe has executed or been scheduled."]){
 assert(ui.includes(token),"explicit TCP source consent UI missing "+token);
}
for(const token of ["/tcp-probe-permits","/tcp-probe-permits/{permitID}/revoke"]){
 assert(server.includes(token),"TCP probe grant route missing "+token);
}
const permitImmutability=fs.readFileSync("migrations/0064_goal_tcp_probe_permit_immutability.sql","utf8");
for(const token of ["target_ipv4!=OLD.target_ipv4","target_port!=OLD.target_port",
 "expires_at!=OLD.expires_at","plan_hash!=OLD.plan_hash",
 "NEW.status!='revoked'"]){
 assert(permitImmutability.includes(token),"post-approval network grant mutation gate missing "+token);
}
assert(tcpMigration.includes("CREATE UNIQUE INDEX idx_goal_tcp_probe_active_source"),
 "same source could be rebound without revocation");
assert(!harness.includes("TickDueGoalTCPProbe"),"network execution must not start by migration or consent");
const tcpRun=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_probe_attempts.go","utf8");
const tcpRunAPI=fs.readFileSync("internal/api/project_goal_tcp_probe_attempts.go","utf8");
const tcpRunMigration=fs.readFileSync("migrations/0061_goal_tcp_probe_attempts.sql","utf8");
for(const token of ["runManualGoalTCPProbe","claimGoalTCPAttempt",
 "tcpAttemptStillAuthorised","net.JoinHostPort",
 "context.WithTimeout","DialContext(connectCtx,\"tcp4\"",
 "not_connected","timed_out","completeTCPAttempt"]){
 assert(tcpRun.includes(token),"bounded TCP executor missing "+token);
}
for(const token of ["data-goal-probe-test=","data-goal-probe-history=",
 "Confirm one TCP connect","TCP handshakes only — not verified device health.",
 "This does NOT establish service security, health or trust."]){
 assert(ui.includes(token),"manual and transparent TCP test UI missing "+token);
}
for(const token of ["RunManualGoalTCPProbe","ListGoalTCPProbeAttempts","project.read","project.write"]){
 assert(tcpRunAPI.includes(token),"TCP source test API missing "+token);
}
for(const token of ["/tcp-probe-permits/{permitID}/test","/tcp-probe-attempts"]){
 assert(server.includes(token),"manual TCP test route missing "+token);
}
const tcpRateMigration=fs.readFileSync("migrations/0062_goal_tcp_probe_source_rate_fence.sql","utf8");
assert(tcpRateMigration.includes("(goal_id,source_id,cadence_window)"),
 "TCP source grant rotation must not reset the per-source cadence rate limit");
assert(tcpRun.includes("WHERE goal_id=? AND source_id=? AND cadence_window=?"),
 "manual TCP admission must enforce source-level rate budget");
assert(ui.includes("approveTCP.dataset.confirmedTarget!==requestedTarget"),
 "TCP consent confirmation must bind the exact source and target");
assert(tcpRunMigration.includes("UNIQUE(permit_id,cadence_window)"),
 "TCP test retries must have an immutable admission fence");
assert(tcpRun.includes("manualTCPProbeSlots=make(chan struct{},2)"),
 "constrained-hardware TCP handshake concurrency must be bounded");
assert(tcpRun.includes("case <-ctx.Done()"),
 "queued network handshakes must respect cancelled requests");
assert(!harness.includes("RunManualGoalTCPProbe"),"manual network test unexpectedly wired to scheduler");

// Expected TCP reachability is a revisioned human-authored BASELINE,
// never evidence of security health or a network action by itself.
const baselineService=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_baselines.go","utf8");
const baselineAPI=fs.readFileSync("internal/api/project_goal_tcp_baselines.go","utf8");
const baselineMigration=fs.readFileSync("migrations/0063_goal_tcp_reachability_baselines.sql","utf8");
for(const token of ["SetGoalTCPBaseline","GoalTCPReachabilityAssessment",
 "GoalTCPBaselineRevision","humanGoalReviewer","monitoring_plan_json",
 "expected_tcp","missing_evidence","stale_test","permission_inactive",
 "unexpected_tcp_reachability","matches_last_tcp_test","DeviceHealth:\"unverified\""]){
 assert(baselineService.includes(token),"L5 baseline safety contract missing "+token);
}
assert(baselineMigration.includes("goal_tcp_baselines_immutable_update"),"prior baseline must be immutable");
for(const token of ["data-goal-tcp-baseline-save=","data-goal-tcp-assess=",
 "TCP reachability expectation saved.","Device health remains UNVERIFIED."]){
 assert(ui.includes(token),"baseline UX missing "+token);
}
for(const token of ["SetGoalTCPBaseline","ListGoalTCPBaselines",
 "GoalTCPReachabilityAssessment","project.read","project.write"]){
 assert(baselineAPI.includes(token),"baseline API contract missing "+token);
}
for(const token of ["/tcp-baselines","/tcp-assessment/{sourceID}"]){
 assert(server.includes(token),"baseline route missing "+token);
}
// A TCP connect sample must never be counted as a trusted Observation,
// but it must still appear as a separate read-only coverage signal.
const coverage=fs.readFileSync("internal/projectworkspace/continuous_goal_coverage.go","utf8");
for(const token of ["LastTCPTestAt","TCPReachability",
 "project_goal_tcp_probe_attempts","unknown_after_interruption",
 "EvidenceState=\"missing\"","MonitoringActive:false"]){
 assert(coverage.includes(token),"TCP/evidence class separation missing "+token);
}
for(const token of ["x.tcp_reachability","x.last_tcp_test_at",
 "(not verified health)"]){
 assert(ui.includes(token),"read-only TCP coverage UI missing "+token);
}
// Opt-in unattended TCP checks require a separate, expiring human grant.
// The ordinary manual source permit is NEVER background-scheduler authority.
const recurring=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_recurring_grants.go","utf8");
const recurringAdmission=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_recurring_admission.go","utf8");
const recurringWorker=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_recurring_worker.go","utf8");
const recurringAPI=fs.readFileSync("internal/api/project_goal_tcp_recurring_grants.go","utf8");
const recurringDDL=fs.readFileSync("migrations/0065_goal_tcp_recurring_grants.sql","utf8");
for(const token of ["ApproveTCPRecurringGrant","RevokeTCPRecurringGrant",
 "ExpectedManualPermitRevision","humanGoalReviewer",
 "ManualPermitID","ExpiresAt","recurring_tcp_connect"]){
 assert(recurring.includes(token),"independent recurring TCP authority missing "+token);
}
for(const token of ["authorizeRecurringTCPClaimTx",
 "GoalTCPRecurringWorkerPrincipal","grant.ManualPermitRevision",
 "grant.TargetIPv4","grant.CadenceMinutes"]){
 assert(recurringAdmission.includes(token),"TCP claim admission missing "+token);
}
for(const token of ["TickDueTCPRecurringProbes","claimGoalTCPAttempt",
 "tcpRecurringClaimStillAuthorised","manualTCPProbeSlots",
 "net.JoinHostPort","LIMIT 32"]){
 assert(recurringWorker.includes(token),"bounded recurring TCP worker missing "+token);
}
for(const token of ["project.write","project.read",
 "ApproveTCPRecurringGrant","RevokeTCPRecurringGrant"]){
 assert(recurringAPI.includes(token),"human recurring TCP consent API missing "+token);
}
assert(recurringDDL.includes("idx_goal_tcp_recurring_active_source"),
 "a source may not own overlapping recurring TCP permissions");
assert(harness.includes("TickDueTCPRecurringProbes(ctx,1)"),
 "recurring TCP worker must be rate bounded to one source per minute");
assert(bootstrap.includes("EnsureTCPRecurringWorkerPrincipal(ctx)"),
 "independent worker identity must be created before scheduler activity");
assert(!harness.includes("RunManualGoalTCPProbe"),
 "a manual TCP permission is not background network authority");
// Immutable baseline provenance: each TCP attempt must carry the exact
// baseline revision at socket admission. Later operator changes cannot
// retroactively transform an old handshake into a verified drift finding.
const attemptSnapshotDDL=fs.readFileSync("migrations/0068_goal_tcp_attempt_baseline_snapshots.sql","utf8");
for(const token of ["baseline_id_at_claim","baseline_revision_at_claim"]){
 assert(attemptSnapshotDDL.includes(token),"TCP baseline snapshot migration missing "+token);
 assert(tcpRun.includes(token),"TCP claim must persist baseline snapshot "+token);
}
assert(attemptSnapshotDDL.includes("goal_tcp_attempts_baseline_snapshot_immutable"),
 "baseline snapshot columns must be immutable");
for(const token of ["SampleBaselineRevision","no_baseline_at_test",
 "baseline_updated_since_test","atClaimBaselineID!=baselineID"]){
 assert(baselineService.includes(token),"baseline assessment must block retrospective evidence "+token);
}
// Baseline drift only creates an immutable, human-acknowledgeable NOTICE,
// never a verified compromise claim or authority to run a remediation.
const driftAlerts=fs.readFileSync("internal/projectworkspace/continuous_goal_tcp_reachability_alerts.go","utf8");
const driftAPI=fs.readFileSync("internal/api/project_goal_tcp_reachability_alerts.go","utf8");
const driftDDL=fs.readFileSync("migrations/0069_goal_tcp_reachability_alerts.sql","utf8");
for(const token of ["recordTCPReachabilityAlertTx",
 "BaselineIDAtClaim","BaselineRevisionAtClaim",
 "unexpected_reachable","unexpected_unreachable",
 "DeviceHealth=\"unverified\"","AcknowledgeTCPReachabilityAlert",
 "humanGoalReviewer"]){
 assert(driftAlerts.includes(token),"baseline alert admission missing "+token);
}
for(const token of ["project.read","project.write",
 "ListTCPReachabilityAlerts","AcknowledgeTCPReachabilityAlert"]){
 assert(driftAPI.includes(token),"human-alert API missing "+token);
}
assert(tcpRun.includes("recordTCPReachabilityAlertTx(ctx,tx,a,status,now)"),
 "drift should be detected only in the immutable TCP completion transaction");
assert(driftDDL.includes("goal_tcp_reachability_alerts_no_rewrite"),
 "human notice provenance must be immutable");
assert(!harness.includes("AcknowledgeTCPReachabilityAlert"),
 "background operations must not silently acknowledge security warnings");
console.log("L5 pause/retire UI and fail-closed goal lifecycle: PASS");
