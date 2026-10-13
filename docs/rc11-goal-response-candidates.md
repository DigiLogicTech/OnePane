# RC11 — Fail-closed Level 5 response candidate ledger

**Status:** proposed response *intent only*. This increment deliberately does **not** provide an execution permit, real response playbook, automatic remediation, independent sensor trust, or a host/Node/device effect.

A human Project operator may create a durable, individually scoped **candidate** linked to an already human-reviewed incident (`decision=needs_investigation`) in the same canonical active Level-5 Project/Workspace. The record captures source, immutable TCP alert, exact case revision, monitoring-plan hash **and monotonic generation**, action code, risk class, explicit finite expiry, immutable audit event and actor. The only accepted *candidate* action is `restart_local_observation_worker` (not an implemented adapter). Expires within 24 hours; no blanket "admin" or high-impact enable switch. One outstanding candidate per incident may exist, with a human-only withdrawal transition and CAS revision. SQLite enforces `execution_authorized=0` for **all** records, including a direct SQL mutation. No migration converts an existing Project into Level 5 or activates probes.

Operator APIs (Project role enforcement **plus human principal**):
- `GET /v1/projects/{projectID}/continuous-goals/{goalID}/response-candidates` — `project.read`, metadata only
- `POST .../response-candidates` — `project.write`, explicit case, action, goal/case revisions and expiry
- `GET .../response-candidates/{candidateID}/readiness` — `project.read`, always `state=blocked`, explicit blockers
- `POST .../response-candidates/{candidateID}/withdraw` — `project.write`, human CAS withdrawal

A candidate **cannot** grant a Tool Gateway mutation permit, Operation Coordinator lease, device action, self-healing effect or active protection. This is intentional while the real adapter and independent postcondition/rollback verification are missing. The existing `internal/operation` coordinator owns the only appropriate later effect lifecycle; a future approved executor needs its own separately bound, versioned explicit consent with execution-time revocation, idempotency, prestate, independently observed result, compensation and blocked-unknown outcome. In particular, this historical candidate must **never** be grandfathered into that new execution authority.

Regression scope: agent/cross-Project denial, stale case revision, invalid action, duplicate intent, tamper-evident immutable scope, expiry/withdrawal, SQLite forced-authority denial, API auth and read-only readiness, no Task/monitor/device effect. CI tests do not replace installed or physical Node acceptance.

**Follow-up:** Build one genuinely reversible *local* adapter with independently verified postconditions under the existing Operation Coordinator, then user-approved advanced action permits and emergency stop. Independently verified external target identity and device actions remain separate release gates. Final comprehensive user-centred UI/UX alignment occurs last before an approved QA build.
