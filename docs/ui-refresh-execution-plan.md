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

Current phase: shell/Projects, Workspace configuration, Tasks filtering/keyboard inspection and Local Models hierarchy implemented; post-refresh review and exact-source CI in progress. Ubuntu draft remains deferred and preserved. No new release-completion claim is authorised.

## Resumed refresh checkpoint — 28 October 2026

- Shell: Work / AI / System groups; current-Project subtree; accessible navigation names when icons are shown alone; Inspector/Logs closed for new profiles, with saved preferences retained. Panels menu provides an explicit focus action. Narrow-window group labels/disclosures no longer crowd the icon rail.
- Projects: full-width compact rows, search/sort/count and explicit Open workspace action. Existing Project settings and deletion controls remain available.
- Workspace: compact expandable configuration with explicit runtime-permission limitations; layout editing shows an unsaved-change notice. Cancel removes the draft notice without saving. Repeated reloads retain the active Project/Workspace and both Development/Dashboard tabs.
- Tasks: literal case-insensitive search, Project/global and state filters, truthful loaded-result counts (API remains bounded to250), readable states/scheduling/weekday names, and a keyboard-operable Inspect button. Scheduled and archived views remain available. Filtering does not mutate Tasks.
- Local Models: installed models precede runtime/hardware management; find/install/Agent Check/Workspace guidance links to the existing real workflows. Runtime management remains expandable and starts open when there are no deployments. Corrected green Not installed runtime badges.
- Startup correctness: boot now waits for DOMContentLoaded so all subsequent classic feature scripts register before restoring the first route. This fixes intermittent missing Library navigation and Development tabs on Workspace reload. Actual repeated browser reload and executable registration-order regression passed.

Validation: all33 JavaScript validators pass, including executable filter/boot/runtime-status regressions. Edge reviewed the real scratch backend through a current-asset gateway at1280x800,800x700 and390x844. Tasks and Models had no document horizontal overflow; small-screen Task tables scroll internally. Verified combined filters/empty results, keyboard Inspect, Panels dismissal, layout cancel, saved Workspace restoration, Find a model catalogue navigation, and Light/Dark appearance. No model download, inference or agent execution was performed in this refresh pass. Earlier genuine CPU evidence remains scoped to its recorded build.

Evidence: `.tools/ui-qa-20261028-01/screenshots/refresh-projects-light.png` and `refresh-models-dark.png` under the parent workspace. This resumed browser uses a new origin/profile; the prior browser-local custom theme and Notes fixture were not available and were not overwritten. Custom-theme and wider page-by-page refreshed regression remain open. The scratch backend is the earlier explicitly labelled developer review executable with current frontend assets; this is not proof of a new packaged-source build.

Next: inspect exact-source CI for the refresh commit; finish custom-theme, remaining-page consistency and full affected-workflow regression, then build the next Windows test MSI from the validated source. Keep formal RC11 and installed/upgrade/hardware acceptance requirements open. The previous functional checkpoint7af19d7 passed complete integration40616132918 and source guards40616132865/40616132867; those results do not validate this later refresh.
