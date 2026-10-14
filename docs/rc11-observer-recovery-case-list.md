# RC11 — Read-only recovery case pointers for local Goal observers

Authenticated human Project readers can inspect up to 25 **exact-Goal, exact-Project/Workspace Task-bound** observer operations that are currently in an in-flight, uncertain or failed-compensation state:

`GET /v1/projects/{projectID}/continuous-goals/{goalID}/observer-recovery-cases`

A result includes only `operation_id`, exact local pause/resume Tool ID, persisted operation state and update time, plus conservative flags: `human_reconciliation_required=true` only for durable unknown, blocked or compensation-failed states and always `independent_device_verification=false`. Transient executing, observing, verified and compensating states are visible but do not themselves prove a crash or mandate reconciliation. The operator can use the existing Operation recovery controls only under their own independent permissions; this endpoint grants none. The separate retention inventory provides the **total** count; only the latest 25 records are shown here, with no unbounded historical payload expansion.

It never returns the original Tool input, Task objective, operation receipt, IP/port, credentials, host identifiers or Project-private payloads. Source-only in-flight or failed operations do not establish physical security incidents. No operation is executed, retried, compensated, marked verified or accepted as reconciled. A crashed worker's prior outcomes must remain in the Operation Coordinator's blocked-unknown path until an authorised human completes independent reconciliation. The operator may not infer target identity, sensor vantage or endpoint health from this projection.

This is API-level evidence accessibility, **not** automatic Level-5 remediation. Final human-focused UI/UX review remains the last development workstream before approved installer QA.
