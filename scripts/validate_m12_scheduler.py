#!/usr/bin/env python3
"""Dependency-free M12 scheduler/provider safety checks."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
scheduler = (ROOT / "internal/scheduler/types.go").read_text()
presets = (ROOT / "internal/provideronboarding/types.go").read_text()
omni = (ROOT / "internal/provideronboarding/omniroute.go").read_text()
browser = (ROOT / "internal/browserworkspace/policy.go").read_text()

required_scheduler = [
    "AllowSubscriptionUsage",
    "AllowPotentialMonetarySpend",
    "candidate consumes protected subscription allowance",
    "candidate may incur monetary cost",
    "resourceTier",
]
for token in required_scheduler:
    assert token in scheduler, f"missing scheduler invariant: {token}"

assert 'RecommendedForFirstRun: true' in presets
assert 'DefaultZeroCostOnly: true' in presets
assert 'RequiresExplicitUsageOptIn: true' in presets
assert 'chatgpt_work_codex' in presets
assert 'freeAccessPolicy' in omni and 'strict' in omni
assert 'CredentialExtraction: "forbidden"' in browser
assert 'SubscriptionUse:      "user_initiated_only"' in browser
assert 'func (Policy) Schedulable() bool { return false }' in browser

print("M12 scheduler/provider/browser safety invariants: PASS")
