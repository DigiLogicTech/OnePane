# RC11 operating modes — L4 default and L5 continuous operations

**Authority:** user alignment on 2026-10-11.
**Scope:** required product design for a local-first harness, not a claim that
L5 features are already implemented. Carry to the second code audit and
production-readiness plan. Source PR: #227.

## Modes and lifecycle

| Field | Level 4: Autonomous Project (default) | Level 5: Continuous Operations |
| --- | --- | --- |
| User input | Desired deliverable and acceptance tests | Long-lived objective, assets, health/risk thresholds |
| Execution | Plan → orchestrate → test → adapt → complete | Deploy → baseline → observe → detect → triage → respond → verify → repeat |
| Final state | Completed after verified criteria | Remains active, healthy/degraded/attention/paused; may be retired |
| Triggers | New or dependent tasks, retries, human decisions | Scheduled checks, events, drift thresholds, incidents and maintenance |
| Resource management | Model queue, unload/hot-swap, Colibri, safe checkpoint/retry | Same, plus low-priority recurring work that yields to interactive tasks |
| Authority | Project/Workspace tool grants and budgets | Same; recurring permission checks, bounded response playbooks |
| Autonomy | Create appropriate workers, tasks and sandboxes under grants | Same, plus ongoing incident workflows and approved self-healing |
| UI | Automatic execution toggle and clear progress | Continuous Operations mode, monitoring sources, response policy, health feed |

Mode selection is **not** a privilege escalation: switching L4→L5
changes scheduling and lifecycle semantics only. Any additional
network/device/host/cloud privileges require a separate, revocable grant.

## Home-network-protection example (reference acceptance scenario)

Goal: "Protect my home network" within a user-defined set of devices,
networks, services and credentials that OnePane is explicitly authorized
to observe or manage. Do not probe third-party networks.

1. **Discover and baseline:** obtain consented local device/service inventory,
   DNS filtering status, known exposed services, patch status, certificate
   expiries, backup health, uptime and configured network controls. Snapshot
   a signed/versioned baseline with source, timestamp and confidence.
2. **Deploy authorised monitoring:** use available read-only adapters for
   router/firewall APIs, local DNS, reverse proxy, uptime/host telemetry and
   security logs where access is explicitly granted. Treat unsupported
   integrations as "not monitored," not "secure".
3. **Correlate and evaluate:** compare observations against baseline,
   suppress repeated alerts, consider source reliability and identify
   unknown device changes, anomalous exposure, failed protection checks
   or meaningful configuration drift. Escalate uncertain findings.
4. **Respond within grants:** for approved low-risk, reversible changes,
   snapshot pre-state, apply via a least-privileged adapter, re-evaluate
   health, and roll back on failed verification when safe. Examples may
   include restarting an authorised monitoring probe or updating a
   previously approved safe-list policy within bounds.
5. **High-impact approval by default, optional preauthorisation:** router/
   firewall rule modifications, device quarantine, WAN exposure changes,
   VLAN reconfiguration, access revocation, credentials and shutdown require
   per-action approval in the default operating mode. The user may opt in to
   precisely scoped, independently approved high-impact playbooks; those may
   execute without repeated prompts only while their permit remains valid
   and all target, parameter, safety and cooldown bounds match.
6. **Keep the objective alive:** persist health snapshots, incident and
   remediation history, last-successful evaluation, next scheduled check
   and unresolved exceptions. Report "degraded", "unobserved" or
   "awaiting approval" instead of claiming protection.
7. **Respect constrained hardware:** continuous *scheduling* must not mean
   continuous local LLM residency. Use lightweight probes and rules when
   possible; queue model inference for meaningful anomalies or reviews.
   Release RAM/VRAM after each stage and resume correctly after downtime.

## Confirmed Level 5 remediation authorization model (2026-10-11)

**Both options required; safe-by-default is mandatory.** Provide a simple
Project setting labelled "Automatic fixes":

- **Recommended default — Low-risk only:** permit granted, reversible,
  bounded low-impact maintenance. Queue high-impact actions for a human
  approval decision; no automatic escalation just because a task is blocked.
- **Advanced opt-in — Include specifically preapproved playbooks:** the user
  explicitly approves individual high-impact actions, selected targets and
  limits in advance. Only those precise approvals can run automatically.
  A new action, target, wider parameter range or expired permit requires
  a fresh approval. There is no "unrestricted administrator" switch.

Every permit must record who approved it, canonical Project/Workspace,
action/tool identity, target resource identifiers, bounded parameters,
risk class, expiry/revocation state, permitted frequency and maintenance
window, an immutable policy revision, and an audit/provenance reference.
Approval must bind to the actual side-effect execution, not simply to an
Assistant's chat message or recommendation.

Safeguards: recheck the permit before execution, prevent duplicate execution
using an operation identity, restrict concurrency/cooldowns, snapshot the
prior state where appropriate, verify postconditions, and use safe rollback
when available. Unknown external outcomes pause for reconciliation. Failed
health verification cannot silently escalate permissions or continue an
unbounded retry loop.

**Example:** the operator may preapprove temporarily blocking outbound
access from **a single explicitly identified IoT device** under specific
conditions and for a fixed duration. That does not grant blanket permission
to quarantine other devices, alter WAN firewall rules or disable the
router. The system must retain an operator-accessible emergency stop and
a verified recovery path to avoid self-lockout.

