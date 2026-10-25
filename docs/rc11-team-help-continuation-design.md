# Durable Team assistance and original-owner continuation

Status: source-reviewed dependency design,25October2026; not implemented.
The release execution manifest remains authoritative for open gates. This is
human assistance for an accepted contribution; peer execution, new helpers and
versioned reviewed reassignment remain separate approved work.

## Existing functional boundary

Agent Protocol `human` proposals already carry a question. The actual execution
Worker's `handleHuman` uses `waitRunAtomically`: original running Task/Attempt,
Worker continuation, waiting journal and audits commit together or all roll
back. The request is currently retained only in Worker continuation. Team-room
human guidance exists, but no request-specific durable answer/continuation
protocol does. Do not represent generic chat or a generic Task resume as that
complete protocol.

The immutable0098accepted assignment/root cannot be reopened or replaced in
place. Existing shared admission applies to Task start/resume, each Worker
step and downstream dispatch. An assistance answer must never release a pause,
cancellation, recovery, dependency, capability, routing or verification fence.

## Next implementation slice

1. Add immutable request and append-only answer storage. A request binds the
   exact accepted session/plan/assignment/member, Task/original Attempt/Worker,
   committed `human` wait journal identity, bounded question and author
   provenance. A unique wait-step identity makes redelivery idempotent. Update
   the reviewed recovery ownership contracts for the new Task/Attempt/Worker
   references; failing those contracts is a required review signal.
2. Materialize request, trusted room notice and audit inside the same
   transaction as the existing wait, using a narrow shared transaction helper.
   Derive Team identity from persisted immutable assignments, never from
   model-supplied session/member IDs. Ordinary non-Team human waits retain
   their behavior. Failed wait/journal/message/audit leaves no help record.
3. Add bounded indexed request reads and human answer writes to the Team
   service. Require exact manifest, scope, sealed criteria and profile identity
   plus current active membership. A reply may be recorded while paused, but
   cannot resume work. Keep questions/answers as untrusted text with explicit
   authored provenance, and never impersonate agent discussion.
4. Expose dedicated session-scoped GET and answer POST endpoints. Reads require
   current team.read/task.read, retain original credential/principal/type and
   reauthenticate immediately before private no-store publication. Answers
   require authenticated human team.write; explicit Continue additionally
   requires task.write and ordinary admission. Reject caller-supplied authors.
5. Explicit Continue binds exact request/answer/current Task revision and
   original waiting Attempt/Worker, updates continuation and commits all
   required journals/audits atomically. Reuse the trusted waiting-approval
   Task resume path; `ResumeWaitingAttemptInTransaction` intentionally accepts
   dependency waits only. Never create a new Attempt or bypass Team holds.
6. Feed the exact accepted answer into the next model context through a
   bounded request-specific source. More than8later generic room messages
   must not evict it. Context consumption retains human provenance and must
   not convert model text into human authority.
7. Show a bounded Needs your input panel in the existing Team surface with the
   named contributor, actual question and explicit Reply/Continue behavior.
   Preserve draft text through polls, suppress duplicate requests, clear data
   on denied reads/Workspace changes/close and reject late stale responses.

## Required source evidence

- Foreign tenant/session/plan/member/Task/Attempt/Worker, changed Project/named
  Workspace/artifact session, sealed criteria/profile/dependency/checkpoint
  drift, duplicate owners and stale revisions fail before writes.
- Pause/cancel/recovery or stale returned model proposals cannot create an
  unauthorized request or release execution. Answering while held preserves
  the hold; continuation uses the original owner after admission.
- Revoked human/system principals, credentials/membership/capabilities and
  nonhuman answers fail closed. Reauthenticate reads after collection and
  test identity replacement, not just initial authentication failure.
- Request/answer/room-message/Task/Attempt/Worker/step/audit rejected writes
  roll back together as appropriate. Cancellation and unavailable DB errors
  propagate. Reconstructed services/redelivery retain one request/answer and
  never create a second execution or refund unknown/spent accounting.
- Long history actual production plans, raw-row/entry/UTF-8/whole-response
  budgets and overflow refusal precede materialization. Global latest-message
  filtering is not a complete request list.
- Actual Worker human proposal -> durable request -> authenticated answer ->
  explicit original-owner resume -> answer in actual next model context is
  required before this slice is implemented end to end. Deterministic HTTP
  transports are source integration evidence, not physical model acceptance.
- UI validators cover injection, duplicate clicks, retained reply drafts,
  scope/request replacement, late replies, access denial and disposal. Final
  Windows/Ubuntu browser/product review remains separately required.

No schema or execution authority is added by this design. All broader Team,
recovery, code/security review, physical and installed acceptance gates remain
open until independently implemented and validated.
