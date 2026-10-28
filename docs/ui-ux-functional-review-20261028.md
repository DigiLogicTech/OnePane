# Windows UI/UX functional review — 28 October 2026

Status: **IN PROGRESS — the earlier Windows Test2 MSI is provisional and has not passed this review**. The user explicitly requested this review before rebuilding. Do not describe source checks, the old CPU smoke or the old green commit as UI acceptance.

Latest decision: the user approved a practical UI refresh after seeing the findings. `ui-refresh-execution-plan.md` now governs the next frontend work. Preserve the fixes below while refreshing the shell and workflows; a new installer waits for post-refresh review/regression.

Latest verified retests: UI-011 through UI-016 pass their browser checks. New Project starter Workspace identity is stable before any layout save; registration enables Library upload without reload; Sessions shows actual Task/mode; theme Overview shows its saved name; Project Follow/Tasks exclude global Tasks. Additional corrected findings: dedicated Workspace route was migrated to Projects on every reload, active Project/Workspace context was not restored, docked panels lacked keyboard resizing, daily cadence required numeric weekday text, component icon actions lacked useful accessible names, and scheduled routine fields used the wrong JSON casing. Dedicated route/context and tab count now survive reload; panel width290px and drawer height220px persist after keyboard resizing; daily weekday checkboxes reject no selection; component maximise/restore/Notes docking/Logs Inspector customisation work. Future Monday-only, one-run schedule metadata was created; actual schedule execution was not tested. Task archive/restore works. All33frontend validators passed before the final small casing/context changes; rerun them at the checkpoint. The authenticated native API nil-federation regression passed.

Screenshots saved under `D:\Codex\OnePane\.tools\ui-qa-20261028-01\screenshots`: schedule weekday validation and Workspace Inspector docking. Mouse drag resizing did not produce a measurable change through the emulated browser viewport; this remains unverified, not a demonstrated product failure. No installed native desktop claim is made.

## Test environment and preservation

The Windows Home host has no Windows Sandbox executable, Hyper-V module, VMware/VirtualBox CLI or OnePane service. No disposable installed environment was available. Existing Alpha/data/models/configuration were not installed over or modified. Scratch backend/data/Projects/models and evidence live under `D:\Codex\OnePane\.tools\ui-qa-20261028-01`.

Initial walkthrough used the exact Test2 backend649fffb2 and its embedded assets, reproducing the first-run blocker. Subsequent UI work uses current repository static assets through a loopback-only development gateway to that isolated backend. A separately labelled `dev-ui-review` backend, exported from649fffb2 with the reviewed API fix, now serves the same scratch data. This is development verification, not a final rebuilt MSI or installed native desktop acceptance. The unrelated Ubuntu draft remains untouched.

## Findings and remediation

