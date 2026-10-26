# Immutable Library text indexing and literal retrieval

Approved scope: #22 Library ingestion. This source increment preserves existing
metadata search, preview and independently verified Research delivery. It adds an
explicit operator action to index one already managed, authorised immutable text
version. It is not the whole Library or release acceptance milestone.

## Implemented path

The Workspace Library card exposes Check indexing, Index text and Search
document. These operate on the card's exact accessible version and canonical
Project/Workspace. There is no automatic text read, indexing or model delivery.
The Project-wide inventory does not acquire a content-read action without a
Workspace grant. Existing direct latest/pinned and directional publication
grants remain authoritative on every operation.

The API routes share the version path
`/v1/projects/{projectID}/workspaces/{workspaceID}/library/{assetID}/versions/{version}`:

- `POST /index` reads only the managed source and accepts no replacement body.
  It requires project.read/project.write; it does not require or grant runtime
  execution. Browser CSRF and original bearer/browser identity are rechecked.
- `GET /index` returns indexing status without text or storage identity.
- `GET /search?q=...` returns literal, case-sensitive matches from that exact
  version. Search requires a ready retained index and current read access.

Before indexing, the managed artifact is independently SHA-256 verified and its
active status, Project, tenant, hash and size are matched to the selected version.
The transaction repeats current Project/Workspace lifetime and original tenant,
actor eligibility, exact bounded version grant, current source identity/status/
storage reference and the original credential/capabilities. Changed identities
cannot commit an index merely because the actor belongs to two tenants. Final
artifact, version, Project, grant and credential checks may withhold a private
response even after a valid commit; identical authorised retry observes the
retained index without another indexing event.

Migration0114 adds retained immutable header and line tables, keyed by asset,
version and parser version1. It modifies no existing source bytes, versions,
grants or user files. Index, lines and one bounded metadata audit are atomic.
Derived text lives in the local database and therefore its normal backups; it
has the same sensitivity as the managed source. This increment does not implement
secure erasure, change backup policy or imply new sharing/execution authority.

## Bounds and citation semantics

Supported content is UTF-8 textual MIME (including Markdown/CSV/code text and
JSON/XML/YAML/TOML); explicit non-UTF8/non-ASCII charsets are unsupported. Input
has at most1MiB,16384lines and16384bytes per line. NUL and invalid UTF-8 are
rejected as invalid_text. Unsupported, too_large and invalid_text are retained
explicit outcomes; absent history is not_indexed. Nothing is silently truncated
or relabelled ready. PDF/DOCX parsing remains separate required work.

Lines split at LF. Offsets are zero-based and end-exclusive into the original
bytes; LF is excluded, a preceding CR is retained verbatim. Empty files have no
lines; a final LF creates no phantom extra line. Citations retain source version,
source SHA-256, one-based line number, exact byte range and SHA-256 of the raw
line. Reads use persistent composite-key lookups, bound metadata/line materialisation,
reconstruct the whole selected source within1MiB and verify its SHA-256 before
returning any match. Missing, orphan or forged retained data fails closed.

Queries are literal UTF-8 up to256bytes; operators such as `%`, `_` and SQL text
have no special meaning. Each result has at most8complete bounded lines and
has_more when further matches exist. The JSON response has a1MiB limit. This is
bounded exact-version line retrieval; it does not claim Project-wide full-text
ranking, semantic search or a parsed PDF page index.

The UI renders source and citations only through textContent, disables duplicate
operations and rejects mismatched result identities. New render/scope generations
clear private excerpts before replacement loading and discard late old results.
Denied reads clear retained excerpts. No content reaches Task, Council, an
Orchestrator or a model from indexing/searching. Those attachment/provenance
flows remain separate governed requirements.

## Validation and remaining acceptance

Actual migrated SQLite/managed artifact/authenticated HTTP tests cover persisted
indexing and reconstructed service retrieval, CRLF/Unicode citations, immutable
retry/audit, atomic rejected line/event writes, malformed/forged/missing/orphan
rows, all status/budget boundaries, pinned/directional grants and revocation.
Production plans with1024unrelated indexed versions use persistent indexed
SEARCH before and after ANALYZE. Same-transaction and final-publication tests
include credential, tenant, Project/Workspace, grant and managed-source drift.
The additive indirect recovery ownership contract is explicitly reviewed;
new derivative references confer no recovery disposition or replay clearance.

Node tests exercise the actual controller and mounted renderer with a DOM that
rejects innerHTML. Source tests establish no physical model, parsed PDF/DOCX,
semantic retrieval, final browser/product UI, platform installation or release
acceptance. Exact-source CI and remaining whole-release gates are recorded in
onepane-release-execution-manifest.md.
