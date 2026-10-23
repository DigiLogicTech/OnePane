# Live collaborative Team mode — completion contract

User requirement confirmed 23 October 2026: Team mode should provide named agents working together visibly in real time, similar to the collaborative Grok Bot experience. The user explicitly selected **both a shared conversation and individual live agent panels, with switchable views**.

This extends the existing Team product contract. Continue to preserve current working Team/Council records and execution controls during the transition. The source progress below is separate from integrated execution and installed acceptance.

## Source progress, 23 October 2026

Implemented the canonical Task Inspector entry to a shared live room with switchable Conversation/Agents views, retained drafts and selected reply targets. Read polling is bounded, reauthorizes every request, stops on close/Workspace change, and clears inaccessible data on failure. Stable storage-assigned message sequence supports long-room pagination and newest worker context even after wall-clock rollback. Named panels show actual turn and correlated shared-compute state; they do not simulate streaming or tool execution.

Team chat must operate alongside the existing application: hot swap, Colibri, global queuing, model residency, CPU/GPU admission, cancellation, budgets, routing policy, Node trust, Research seat pinning and Workspace isolation. It must use the shared inference/runtime path. A separate Team model loader or independent capacity queue would violate this requirement. Agents waiting for compute are not represented as executing inference.

The existing path is Team worker → scheduler → shared inference service → global runtime coordinator → managed runtime transport/supervisor. Exact session/member/turn correlation now projects the worker's durable compute queue into the room. A real migrated-SQLite test calls Team dispatch while another task holds the same compute pool, checks that it queues with its Task identity, observes the Colibri/CPU claim in the live room, and cancels before any model transport. Shared coordinator tests also cover independent Nodes and simultaneous admission/cancellation; cancellation must not strand a lease. These use synthetic deployments, not physical Colibri engines.

Still required: governed per-member execution assignments/handoffs and artifact ownership; fair resource-admitted concurrent teamwork; precise waiting-for-runtime/swap/loading observations where the supervisor emits them; explicit pause/cancel/reassign controls; two-agent end-to-end execution; actual hot-swap/Colibri contention with ordinary chat/Project workers on physical hardware; final browser/product UI and installed Windows-first acceptance. Existing queue records currently mark admission as running before runtime loading, so the UI must not infer a finer residency state from that record.

## Required experience

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