**UI:** use checkbox/drop-down affordances for mode choice and authorised
playbooks, individual target selectors, time/window and expiry settings,
plus visible pause, revoke and "approval required" states. Start all
Projects and upgraded installations in Low-risk only; do not activate
advanced grants through migration or choosing Level 5 alone.

**Required negative tests:** invalid/missing approval; revoked/expired
permit; different target, action or parameters; policy changes before
side effect; approval escalation through alternative tools or a spawned
Worker; duplicate incident delivery; cooldown breach; unverified rollback;
unknown outcome; service restart; emergency pause; insufficient network
telemetry; and loss of operator connectivity.

## Implementation backlog — ordered, independently testable

- [ ] **OPM-01: Mode/goal schema and migration.** Add durable Project mode
      (`autonomous_project` default for all existing Projects) and L5
      objective configuration with revision/CAS, owner, scope, status,
      evaluation timestamps, retention, cadence, timezone and approval policy.
      Relevant areas: `internal/projectworkspace`, `internal/storage/sqlite`.
      Migration must not start existing Projects as L5 automatically.
- [ ] **OPM-02: Unified project lifecycle state machine.** Extend
      `internal/projectorchestrator` with goal evaluation,
      outcome/compliance criteria, schedules and explicit
      `active/healthy/degraded/attention/paused/retired` semantics. Never
      conflate an idle queue with a completed L5 goal.
- [ ] **OPM-03: Durable triggers and scheduling.** Add safe periodic/event
      task dispatch, idempotency keys, lease/cooldown windows, restart
      reconciliation and bounded backoff, integrating `internal/scheduler`
      and `internal/agentworker`. Respect interactive priority and
      resource starvation without missing recurring checks indefinitely.
- [ ] **OPM-04: Safe observation ingest and baselines.** Version inventory
      and evidence with provenance, capability grant, freshness and
      confidence. Build on `internal/observation` and
      `internal/projectworkspace`. No implicit LAN discovery.
- [ ] **OPM-05: Playbook policy and enforcement.** Default to low-risk,
      reversible fixes plus required approval for higher-impact actions.
      Implement opt-in, per-playbook preauthorisation with target/parameter
      bounds, expiry/revocation, maintenance windows, transaction-aware
      permission rechecks, pre/post verification, safe rollback, audit,
      idempotency and unknown-outcome holds via `internal/operation`,
      `internal/tool` and `internal/featurepolicy`.
- [ ] **OPM-06: Local-first runtime resource plan.** Queue monitoring
      evaluations, opportunistically run models, use Colibri and
      hotswap/unload where supported. Define recovery checkpoints,
      cloud spend ceilings and protected research pinning. No always-on
      model requirement or hidden fallback.
- [ ] **OPM-07: Mode controls and health UI.** In Project creation/Settings,
      add a single mode selector and simple controls for monitoring,
      allowed remediation, approvals, cadence and pause/resume.
      Show active mode, last healthy check, current detection, pending
      jobs, source freshness and reasons for blocking. Relevant:
      `internal/webui/static/project-development-page.js` and API.
- [ ] **OPM-08: Home-network defensive reference integration.** Start with
      opt-in, read-only availability/DNS/reverse-proxy/host diagnostics and
      explicit device inventory. Integrate only authorised sources; never
      make unsupported or unconfigured sources silently green.
- [ ] **OPM-09: Acceptance and negative-path tests.** SQLite migrations,
      no surprise L5 activation, resume after restart, no duplicate alerts
      or remediation, repeated failing health checks, canceled permits,
      illegal scope escalation, revoked approval, telemetry staleness,
      resource-constrained model scheduling and paused/retired goals.
- [ ] **OPM-10: Integrated code review and QA.** Run Windows/Ubuntu CI;
      compare implementation to the user-approved design, perform second
      code audit, then approved installer and physical-node testing.

## Definition of done

- L4 projects can be completed by autonomous planning and resource-aware
  Workers without manual task graph or model-residency management.
- L5 projects can independently run multiple monitoring/evaluation cycles,
  persist state across downtime, open and resolve issues and verify permitted
  remediations while staying within approved scopes.
- Policy revocation, unavailable resources, uncertain effects and exhausted
  budgets cannot trigger hidden escalation, duplicate actions or false
  healthy/completed statuses.
- The home-network reference can be demonstrated in simulated, consented
  test fixtures before enabling any live router/firewall mutation.
- New users can configure either mode through clear UI controls; experts
  can customize policies without weakening isolation.

## Design decisions still pending

- **Decided:** low-risk-only automatic remediation by default, plus an
  opt-in preauthorisation mode for individually bounded high-impact
  playbooks; never a blanket high-impact switch.
- **Outstanding:** concrete risk classification per adapter and action,
  permitted sensors/monitoring frequency, alert escalation, and target-
  specific permissions for routers, hosts and other devices.
- All six vision decisions are confirmed. Runtime implementation and
  full negative-path testing are still required.

**Status:** VISION CONFIRMED; IMPLEMENTATION PENDING and release review gates
remain in effect.

## Partial implementation checkpoint

The Project operating-mode representation is durable and nonactivating: existing Projects resolve to Level 4, while explicitly selected Level 5 appears in the create dialog and Inspector. An additive SQLite migration and authenticated API now support canonical scoped continuous goals with strict `configured/unobserved` initial state; a Project Inspector form uses verified canonical Workspace choices. No periodic triggers, probes, remediation, or claims of network protection have been activated. OPM-01 and OPM-07 remain **partial**; OPM-02 through OPM-10 require completion and full acceptance QA.
