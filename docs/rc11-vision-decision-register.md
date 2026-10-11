# OnePane RC11 — user vision alignment decision register

This register follows the **first assistant code/security review** of draft
[PR #227](https://github.com/DigiLogicTech/OnePane/pull/227).
No question below is approved yet. Do not interpret recommended starting
positions as permission to merge or release.

## Decision 1 — global assistant and Workspace authority
**Question:** Should OnePane's global Chat Assistant run in a separately
governed **Home Workspace**, with explicit Project/Workspace switching for
project conversations, or always require a selected Project/Workspace?

**Proposed starting position:** A separate local Home Workspace for global
assistant tasks, plus a visible active Project/Workspace selector for
project-specific chats. No implicit read/write access across Workspaces.
Any cross-Workspace coordination must use explicit, auditable grants.

**Status:** AWAITING USER.

## Decision 2 — local-first autonomy defaults
**Question:** Once the user approves an isolated local sandbox, should
autonomous work be active by default, even on slower hardware, or require
additional per-Project/Workspace consent?

**Proposed starting position:** Automatic action is available for
explicitly approved Workspace sandboxes, subject to per-Workspace
caps and revocable permissions. Never give implicit host, network, secrets
or cross-Workspace authority; no cloud fallback without user grant.

**Status:** AWAITING USER.

## Decision 3 — file permissions across all tools
**Question:** Should disabling Files block all file-capable paths including
shell, Git, MCP, artifacts and OCI execution, or only high-level File tools?

**Proposed starting position:** All file-capable operations must be
classified as read, write, publish or execute before execution. Denied
read/write/publish cannot be bypassed via a generic shell or MCP adapter.
Trusted toolchains use explicit scoped grants, never hidden exception lists.

**Status:** AWAITING USER.

## Decision 4 — Agent policy and revocation
**Question:** Should Agent policies use immutable versioned snapshots,
live revocation checks, or both?

**Proposed starting position:** Versioned Agent policies for provenance,
plus rechecks at each Task, inference and tool side-effect boundary.
A revoked Agent must not start a new action; in-flight external calls
need explicit cancellation/unknown-outcome semantics and safe recovery.

**Status:** AWAITING USER.

## Decision 5 — diagnostics visibility and privacy
**Question:** Is operator-controlled, time-limited live policy tracing
needed in addition to the offline support-bundle reviewer?

**Proposed starting position:** Opt-in, time-limited local trace sessions
with redacted evidence and explicit export, disabled by default. No raw
prompts, tokens, credentials or persistent cross-process correlation key
in support bundles.

**Status:** AWAITING USER.

## Decision 6 — compute and provider trust categories
**Question:** Can verified on-device OpenAI-compatible model runtimes and
paired trusted LAN Nodes count as local-first when cloud permission is off?

**Proposed starting position:** Distinguish **origin-host local**, **trusted
paired Node** and **external/cloud** by observed and verified network
destination, node identity and data-flow policy, not model/provider labels.
Require an explicit grant for off-host transfers even to trusted Nodes.

**Status:** AWAITING USER.

## Unresolved release gates
- [ ] User approval of first code review and this vision alignment.
- [ ] Apply decisions to Workspace, Agent, UI and runtime permission boundaries.
- [ ] Repeat independent code/security audit on the finalized contract.
- [ ] Obtain approval to create Windows x64 / Ubuntu amd64 installers.
- [ ] Run installed-build persistence, hardware and manual QA.
- [ ] Authorize release merges.

**Hard stop:** No merge, packaging or claim of full product QA from draft CI.
