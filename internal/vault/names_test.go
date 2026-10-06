package vault

import "testing"

func TestProviderCredentialNamesSeparateDirectAndOmniRoute(t *testing.T) {
	direct, err := ProviderCredentialLogicalName(ScopeDirectProvider, "xAI", CredentialAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	omni, err := ProviderCredentialLogicalName(ScopeOmniRoute, "xAI", CredentialAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	if direct != "provider/xai/api-key" {
		t.Fatalf("direct=%q", direct)
	}
	if omni != "omniroute/xai/api-key" {
		t.Fatalf("omni=%q", omni)
	}
	if direct == omni {
		t.Fatal("credential scopes must not collide")
	}
}

func TestProviderCredentialNameNormalizesFriendlyNames(t *testing.T) {
	got, err := ProviderCredentialLogicalName(ScopeOmniRoute, "Open AI", CredentialAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != "omniroute/open-ai/api-key" {
		t.Fatalf("got %q", got)
	}
}

func TestOmniRouteGatewayCredentialNameIsDistinct(t *testing.T) {
	if OmniRouteGatewayCredentialLogicalName() != "omniroute/gateway/access-token" {
		t.Fatal("unexpected gateway name")
	}
}

func TestPluginAndAgentRuntimeCredentialNamesAreIsolated(t *testing.T) {
	plugin, err := PluginCredentialLogicalName("GitHub", CredentialAccessToken)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := AgentRuntimeCredentialLogicalName("Claude Code", CredentialAccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if plugin != "plugin/github/access-token" {
		t.Fatalf("plugin=%q", plugin)
	}
	if runtime != "agent-runtime/claude-code/access-token" {
		t.Fatalf("runtime=%q", runtime)
	}
	if plugin == runtime {
		t.Fatal("credential namespaces collided")
	}
}

func TestGatewayCredentialLogicalName(t *testing.T) {
	got, err := GatewayCredentialLogicalName("Discord", "bot-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "gateway/discord/bot-token" {
		t.Fatalf("got %q", got)
	}
	if _, err := GatewayCredentialLogicalName("discord", "not-a-kind"); err == nil {
		t.Fatal("expected unsupported gateway credential kind to fail")
	}
}

func TestBotCredentialNamesAreIsolated(t *testing.T) {
	bot, err := BotCredentialLogicalName("custom-bot-relay", CredentialAccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if bot != "bot/custom-bot-relay/access-token" {
		t.Fatalf("unexpected bot credential name: %s", bot)
	}
	plugin, _ := PluginCredentialLogicalName("custom-bot-relay", CredentialAccessToken)
	if bot == plugin {
		t.Fatal("bot and plugin credential namespaces must remain isolated")
	}
}
