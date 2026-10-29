# UI refresh QA fixes — 28 October 2026

Follow-up: the user requested repair of the remaining Policy integration fixture and installation. The fixture now uses a real persisted watchdog and additionally verifies denial with a missing/stale watchdog and recovery after a renewed heartbeat. The complete tagged Policy suite passes alongside the affected Watchdog/API/SQLite/Windows service suites. UI QA 5 is prepared from the same targeted runtime fixes plus this test correction. Its installation and final verification are recorded separately in the parent workspace's `builds/windows-ui-qa-20261028-05` evidence. The earlier Policy failure below is a historical QA 4 result.

The two findings from `builds/windows-ui-qa-20261028-03/QA-REPORT.md` in the parent workspace are addressed in the working source. The original failing build and report are preserved.

## Changes

- Watchdog retries SQLite BUSY/LOCKED result codes (including extended codes) at the existing tick interval. Failed writes never advance the durable heartbeat. Permanent failures remain fatal to the loop, cancellation still terminates promptly, and the existing 15-second policy freshness gate is unchanged.
- The production API is wired to the actual watchdog. `/v1/health` retains HTTP 200 for liveness but reports `status: degraded` with `watchdog: stale_or_unavailable` when the heartbeat is stale or cannot be read. A new successful durable heartbeat restores `status: ok`.
- Periodic health polling updates both desktop and mobile shell badges, rather than leaving the startup value displayed indefinitely.
- Operational polling refreshes the active Tasks rows and state options in place. Search, Project selection, selected state and archived/scheduled view remain intact. A selected state stays available even when no loaded task currently matches it. Closed/replaced Tasks views are not redrawn.
- Added watchdog contention/cancellation/permanent-error regressions, a real SQLite lock/expiry/recovery integration test, health degradation/recovery API tests, and an executable Tasks renderer regression. The new JavaScript regression is included in the existing frontend CI workflow.

## Validation performed

- All 34 JavaScript validators pass, including the new live renderer and shell health badge cases.
- Native Go tests pass for API, WebUI, Watchdog, Policy and Windows service. The bootstrap package compiles and has no test files.
- Tagged integration tests pass for Watchdog, API, WebUI and Windows service. The real SQLite watchdog regression verifies that heartbeat/revision do not advance under contention, health expires, and the same running loop resumes after release.
- A wider tagged Policy run fails `TestSQLiteAuthorityAndPolicyDenials` with `watchdog_unavailable`. The exact same test also fails on the untouched `787274d0` archive: its fixture constructs an engine without configuring a watchdog. This pre-existing fixture failure remains open and is not represented as passing validation.
- The rebuilt Windows backend was run against a copy of the earlier disposable QA database, not installed operator data.
- Real process fault injection held a rollback-only SQLite write transaction for 38 seconds. `/v1/health` became degraded while the heartbeat aged; the UI displayed Degraded. After rollback, the same backend committed revision 41 with a heartbeat age of 0.5 seconds and health returned to OK. The UI then returned to Healthy without restarting/reloading.
- A newly created task changed from Created to Blocked while Tasks remained open. The Created filter and search/Project selections survived the poll; selecting Blocked showed the task, and no page reload was needed. No model was configured, so the routing block was expected.
- No browser console warnings/errors were captured in the regression session.

## Build and provenance

Fixed diagnostic package: `builds/windows-ui-qa-20261028-04` under the parent workspace. It contains a new backend, service, desktop launcher and unsigned MSI, with extracted-payload verification and checksums.

This is an explicitly labelled **uncommitted fix build**: base commit `787274d05cc8cffae32326c4b4c4d264f85b0a07` plus the targeted fix overlay listed and hashed in `source-overlay.json`. The backend reports revision `787274d05cc8cffae32326c4b4c4d264f85b0a07+ui-fixes` and version `0.1.0-alpha.3.3-ui-qa.4+787274d0-fixes`. The build snapshot reconstructs only the added watchdog wiring in `cmd/harnessd/main.go` and the new workflow regression entries; it excludes the unrelated Ubuntu working-tree edits. No remote CI for this uncommitted overlay is claimed.

The two targeted regressions pass; full release acceptance remains open. No MSI installation/upgrade/rollback, installed Windows service or native WebView2 walkthrough, model download, successful inference, or broad hardware/autonomy acceptance was performed. MSI ProductVersion remains `3.3.11`, shared with Test 2 and reserved RC11, so this is not a validated same-version upgrade path. The isolated QA process is stopped after evidence collection; test data and evidence remain available.
