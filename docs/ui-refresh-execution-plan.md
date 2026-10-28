# OnePane practical UI refresh

User decision,28October2026 Brisbane: after the functional walkthrough, the user agreed to refresh the UI around practical workflows. This authorises frontend redesign in the existing Windows-first continuation. It does not waive backend authority, data preservation, formal RC11 gates or the requirement to review before rebuilding.

## Agreed direction

Make Projects and Tasks the primary work entry points. Give the current task and Workspace most of the window. Put Inspector and Logs on demand with discoverable controls. Present local model setup as find/install/test/use, with advanced routing/runtime details secondary. Retain every existing capability and current backend APIs. Preserve persisted Project/Workspace data, saved layouts, permissions, provider choices, models and runtimes.

## Execution order and checkpoints

1. Preserve the functional-review fixes and evidence before broader visual changes. The review found setup blockers, invalid selectors, stale async renders, scope/display errors, broken responsive docking and persistence defects. See `ui-ux-functional-review-20261028.md`. The existing Windows Test2 MSI remains provisional.
2. Refresh the shell and Project work entry: grouped navigation, clear current context, readable Project list, deliberate panels, understandable actions. Use the real scratch backend rather than fixture-only success.
3. Refresh Workspace/Task working surfaces and model setup. Consolidate duplicate controls; keep advanced settings available without occupying the primary work area. Do not expose scaffolding as completed workflows.
4. Review all affected routes in dark/light/custom themes, desktop/narrow windows, keyboard flows and saved-state restoration. Carry every functional finding into the refreshed implementation.
5. Run affected frontend/API checks and exact-source CI; remedy failures. Only then rebuild the next Windows test installer with provenance, checksums and documented acceptance limits. No production installation or upgrade is part of the frontend review.

Current phase: save the functional checkpoint, then implement and demonstrate the shell/Project refresh. Ubuntu draft remains deferred and preserved. No new release-completion claim is authorised.
