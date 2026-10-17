# RC11 — Node federation identity preservation and refusal of partial keys

**Status:** added in draft PR #227, 15 October 2026. This is a credentials-on-disk safety gate, not proof of physical Node device or network target ownership.

`nodefederation.EnsureIdentity(dataDir,nodeID)` now checks the existing federation certificate and private key as one inseparable pair. When both are absent, a new Ed25519 keypair is written exclusively to the private `keys` directory (private key first; certificate second, with file synchronization). If only one file exists, the caller receives an error; **OnePane does not recreate, truncate, rotate, or overwrite the surviving credential**. An interrupted first setup remains visibly incomplete, requiring explicit operator recovery rather than silent identity rotation.

For installed existing pairs, `Lstat` rejects symlinks and non-regular credential files. On Unix, the identity directory must not grant group/other access and the private key must not grant group/other permissions; on Windows, POSIX mode bits do not attest NTFS ACLs, so ACL acceptance is still a separate installed Windows QA gate. Exclusive creation refuses races that add files between preflight and persistence. The Node-ID/certificate fingerprint pin and private key matching continue to be validated by the existing loader and custody checks.

Tests cover stable same-Node fingerprint, different Node-ID denial, missing certificate, missing private key, public/group-readable private key, and linked private key with unchanged out-of-directory contents. No application model/project database, existing credentials, network service, machine process or installer is modified by the tests.

**Remaining acceptance:** actual Windows NTFS ACL verification and installed upgrade recovery, host ownership and physical device/vantage identity, controlled reversible Level 5 actions, low-memory endurance, full source/security review and LAST human-focused UI/UX alignment before an approved QA build.

## Node identity input discipline (2026-10-15)

Before creating a new key directory, the federation identity loader validates that a Node ID is non-empty, at most 128 UTF-8 bytes, has no leading/trailing Unicode whitespace and contains no control characters. Invalid Node IDs fail without filesystem writes or permanent credentials; a specially crafted ID cannot make an unusable ten-year certificate that a later custody operation rejects. Regression confirms invalid IDs have no side effects. This guard does not prove an endpoint's ownership or the physical identity of the hosting device.

## Exact checked-descriptor identity loading (2026-10-15)

The persistent federation identity loader now reads both PEM files through separate bounded descriptors, comparing the opened file's identity with the previously checked `Lstat` metadata before reading. Each certificate/key file is capped at 16 KiB and must remain regular; the Unix private-key owner-only permission is checked again on the opened file. The loader uses `tls.X509KeyPair` over those **same checked bytes** instead of invoking a new pathname-based loader that could follow a raced symlink or see a different credential pair. Regression verifies stable original fingerprint, valid paired credentials, rejection of oversized certificate/private-key files, restored files and linked certificate denial. This reduces filesystem races but does not defend against a fully compromised account concurrently mutating an already-open file; Windows NTFS ACLs and actual installed host acceptance remain separate release gates.
