# RC11: Human-approved paired-Node measurement transport

**Status:** P0 foundation, draft RC11 branch (2026-10-12). This is an independently authenticated peer **transport identity**, not measurement-device attestation, host compliance, trustworthy physical sensor data, confirmed home-network security or active L5 remediation.

## Scope

Existing Node federation uses pinned Ed25519-backed TLS certificates, two-sided user-approved pairing and a separate federation HTTP service. This implementation adds a distinct **off by default** narrowly approved transport path: a human first binds one exact active Level 5 Project/Goal, allowed monitoring-plan `source_id`, paired peer Node ID and its current TLS certificate fingerprint, with a time-limited grant. Remote peers cannot create this grant through their federation authority.

The local OnePane operator API now supports:

| Method and endpoint | Authority | Outcome |
| --- | --- | --- |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/node-measurement-grants` | Authenticated human with `project.write`; source permitted by Goal, peer currently and mutually paired | Approves one exact source/Node/Goal with bound mTLS fingerprint and expiry |
| `GET /v1/projects/{projectID}/continuous-goals/{goalID}/node-measurement-grants` | Human `project.read` | Last 100 safe grant metadata records |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/node-measurement-grants/{grantID}/revoke` | Human `project.write`, optimistic Goal and grant revisions | Revokes grant; existing audit and receipts preserved |
| `GET /v1/projects/{projectID}/continuous-goals/{goalID}/node-measurements` | Human `project.read` | Last 100 metadata-only immutable transport receipts |

Approval body, illustration:

```json
{
  "peer_node_id": "approved-paired-node-id",
  "source_id": "peer-link",
  "expected_goal_revision": 1,
  "expires_at": 1790000000000
}
```

Expires are checked against the current server clock: more than five minutes and no more than seven days. Revision fields must match the live Goal and the submitted grant for revocation. An existing approved source/peer/Goal cannot be silently replaced or duplicated. The trust boundary does not grant arbitrary Node operations, network scanning or Task actions.

The separate Node federation TLS listener accepts:

`POST /federation/v1/measurements`

The TLS client must demonstrate possession of the **current** enrolled peer certificate. The request is tied to a *live* paired Node, with both parties confirmed, and grant must still be active and match the exact peer certificate fingerprint. The certificate's validity dates must be current. TLS failure returns 401. Permission failures and revoked or mismatched grants return 403; replay returns 409, and a grant exceeding 30 accepted samples in the preceding minute receives 429 with `Retry-After: 60`.

Maximum request body is **2048 bytes**. The body must contain one valid unambiguous JSON document with no unknown/duplicate fields (including nested/escaped duplicates):

```json
{
  "protocol": "onepane-federation-v1",
  "grant_id": "human-approved-goal-node-grant",
  "sequence": 1,
  "signal_type": "peer_connectivity",
  "outcome": "reachable",
  "observed_at": 1790000000000
}
```

Only `peer_connectivity` and `reachable`, `unreachable` or `unknown` may be reported. The observed time must not be in the future, predate approval, or be more than five minutes old. The sender **cannot select** a Workspace, Project, actor, source, claimed device hostname, arbitrary origin Node, trust class, health verdict, scanning target, raw sensor values or actions. All these bindings are taken from the current human-approved grant and pinned TLS client certificate.

The Node federation writer makes one **serialised SQLite transaction** to recheck current Project/Goal/Workspace state, peer pairing, certificate fingerprint, human approver's live role/membership, active grant and freshness. It advances a strictly increasing signer-specific sequence and appends an immutable small receipt. The transaction also enforces 30 accepted samples per 60-second **sliding** window with an indexed bounded query. Duplicate, lower and replayed sequences do not advance the counter or persist new receipts. Ledger records and their events contain safe metadata, not raw network scan output.

## Explicitly NOT implemented by this stage

- A paired TLS connection proves only which registered **Node connection** sent a signal. It cannot verify that an external device was measured or that the Node's answer is truthful. A compromised paired Node may report any of the three allowed outcomes. This stage is a narrow and auditable signal **source**, not automatic green health.
- The transport receipts are **not** imported as trusted `observations` or Ed25519 **source-signed** receipts; this avoids silently upgrading a peer TLS sample into previously-approved local sensor trust. A future bridge must separately demonstrate correct origin/source provenance and retain conservative labels.
- No Node background sender or automatic polling is enabled, no scanning, detection, threat scoring, host administration or recovery occurs as a result of approval or receiving an envelope. L5 remains `configured` with health `unobserved`, not active monitoring.
- The outbound sender is an **explicitly invoked internal Go service call**, not background work: `nodefederation.Service.SendPeerNodeMeasurement(ctx, SendPeerNodeMeasurementCommand{RecipientNodeID, GrantID, Outcome})`. It uses the existing pinned federated TLS client and its local client identity certificate to POST the narrow measurement to an exact paired recipient. Migration `0076_node_outbound_measurement_sequences.sql` reserves a monotonically increasing per-origin Node/recipient/grant counter in SQLite **before network transmission**. Failed requests, timeouts and Node restarts do not recycle sequence numbers. A local Node is limited to 32 tracked grant counters per paired recipient to prevent unbounded accumulation from accidental unique grant IDs. Both pairing confirmations are rechecked before reservation; the receiver independently rechecks live pairing and the human Goal grant at admission. The exact grant ID is provided deliberately rather than auto-discovered from other Projects.
- **Not yet enabled:** automated outbound scheduling/polling, source-specific probe Tools, human-facing configuration for handoff of the grant ID, operator-authenticated Node startup workflows, and automated incident/remediation policy. The explicit sender reports only caller-supplied peer self-connectivity state; it does not run a physical network probe or establish third-party device truth.
- Source credential issuance, aggregate network budgets, incident correlation, postcondition-tested reversible remediation, operator consent, local-first soak testing, UI controls and Windows/Ubuntu installed QA still need further work.

## Validation and QA sequencing

Integration coverage uses actual sqlite migrations, active L5 Project, human review and a paired Node with real federation identities and certificate fingerprints. It validates direct HTTP authentication denial, unknown/repeated source fields, arbitrary claimed Node/device-health denial, sequence anti-replay, 30/min budget, immutable receipt data, revocation, Goal-health non-promotion and a **real local TLS client-certificate handshake**. A second two-Node/independent-SQLite integration test uses a real pinned TLS server and sender identity, persists a sequence across service recreation, and confirms that revoked grants deny further transmissions while keeping spent sequences. The operator Project API separately tests human-only grant creation/revocation, stale optimistic revision and read-only metadata scope.

Go/SQLite, frontend contracts and Windows/Ubuntu cross-platform CI are the current source-verification gates. They do **not** replace installed Windows MSI / Ubuntu service testing against actual devices.

Keep the agreed order: complete functional implementation and initial code/security audit; perform human-focused user-control/customisation and UI/UX review **last before the next QA build**; then final audit, CI and packaging. PR #227 remains draft; no release merge or installer has been authorised.
