# Durable Team assistance and original-owner continuation

Status: durable request/wait, bounded single-request/answer reads and immutable
authenticated human answer storage implemented. Explicit Continue and required
request-specific next-model context source are published as abebacc6. Bounded
session discovery and the input panel source are published as f7204def and pass
their exact-source hosted workflows as confirmed on26October2026.
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
Team-room human guidance remains separate from the implemented request-specific
durable answer/continuation protocol. Generic chat cannot answer a request and
generic Task resume is fenced from its current assistance wait.

The25October continuation reproduced six human/wait proposals parking Task,
Attempt and Worker after tenancy or sealed root/child criteria drift. The wait
transaction now repeats the shared exact active execution binding and accepted
Team/dependency/ancestor checks before any mutation. Actual handler authority
and rejected-write matrices preserve the original lineage and prior model
journal. Current active system principal and exact Workspace membership are
also required at preflight and atomic claim/continuation. The request half of
steps1/2 below is implemented. Bounded single-request reads in steps3/4 exist;
answer storage/writes are implemented as described below. Stored-answer
observation and steps5-7 now have source implementations described below,
including bounded discovery and the existing Team-room input panel.

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
Dedicated GET .../assistance/{requestID}/answer observes a comparable request+
answer pair in one transaction; two HTTP collections, current read permissions,
final active Project/named scope and original credential reauthentication precede
bounded48KiBprivate output. Missing answers are absent, not generated; malformed
retained rows require reconciliation. Observation changes no execution/answer/
audit/hold/accounting history. Existing browser authentication may separately
refresh session timestamps. The dedicated Continue endpoint and model-context
consumer are described below. Do not describe these answer boundaries as complete assistance or
permission to execute/recover/replay.

## Continue boundary contract

Task.ResumeInTransaction already checks Team holds, recovery, canonical scope
and dependency admission, but permits paused/dependency waits and a READY fallback
without an active Attempt. Add an approval-only exact-original-Attempt helper;
ResumeWaitingAttemptInTransaction is deliberately dependency-only. Continue must
reject manually paused Tasks and active Team/root holds, require task.write and
repeat featurepolicy.StoredScopes.AuthorizeTaskTx(...,"tasks.autonomous").

Use a narrow injected Worker transaction collaborator to check current Worker
authority/CAS and reuse journals. Atomically resume Task/original Attempt/Worker
with exact request/final answer/revisions, required journal/audits and retained
immutable applied-answer evidence. Avoid importing agentworker into team (cycle)
or duplicating Worker journal semantics. Generic Task.Resume plus syncResumedRuns
could reactivate an assisted human wait without an answer/Continue receipt.
The dedicated current-wait fence now denies generic SQL Task resume and the
preexisting running-Task/Attempt reconciliation projection, in the same
transaction as Worker promotion. It preserves ordinary non-Team operator resumes,
later ordinary waits and replacement Attempts. The fence has no Continue bypass;
implement the narrow positively authorized continuation transaction above.

Keep the applied-answer identity independent of continuation_json, which resource
waits, replan and tool handlers replace. Resolve required authored human input
through the exact latest committed human-wait/continuation/answer keys for the
original Worker, not optional worker-continuation context or the last8roommessages.
Fail on required-context overflow rather than silently omit the answer. Validate
actual wait -> authenticated reply -> explicit Continue -> same Attempt/Worker ->
answer in the actual next model context, including later room traffic, resource
wait replacement, stale owners, holds, recovery and every rejected journal/write.

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

## Explicit continuation source — 26 October 2026

Migration0113 retains one immutable applied-answer receipt per original wait,
with composite original request/answer identity, authenticated continuing human,
exact committed continuation journal and pre-transition revisions. It neither
backfills approvals nor modifies existing waiting/answer/history rows. Reviewed
indirect recovery vocabulary includes the receipt and human journal provenance;
this structural vocabulary grants no recovery disposition or replay authority.

