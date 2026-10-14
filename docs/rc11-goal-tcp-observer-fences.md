# RC11 — Human pause/resume of consented recurring TCP observation

A **reversible, per-Goal dispatch fence** now controls **future** unattended exact-IP TCP attempts without mutating an existing human TCP probe permit, recurring grant, device or process. A human with Project write permission can set `paused`, then independently set `running` using optimistic concurrency. It stays paused across daemon restarts. `running` adds **no network authority**: both separately approved existing manual+recurring TCP grants, original human identity and membership, exact target, source and cadence, active Level 5 Project and Goal, all still must pass.

Routes:
- GET `/v1/projects/{projectID}/continuous-goals/{goalID}/tcp-observer-fence` — human `project.read`
- POST same — human `project.write`, JSON `{"target_status":"paused","expected_goal_revision":12,"expected_fence_revision":0}` (reverse via `running` and the returned fence revision)

The new migration `0094_goal_tcp_observation_fences.sql` retains the durable per-Goal fence. Human activity creates append-only audit events; SQL cannot delete the latch, repoint its Project/Workspace, skip a CAS revision or pretend toggling is an execution permission.

**Three observation admission boundaries** check suspension: the bounded due-grant selection query, the SQLite writer transaction when an attempt is claimed, and the last check before `DialContext`. A human pause does not erase or revise prior probes, does not assert device security or remediation, and does **not** cancel an already-in-flight network handshake. The existing 100–1500ms socket timeout remains. Manual explicit TCP tests remain separately permitted and unaffected. Resumption does not backfill missed cadence windows.

This is the **real reversible local scheduling effect**, **not** yet the proposed `restart_local_observation_worker` action and not yet attached as a Tool Adapter under Operation Coordinator. The prior response candidate remains non-executable and cannot be converted into consent.

Next: a separately human-approved, permit-bound Tool Adapter for this *exact* reversible pause action under Operation Coordinator, plus independent state/postcondition verification, crash/unknown-outcome and compensation acceptance. Do not conflate local scheduling control with independent external device remediation. Keep final user-centred UI/UX review last before an approved QA build.

## Operation Coordinator adapter (controlled local effect)

A dedicated `internal/goalobserveradapter` now registers exact Tool IDs `project.goal.tcp_observer.pause@1` and `project.goal.tcp_observer.resume@1`. Both are `mutate` operations, separately require an `ApprovalApprover` approval, minimum V2 post-operation verification and an independently scoped authority lease and workspace `network.connect` permission. **Registration does not grant leases or automation.**

Only the existing Operation Coordinator may mint a one-use mutation permit. The Tool Gateway validates and consumes that token and the capability lease before calling the adapter. The adapter independently resolves the true human actor from the persisted running Tool Invocation and its executing Operation, active principal, canonical Project tenant membership and exact Goal-scoped resource; it **rejects an actor field in caller JSON**. The resource must equal `goal-tcp-observer://{projectID}/{goalID}` and the JSON input must specify `project_id`, `goal_id`, `expected_goal_revision`, `expected_fence_revision` and nothing else.

The only effect is the existing SQLite CAS pause/resume change. The adapter rereads persisted status, fence revision and human actor as a local postcondition. It does **not** independently attest a physical Node or endpoint, commit the Operation on its own, open a network socket, cancel an in-flight check, or silently replay an uncertain mutation. Interrupted Operation Coordinator effects remain `blocked_unknown_outcome` after restart until operator reconciliation. Compensating a committed pause requires a **new** separately approved `resume` Operation and an updated expected fence revision; there is no hidden automatic rollback or restoration of revoked human consent.

An existing `restart_local_observation_worker` response candidate is **never** converted into a Tool grant by this integration: these are distinct human-governed pause/resume effects. Automated Level 5 execution remains disabled pending explicit playbook-scoped permits, stronger independent verification and acceptance.
