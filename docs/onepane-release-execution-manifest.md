# OnePane release execution manifest

Updated: 2026-10-23. Release state: **IN DEVELOPMENT — NOT FINAL BUILD COMPLETE**.

## Authority and continuation

The Git repository is `D:\Codex\OnePane\source`; its parent is an unrelated unborn wrapper repository. Continue `codex/team-live-ci-completion`, existing draft PR [228](https://github.com/DigiLogicTech/OnePane/pull/228), based on `feature/rc11-policy-worker-diagnostics-integration-20261011` (`fb7f5df7`). Preserve the stacked integration route; do not merge the historical feature PRs independently or reset local work.

Approved product decisions live in `rc11-vision-decision-register.md`; Team additions in `rc11-live-team-completion.md`; implementation/review history in `rc11-integrated-code-review.md` and `rc11-second-review-gap-matrix.md`. This manifest indexes remaining gates; it does not silently narrow their requirements. Default autonomous Projects remain L4, with explicit L5 consent and governed Team plan acceptance. Reconcile every open acceptance item before declaring a release.

Recovered checkpoint: published `bb840b91b1f4fabc7bd810187ad64feb3aab7112`; local commits `95fb4470` (bounded concurrent Team inference, additive criteria, draft adoption and human guidance) and `28fd4a52` (scoped cancellation). Recovered uncommitted Team pause/resume implementation is being completed and validated, not discarded. The automation memory did not exist at the start of this run.

## Execution queue

| ID | Status | Work and exit evidence | Dependencies / exact next action |
| --- | --- | --- | --- |
| TEAM-HOLD | Source validated; hosted/physical acceptance pending | Atomic accepted-Team pause/resume, original Attempt retention, draining observation, authority and descendant dispatch fences; backend/controller/migration regressions | Publish to existing PR and inspect exact-head CI; physical effects and final browser review remain open |
| TEAM-CANCEL | Source implemented; acceptance open | Scoped atomic cancellation intent and proposal discard (`28fd4a52`); complete cancellation/compensation with unknown effects reconciled | Exercise ordinary cancellation owner with descendants and real tools; never equate request with process termination |
| TEAM-COLLAB | Partial source | Bounded two-member concurrency and constrained queueing, agent draft adoption, criteria and guidance (`95fb4470`); dynamic help/reassignment, artifact navigation and broader fairness remain open | Implement accepted-graph lifecycle safely without weakening immutable authority; keep physical multi-agent acceptance separate |
| SCOPE-AUDIT | Open | Trace approved Alpha/RC requirements to code, tests and missing acceptance across core AI, autonomy, application and infrastructure | Reconcile whole branch, open issues and integration manifests; do not treat old gap descriptions as current defects |
| L4-E2E | Open blocking acceptance | Real local objective → authorized Workspace Tasks → tools/build/tests → verified Library deliverable, restart and resource-wait recovery, no unintended cloud or duplicate effects | Complete functional gaps from scope audit; capture real CPU-only evidence |
| AI-LIFECYCLE | Open blocking acceptance | Real model download/install/verify/remove; CPU/GPU inference; Colibri and OmniRoute; queuing, swap, memory reclamation, fallback and provider health | Follow physical lifecycle protocol; fixtures/compilation cannot close gate |
| NODE-SANDBOX | Open blocking acceptance | Trusted paired transport, isolated rootless Workspaces, grants/revocation and runtime/model management | Approved physical machines and runtimes; service-health Task adapter remains disabled pending acceptance/review |
| L5-DIAGNOSTICS | Open | Consented observe/detect/respond/independent verification, pause/revocation and unknown outcomes; real scoped redacted failure report | Existing bounded hosted simulations are source evidence only |
| DATA-SECURITY | Open | Permissions, secrets, persistence, migration, backup/restore, sandbox/project isolation and safe upgrades | Final integrated code/security review and affected negative tests |
| FINAL-UI | Open blocking acceptance | Page-by-page functional, responsive, accessible, theme-aware Windows/Ubuntu review; layout, docking, dialogs and recovery remediation | After core integration; changed Team controls have controller tests only. Prior browser approval-review failure remains unresolved; do not bypass it |
| EXACT-CI | Pending final source | All applicable mandatory workflows pass against exact release commit; inspect job results/skips, distinguish superseded runs | Publish validated source to existing PR; reconcile stacked PRs and required workflow coverage |
| WINDOWS-INSTALL | Open blocking acceptance | Versioned x64 MSI, clean install, populated Alpha/RC upgrade, chosen install/data drive, service/reboot, rollback, uninstall/reinstall preserving models/Projects/credentials | Final reviews; reviewed-SHA/protected-environment workflow configuration; record existing roots/backups before installed QA |
| UBUNTU-INSTALL | Open blocking acceptance | Versioned amd64 artifact; clean install, upgrade, restart, rollback and preserved data | Windows-first sequence; Ubuntu host and applicable installed QA |
| RELEASE | Blocked by preceding gates | Final version/commit, artifacts, SHA-256, provenance, notes and final regression; explicit manual acceptance complete | No installer generated or final release claimed in this checkpoint |

## Evidence register

- GitHub inspected this run: PR 228 remains open/draft/unmerged, published head `bb840b91`, base `fb7f5df7`. There are 27 returned open PRs, including the stacked historical release branches; presence alone does not authorize merging them.
- Published-head integration workflow [39913940725](https://github.com/DigiLogicTech/OnePane/actions/runs/39913940725) completed successfully. QA guard workflow [39913940776](https://github.com/DigiLogicTech/OnePane/actions/runs/39913940776) completed successfully. These are not results for the newer local source and do not establish a packaged MSI or physical acceptance.
- Current run: all 25 JavaScript validators pass; module verification, affected-package `go vet`, JavaScript syntax, negative QA packaging guard and whitespace checks pass.
- Windows `go test -tags integration -count=1 -timeout 5m` passes for Team, Team worker, Task, Agent Worker, Tool Gateway, API, inference, Project orchestrator, SQLite, web assets, runtime coordinator and scheduler. Final focused `TestTeamPause|TestTeamResume` checks pass after adding deep/oversized/foreign ancestry, stale/revoked API and reconstructed-Worker continuation cases. These use disposable SQLite and synthetic transports; they do not prove physical inference or installed-app restart.
- Local development toolchain: checksum-verified portable Go 1.23.12 at `D:\Codex\toolchains\go\bin`; no installed OnePane service, user configuration, runtime or model was changed by its preparation.
- Existing native Windows sandbox sweep limitations (Unix permissions / Python Store stub) remain classified separately from affected regression results.

## Defects and disposition

| ID | Severity | Finding | Disposition / evidence |
| --- | --- | --- | --- |
| HOLD-001 | Blocking authority defect | Task hold ancestry lookup stopped at depth 32 and could miss a held root above deeply delegated work | Remediated in current source using cycle-deduplicated traversal with the 256-Task control bound; foreign/truncated ancestry fails closed. Deep/oversized/foreign regressions pass |
| HOLD-002 | Blocking validation gap | Recovered pause/resume source lacked Worker-boundary drain/resume evidence | Source gap closed: queued/returned reasoning, no premature checkpoint, drain/resume and original Worker/Attempt pass, including reconstructed service. Physical effects/restart acceptance remains open |

## Recovery rules

Record commit and exact test/workflow outcomes here after validation. Keep installed QA outstanding until performed. Never reset the active checkout or package `.recovery` payloads as current-source release evidence. If a run ends, resume the first eligible queue row and retained work rather than rebuilding the assessment from scratch.
