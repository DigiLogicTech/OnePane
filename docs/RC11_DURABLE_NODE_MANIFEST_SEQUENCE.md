# RC11: Durable local federation manifest sequencing

## Problem

An origin Node previously used its current Unix millisecond as the
outgoing capability-manifest sequence. Two different manifests produced
in one millisecond could share a sequence; a host clock rollback across
harnessd restarts could produce a sequence lower than the receiver's
persisted high-water mark. The peer correctly rejected these as replay
or equivocation, leaving valid local-first model capacity unavailable.

## Change

Migration 0050 adds a private, per-origin-Node SQLite sequence high-water
mark. `BuildManifest` reserves the next sequence atomically using a
single SQL upsert:

- The first sequence is at least the current Unix millisecond.
- Subsequent sequences are greater than the previous committed mark,
  including requests made during the same millisecond.
- A clock rollback or process restart cannot reuse a committed mark.
- Reservation is restricted to the registered `local=1` Node.
- A failed manifest construction may leave a harmless gap but does not
  license reuse of the consumed sequence.

Existing paired peers can continue comparing monotonically increasing
sequences; no automatic pairing, remote tool dispatch or fallback is
introduced. The normal schema migration backup and rollback
requirements apply. An operator restoring an *older entire database
snapshot* may need to establish a new Node pairing if its sequence
ledger is behind a peer's persisted high-water mark; this change does
not claim safety across independent database restoration.

## Acceptance

The real SQLite integration test verifies repeat generation at one
millisecond, backward clock movement, closing and reopening the database
to simulate harness restart, persisted high-water integrity and
rejection when a remote Node identity attempts a local reservation.

This is control-plane correctness testing, **not** physical Node
acceptance. The opt-in rootless hardware tests remain separate and
RC11 still requires comprehensive code review, vision realignment,
second review and explicit release approval before packaging.
