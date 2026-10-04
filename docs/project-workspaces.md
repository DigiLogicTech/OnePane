# Project Workspaces and Project Runtimes

A Project is the durable technical context. A **Project Runtime** is its isolated, runnable environment. The two are deliberately separate: project state survives runtime rebuilds and a runtime never becomes the authority for the Project.

## Intended GUI experience

A Project page can present, in one native workspace:

- source/content and future Git worktrees;
- humans and agents working against the same Project context;
- reviewable `ProjectChangeProposal` objects instead of invisible agent overwrites;
- desired vs observed runtime state;
- installed applications;
- preview/workspace/private/public endpoint declarations;
- Routine bindings;
- a live Event Ledger feed over SSE.

SSE is a transport for the authoritative Event Ledger, not a second collaboration state store. Reconnecting clients resume from an event sequence and then refresh materialized state if required.

## Sandbox model

`project_runtimes` are always declared against the trusted `sandbox_runner` backend. v0.1 accepts two isolation classes: `sandboxed_container` and `microvm`.

The platform owns security policy. User-, repository-, model-, or application-provided configuration cannot request:

- privileged containers;
- host PID/IPC/network namespaces;
- arbitrary host bind mounts;
- the Docker/Podman socket;
- raw device passthrough;
- added Linux capabilities.

Default filesystem policy gives the sandbox an ephemeral root plus `/workspace`. Host mounts, Docker socket access and device passthrough are disabled and `no_new_privileges` is on.

Default networking is deny-by-default, proxy-only ingress, with no egress rules. A later sandbox/network broker may grant narrow JIT egress without changing the Project model.

**Important:** M13 records runtime intent and reconciliation jobs. It does not claim that a sandbox is running until the sandbox-runner milestone observes it. Therefore `desired_state=running` may coexist with `status=defined` or `provisioning`.

## Installed applications

`project_applications` lets an existing application be brought into the Project instead of requiring a greenfield repository. Initial source kinds are:

- OCI image;
- Git source;
- package;
- Artifact Store object;
- Compose definition.

Host-local install sources are rejected. Git inputs must be remote, package refs cannot traverse the filesystem, and Artifact/Compose inputs must resolve through the managed Artifact Store.

Imported applications default to `untrusted_content`. Their own manifests cannot grant host authority, secrets, broader networking or project capability leases.

Environment bindings use either a non-secret literal or a `secret:` reference. Obvious secret-bearing environment names/values are refused as literals. Raw secrets are not accepted as the secret-binding mechanism. A future Secret Broker resolves the reference directly into the trusted sandbox adapter.

## Live endpoints

`project_runtime_endpoints` declare application ports and desired exposure:

- `preview` — ephemeral/project-preview surface;
- `workspace` — authenticated members of the Workspace;
- `private` — deployment-specific private access;
- `public` — explicitly published service.

Declaring an endpoint never creates a raw host bind or proves it is reachable. The trusted runtime/proxy reconciler establishes and observes the endpoint later.

## Human + agent review

Changes are represented by `project_change_proposals` and may reference a patch/diff Artifact. Agents therefore propose modifications instead of silently replacing authoritative files.

A proposal flows through review (`proposed/reviewing -> approved|rejected`) before a future application path commits it to the live Project revision. The proposer cannot satisfy the review gate by approving their own proposal.

Future Git/worktree support will attach repository diffs to the same proposal object rather than inventing a competing review model.

## Routine integration

`project_routine_bindings` binds an existing Routine to a sandbox-internal action:

- `app_command`;
- `http_request`;
- `tool_capability`.

The binding **does not execute cron inside the container** and does not grant authority. When the Routine Engine is implemented, an occurrence creates a normal Task referencing the Project. The normal Task/Policy/CapabilityLease/Verification path then invokes the bound action inside the sandbox.

This keeps scheduled self-hosted apps under the same audit, recovery, safety-mode and verification controls as every other autonomous action.
