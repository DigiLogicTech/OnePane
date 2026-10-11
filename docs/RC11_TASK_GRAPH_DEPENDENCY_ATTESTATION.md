# RC11 — operator-approved Task DAG dependency integrity

Migration `0052_project_task_graph_approved_dependencies.sql` adds an
independent, durable approved-predecessor snapshot to each graph node.
Every new operator-approved graph records the original predecessor keys
atomically with its Task, dependency edges, audit event and outbox work.

When a graph is observed, OnePane compares every live hard dependency
against that approved snapshot. A deleted predecessor, newly added edge,
invalid JSON, duplicate predecessor or mismatched key is reported as
`dependency_drift=true`, `needs_attention` and a human recovery-review
checkpoint. A healthy independent branch remains unaffected. The graph
reader does not repair SQL or restart any external Task.

**Historical graphs:** older nodes created before migration 0052 lack
a separately attested original predecessor manifest. Their new
`approved_dependencies_recorded` flag remains **0**. The migration
does **not** guess the originally authorized plan from the mutable live
dependency table or assert the old graph hash proves its current edges.
Such nodes are marked `dependency_drift` and require human review;
existing Task data is retained. Do not manually toggle the attestation
flag in production. An explicit, human-authenticated legacy graph review operation is now
available at `POST /v1/projects/{projectID}/orchestrator/task-graphs/{graphID}/reattest`
with `task.write`. It requires:

- `confirm_operator_review: true`;
- `expected_manifest_sha256` copied from the ORIGINAL graph;
- `nodes`: the complete original reviewed graph node specifications,
  including names, objectives, Workspace IDs, ordered dependencies, priority
  and compute preferences.

The server reconstructs the SHA-256 manifest, compares the submitted plan
against the original graph hash and all persisted Tasks, live predecessors,
active Workspace ownership and placement settings in a single transaction,
and refuses any graph with attempted work or external operation records.
It does not add, delete, reroute, reset or replay Tasks or dependency edges.
A successful review atomically stores node attestations, one immutable human
review receipt (migration 0053), and a scoped audit event. A replayed review
is rejected. This is not a generic 'accept current edges' button. The
operator must possess the original plan; if it cannot be reconstructed,
create a separately approved replacement graph through the existing API,
and review the old Tasks independently.

**Authoritative admission:** the Task service now repeats this check
inside the same SQLite transaction as `Task.StartInTransaction`, before any
Attempt or `task.started` event is inserted. A deleted edge, an unattested
historical graph, a newly inserted unapproved predecessor, or an archived
Workspace cannot make a Task runnable merely because a UI/Orchestrator
snapshot is stale. Ordinary non-graph Tasks keep the existing Task lifecycle.
A Task graph's read-only recovery checkpoint is not an admission lease.

This is a control-plane integrity check, not proof that the underlying
Worker ran or a physical Node completed work. Rootless physical tests
and the two user-mandated release code reviews remain outstanding.
