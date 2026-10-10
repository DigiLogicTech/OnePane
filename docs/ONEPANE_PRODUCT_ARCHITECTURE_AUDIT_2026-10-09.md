# OnePane — Product completeness, File Library and architecture audit
**Date:** 2026-10-09  
**Read-only release baseline:** `v0.1.0-alpha.3.3-rc.10` / `a5b22934a9c25adc6e1c891c2962ce89016e526b`  
**Scope:** Available OnePane project discussion and QA history; current repository schema, Go domains/API, frontend routes, documentation, and CI packaging.  
**Verification note:** Source existence is not proof of operational completeness. A "present" item still requires user-facing integration/real-hardware QA. No production data was accessed, and no physical Windows UI manual acceptance test was performed by this audit.

## 1. Product thesis / non-negotiables

OnePane is DigiLogic's **local-first AI operations and research control plane**. It is not primarily another chatbot. A Project contains Workspaces; Workspaces coordinate model and agent/team execution, scoped tool capability, explicitly granted knowledge, Tasks/Routines, evidence and human approvals, while the OnePane global layer manages hardware, models, providers, Secrets, Nodes, observability and recovery.

Retain these invariant requirements:
- **Authority:** deny by default, workspace-scoped grants and explicit tool bundles; never infer tool/file/secrets rights from an agent's prompt or role.
- **Research integrity:** deterministic model identities; no silent fallback/substitution in Research mode; isolated independent first passes, full provenance, retry-on-same-model, preservation of failed Council seats and operator approval where required.
- **Evidence:** raw prompts/outputs retained with provider, resolved/declaration-model identity, version, source/version/hash, timestamps, retries, result state and human approvals; no falsified certainty about external Web-provider models.
- **Local-first data:** choose a persistent data root independently from the installed application; model and Project roots are independently configurable. No silent loss on update, uninstall or relocation.
- **One experience:** consistent panels, typography, iconography, forms, controls, responsive layout, keyboard interactions, theme tokens, logs drawer, Inspector, and route lifecycle across every page.

## 2. Major gap: File Library and knowledge retrieval (P0)

**Repository foundations exist, but the product-facing Library is missing.**

Present:
- `migrations/0022_alpha3_control_plane.sql` defines `project_library_assets`, immutable `project_library_asset_versions`, and `workspace_library_grants` (including latest/pinned version policy and granular read/modify/derivative/execute/delete permissions).
- `projects.indexing_config_json` already exists in Project creation.
- `internal/artifact` provides content-addressed artifacts/evidence; `internal/projectworkspace/storage.go` handles Project storage relocation.
- `internal/contextcompiler` exposes a bounded structured context compiler, not a complete uploaded-document retrieval/indexing product.

Not evidenced in RC-10:
- Dedicated Library service and `/v1/library` routes; Library navigation/page; upload/import/browse/version-preview UX; ingestion/extraction/index queue; document search/RAG source citing; explicit attach-to-Chat/Task/Council workflows; source access grants applied at retrieval and tool boundaries.
- The README explicitly describes memory/knowledge compilation as a later milestone.

### Canonical Library architecture

1. **Global / Personal Library:** top-level searchable assets, folders/collections, file versions, metadata, import/source provenance, tags and hashes. Do not implicitly make every global file accessible to every Workspace.
2. **Project Library:** assets owned by a Project. Workspaces have explicit `workspace_library_grants` with permissions and pinned/latest version selection. A Project can link assets from the global Library only after deliberate assignment.
3. **Evidence packets:** a single operator-reviewed attachment manifest for Tasks, chats and Research Council rounds: `asset_id`, `version`, `sha256`, `granted_scope`, `excerpt/page/span`, `trust_label` and contextual instructions. Independent Council passes must not see previous seat outputs or unrelated Library content.
4. **Ingestion:** local upload, folders, large-file streaming, import from approved mounted Project directories, optional explicit read-only external connectors. Initial types: PDF, TXT/MD, DOCX, XLSX/CSV, PPTX, code and images (without assuming every image is machine-readable). Report parse/index status and failures.
5. **Retrieval:** keyword search first; embedding/semantic indexing as an opt-in, governed feature backed by a selected local or authorised model. Respect page/sheet/line anchors, metadata filters, token budgets, source citations and version changes; no silent content injection.
6. **Security and lifecycle:** archive/trash and restore, audit every access, refuse executable content by default, content sniffing, quotas, secure storage, duplicate hash recognition, migrations, index rebuild and cleanup, no symlink traversal or path escape, and preserve immutable Council evidence.
7. **Interface:** Library in main navigation; Project Library subpage; add/attach control in Assistant/Orchestrator/Tasks/Council; read-only source preview in Inspector; attached-source provenance in Evidence/Audit.

