#!/usr/bin/env python3
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
def read(rel):
    p=ROOT/rel
    if not p.exists(): raise SystemExit(f"M30 gateway validation: FAIL: missing {rel}")
    return p.read_text(encoding='utf-8')
def req(rel,*needles):
    t=read(rel); missing=[x for x in needles if x not in t]
    if missing: raise SystemExit(f"M30 gateway validation: FAIL: {rel} missing {missing}")
    return t

mig=req('migrations/0013_gateway_notifications.sql','CREATE TABLE gateway_connections','CREATE TABLE gateway_targets','CREATE TABLE notification_rules','CREATE TABLE notification_deliveries','gateway_event_cursor','UNIQUE(workspace_id,idempotency_key)')
cat=req('internal/gateway/catalog.go','telegram','discord','slack','google-chat','whatsapp-cloud','signal','matrix','mattermost','bluebubbles','photon','microsoft-teams','line','ntfy','simplex','open-webui','webhook','raft','irc','buzz','Capabilities')
svc=req('internal/gateway/service.go','CapabilityID: "gateway.send"','ActionExternalSend','operation.ErrUnknownOutcome','status=\'unknown\'','task.completed','routine_occurrences','RecoverInterrupted')
req('internal/gateway/register.go','ID: "gateway.send"','authority.ActionExternalSend','policy.VerificationV1')
req('internal/gateway/adapter.go','ValidateGatewayCredentialRef','http.ErrUseLastResponse','nativeBase','credential destination','/v1/messages/send','message_thread_id','thread_ts')
req('internal/vault/names.go','"gateway/"','CreateGatewayCredential','ValidateGatewayCredentialRef')
req('internal/api/server.go','"GET /v1/gateway-presets"','"POST /v1/vault/gateway-credentials"','"POST /v1/gateways"','"POST /v1/gateway-targets"','"POST /v1/notification-rules"','"POST /v1/notifications"','notification-targets')
req('internal/bootstrap/bootstrap.go','gateway.Register','gateway.NewService','RecoverInterrupted')
req('cmd/harnessd/main.go','SetGateway','runtime.Gateway.Tick','Unknown transport outcomes')
count=len(re.findall(r'\bp\("',cat))
if count < 32: raise SystemExit(f"M30 gateway validation: FAIL: catalog too small ({count})")
print(f"M30 messaging gateway validation: PASS ({count} gateway presets)")
