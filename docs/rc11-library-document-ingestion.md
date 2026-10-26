# Source-bound DOCX paragraph extraction

Approved workstream: #22 Library ingestion/evidence. This increment adds actual
managed DOCX extraction, retained provenance, authenticated status/retrieval and
Workspace controls. It preserves the existing immutable UTF-8 line index and
its distinct original-byte citations. It does not complete PDF, semantic search,
attachment delivery, final product UI or physical/installed acceptance.

## Operator path and authority

A canonical Workspace Library card with the exact DOCX media type exposes Check
extraction, Extract DOCX text, Read extracted paragraphs and Next paragraphs.
An optional case-sensitive literal phrase filters the same retained paragraphs.
Nothing loads automatically. Each page replaces prior content, and scope changes,
denial, disposal or stale replies cannot restore old excerpts. Source text and
part/paragraph coordinates use textContent, never executable markup.

The version path has POST/GET `document-index` and GET `document-text` with
optional `q` and `after`. Indexing requires project.read/project.write; reads
require project.read. Original bearer/browser credentials, CSRF for writes,
captured tenant, current active Project/canonical Workspace, actor membership,
active exact source artifact and current bounded direct/pinned/directional
source grants are checked in the transaction. POST accepts no replacement body,
filesystem path or caller-supplied derived text. No project.run, new Library
asset/artifact/grant, Task execution or implicit model/Research access follows.

Final full version identity, active source/storage reference, bounded grant,
Project tenant/lifetime and original credential checks precede private output.
A response can be withheld after a valid committed extraction; an authorised
identical retry observes its immutable history without another indexing audit.
Original bytes and all existing user roots/versions/grants are preserved.

## Supported stored text and honest coverage

The current parser is `docx-wordml-paragraphs-v2`, for the exact canonical media
type `application/vnd.openxmlformats-officedocument.wordprocessingml.document`.
It reads declared Word main/header/footer/footnote/endnote/comment stories,
sorted by part name, and retains literal paragraph text, tabs and line breaks.
Normal tables and empty paragraphs preserve their paragraph ordinals. Main
WordML requires one direct body; internal office-document relationships support
relative or package-absolute targets. No external relationship is followed.

