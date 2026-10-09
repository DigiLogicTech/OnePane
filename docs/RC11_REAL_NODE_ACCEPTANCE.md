# RC11 — real Ubuntu Node acceptance (opt-in)

This document describes **physical** acceptance, not a substitute for RC11
source/SQLite CI. The existing **RC10 installer is the protected QA baseline**;
do not replace it until the full RC11 Windows+Ubuntu release gates pass.

## Runner requirements

Use an Ubuntu x64 self-hosted Actions runner with Podman operating as the
**unprivileged runner service user**. Do not run the test as `root` or assign
`sudo` privileges. Run these commands as the exact runner identity:

```bash
id -u                         # must not be 0
podman info --format '{{.Host.Security.Rootless}}'    # must print true
podman version
```

Select an approved and reviewed BusyBox-compatible OCI image for the general
smokes, and optionally a reviewed Godot **4.x** OCI image with `sh`,
`sleep`, and a `godot` executable for game development. Both must be
specified by full immutable `registry/path@sha256:<64-hex-digits>` reference,
not `:latest`. Check digest, provenance, licence, architecture and trust
policy independently.

**Operator action required:** pre-pull those images as the same unprivileged
runner user, inspect their exact digest and confirm they already exist:

```bash
podman image exists "$ONEPANE_SANDBOX_SMOKE_IMAGE"
podman image exists "$ONEPANE_GODOT_SMOKE_IMAGE" # if testing Godot
```

The workflow itself uses `--pull=never` for launched containers and deliberately
does not pull or install arbitrary images onto the runner. The runtime uses
network-internal isolated containers, read-only root filesystems, no added
capabilities, no-new-privileges and dedicated generated names/temporary
writable Workspace directories.

## Opt-in via repository Actions variables

In **GitHub → repository Settings → Secrets and variables → Actions → Variables**:

- `ONEPANE_ROOTLESS_SMOKE_IMAGE`: exact approved digest for the BusyBox-compatible image.
- `ONEPANE_GODOT_SMOKE_IMAGE`: optional exact approved digest for a Godot 4.x image that contains `sh`, `sleep` and `godot`.

These are nonsecret public image reference identifiers. Keep credentials in the
Vault; no registry credentials are needed in test containers. No variable means
the corresponding physical gate is **skipped, not passed**.

For self-hosted runner security, the physical jobs are eligible **only on
trusted pushes to** `feature/project-workspace-development-environments`.
Pull-request workflows do not run third-party code on the self-hosted runner.
Use the standard GitHub-hosted architecture CI to validate PRs before merge.

### General dual-Workspace evidence

`TestRealRootlessNodeSmoke` first proves one real runtime can launch,
inspect, execute, persist, stop and be independently observed as stopped.
`TestRealRootlessWorkspaceIsolation` then verifies:

1. Two concurrently executing Workspaces with distinct internal networks and
   distinct writable roots;
2. Each independent container writes and observes its own on-disk identity;
3. Stopping World does not stop Story, which continues to execute commands;
4. Both workspace files persist even after each runtime is stopped;
5. Only exact generated smoke container/network names are deleted in cleanup.

This is not yet a test of placement onto a remote Windows-controlled Node,
host network policies beyond internal isolation, or SDK/tool packaging.

### Godot development evidence

`TestRealRootlessGodotWorldBuild` requires the separate Godot variable.
It creates a minimal Godot 4 project, scene and GDScript in its dedicated
Workspace; imports resources, executes the scene in headless mode, and
checks deterministic output bytes plus their SHA-256 on the host. It also
verifies observed rootless isolation and stopped state.

The generated `world-build.txt` is a **Godot-executed build/test artifact**,
not a packaged game export. Export-template management, editor GUI streaming,
signed binaries and automatic Project Library ingestion remain separate work.

## Release gate

Record the exact workflow URL, commit SHA, runner identity/OS,
image digest(s), observed test output, cleanup result and pass/fail status.
The workflow's summary must be **completed / success** with the intended
physical jobs actually **completed / success**, not `skipped`.

Do not call RC11 physically validated if neither rootless job ran. Keep
uninstall/reinstall and data preservation, remote Node placement, live
user Node orchestration, and Windows installer QA on the remaining RC11 checklist.
