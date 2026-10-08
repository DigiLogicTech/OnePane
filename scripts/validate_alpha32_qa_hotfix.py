#!/usr/bin/env python3
"""QA regression guards for Windows Alpha 3.2 fixes."""
from pathlib import Path
r=Path(__file__).resolve().parents[1]
read=lambda p:(r/p).read_text(encoding="utf-8")
runtime=read("internal/webui/static/model-qa-remediation.js")
theme=read("internal/webui/static/compact-themes.js")
css=read("internal/webui/static/attention-drawer.css")
testbed=read("internal/localai/testbed.go")
storage=read("internal/localai/storage_lifecycle.go")
install=read("internal/webui/static/models-followup-jobs.js")
assert '$$("[data-a36-remove-backend]").forEach' in runtime, "llama uninstall button binding must use all-selector"
assert '\n $("[data-a36-remove-backend]").forEach' not in runtime, "single-element selector cannot call forEach"
assert "ensurePendingManualTestbedSpec" in testbed and "ON CONFLICT(deployment_id) DO NOTHING" in testbed, "Agent Check pending record repair missing"
assert "status='disabled'" in storage and "status='ready'" in storage, "disabled backend cleanup must protect active runtime roots"
assert "theme-compact-swatches" in theme and '<rect x="0"' in theme and '"fill="${escapeHtml(c)}"' in theme and "background-color:" not in theme, "theme palette chips missing"
assert "toLocaleUpperCase" in theme, "theme title capitalisation missing"
assert "#modelDownloadLabel" in css and "white-space:nowrap" in css, "header install indicator may wrap"
assert "Installing · " in install, "install indicator should be compact"
print("[PASS] QA hotfix: llama backend controls, pending Agent Check, safe runtime cleanup, compact palette and install header")
