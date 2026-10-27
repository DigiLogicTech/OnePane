# Stored recovery journal inspection

This continuation adds an explicit read-only diagnostic to a captured recovery case. It does not complete effect inventory, operator decisions, independent postconditions or safe retry. The original interrupted Task and Attempt remain blocked.

`GET /v1/tasks/{taskID}/recovery-cases/{caseID}/journal` requires the current original credential and `task.read` in the Task tenant. It reads the case and original interrupted Attempt, resolves the Worker by that Attempt, then uses the reviewed bounded SQLite Worker-journal observation in one transaction. A mismatched Worker owner is rejected rather than hidden as missing. Unknown or oversized evidence withholds the whole response.

Only case/Task/Attempt/run identity, Task revision, observation version/fingerprint and bounded step identity/number/kind/status/metadata hashes are returned. A boolean reports whether a stored result-reference format was recognized; it does not report that the target belongs to the Task or that an external effect succeeded. Raw result-reference targets, prompts, continuation state, policy JSON, error reasons and payloads are withheld. Missing records are explicitly not proof that no effect occurred.

After collection, the handler rechecks the exact case, current Task scope/revision and original credential/permission. Responses use private no-store caching. The UI collects only on explicit inspection, labels unknown outcomes and point-in-time observations, rejects malformed evidence and clears retained private data after failed reads or scope changes. It exposes no settlement, retry or execution control.

Source tests use disposable SQLite and synthetic journal rows. They verify original ownership, foreign-owner rejection, bounds, no Task/audit transitions, blocked readmission, actual bearer/browser live revocation/expiry/permission loss and UI disposal/inert rendering. They do not constitute real model, external effects, installed restart or final UI acceptance.
