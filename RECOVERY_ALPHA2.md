# Alpha 2 source recovery checkpoint

Recovered: 2026-10-03

This branch is reserved for reconciliation of the complete OnePane Alpha 2 source tree recovered from the project Library.

## Provenance

- RC8 baseline: `onepane-v0.1-rc8-hardware-testbed.zip`
- RC8 SHA-256: `8877113df183a7c43d0ccff608aeb866a5d1cea8e44a4d38054ad172bd1aa2ee`
- Recovered Alpha 2 source snapshot: `_build_alpha2_source_final4.b64` (gzip-compressed tar after base64 decoding)
- Recovered tree: 22 migrations, 266 Go files, embedded WebGUI, Windows desktop/service/setup packaging.

## Verification completed

- RC8 artifact hash matches the historical release hash.
- `node --check internal/webui/static/app.js`: PASS
- `scripts/validate_windows_qa5_source.py`: 31/31 PASS
- `scripts/validate_windows_qa4_source.py`: 55/55 PASS
- `scripts/validate_windows_qa_source.py`: 36/36 PASS
- `scripts/validate_m23_webgui.py`: PASS
- Full `go test ./...` was attempted but the isolated recovery runner could not resolve/reach `proxy.golang.org`; this is an environment/network limitation, not a recorded test failure.

## Alpha 2 features confirmed in recovered source

- Global Settings that seed new Project defaults without overriding existing Project/Workspace policy.
- Revised Light theme, two-tone themes, installable theme packs.
- Built-in language support and installable language packs.
- Core Skills pack.
- Local AI runtime strategy selection: Auto / Managed Hot Swap / Colibri Large Model.
- Colibri v1.12.1 optional component with pinned SHA-256, install/remove lifecycle, model-folder registration, managed deployment integration, supervisor launch and OpenAI-compatible inference transport.
- OmniRoute remains optional and independent.
- Model spec sheet and Agent Check/Testbed.
- Operations View All cleanup and Inspector-oriented detail flow.

Do not merge this branch to `main` until the complete recovered source tree has been committed and release gates have been rerun on a network-connected build host.
