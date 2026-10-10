# OnePane general-purpose Workspace sandbox (RC11)

## Product principle

A **Project** orchestrates objectives, artifacts, Tasks and selected connectivity. Each
**Workspace** is an independently bounded development and automation environment,
not a game engine, Docker launcher, fixed language runtime or chat transcript.

The user may supply a reviewed OCI image containing almost any compatible Linux
toolchain. The Workspace provides an isolated writable volume, approved rootless
container lifecycle, scheduler / Agent Tasks, scoped capabilities and controlled
publication. Tools can include Python, Node.js, Go, Rust, C/C++, Java, .NET,
R, Julia, SQL clients, Git, testing frameworks, document converters, build systems,
creative and game engines, as long as the binaries and their dependencies actually
exist in the selected image. An image is **not** implicitly granted host peripherals,
GPU access, arbitrary network egress, another Workspace's files, external API keys,
or privileges just because a corresponding CLI is installed.

**This is open-ended compatibility, not a guarantee that all software runs on
every Node or image.** Windows-native GUI software, Unreal Editor and UEFN require
appropriate supported Nodes, dedicated adapters or a separately authorised
remote-control channel; a Linux rootless headless Workspace does not become a
Windows interactive desktop by installing a tool name.

## Existing execution path

- `project.app.exec` remains the general-purpose, authority-governed Task tool.
  It can invoke a selected installed program with program arguments **inside**
  its independently verified running rootless OCI application. Sandbox network,
  resources and mounts are determined by the Workspace runtime, not the agent
  argument list.
- Scoped `project.app.files.inspect/edit/publish`, `project.app.git.inspect/mutate`
  and Project/Workspace Library grants continue to control artifact production,
  evidence and sharing. A compiler returning status 0 does **not** prove output
  integrity or publication.
- Substantial build outputs can opt into `project.app.files.publish` action
  `publish_large`: a fixed, no-follow OCI read plan streams 512 KiB chunks
  into a maximum 32 MiB **in-memory** assembly, verifying each chunk against
  source device/inode/size/mtime/ctime metadata. It compares SHA-256 of the
  full file before and after transfer to the independently recomputed host
  digest, then invokes the **same Task-owned, idempotent, immutable**
  publisher. The original `publish` 256 KiB path stays unchanged; neither
  mode accepts a host filename, transfer command or arbitrary destination.
  The Tool result contains a receipt, never actual file bytes.
- This is a bounded first large-artifact step, not infinite streaming,
  resumable multi-GiB transfers or automatic project-directory packaging.
  File size, number of OCI reads and task deadlines remain constrained.
- `project.app.tools.discover` is a new **read-only inventory** requiring the
  existing `project.app.execute` capability and an independently verified OCI
  mount/identity. The tool runs a fixed POSIX sh script inside the container,
  examines only executable names on the image's absolute PATH, and returns up
  to 256 deduplicated safe names sorted with a coarse category label. No binary
  is invoked, no env/paths/credentials or command output are returned. It has
  a bounded 15-second timeout and 32 KiB output cap.
- The inventory **does not drive a restrictive allowlist**: a tool not recognised
  by OnePane's examples still appears as `other_installed_executable`. Its actual
  execution is subject to separate Task, Tool Gateway, sandbox and resource
  permission checks. A missing POSIX sh in a distroless image is reported
  unavailable; the host is never probed as a fallback.
- No automatic package download, runtime image mutation, remote shell, privileged
  container, engine socket bind, uncontrolled MCP forwarding or silent cloud
  substitution is introduced.

## Specialized integrations are plugins, not the foundation

Godot 4 currently has a fixed-argument, scoped headless build/import adapter
whose physical validation is pending. Unreal Engine 5.8 experimental MCP currently
has **local-container, identity-only readiness probing** and *no* editor Tool
call permission. UEFN remains planned and unconnected. Browser, data science,
creative/design, documentation and other MCP or native application integrations
should use the same capability-bundle model as they mature.

MCP servers must not be automatically trusted or made available to every Agent.
Each connector must enforce explicit authentication where applicable, an
individual Workspace origin, tool-specific authority, resource constraints,
request/result provenance, and denial of uncontrolled cross-Workspace/host access.
A loopback-only unauthenticated MCP endpoint is **never** automatically forwarded.

## RC11 acceptance still outstanding

1. Physical rootless Ubuntu Node with approved pinned OCI image; two real,
   independently provisioned Workspaces sharing no writable mounts or secret grants.
2. Execute a real language/toolchain build or conversion and publish a verified
   immutable artifact to the Project Library; test retry/failure semantics.
3. Confirm Workspace restart/rebuild and toolchain package/image immutability
   without losing data or changing other Workspaces.
