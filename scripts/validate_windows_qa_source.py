#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
ui = (ROOT/'internal/webui/static/app.js').read_text(encoding='utf-8')
html = (ROOT/'internal/webui/static/index.html').read_text(encoding='utf-8')
css = (ROOT/'internal/webui/static/style.css').read_text(encoding='utf-8')
api = (ROOT/'internal/api/server.go').read_text(encoding='utf-8')
omni = (ROOT/'internal/provideronboarding/omniroute.go').read_text(encoding='utf-8')
webui = (ROOT/'internal/webui/webui.go').read_text(encoding='utf-8')
desktop = (ROOT/'packaging/windows/desktop/main.go').read_text(encoding='utf-8')
setup = (ROOT/'packaging/windows/setup/main.go').read_text(encoding='utf-8')
verify = (ROOT/'packaging/windows/Verify-OnePane.cmd').read_text(encoding='utf-8')

checks = []
def check(name, cond):
    checks.append((name, bool(cond)))

# Guided tour / Windows keyboard language.
check('tour spotlight target exists', 'tour-spotlight' in ui and 'scrollIntoView' in ui)
check('tour uses Windows Ctrl+K', 'Ctrl+K' in ui and '⌘K' not in ui and '⌘K' not in html)

# Notifications backed by live control-plane state.
check('notification badge starts hidden/zero', 'data-notification-badge>0<' in html and 'badge hidden' in html)
check('notification centre is interactive', 'notification-center' in ui and 'markNotificationRead' in ui)
check('notifications derive from live data', 'attentionFromLiveData' in ui and 'refreshOperationalDataQA' in ui)

# Configurable/snap layouts.
check('operations configurable', 'operationsWidgets' in ui and 'data-op-size' in ui and 'data-add-op' in ui and 'resetOperationsLayout' in ui)
check('workspaces configurable', 'workspaceWidgets' in ui and 'data-ws-size' in ui and 'data-add-ws' in ui and 'resetWorkspaceLayout' in ui)
check('workspace freeform resize disabled', 'resize:none !important' in css)
check('operations uses 12-column snap grid', 'grid-template-columns:repeat(12' in css and 'operations-layout-grid' in css)

# Recoverability / tabs / themes / auth.
check('collapsed drawer and inspector restore controls', 'drawerRestore' in html and 'inspectorRestore' in html and 'syncPanelRestoreButtons' in ui)
check('new tab opens picker', "$('#newTabButton').onclick=openNewTabPicker" in ui and 'data-new-tab-route' in ui)
check('expanded theme family', all(f'data-theme="{x}"' in css for x in ['ocean','violet','ember']))
check('auth surface inherits theme variables', '.auth-gate' in css and 'var(--bg)' in css and 'prefers-color-scheme: light' in css)

# Previously dead actions now backed by real API calls.
check('new task flow', 'id="newTaskButton"' in ui and "apiRequest('/v1/tasks'" in ui and 'normal_task' in ui)
check('project configure flow', 'id="configureProject"' in ui and "apiRequest('/v1/projects'" in ui)
check('node pairing flow', 'id="newNode"' in ui and '/pair/confirm' in ui)
check('provider connection flow', 'id="newProvider"' in ui and "apiRequest('/v1/providers'" in ui)
check('integration configure flow', 'data-config-plugin' in ui and '/v1/vault/plugin-credentials' in ui)

# Secrets / providers / OmniRoute.
check('secrets full filtered catalogue', 'secret-catalogue' in ui and 'secretScope' in ui and 'secretStatus' in ui and 'data-toggle-secret' in ui)
check('provider dialog chooses Vault refs', 'providerCredential' in ui and 'vaultRef(r)' in ui and 'loadVaultRecords' in ui)
check('provider list API exposed', 'GET /v1/providers' in api and 'func (s *Server) listProviders' in api)
check('OmniRoute accepts remote HTTPS but not remote HTTP', 'u.Scheme != "https"' in omni and 'loopback && u.Scheme == "http"' in omni and 'must use HTTPS except loopback' in omni)
check('OmniRoute URL persisted and failure isolated', 'onepane:omniroute-url' in ui and 'This does not affect OnePane health' in ui)

# Models / model pool.
check('model catalogue is real and scrollable', '/v1/local-ai/catalog' in ui and 'model-catalogue-scroll' in css)
check('model pool GUI setting', 'modelPoolPath' in ui and '/v1/settings/local-ai' in ui)
check('installer model pool chooser', 'Choose the OnePane local model pool' in setup and 'model_pool_path' in setup and 'project_root' in setup)

# Real list/read APIs used by dashboards.
for route, handler in [('tasks','listTasks'),('projects','listProjects'),('events','listEvents')]:
    check(f'{route} list API exposed', f'GET /v1/{route}' in api and f'func (s *Server) {handler}' in api)

# Windows app shell integration.
check('desktop uses single authoritative backend WebUI', 'NewSingleHostReverseProxy' in desktop and '127.0.0.1:18181' in desktop)
check('desktop reserves native settings routes', '/desktop/settings' in desktop and '/desktop/settings/model-pool' in desktop)
check('app icon integrated', 'OnePane.ico' in desktop and 'OnePane.ico' in setup)
check('native title bar follows theme', 'DwmSetWindowAttribute' in desktop and 'onepane-theme|' in desktop)
check('double-click verifier exists', 'ExecutionPolicy Bypass' in verify and 'Verify-OnePane.ps1' in verify)

# Redirect regression / canonical root serving.
check('WebUI root direct serving', 'serveShell' in webui or 'index.html' in webui)
check('root redirect regression tests exist', 'TestRootAndIndexServeDirectlyWithoutRedirect' in (ROOT/'internal/webui/webui_test.go').read_text(encoding='utf-8') and 'TestSPAFallbackServesShellWithoutRedirect' in (ROOT/'internal/webui/webui_test.go').read_text(encoding='utf-8'))

failed = [name for name, ok in checks if not ok]
for name, ok in checks:
    print(f"[{'PASS' if ok else 'FAIL'}] {name}")
if failed:
    print(f"\nWINDOWS QA SOURCE: {len(failed)} CHECK(S) FAILED", file=sys.stderr)
    sys.exit(1)
print(f"\nWINDOWS QA SOURCE: ALL {len(checks)} CHECKS PASSED")
