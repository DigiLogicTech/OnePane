# Current RC11 scope reconciliation — 25 October 2026

This is a source survey against the15currently open approved GitHub issues and
current implementation, not completed product/physical acceptance or a new scope.
The release execution manifest remains authoritative. Source baseline0faf8b49
and the following OmniRoute/Evidence remediations continue the same draft PR228.
Old issue/gap checkboxes describe historical checkpoints; preserve working code.
No issue is closed by this survey. All required release gates remain open until
independently evidenced, including #86 and the final Windows/Ubuntu UI/install QA.

| Approved workstream | Current code/evidence to preserve | Remaining functionality and acceptance |
| --- | --- | --- |
| #19 Workspace lifecycle | Rootless runtime/application lifecycle; `internal/sandboxrunner/real_node_multi_workspace_integration_test.go` covers two-Workspace isolation, restart/rebuild/preservation; crash-recovery/name-collision tests | Run approved real rootless Ubuntu/image/node cases, observed resource enforcement, old-runtime migration/rollback and Windows control-plane/installed acceptance. Another lifecycle scaffold is not the next dependency |
| #20 General-purpose toolchains | Scoped exec/discovery/files/Git; presence-only preflight; immutable reviewed desired manifests in `internal/projectworkspace/toolchain_manifests.go`; original-owner waits and prerequisite injection in `internal/agentworker/toolchain_admission.go`; Godot and32MiB immutable output | Installed-version/SBOM attestation, reviewed offline provisioner/image lifecycle, consistent adapter admission and Node resources; resumable quota-controlled output beyond32MiB. `approved_unverified` is desired state, not installed-version proof. Real generic/Godot execution and artifacts remain required; #114 extends this scope |
| #21 Workspace collaboration | Actual directional publication/version/hash grants, expiry/revocation, preview/download integrity and bounded contribution navigation; broker and Task adapter source | General authorized service transport is not established. `internal/workspacebroker/task_adapter.go` deliberately leaves readiness adapter unregistered pending review/physical acceptance; readiness metadata is not arbitrary traffic. Run physical no-transitive-access/revocation cases before enabling reviewed transport |
| #22 Library ingestion/evidence | Upload/adopt immutable assets/versions/grants; managed content preview/download; metadata search; bounded verified text/pinned Research delivery; immutable UTF-8 line index/status, exact-version citations and bounded Workspace-wide indexed-text discovery with opaque scoped paging and real-auth API/UI (`rc11-library-text-index.md`) | Metadata search remains separate. PDF/DOCX/page pipeline, governed semantic retrieval, broader Project discovery/permissions and full Assistant/Task/Council attachment lifecycle remain functional gaps. New source requires exact CI; global browse authority, final product UI and installed history need review/acceptance |
| #23 Autonomous orchestration | Actual objective-to-DAG L4 and revocation (`internal/projectorchestrator/autonomous_turn_integration_test.go`); dependencies, delegates, toolchain/resource waits, holds, durable recovery intake/read-only cases | Cases remain `awaiting_inventory` (`internal/task/recovery_capture.go`). Complete closed semantic effect attribution, immutable bounded inventory/freshness, authenticated human disposition and independent actual postconditions before replay/readmission. Team help/reassignment below. Real CPU-only objective→tools→verified artifact/restart/no-cloud remains acceptance |
| #24 Models and routing | Verified discovery/install/rescan/reuse/catalog, model supervisor/lease/pin/tier/hot-swap compensation, Agent Check and managed CPU/GPU paths (`internal/localai`, `internal/inference`, `internal/scheduler`); OmniRoute component source | OmniRoute process ownership/readiness/privacy and component recovery require current remediation, beyond model supervisor hardening. Real Windows/Ubuntu CPU/GPU/partial-offload, constrained6GBGPU/16GBRAM, cancellation/memory reclaim/swap/route/Research pin/restart remain acceptance, not source-fixture passes |
| #25 Research Council | Strict seat pin/no-substitute, independent first-pass visibility, critique/synthesis, consent and actual byte/hash evidence; raw requests/response artifacts and durable provenance (`internal/team/research_test.go`, `internal/teamworker/research_*`, `internal/inference/request_repository_sql.go`) | Trace complete version identity/replay/export requirements and run real provider-limit/restart reproducibility. Bounded two-timer retries then explicit manual pause are deliberate; do not replace Council from old bullets or claim real reproducibility from fixtures |
| #26 Observability and operations | Actual Operations feeds/attention, Nodes management, scheduled Tasks/routine creation, permission-checked100event Evidence ledger (`app-foundation.js`, `nodes-management.js`, `evidence-audit-page.js`) | Evidence stale Workspace/render-response defect now remediated with source tests; historical search and linked Task→artifact→approval/verification inspection remain incomplete. Backend feeds do not prove complete final product behavior |
| #27 Canonical UI/UX | Existing functional surfaces/controllers and retained recovery/SQL history | Renderer/Inspector wrapper chains and33tolerated duplicate function definitions (`scripts/validate_legacy_cleanup.js`) remain source debt. Finish canonicalization according to actual dependencies; final wide/narrow/theme/Inspector/drawer and Windows/Ubuntu product review remains outstanding. Controller tests cannot replace that review |
| #28 Security/privacy/data | AES-GCM Vault (`internal/vault/service.go`), typed Tool capabilities/revocation, live Agent scope and final inference destination identity fences (`internal/tool/feature_scope_gateway_test.go`, `internal/inference/task_agent_policy_revocation_test.go`) | Complete integrated security review, permission/secret/migration/backup/restore and installed upgrade/privacy QA. SQLite free pages/WAL/backups/remanence need explicit disposition; numeric storage inspection is not secure deletion. Preserve all user roots and data |
| #29 Installation and packaging | Guarded current-source Windows MSI and Ubuntu Debian QA builders/workflows; source syntax/provenance/denied-packaging checks | Required #86/sign-offs/finalUI/Windows-first installed acceptance; protected environments and real reviewed exact-SHA evidence; actual clean/upgrade/rollback/custom roots/permissions/service/reboot/uninstall-preservation runs; artifacts/checksums/signing/provenance. Source guards are not packages |
| #30 Full integrated QA | Hosted Go/SQLite/controller/race and OS-native checks; opt-in physical protocols already exist | Real CPU/GPU/Node/Research/diagnostic-export and whole-product E2E/stability/low-resource/recovery, final UI and installed final-source regression remain open. Skipped/cancelled/stale-source CI never closes acceptance |
| #85 Debug and QA diagnostics | Typed bounded QA ZIP/provenance/trace/browser incident capture, read-only OS service probes (`internal/api/qa_snapshot.go`, `qa_*provenance*`, `internal/webui/static/debug-centre.js`) | Current snapshot explicitly excludes raw logs/external Nodes/screenshots/attachments; separately reviewed text is not those integrations. Reconcile full selectors/attachment requirements and actual Task/Tool/AgentCheck trace, installed/theme/export acceptance. Do not claim all debug scope from ZIP availability |
| #86 Ordered human reviews | Decision register and source review/remediation history; scope remains recorded | Mandatory whole-tree first audit→owner vision decisions→second audit/sign-offs in approved order, and final UI before packaging. Prior slice reviews, this survey and source tests do not constitute human sign-off |
| #114 Native Unreal and UEFN | Fixed read-only rootless same-container Unreal initialize identity probe (`internal/sandboxrunner/unreal_mcp_probe.go`) and honest engine registry (`game_engine_capabilities.go`); UEFN `planned_not_connected` | Authenticated explicitly paired Windows-local companion, actual Editor/Project/Workspace/toolset identity, governed read capabilities, approved serialized mutations/undo/idempotency/independent verification, UBT/build/PIE/artifacts and separate UEFN capability pack are unimplemented approved functionality. Physical licensed/toolchain/host acceptance remains separately required |

