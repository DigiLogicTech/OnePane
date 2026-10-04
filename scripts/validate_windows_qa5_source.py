#!/usr/bin/env python3
from pathlib import Path
root=Path(__file__).resolve().parents[1]
files={
 'app':(root/'internal/webui/static/app.js').read_text(),
 'css':(root/'internal/webui/static/style.css').read_text(),
 'setup':(root/'packaging/windows/setup/main.go').read_text(),
 'desktop':(root/'packaging/windows/desktop/main.go').read_text(),
 'api':(root/'internal/api/server.go').read_text(),
 'local':(root/'internal/localai/managed_deployments.go').read_text(),
 'components':(root/'internal/localai/components.go').read_text(),
 'sup':(root/'internal/localai/supervisor.go').read_text(),
 'testbed':(root/'internal/localai/testbed.go').read_text(),
 'boot':(root/'internal/bootstrap/bootstrap.go').read_text(),
}
checks=[
 ('alpha.3 installer version','0.1.0-alpha.3' in files['setup']),
 ('DigiLogic publisher','"Publisher"' in files['setup'] and '"DigiLogic"' in files['setup']),
 ('independent Project and Model Pool locations','Choose the default location for Project and Workspace data.' in files['setup'] and 'Choose the OnePane local model pool' in files['setup'] and 'project_root' in files['setup'] and 'model_pool_path' in files['setup']),
 ('desktop alpha.3 singleton','OnePaneDesktop-v0.1.0-alpha.3' in files['desktop']),
 ('OmniRoute removed from OS installer','Install optional OmniRoute now?' not in files['setup'] and 'optional runtimes are managed by the OnePane harness' in files['setup']),
 ('OmniRoute harness lifecycle','"omniroute"' in files['components'] and 'case "enable"' in files['components'] and 'case "disable"' in files['components']),
 ('Colibri removed from OS installer','Install the optional Colibri Large Model runtime now?' not in files['setup']),
 ('Colibri harness lifecycle','"colibri"' in files['components'] and 'installed_disabled' in files['components']),
 ('Colibri pinned version','ColibriRuntimeVersion = "1.12.1"' in files['local']),
 ('Colibri pinned SHA','1d0cc6760a6e53fcafe77376ca20ec254c6ff8995e55fba1bcd883278787f794' in files['local'] and 'ColibriRuntimeSource' in files['local']),
 ('Colibri register API','/v1/local-ai/colibri/register' in files['api']),
 ('managed deployments API','/v1/local-ai/deployments' in files['api']),
 ('Colibri OpenAI server launch','openai_server.py' in files['local'] and '--engine' in files['sup'] and '--model-id' in files['sup']),
 ('Colibri inference transport registered','"colibri"' in files['boot'] and 'LocalOpenAITransport' in files['boot']),
 ('qualifying model Testbed starts runtime','start managed runtime for testbed' in files['testbed']),
 ('defaults are inheritance only','Defaults are copied only when a workspace is created' in files['app'] and 'existing workspaces keep their own settings' in files['app']),
 ('new Workspace defaults only','Defaults for new Workspaces' in files['app'] and 'Existing workspaces were not changed' in files['app']),
 ('softer light theme','#e8edf2' in files['css']),
 ('two-tone themes','data-theme="aurora"' in files['css'] and 'data-theme="cobalt"' in files['css'] and 'data-theme="dusk"' in files['css']),
 ('install theme pack','Install theme pack' in files['app'] and 'qa5InstallThemePack' in files['app']),
 ('language support','Language & region' in files['app'] and 'Install language pack' in files['app'] and 'ja-JP' in files['app'] and 'zh-CN' in files['app']),
 ('Core Skills pack','Core Skills pack' in files['app'] and 'Skills never create authority' in files['app']),
 ('model runtime strategy','Managed Hot Swap' in files['app'] and 'Colibri Large Model' in files['app'] and '>Auto<' in files['app']),
 ('managed model inventory','/v1/local-ai/deployments' in files['app']),
 ('Agent Check UI','Agent Check' in files['app'] and 'qa5AgentCheck' in files['app']),
 ('spec sheet inspector','spec-sheet' in files['app'] and 'Spec sheet' in files['app']),
 ('Colibri folder picker','/desktop/folder-picker' in files['app'] and '/desktop/folder-picker' in files['desktop']),
 ('component harness API','/v1/local-ai/components' in files['app'] and '/v1/local-ai/components' in files['api']),
 ('OmniRoute separate','Direct cloud providers' in files['app'] and 'OmniRoute remains a separate optional router' in files['app']),
 ('OAuth connected/revoke','OAuth connected' in files['app'] and 'Revoke' in files['app']),
 ('Operations view-all hidden','qa5StripOperationViewAll' in files['app']),
 ('DigiLogic about branding','DigiLogic · GitHub: DigiLogicTech/OnePane' in files['app']),
]
failed=[]
for name,ok in checks:
 print(f"[{'PASS' if ok else 'FAIL'}] {name}")
 if not ok: failed.append(name)
print(f"\nWINDOWS QA5 SOURCE: {len(checks)-len(failed)}/{len(checks)} CHECKS PASSED")
raise SystemExit(1 if failed else 0)
