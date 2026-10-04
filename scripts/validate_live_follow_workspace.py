#!/usr/bin/env python3
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
app=(ROOT/'internal/webui/static/app.js').read_text(encoding='utf-8')
css=(ROOT/'internal/webui/static/style.css').read_text(encoding='utf-8')
sse=(ROOT/'internal/api/sse.go').read_text(encoding='utf-8')
op=(ROOT/'internal/operation/coordinator.go').read_text(encoding='utf-8')
checks={
  'Follow component registered': "follow:{title:'Follow'" in app,
  'workspace components include logs agents terminal verification checkpoints chat': all((f"{x}:{{title:" in app) for x in ['logs','agents','terminal','verification','checkpoints','chat']),
  'workspace maximize control': 'qa6ToggleMaximize' in app and 'data-qa6-max' in app,
  'workspace to Inspector control': 'qa6OpenInInspector' in app and 'data-qa6-inspector' in app,
  'Inspector tiled panels': 'qa6RenderInspectorPanels' in app and 'Add Inspector tile' in app,
  'slash commands remain in workspace chat': 'suggestChatCommands(input.value)' in app and 'qa6RunProjectSlash' in app,
  'realtime EventSource': 'new EventSource' in app and 'generic=1' in app,
  'generic SSE mode': 'genericEvent' in sse and 'eventName = "onepane"' in sse,
  'operation events expose resource/tool/task': all(x in op for x in ['"task_id": op.TaskID','"resource_ref": op.ResourceRef','"tool_id": op.ToolID']),
  'Follow styles': '.qa6-follow-surface' in css and '.qa6-maximized-widget' in css,
}
failed=[k for k,v in checks.items() if not v]
for k,v in checks.items(): print(f"[{'PASS' if v else 'FAIL'}] {k}")
if failed: raise SystemExit(f"LIVE FOLLOW WORKSPACE: {len(failed)} checks failed")
print(f"LIVE FOLLOW WORKSPACE: {len(checks)}/{len(checks)} CHECKS PASSED")
