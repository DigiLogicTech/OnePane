#!/usr/bin/env python3
from pathlib import Path
ui=Path("internal/webui/static/app.js").read_text()
checks={
 "typed snapshot parser":"qa31SnapshotForEvent" in ui,
 "supported browser snapshot":"browser:['Browser'" in ui,
 "supported editor snapshot":"editor:['Editor'" in ui,
 "supported computer snapshot":"computer:['Computer'" in ui,
 "supported terminal snapshot":"terminal:['Terminal'" in ui,
 "node attribution":"snap.node_id" in ui,
 "member attribution":"snap.member_id" in ui,
 "artifact reference":"snap.artifact_ref" in ui,
 "sequence ordering":"a.s.sequence-b.s.sequence" in ui,
 "heuristic fallback":"if(/browser|web|http/.test(hay))" in ui,
}
bad=[k for k,v in checks.items() if not v]
for k,v in checks.items(): print(f"[{'PASS' if v else 'FAIL'}] {k}")
if bad: raise SystemExit("Follow snapshot contract failed: "+", ".join(bad))
print(f"Alpha 3.1 Follow snapshot contract: {len(checks)}/{len(checks)} passed")