**Acceptance test:** Upload a PDF and DOCX to Library, grant only one Workspace access to explicit pinned versions, index them, attach two exact excerpts to a Project Council round, confirm citations and content hashes, confirm a different Workspace/independent first-pass seat cannot retrieve other documents, update a source without silently changing pinned evidence, restart/reinstall without data loss.

## 3. Feature-completeness register

| Domain | RC-10 source/QA evidence | Gap or next acceptance condition | Priority |
|---|---|---|---|
| Project → Workspace hierarchy | Project Workspaces, layout persistence, Project storage movement and navigation are implemented | Unify canonical Project page, nested Workspace navigation, Project settings, Library grants, deletion/recovery semantics | P1 |
| OnePane Assistant and Project Orchestrator | `internal/assistant`, `internal/projectorchestrator`, two control-chat tabs and Project selection exist | Scope attachments, orchestrator tool bundles and evidence-backed plans to Tasks/Routines; prevent unsolicited access | P1 |
| File Library and retrieval | Library asset/version/grant SQL only, generic Artifact Store | Build missing service, API, UI, indexing/retrieval, preview, attachments and grants | **P0** |
| Agents, Teams and Councils | Profiles, sessions, Team/Council workflow and Research mode configuration exist | Full multi-round acceptance, retry/rate-limit pause/manual resumption, pinned model/provenance/failed seats, Council context isolation | P1 |
| Skills and scoped tools | `internal/skillcatalog` supports packages, tool bundles and assignments to agent_profile/team/team_member/workspace | Unified visible capability matrix and execution-time effective-rights check; scope research bundles | P1 |
| Local/cloud Models | Managed runtimes, Colibri, llama.cpp, llmfit, routing, discovery and Spec Sheets exist | Scan/adopt existing model weights after reset; resolve CUDA device mapping, Agent Check/admission parity, unloaded-check release, active-transfer cancellation | P1 |
| Nodes and compute federation | Nodes, remote management, hardware telemetry and managed components exist | Verify live remote execution, policy parity, responsive tab layout, local-only management actions | P1 |
| Web Chat | Multi-provider tabs, Chair/manual Council workflow and external browser handoff | Native WebView embed QA failed for all tested providers; preserve reliable external fallback; durable non-Council response history | P1 |
| Operations and Inspector | Operations widgets, bottom drawer, attention state, context Inspector | Single Inspector renderer/registry and drag/close lifecycle, live attention/activity/health/recovery, every route drawer-aware | P1 |
| Evidence / Audit | Artifacts, Observations, Verification, Task provenance and events in backend | Final active frontend router still renders `Evidence / Audit` placeholder; build a working audit/evidence page | **P0** |
| Routines and automation | Scheduler/routine engine and task scheduling backend exist | Expose full Routine management in canonical nav rather than leaving legacy placeholder route | P1 |
| Providers, Secrets and integrations | Vault, provider onboarding/OAuth presets, integration surface and OmniRoute adapter exist | End-to-end external auth tests, provider/model routing parity, credential audit, consistent secrets UX | P2 |
| Themes and Settings | Light/Dark/Graphite/Midnight + additional themes and custom theme editor, redesigned Settings | Shared semantic tokens, standardised controls, contrast and focus states, live preview, duplicate control handling, responsive parity | P1 |
| Installer/storage | First Windows MSI (RC-10), selected data/model/project roots and service registration passed CI | Safe legacy EXE → MSI migration and ProgramData transfer, hard storage caps, backup/retention/preservation QA, Authenticode signing | P1 |
| Package/release quality | Windows/Ubuntu CI, package smoke and source validators | Add true functional UI/regression checks, generated dependency map, dead-code detection, stronger build/release provenance | P1 |

