# Windows UI QA 5 startup failure — 29 October 2026

## Result and cause

**Confirmed startup blocker in UI QA 5.** The package's backend and service hashes match the files observed during installation. Windows service startup reached SQLite migration completion, then failed before HTTP readiness with:

```text
bootstrap: bootstrap key DACL contains unexpected grants
```

`privatekeyfile.descriptor` constructed full-access grants for SYSTEM, Administrators and the current process user. Under the installed LocalSystem service, the current user is already SYSTEM (`S-1-5-18`), so the intended descriptor contained SYSTEM twice. Applying that descriptor through Windows merges the duplicate grant. The verifier compared the exact ACE multiset and rejected the equivalent canonical result. This explains why interactive-user QA passed while service startup failed.

A real Windows descriptor round-trip regression reproduced the original failure for SYSTEM and Administrators. The source fix avoids adding a duplicate current-user grant when that SID is already in the allowlist. It retains the protected DACL and the same authorized principals; it does not weaken key privacy or rotate key material.

## Installer rollback and retry condition

The service was present and stopped during the first inspection. The user then confirmed they cancelled the installer, explaining the subsequent removal of the service registration. This was a failed installation followed by rollback, not a successfully installed service being removed by QA.

Observed MSI-selected paths were:

- Data root: `C:\ProgramData\OnePane`.
- Model pool: `D:\OnePane\models`.
- Projects: `D:\OnePane\Projects`.

These differ from the previously discussed `D:\OnePaneData` selection. QA did not change them.

The failed initialization left `C:\ProgramData\OnePane\data\keys\node.identity` at **zero bytes**. Permission metadata confirms SYSTEM and Administrators grants. No key contents were read. A corrected executable must still reject an existing empty key rather than silently regenerate an identity or Vault key. Retrying against that partial root therefore requires preserving and inspecting the partial state, then explicitly handling the empty initialization artifact, or choosing the intended fresh data root while preserving the old root. QA has not removed, replaced or changed that key, configuration or database.

## Validation and evidence

- Original regression fails with expected ACEs `BA, SY, SY` but actual `BA, SY` (and the corresponding duplicate-Administrators case).
- Fixed private-key, node identity, Vault and Windows service native test suites pass.
- The complete private-key suite also passes as LocalSystem (`S-1-5-18`), including stable key preservation, broad-ACL repair and hard-link rejection. The diagnostic scheduled task was removed after use.
- Complete corrected backend bootstrap also passes as LocalSystem in a new disposable root: database migration, node and Vault initialization, HTTP health `ok`, and first-run setup `required: true` with no human accounts. Both generated key files are 32 bytes. The diagnostic backend was stopped and the scheduled task removed. This validates the service-account backend startup boundary; it does not claim a completed MSI or WebView2 desktop installation.
- Original startup stages show configuration OK, bootstrap begin, migration complete, bootstrap failure and failed service readiness. SCM reported error 7023; that generic service error was not the root cause.
- Protected logs were read through a narrowly scoped elevated diagnostic. No permission was broadened to expose the installed key or service log.

Evidence is preserved under the parent workspace's `.tools/installed-startup-qa-20261029/evidence`: `protected-startup.json`, `descriptor-baseline-repro.log`, `fixed-key-tests.log`, `system-tests.json`, `system-tests.log`, and `system-task-cleanup.json`. Full backend LocalSystem bootstrap validation, when present, is recorded separately in `bootstrap-system.json`; do not infer a successful installed UI or MSI acceptance from the key suite alone.

The source fix is local and uncommitted. The tested backend identifies itself as `0.1.0-alpha.3.3-ui-qa.6+startup-fix`, revision `787274d05cc8cffae32326c4b4c4d264f85b0a07+ui-policy-system-fixes`, and SHA-256 `0a65d4781a65de3381394d65cdfafddee1beb6f3f1ee0d50b81484bd94c15019`. Its source snapshot is the earlier clean UI QA 5 build source plus the changed `internal/privatekeyfile/key_windows.go` and its Windows regression tests. No replacement MSI or installation has been launched. Existing installation/upgrade/rollback, WebView2, successful model inference and broader release acceptance remain separate checks.
