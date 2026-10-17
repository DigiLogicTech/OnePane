# RC11 — Unified Assistant and local-first execution contract

**Decision authority:** user's OnePane vision alignment on 2026-10-11.
**Status:** Design acceptance baseline; implementations are not automatically
complete or approved. Tracks decision 1 in the vision decision register.

## Primary goal

On modest or older hardware, a user can give OnePane a substantial task and
OnePane will continue working toward completion, **including slowly**, by
queueing tasks, reusing models, swapping inactive runtimes, and dispatching
stages to suitable local Workers. Rigorous research and operations controls
remain available without obstructing routine usage.

## User-facing behaviour

1. **One Assistant, not global and Project Assistant duplicates.** Chat is a
   single entry point with an optional visible Project/Workspace context.
   No Project selection denotes general assistant conversation only.
2. The assistant may plan, explain and propose work anywhere. To read/write
   sandboxed resources or run tools, it must explicitly route through an
   authorised Workspace-bound Task/Worker. Chat itself confers no privileges.
3. Users should be able to express the desired result conversationally,
   review a proposed execution plan when appropriate, then execute and track
   progress with clear run, pause, retry, cancel and resume controls.
4. Default UI exposes understandable drop-downs, checkboxes and concise
   status. Advanced policies, isolated research rounds and provenance are
   discoverable but do not have to be configured for routine local work.
5. Show the selected context, chosen model or automatic routing mode, job
   queue status, current resource bottleneck, and any consent/action needed.
   Never represent a stalled job as completed.

## Resource-aware orchestration

1. Queue stages when RAM/VRAM or a compatible runtime is unavailable; avoid
   discarding viable tasks solely because immediate capacity is insufficient.
2. Hot-swap and unload idle models, preserve checkpoints and retry only
   operations known to be safe to repeat. On an unknown external outcome,
   stop and surface explicit recovery choices.
3. Prefer local execution even at lower throughput. Colibri, memory-saving
   strategies and model routing are **complementary** options chosen by
   capability, hardware fit, tools and user preferences.
4. Prefer models with the required tool capability and protocol level. Route
   to a stronger model or authorised remote/cloud compute only under
   explicit user grant and cost/data-flow constraints.
5. Worker models need not all be resident simultaneously. Batch or stagger
   operations, preserve lineage and resume after a model swap or service restart.
6. Long-running research may use strict seat isolation, provider pinning,
   evidence provenance and council rounds, without secretly enabling fallbacks.

## Isolation and authority invariants

- Selecting a Project in chat changes the **conversation's context**, not
  filesystem, secret, network, or Workspace permissions.
- Child Workspaces may share artifacts only through an explicit, auditable
  brokered grant. A prompt cannot declare a new grant.
- Every side-effecting operation runs with canonical Project/Workspace and
  Agent identity at the execution boundary; revalidate revocable permissions.
- Avoid a second default Home Workspace hierarchy merely to host a global
  assistant. If general conversations need durable records, store them under
  a distinct chat/session record with separately scoped execution authority.
- Retain local-first/offline utility and opt-in cloud spending; do not
  misclassify remote calls as local based solely on friendly model names.

## Testable scenarios to include in implementation QA

- User asks the unified Assistant to complete a multistage project on
  hardware that cannot load all models concurrently. Queued stages finish
  sequentially, inactive models unload, and work checkpoints persist.
- User switches Project context without acquiring read/write access to
  another Project's isolated Workspace.
- Assistant selects a model capable of a requested tool, then falls back
  to waiting/queueing when none fits local resources; paid cloud use requires
  an explicit grant.
- Stop/restart of OnePane while waiting for a model does not duplicate
  side effects or falsely mark a task complete.
- New users can configure the core workflow with checkboxes and drop-downs
  instead of hand-editing JSON or provider routing scripts.
- Advanced Research mode remains isolated, provenance-aware and capable
  of pausing rather than silently replacing a pinned failed model.

## Not yet decided

Confirmed: L4 is the default, L5 is a first-class continuous-operations
mode with safe-by-default remediation plus opt-in bounded higher-impact
playbooks (decision 2). The user also approved distinct Read, Write,
Execute, Network and Secrets controls enforced across all tools, with
optional advanced scoping (decision 3). See the dedicated operating-mode
and universal Workspace permissions specifications.

Confirmed: Agent policies have immutable versioned revisions for
reproducibility and provenance, with active permission revocation at
every relevant side-effect boundary (decision 4). Read the dedicated
Agent policy provenance and live revocation contract for implementation,
cancellation and unknown-outcome acceptance cases.

Confirmed: the Debug & Diagnostics Centre will provide low-overhead live
health and durable historical incident evidence by default, with bounded,
opt-in detailed tracing per Project or Agent (decision 5).

Still undecided: classification and grants for local, trusted-Node and
external/cloud compute (decision 6). Do not infer these from the local-first objective.

## Gate

Implementation follows further user alignment, code changes and regression
tests, then the second code audit. No release merge, installation package
or QA-ready claim follows from this design document.
