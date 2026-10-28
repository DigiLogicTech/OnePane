# Windows current-product test build — 28 October 2026

The user resumed Windows-only work and explicitly selected **a testable installer of the current product, with unfinished RC11 work documented**. Ubuntu and full formal RC11 completion are deferred. This is a separate milestone; the existing formal RC11 packaging builder, protected approvals and release acceptance criteria remain unchanged.

## Source and artifact identity

**Current handoff is Windows Test 2**, built from `649fffb25f99d79a0de97ba42d4118d8a7fc078f` at `D:\Codex\OnePane\builds\windows-test-20261028-02`. It fixes explicit CPU/hybrid placement falling through to automatic GPU sharding. Regression tests cover a dual-compatible-GPU host, explicit hybrid placement and CPU memory rejection without GPU fallback. The source fix was committed/pushed on the existing branch. Application version: `0.1.0-alpha.3.3-windows-test.2+649fffb2`; MSI ProductName ends in `Windows Test 2`; numeric MSI version remains `3.3.11`.

MSI SHA-256: `3b3c5717b2365deb078481f6f721a9a585c93c52761cdfe584e4a3567dde41a1`.
The manifest, build recipe, extraction/hash verification and physical-validation record accompany the MSI. MSI identity, x64 PE headers, desktop GUI subsystem, service registration, storage-location wizard controls and all four extracted payload hashes passed. Exact-source [integration40588178955](https://github.com/DigiLogicTech/OnePane/actions/runs/40588178955) passed all twelve mandatory source families and their aggregate, including all four native Windows families. Windows guard40588178989 passed with formal packaging skipped. Physical Node approval was informational/skipped and is not a physical acceptance pass.

The earlier Windows Test 1 is superseded and retained only as evidence:

- Source: `6ec82154685d89682145f127235fd33a44c253d2` on `codex/team-live-ci-completion`, draft [PR228](https://github.com/DigiLogicTech/OnePane/pull/228), unchanged integration base.
- Exact-source integration [40578704014](https://github.com/DigiLogicTech/OnePane/actions/runs/40578704014) passed all twelve mandatory source families and aggregate. Windows guard40578704048 passed; its formal packaging job was skipped. These runs were checked again during this milestone.
- Build source is an isolated `git archive` export, excluding every uncommitted Ubuntu/readiness change. Archive SHA-256: `1026fc523d129b32e61b1efb00ca9318feaa381ed25e309e37f13a7ee57c2b39`.
- Test binary version: `0.1.0-alpha.3.3-windows-test.1+6ec82154`; MSI ProductName: `DigiLogic OnePane Alpha 3.3 Windows Test 1`; MSI numeric ProductVersion: `3.3.11`.
- Artifact directory: `D:\Codex\OnePane\builds\windows-test-20261028-01`. Build recipe, exact hashes and provenance accompany the MSI. It is unsigned and is not the formal RC11 candidate.
- The MSI uses the existing OnePane service/upgrade identity. It is not a side-by-side installation. Its numeric version equals the reserved RC11 MSI version, so remove the test package while preserving data before installing a same-version formal candidate. Installed upgrade/removal/data preservation remains unverified; use a disposable Windows test machine first.

## Validation and continuation

Windows backend, service and desktop developer binaries compiled from that exact export. Go dependency verification passed. Service and serviceinstall tests passed natively; desktop/buildinfo packages contain no tests. Developer payload SHA-256 values match their original build manifest, and backend help executed successfully. This does not establish desktop launch or installed operation.

WiX5.0.2 authoring preflight assembled an MSI. The pinned WiX UI5.0.2 package was downloaded into workspace-local tooling, SHA-256 `5ef2c707614b9f70b6bbadd2d4abcb4124efee215e9b16bfbc80113079a604c7`. No global tooling installation or installed application changes were made.

Physical attempt04 reached normal bootstrap, hardware detection and actual managed engine launch from exact source6ec82154. Agent Check then failed to unload the runtime because the Windows tree-stop command failed. A separate disposable, owned-process diagnostic reproduced `taskkill` Access denied inside the sandbox. The attempt is **not passed** and its exact test-owned processes were cleaned up. Evidence: `D:\Codex\OnePane\.tools\physical-onepane-cpu-20261028-04\result.json`. Attempt05 repeats in fresh scratch storage outside that sandbox restriction; its outcome must be inspected before making any managed inference claim.

Attempt05 produced a genuine `READY` response using the managed Vulkan runtime, but contradicted the requested CPU-only placement. This led to the Test2 source fix. Its harness additionally treated the backend's pre-existing console host as a surviving model child. The raw result remains **not passed**; a later observation found every previously observed test descendant absent after cleanup. It is not CPU or complete GPU acceptance.

Attempt06 runs the exact Windows Test2 MSI backend. Its `/about` identity matches the final source/version; normal bootstrap and managed installation/Agent Check reached ready with `cpu_only`/`cpu` placement. The actual model returned `READY` (37 prompt/3 completion tokens;4558ms request including startup). The independently observed CPU engine used971464704bytes working set and270049280bytes private memory. Normal testbed abort removed the engine and listener without fallback force-stop. The baseline backend console host is identified by PID and start time, separately from inference descendants; it remains until backend teardown. The raw record is `D:\Codex\OnePane\.tools\physical-onepane-cpu-20261028-06\result.json`. Final independent integrity/cleanup review passed: the tested binary equals the extracted MSI backend, model/runtime hashes match, and all previously observed test descendants are absent after cleanup. This remains a bounded smoke test, not installed/endurance/queue/swap/autonomy acceptance.

**Windows current-product QA installer is ready for handoff.** Test instructions/known gaps and checksums accompany Windows Test2. The next acceptance boundary is installation, SCM startup, desktop launch and page-by-page testing on a disposable Windows machine, followed by a preserved-data upgrade scenario. No suitable disposable installed environment was used here; these checks remain outstanding. Preserve the checksummed installer. Do not install into the user's current Alpha environment as part of building it. Ubuntu and the larger RC11 implementation remain deferred.

## Deferred work and acceptance

The test build includes the current implementation, including governed Team collaboration, indexed literal text/DOCX Library search and scoped recovery-journal inspection. Full recovery inventories/human decisions/independent postconditions, Team peer help/versioned reassignment, represented Research delivery, PDF/semantic retrieval/full attachments, toolchain/SBOM/offline provisioning, reviewed service transport and native Unreal/UEFN remain unfinished as recorded in the existing scope matrix.

Final UI/UX review, GPU/Colibri/OmniRoute/Node/autonomy acceptance, model queue/swap/endurance, installed clean/upgrade/custom-root/data-preservation/uninstall checks and final release approvals remain outstanding. A test MSI, green source CI or one CPU test does not close those gates. Ubuntu's eleven-file draft remains untouched and unvalidated for integration. This milestone must not be reported as FINAL BUILD COMPLETE.
