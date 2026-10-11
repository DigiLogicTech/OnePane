# RC11 — Universal Workspace Permissions

**Confirmed by user:** 2026-10-11.
**Purpose:** Simple, consistently enforced Workspace access controls for
the local-first OnePane harness. This is an implementation contract, not
a claim that all adapters already enforce it.

## User-facing controls

On Project/Workspace settings expose familiar, separate controls:

| Simple control | Meaning within authorized scope | Never implicitly grants |
| --- | --- | --- |
| Read files | Read approved Workspace data and allowed mounts | Other Workspaces, unrelated host paths |
| Write files | Create, edit, delete and publish approved Workspace data | Host filesystem, destructive changes outside scope |
| Execute | Run approved programs/scripts in constrained sandbox | Unrestricted host shell or privilege elevation |
| Network | Use approved destinations/protocols | LAN scans, other networks, cloud or WAN destinations |
| Secrets | Use individually approved secret bindings | List all credentials, export plaintext, inherit other scopes |

Offer presets such as **Local development**, **Read-only research** and
**Restricted monitoring**, but never let a preset silently override a
parent policy. Show what each control enables, and an **Advanced** section
for path/mount, program/capability, network target, source and destination,
secret name, per-operation approval, schedule and budget constraints.
A denied capability should explain what is blocked and why.

## Canonical policy evaluation

Project grant -> Workspace restriction -> Agent restriction -> Task
restriction -> approved operation permit -> actual adapter invocation.

- Descendants can only narrow the permitted action set. Reject unknown or
  malformed grants and missing canonical ownership, even if the request
  originated from a trusted model or UI.
- Evaluate current policy, principals, scopes and resource IDs at every
  side-effect gateway, including headless/background paths, retries,
  delegated Workers and resumed operations.
- All effective grants are **capability- and target-scoped**. A tool's
  friendly name or declared purpose is never itself sufficient authority.
- Apply runtime isolation to the real filesystem, mount, network,
  process and secret boundaries. The app's policy check complements,
  rather than substitutes for, operating-system sandbox enforcement.
- Cross-Workspace reads/writes, communication and artifact publication
  require explicit scoped brokered grants; no implicit sharing through
  a global Assistant, Project context switch or common model.
- Secrets should be referenced by opaque IDs and delivered only to
  authorised tool/runtime destinations. No arbitrary environment dumps
  or plaintext logging.

## Required operation taxonomy

Every adapter must map the **actual effects** of its operations to
`files.read`, `files.write`, `process.execute`, `network.connect`,
`secrets.use` and any additional required operation-specific authority
(e.g. artifact publication, host mutation, cross-Workspace transfer).

For shell, Python, Git, OCI, MCP, browser automation, GUI automation,
artifact/Workspace services and custom toolchains, identify all relevant
effects, not just the first one. A Git clone may require execute, network
and file write. Executing a shell command can imply file access and
outbound network effects; grant only where sandbox confinement makes those
effects bounded. Unknown or ambiguous effects fail closed or request a
narrowly scoped approval, not unrestricted execution.

## Resource-constrained first-class operation

Local users should be able to grant ordinary Workspace-local read, write
and execution once for an authorised sandbox. The Worker may then create
files, run tests, change models and continue a long-running project without
a new approval each time. The model queue and Colibri/hotswap must not
change or bypass the effective permission set.

Network/cloud/secret access and any host modification remain separately
controlled. These grants can be preapproved and precisely scoped for
continuous operations (Level 5), with expiry, resource limits and an
emergency pause/revoke.

## Workstreams

- [ ] **PERM-01:** Define a typed, versioned policy schema, target/resource
  selectors and migration preserving existing data. Existing installations
  do not silently receive extra privileges.
- [ ] **PERM-02:** Implement shared effective-scope and action evaluation
  with immutable audit evidence and live revocation at execution boundaries.
- [ ] **PERM-03:** Inventory and classify all adapter capabilities: file
  gateway, shell/Python, Git, OCI, MCP, GUI, artifacts and host tools.
  Treat unclassified adapters as unavailable rather than unrestricted.
- [ ] **PERM-04:** Enforce sandbox-level filesystem/mount/process/network
  restrictions where supported; do not rely solely on text/classification
  heuristics to confine general-purpose code execution.
- [ ] **PERM-05:** Apply separate bounded secret IDs and network destination
  grants with explicit Project/Workspace ownership.
- [ ] **PERM-06:** Expose separate checkboxes/dropdowns, intuitive presets,
  optional advanced settings and an audit/approval explanation surface.
- [ ] **PERM-07:** Integrate parent/child inheritance, cross-Workspace
  publication approval, Agent/Task delegation and Level 5 playbook permits.
- [ ] **PERM-08:** Add end-to-end regression across API, queued Workers,
  restarts and every adapter family, including denied/unknown operations.
- [ ] **PERM-09:** Second code/security audit, hosted CI and physical
  Windows/Ubuntu QA following the required product review gates.

## Negative-path acceptance

1. Files Write off: shell, Git, Python and MCP cannot write through a
   mounted path, a Workspace file API or another tool implementation.
2. Network off: execution is confined even when a program attempts an
   unexpected outbound connection; a model cannot request unrestricted
   access through a new tool.
3. Secrets off: a Worker cannot retrieve secret values using alternate
   adapter APIs, environment variables or another Workspace's resources.
4. A read-only Workspace cannot publish a modified artifact or delete a
   file through a different endpoint.
5. An Agent delegated by a privileged parent cannot exceed the child
   Workspace's actual permission set.
6. Revoked grants block new operations and recurring Level 5 playbooks;
   unknown in-flight outcomes remain visible for operator reconciliation.
7. A model swap, service restart or queued task resume does not
   reacquire or invent a previously revoked grant.
8. Unknown/opaque adapters cannot bypass denied permissions by using
   generic tool names or action metadata in a prompt.

**Status:** Requirements accepted; implementation and full adapter
coverage pending. Do not merge to release or publish an installer before
vision alignment, second audit and user approval.

## Partial enforcement checkpoint

`internal/featurepolicy/policy.go` now separately models FileRead, FileWrite, ProcessExecute, NetworkAccess and SecretsAccess. The legacy Files flag is retained as an upper-bound master grant to avoid weakening historical decisions. `internal/tool/feature_action.go` classifies known file, Git, OCI, command, MCP probe, network and secret actions, and `internal/tool/gateway.go` checks all required actions both before consuming authority and immediately before adapter dispatch. Unknown configured operations are denied. Project/Workspace policy editors block Agent-initiated expansion of these feature grants while permitting restriction. **Still incomplete:** universal sandbox OS policy enforcement for arbitrary spawned code, all host/network/secret APIs, operation/playbook classification, UI permission presets and full cross-adapter end-to-end tests. Do not claim the full five-switch product is complete.
