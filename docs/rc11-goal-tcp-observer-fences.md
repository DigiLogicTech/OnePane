# RC11 — Human pause/resume of consented recurring TCP observation

A **reversible, per-Goal dispatch fence** now controls **future** unattended exact-IP TCP attempts without mutating an existing human TCP probe permit, recurring grant, device or process. A human with Project write permission can set `paused`, then independently set `running` using optimistic concurrency. It stays paused across daemon restarts. `running` adds **no network authority**: both separately approved existing manual+recurring TCP grants, original human identity and membership, exact target, source and cadence, active Level 5 Project and Goal, all still must pass.

Routes:
- GET `/v1/projects/{projectID}/continuous-goals/{goalID}/tcp-observer-fence` — human `project.read`
- POST same — human `project.write`, JSON `{"target_status":"paused","expected_goal_revision":12,"expected_fence_revision":0}` (reverse via `running` and the returned fence revision)

The new migration `0094_goal_tcp_observation_fences.sql` retains the durable per-Goal fence. Human activity creates append-only audit events; SQL cannot delete the latch, repoint its Project/Workspace, skip a CAS revision or pretend toggling is an execution permission.

**Three observation admission boundaries** check suspension: the bounded due-grant selection query, the SQLite writer transaction when an attempt is claimed, and the last check before `DialContext`. A human pause does not erase or revise prior probes, does not assert device security or remediation, and does **not** cancel an already-in-flight network handshake. The existing 100–1500ms socket timeout remains. Manual explicit TCP tests remain separately permitted and unaffected. Resumption does not backfill missed cadence windows.

This is the **real reversible local scheduling effect**, **not** yet the proposed `restart_local_observation_worker` action and not yet attached as a Tool Adapter under Operation Coordinator. The prior response candidate remains non-executable and cannot be converted into consent.

Next: a separately human-approved, permit-bound Tool Adapter for this *exact* reversible pause action under Operation Coordinator, plus independent state/postcondition verification, crash/unknown-outcome and compensation acceptance. Do not conflate local scheduling control with independent external device remediation. Keep final user-centred UI/UX review last before an approved QA build.