Statuses are **not** binary complete/incomplete: backend existence and green installer tests do not verify the user's end-to-end workflow.

## 4. UI structure and thematic uniformity

**Design hierarchy:** Global shell (Operations, Projects, Library, Models, Agents, Skills, Nodes, Tasks, Evidence, Settings) → Project → Workspace → a contextual Inspector. Web Chat is a distinct governed manual-provider surface, not an alternative ungoverned executor.

Canonical placement:
- **Operations:** system-wide KPI/activity, task and attention queues, recovery.
- **Projects:** Project ownership, Workspaces, Project Library, scoped agents/routing, Project Orchestrator and project-level deletion.
- **Library:** global/Project assets, imports, versioning, search, access grants.
- **Models:** Local, Cloud, Model Routing, Discovery; coherent compute/Agent Check and node target selection.
- **Agents:** Profiles, Teams, Sessions, Research Councils; connect to assigned Skills and effective permission sets.
- **Skills:** Installed, Catalogue, capability packs, assignments.
- **Nodes:** Manage Node tabs and local-only actions.
- **Tasks/Routines:** create, run, schedule, review, resume and inspect; durable state.
- **Evidence/Audit:** event ledger, artifact lineage, council provenance, approvals and verification.
- **Settings:** application preferences only; per-Project or per-Workspace settings remain in their owning page.

Uniform component contract:
- Theme semantic tokens: `surface.default/card/elevated`, `text.primary/muted/inverse`, `border`, `accent`, `interactive.hover/focus/disabled`, `status.success/warning/error`, scrollbar and modal/overlay layers. Never hardcode a provider-specific purple link on a button.
- Consistent button variants and alignment: one primary action per card; secondary/destructive states; right-hand utility actions; identical disabled/loading/hover/focus.
- Shared select/combobox (editable without second textbox), input validation, dialog shell, tabs/draggable inspector tabs, drawers, progress/jobs, confirmations, empty states and error details.
- Single spacing, type scale, icon family, contrast and density; design tokens for all built-in/custom themes, with Light/Dark/Midnight/Graphite checked distinctly.
- Responsive and accessibility acceptance: resized Inspector+log drawer on **every** route, no lost focus/clipped table, keyboard navigation/Escape dismissal, semantic controls and screen-reader labels.
- Per-page automated visual snapshots: Light/Dark/Midnight/Graphite/custom; narrow/wide viewport; drawer closed/open and Inspector closed/open.

## 5. Legacy / stale code findings, with conservative treatment

### Proven
- `internal/webui/static/app-foundation.js` is approximately 288 KB, with 303 function declarations and **30 duplicate function names** in the same file, including renderModels (five declarations), renderActiveView (three), openThemePopover (three) and repeated Settings/Nodes/Workspaces renderers. Additional `app.js` overwrites some global functions after load. This is a significant regression/maintainability risk; do **not** delete by text search alone.
- The active `app.js` router still includes an Evidence/Audit placeholder. The older foundation router also retains placeholders for Routines, Projects and other initial UI concepts.
- `mock` sample nodes/tasks/providers/logs remain in the foundation source. Their continued existence is a safety/quality smell: fake or placeholder data should never reach production decision surfaces. First audit actual call sites and test fallbacks.
- Frontend uses multiple appended CSS/JS layers (including `style.css`, `ui-consistency.css`, page-specific sheets and dozens of browser-global scripts). Global name collisions and accumulated high-specificity overrides need systematic extraction into canonical modules.
- Tracked `.recovery/` contains 65 early-source/recovery files (~1.75 MB); Git history retains their original commits. These are candidates for **archival** only after proving no restore/test/build paths require them.
- `internal/teamworker/service.go.orig` is a 15.7 KB backup file that is not compiled by Go. Removing this isolated stale backup has no effect on active Go package compilation. Preserve original content in Git history.
- There are 65 `scripts/` files (validators, checks and build scripts). They are **not automatically dead code**: source-validation and release gates depend on many of them.
- `docs/manual-web-council.md` says native WebView is not included, but current Windows desktop source includes a provider WebView2 implementation. This documentation needs reconciliation to actual behavior (which RC-8 testing reported broken).
- The published RC-10 release includes an obsolete `setup.exe.sha256` metadata asset without the corresponding EXE; packaging-output hygiene should be updated when the compatibility installer is retired.

