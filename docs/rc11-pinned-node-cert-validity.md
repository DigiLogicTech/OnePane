# RC11 — Pinned paired-Node outbound certificate validation

The Node-to-Node TLS client deliberately uses a human-approved pinned self-signed certificate instead of public CA trust. Its TLS configuration disables default chain verification, which also disables automatic certificate validity-period and extended-key-usage checks. The previous outbound VerifyConnection callback checked only the raw leaf SHA-256 fingerprint.

The new callback checks exactly one leaf, unchanged enrolled fingerprint, current NotBefore/NotAfter validity, digital-signature KeyUsage, and explicit server-authentication EKU. Failed checks reject transport before any authenticated HTTP measurement or private Project data can be sent. The existing peer mTLS and persisted permission checks remain separate and continue to govern all Workspaces, Projects, Nodes and Goals.

Important distinction: the five-minute node_pairings.expires_at field is used for enrollment confirmation, not active paired-Node lifetime. This remediation intentionally does not treat the old enrollment deadline as an active-pairing revocation event. Human revocation, current certificate validity and pairing state remain distinct controls.

Tests cover an enrolled OnePane Node leaf, wrong fingerprint, no/multiple certificates, expired and not-yet-valid leaves, wrong EKU and absent digital-signature key usage. This validates TLS certificate possession rather than hardware identity or device-health attestation; full physical Nodes and installed clients remain a separate QA gate.