## Cross-cutting approved Team and AI requirements

Named accepted contributions, checkpoint handoffs, bounded concurrency/shared
compute, both live views, human guidance, pause/resume/scoped cancellation,
ledger-bound result and Task-associated Library navigation exist. Preserve their
immutable accepted graph and distinct V0 declarations. Durable help/answer/explicit
original-owner continuation (`rc11-team-help-continuation-design.md`) now has
published requests/answers/explicit Continue, required model context, bounded
discovery and the Team-room input panel, with exact-source hosted CI. Peer help,
versioned human-reviewed reassignment, broader fair concurrent scheduling and
precise observed runtime/swap/loading stages remain functional work. Queue admission
must not be labelled physical model execution. Complete safe effect recovery and
real model/tool restart/unknown-outcome acceptance before terminal claims.

Managed model ownership/rollback negatives do not automatically cover every managed
component. Physical Colibri/larger-model constrained hardware, llama.cpp CPU/GPU,
OmniRoute/provider compatibility/health/fallback, queues/memory reclamation and hot
swap with chat/Project/Team contention remain independent acceptance requirements.
Source runtime declarations and synthetic HTTP/model transports cannot close them.

## Eligible execution order

1. Finish the confirmed OmniRoute ownership/privacy/recovery and Evidence
   scope-lifetime defects, validate affected code and exact-source CI. Leave
   physical listener attribution and installed acceptance explicit.
2. Durable Team assistance/original-owner continuation source is published;
   nonadjacent bounded pairing extends scheduling without new admission. Continue
   peer help/versioned reassignment or closed recovery semantic attribution;
   immutable decisions/postconditions must precede replay.
3. Exact-version bounded immutable UTF-8 line indexing/status/literal citations
   now has source implementation and authenticated UI/API integration. Advance
   independently authorised Workspace-wide indexed-text discovery now also has
   bounded source API/UI implementation. Continue governed PDF/DOCX/page citations/semantic retrieval with reviewed dependencies and full
   attachment flows. Metadata search and preview remain separate capabilities.
4. Continue remaining approved toolchain/service/native-engine and product
   dependencies above. Reconcile source evidence after implementation; do not let
   recurring local fixes silently remove those workstreams.
5. Complete physical/integrated acceptance and ordered human reviews/final UI;
   only then use reviewed exact-source Windows-first/Ubuntu packaging and installed
   regression. Produce final version/artifacts/checksums/provenance only after all
   mandatory gates pass.

