# RC11 — bounded on-disk SQLite/WAL observer stress gate

The hosted Linux source CI uses a **temporary disposable SQLite file** (WAL, synchronous FULL) with real human-authored Project and Goal records. An integration test opens the DB, performs **640 alternating pause/resume operations** across **four close/reopen cycles**, then validates the monotonic compare-and-swap revision, 640 corresponding append-only audit events, `PRAGMA quick_check=ok`, WAL journal mode, and **zero** manual/recurring TCP probe attempts or new network permissions.

Test reports JSON-line `soak_stats` with measured `db_bytes`, `wal_bytes`, `shm_bytes`, sampled Go heap, Linux max RSS and wall elapsed milliseconds. The small repeat count and scheduled Go heap limits aim to avoid locking the older machines OnePane targets; the hosted gate fails if combined temporary database files exceed 64 MiB, sampled heap exceeds 256 MiB, or Linux RSS exceeds 1 GiB. These are **test-fixture caps**, not production hard limits, user-configured quotas, or evidence of performance on the Alienware/Ubuntu host. Memory figures are **process-wide**, and Linux maximum resident set is measured using `getrusage` (KiB converted to bytes).

The test never invokes the actual socket dialer, deletes historical rows, rewrites real user data, forces a production WAL checkpoint, or attests a real physical Node/endpoint. There is **no automatic retention pruning**. Long-running installed application RSS, WAL under concurrent active readers, I/O saturation, full disk and backup/recovery remain operator/physical acceptance gates.

This is a QA developer diagnostic. Final product UI/UX work remains the **last** pass before a separately approved QA installer.
