## Manual offline Node custody command

The standalone go command under cmd/onepane-node-custody offers exactly two explicit operations: sign and verify. Both require the original signed instrument report, independently supplied enrolled instrument public key, private authenticated challenge and private human-approved plan. Neither command opens a network socket or attempts to run the original probe.

For sign, the operator supplies --data-dir pointing to the existing OnePane Node's data directory (containing keys/federation.crt and keys/federation.key), --node-id, --receipt, --challenge, --plan, --instrument-public-key and --output. The command **must not generate or rotate Node credentials** and exclusively creates a private 0600 output after signing. It refuses a missing/mismatched Node identity, stale nonce, off-target report or shared private file on POSIX.

For verify, the operator supplies --witness, --receipt, --challenge, --plan, --node-certificate, --node-fingerprint and --instrument-public-key. The trusted certificate and enrolled fingerprint must come from an independent channel. No private Node key is needed for verification. A signature is not trusted merely because the witness says which Node signed it.

Inputs are bounded (8 KiB canonical private JSON; 64 KiB Node PEM), regular non-symlink files, checked on the opened file descriptor after lstat. On Windows, owner-only NTFS ACL restrictions require explicit operator validation; POSIX file mode checks do not prove Windows isolation. All local report and plan files remain private and are never sent to the server.

This is a local, operator-controlled QA/evidence utility. It does NOT provide hardware host attestation, an autonomously running sensor or a server-accepted verified-health signal. The final human-centric UI/UX review remains scheduled LAST before any packaging or installed real Node QA.
