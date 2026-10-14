# RC11 — Bounded per-Goal retention inventory (no erasure)

The human operator can inspect read-only historical record counts for an exact active Project / Goal / Project Workspace:

`GET /v1/projects/{projectID}/continuous-goals/{goalID}/retention-inventory`

The request requires both an authenticated **human** principal and Project **project.read**; it does not take action or expose another tenant's or Project's contents. The snapshot includes historic manual/recurring TCP attempts and grants, immutable reachability alerts, human incident reviews, non-executable response candidates and the append-only observer-fence audit-event count. It reports the current extra observer pause latch state, but **`running` does not mean observation is authorised or active**.

The bounded implementation uses a single SQLite read transaction, fixed scoped counts and the aggregate index; it never materialises historic receipts, endpoint/IP details, secrets, prompts, signatures or Tool inputs. It reports advisory flags at >=1024 local fence audit events, >=10000 TCP attempts or >=1000 review/candidate records. These are **operator-attention thresholds**, not byte estimates, protective quotas or a guarantee that sufficient storage exists. SQLite database pages and WAL are shared across Projects, so it explicitly returns `bytes_measured=false`.

**No automatic pruning, VACUUM, deletion of events, mutation of incident history or change to existing grants is performed.** Growing append-only evidence can eventually exhaust disk space; real long-term storage policy must be decided independently, with operator-reviewed export, verified integrity and permission-preserving tombstones as appropriate. Do not claim physical Node acceptance, external device health or automatic Level 5 remediation from this endpoint.

The remaining deliverables are a full installed low-RAM soak (measured RSS, WAL and disk usage), explicit operator data governance, physical Node/target association, security/code audit and the **LAST** human-centred UI/UX pass before an approved QA installer.

**Unknown-outcome quarantine visibility:** `unreconciled_observer_operations` counts only persisted exact-Project/Goal/Task observer pause/resume operations in potentially effected or unresolved states (executing, observing, verified, unknown, blocked, compensating and compensation-failed). It exposes an `observer_mutation_needs_reconciliation` advisory to the authenticated human. The inventory does not display Tool inputs, call `AcceptReconciledOutcome`, mint a new permit, retry an operation, turn on probes or attest an external device. A running process can have transient executing/observing states: this is an operator-attention signal, not a confirmed crash or security incident.
