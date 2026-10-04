#!/usr/bin/env python3
"""Dependency-free source contract checks for the M14 read-only reference slice."""
from pathlib import Path

root = Path(__file__).resolve().parents[1]
src = (root / "internal" / "verticalslice" / "readonly.go").read_text()
required = [
    "tasks.Create", "tasks.MarkReady", "tasks.Start", "authority.Issue",
    "tools.Invoke", "observations.Record", "observations.VerifyIntegrity",
    "tasks.RequestCompletion", "tasks.BeginVerification", "verification.Create",
    "verification.Resolve", "verification.CreateCheckpoint", "tasks.CompleteVerified",
    "WorkerPrincipalID == cmd.VerifierPrincipalID",
    "authority.ActionRead", "policy.VerificationV1",
]
missing = [x for x in required if x not in src]
assert not missing, f"M14 vertical slice missing contracts: {missing}"
assert "ActionMutate" not in src and "ActionExternalSend" not in src, "M14 read-only slice grants mutation authority"
print("M14 read-only vertical slice contract: PASS")
