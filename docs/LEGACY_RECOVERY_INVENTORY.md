# Legacy source and artifact recovery — OnePane
**Revision:** 2026-10-09; implemented on the feature branch, not on published RC-10.

## Preservation guarantee
Historical Alpha 2 and Alpha 3 source patches are retained under `.recovery/` and immutable Git history. They are **not runtime assets** and must not be replayed over newer files or installed blindly. Existing Project files, model pools, SQLite migrations and content-addressed artifact blobs are durable user data, not "stale code." No cleanup may delete or relocate them without an auditable migration/retention decision and user confirmation.

## Source recovery status

| Historical source / capability | Current integration | Decision |
|---|---|---|
| `.recovery/alpha3.1/orchestration-foundations.patch` → `internal/featurepolicy` | Restored pure policy types, no-expansion resolution, capability defaults and unit tests | **Recovered**. Wire effective policy into Project and Workspace execution gates before exposing custom policy editing. |
| Same patch → `internal/agentrole` | Restored agent definition/assignment/session model and unit tests | **Recovered foundation**. Existing agentprofile/agentworker identities remain authoritative until versioned, tested migration. |
| Same patch → `internal/deploymentcap` | Restored capability/provenance/context-limit filter and tests | **Recovered foundation**. Need adapters from real model admission/spec sheets; must not replace empirical Agent Check with claims. |
| `.recovery/alpha3.1/council-runtime.patch` | Existing live Team Worker/Council and Research mode code is newer | Compare independent-seat restrictions and provenance; **do not replay** old patch over current code. |
| `.recovery/alpha3.1/follow-snapshots.patch` | Inspector and Follow view remain in current frontend | Keep as historical design input; consolidate under canonical Inspector; add actual event-source and tab lifecycle tests. |
| `.recovery/alpha3.1/stabilization.patch` | Current shell/popover code supersedes patched code | Treat as historical regression examples; do not reintroduce older event handlers. |
| `.recovery/alpha3.1/uninstall-lifecycle.patch` | Native WiX MSI packaging supersedes legacy EXE lifecycle | Only retain data-preservation requirements; keep MSI repair/uninstall tests rather than executing old setup code. |
| `.recovery/alpha3/source-patch/part-000..045.patch` | Many overlapping Alpha 3 UI/feature snapshots; 46 chunks | Maintain source provenance in Git and classify per feature during renderer consolidation. No blind re-application. |
| `.recovery/alpha2-buildsrc-current.b64`, `alpha3-base-compat.b64`, `alpha3/source.patch.xz.b64` | Encoded historical source bundles | Preserve Git history; never decode or execute automatically at runtime. |
| `internal/teamworker/service.go.orig` | Noncompiled source backup duplicated live Team worker | **Removed from feature branch**, preserved through Git. No runtime feature removed. |
| Earlier Project/Workspace data and Artifact blobs | Existing managed artifact store and Project Library adoption API | **Recoverable in place**: `POST /v1/projects/{projectID}/library/import-managed` verifies exact managed Artifact ID, project/tenancy scope and blob integrity; no copy, no move, no delete. |

## Canonical active implementations

- Project ownership and per-Workspace runtime: `internal/projectworkspace/`, migrations 0036/0037, `internal/projectruntime/`, `internal/sandboxrunner/`.
- Agent/team execution: `internal/agentworker/`, `internal/agentprofile/`, `internal/teamworker/`, `internal/projectorchestrator/`.
- Hardware/model lifecycle: `internal/localai/`, `internal/scheduler/`, Colibri and managed OmniRoute components.
- Library/versioned artifacts: `internal/artifact/`, `internal/projectworkspace/library.go`, `internal/projectworkspace/workspace_links.go`.
- Canonical Development screen: `internal/webui/static/project-development-page.js` with explicit mount functions for Workspace Library, collaboration and runtime controls. Preserve dashboard on a secondary tab.
- Storage: configured OnePane data root, model pool and Project root; never overwrite/delete previous paths during ordinary upgrade or repair.

## Stale code approach
1. Recover unique historical policies and algorithms with unit tests, and map recovered concepts to existing data models.
2. Promote one canonical implementation per current subsystem; add behaviour tests before removing shadowed function definitions.
3. Prevent **new** duplicate global function declarations through CI; classify older duplicates and cut over module by module.
4. Keep all migrations and snapshots until migration and rollback tests establish no user-data loss.
5. Remove backups and old build metadata only if proven not referenced by active CI, schema migration, bootstrap, restore, or runtime.
6. Follow with a controlled Git-history-preserving cleanup commit, never an unreviewed bulk-delete.

## Pending QA
- Manual comparison of old Alpha 3 features against current active pages: Library, model routing, council, research, Inspector, command palette, settings, nodes, Project Orchestrator.
- Controlled migration of existing legacy projects and model registrations from ProgramData to custom MSI data root.
- Real Windows and Ubuntu sandbox provisioning, installation and upgraded-release testing.
- Ensure recovered policy and agent-role definitions are integrated at runtime, not simply compiled library packages.
