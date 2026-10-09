# RC11 — Workspace QA diagnostic snapshot (foundation)

This is an **incremental, explicitly limited part** of [Debug & QA Diagnostics Centre #85](https://github.com/DigiLogicTech/OnePane/issues/85). It is not the final global Debug Centre or a replacement for system/installer/Node trace capture.

## Operator flow

1. Open the **Project → Development Workspace → Workspace Task queue**.
2. Expand **QA diagnostic snapshot (read-only, opt-in)**.
3. Choose **Review included data**. OnePane checks the current logged-in principal's `project.read` and `task.read` rights, resolves the exact Project and canonical Workspace and returns up to 50 recent scoped Task status summaries.
4. Inspect the shown JSON, excluded categories and status. **Generate QA ZIP** becomes available only after successful preview.
5. Optionally choose **Copy sanitized QA summary** to copy a short text report (build version/revision when format-valid, UTC time, Task-state counts and error-event totals). It does **not** copy Task names/objectives, IDs, source paths, prompts, raw traces or log payloads. If the browser blocks Clipboard access, the read-only text area is selected for manual copying. No automatic upload.
6. Choose **Generate QA ZIP**, then inspect the locally saved `onepane-workspace-qa-snapshot.zip` before deliberately attaching it to a bug report. OnePane does not automatically upload it to a server or vendor.

### API

- `GET /v1/qa/workspace-snapshot?workspace_id=<tenancy>&project_id=<id>&project_workspace_id=<canonical-id>` returns the allowlisted JSON preview.
- `POST /v1/qa/workspace-bundle` with JSON `{"workspace_id":"…","project_id":"…","project_workspace_id":"…"}` returns a small on-device ZIP; normal authenticated session **and CSRF protection** apply.

Both are denied when tenancy/Project/Workspace scope or permission is not valid. They use the existing scoped Task reader and scoped Worker/dependency projections; no raw journal, arbitrary SQL, Task completion, model prompt, source file or log payload is serialized.

### ZIP manifest

Contains exactly:
- `manifest.json`: schema version, created UTC, origin, excluded categories and SHA-256 digests.
- `snapshot.json`: sanitised Task IDs and opaque Task/Run refs, state/revision/update time, permitted latest Worker status/counters, hard dependency counts, and **up to 96 recent Task/Worker event ledger entries**. The event timeline includes only a fixed known event-type vocabulary, timestamp, severity, and SHA-256-derived Task/Run/event/trace/request references; it excludes event payloads, principal IDs, messages and unknown event types. No objective, raw continuation, execution inputs, failure text or tool output.
- `README.txt`: explicit limitations and sharing guidance.

Up to **50 Tasks and 96 timeline events** (sorted newest first); the ZIP is in-memory, maximum **128 KiB**, with fixed archive paths. It neither writes a temporary system log nor archives arbitrary folders. Source changes after preview may alter the fresh exported snapshot.

### What this version cannot diagnose

It can correlate existing Task/Worker event metadata but **cannot yet capture per-click UI actions, complete cross-component HTTP traces, MSI setup logs, system resource snapshots, raw Node diagnostics, interactive timeline filtering, operator-entered reproduction notes or time-limited verbose instrumentation. These remain open in #85. A missing Worker run is reported as no execution entry—not as proof that execution occurred or succeeded.

**Release gate:** Finish #85 plus [three-phase code audit → vision questions → second code review #86](https://github.com/DigiLogicTech/OnePane/issues/86) before RC11 packaging. Preserve RC10 as the recovery baseline.

## Opt-in browser incident capture (incremental #85 implementation)

The Development Workspace Task queue now also has **Browser incident capture**, separate from the Task/Worker ZIP. This is an in-memory session recorder controlled by the **user**, not always-on telemetry.

1. Expand **Browser incident capture** and select **Start capture**. Recording is **off by default** and starts only from that button. It continues across OnePane routes in this browser tab, even when the Workspace panel is no longer visible. It never automatically starts after refresh/restart.
2. Reproduce the problem and optionally press **Mark issue** to insert a timestamped bookmark. **Stop capture**, or allow the hard **10-minute expiry** to stop automatically. Capture can be cleared at any time.
3. Select **Review incident trace**. It shows a bounded, locally constructed JSON report with generic UI button/link/form activity, fixed route categories, API *subsystem buckets*, request method, response status and elapsed milliseconds, and counts of unhandled browser errors. **No exception messages, stack traces, raw API paths or parameters, bodies, text inputs, clipboard, chat or model output.**
4. Select **Download reviewed JSON**, which is available only after stopping and reviewing the exact report. A changed/restarted capture requires reviewing again. The downloaded `onepane-browser-incident.json` is created entirely by the browser; **OnePane never uploads the report or transmits it to a diagnostic backend**. Attach it manually together with the separate Workspace QA ZIP only if you choose.
5. **Clear capture** removes the in-memory events. Navigation between pages preserves the current capture, but browser refresh/restart clears it.

**Safety:** Capture expires after 600 seconds, retains only 120 recent events and discloses a count of events dropped if exceeded. Only same-origin `/v1/` request categories are observed, without paths, queries, headers, bodies or content. DOM listeners and the temporary browser fetch wrapper are removed when capture ends. The recorder does not change network requests, task permissions, local/cloud inference routing, agent tools, Workspace isolation or any backend policies.

**Limits:** A browser-side incident timeline is not end-to-end server tracing and does not include installer logs, Node/container journals, Agent Check offload telemetry, Windows service logs, or user-entered reproduction notes. Neither the new incident JSON nor the older scoped Workspace ZIP is the complete consolidated support bundle. Those remain mandatory work in #85.

## Local model Agent Check evidence (incremental #85)

A read-only **QA evidence** button is available in the Local Model **Agent Check** review dialog. Selecting it opens a permission-checked preview; the operator can then choose **Download reviewed JSON** to save `onepane-agent-check-qa.json` locally. No upload or model inference occurs merely from viewing diagnostics.

The endpoint is `GET /v1/qa/model-deployments/{deploymentID}/agent-check`. It authenticates the current user and enforces OnePane's existing `model.read`/managed-deployment authority before opening any evidence records. It does **not** enumerate other models or Nodes. Model deployments that belong to the Node-wide inventory continue to require the existing administrator authorization.

The report includes only:
- Opaque hashed deployment/session references (not the raw IDs).
- Actual stored model deployment status, reported residency state and last update time.
- Up to **10** latest testbed sessions, their known status (`active`, `completed` or `cancelled`), start/end timestamps and counts of *persisted successful* inference and synthetic tool-probe turns.
- **For newly observed failures after the RC11 schema upgrade:** the latest machine-coded stage and broad category, with observed timestamp. Categories include deadline/cancellation, missing resource, invalid response, persistence, runtime execution, or explicit session abort. They are generated from known code paths and error types, **not** by exporting arbitrary runtime messages. The report prefers the latest **non-abort** failure over the UI's later cleanup/abort event so cleanup cannot conceal a previously recorded failing inference stage. If the only evidence is an abort, it reports that explicitly. This is a selected observed stage, not a guaranteed root-cause diagnosis. Older sessions retain the `not_recorded_as_structured_evidence` state; no backfill guesses.
- Explicit limitations and whether older sessions were truncated.

**Important limits:** The Testbed schema stores full request/response JSON and arbitrary notes; the QA projector does not select them. The separate additive `model_agentcheck_failure_observations` table stores only fixed stage/category enums and timestamps; its contents cannot be edited/deleted. Not every error path has a persisted Testbed session: preflight failures before session creation and diagnostic-write failures may still lack evidence. A cancelled session is **not** proof of a particular inference error. A reported model residency value is *not* an independently measured CPU/GPU load or proof of model unload. Failed probes that never became persisted turns are not counted; absence of turn evidence is not success or failure evidence. No remote Node state, GPU telemetry, driver diagnostics, error text, installer log or raw model output is returned. Full correlation and redaction acceptance remain on Debug Centre issue #85.

This feature is separate from the authorised Workspace Task ZIP and the opt-in browser incident JSON. A consolidated support bundle is **not yet built**.

## Legacy Agent Check qualification privacy remediation (migration 0042)

Older OnePane manual Agent Check v2 qualification records could include raw exception strings under `evidence.errors`. Those messages may contain local paths, backend output or even sensitive string values. They are **not** needed to diagnose failures now that typed failure observations exist.

- **New failures:** manual qualification v3 stores only `infrastructure_error` and the fixed `inspect_structured_agentcheck_evidence` guidance. It does not write `error.Error()`; successful testbed completion also does **not** copy raw error arrays into a qualification report. It counts any typed failure observations and limits the result rather than silently marking a recovered but previously failing session fully passed.
- **Existing records:** additive migration `0042_redact_legacy_agentcheck_errors.sql` removes the known `$.evidence.errors` arrays from `model_spec_sheets.qualification_json` and removes the corresponding nested `$.qualification.evidence.errors` arrays from `model_identity_specs.qualification_json` for **manual Agent Check v2 only**. A fixed `redacted_on_upgrade` marker replaces the removed content. Model inventory, installation locations, admissions, testbed sessions, observed performance values and unrelated qualification documents remain intact.
- **No invented history:** migration 0042 does not synthesize failure categories for old records, change production admission decisions, or claim that all old probe errors can be classified. Structured failure observations from migration 0041 remain the preferred source of new evidence.

**Data-remanence limitation:** This migration removes sensitive values from live logical records; it does **not** erase old database backups, copies already exported, retained WAL frames, SQLite free pages or files elsewhere on disk. Historical backup retention and storage-level secure-erasure policy need separate review during the pre-release code/security audit. No blanket VACUUM or filesystem deletion is performed automatically, to protect existing installations.

This is a targeted data-privacy remediation, not final physical Windows/Ubuntu or installer validation.

## Admin-only Node control-plane diagnostics (incremental #85)

Open **Nodes → Manage node → QA evidence** to view the selected Node's persisted control-plane observations. The read-only route is `GET /v1/qa/nodes/{nodeID}/evidence` and reuses OnePane's existing **Node Administrator** check, not ordinary Workspace access. After viewing the plain-text report, use **Download reviewed JSON** to save `onepane-node-qa-evidence.json` locally. The report is never automatically uploaded.

The report deliberately includes only machine-typed metadata: a pseudonymous Node reference, recorded local/remote trust state and last-seen timestamp, capability manifest receipt/expiry timestamps and sequence (never the manifest contents), known pairing state, aggregated historical wake attempt results and aggregate remote-inference receipt statuses. Every query uses the exact administrator-selected registered Node ID. Missing manifests and unrecorded events show explicit unavailable states rather than fabricated success.

**Local service-manager observation (incremental):** For the **canonical local Node only**, an administrator opening QA Evidence now triggers one bounded, read-only check of the packaged OnePane service: `/usr/bin/systemctl show ... onepane.service` on Linux or the fixed `OnePane` Windows SCM query through System32 `sc.exe`. The server uses no shell, no user-provided service names, no privilege escalation, no remote execution or journal/log read, a **2-second deadline**, and an **at-most-4-KiB discarded-output buffer**. Only an allowlisted status (`running`, `stopped`, `starting`, `stopping`, `failed`, `paused`, `not_installed`) and the collection source/time are returned. If a query fails, times out or is unsupported, `operating_system_service_state` remains `not_collected` and the collection status indicates why.

**Operational caveat:** The service-manager result is **not** end-to-end OnePane application readiness, and even `running` does not establish healthy tasks, models, storage or dependencies. Node last-seen timestamps and historical inference receipts still do not establish current reachability. For **remote Nodes**, `operating_system_service_state` remains **`not_collected`**; no remote commands or privileged Node-agent operations occur. Local OS manager checks do not include logs, raw process arguments, driver diagnostics or GPU/CPU measurements. Installed Windows/Ubuntu acceptance testing and separately governed remote service health remain outstanding under #85.

Excluded: TLS keys and pairing codes/tokens/certificates, endpoint URLs, manifest payloads, wake targets, task IDs, remote request IDs, model inference outputs, error codes, log strings and raw service journals. This report is separate from the Workspace QA ZIP, browser incident trace and Agent Check JSON. Consolidated privacy-reviewed support-bundle packaging remains outstanding.
