# RC11 P0-O — Explicit one-shot local instrument runner

**Status:** implemented source in draft OnePane PR #227, 13 October 2026. No installer, recurring monitoring, physical device/vantage attestation or automatic network defence is enabled. This is an opt-in Go CLI for operator-mediated testing of a single human-approved private TCP endpoint.

## Design and effect boundaries

Executable: `cmd/onepane-instrument-probe`, package `internal/instrumentprobe`. The runner has three modes: `keygen` (generate local Ed25519 instrument key), `probe` (one TCP connect and locally signed receipt), and `sign` (compare the original receipt with an authenticated OnePane server-prepared measurement envelope and create a detached-signature admission request).

**The private Ed25519 instrument key never goes to OnePane or is passed on the process command line.** Keygen creates a new file with exclusive creation and owner-only POSIX mode `0600`; the runner refuses group/world-readable private key files on POSIX and refuses to overwrite an existing key. On Windows the operator must also set a restrictive NTFS ACL for the intended instrument account; Go's POSIX file permission bits are not a Windows ACL. Only the public key (base64) and its SHA-256 fingerprint are shown for independent human enrollment via the existing OnePane instrument-key approval API.

The `probe` mode accepts an existing unconsumed OnePane server challenge JSON, explicitly specified current human-approved target ID, **one** private IPv4 literal, port and 100–1500ms timeout from the original permit. The operator must cross-check the IP/port against the approved permit **out of band**; these values are not asserted by the server's challenge payload and OnePane CANNOT verify that a manually entered IP is the intended device from this receipt alone. The runner does not use arbitrary DNS, public/loopback/link-local/CGNAT targets, network ranges, UDP, HTTP, application data, redirects, subprocesses, remote shell commands, retries or scheduled jobs. It makes at most one `tcp4` dial, closes the socket immediately and signs a local receipt identifying the challenge, operator-selected target, observed outcome/time and confirmation ID.

The signed local report contains the literal private IP and port **only on the operator's instrument computer**; it must be saved in a private local file and **not uploaded** to OnePane's measurement API, which stores only the constrained signed claim. The command cannot attest a trustworthy physical network vantage, actual hardware identity, absence of malicious code or independently validated observation. An operator with the instrument's signing key can still manufacture a valid-looking self-report without this official runner; neither a software signature nor a CLI printout is an attestation.

## Manual operator workflow

Use a trusted, already installed Go toolchain on the intended instrument machine. No automatic remote deployment or privileged install is performed.

```bash
go build -o onepane-instrument-probe ./cmd/onepane-instrument-probe
./onepane-instrument-probe keygen --private-key-out ./instrument-private.key
```

Take the displayed **public key** through a separately trusted enrollment channel to the existing human-only `POST /v1/projects/{projectID}/continuous-goals/{goalID}/instrument-keys` API. OnePane keeps the key associated with the current TLS pin, declared target, canonical monitoring plan and human approval. Never send the private file as part of enrollment.

An authenticated human issues `POST /instrument-keys/{keyID}/challenge` (expected Goal/key revisions). Save the unmodified, compact JSON server response to a local challenge file. The challenge is limited to **two minutes**; do not attempt to reuse an expired/consumed challenge. Run the one-shot probe against the literal private address and port already approved for that exact declared target:

```bash
./onepane-instrument-probe probe \
  --challenge ./challenge.json \
  --private-key ./instrument-private.key \
  --ipv4 192.168.42.71 --port 443 --timeout-ms 700 \
  --confirm-target-id YOUR_APPROVED_DECLARED_TARGET_ID \
  --out ./local-probe-receipt.json
```

This private local report is itself Ed25519-signed immediately after the single TCP connect, with no user-provided outcome override. An authenticated human then calls the existing **`/instrument-keys/{keyID}/measurement/prepare`** API using `challenge_id`, `outcome`, `observed_at` copied from the report, and the current `expected_goal_revision`. Save the unmodified, compact server JSON response into `prepared.json`.

```bash
./onepane-instrument-probe sign \
  --receipt ./local-probe-receipt.json \
  --prepared ./prepared.json \
  --private-key ./instrument-private.key \
  --goal-revision YOUR_CURRENT_GOAL_REVISION \
  --out ./measurement-accept.json
```

The `sign` mode verifies the original locally signed report using that key and checks the *exact canonical server-generated v1 measurement bytes* for matching Project/Goal/key/challenge/target, outcome, source, observed time and TLS pin ID. It refuses wrong keys, noncanonical/modified server bytes, a changed outcome, target ID, reported time, expired challenge or a false `observer_vantage_verified` claim. The output contains the detached signature and permitted import fields **without the local IP/port**. The authenticated human sends that output JSON to **`POST /instrument-keys/{keyID}/measurement/accept`**. The OnePane server independently rechecks all live approvals, exact source/target policy, challenge status and Ed25519 signature, atomically consumes the challenge and stores an immutable proof.

None of these steps marks a Goal healthy, verifies a physical device, proves an independently trusted measurement vantage, establishes correlation with a paired Node, counts as a security incident, initiates background Tasks or authorises protective remediation.

**Operational limitation:** the current challenge lifetime is only two minutes. The manual copy/prepare/sign/accept sequence is intentionally a QA/developer reference workflow and can be awkward to complete within that period. The later UI/UX and instrument integration must support a carefully scoped, authenticated one-shot handoff without exposing a broad human Project.write token to an unmanaged instrument or storing instrument private keys in OnePane. Do not extend nonce lifetime, silently retry, or automate account-authorised effects merely to make a demo appear successful.

## Source verification and remaining P0

The hermetic `internal/instrumentprobe` tests cover the exact single TCP4 address/no retries; unsafe IPv4, DNS/IPv6/loopback/public/link-local/CGNAT denial before dial; expiry, cancelled context and consumed challenge denial; immediate locally signed receipt; prepared server payload validation; changed outcome, altered canonical signature bytes, wrong key and false vantage rejection. CLI tests cover exclusive private-key creation, no replacement, POSIX owner-only bits and invalid-key rejection. CI cross-compilation includes Windows and Ubuntu but is **not proof** the runner was installed and performed a real external network check.

Next P0: independently bind the instrument signing identity to a trusted, enrolled physical/paired Node runtime with verifiable execution evidence and a verified source-to-physical-target association. Only then consider calibrated cross-vantage incident confidence and human-approved reversible L5 actions with rollback. Low-RAM endurance, full security/code review and **human-focused control/customisation UI/UX last before next QA build** remain open.
