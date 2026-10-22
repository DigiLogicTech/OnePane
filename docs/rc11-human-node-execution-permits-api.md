## Human-only Node one-shot approval API

The active signed-session API exposes:
GET /v1/projects/{projectID}/continuous-goals/{goalID}/node-probe-execution-permits
POST /v1/projects/{projectID}/continuous-goals/{goalID}/node-probe-execution-permits
POST /v1/projects/{projectID}/continuous-goals/{goalID}/node-probe-execution-permits/{permitID}/revoke

The POST body requires instrument_key_id, challenge_id, peer_node_id, expected_goal_revision. Revocation requires expected_goal_revision and expected_permit_revision. The backend does not accept a caller-selected target IP, port, timeout, source, fingerprint, Project Workspace or physical trust class. These are derived transactionally from the original previously-approved instrument challenge, current target/pin and active paired Node. The endpoints do not execute any Node action and require an authenticated HUMAN actor with project.read/project.write accordingly. Anonymous, Agent and foreign-Workspace access is denied.

The returned JSON is sensitive metadata, not a server-signed portable token. It must be transported to the offline Node over an independently authenticated human channel; a revoked but previously exported grant remains locally usable until original short-lived challenge expiry, although server-side proof acceptance separately enforces current authority. No physical attestation or automatic Level 5 defence is claimed.