4. Controlled per-Workspace network/service sharing with explicit revocation.
5. Prove CPU-only, low-memory operation, appropriate resource waiting, and
   honest reporting when required binaries/compute are unavailable.
6. Validate optional high-end integrations (game engines, desktop apps and MCP)
   **separately**; do not hold basic software development hostage to a single
   engine installation.

The mandatory first code audit, user vision-alignment interview and second
code audit precede release packaging. Green hosted CI does not replace the
skipped physical rootless Node acceptance test.


## RC11 scoped toolchain prerequisite preflight

The existing `project.app.tools.discover` inventory lists installed executable
names, but a truncated inventory cannot establish that an unlisted dependency
is absent. The new `project.app.toolchain.preflight` Tool checks **specific
declared executable requirements** directly in one independently inspected,
running, rootless OCI application. It is registered with the existing
`project.app.execute` Task capability/lease.

A request consists of exact `runtime_id`, `application_id` and an array of
**1–32 unique executable names**. Unknown language/framework names are valid,
subject only to a pathless and shell-safe character check; no predefined engine
or language allowlist is used. OnePane normalises names and computes a stable
requirements digest. The fixed POSIX script looks only for regular executable
files in **absolute OCI PATH directories**, never invokes them, follows no
remote URL, does not report filesystem paths or environment variables, and
does not download missing packages. The host accepts only an exact,
bounded (8 KiB) ordered list of present/absent flags. Reported states are:

- `ready`: every declared executable was observed on this exact running
  sandbox image; **not** a version, dependency or functional qualification
- `missing`: the fixed scanner ran and could not find one or more names;
  does **not** authorise automatic installation or fallback
- `unavailable`: the image cannot run the fixed scanner, or it reports
  failure; the requested binaries are **unknown**, not proven missing

The optional `required_executables` field on existing `project.app.exec`
checks the same requirements **before** the actual Task command. The command
is not launched when prerequisites are missing, unverifiable or malformed;
the Gateway receives a known failure. Existing Task commands without the
opt-in field retain their current behavior. The successful execution receipt
includes the requirements SHA-256 and `toolchain_preflight=ready`, while
output status zero **still does not prove artifact integrity**; separate
independent readback and Task-owned Library publication are required.

**Important scope limit:** Prerequisites are currently declared *per Task
Tool invocation*, not yet an approved, persistent Project/Workspace policy.
A model with ordinary execute permission can omit the optional list; this
does not replace a security grant, a mandatory fleet-wide toolchain policy or
a locked software bill of materials. Persistent toolchain manifests with
image digest, package versions, build lockfile hashes, tracked approvals and
Node/GPU compatibility must be implemented before claiming full managed
provisioning. The same physical, opt-in rootless Python OCI smoke now checks
a real `ready` preflight, a guarded Python command and a denied missing
dependency; it is **not passed** while runner/image prerequisites are skipped.


## RC11 immutable operator-approved Workspace toolchain manifests

Workspaces now support a separate, durable **human-approved desired toolchain
profile**, in addition to the existing inventory and per-Task executable checks.

- The approval references one **canonical active Project Workspace** and a
  registered OCI application already tied to that Workspace's runtime.
  OnePane records the actual registered **sha256-pinned OCI image** and the
  application's revision, never a model-invented image or a floating tag.
- A human operator with the existing `project.write` and `project.run`
  permissions explicitly approves **1–32 unique executable requirements**,
  including optional *declared* version constraints such as `go:>=1.23`.
  Agent/service identities cannot approve on their own. Version constraints
  are for planning only; they are **not currently checked against installed
  package versions** by the presence-only preflight.
- Each approval creates an **append-only SQL revision** with a deterministic
  SHA-256 of Project/Workspace, application ID/revision, image and sorted
  requirement declarations. Database triggers reject changes or deletion
  of historical revisions. Reapproval requires an exact expected revision;
  concurrent/stale updates fail closed. Approval events preserve actor and
  digest provenance, without secret or package output.
- API: `GET/PUT /v1/projects/{projectID}/workspaces/{workspaceID}/toolchain-manifest`
  with canonical Workspace membership and Project permissions.
  **Workspace sandbox** displays the selected approved image, revision,
  current status and required software, and exposes a human approval form.
- Agent execution manifests include the Task's own approved Workspace profile
  as **approved_unverified**; changed application image/revision makes it
  `stale_application_changed`. Cross-Workspace profiles never appear.
  The Agent should use `project.app.toolchain.preflight` or
  `project.app.exec.required_executables` to observe presence before building,
  and use independently verified publication for actual outputs.

