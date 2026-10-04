#!/usr/bin/env python3
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
def read(rel):
    p=ROOT/rel
    if not p.exists(): raise SystemExit(f"M32 Bot Runtime validation: FAIL: missing {rel}")
    return p.read_text(encoding='utf-8')
def req(rel,*needles):
    t=read(rel); missing=[n for n in needles if n not in t]
    if missing: raise SystemExit(f"M32 Bot Runtime validation: FAIL: {rel} missing {missing}")
    return t
req('migrations/0015_bot_runtime.sql','CREATE TABLE bot_connections','CREATE TABLE bot_profiles','CREATE TABLE bot_sessions','CREATE TABLE bot_messages','CREATE TABLE bot_turns',"'harness_api','hosted_surface','relay_api'")
cat=req('internal/botruntime/catalog.go','Hermes Bot Mode','ChatGPT GPT','Grok Bot','Gemini Gem','Microsoft Copilot Agent','Poe Bot','Dify App / Bot','Flowise Chatflow / Agentflow','n8n Chat / Agent','custom-bot-relay')
if cat.count('ID:') < 10: raise SystemExit('M32 Bot Runtime validation: FAIL: bot preset catalog unexpectedly small')
req('internal/botruntime/adapter.go','/api/sessions','/v1/bot/chat','Idempotency-Key','http.ErrUseLastResponse','invalid Hermes profile name','unknown')
req('internal/botruntime/service.go','authority','native_runtime','onepane_autonomous','ErrHostedSurface','ErrUnknownOutcome','ValidateBotCredentialRef','bot.turn_succeeded')
req('internal/vault/names.go','BotCredentialLogicalName','bot/','ValidateBotCredentialRef','ProviderType: "bot:"')
req('internal/api/server.go','GET /v1/bot-presets','POST /v1/vault/bot-credentials','POST /v1/bot-connections','POST /v1/bots','POST /v1/bots/{botID}/sessions','POST /v1/bot-sessions/{sessionID}/messages')
req('internal/api/bots.go','bot.read','bot.write','bot.chat','ErrHostedSurface','ErrUnknownOutcome')
req('internal/bootstrap/bootstrap.go','botruntime.NewService','Bots: botRuntimeService')
req('cmd/harnessd/main.go','SetBots(runtime.Bots)')
req('internal/webui/static/index.html','data-view="bots"')
req('internal/webui/static/app.js','renderBots','Open provider Bot','Native runtime authority')
req('docs/bot-runtime.md','native authority','bot/hermes/api-key','agent-runtime/hermes/access-token','OnePane Bot Protocol v1','unknown')
print('M32 Bot Runtime validation: PASS')