Revision v2 accepts XML's single leading UTF-8 encoding signature, as specified
by [XML 1.0 section 4.3.3](https://www.w3.org/TR/xml/#charencoding), with or
without an XML declaration, in package metadata and every selected story. The
signature is removed only from the decoder view: raw source and XML-part hashes
still bind its original bytes. Repeated/misplaced signatures are rejected;
literal U+FEFF within paragraph text is retained. Revision v1 rejected these
otherwise valid documents. Migration0117 preserves every v1 header/unit and
permits distinct immutable v2 records for the same original source version.
Current controls require explicit extraction under v2; no old outcome is
rewritten or relabelled as newly verified. The transactional table rebuild
retains foreign keys and insertion-only history, with normal upgrade backups.

This extracts stored text, including stored field display text. It does not
evaluate fields, render layout/page numbers, compute list numbering, OCR images
or execute/read embedded documents. Strict WordML, ambiguous tracked changes,
moves, AlternateContent, altChunk, unresolved symbols, nested paragraphs and
unsupported structures remain explicit non-ready outcomes. Other declared MIME
types, including PDF and untyped binary uploads, remain unsupported here.
These compatibility/coverage limitations remain visible requirements; no final
format-acceptance or full Word rendering claim is made.

Raw source SHA/version/size remain separate from the canonical derived text
SHA/UTF-8 byte count. Each unit records global ordinal, source part, one-based
paragraph number, literal text and text SHA. Part receipts retain the exact XML
part SHA and paragraph count. No ZIP byte offset or invented page number is
presented as a document-text location. The format's separate stories are
documented by [Microsoft's WordprocessingML reference](https://learn.microsoft.com/en-us/office/open-xml/word/structure-of-a-wordprocessingml-document).

## Bounds, retention and independent source checks

Migration0116 adds only immutable document header/unit tables under the original
Library source version. Parser identity, source and derived hashes, coverage and
part receipts are retained, with a separate manifest seal. Header, units and one
bounded metadata audit commit atomically; same-source retries are idempotent.
Current source grants remain mandatory on every read. Derived text lives in the
local database and its normal backups; no secure-erasure or new sharing claim.

Budgets are8MiB raw source,256ZIP entries,32MiB declared total expansion,
8MiB actual XML expansion across the package metadata and selected stories,
4MiB per XML part,64stories, XML depth64,262144tokens per XML part,
8192paragraph units,16384UTF-8 bytes per unit and1MiB total derived text.
Reads return at most8units and a1MiB JSON response. Literal queries are at most
256UTF-8 bytes. Unsupported/too_large metadata outcomes require no binary read;
invalid_document is distinct from ready. No content is silently truncated.

EOCD and central/local ZIP identities are bounded before archive/zip can
allocate its entry slice. Unsafe paths, duplicates/aliases, multiple disks,
ZIP64, encryption, unsupported methods and inconsistent metadata fail closed.
Canonical directory entries are accepted without filesystem extraction. Actual
part reads use the remaining expansion budget plus a sentinel byte, and validate
decompression/CRC/error outcomes. XML directives/entities, invalid UTF-8, duplicate
attributes/declarations, ambiguous bodies and excessive nesting/tokens deny
extraction. [Go archive/zip](https://pkg.go.dev/archive/zip) supplies the readers;
the application enforces the budgets before and during their use.

Retained unit hashes, canonical part JSON, manifest seal and derived digest are
checked before reads. Those checks alone cannot independently prove derivation
from a ZIP source: a self-consistent forged derived receipt could recompute all
its own hashes. Therefore every eligible GET status/content publication reparses
the independently verified original raw bytes and compares the complete receipt.
POST already parses those source bytes before its transaction/retry comparison.
Self-consistent derived corruption against unchanged raw DOCX is rejected.
Raw verification/read, XML reparse and retained text are separately bounded;
this prioritises faithful source binding over treating a cached hash as proof.

## Source validation and remaining release work

Actual generated OOXML ZIPs and migrated, isolated SQLite tests cover body,
Unicode/tabs/breaks, tables, empty paragraphs, headers/notes, canonical directory
entries, relationships and malformed/ambiguous structures. Tests exercise raw
versus derived identities, component citations, paging/search, reconstruction,
immutable retry/audit, query-only reads, current/pinned/directional/revoked grants,
actor/tenant/Workspace/source drift, rejected units/audits and retained corruption.
Real bearer/browser HTTP tests cover source collection, same-transaction
credentials, final publication, CSRF, client body denial, source retention,
metadata-only statuses and self-consistent forged derived data.

The multipart upload-to-extraction test uses the production upload endpoint,
canonical browser media type, actual managed bytes, real bearer/browser
credentials and browser CSRF. It verifies independent raw hashes, original
source grants, explicit extraction, paragraph search and authenticated HTTP
grant revocation. Parser fuzz targets separately mutate raw ZIP packages and
XML inside valid packages; they check bounded output, no partial rejected
content, canonical paragraph/hash identities and deterministic receipts. Small
bounded fuzz runs are supplementary robustness evidence, not exhaustive format
or hardware qualification. Populated revision upgrades and an injected failure
after the copy/drop/rename/trigger operations verify unchanged v1 history,
atomic rollback, foreign keys, v2 coexistence and immutable retention.

The additive recovery ownership contract is explicitly reviewed with15new
negative schema cases. It records shared derived context, never Task ownership,
read permission, replay/disposition authority or independent effect verification.
Controller/mounted-renderer tests establish safe source UI behavior. Hosted
native Windows and a dedicated Linux document race family remain required for
each published source; the required aggregate includes that new family.

PDF/page-aware ingestion, unified Workspace-wide DOCX retrieval, semantic search,
full Assistant/Task/Council attachment provenance and genuine format/UI/platform
qualification remain open. Generated ZIP/source tests do not prove Word or
LibreOffice rendering, final Windows/Ubuntu product review, physical AI or
installed upgrade/release acceptance. The release execution manifest controls
those remaining gates.
