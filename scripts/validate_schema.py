#!/usr/bin/env python3
"""Dependency-free validation for the embedded v0.1 SQLite schema."""

from pathlib import Path
import sqlite3
import sys

ROOT = Path(__file__).resolve().parents[1]


def main() -> int:
    db = sqlite3.connect(":memory:")
    db.execute("PRAGMA foreign_keys=ON")
    for migration in sorted((ROOT / "migrations").glob("*.sql")):
        db.executescript(migration.read_text(encoding="utf-8"))
    failures = db.execute("PRAGMA foreign_key_check").fetchall()
    if failures:
        print("foreign-key validation failed:", failures, file=sys.stderr)
        return 1

    tables = db.execute(
        "SELECT count(*) FROM sqlite_master "
        "WHERE type='table' AND name NOT LIKE 'sqlite_%'"
    ).fetchone()[0]
    triggers = db.execute(
        "SELECT count(*) FROM sqlite_master WHERE type='trigger'"
    ).fetchone()[0]

    print(f"schema valid: sqlite={sqlite3.sqlite_version} tables={tables} triggers={triggers}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
