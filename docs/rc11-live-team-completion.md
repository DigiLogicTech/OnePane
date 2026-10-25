# Live collaborative Team mode — completion contract

User requirement confirmed 23 October 2026: Team mode should provide named agents working together visibly in real time, similar to the collaborative Grok Bot experience. The user explicitly selected **both a shared conversation and individual live agent panels, with switchable views**.

This extends the existing Team product contract. Continue to preserve current working Team/Council records and execution controls during the transition. The source progress below is separate from integrated execution and installed acceptance.

## Source progress, 23 October 2026

Implemented the canonical Task Inspector entry to a shared live room with switchable Conversation/Agents views, retained drafts and selected reply targets. Read polling is bounded, reauthorizes every request, stops on close/Workspace change, and clears inaccessible data on failure. Stable storage-assigned message sequence supports long-room pagination and newest worker context even after wall-clock rollback. Named panels show actual turn and correlated shared-compute state; they do not simulate streaming or tool execution.

Team chat must operate alongside the existing application: hot swap, Colibri, global queuing, model residency, CPU/GPU admission, cancellation, budgets, routing policy, Node trust, Research seat pinning and Workspace isolation. It must use the shared inference/runtime path. A separate Team model loader or independent capacity queue would violate this requirement. Agents waiting for compute are not represented as executing inference.

The existing path is Team worker → scheduler → shared inference service → global runtime coordinator → managed runtime transport/supervisor. Exact session/member/turn correlation now projects the worker's durable compute queue into the room. A real migrated-SQLite test calls Team dispatch while another task holds the same compute pool, checks that it queues with its Task identity, observes the Colibri/CPU claim in the live room, and cancels before any model transport. Shared coordinator tests also cover independent Nodes and simultaneous admission/cancellation; cancellation must not strand a lease. These use synthetic deployments, not physical Colibri engines.

Named execution assignments and causal result handoffs now have a source implementation. The operator saves and reviews an explicit plan, then approves it to create ordinary governed child Tasks atomically. Each contribution inherits the parent Project/Workspace, artifact session, model restrictions, toolchain pin and verification criteria. The original Task consolidates after all contributors complete. Immutable assignment records retain member/profile identity and dependency order. The live room shows actual Task/Worker/step/compute states, allows Task inspection and records idempotent harness completion notices. Notices are labelled system updates, not agent-authored chat.

The shared authority fence runs at Task start/resume, each Worker step, model dispatch after queueing, runtime dispatch, tool authorization and proposal application. It rejects paused/cancelled roots, changed profiles, scope/completion/objective drift, missing/weakened dependency edges and invalidated predecessor checkpoints. Contributions retain their recorded verification level and remain derived context; V0 is explicitly declaration only. Existing Project graph prerequisites also gate every contribution, and graph reporting recognizes the separately accepted Team contributions without classifying them as foreign dependencies. A scheduler cursor deadlock exposed by single-connection SQLite was repaired without loosening qualification/admission checks.

Source acceptance now includes a two-member Builder → Reviewer → consolidation scenario through the real Worker, inference service, shared coordinator and checkpoint pipeline, using a deterministic synthetic model transport. The reviewer consumes only the explicitly linked builder checkpoint. Tests also pause after runtime admission and during Tool Gateway permit consumption and prove the model/tool adapter is not called. This proves harness plumbing, not real-model reasoning quality or physical tools.

Local source now includes bounded two-member concurrent inference through shared admission, agent-suggested assignment drafts for human review, additive per-contribution assurance criteria, and addressed human guidance during execution. A Team-scoped cancellation request covers unfinished descendant Tasks atomically and preserves completed results. Returned reasoning proposals are discarded after cancellation and the original Worker/Attempt waits for reconciliation. Explicit Team pause/resume now retains original Attempts and reports draining; source tests cover queued/returned reasoning and reconstruction of a held Worker service. These are synthetic source checks; see `rc11-qa-execution-status.md` for current evidence.

Still required: broader fair concurrent teamwork; dynamic help/reassignment; richer artifact navigation; precise waiting-for-runtime/swap/loading observations; cancellation completion and restart/unknown-outcome acceptance with real models/tools; actual hot-swap/Colibri contention with ordinary chat/Project workers on physical hardware; final browser/product UI and installed Windows-first acceptance. Accepted assignment graphs currently cannot be reopened/replaced in place; do not infer reassignment support. Existing queue records mark admission as running before runtime loading, so the UI must not infer a finer residency state from that record.

## Required experience

26October source continuation: durable original-owner human assistance now has
immutable requests/answers, explicit authenticated Continue, atomic applied-input
receipts and required next-model provenance. Bounded current-request discovery
and Needs your input controls are wired to the existing Team room, with separate
Save reply/Continue actions, draft retention and denied/stale-response fencing.
Actual authenticated HTTP -> Worker wait -> answer/Continue -> reconstructed
Worker -> next-model transport source tests cover bearer and browser credentials.
Deterministic source/controller/renderer tests do not establish real two-agent
execution, final Windows/Ubuntu product review or installed acceptance. Peer
help, reviewed versioned reassignment and broader fairness remain required.

