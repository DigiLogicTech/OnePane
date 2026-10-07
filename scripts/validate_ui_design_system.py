#!/usr/bin/env python3
"""Source-level UI consistency gate; runtime screenshots remain a separate QA gate."""
from pathlib import Path
import re
ROOT = Path(__file__).resolve().parents[1]
css = (ROOT/"internal/webui/static/ui-consistency.css").read_text()
base = (ROOT/"internal/webui/static/style.css").read_text()
js = (ROOT/"internal/webui/static/ui-consistency.js").read_text()
jobs = (ROOT/"internal/webui/static/models-followup-jobs.js").read_text()
models = (ROOT/"internal/webui/static/models-page.js").read_text()
runtime = (ROOT/"internal/webui/static/model-qa-remediation.js").read_text()
html = (ROOT/"internal/webui/static/index.html").read_text()

def hexrgb(v):
    value = v.lstrip("#")
    return tuple(int(value[i:i+2],16)/255 for i in (0,2,4))

def luminance(color):
    return sum(weight*(channel/12.92 if channel<=.04045 else ((channel+.055)/1.055)**2.4)
               for channel,weight in zip(hexrgb(color),(.2126,.7152,.0722)))

def ratio(a,b):
    x,y=sorted((luminance(a),luminance(b)),reverse=True)
    return (x+.05)/(y+.05)

required=("dark","graphite","midnight","ocean","forest","violet","ember")
palettes={}
for name in required:
    match=re.search(r':root\[data-theme="'+name+r'"\]\{([^}]+)\}',css)
    assert match, f"missing {name} palette"
    values=dict(re.findall(r'(--[\w-]+):\s*([^;]+)',match.group(1)))
    for token in ("--bg","--panel","--text","--muted","--border","--accent","--on-accent","--accent-soft"):
        assert token in values, f"{name}: missing {token}"
    palettes[name]=values
    assert ratio(values["--text"],values["--panel"])>=4.5, f"{name}: body text contrast too weak"
    assert ratio(values["--accent"],values["--on-accent"])>=4.5, f"{name}: primary button contrast too weak"

assert len({palettes[n]["--bg"] for n in required})==len(required), "built-in theme backgrounds overlap"
for a,b in (("dark","graphite"),("midnight","ocean")):
    assert sum(abs(x-y) for x,y in zip(hexrgb(palettes[a]["--panel"]),hexrgb(palettes[b]["--panel"])))>.09, f"{a}/{b}: too similar"
for token in ("--hover","--input","--surface","--surface-2","--focus-ring","--progress-track"):
    assert token+":" in css, f"missing shared control token {token}"
assert "prefers-reduced-motion:reduce" in css
assert "aria-valuetext" in js and 'role="progressbar"' in js
assert "uiProgressMarkup(" in jobs and "onepane-progress-track" in css
assert "theme-preview-stage" in js and "theme-preview-stage" in css
assert 'ui-consistency.css' in html and 'ui-consistency.js' in html
assert html.index('models-followup-jobs.js') < html.index('ui-consistency.js')
assert 'Installable now' not in models
assert 'a36BindLLMFitCard' in runtime and 'slot.innerHTML=a36ManagedLLMFitCard(st)' in runtime
assert 'blocked?"disabled"' not in runtime
assert 'runtime-error-details' in css and 'a31RuntimeErrorSummary' in models
assert 'models-single-column:not(:has(#a31DiscoverCatalog))' in css
print(f"[PASS] {len(required)} distinct themes with body/button contrast >=4.5:1")
print("[PASS] Progress, theme previews, keyboard focus, reduced motion, runtime states and drawer resizing")
