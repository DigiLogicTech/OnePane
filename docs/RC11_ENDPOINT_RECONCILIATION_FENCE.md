# RC11 — OCI endpoint route revision fencing

## Safety boundary

Workspace runtime reconciliation captures the ordered set of declared
application endpoints before launching the OCI application. An operator
can alter an Endpoint while the image is being pulled or the container
is being started. Previously, the reconciler might apply the old port
mapping and then publish it as a verified route for a newer Endpoint
configuration.

The reconciler now compares the complete immutable declarative endpoint
manifest (endpoint identities, owning Runtime/Application, revisions,
internal ports, protocol, path prefixes, exposures and desired states)
with freshly loaded persisted endpoints before an OCI ensure, after the
ensure, and before every verified route publication. Added or removed
endpoints and revision drift fail with ErrReconciliationDrift. Derived
statuses, URLs and observation timestamps are deliberately ignored so
that marking one route ready does not invalidate other endpoints.

## Durable publish fence

ApplyEndpointRouteCommand now includes a mandatory
ExpectedEndpointRevision. The Workspace service compares it with the
persisted Endpoint revision *inside the same write transaction* as
checking independent V2 evidence and persisting a verified route. Old
app/endpoint observations cannot be used to publish after the operator
changes the desired endpoint revision. Before each route, the reconciler
also revalidates origin-Node placement, current Runtime/Application
intent and the complete endpoint manifest.

Negative unit tests cover endpoint replacement, added/removed endpoints,
port/protocol/path/exposure/desired state drift and changed revisions.
Real SQLite integration verifies that a previously valid verified OCI
port observation fails if the Endpoint revision advances.

This bounds the existing local-first rootless OCI backend. It cannot
cancel a mutation already dispatched during a concurrent edit, and it
is not proof of physical Node acceptance or remote sandbox execution.
RC11 packaging still requires the comprehensive code review, vision
realignment and second review before user QA.

## Idempotency across Endpoint-only edits

An Endpoint declaration can change without incrementing the owning
application's revision. An OCI ensure keyed only to that application
revision would reuse a previously committed external Operation even
when the requested ports have changed. The ensure idempotency key
therefore also includes the SHA-256 of a canonical, application-scoped
declarative Endpoint plan. Ordering differences and unrelated
applications do not cause reexecution; a changed/added/removed Endpoint,
port, revision, desired state or exposure produces a distinct
operation. Tool mutation remains subject to normal authority,
independent inspection and V2 verification; no automatic replay of
unknown-outcome operations is introduced.
