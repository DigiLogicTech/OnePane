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