POST .../assistance/{requestID}/continue requires current human team/task reads
and writes plus Project reads/writes when bound. Original bearer/browser
credential/expiry/revocation/scopes/roles/CSRF/membership are checked in the SAME
transaction as exact original waiting Task/Attempt/Worker, answer, current
autonomy policy, dependency/recovery/Team/root admission, revisions, transition,
Worker journal, receipt and audits. Manual pauses and holds cannot be released.
The narrow approval-only Task helper requires an original started waiting
Attempt and never falls back to READY or a new Attempt. Duplicate approvals
conflict without another execution. Postcommit credential denial withholds the
private receipt; it cannot undo a committed continuation.

Required context resolves the latest original human wait through a partial
run/step index and exact request/answer/receipt/journal keys. SQL bounds precede
materialization; a malformed latest step produces an error, never an older
answer. Question and authenticated human input have separate authored provenance.
The answer survives later room traffic and resource-wait continuation replacement.
Required overflow, revoked author/continuing human, original scope drift or a
new unanswered wait fail closed. Rechecks occur at transactional dispatch claim,
deferred transport invocation and successful-response proposal application.

Source tests exercise actual human handler -> retained domain answer -> Continue
-> reconstructed original Worker -> production inference gateway/HTTP transport
-> next model request, with separate production bearer/browser HTTP credential
tests. The transport is deterministic; this is neither physical AI nor installed
acceptance, nor a single authenticated product/UI end-to-end acceptance run.
Bounded session discovery and the input panel are described below.

## Current-request discovery and input panel source — 26 October 2026

GET /v1/team-sessions/{sessionID}/assistance returns at most eight current
requests. Discovery starts from the accepted assignment graph and follows the
latest exact original Attempt/Worker/committed human wait to its unique request;
unrelated newer history and room traffic cannot evict pending work. Root/child
seals, accepted manifest/graph, frozen/current member binding, active scope and
reader membership are checked in one transaction, including an empty result.
The endpoint repeats comparable collections, canonical root scope/revision,
conditional Project permission and original credential checks before private
128KiB output. Discovery changes no assistance/execution/audit/accounting rows.

The existing Team room mounts a Needs your input panel showing the named
contributor and actual question as text. Save reply stores one final reply;
Continue original task is a separate explicit action after a saved reply is
observed. It retains current original-owner IDs/revisions, suppresses duplicate
submits and never retries execution automatically. Unknown outcomes direct the
operator to inspect the saved reply/current work. Existing replies can be loaded;
a genuine missing-answer404 preserves the draft, while denied reads clear private
state. The shared API helper retains HTTP status without changing CSRF behavior.

Drafts survive ordinary polls, revision changes and holds. Same session/plan
assignment or member replacement, scope changes, denied reads and disposal
invalidate retained data and in-flight responses. Room snapshots prevent external
in-place mutation from evading these checks. Retained request revisions never
move backward on delayed list/answer responses. Unchanged answer retries retain
their submission identity; a newer waiting revision creates a fresh submission
identity and still cannot replace a committed final answer. Required permissions,
holds and exact CAS remain server-enforced.

Node controller and mounted renderer tests cover inert markup, byte bounds,
draft/focus/selection retention, actual labels and disabled pending controls,
separate Save/Continue, revised holds, duplicate operations, both late-response
orders, source API HTTP statuses/CSRF, scope replacement and disposal. This is
source UI evidence. Final page-by-page Windows/Ubuntu product review, physical
inference/hardware and installed acceptance remain outstanding.

Combined source integration additionally exercises both real bearer and browser
HTTP authentication: after an explicitly synthetic bootstrap wait, public Worker
Tick and production loopback inference create a new actual human question and
request. Authenticated discovery/answer/Continue retains the original owner and
exact receipt. A reconstructed Worker includes exact authored question/answer
and continuation provenance in its actual next model request after12later room
messages. Another waiting Tick dispatches nothing without another approval.
This closes the separate-backend-flow source-test gap; it remains deterministic
source evidence and does not establish browser, physical AI or installed QA.