- One objective and a reusable roster of named agents with roles, model choices, scoped capabilities and persistent project context.
- **Conversation view:** a shared room showing human guidance, agent messages, requests for help, replies, assignments, handoffs, review and the consolidated result. Show actual authored messages and concise progress updates.
- **Agents view:** individual panels showing each agent's assignment, current observed state, selected model/runtime, tool activity, dependencies, outputs and blockers. Selecting a panel opens its history and artifacts.
- Switching views observes the **same run and durable records**. It must preserve the draft message, selected agent, reader position and ongoing work; switching views never creates another turn or model request.
- The operator can address the whole team or a named member, steer work during execution and inspect decisions. Pause/cancel/reassign must have explicit scope and honest states for work already underway.
- Agents can divide work, request help, share authorised findings, review each other's artifacts and hand off dependent work. The harness records who owns each assignment and which results it depends on.
- Deliver one verified result with linked contributions, tests and unresolved issues. A discussion alone is not completed execution.
- Local-first scheduling remains fundamental: run concurrently when capacity permits, queue when memory/model residency requires it, and show that distinction. No requirement to keep one resident model per agent, use cloud inference or run all agents simultaneously on small hardware.

The reference is the interaction and collaboration experience. OnePane retains independent Workspace isolation and explicit sharing grants; it does not adopt another product's shared filesystem/account authority model.

## Existing source and the gap

| Path | Current behaviour | Completion work |
| --- | --- | --- |
| `internal/team/service.go` | Durable members, sessions, messages, rounds, plans, objections and decisions | Extend with scoped, durable assignments, addressed collaboration and handoff ownership, reusing existing Task/Attempt records where possible |
| `internal/teamworker/service.go:109` | Bounded pending-turn scan, sequential processing inside a Tick | Resource-admitted concurrency and fair scheduling; no blanket goroutine fan-out or duplicate model residency |
| `internal/teamworker/service.go:690` | Reasoning-only inference/runtime dispatch; response recorded after completion | Agent progress/public messages and governed execution through Task/Worker/Tool Gateway, with correlated activity and artifacts |
| `internal/team/service.go:811` and Task admission | Human acceptance of a TeamPlan gates execution | Preserve this default. Any preapproved execution option needs an explicit policy design, bounded grants and negative tests; selecting Team mode does not remove acceptance gates |
| `internal/api/sse.go` | Workspace event stream, durable sequence, one-second server polling | Session/Task/member-scoped projection, reconnect cursor, live authorization/revocation, bounded replay and observable stale/disconnected state |
| `internal/webui/static/app.js` | Team setup/configuration and Research controls | A canonical live Team surface reachable from Team Tasks and the unified Assistant, with conversation/agent view switch |
| `internal/team/service.go:560` | Message list returns earliest bounded messages | Add stable cursor pagination/latest window before relying on it for long live sessions; never silently stop updating after 200 messages |

These are inspected implementation facts and proposed work, not a completed architecture audit. Model-private reasoning is not part of the live feed. Show agent-authored communication and harness-observed work.

## Implementation order

1. **Durable activity contract:** session/run/member/Task/Attempt/operation identity, assignment lifecycle, public messages, causal replies/handoffs, persisted sequence and bounded pagination. Reconcile restart/unknown outcomes without replaying external effects.
2. **Scoped live projection:** reuse the ledger/event infrastructure; expose only authorized Team data and safe activity metadata. Do not rely on broad workspace events to authorize raw transcripts or tool content.
3. **Switchable Team surface:** conversation and agent panels backed by the same projection. Retain drafts and reader position, accessible controls, stale status and explicit queue reasons.
4. **Collaborative execution:** map accepted assignments onto existing governed workers, dependency graphs, model/resource admission and Library artifacts. Test bounded concurrent execution and sequential fallback on constrained hardware.
5. **Integrated acceptance:** run the cases below, then include the surface in the mandatory final product UI review. Preserve Council independent first-pass evidence and pinned-seat semantics separately.

## Required CI and acceptance cases

| Case | Expected evidence |
| --- | --- |
| Two agents, one objective | Distinct persisted assignments and authors; one consumes an explicitly shared verified output from the other; consolidated verified result |
| View switch during active work | Both views identify the same run/sequence; draft retained; no duplicate round, inference, task or tool effect |
| Independent work | Concurrent execution with available capacity; visible queueing and eventual completion when only one local model fits |
| Human steering | Addressed message reaches intended recipient(s), persists across reconnect and does not impersonate another member |
| Live activity | Started/waiting/tool/result events are grounded in actual records; missing token streaming is not simulated as typing or tool execution |
| Reconnect/restart | Resume cursor with no gaps/duplicates, deterministic snapshots, preserved assignments/checkpoints and interrupted effects requiring reconciliation |
| Long room | More than 200 messages remain navigable; the newest message is visible; equal-timestamp ordering is stable |
| Isolation and revocation | Wrong tenant/Workspace/Team cannot subscribe, page, address or obtain artifacts; live revocation denies subsequent reads and dispatches |
| Security and boundedness | Untrusted agent/tool text rendered as text; no secrets or private reasoning in public activity; bounded replay/buffers, slow-client handling and cleanup on close |
| Research regression | Council independent seats, pinned models, approved evidence, manual web seats and same-round isolation continue to pass |

No implementation-complete or live-collaboration claim until the actual two-agent execution scenario and UI checks pass. Hardware and installer acceptance remain independent release gates.

Reference: [Grok Bot overview](https://docs.x.ai/grok-bot/overview) describes named persistent Bots, parallel work, inter-Bot messaging, group context and ownership handoffs. The switchable-view requirement is the OnePane user's explicit choice, not an asserted Grok feature.
