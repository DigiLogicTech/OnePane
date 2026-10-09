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

## Local-first autonomous execution contract (2026-10-09 amendment)

### Intent
A slow local machine remains a useful autonomous worker. OnePane must not turn a
predicted low token rate, small GPU, or CPU-only placement into an arbitrary
**ineligibility** rule. If a compatible, empirically qualified local model and
approved sandbox tools can complete a step, it should be schedulable. However,
OnePane cannot guarantee that an impossible memory fit, unsupported toolchain,
insufficient model capability or unbounded task will succeed merely by waiting.

### Compute/runtime responsibility
- **llama.cpp:** choose supported CPU, GPU or partial GPU offload placement;
  favour supported hardware without depending on CUDA. A failed GPU
  initialization may trigger a *declared placement change* to CPU only after
  validating sufficient host RAM and preserving model identity, with Agent Check
  and admission revalidation where required.
- **Colibri:** separate runtime with SSD/RAM/VRAM expert tiering and hot swap.
  OnePane manages cold/warm/hot residency, queues admission under pressure,
  and verifies that model processes release resources on exit. Disk streaming
  may be very slow; SSD expert caching does not imply unlimited RAM capacity.
- **llmfit:** advisory hardware/model compatibility and expected throughput.
  It cannot authorise execution or make a tested CPU-compatible model
  unschedulable based only on low speed estimates.
- **OmniRoute:** managed optional provider/proxy routing component, separate from
  the scheduler, Project Orchestrator and local model runtime engines. An
  OmniRoute provider is only a candidate when healthy, properly onboarded,
  qualified, policy-permitted, and cost/data-residency rules permit it; it must
  not circumvent local-first or Research model pinning.
- **Nodes:** an additional *explicitly permitted* trusted Node can provide
  local/federated compute. The origin device can continue work on CPU when
  other Nodes are unavailable; remote/cloud eligibility is a policy decision.
- **Sandboxes:** actual code, tools, tests and game engines execute inside a
  verified isolated Project Workspace environment, not in the model process or
  the OnePane Windows installer.

### Execution policy profiles
| Policy | Intended default | Route change | Failure behaviour |
|---|---|---|---|
| Standard/Best effort | Local-first, zero incremental spend, compatible CPU/partial offload accepted | Bounded retries/replans, qualified alternative model if needed, only within grants | Resource wait or checkpoint/re-plan, explain blockers |
| Strict Local | No cloud/remote reasoning | CPU/GPU placement may change without model substitution, subject to requalification | Queue until local capacity or report true incompatibility |
| Research Integrity | Exact configured provider/model/version/seat; no fallback or substitution | Same-model retries only; independent evidence scope | Preserve failed seat and provenance; pause/manual retry |
| Operator-authorised external | Permit specific trusted Nodes, provider plans or budgeted paid routes | Only within explicit consent, cost quota and data policy | Pause when rate-limited/budget or credentials exhausted |

Research Council execution does **not** inherit permissive Best-effort
substitutions; its routing and artifact/evidence boundaries are separate.

### Long-running task lifecycle
1. Decompose an objective into steps with prerequisite dependencies, scoped
   capabilities, inputs/outputs and verifiable completion criteria.
2. Select the smallest **adequately qualified** local model that can handle
   each step. Large capacity/throughput is an optimisation, not a blanket gate.
3. Fit the actual runtime into memory using a conservative resource budget and
   verified model placement. Avoid starting two memory-heavy runtimes at once
   if doing so exceeds free RAM/VRAM; cold-load on demand.
4. For insufficient context, reduce/partition **non-authoritative** history
   through bounded context compaction and evidence-backed summaries; never
   silently truncate instructions, immutable evidence, Project authority,
   pinned Research inputs or exact agent protocol. Re-evaluate candidate
   capability/context after compaction.
5. Queue with explicit `waiting_resources`, `waiting_model`,
   `waiting_external`, or `waiting_approval` semantics and actionable
   reasons. Sleeping or a busy GPU must not consume the full retry budget.
6. Persist checkpoints, step artifacts, source hashes, sandbox state, model
   identity, cumulative resource usage, and unfinished dependencies before
   suspension. Resume safely after app/service/Node restart without reissuing
   unsafe mutations.
7. Use bounded retries with exponential/backpressure delays; distinguish
   resource contention, timeout, model failure, unsafe request and true
   incompatibility. Never spin indefinitely or silently pay for cloud usage.
8. Finish only when verification matches the Task completion contract;
   impossible resource/capability combinations remain a **clear blocker**.

### Findings from RC-10 source review
- `internal/localai/recommend.go` supports GPU, mixed CPU/GPU and CPU-only
  placement. The estimator supplies *advisory* TPS estimates; those must not
  become execution requirements.
- `internal/localai/colibri_tiering.go` and `colibri_hotswap.go` support
  persistence and residency control, but cross-runtime GPU accounting,
  safe concurrent admission and low-memory end-to-end QA remain outstanding.
- `internal/localai/components_omniroute.go` manages OmniRoute 3.8.51;
  `internal/provideronboarding/omniroute.go` separately exposes a provider
  connection. The conceptual distinction is intentional.
