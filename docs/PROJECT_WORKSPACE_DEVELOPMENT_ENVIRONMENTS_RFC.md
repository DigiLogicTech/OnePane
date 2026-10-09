# OnePane — Project-first development environments (RFC)

Status: **Implementation direction / Phase 0–1 integration**, 2026-10-09. Baseline: RC-10 `a5b22934a9c25adc6e1c891c2962ce89016e526b`.

## Product correction

OnePane is a local-first, multi-agent **AI development and research environment**, governed by an operations control plane. The purpose of a Project/Workspace is to create, execute, test and preserve work in constrained environments. Dashboard widgets describe the work; **they are not the work**.

**Project**: durable unit of ownership: objective, members and agents, Library and code/assets, version/revisions, budget, Node policies, project-wide Skills/tool packs, Workspaces, builds/results, change review and provenance. A Project persists when every Sandbox stops, moves or is rebuilt.

**Workspace**: durable, named activity boundary inside a Project: an isolated development/research environment with its own toolchain(s), source branch/worktree or content snapshot, models/agents and routing, explicit Library grants, Tasks/Routines, terminal/execution results, preview endpoints and state. Research mode isolates first-pass agent context. A Workspace may own multiple SandboxInstances (e.g. dev + test) with a clear primary environment.

**Environment Template**: immutable-ish/lockable recipe describing engine/toolchain, base OCI image or VM, CPU/RAM/GPU requirements, runtime deps, version lockfiles, network/evidence/security policy, mount policy and reproducible provisioning steps. Store the digest, not only a mutable `:latest` tag. Templates are not executable permissions.

**SandboxInstance**: concrete, observed process/container/VM, linked to exactly one Workspace (and through it a Project and security tenancy). Never equate a requested `running` state with verified execution. Individual Workspace instances may share read-only Project sources and explicitly granted Library assets, never their writable roots, secrets or sockets by default.

**Application/Tool Installation**: governed, auditable creation of an environment layer or app instance, using approved image, versioned package registry, project Artifact or Git source. Installation runs inside the sandbox or trusted image builder, not on OnePane's Windows host. Mutable package install has a lockfile/snapshot; rollback and disk cleanup are expected.

## Example: game theory and game development

Project: **Game Theory & Development**

- Workspace **Theory Research**: Python/statistical computing, notebooks, local LLM, approved academic papers, simulation data and provenance. No game engine required.
- Workspace **Game Development**: source repository, pinned Godot toolchain image, engine CLI/editor where supported, asset pipeline, build/test commands and accessible preview. Distinguish headless engine operations from GUI editors; a remote graphical session/stream requires additional host support and explicit GPU/display policy.
- Workspace **AI Simulation**: reinforcement/self-play runs, constrained experiment plans, isolated first passes and model provenance.
- Workspace **Testing & Build**: clean locked build image, repeatable exports, test results and approved release artifacts.

Project Library can hold shared reference docs or assets. Distinct Workspace file trees and toolchains are not automatically shared. Changes are proposed/reviewed and merged deliberately.

## Verified RC-10 gap

- `migrations/0004_project_workspaces.sql` defines `project_runtimes.project_id TEXT NOT NULL UNIQUE`: **one runtime per Project**; `project_applications` are linked to that single runtime.
- `migrations/0022_alpha3_control_plane.sql` defines `project_workspaces` with UI/layout/AI/resource state, but no owned execution runtime foreign key.
- `internal/projectworkspace/views.go` labels Project Workspace UI as distinct from legacy tenancy Workspace.
- `internal/webui/static/project-workspace-page.js` primarily renders draggable tiles and layout presets. The Sandbox context chip derives from a workspace JSON flag, not an observed runtime.
- `internal/sandboxrunner` has guarded OCI rootless CLI operations and verified application execution; **this engine is real**, but lacks per-Workspace lifecycle ownership and a complete end-user installer/IDE experience.
- `/v1/projects/{projectID}/runtime`, `/v1/project-runtimes/{runtimeID}/applications`, desired-state endpoints are already present but Project-scoped.
- Rootless Podman/Docker backend is a Linux-style runtime. A Windows MSI alone does not provide Linux OCI isolation or a GUI renderer. Support Linux Nodes and Windows WSL2/Hyper-V or remote runtimes explicitly. Never silently execute installed tools on the host.
- Current Library is schema-first; full document ingestion, governed source grants and project-context search need implementation.

## Required domain model migration (phase 2, not yet implemented)

