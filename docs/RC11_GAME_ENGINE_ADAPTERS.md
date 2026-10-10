# RC11 game-engine development adapters — Godot, Unreal Engine and UEFN

Date: 2026-10-10. Status: **development contract; physical acceptance outstanding**.

## Core architecture

OnePane game development is **engine-neutral at the Project and Workspace layers**. A Project can have isolated World, Gameplay, Story, Art, Build and Testing Workspaces, with explicit Library version/grant links between them. Each Task receives role-scoped engine capabilities through the existing Tool Gateway, Workspace authority, Task/Attempt/lease and audit/provenance rules. Selecting an engine must never grant unrestricted host execution, filesystem access or access to other Workspaces.

The runtime adapter, not the model, owns command shape, executable identity, timeout, environment, network placement and resource policy. An engine that is unavailable or has incompatible hardware yields a visible resource wait or failure; it is not silently substituted with another engine or cloud backend.

| Engine | OnePane adapter | Implemented in this increment | Honest release status |
| --- | --- | --- | --- |
| Godot 4 | Fixed-argv rootless OCI CLI | Governed headless `import` and `run`, observable exit code; existing separate immutable Task artifact publication | Implemented code; approved physical Godot OCI test pending |
| Unreal Engine 5.8+ | Unreal MCP / Toolset Registry inside editor | Fixed local-only `initialize` handshake, server identity `unreal-mcp`, no editable MCP calls or remote bridge | Experimental **readiness only**; full editor authorization and live workflows pending |
| Unreal Editor for Fortnite (UEFN) | UEFN-specific Unreal MCP toolsets | Static capability declaration only | Planned, **not connected**; do not treat native UE tools as equivalent to Verse/UEFN workflows |

The game-engine capability list is deliberately truthful: `internal/sandboxrunner/game_engine_capabilities.go` reports no unverified tool calls.

## Unreal MCP 5.8 source and protocol distinctions

Epic's official experimental `ModelContextProtocol` plugin ships in UE 5.8. The editor integration requires both **Unreal MCP** and **All Toolsets** (which consumes the Toolset Registry). It advertises an HTTP/SSE MCP server bound by default to **127.0.0.1:8000/mcp**; the friendly server info name is `unreal-mcp`. The default tool-search experience advertises `list_toolsets`, `describe_toolset` and `call_tool` meta tools. The editor executes tool calls serially on its game thread. Toolsets are dynamic: do not infer a capability is safe merely because the MCP server lists it.

**Upstream references (checked October 2026):**
- https://dev.epicgames.com/documentation/unreal-engine/unreal-mcp-in-unreal-editor
- https://www.fortnite.com/news/unreal-mcp-is-now-available-in-uefn

### Immediate safety boundary

Epic documents that the first-party server has **no authentication layer** and is not designed for remote access. OnePane must **not** proxy it to Tailscale, a shared Node, OnePane WebUI, arbitrary agents, or a cross-Workspace port. It must not read a user-supplied MCP URL and follow redirects.

The experimental `project.app.unreal.mcp.probe` Tool is registered under the *existing* Workspace `project.app.execute` capability and sandbox authority. It admits only owned running verified **rootless** OCI applications with their sole writable Workspace bind. It launches fixed Python from **inside that same container's network namespace**: no host localhost, no remote URL, no caller-provided ports, no shell or arbitrary Python. The script uses proxy-disabled HTTP, rejects redirects and unexpected response types, reads a bounded JSON or SSE response, and only makes a JSON-RPC `initialize` request. It accepts server identity only if `serverInfo.name=unreal-mcp`. It does not enumerate, dispatch, authorize or execute any MCP editor Tool. A probe result explicitly states `editor_tool_calls_enabled=false` and `artifact_verified=false`.

An editor running *natively on Windows*, or on a different Node/network namespace, will **not** be reached by this same-container probe. Adding native Windows support would require a separately reviewed, **authenticated, Workspace-scoped local companion** with explicit user pairing, domain-socket/pipe controls and read-only discovery first; never bypass loopback restrictions or expose the unauthenticated server to a remote agent. Unreal runtime binaries, assets and projects must be installed/managed by the operator subject to Epic's distribution/licensing requirements; this increment does not download, bundle or redistribute Unreal Engine or UEFN.

### Planned incremental Editor tool capability packs

1. **Discovery and capability negotiation:** local companion/probe observes correct Editor/project identity, plugin version, supported protocol version and toolset schema, with bounded response and no raw secret/actor data.
2. **Scoped read-only inspection:** explicit allowlist for scene hierarchy, project state, relevant material/actor metadata, test status; every tool tied to a specific authorised Workspace+Task. Discovery must not automatically grant execution.
3. **Approved mutations:** individual operation capabilities for actors/levels/materials/assets; version/expected-state checks, human approval based on risk, serialized editor game-thread calls, action log and independent undo/recovery strategy. Default deny dynamic `call_tool`; never accept arbitrary embedded Python or console commands.
4. **Build, validation and artifact handling:** operator-provided Unreal/UEFN installation and engine/version compatibility checks, UBT/Automation/PIE or UEFN/Verse-specific tests through governed adapters; publish only after Task-bound independently verified content hashes using existing Project Library/version/grant system.
5. **Multi-Workspace game studio:** World and Art Workspaces publish explicit immutable versions, Gameplay and Story consume only granted snapshots; independent QA Workspace verifies builds without acquiring write authority or inheriting privileged MCP sessions.

All stages require Workspaces to remain isolated, a physical Node/Windows installation acceptance run, and successful negative authorization/isolation tests. Do not close RC11-02 just because the fixed local identity probe passes.

## Existing security/release invariants

- The local-first orchestration model and CPU-only execution continue functioning independently of Unreal MCP availability; no silent cloud fallback.
- Workspace runtime lifecycle and OCI image admission are unchanged; only approved sha256-pinned toolchain images execute. No host-level MCP bridge, background proxy or port forwarding is installed.
- Godot command successes remain separate from immutable Project Library publication and independent assurance.
- Owner-scoped secrets, agent/council seat isolation, capability-denied operations, retries, research provenance and review requirements remain unchanged.
- Mandatory first code audit → user-led vision review → second code audit (#86) precede RC11 packaging. Real rootless Godot/Unreal/UEFN acceptance remains outstanding.

## RC11 acceptance still open

- Live Godot headless import/run on approved physical rootless Node, then separately authorised Task-owned immutable Library publication.
- Real Unreal 5.8 editor local MCP handshake on a trusted approved image, and (later) a verified native Windows companion without unauthenticated remote access.
- Actual Unreal toolsets/scene mutation/risk approval, undo and tests, not just MCP discovery.
- UEFN plugin/Verse-specific workflows and platform constraints.
- Full end-to-end multi-engine Project Orchestrator flows, versioned Workspace grants, and cross-engine artifacts.