**Not yet implemented:** mandatory scheduler enforcement of a saved profile,
version/package/SBOM attestation, operator-authorized offline image builds,
signed runtime provenance, physical rootless Node acceptance and approvals
for shared Workspace artifact transfer. Merely approving software does not
install it, grant a Tool lease, prove GPU compatibility or authorize a
remote MCP endpoint. A Task may still explicitly execute without attaching
`required_executables`; persistence improves orchestration context but
is not presently a security admission policy for all Tasks.


## RC11 Project Orchestrator toolchain admission (2026-10-10)

A named Project Workspace's approved manifest now informs **planning and
Agent-initiated general-purpose sandbox execution**. This connects the
previously separate persisted approval and per-Task preflight features.

- The Project Orchestrator's bounded selected-Workspace snapshot includes a
  scoped `toolchain_readiness` observation with a distinct state:
  `not_approved` (no operator approval),
  `waiting_approval` (approved application image/revision changed),
  `waiting_resources` (application/runtime not persistently observed
  running), or `ready_for_preflight` (the selected application and runtime
  report running, **not** live software/version qualified). It records only
  the approved application, manifest digest, required software and bounded
  status; it does not inspect another Workspace, start a runtime, download
  software or substitute an image.
- Newly created Project Orchestrator Tasks now set the **canonical relational
  `ProjectWorkspaceID`** rather than only a JSON copy of the Workspace ID.
  If there is an approved manifest, the Task's completion context pins the
  exact SHA-256 of that approval. A different approval later requires review
  before the Task's general-purpose OCI execution can proceed.
- The Agent Worker, after checking the Task's canonical Project/Workspace/OCI
  ownership but **before consulting its capability lease**, validates each
  `project.app.exec` proposal against the latest approved manifest.
  If one exists, a stale approval, pinned Task digest mismatch, different OCI
  application or agent-supplied weaker prerequisite set is denied. Otherwise
  the Worker injects **all** approved executable names as
  `required_executables`, activating the existing in-container presence
  preflight before a command can launch. An Agent may not omit the field to
  skip a reviewed Workspace's declared software prerequisites.
- Legacy/no-manifest Project execution retains its existing behaviour.
  Agents can continue to plan or request governed provisioning even when
  the runtime is not yet running. There is **no automatic image pull,
  privileged installation, silent fallback, or claim that presence validates
  exact package versions**. The Task must still obtain the normal lease,
  run within its independently inspected OCI runtime, and independently
  verify and publish any real artifact.
- Newly registered `project.app.tools.discover`,
  `project.app.toolchain.preflight`, `project.app.godot.build` and
  `project.app.unreal.mcp.probe` now share the existing
  Task-bound canonical sandbox ownership checks; a broad execution lease
  cannot choose another Project Workspace's application through these IDs.

**Still pending:** scheduler-level resource reservations and persisted
backoff/wake, full-version constraint attestation (currently presence-only),
mandatory approved manifest policy for *all* new Workspaces, comprehensive
specialized-adapter execution admission, operator-governed image provisioning,
physical Node acceptance and complete automated recovery. Orchestrator status
and model inference are distinct from OS scheduler readiness; a persisted
`running` claim is not an externally refreshed Node health or execution
postcondition. RC11 review gates #86 remain blockers to packaging.


## RC11 approved Workspace OCI resource waits (durable Agent Worker)

After a user or Agent has proposed `project.app.exec`, the Agent Worker
rechecks the human-approved Task/Workspace toolchain, augments the command
with every approved `required_executables` prerequisite, and checks the
*registered status* of the exact approved OCI runtime and application.

If the approved image has not yet been marked running by the governed Node
reconciler, the Worker records a durable `toolchain_wait` continuation,
moves the *existing* Task/Attempt to `waiting_dependency`, and exposes
`waiting_toolchain` instead of repeatedly sending inference or launching
a build against substitute software. A later Worker tick (including across
service restarts) rechecks the exact approved manifest digest, runtime,
application, live Project/Workspace ownership and registered desired/state.
It waits at least 15 seconds between checks and **does not create a second
Attempt** or replay the previous model-supplied Tool command. Once registered
status is running, the same Worker/Attempt resumes to plan/issue a fresh
proposal; normal rootless OCI verification and executable-presence preflight
remain separate mandatory execution boundaries.

**Honest limits:** This is not automatic package installation, a remote Node
heartbeat proof, a physical OCI inspection or installed-version attestation.
An expired/changed human approval cannot wake the wait; human review is
required. A runtime that merely *reports* running can still fail independent
rootless OCI inspection; OnePane must surface that Tool failure rather than
assert a successful build. Routine-backed direct tools and external API callers
have distinct admission paths; this increment specifically protects the Agent
Worker. Physical trusted Node acceptance remains skipped until explicitly
enabled with an approved pinned image.