- `internal/scheduler/types.go` has `AllowLimited`, local preference,
  compute preferences and cost gates; `internal/scheduler/catalog.go` still
  requires accepted/restricted model-spec admission for managed deployments.
- RC-10's ordinary `agentworker.defaultRoutePolicy()` omitted `AllowLimited`
  (default false), unnecessarily excluding qualified restricted models. The
  feature branch now sets it true for ordinary autonomous Tasks, while leaving
  `AllowUntested`, paid routing and subscription usage disabled by default.
- `internal/agentworker/execution.go` currently blocks when no candidate
  meets the current context size; it does not yet checkpoint/wait or recompile
  scoped context for a smaller model. Dispatch failures exclude candidates
  and escalate boundedly; no unconditional guarantee of future completion.
- `internal/projectorchestrator/service.go` creates Tasks from operator
  intent, but is not yet a general autonomous environment-provisioning and
  experiment-planning agent.
- Executable runtime admission continues to depend on the actual
  model/interface protocol and approved Workspace tools. A slow local L0
  model alone cannot satisfy an L1/L2 Task contract without a trusted,
  separately qualified adapter.

### Acceptance scenarios
1. On a CPU-only machine with enough RAM for a qualified small GGUF model,
   OnePane can run a governed multi-step code Task without NVIDIA drivers or
   cloud credentials. Low TPS is shown, not interpreted as failure.
2. On a 6 GB VRAM GPU and 16 GB system RAM, a compatible model can prefer CUDA,
   use **verified** partial offload/CPU when needed, and release RAM/VRAM at the
   end of Agent Check and task execution.
3. If a bigger model cannot fit in RAM even with offload, OnePane identifies a
   different qualified small model for Best-effort mode if permitted; Research
   mode retains the pinned model and holds the seat rather than substituting.
4. The Task queue survives restart, GPU contention, external rate-limits and
   temporary Node loss; progress resumes from safe checkpoints.
5. A Project's Research and Godot Development Workspaces execute distinct
   toolchains in their respective sandbox environments with scoped files,
   without automatically installing anything on the Windows host.
6. An operator's paid provider, subscriptions, Vault secrets or Project Library
   documents are not silently used to overcome local hardware constraints.

## Implemented cross-Workspace artifact exchange (feature branch, 2026-10-09)

This section describes **code staged on the feature branch**, not a shipped RC-10 feature.

- `migrations/0036_workspace_artifact_links.sql`: directional, explicitly enabled/revocable, same-Project `project_workspace_links` and hash-bound immutable `project_workspace_publications`. Two independent Workspaces can exchange a chosen artifact without sharing writable filesystem roots or secrets.
- `internal/projectworkspace/workspace_links.go`: validates project/Workspace relationships, active source read+derivative grants, latest versus pinned version policies, revision-safe enable/disable and durable event provenance. A target Workspace may retrieve **only the exact published asset version** while the link remains enabled. Publication does not transitively grant another Workspace authority to re-publish.
- `internal/projectworkspace/library.go`: Project Library assets and versions, per-Workspace direct grants and revocations, immutable resolution of pinned content, project-membership checks and separate read/access policies.
- `internal/api/project_library.go`: streamed, bounded multipart uploads (initial 32 MiB limit) through the existing content-addressed ArtifactStore; downloads verify the hash and size and are always treated as attachments. Uploads are **untrusted content**, never sandbox-executable simply because a filename claims so.
- `internal/api/workspace_links.go`: Project-authorised link and publication endpoints, and explicit reconciliation of legacy dashboard Workspace identifiers to canonical backend Workspace IDs.
- Frontend: a top-level `Library` navigation route, Project Library and version/grant controls, plus Development-view inbound/outbound Workspace connections. Users can disable links and publish source versions via a Library dropdown. Existing Dashboard remains secondary.
- CI: unit, migration/integration and static UI-contract tests cover source grants, three Workspace chain non-transitivity (Research → World → Story), enable/revoke, version/hash pinning, Source-target ownership, and existing sandbox tests.

This delivers **controlled artifact/data interchange**, not live sandbox networking or a running game engine. Shared services, live API connections, Git patch promotion, toolchain installation and engine/IDE execution must each have separate explicit policy, traffic/content mediation and tests. The legacy runtime remains one *shared Project* runtime in RC-10; Workspace runtime isolation and a safe migration are still outstanding.

### Remaining release blockers
1. Canonical per-Workspace sandbox runtime ownership with independent stop/rebuild and isolated storage/network.
2. Per-Workspace image/toolchain install (version locked), reproducible builds, governed terminal and restricted preview/GUI access on compatible Nodes.
3. Agent execution of code-generation, build/test and publishing workflows with approvals, scoped Skills/tools, durable state and evidence.
4. Real functional WebUI/Windows testing of all Library and connection controls, including dark/light/Graphite/Midnight and Inspector/drawer geometry.
5. Library import/version updates, file preview/indexing/search, size-quota/retention and explicit global cross-Project tenancy (distinct from Project Library).
6. Safe existing EXE→MSI and legacy Project/Workspace data migration; no data deletion or host-wide silent installs.

**Do not merge or publish based on source tests alone.** The acceptance demonstration is a Game Development Project with separate executing World, Story and Art Workspaces, a versioned asset moving between approved Workspaces and independent sandbox lifecycle verification.
