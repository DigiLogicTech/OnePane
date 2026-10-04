# Budget enforcement and independent assurance

## Budget reservations

OnePane separates permission to use a protected route from capacity to consume it.
Routes classified as included-subscription, provider-managed, paid, unknown, or
otherwise not proven hard-zero-cost require a durable BudgetReservation before
external dispatch.

A reservation consumes capacity atomically through the complete parent-account
chain. Child accounts therefore cannot oversubscribe a Workspace/global parent.
Reservations may bind to a Task and then exactly one inference request or external
agent-runtime invocation. Successful usage is committed; pre-dispatch failures
release capacity; once dispatch has been attempted OnePane accounts conservatively
because external quota may already have been consumed.

Reservation expiry is fail-safe. Expiry selection excludes active Tasks/inference
requests/runtime invocations, and the same active-work condition is rechecked in
the expiry transaction to close the select/expire race.

## Independent V1-V5 assurance

A model saying `complete`, another model agreeing, or an adapter returning success
does not establish V1-V5. The Assurance service evaluates immutable evidence and
resolves the existing Verification object through the trusted verification service.

- **V1** — deterministic result/schema/invariant checks and/or integrity-verified
  immutable artifacts.
- **V2** — direct resulting-state Observation satisfying declared predicates and
  freshness requirements.
- **V3** — V2 plus a distinct integration Observation path whose source principal
  is independent of the worker that proposed completion.
- **V4** — V3 plus multiple evidence layers and at least two distinct evidence
  sources/paths. Model agreement alone never counts as a layer.
- **V5** — V4 plus explicit acceptance from an active human Approver/Admin. The
  acceptance is bound to the exact assurance evidence hash.

Evidence is workspace-bound and immutable. Observation integrity is rechecked;
artifact content is rehashed; evidence older than the latest applicable causal
boundary (Task attempt and/or Operation execution) is rejected. Observation provenance is actor-bound:
when a recording actor is supplied, the Observation source principal must be that
actor, so a worker cannot manufacture independent V3/V4 evidence by relabelling its
own probe as another principal. V5 acceptance is never reusable after evidence
changes.

Task completion remains `Verification PASS -> valid Checkpoint -> CompleteVerified`.
Operation mutation success remains `execute -> observe -> Verification PASS ->
commit`. The Assurance worker does not bypass either state machine.

### Crash recovery

Assurance evaluation itself performs no external mutation, so an interrupted
evaluation may be requeued. More importantly, if OnePane crashes after the
Verification has already been resolved but before Task/Operation finalization, the
resolved Verification remains eligible for finalization after restart. Finalization
is idempotent: existing committed Operations/valid Checkpoints/completed Tasks are
recognized instead of duplicated.

## Human acceptance API

The backend exposes `POST /v1/verifications/{verificationID}/acceptance` for V5.
Normal API/session authorization must grant `verification.accept`, and the
Assurance service independently requires the authenticated principal to be an
active human with the Approver or Admin role in the Verification workspace.
No WebUI dependency is required for this backend contract.
