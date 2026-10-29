# Unified literal discovery across text and Word

Approved workstream #22 now has one explicit **Search contents** control in the
canonical Workspace Library. It searches existing indexed UTF-8 files and
extracted DOCX paragraphs with one case-sensitive phrase. Indexing/extraction
remains a separate explicit file action. Nothing is automatically indexed,
attached to a Task/Council or sent to a model.

`GET .../library/search-content?q=...&cursor=...` collects both existing domain
search pages and uses the same source-verification helpers as the retained
`search-text` and `search-documents` endpoints. After *all* format-specific
artifact I/O, both comparable bounded collections and both full selected
version/artifact/storage checks run again. Original credential, tenant,
Project and active named Workspace checks precede publication. A failure in
either format withholds the entire combined response and all excerpts.

Raw text still cites its original version/hash, line and byte interval. Word
still cites separate raw package and derived hashes plus XML part/paragraph.
No invented common coordinate, page number or global relevance rank is added.
The response groups the native text matches and compact Word receipts. Each
format retains its existing scan and eight-match limit: the bounded union is
at most eight raw lines plus eight Word paragraphs. The combined JSON cap is
2MiB, the sum of the existing per-format caps. Private candidates and raw scan
positions never leave the server. Word verification includes authorized
nonmatches and retains independent raw-source rederivation.

A single opaque continuation retains both private forward positions and which
format has finished; finishing one format does not restart it while the other
continues. Empty intermediate pages are honest and can still have more work.
Each page replaces the previous combined results. Pagination is live forward
keyset navigation, not a cross-request frozen snapshot. A fresh search is needed
to see newly added keys before its current position.

The navigation registry contains no excerpts or plaintext query. It binds
original tenant/Project/Workspace, principal/type/credential/authentication
method, scopes and query hash. Random32byte tokens expire after5minutes with
global256/per-credential32LRU bounds. Expiry, scope/query changes, process restart
or transfer to a dedicated format registry requires a fresh search.

The mounted controller reuses both format citation validators against the
*same* combined HTTP response; it performs no independent source fetch. Text
uses inert DOM `textContent`. Invalid receipts, denial, replacement, disposal
and late scope responses cannot restore previous excerpts. There is no implicit
paging. The dedicated format controllers/endpoints remain available and tested.

Source validation covers real bearer/browser two-format retrieval, native
citations, compound continuation, asymmetric completion, cursor transfer/expiry,
query bounds and revocation of a text source while a Word source is being read.
Credential/membership/storage drift at that boundary withholds both formats.
Cursor tests retain identity, per-credential/global memory and expiry bounds.
Controller and mounted renderer checks cover one network request, both native
formats, empty pages, duplicate/invalid receipts, immutable scope, inert text,
denied reads and late/disposed responses. These are source checks, not final
visual, physical inference or installed-platform acceptance.

PDF/page-aware extraction, semantic retrieval, broader discovery/permissions and
the full Assistant/Task/Council attachment lifecycle remain approved unfinished
functionality. Existing Research seat consent accepts raw textual MIME only;
this search does not extend that approval to DOCX-derived model context.
