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
