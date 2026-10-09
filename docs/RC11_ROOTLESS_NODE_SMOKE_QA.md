# RC11 rootless Workspace sandbox smoke test

This is the real-Node acceptance gate for OnePane Workspace execution. It is **not**
satisfied by source inspection, SQLite tests, a Windows MSI installation, or
creating a runtime record. The test exercises rootless Podman/Docker on the Node
that actually executes OCI applications.

## Preconditions

- Use a dedicated, approved Ubuntu/other compatible Linux Node, under the
  unprivileged identity that runs OnePane's sandbox adapter.
- Confirm the Node can run **rootless** Podman or Docker and that the selected
  image has `sh` and `sleep` (e.g. a pinned Alpine/BusyBox-compatible image).
- Select an **explicit immutable** OCI `@sha256:<64-hex-digest>` image reference.
  Pre-pull it as the exact unprivileged Node identity. The smoke does **not**
  auto-download images or use registry credentials.
- Confirm the Node has capacity for a 256 MiB RAM / 0.5 CPU container and can
  create a rootless `--internal` network.
- Do not run this test from Windows expecting it to provide a Linux sandbox.
  The Windows OnePane client must use a separately verified Linux Node/WSL2
  execution path with the same authority and isolation checks.

## Commands on the sandbox Node

```bash
podman info --format json   # confirm host.security.rootless is true
# Pre-pull an approved image explicitly, as the OnePane service identity.
# podman pull 'registry.example/approved-image@sha256:<64-hexdigits>'
export ONEPANE_SANDBOX_SMOKE_IMAGE='registry.example/approved-image@sha256:<64-hexdigits>'
go test -tags integration ./internal/sandboxrunner -run '^TestRealRootlessNodeSmoke$' -v -count=1
```

An unset `ONEPANE_SANDBOX_SMOKE_IMAGE` **skips** physical QA; that is not a pass
for the release gate. The test refuses floating tags, checks the image exists
locally, creates an isolated network and OCI application under a unique runtime
ID, independently inspects image/identity/isolation/mounts, executes a command
and checks writable output in the exact Workspace directory, stops the runtime,
and independently confirms the stop. It removes only its generated test
container/network; the working directory is managed by Go's temporary test
cleanup.

## Capture as release evidence

Record OS/architecture, rootless engine version, image reference/digest,
OnePane binary commit, operator/sandbox service account, command exit status,
output/logs, exact runtime/app identity, observed container status and mount
source, CPU/memory policy, and teardown status. Redact machine-identifying
secrets. Repeat on the intended production Node execution path, not solely on a
GitHub-hosted ephemeral runner.

A successful local CLI smoke is only **one** gate. Still required before RC11:
the real OnePane API → Task/Policy/CapabilityLease → Operation Coordinator →
Sandbox Adapter → independent Observation/Verification path on the installed
Windows+Ubuntu configuration; Workspace A/B isolation and cross-Workspace grants;
migration preserving RC10 Projects/models; UI/Inspector/drawer QA. See issues
#19, #20, #29 and #30.
