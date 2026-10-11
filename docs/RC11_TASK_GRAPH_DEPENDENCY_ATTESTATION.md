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
flag in production. A future explicit graph review/reapproval operation
must validate and reissue an operator-approved manifest before any
automated resumption.

This is a control-plane integrity check, not proof that the underlying
Worker ran or a physical Node completed work. Rootless physical tests
and the two user-mandated release code reviews remain outstanding.
