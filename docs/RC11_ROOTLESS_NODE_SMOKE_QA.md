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

## Optional self-hosted GitHub Actions gate

The `Project Workspace Development Architecture` workflow has a separate
`physical-rootless-node` job. It is **skipped by default**; a skipped job
means physical acceptance **has not happened**.

To opt in for the development branch:

1. Verify the self-hosted Linux x64 runner uses an unprivileged service
   identity with **rootless Podman**, and that the intended repository/workflow
   is trusted to execute on that runner.
2. Pre-pull an approved immutable `@sha256:<64 hex>` OCI image containing
   `sh` and `sleep` **as the runner's exact service identity**. Do not put
   credentials or mutable tags in the image reference.
3. Set repository Actions variable `ONEPANE_ROOTLESS_SMOKE_IMAGE` to that
   pre-pulled reference, then run the branch workflow. The physical job will
   fail explicitly if the image is absent or Podman is not rootless. No
   automatic registry pull is performed.
4. Archive the successful physical job URL/log as release evidence, along
   with the Node and image versions. Remove or unset the repository variable
   to opt out of further physical smoke execution.

Ordinary hosted Go/SQLite and Windows cross-compile jobs remain independent.
A green hosted workflow with a **skipped** physical job is never sufficient
to approve RC11. The smoke is one acceptance gate; real installed OnePane
API→Task/lease→operation→verification, multi-Workspace isolation and
Windows/Ubuntu upgrade preservation must also pass.

## RC11 Node readiness preflight (read-only, available before image approval)

To see the **reason the physical gates are not running**, use this safe
inspection command on the intended unprivileged Linux runner (or from the
checked-out RC11 tree under the OnePane runtime's service account):

```bash
go run ./cmd/onepane-node-preflight
# To check only the Python, Library and HTTP broker acceptance family:
go run ./cmd/onepane-node-preflight -require ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE -strict
```

The command does **not** modify Node state. It only checks whether this is
Linux, whether the current process is nonroot with functional rootless Podman,
and whether immutable OCI digest references supplied through an explicit
allowlist of repository/local environment variable names already exist in
the current user's local image pool. It never runs `podman pull`, `run`,
`exec`, `rm`, `network` or privileged host commands. It does not print
the actual image repository/ref, username, environment contents, local paths
or diagnostic output. Image identities in reports are abbreviated one-way
fingerprints.

The three independent opt-in image variables are:
- `ONEPANE_ROOTLESS_SMOKE_IMAGE`: POSIX `sh` + `sleep` image for general
  rootless Workspace isolation/restart.
- `ONEPANE_GODOT_SMOKE_IMAGE`: digest-pinned Godot 4 image for asset builds.
- `ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE`: digest-pinned Python 3 image
  containing `sh` for large publication, immutable Library and HTTP broker.

The report distinguishes `approval_missing`, `invalid_immutable_reference`,
`not_rootless`, `unavailable`, `image_not_present`,
`image_check_failed` and `locally_present` and always sets
`physical_acceptance_verified: false`. `ready_for_physical_test: true`
means **prerequisites only**; it is not proof that the image has all expected
commands or that the source container passed any of the five physical
acceptance suites.

A new *hosted* informational job in the Project Workspace Development Architecture workflow
prints a GitHub Actions job summary on trusted integration-branch pushes for which approval-variable families
are configured. It never invokes the self-hosted runner or sees the image
values. That summary is not a physical pass either. Only the opt-in jobs
with actual rootless execution are eligible for physical acceptance. The
HTTP broker job uses the same tested read-only preflight in `-strict` mode
before running its real OCI assertions. Existing elevated privileges, unknown
images, remote host network changes and download operations are never
auto-approved.

### Workflow status integrity

The **RC11 Trusted Rootless Node Acceptance** workflow intentionally has
*only physical Node jobs*. With no opt-in image approval configured, GitHub
marks this entire workflow **skipped**, not successful. Its approval-only,
hosted summary is a separate job in the Project Workspace Development
Architecture workflow, so a successful informational report cannot turn the
physical acceptance workflow green. Inspect each physical job's actual
conclusion to decide whether its acceptance requirement has passed.
