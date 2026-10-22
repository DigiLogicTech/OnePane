# RC11 integrated branch — initial code and security audit

**Scope:** draft PR [#227](https://github.com/DigiLogicTech/OnePane/pull/227),
consolidating workstream PRs #224, #225 and #226.
**Code evidence:** commit `8c2fdc429118e84850d92f6ff81a590f3e55d14d`
passed [hosted CI #38118216676](https://github.com/DigiLogicTech/OnePane/actions/runs/38118216676).
This is an assistant-performed preliminary audit, **not the user's first code approval**
and **not** installed Windows/Ubuntu or physical Node QA.

## Inspected integration paths
- `internal/featurepolicy/policy.go` and `persisted_scopes.go`:
  permissions flow from a persisted Project through its canonical Workspace
  and Task override. Invalid owner IDs, missing scopes and inactive tenants
  are rejected; a child cannot restore a disabled parent capability.
- `internal/agentworker/service.go` and `execution.go`:
  admission and continuation check active Workspace, Task, Attempt and
  policy; fallback grants are independently checked.
- `internal/inference/execute.go` and `internal/tool/gateway.go`:
  independently retain existing authority, information-flow, approval,
  budget and sandbox rules. A configured feature grant is also checked
  before inference dispatch or a classified file adapter runs.
- `internal/webui/static/policy-diagnostic-review.js` and Go diagnostic
  source: fixed allowlist and typed categories, bounded event count/size,
  opaque hashed correlation IDs and local-only deliberate review/export.

## Findings and decisions required

| Priority | Finding | Treatment before release |
| --- | --- | --- |
| High | Task-less interactive chat/direct inference does not carry a canonical Project/Workspace/Task feature-policy context. Existing authority/data-flow boundaries remain, but feature policy cannot be claimed universal. | Choose explicit Workspace binding for sessions, then implement and test it. |
| High | Agent-profile policy is not independently persisted and bound to each task/run. Current execution override is restrictive, but it is not an Agent-profile authority source. | Define Agent identity, policy version/provenance and inheritance rules. |
| High | File feature checks cover explicit file Tool Gateway mappings, not every way an OCI command or Git tool can read/write Workspace files. Existing sandbox checks remain mandatory. | Define an action taxonomy and apply it at all side-effect gateways; block if mapping is unknown. |
| Medium | Provider-backed local inference may be classified as cloud when `ProviderConnectionID` is non-nil. This errs toward denial, but may frustrate local-first setups. | Agree a verifiable trust/destination classification independent of friendly provider names. |
| Medium | Revocations are rechecked at last application-controlled boundaries, but the check and external side effect are not one atomic transaction. | Agree permit lifetime and cancellation semantics; define race tests for revocation. |
| Medium | The offline diagnostic reviewer is not a live capture source and produces no proof that a runtime action was enforced; hashing identifiers protects raw values but does not guarantee strong anonymity for guessable low-entropy inputs. | Decide whether live scoped policy events and keyed correlation are required; keep exports explicitly reviewed. |
| Medium | `AssistedDefaults` disables autonomous routines until parent policy grants them. Enabling just one feature-policy field may unexpectedly stop a previously active autonomous Workspace. | Define upgrade defaults, user-facing consent and migration behaviour. |
| Release gate | Hosted Go, Node, Windows compilation and MSI reader succeeded, but physical Node execution, MSI installation/upgrade, GPU model residency, responsive themes and operator QA have not run. | Retain the user-approved QA/build gate after the second code audit. |

## Vision-alignment interview — product decisions

1. Should **OnePane Chat** always run in a selected Project/Workspace
   context, or should the global assistant have a separately governed
   home Workspace with explicit grants?
2. For the local-first promise, should autonomous local execution be
   **enabled by default inside an approved sandbox**, or explicitly
   enabled for each Project/Workspace before agents can act?
3. When the **Files** switch is off, should it also deny arbitrary
   shell commands, Git operations, artifact publishing and MCP tools
   that might touch the Workspace filesystem?
4. Should per-Agent policy be a **versioned, immutable execution
   snapshot**, or should revoking permissions instantly stop running
   Agents, including when a model call is already underway?
5. Should Debug & Diagnostics support an **operator-controlled, time-
   limited live policy-event stream**, or is offline support-bundle
   review the preferred privacy posture?
6. Should local OpenAI-compatible providers and remote trusted Nodes
   qualify as **local-only inference** when destination and trust are
   verified, without enabling cloud-model grants?

## Order of operations
- **Completed:** isolated branches, automated tests, combined integration,
  hosted CI and preliminary audit.
- **Next human gate:** user first code review, then resolve the six
  architectural questions and record a definitive product contract.
- **Next solo work:** implement the agreed scope, rerun full code audit,
  CI, safety checks and compatibility regressions.
- **After explicit approval:** produce installers, run upgrade/persistence
  QA on Windows x64 and Ubuntu amd64, and validate physical Nodes.
- **Prohibited until then:** release merge, installer publishing and
  claims of full production/security certification.


## Additional implementation-level review (2026-10-11)

This second technical inspection of the first-review candidate found two
concrete, reproducible transaction-bound policy races and one privacy issue:

1. **Fixed — Worker admission race.** The original `startRun` checked
   `tasks.autonomous` and `inference.fallback` before entering its durable
   Task/Attempt/Worker transaction. It now resolves both grants through the
   *same* `storage.Tx` immediately before writing any admission rows.
   Concurrent scope changes observed at admission fail closed.
   Regression: `policy_admission_transaction_integration_test.go`
   deterministically revokes each grant after preflight and verifies that
   Task, Attempt, Worker and audit rows are not created.
2. **Fixed — Worker resume race.** The original
   `resumeWaitingWorkerAndAttempt` could mark a suspended Worker runnable
   after autonomy had been revoked. It now checks the stored effective grant
   inside its resume transaction before updating Task/Attempt/Worker state.
   Regression verifies a revoked Workspace leaves the same waiting Attempt
   and Task revision intact.
3. **Fixed — Guessable diagnostics identifiers.** A simple truncated
   SHA-256 digest of a typed correlation ID could be enumerated offline,
   especially for low-entropy IDs. The exporter now uses a random,
   process-local keyed HMAC-SHA-256 fingerprint and redacts identifiers if
   its key cannot be generated. It does not persist or export its key.
   Fingerprints deliberately change when the backend process restarts;
   long-term correlation is not currently guaranteed. Tests reject
   unkeyed SHA-256 output.

These fixes preserve the earlier architecture gates. The direct inference
and federated transport, interactive chat, non-file tools and Agent-profile
paths still need a **user-approved scope contract**, not an implicit grant.
Rechecking grants just before a side-effect narrows a race window but is
not an atomic transaction with an external model provider or tool adapter.

### Review boundary

- Completed: assistant source-level review, documented defect fixes and
  automatic CI execution of the affected Go/SQLite and JavaScript suites.
- Not completed: **user's first approval of the code**, confirmation of
  the six vision decisions above, post-alignment second audit, native
  installer/upgrade testing and physical Node QA.
- Keep [PR #227](https://github.com/DigiLogicTech/OnePane/pull/227) draft
  until those gates are explicitly satisfied.
