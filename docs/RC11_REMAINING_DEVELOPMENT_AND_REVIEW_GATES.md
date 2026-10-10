# RC11 — remaining development and release gates

**Source of truth:** [draft integration PR #18](https://github.com/DigiLogicTech/OnePane/pull/18) and linked GitHub issues below. **Protected baseline:** `release/alpha3.3-rc10-msi-qa`. This file is a living execution plan, not evidence that any unchecked acceptance has passed.

## Remaining development work (complete before the consolidated QA build)

| Order | Workstream | Tracking | Outstanding release-level acceptance |
| --- | --- | --- | --- |
| 1 | Independent Workspace sandbox/runtime execution | [RC11-01 #19](https://github.com/DigiLogicTech/OnePane/issues/19) | Real rootless Node / two isolated Workspaces; trustworthy observed state |
| 2 | Toolchains, files, Git, terminal | [RC11-02 #20](https://github.com/DigiLogicTech/OnePane/issues/20) | Pinned image/toolchain, Godot sample build, safe mutations, restart |
| 3 | Selective Workspace collaboration | [RC11-03 #21](https://github.com/DigiLogicTech/OnePane/issues/21) | Explicit runtime service links, lifecycle and revocation, safe previews |
| 4 | Project Library and scoped evidence | [RC11-04 #22](https://github.com/DigiLogicTech/OnePane/issues/22) | Document ingestion, indexing/search, selected versions and access tests |
| 5 | Autonomous Project Orchestrator | [RC11-05 #23](https://github.com/DigiLogicTech/OnePane/issues/23) | Objective decomposition, dependency control, recovery approval, resource waits and CPU-only execution |
| 6 | Model lifecycle and routing | [RC11-06 #24](https://github.com/DigiLogicTech/OnePane/issues/24) | Agent Check residency, Colibri/llama.cpp offload, OmniRoute policy, existing-model adoption |
| 7 | Research Council | [RC11-07 #25](https://github.com/DigiLogicTech/OnePane/issues/25) | Strict pinned seats and isolation, multiple rounds, resumable rate limits, immutable provenance |
| 8 | Operations/Evidence/Nodes/Tasks | [RC11-08 #26](https://github.com/DigiLogicTech/OnePane/issues/26) | Real operator views and a traceable Task → verification → artifact history |
| 9 | UI and legacy consolidation | [RC11-09 #27](https://github.com/DigiLogicTech/OnePane/issues/27) | Complete canonical navigation, Inspector/drawer responsiveness, themes and safe source cleanup |
| 10 | Security, Vault, governance | [RC11-10 #28](https://github.com/DigiLogicTech/OnePane/issues/28) | Enforced least-privilege, no Workspace leaks, safe secret grants and adversarial tests |
| 11 | **Debug & QA Diagnostics Centre** | **[RC11-13 #85](https://github.com/DigiLogicTech/OnePane/issues/85)** | Time-bounded opt-in capture, correlated diagnostics, on-device sanitized report export, privacy/disk/performance tests |

These are interdependent workstreams, not a promise that each must be completed in strict numeric order. An implementation with green Go CI can still fail physical Node or installed Windows/Ubuntu QA. The Debug Centre must be complete before operator testing to make intermittent failures diagnosable.

## Mandatory user-directed review sequence — before packaging

**[RC11-14 #86](https://github.com/DigiLogicTech/OnePane/issues/86)** is now a **release blocker**, not an optional document review.

1. **First full code review:** inspect *all* source, legacy/recovery assets, workflows, migrations and actual feature wiring. Deliver implementation map, defects, risks and evidence; **user reviews the findings**.
2. **Vision alignment interview:** assistant asks targeted questions in manageable rounds from those findings. Reconfirm Projects and independently sandboxed Workspaces, selective cross-Workspace sharing, local-first autonomy on limited hardware, models, research, tool capabilities and UX. Record decisions and receive **explicit user approval**.
3. **Second code review and realignment:** map the agreed vision to code, remedy approved gaps, re-run relevant tests, document what is and is not verified, and receive **second user approval**.

**Do not cut, publish or present an RC11 consolidated QA installer before all three gates are signed off.** The operator should be able to challenge both the implementation **and whether it implements the right product**. Feature engineering and existing CI may continue before these reviews.

## Final packaging and QA

- **[RC11-11 #29](https://github.com/DigiLogicTech/OnePane/issues/29):** only after the three reviews; build and validate Windows MSI and Ubuntu package with RC10 upgrade, new installs, data/model/Project preservation, temp/cache controls, rollback and checksum/provenance.
- **[RC11-12 #30](https://github.com/DigiLogicTech/OnePane/issues/30):** complete end-to-end application/Node/Research/Model/Debug regressions, immutable QA artifacts, explicit unsupported-environment list and operator test checklist.

**No default cloud inference fallback; no invisible Project Workspace authority escalation; no physical test claim without physical execution.** The RC10 release branch remains untouched until operator acceptance.

## Rootless two-Workspace physical acceptance expansion (RC11-01/02)

The separately approved, digest-pinned **rootless Node** integration test `TestRealRootlessWorkspaceIsolation` now checks more than an initial stop:

- World and Story must start as distinct real OCI containers with independently inspected rootless isolation, private internal networks and writable Workspace roots.
- Stopping **World** must leave **Story** executing and reading its private file.
- Restarting World's **exact original runtime spec** must preserve its independently verified OCI identity and its pre-existing file.
- Rebuilding World's **versioned command spec** must replace only its originally verified owned container identity, preserve World's writable file, and leave Story's original process, file and isolated network unchanged.
- Both Workspaces must stop cleanly without image pruning, uncontrolled host-shell commands, or deletion of existing user roots. The explicit trusted test uses only fresh generated OCI names and disposable temporary directories.

This added acceptance is source-compiled by integration CI, but physical results remain **unverified** while the trusted self-hosted rootless job is **skipped**. The physical workflow still requires a deliberate repository administrator-provided digest-pinned pre-pulled image and verified unprivileged Podman identity. Do not substitute a hosted/unit simulation or a green source-compilation run for the actual rootless acceptance check. After the approved Node/image become available, run the existing opt-in workflow and record the actual result before closing #19/#20 or packaging RC11.

## RC11-02 — governed Godot 4 toolchain execution (2026-10-10)

A dedicated `project.app.godot.build` sandbox tool is now registered with the existing `project.app.execute` capability and `ActionExecuteSandboxed` authority/lease path. It is **not** an uncontrolled package installer, a host command or a model-controlled Godot CLI. It accepts only `runtime_id`, `application_id`, `action` (`import` or `run`) and optional bounded `timeout_seconds`; unknown JSON keys and all executable/image/path/environment/mount/network overrides are rejected.

Commands are immutable argument vectors, never a shell:
- `import` → `godot --headless --path /workspace --editor --import`
- `run` → `godot --headless --path /workspace --quit-after 60`

Godot must already exist **inside the explicitly registered immutable OCI image**. The adapter probes rootless OCI, checks the owned runtime + application, requires running/observed `IsolationVerified` with exact single allowed writable Workspace mount and valid spec hash, and bounds execution by caller cancellation or 600 s default / 3600 s maximum. Nonzero scene/import exit status is preserved as an observed **build failure** (not labelled build success), and OCI timeouts/errors fail the Tool invocation. A successful command explicitly reports `artifact_verified=false`: it does **not** publish or externally attest to an artifact. A distinct Task-owned `project.app.files.publish` invocation remains responsible for independently hashed, immutable Library publication and grant controls.

Unit tests cover the exact arguments, forbidden injection parameters/paths, unsupported actions, rootful/extra-bind denial, failed Godot run and deadlines. The opt-in, trusted self-hosted Godot physical acceptance now exercises the **same adapter action path** instead of bypassing it with raw `ExecContainer`, followed by independent host SHA-256 readback of the generated file. Physical CI remains explicitly skipped until a repo administrator pre-pulls and authorises a compatible digest-pinned Godot OCI image and rootless runner; these green source tests cannot claim installed Godot acceptance.

**Still open:** real physical Godot run, package/image preflight, Task Gateway/capability lease live acceptance, publication to immutable Project Library through the real publisher, two-Workspace controlled grants, error/retry/rollback and high-confidence postcondition probe. Do not close #20 or build RC11 yet; user-mandated #86 reviews remain release blockers.

## General-purpose, multi-MiB Task outputs — source and QA scope

The `project.app.files.publish` Tool now supports **opt-in** `publish_large`
for arbitrary generated files from a verified, owned, running rootless OCI
Workspace. This fixes the previous 256 KiB *per-file* limitation without
raising the command-output limit or trusting CLI output truncation. The
**existing `publish` action is unchanged**.

- A fixed OnePane-owned in-container Python reader uses O_NOFOLLOW on every
  directory and the file. It returns a full-file SHA-256 and stable file
  fingerprint, followed by 512 KiB chunks encoded as bounded receipts, then a
  second full-file hash/fingerprint. The host independently verifies every
  chunk and final SHA-256 before publication.
- Maximum file size is **32 MiB** for now, with a **600-second default**
  transfer timeout and a configurable maximum of 1200 seconds, still subject
  to the shorter Task cancellation deadline. Larger
  files, changed sources, malformed/chopped OCI replies, wrong digests,
  unsafe paths, symlinks and extra host mounts fail closed.
- A second authority boundary rechecks the active Task and Attempt, Project,
  canonical Workspace, running on-Node runtime/application and worker before
  the artifact store writes any new bytes. The existing content-addressed
  blob store, immutable Library versions, duplicate suppression, unknown-
  outcome recovery and revoked/cross-Workspace access restrictions remain
  authoritative. No host temp staging or additional network listener is
  introduced.
- Regression suite includes real Python no-follow chunk reads, multi-MiB
  source mutation during transfer, corrupt chunk/hash, same Task ownership,
  missing Attempt, foreign OCI mounts, immutable blob readback, same-hash
  retries, new-version handling and cross-Workspace access denial.

**Still open:** external physical Ubuntu rootless Node smoke must run on an
explicitly trusted host and digest-pinned Python-capable image. 32 MiB memory
assembly is unsuitable for multi-GiB models, large videos/game builds or
low-RAM servers; streaming directly into a bounded, permission-checked blob
sink with durable chunk manifests, disk quota, resumability and recovery is
future work. Do not claim unlimited artifact support or close #20 yet.

## RC11-03 — expiring selective Workspace artifact links (2026-10-10)

Directional artifact links from one active Project Workspace to another can now
carry an operator-defined `expires_at_ms` deadline (Unix milliseconds).
Existing links keep a null, perpetual expiry for backward compatibility.
Create and toggle/update APIs may set a bounded future expiry; renewal or
explicit clearance requires the current revision and authorised Project writer.
An expired link remains in historical metadata as `enabled: true,
expired: true`, never silently turning into a fresh grant.

The same effective expiry check is enforced before new artifact publications,
publication listing, Workspace Library asset enumeration, immutable version
listing/resolution, and verified Workspace output reads. Both endpoints and
the containing Project must still be active. Stale expiry renewals are rejected.
The underlying immutable blob/version and provenance do not get deleted on
expiry; re-enabling/renewing is an explicit operator act.

This is **bounded artifact exchange**, not rootless runtime-to-runtime service
connectivity, desktop preview forwarding, a transitively propagated Library
grant, or a lease to use another Workspace's secrets/tools. RC11-03 #21 remains
open for separately governed service connectivity and installed UI acceptance.
