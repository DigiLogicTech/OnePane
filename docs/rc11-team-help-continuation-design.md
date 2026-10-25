# Durable Team assistance and original-owner continuation

Status: durable request/wait, bounded single-request read and immutable human
answer storage plus authenticated answer POST source implemented,25October2026.
Explicit Continue, stored-answer observation, session request listing and
request-specific context/UI remain unimplemented.
The release execution manifest remains authoritative for open gates. This is
human assistance for an accepted contribution; peer execution, new helpers and
versioned reviewed reassignment remain separate approved work.

## Existing functional boundary

Agent Protocol `human` proposals already carry a question. The actual execution
Worker's `handleHuman` uses `waitRunAtomically`: original running Task/Attempt,
Worker continuation, waiting journal and audits commit together or all roll
back. Migration0110 adds immutable request history for accepted contributions,
binding the returned exact waiting journal identity, original Task/Attempt/Worker
and persisted accepted assignment. A trusted system notice and request audit
commit inside that same transaction. Questions have a UTF-8-safe512byte bound.
Migration0111 records the exact nullable Project/named Workspace/artifact-session
scope in that same transaction, with retained immutable history and no backfill.
Old requests without original scope remain unavailable for reconciliation.
The dedicated request GET rechecks accepted identity, original waiting owner,
current membership, frozen policy, bounded private metadata and original scope;
it repeats credential and active Project/named scope checks before publication.
Team-room human guidance exists, but no request-specific durable answer/
continuation protocol does. Generic chat or Task resume does not complete it.

The25October continuation reproduced six human/wait proposals parking Task,
Attempt and Worker after tenancy or sealed root/child criteria drift. The wait
transaction now repeats the shared exact active execution binding and accepted
Team/dependency/ancestor checks before any mutation. Actual handler authority
and rejected-write matrices preserve the original lineage and prior model
journal. Current active system principal and exact Workspace membership are
also required at preflight and atomic claim/continuation. The request half of
steps1/2 below is implemented. Bounded single-request reads in steps3/4 exist;
answer storage/writes are implemented as described below. Stored-answer
observation, list discovery and steps5-7 remain required.

## Current answer boundary

Migration0112 retains one final immutable authored answer per request. This first
answer API deliberately refuses conflicting replacements; identical original
actor/submission/text/revision retries return the retained answer while its
original contribution remains waiting. The service shares the bounded request
reader within its write transaction, revalidates original scope/accepted graph/
current active human membership and exact Task/Attempt/Worker/wait-step/revisions,
then commits answer and audit together. It adds no room-message duplicate and
never resumes execution, releases holds or alters accounting.

POST /v1/team-sessions/{sessionID}/assistance/{requestID}/answers derives its author
from the current human credential and requires team.read/task.read/team.write,
with project.read only when a Project is bound. A narrow query-only verifier
rechecks the original bearer credential or browser session, expiry, CSRF, current
scopes/roles and exact active tenant membership in the SAME supplied transaction.
It never invokes the session-refreshing authenticator inside that transaction.
The verifier runs for retries too. Raw JSON has32KiB, authored text4096UTF8bytes,
submission key128bytes and identifiers256byte limits. Text remains untrusted.
Final response reauthentication can withhold a private response after a successful
commit; an authorized identical retry retrieves the retained answer. This does
not promise continuous authorization after commit/publication.

Current GET request responses remain unchanged and contain no stored answer.
There is no Continue endpoint or model-context consumer. Do not describe this
answer boundary as complete assistance or permission to execute/recover/replay.

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

The new request schema adds no execution or human decision authority. All broader Team,
recovery, code/security review, physical and installed acceptance gates remain
open until independently implemented and validated.
