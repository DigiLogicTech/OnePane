# RC11 completion execution status — 23 October 2026

Target: Windows installed QA first, Ubuntu afterwards. This is source progress, not a QA-ready declaration.

## Implemented in this increment

- Live Team conversation and named agent panels on the same durable session; explicit saved guidance and targeted reply requests; no inference caused by viewing or switching. Stable bounded message paging, scoped causal references, newest worker context and additive SQLite upgrade coverage.
- Live reads recheck Team/Task permissions; frozen identities and typed metadata omit private configuration, provider failures and raw tool payloads. Exact worker session/member/turn correlation exposes actual shared-compute state and backend/placement.
- Team uses existing shared model admission. Fixed a coordinator cancellation/admission race that could strand an abandoned lease. Added real SQLite tests for Team dispatch queuing behind other work, independent Nodes and cancellation.
- Windows bootstrap keys use protected NTFS DACLs at creation and repair existing key access without rotating valid bytes. Reject reparse points/hardlinks and preserve invalid contents for operator recovery. POSIX permission validation remains fail-closed. This does not qualify all ancestor directory ACLs or physical power-loss durability.
- A current-source, exact-reviewed-SHA Windows QA builder and manual workflow, with pinned WiX, MSI identity checks, executable/MSI hashes and a provenance manifest. Default historical RC10 MSI authoring remains available. Packaging requires successful exact-source CI, final reviews and operator approval. No installer has been assembled or installed.

## Verification and limitations

Local Windows checks passed for Team, Team worker, API, SQLite, web assets, Node identity, Vault and private-key handling. JavaScript Team controller checks cover view switching, long history, drafts, targets, failed reads, reconnect, disposal and duplicate dispatch. PowerShell/XML/workflow syntax and unapproved-packaging rejection passed. New shared-admission/projection checks are required on the final source head in hosted Windows and Linux CI.

The broad native Windows sweep is not wholly green: rootless sandbox-runner tests require Unix permission semantics and an actual Python interpreter; this PC resolves Python to a Microsoft Store stub. Instrument and workspace-broker fixture tests pass outside the filesystem sandbox. Preserve these distinctions; do not relabel the broad sweep as passed or weaken credential/path checks to obtain a green result.

An isolated browser preview used synthetic records, not model execution. Earlier view/draft/history/target checks passed. Final browser refresh was blocked by automatic approval review because its usage-limit check could not complete. Final changed UI remains unverified in-browser; the block must not be bypassed using another browser mechanism.

## Required before installed QA

1. Complete governed Team assignments/handoffs, resource-admitted concurrency, lifecycle controls and two-agent verified output. Exercise hot swap/Colibri/queuing alongside ordinary chat and Project execution, including constrained capacity, pinned models and cancellation.
2. Reconcile stacked PR requirements against integration; complete whole-product code/security review and remediation, then LAST product UI review.
3. Obtain current-head hosted CI success and review approval; configure the protected QA environment and exact reviewed-SHA variable before manually dispatching the new packaging workflow. A skipped package job is not a built candidate.
4. Record the operator's existing installation/data/model roots and backups. Run populated RC10 upgrade, chosen non-C: root, service/reboot, rollback and preserved credentials/data acceptance on Windows; then Ubuntu.
5. Run separately approved physical rootless, CPU/GPU/Colibri and Node lifecycle cases. Source fixtures do not prove real engine termination, VRAM reclamation or installed service behavior.

The preview server and local toolchain are development tools outside the source tree. The temporary legacy-setup compile payload was removed and must never enter a distributable or commit.
