# RC11 — Human-declared TCP target provenance (P0-K)

**Status:** implementation on draft, unmerged RC11 PR #227 (2026-10-12). This stage establishes **operator-assigned target association**, never proven external physical-device identity, sensor authentication, network trust or autonomous Level 5 protection.

## Why this needs an independent boundary

A paired Node with valid pinned mTLS proves who **sent the peer self-report**, not what outside device was actually measured. A local TCP connection to an approved RFC1918 IPv4 literal and port proves only a socket handshake result from the local probing environment. The two streams do not become reports about the *same* physical asset merely because they appear in the same Goal or Project.

The registry added in migration `0078_goal_tcp_declared_target_bindings.sql` lets a human explicitly name an already-authorised **TCP endpoint**. The approved identity class is `operator_declared_tcp_endpoint`. It is deliberately **not** `verified_device`, `hardware_attested` or a signed external device identity.

## Operator API

The following APIs require authenticated human and canonical Workspace `project.read` or `project.write`:

| Method | Path suffix under `/v1/projects/{projectID}/continuous-goals/{goalID}` | Permission |
| --- | --- | --- |
| POST | `/tcp-declared-targets` | human `project.write`; checks current exact approved human TCP permit, source allowlist/plan hash, reviewer membership, Project/Goal revision |
| GET | `/tcp-declared-targets` | human `project.read`; bounded last 100 historical metadata records, excluding exact target IP and port |
| POST | `/tcp-declared-targets/{targetID}/revoke` | human `project.write`; optimistic Goal and target revision checks |

Example approval body (identifiers/timestamps are illustrative):

```json
{
  "permit_id": "existing_human_tcp_permit_id",
  "display_label": "Router uplink",
  "expected_goal_revision": 12,
  "expected_permit_revision": 1,
  "expires_at": 1791790000000
}
```

The source, target IPv4/port, timeout, plan hash, canonical Project Workspace and human TCP permit are **derived server-side**, not supplied in the declaration request. The label is an operator-specified short, restricted ASCII name (1–64 characters), not a hostname or automatic identity claim. Only exactly one active declaration may be attached to a permit at once. Expiry is no more than 24 hours and **never later** than that permit's expiry. The stored identity scope is immutable; revocation can stop future attribution but does not erase history. Exact IP/port are retained privately only for equality enforcement and omitted from the declaration JSON response and triage.

The new triage `tcp_declared_target_id` and `tcp_identity_class` fields appear only if the latest completed TCP receipt was recorded **on or after** that human identity declaration, the exact permit and target still match, the Goal plan remains unchanged, the target/permit remain in force, and the approving human remains an active member. A pre-declaration TCP receipt cannot retrospectively inherit an identity. Following revocation or role withdrawal, the target ID disappears from *current* triage without deleting the historical TCP receipt. The connectivity status remains independently visible.

**Paired-Node self-reported connectivity receives no target association from this registry.** It remains a distinct `peer_connectivity` transport assertion. Source IDs and coincident timestamps are not sufficient to correlate the remote peer and the declared TCP endpoint. The existing informational investigation indicator still cannot infer a shared target, compromise or security incident.

## Security and resource properties

- Existing manual permit restricts the only supported network target to **one** private IPv4 literal and TCP port, with a hard timeout of 100–1500 ms. Declaring a label opens **no socket** and starts no agent, Task, polling or scan.
- Human reviewer must hold live membership at both original permit approval and target declaration. An Agent with Project write privileges is not a valid identity registrant; public APIs reject it even if other capability checks pass.
- SQLite enforces one active declaration per TCP permit, bounded metadata-only listing, strict scope immutability and non-deletable audit. Optimistic Goal revision changes on declaration and revocation. Existing TCP receipts remain immutable.
- The declaration is **not proof** that a physical device occupies that IPv4 address, that DHCP lease ownership is stable, that a certificate presents a trusted name, or that the socket's peer is the intended device. DHCP/IP reassignment and shared proxies mean future real-device identity requires a separately verified instrument attestation plus explicit same-target subject binding.

## Tests and next work

The Go/SQLite/HTTP integration test verifies human-only approval, cross-tenant/Agent denial, duplicate key rejection, one active permit binding, restricted labels, hidden target values, before/after declaration chronology, exact triage association, human revocation without deleting TCP evidence, immutable stored scope and Goal health remaining `unobserved`.

CI validates source/API integration, Go regression, frontend contracts and Windows/Ubuntu cross-compilation. CI does not validate physical device identity or installed Windows/Ubuntu Node operations.

**Remaining P0:** independently authenticated and explicitly consented instrument/target evidence (e.g. a pinned device application certificate or tightly-scoped sensor signed measurement), user-approved same-target source correlation and network-safe sampling, calibrated confidence, operator-approved reversible remediation with postcondition/rollback, low-resource endurance, full code/security review. Human-focused UI/UX/control/customisation review is still the final development workstream before a QA build.
