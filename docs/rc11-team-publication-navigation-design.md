# Team contribution Library navigation dependency

Status: bounded assignment navigation implemented and source-tested on 25 October
2026. Exact-source hosted validation is recorded in the execution manifest.
Recorded Task result preview and ledger-bound completion metadata remain separate.
This document does not authorize execution, grant content access or establish
physical QA.

## Smallest remaining source slice

The dedicated read-only assignment output endpoint loads publication history
only after an explicit operator action, outside the live-room poll:
`GET /v1/team-sessions/{sessionID}/assignments/{taskID}/outputs`.

Bind the exact current accepted session/plan/root and immutable assignment,
tenant, Project, named Workspace and artifact-session scope. Require the current
completion metadata protocol in `internal/team/live_assignments.go`, retaining
its checkpoint/verification/level and latest succeeded Attempt/start-event
checks. Revalidate completion identity after output collection. Deny unresolved
or invalidated completion; do not substitute another valid checkpoint.

Require current `team.read`, `task.read` and `project.read`, retaining the same
principal, credential and principal type across the read. Team membership or a
model-supplied artifact ID never grants Library access. Resolve each returned
immutable version through the existing source Workspace Library permissions.
Reauthenticate immediately before response; publish no-store metadata only.

## Publication ownership and bounded work

`workspace_file_publications` has a Task-prefixed primary key
`(task_id,relative_path,content_hash)`. It has no Attempt ID. Query that exact
Task's raw receipts in primary-key order with limit33 for a32-receipt first
slice. Overflow must explicitly defer/refuse; filtering the global recent100
Workspace outputs is not a complete Task inventory. Apply payload/path byte
limits before materialization and retain a whole-response byte bound.

Require complete receipts, exact Project/named Workspace, the immutable asset
and version, matching hash and `artifact:` storage reference, active asset and
managed artifact. Reconcile all paths without exposing artifact IDs/storage
URIs/raw provenance. Independently enforce current latest/pinned grants or
enabled, unexpired directional publications. Inspect actual resolver plans:
the directional-publication branch needs a reviewed asset/version lookup index
before repeated calls can be described as bounded negative probes.

Migration0109 now provides the exact `(asset_id,asset_version,content_hash,link_id)`
index. The unchanged production resolver is tested with1,024 unrelated
publications before/after statistics collection; historical whole-publication
scans and Project-wide link enumeration are replaced by exact version/hash
probes. This removes unrelated-history dependence only. Multiple directional
publications of the **same** version/hash are now bounded by the separate
`ResolveBoundedWorkspaceLibraryVersion` reader. A materialized raw candidate set
stops at129 before permission filtering; more than128 refuses the entire
directional decision. Current direct latest/pinned grants bypass that set.
Existing content endpoints retain their original permission behavior and
independently authorize every click. No global constant-work claim is made for
those older content readers.

Unchanged Library versions intentionally retain older originating Task/Attempt
provenance. The current Task's exact receipt may associate those same bytes.
Label the UI **Task-associated recorded publications**. Do not assert every
version originated in the final Attempt or rewrite the version's provenance.

Suggested response: Project and named Workspace IDs, completion identity,
and bounded records containing relative path, asset ID, version, content hash,
size and MIME. Never return a raw caller-supplied URL. Derive existing scoped
preview/download URLs from validated records; those endpoints independently
authorize content access on every click.

## Dependencies remediated during source review

Real Workspace publisher receipts use bare64 lowercase SHA-256 digests, while
managed artifacts use `sha256:` plus64 digits. Strict comparison now accepts
only those two existing Library forms and canonical artifact hashes, retaining
tenant/Project/status/size checks. Actual publisher-to-preview/download and
Research evidence fixtures verify exact bytes. This does not run OCI or models.

Library preview and download retain the original credential across slow content
collection, recheck current Project state, the exact version and current grant,
then reauthenticate before publishing. Preview checks occur after its bounded
read; download checks occur after verification/opening and before response
headers. This is not a claim of continuous mid-stream credential revocation.

## Required acceptance matrix

Use genuine migrated publication/Library/grant/artifact fixtures. Cover foreign
tenant/Project/named Workspace/artifact session; changed accepted plan,
completion revision/checkpoint/verification; wrong receipt/hash/storage/version;
in-progress/unknown receipts; archived assets/artifacts; revoked and changed
latest/pinned grants; disabled/expired directional links;32/33 receipts; UTF-8
byte and whole-response limits; actual populated query plans; revoked or changed
credentials during collection; late UI replies after evidence/scope changes;
and preservation of original histories and zero mutation/model/tool dispatch.

The implementation returns no partial inventory on receipt/fan-out/metadata or
encoded-response overflow. It recollects receipts/grants, compares current
completion identity, checks Project lifetime after collection and reauthenticates
before returning a private no-store response. It is not a globally atomic
authorization guarantee after the response starts. The UI validates the complete
metadata protocol and derives same-origin URLs; it offers plaintext preview only
for supported textual MIME types up to256KiB and Download for other files.

Real-model, final browser/UI and installed Windows/Ubuntu acceptance remain
separate open release gates. Durable help, versioned reviewed reassignment and
broader fairness remain subsequent Team collaboration work.
