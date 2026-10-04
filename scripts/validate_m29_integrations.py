#!/usr/bin/env python3
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]

def read(rel):
    p = ROOT / rel
    if not p.exists():
        raise SystemExit(f"M29 integration validation: FAIL: missing {rel}")
    return p.read_text(encoding="utf-8")

def require(rel, *needles):
    text = read(rel)
    missing = [n for n in needles if n not in text]
    if missing:
        raise SystemExit(f"M29 integration validation: FAIL: {rel} missing {missing}")
    return text

providers = require(
    "internal/provideronboarding/types.go",
    "PresetOpenAI", "PresetAnthropic", "PresetGemini", "PresetVertexAI",
    "PresetAzureOpenAI", "PresetBedrock", "PresetXAI", "PresetMistral",
    "PresetGroq", "PresetDeepSeek", "PresetOpenRouter", "PresetTogether",
    "PresetFireworks", "PresetCerebras", "PresetCohere", "PresetCloudflare",
    "PresetNVIDIA", "PresetHuggingFace", "PresetAlibaba", "PresetKimi",
    "PresetMiniMax", "PresetCustomOpenAI", "AllowedHostSuffixes", "CredentialProvider",
)
require(
    "internal/provideronboarding/generic.go",
    "normalizeProviderURL", "ErrEndpointNotAllowed", "secret resolver is unavailable",
    "ValidateProviderCredentialRef", "ScopeDirectProvider", '"auth_raw":              p.AuthRaw',
)
require(
    "internal/inference/transport_openai.go",
    "AllowedHostSuffixes", "untrusted_credential_destination", "AuthRaw",
)
require(
    "internal/inference/transport_anthropic.go",
    "x-api-key", "anthropic-version", "untrusted_credential_destination",
)
require("internal/inference/transport.go", '{"anthropic", anthropic}')

harnesses = require(
    "internal/agentruntime/presets.go",
    "microsoft-agent-framework", "google-adk", "strands", "openai-agents-sdk",
    "langgraph", "crewai", "pydantic-ai", "llamaindex", "openhands", "goose",
    "opencode", "openclaw", "claude-code", "openai-codex", "gemini-cli", "aider",
    "cursor", "windsurf", "cline", "roo-code", "ProposalOnly", "Unmanaged",
)
require("internal/agentruntime/service.go", "SetCredentialValidator", "ValidateAgentRuntimeCredentialRef")
require("internal/agentruntime/transport_http.go", "insecure_credential_destination")

connectors = require(
    "internal/connectors/catalog.go",
    'preset("gmail"', 'preset("outlook-mail"', 'preset("github"', 'preset("slack"',
    'preset("google-drive"', 'preset("google-calendar"', 'preset("teams"', 'preset("notion"',
    'preset("linear"', 'preset("dropbox"', 'preset("jira"', 'preset("confluence"',
    "ReadToolID", "MutateToolID", "SendToolID", "SendPathMarkers", "ReadPostPrefixes",
)
require(
    "internal/connectors/http_adapter.go",
    "ValidatePluginCredentialRef", "http.ErrUseLastResponse", "is reserved",
    "external-send endpoint requires", "GraphQL mutation requires the mutate tool", "tool.KnownFailure",
)
require(
    "internal/connectors/register.go",
    "authority.ActionRead", "authority.ActionMutate", "authority.ActionExternalSend",
)
require(
    "internal/vault/names.go",
    '"plugin/"', '"agent-runtime/"', "ValidatePluginCredentialRef",
    "ValidateAgentRuntimeCredentialRef", "ScopeDirectProvider",
)
require(
    "internal/api/server.go",
    '"POST /v1/providers/probe"', '"POST /v1/providers"', '"GET /v1/agent-runtime-presets"',
    '"POST /v1/agent-runtimes"', '"GET /v1/plugin-presets"',
    '"POST /v1/vault/plugin-credentials"', '"POST /v1/vault/agent-runtime-credentials"',
)
require("internal/bootstrap/bootstrap.go", "connectors.RegisterBuiltins", "SetCredentialValidator")
require("cmd/harnessd/main.go", "SetAgentRuntimes")

# Count guards: make accidental catalog shrinkage fail structurally.
provider_ids = len(re.findall(r"^\s*Preset[A-Za-z0-9_]+\s+PresetID\s*=", providers, flags=re.M))
connector_ids = connectors.count('preset("')
harness_ids = harnesses.count('runtimePreset("') + harnesses.count('unmanagedPreset("')
if provider_ids < 26:
    raise SystemExit(f"M29 integration validation: FAIL: provider catalog too small ({provider_ids})")
if connector_ids < 20:
    raise SystemExit(f"M29 integration validation: FAIL: connector catalog too small ({connector_ids})")
if harness_ids < 30:
    raise SystemExit(f"M29 integration validation: FAIL: harness catalog too small ({harness_ids})")

print(f"M29 integration validation: PASS ({provider_ids} provider presets, {harness_ids} harness presets, {connector_ids} connectors)")