### Cleanup order / safeguards

1. **Safe removal**: noncompiled `.orig` backup, after verifying no tests consume it. Stage on dedicated audit branch; no production release.
2. **Static source inventory**: derive definition/call graph and mark each old function as active, overwritten, test-only or truly unreachable. Replace browser globals one slice at a time with named modules/registries; preserve API/DOM compatibility.
3. **Canonical renderer migration**: consolidate `renderActiveView`, Models, Settings, Nodes, Web Chat, project/worskpace and Inspector routing; remove samples/placeholders after equivalent real service-backed views pass end-to-end QA.
4. **CSS tokens/components**: replace overrides with the audited design system. Verify theme and responsive screenshots across pages.
5. **Archival reduction**: prove `.recovery/` is unused by scripts/workflows, retain a Git tag/archive, then remove it from the live tree in a separate reviewed commit.
6. **Tooling hygiene**: classify every validator into active CI, migration-only, archival, or duplicate before any removal. Delete obsolete build artifact checksum outputs, not user data or historical migrations.

**Never bulk-delete historical SQL migrations, recovery paths holding user data, model weights, runtime state, policy/evidence records, or seemingly-unused code without call-path tests.**

## 6. Proposed delivery sequence

### Phase A — Audit and product skeleton (current)
- Freeze RC-10 as a known-good release; add living capability matrix and domain ownership documentation.
- Stage only low-risk backup cleanup; define one canonical component/route registry.
- Add automated checks for duplicate function declarations, all routes reachable, empty-state honesty and theme/drawer matrix.

### Phase B — File Library / Evidence MVP (**highest priority**)
- Library CRUD+versions, secure local ingestion, Project grants, browse/preview/search (keyword first), attach manifests for Assistant/Orchestrator/Council and effective access-denial tests.
- Make Evidence/Audit real: trace Library provenance through Task, Council, Artifact, Verification and human approval.

### Phase C — Agent orchestration / model lifecycle
- Agent capability packs with explicit tool/skill mapping; Orchestrator experiment/task/routine creation; Research Council integrity and multi-round automation; robust model-pool re-adoption and hardware-specific Agent Checks.
- Routine management page, Node consistency and secure remote actions.

### Phase D — UI platform unification / installer maturation
- Canonical theme-aware primitives across all pages, Inspector and drawer geometry, responsive and keyboard QA.
- Library-backed file location/migration, retention/cleanup, EXE→MSI migration and end-to-end installer lifecycle, runtime WebView validation.

## 7. Required release gates

- **Library:** version/grant isolation, indexing/re-indexing, pinned evidence, upload/preview, malicious file/path handling and persistence.
- **Research:** independent seat isolation, no implicit Library history, exact pinned model identity, manual retry and full provenance; rate-limit preservation.
- **Windows/Ubuntu:** configured location, upgrade data preservation, service permissions, uninstall retention, model rediscovery and fallback; never silently overwrite.
- **Frontend:** route smoke, keyboard/visual regression of every page under all themes and Inspector/drawer states.
- **Regression:** source tests and Go tests remain passing; real-device Agent Check and WebView QA cannot be replaced by static validator success.

### Source pointers

- `migrations/0022_alpha3_control_plane.sql` — Library asset versions and grants
- `internal/artifact/`, `internal/projectworkspace/storage.go`, `internal/contextcompiler/` — relevant foundations
- `internal/projectorchestrator/`, `internal/assistant/`, `internal/skillcatalog/` — project and capability models
- `internal/webui/static/app-foundation.js`, `app.js`, `index.html` — renderer and UI layering
- `docs/manual-web-council.md`, `docs/project-workspaces.md`, `README.md` — stated boundaries and documentation drift
- `packaging/windows/msi/`, `.github/workflows/alpha3.1-stabilization.yml` — release lifecycle
