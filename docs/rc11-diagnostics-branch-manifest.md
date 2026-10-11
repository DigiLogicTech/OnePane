# RC11 diagnostics branch task manifest

Branch: `feature/rc11-batch-diagnostics-v2-20261011`
PR: [#226](https://github.com/DigiLogicTech/OnePane/pull/226)
Base: `feature/rc11-featurepolicy-fail-closed-20261011`

## Completed implementation
- [x] Bounded, redacted policy decision schema with explicit JSON keys and fixed remediation hints.
- [x] Typed correlation ID validation and unknown action redaction.
- [x] Effective scoped policy decision helper with denial regression.
- [x] Unit tests for JSON allowlist, malformed identifiers, secret canaries and redaction.
- [x] Integrated offline policy decision evidence review into existing Settings Debug & Diagnostics Centre.
- [x] Explicit review and local export: maximum 32 KiB, maximum 64 records, no automatic uploads or storage.
- [x] JavaScript privacy, UI binding, raw-input eviction and boundary tests.
- [x] Embedded WebUI asset/package and script load-order regression.
- [x] CI path filters and dedicated Go/Node diagnostic tests added.
- [x] Reviewed PR diff for accidental data transport, storage and unsafe HTML insertion.

## Verified CI
- Code head `79f3a6a8802b8cd7d3e9a5bfb825e1c7b58ced65`:
  Project Workspace Development Architecture run
  [#38115854959](https://github.com/DigiLogicTech/OnePane/actions/runs/38115854959)
  **completed successfully** (frontend validators, Go unit/integration regression,
  WebUI embedded asset tests, cross-platform checks and Windows MSI log reader).
- Earlier CI failures were caused by a malformed `diagnostic.go` write; source
  was replaced and later complete CI runs passed. Do not reuse those failed heads.
- The current branch documentation update is not intended to alter executable code.

## Release and human QA gates (not waived)
- [ ] Human code/security review and architecture alignment approval.
- [ ] Merge upstream policy runtime only after its live inference and tool
      permission checks independently pass.
- [ ] Human WebUI QA in Windows/Ubuntu across themes and drawer sizes.
- [ ] Final code review, packaging CI and release approval per OnePane process.

## Important scope limitation
This branch provides an **offline operator-initiated policy evidence viewer**.
It does not implement live policy event collection or enforce policy at
runtime. Avoid presenting pasted evidence as proof of runtime enforcement.
Existing separate authorised diagnostic panels cover Worker, Node and API data.

## Continuation
The scoped diagnostic implementation and branch CI are complete. Keep PR #226
draft until the upstream policy enforcement and user review gates are satisfied.
Do not merge or cut a QA installer from this branch alone.
