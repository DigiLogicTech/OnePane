#!/usr/bin/env python3
from pathlib import Path
import hashlib, re, sys

ROOT=Path(__file__).resolve().parents[1]
foundation=(ROOT/"internal/webui/static/app-foundation.js").read_text(encoding="utf-8")
canonical=(ROOT/"internal/webui/static/app.js").read_text(encoding="utf-8")
html=(ROOT/"internal/webui/static/index.html").read_text(encoding="utf-8")

def definitions(src):
    out={}
    for line_no,line in enumerate(src.splitlines(),1):
        match=re.match(r"^\s*(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(",line)
        if not match:
            match=re.match(r"^\s*([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?function\s*\(",line)
        if match:
            out.setdefault(match.group(1),[]).append(line_no)
    return out

expected_foundation_blob="8bb6eec38da56b85e6bd6a460fce0e286b9d3543"
foundation_bytes=foundation.encode("utf-8")
actual_foundation_blob=hashlib.sha1(b"blob "+str(len(foundation_bytes)).encode("ascii")+b"\0"+foundation_bytes).hexdigest()

foundation_defs=definitions(foundation)
canonical_defs=definitions(canonical)
legacy_duplicate_budget={
    'activityCard': 2,
    'applyTheme': 2,
    'attentionCard': 2,
    'attentionFromLiveData': 2,
    'bindShell': 2,
    'effectiveThemePalette': 2,
    'enableWorkspaceDrag': 2,
    'metric': 2,
    'nodesCard': 2,
    'omniQA': 2,
    'openAttentionPopover': 2,
    'openCommandPalette': 2,
    'openProviderDialog': 2,
    'openThemePopover': 3,
    'providersCard': 2,
    'qa4AddInspectorTab': 2,
    'qa4AddWorkspaceComponent': 2,
    'qa4DefaultSandbox': 2,
    'qa4DefaultWorkspace': 4,
    'qa4InspectorOverview': 3,
    'qa4ProjectSandbox': 2,
    'qa4WorkspaceCatalogue': 2,
    'qa4Workspaces': 2,
    'qa4WorkspaceWidgetContent': 2,
    'qa6BindProjectComponents': 2,
    'qa6ComponentContent': 2,
    'qa6FollowContent': 2,
    'qa6InspectorTabs': 2,
    'renderActiveView': 3,
    'renderInspector': 4,
    'renderIntegrations': 2,
    'renderMobileNav': 2,
    'renderModels': 5,
    'renderNav': 2,
    'renderNodes': 2,
    'renderProjects': 4,
    'renderProviders': 2,
    'renderSecrets': 3,
    'renderSettings': 2,
    'renderTasks': 2,
    'renderWorkspaces': 2,
    'renderWorkspaceWidget': 2,
    'resourceCard': 2,
    'taskCard': 2
}
critical=["renderNav","renderOperations","renderProjects","renderModels","renderNodes","renderAgents","renderSkills","renderSettings","startProductTour","bindShell","renderActiveView","qa4BindWorkspaceEdit","qa4AddWorkspaceComponent"]

failed=[]
def fail(msg): failed.append(msg); print("[FAIL] "+msg)
def ok(msg): print("[PASS] "+msg)

if actual_foundation_blob!=expected_foundation_blob:
    fail(f"compatibility foundation changed: {actual_foundation_blob}; review deliberately and update the frozen hash")
else:
    ok("compatibility foundation content is frozen")

canonical_dupes={name:lines for name,lines in canonical_defs.items() if len(lines)>1}
if canonical_dupes: fail(f"canonical runtime contains duplicate definitions: {canonical_dupes}")
else: ok("canonical runtime has no duplicate function definitions")

for name in critical:
    count=len(canonical_defs.get(name,[]))
    if count!=1: fail(f"canonical runtime must define {name} exactly once, found {count}")
    else: ok(f"canonical runtime owns {name}")

legacy_dupes={name:len(lines) for name,lines in foundation_defs.items() if len(lines)>1}
for name,count in legacy_dupes.items():
    budget=legacy_duplicate_budget.get(name)
    if budget is None: fail(f"new duplicate definition introduced in compatibility foundation: {name} x{count}")
    elif count>budget: fail(f"duplicate definition budget increased for {name}: {count}>{budget}")
for name,budget in legacy_duplicate_budget.items():
    count=legacy_dupes.get(name,0)
    if count<=budget: pass
if not any("compatibility foundation" in x or "budget increased" in x for x in failed):
    ok("legacy duplicate debt is bounded and can only decrease")

marker="/* === Alpha 3.1 canonical product layer === */"
if marker in foundation: fail("canonical runtime marker leaked into compatibility foundation")
else: ok("compatibility foundation does not claim canonical ownership")
if canonical.count(marker)!=1: fail("canonical runtime marker must appear exactly once")
else: ok("canonical runtime marker is unique")

boot_call=re.compile(r"(?<!function )\bbootOnePane\s*\(\s*\)")
foundation_boots=len(boot_call.findall(foundation))
canonical_boots=len(boot_call.findall(canonical))
if foundation_boots: fail(f"compatibility foundation must not boot the application; found {foundation_boots} call(s)")
else: ok("only canonical runtime may boot the application")
if canonical_boots!=1: fail(f"canonical runtime must boot exactly once; found {canonical_boots}")
else: ok("canonical runtime boots exactly once")

foundation_tag='<script src="/app-foundation.js"></script>'
canonical_tag='<script src="/app.js"></script>'
if foundation_tag not in html or canonical_tag not in html or html.index(foundation_tag)>html.index(canonical_tag):
    fail("index.html must load compatibility foundation before canonical runtime")
else: ok("script load order is deterministic")

if len(canonical.splitlines())>800: fail("canonical runtime exceeded 800-line review budget; split features before adding more")
else: ok(f"canonical runtime remains reviewable at {len(canonical.splitlines())} lines")

if failed:
    print(f"\nFRONTEND ARCHITECTURE: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    sys.exit(1)
print("\nFRONTEND ARCHITECTURE: ALL CHECKS PASSED")