| ID | Impact | Finding | Current evidence |
| --- | --- | --- | --- |
| UI-001 | Blocking | Language Continue throws because navigation iterates a single selected element | Fixed desktop/mobile collection selectors; real setup now completes; actual navigation decorator executes in regression validator |
| UI-002 | High | Agents Profiles and model pin/tier/component actions repeat the same selector mistake | Five further selectors corrected; Agents renders; all UI scripts checked for this invalid collection pattern; installed Colibri actions still need hardware/runtime QA |
| UI-003 | High | Disabled federation is stored as a typed nil interface; Nodes requests panic | Setter normalises nil pointer; authenticated HTTP regression passes; review backend Nodes now returns503 unavailable without panic |
| UI-004 | High | Dialogs leave focus behind the overlay, lack Escape and Tab containment | Named dialog/close control, initial focus, Tab wrapping, Escape and opener-focus restoration verified in browser |
| UI-005 | High | Workspace selector flex-shrinks to16px and clips its text | Selector is36px after fix; observed desktop layout |
| UI-006 | High | Models async responses bind controls after navigating away | Detached-root checks added to all four model renderers and external discovery completion; rapid Local/Cloud/Routing/Discover/Agents navigation retested without new console errors |
| UI-007 | High | Late desktop docking CSS overrides phone grid/Inspector; mobile navigation stays hidden after resizing | Phone grid/overlay rules and nav visibility corrected;390x844 now shows page and bottom navigation with no document horizontal overflow; more breakpoint checks remain |
| UI-008 | High | Custom theme builder drops RGBA alpha, turning soft highlight into the same colour as selected text | Alpha composited against panel; executable helper regressions pass; new saved custom theme has distinct foreground/background and readable Logs label |
| UI-009 | Medium | Narrow top command button wraps beyond its height | Single-line clipping/ellipsis applied; desktop screenshot shows text contained |
| UI-010 | Medium | Logs drawer retains initial unavailable state after feeds refresh | Open drawer refreshes after operational fetch; actual event rows now appear |
| UI-011 | Medium | Cloud page says OmniRoute is installed/stopped while its card says Not installed | Conditional installation-state guidance corrected; final browser retest pending |
| UI-012 | High | Register Workspace refreshes only Connections, leaving Library/Tasks/environment controls disabled | Owning Workspace view now refreshes; test another fresh registration without reload before closing |
| UI-013 | Medium | Agents Sessions reads task_id/execution_mode while the API supplies id/mode | Corrected fallbacks and explicit empty state; browser retest pending |
| UI-014 | Medium | New Project's default dashboard Workspace exists only in memory and changes identity after reload | Initial dashboard layout now included in Project creation policy; new-Project reload test pending |
| UI-015 | Medium | Settings Overview exposes raw custom theme ID | Theme package display name used; browser retest pending |
| UI-016 | High | Project Dashboard Tasks/Follow include unassigned global Tasks | Strict Project filter shared by task widgets; global/other-Project exclusion regression passes; browser retest pending |

## Actual walkthrough coverage

- First-run language/admin setup: completed with disposable local QA credentials after UI-001 remediation; onboarding tour displayed and skipped.
- Operations: Overview/Activity/Health/Recovery rendered. Node feed correctly remains unavailable in this intentionally federation-disabled configuration. No general recovery/no-effects acceptance is inferred.
- Tasks: created a disposable global Task, observed scheduler transition to blocked without an eligible model. No model/agent execution claim. Scheduled-task editor/archiving and inspection remain to exercise.
- Projects/Workspaces: created Project with automatic execution disabled, created second Workspace, registered the first, switched Development/Dashboard, changed layout size, saved and reloaded. Workspace list and Notes value survive reload. Unsaved starter identity finding remains tracked separately.
- Library: uploaded a non-sensitive text fixture, indexed it, searched a literal phrase in the exact document and across the Workspace. Actual text, version, line/byte and digest citations returned. DOCX/PDF/Council delivery not exercised in this pass.
- Models: hardware detection, Local/Cloud/Routing/Discover rendered. Routing editor opened; no external credentials, provider probe, installation or cloud spending was enabled in this pass.
- Agents: Profiles, Teams, Sessions and Councils visited; no Team/Council execution proven. Skills Installed and Secrets empty credential forms inspected without entering real secrets.
- Settings: every category visited read-only, except tested Light/Dark selection and a saved custom theme/reload. No security policy, credentials or Node permissions changed.
- Web Chat and OnePane Chat: manual provider/scratchpad and no-eligible-model states inspected. No external provider site opened or prompt transmitted.
- Themes and responsive: Light and Dark visually inspected; custom theme seed/save/reload tested;1280x800,800x700 and390x844 examined. Shell toggles and more keyboard/resizing checks remain.

## Exact continuation

Finish the pending browser retests above. Exercise Skills subviews, Team template creation/configuration, scheduled-task editor, inspector add/maximise/dock controls and panel resizing. Retest responsive layout and light/custom themes after final changes. Save screenshots and a defect disposition record. Run the relevant frontend validators and native API tests, then commit only reviewed UI/API/docs files on the existing branch and inspect exact-source CI. Rebuild a new Windows test MSI only after this review closes and post-review regression passes. Preserve the old checksummed MSI as provisional evidence.

Physical sandbox execution is not available here (no compatible rootless Podman/Docker environment). Installed clean/upgrade/native desktop acceptance, actual external providers, GPU/Colibri/Node and full agent/Council execution remain separate outstanding acceptance requirements; do not mark them passed from UI navigation.