1. Add a `project_workspace_environments` table (`id`, `project_workspace_id`, `template_id`, `kind`, `requested_state`, `observed_state`, `node_id`, `revision`, `created_by`, timestamps); unique active primary environment per Workspace; permit auxiliary test/build environments.
2. Remove `UNIQUE(project_id)` from the **new canonical** runtime ownership; instead reference an environment_id or a Workspace id and enforce isolation at the service and database layer. Existing Project runtimes are preserved as **legacy shared Project environments** until explicitly migrated. Avoid destructive SQLite table rebuilds without FK, rollback and data-preservation tests.
3. Bind app/tool install definitions, writable source volumes, image snapshots, endpoints, tool permission leases and observed usage to the environment instance. Node placement must respect capability, storage/GPU and tenancy policy.
4. Extend REST under `/v1/projects/{projectID}/workspaces/{workspaceID}/environments` for list/create/provision/observe/stop/clone/snapshot/rebuild and app install, plus `/v1/.../files`, `/v1/.../terminal-sessions` and `/v1/.../previews`, with server-side Project/Workspace ownership and authorization checks on **every** operation.
5. Existing `/v1/projects/{projectID}/runtime` remains read-only/legacy or is explicitly migrated, not aliased silently to a Workspace. Respect old Project ids for automation and data.
6. Every mutating operation flows through Task/Policy/CapabilityLease/OperationCoordinator; do not let LLM prompts, templates or Git files escalate host/network/device/secrets permissions.

## Canonical Workspace interface

The default landing surface should be **Development**, not a movable card board:

1. **Overview / Environment**: exact lifecycle and real resource/Node status, controls for provision/start/stop/rebuild, environment image/version and snapshots, permissions and limits.
2. **Files & Code**: scoped tree, Git branch/worktree, diffs/approved patches, files and versions. Library attachments live in a clearly separate "Reference Knowledge" view and must be granted to agents.
3. **Tools & Apps**: pinned toolchains, engine packages, dependencies, installed applications and install jobs, with compatibility, provenance and environment target.
4. **Execution**: authenticated terminal/task commands, jobs, logs, stdout/stderr, failure detail, approvals and CPU/GPU utilisation.
5. **Preview & Testing**: protected web/graphical preview endpoint where supported, automated test results, build/export artifacts.
6. **Agents & Research**: assigned models/agents, Skills/tool packs, Council rounds, independently scoped evidence and provenance.
7. **Dashboard**: a *secondary optional customisation surface* for widgets, not the primary definition of a Workspace.

Navigation must follow Project → Workspace → Environment. Project list remains the owning place for Project deletion and high-level overview.

## Phase 1 UI honest bridge

Until the per-Workspace runtime migration is tested, display the real existing **shared Project sandbox** status and Project-installed apps in a dedicated Development tab. Clearly identify it as **shared between Workspaces**, avoid showing sandbox ready based on a local UI checkbox, and don't permit arbitrary host installation or claim separate isolation. Keep existing dashboard editing intact under a secondary tab.

## Capability acceptance

- Two Workspaces in one Project can provision distinct environments with different image digests and dependency lists.
- Stopping/rebuilding one does not stop/overwrite the other.
- Approved Project assets can be attached explicitly, and agent first passes cannot read ungranted/peer evidence.
- A pinned Godot CLI image can compile/test a disposable example on a **compatible Linux rootless container node** and produce a hashed build artifact; image acquisition needs an approved/observable job.
- No implicit host mounts, engine sockets, privileged containers, device access or unrestricted egress.
- A Windows client may manage the Linux runtime remotely but does not claim the engine is installed natively on Windows.
- Terminal, file edits, git changes, agent code patches and app installations are attributable, scoped, inspectable, revocable and durable.
- Verify data/model/workspace roots live under chosen disk, survive MSI replacement and honour recovery/cleanup policies.
- QA every page with Inspector/log drawer expanded, all built-in themes/custom theme, keyboard/focus and resized viewport.
- Old Project runtimes migrate or remain accessible without silently assigning one shared runtime to multiple isolated Workspaces.

## Delivery plan

**Stage 0**: Document domain contract, expose read-only actual Project runtime/app states and label legacy shared scope accurately. No false per-Workspace isolation.

**Stage 1**: Canonical Development-first Workspace IA, shared UI primitives and application states, backed by live APIs. Keep existing Dashboard intact.

**Stage 2**: Schema/service refactor for Workspace-environment ownership and safe automatic migration of legacy Project runtimes, with SQL integration tests.

**Stage 3**: Toolchain manifests, OCI image/podman build/verified installation, sandbox command terminal, files/Git, snapshots, previews and Routines.

**Stage 4**: Library/evidence attachments, scoped Research Councils and Project Orchestrator automation, end-to-end game theory/Godot QA on a compatible node.

Release gate: no acceptance of "sandbox working" solely from creating a database record, changing a template, or passing frontend source-smoke. A verified running isolated environment and successful actual code execution are required.
