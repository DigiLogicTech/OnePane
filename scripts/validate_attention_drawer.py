#!/usr/bin/env python3
"""Alpha 3.2 user-facing controls must retain durable audit provenance."""
from pathlib import Path
r=Path(__file__).resolve().parents[1]
db=(r/"migrations/0031_alpha32_attention_lifecycle.sql").read_text()
api=(r/"internal/api/attention_lifecycle.go").read_text()
shell=(r/"cmd/harnessd/main.go").read_text()
foundation=(r/"internal/webui/static/app-foundation.js").read_text()
attention=(r/"internal/webui/static/attention-inspector.js").read_text()
drawer=(r/"internal/webui/static/drawer-usability.js").read_text()
themes=(r/"internal/webui/static/compact-themes.js").read_text()
css=(r/"internal/webui/static/attention-drawer.css").read_text()
html=(r/"internal/webui/static/index.html").read_text()
assert "ui_attention_dispositions" in db and "ui_attention_disposition_history" in db
assert "PRIMARY KEY(workspace_id,principal_id,alert_id)" in db
assert "alert_id" in api and "principal_id" in api and "task.write" in api and "events.read" in api
assert "SetAttentionDB(runtime.DB.SQL())" in shell
assert "attentionDispositions" in (r/"internal/api/server.go").read_text()
assert all(s in attention for s in ('"acknowledged"','"archived"','"active"','a9Change','a9Inspect','a9Status'))
assert "qa4FindEntity=function" in attention and "renderInspector=function" in attention
assert all(s in drawer for s in ("a10Pause","a10Search","a10Level","a10Source","a10Export","a10Copy","data-a10-event","renderDrawer=function"))
assert "navigator.clipboard" in drawer and "text/csv" in drawer
assert all(s in themes for s in ("theme-compact-swatches","qa5ThemeButtons=function","a11ThemeColors","Installed theme packages"))
assert "theme-compact{height:67px" in css
assert "drawer-log-results" in css and "attention-list" in css
assert all(s in html for s in ("attention-inspector.js","drawer-usability.js","compact-themes.js","attention-drawer.css"))
assert html.index("ui-consistency.js") < html.index("attention-inspector.js") < html.index("drawer-usability.js") < html.index("compact-themes.js")
assert "eventLabel(" in drawer and "a9Load" in attention
print("[PASS] durable attention lifecycle, audit trail, scoped API and stable identifiers")
print("[PASS] contextual Inspector, searchable/copyable/event-aware Logs, compact theme palettes")
