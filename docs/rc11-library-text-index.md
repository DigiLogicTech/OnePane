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

## Workspace-wide indexed text discovery

`GET /v1/projects/{projectID}/workspaces/{workspaceID}/library/search-text?q=...`
searches saved indexes across independently permitted immutable versions. The
Workspace Library adds Search text and a separate Continue search action.
Each response replaces the previous page. Source citations stay as inert text;
neither rendering nor navigation attaches them to a model or Task.

Migration0115 adds only `(project_id,archived,id)` asset keyset indexing. A domain
page inspects at most8raw Project assets and8raw indexed versions, with at most
8matching lines. Unindexed assets and versions without a current grant consume
the same bounded scan budget. They cannot force an unlimited walk or disappear
behind a global latest-N filter. Grant resolution precedes source-text loading.
Every eligible source still undergoes bounded line/source-hash verification.

An empty page with has_more=true means more candidates remain; it never means
that no documents match. Continuation can resume within a document after the
last returned line. Has_more is conservative and can require a final empty page
to establish forward exhaustion. This is a live forward scan, not a snapshot
across requests: a fresh search includes newly inserted earlier keys or newer
versions of an already drained asset. Metadata filename filtering remains a
separate operation and does not silently change text-search scope.

Raw candidate identities, line positions and scanned-candidate counts never
leave the server. A32byte cryptographically random opaque cursor retains only
navigation, bound to original tenant/Project/canonical Workspace, principal/type,
credential/auth method, hashed scopes and literal query. It confers no access.
The process-local registry has a fixed5minute TTL, maximum256entries and a
32entry-per-credential LRU bound. Restart, expiry and eviction return409 with an
explicit new-search instruction. Failed wrong-scope reuse does not consume a
valid cursor. No plaintext source/query or raw credential scope is cached.

Every page repeats original credential and captured Project-tenant authority in
the domain transaction, including empty pages. After independently verifying and
opening each distinct matching managed source, the API repeats the comparable
bounded domain collection. It finally re-resolves the FULL LibraryVersion,
active artifact/storage reference, Project/canonical Workspace and credential.
Matching hashes alone cannot hide a changed underlying source URI. No partial
private response or next cursor is returned on those failures.

The two collections can reconstruct at most16MiB total; independent verification
of up to8distinct sources adds at most8MiB. Thus source traversal per HTTP page
is bounded by24MiB, with at most128KiB of raw matching line text and a1MiB JSON
response limit. Those figures are source/data bounds, not hardware-performance
or physical inference claims. The UI never follows cursors automatically and
clears prior excerpts before requests, denial, disposal or replacement scope.

Actual source tests cover denied/unindexed empty pages before later matches,
more than8matching lines in one version without duplication, pinned/directional
grant revocation, malformed data, expiry/scope/query/credential cursor binding,
LRU/concurrent cursor reads and actual1024foreign-asset production plans before
and after ANALYZE. Real bearer/browser API paths include source verification,
same-hash source rebinding, original-tenant and final publication negatives.
Controller and mounted renderer tests cover honest empty pages, explicit
continuation, duplicate/late responses, query changes, inert markup and denial.
PDF/DOCX/page parsing, semantic retrieval, broader Project discovery/permissions,
full attachment lifecycle and final product/platform acceptance remain open.
