# Alpha 3.1 atomic release-candidate workflow

Alpha 3.1 release candidates are published as complete Git snapshots. Do not assemble an RC on `alpha3.1-orchestration` by repeatedly calling the repository contents API for individual files.

## Authoritative sequence

1. Start from the current accepted branch head and tree.
2. Assemble every changed canonical file and its Windows embedded-WebUI mirror off-branch.
3. Run structural checks against the assembled contents.
4. Create immutable Git blobs for all changed files.
5. Create one Git tree using the accepted tree as the base.
6. Create one commit pointing at that tree.
7. Inspect that exact commit for JavaScript syntax, source contracts, canonical/Windows parity and expected changed paths.
8. Move `alpha3.1-orchestration` once, using the previous head as an expected-SHA lease.
9. CI runs against that exact snapshot and produces the candidate artifacts.

## CI policy

The Alpha 3.1 native workflow runs once on pushes to `alpha3.1-orchestration` (plus explicit manual dispatch). It does not duplicate the same candidate through a pull-request trigger. Workflow concurrency cancels stale queued/running candidates when a newer snapshot becomes authoritative.

## Frontend policy

The canonical WebUI lives in `internal/webui/static`. The Windows desktop embedded copy in `packaging/windows/desktop/static` must be byte-for-byte equivalent for shared HTML, CSS and JavaScript assets. Temporary RC override files are not release architecture; accepted fixes are consolidated into canonical assets before promotion.

## Promotion rule

A branch move is promotion. Blobs, trees and commits can be created and inspected without changing the branch and without exposing partial repository states to CI. Only the validated complete commit is promoted.


## Authoritative build runner

Normal Alpha 3.1 candidate pushes use the repository self-hosted Linux x64 runner as the authoritative validation/build lane. That runner performs source validation, Go tests, Windows x64 cross-build/setup packaging, Ubuntu amd64 package generation and artifact upload.

GitHub-hosted Windows and Ubuntu installed-product smoke lanes are retained as explicit manual diagnostics only. They run only when a manual workflow dispatch sets `run_hosted_smoke=true`. This prevents hosted-runner allocation incidents from blocking production of a candidate installer.

The self-hosted build lane must not install or uninstall OnePane on the live Ubuntu host. Native Ubuntu package lifecycle testing belongs in an isolated disposable environment; native Windows install/launch testing belongs on a Windows runner or manual acceptance machine.
