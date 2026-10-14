# RC11: explicit Node-admin SQLite storage footprint

GET /v1/qa/sqlite-footprint is read-only and uses the existing Debug Centre Node-operator permission boundary. It reports numeric local database size, WAL and shared-memory sizes, SQLite page count, page size, freelist count, and page allocation estimate. It never exports filenames, directories, SQL errors, database rows, receipts, credentials or raw configuration. Cache-Control no-store is applied.

Measurements are sequential samples and may differ during writes. The database_healthy field means the local file and SQLite metadata are readable, NOT that integrity_check was completed or future capacity is guaranteed. No checkpoint, VACUUM, destructive retention, export, socket or device effect is performed. This global database figure is intentionally separate from per-Goal Project-scoped evidence counts, because other tenants can share a database.

Live Windows/Ubuntu host RSS, multi-hour pinned readers, on-device WAL retention and capacity acceptance remain release gates. The holistic human-centred UI/UX alignment will remain the last development workstream before the authorised QA installer.

The response explicitly labels the status metadata_readable (not database_healthy) and cleanup_performed=false (not a claim about every subsystem's retention policy). It is a narrow description of this single request, never an assertion that every database record is valid.
