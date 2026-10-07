package provideronboarding

import "errors"

type PresetID string

const (
	PresetOmniRoute         PresetID = "omniroute"
	PresetOpenAIChatGPTPlan PresetID = "openai_chatgpt_plan"
	PresetOpenAI            PresetID = "openai"
	PresetAnthropic         PresetID = "anthropic"
	PresetGemini            PresetID = "google_gemini"
	PresetVertexAI          PresetID = "google_vertex_ai"
	PresetAzureOpenAI       PresetID = "azure_openai"
	PresetBedrock           PresetID = "amazon_bedrock"
	PresetXAI               PresetID = "xai"
	PresetMistral           PresetID = "mistral"
	PresetGroq              PresetID = "groq"
	PresetDeepSeek          PresetID = "deepseek"
	PresetOpenRouter        PresetID = "openrouter"
	PresetTogether          PresetID = "together"
	PresetFireworks         PresetID = "fireworks"
	PresetCerebras          PresetID = "cerebras"
	PresetCohere            PresetID = "cohere"
	PresetPerplexity        PresetID = "perplexity"
	PresetCloudflare        PresetID = "cloudflare_workers_ai"
	PresetNVIDIA            PresetID = "nvidia_nim"
	PresetSambaNova         PresetID = "sambanova"
	PresetHuggingFace       PresetID = "huggingface"
	PresetAlibaba           PresetID = "alibaba_model_studio"
	PresetNebius            PresetID = "nebius"
	PresetKimi              PresetID = "moonshot_kimi"
	PresetMiniMax           PresetID = "minimax"
	PresetNousPortal        PresetID = "nous_portal"
	PresetQwenOAuth         PresetID = "qwen_oauth"
	PresetMiniMaxOAuth      PresetID = "minimax_oauth"
	PresetXAIOAuth          PresetID = "xai_oauth"
	PresetCustomOpenAI      PresetID = "custom_openai_compatible"
)

type Preset struct {
	ID                         PresetID `json:"id"`
	DisplayName                string   `json:"display_name"`
	Description                string   `json:"description"`
	AccessMode                 string   `json:"access_mode"`
	Transport                  string   `json:"transport"`
	DefaultEndpoint            string   `json:"default_endpoint,omitempty"`
	EndpointTemplate           string   `json:"endpoint_template,omitempty"`
	EndpointEditable           bool     `json:"endpoint_editable"`
	AllowedHostSuffixes        []string `json:"allowed_host_suffixes,omitempty"`
	AuthType                   string   `json:"auth_type"`
	AuthMethods                []string `json:"auth_methods,omitempty"`
	OAuthMode                  string   `json:"oauth_mode,omitempty"`
	AuthHeader                 string   `json:"auth_header,omitempty"`
	AuthPrefix                 string   `json:"auth_prefix,omitempty"`
	AuthRaw                    bool     `json:"auth_raw,omitempty"`
	CredentialProvider         string   `json:"credential_provider,omitempty"`
	ProbePath                  string   `json:"probe_path,omitempty"`
	Automated                  bool     `json:"automated"`
	CostHint                   string   `json:"cost_hint"`
	UsagePool                  string   `json:"usage_pool"`
	RecommendedForFirstRun     bool     `json:"recommended_for_first_run"`
	DefaultZeroCostOnly        bool     `json:"default_zero_cost_only"`
	RequiresExplicitUsageOptIn bool     `json:"requires_explicit_usage_opt_in"`
	InstallSteps               []string `json:"install_steps"`
	Notes                      []string `json:"notes,omitempty"`
}

func openAICompatible(id PresetID, name, endpoint, provider string, suffixes ...string) Preset {
	return Preset{
		ID: id, DisplayName: name,
		Description: "Direct cloud inference through an OpenAI-compatible API while OnePane retains routing, budgets, policy and verification.",
		AccessMode:  "direct_api", Transport: "openai-compatible", DefaultEndpoint: endpoint,
		EndpointEditable: endpoint == "", AllowedHostSuffixes: suffixes,
		AuthType: "bearer", AuthMethods: []string{"api_key"}, AuthHeader: "Authorization", AuthPrefix: "Bearer", CredentialProvider: provider,
		ProbePath: "/models", Automated: true, CostHint: "paid_or_provider_managed", UsagePool: "direct_provider",
		RequiresExplicitUsageOptIn: true,
		InstallSteps:               []string{"Create/store the provider credential in OnePane Vault", "Select a model", "Probe the provider when model discovery is available", "Enable an explicit paid/subscription budget policy before autonomous use"},
	}
}

func Builtins() []Preset {
	ps := []Preset{
		{
			ID: PresetOmniRoute, DisplayName: "OmniRoute",
			Description: "Local OpenAI-compatible gateway that can aggregate free, subscription and paid providers.",
			AccessMode:  "openai_compatible_gateway", Transport: "openai-compatible", DefaultEndpoint: "http://127.0.0.1:20128/v1", EndpointEditable: true,
			AllowedHostSuffixes: []string{"localhost", "127.0.0.1", "::1"}, AuthType: "bearer", AuthHeader: "Authorization", AuthPrefix: "Bearer", CredentialProvider: "omniroute",
			ProbePath: "/models", Automated: true, CostHint: "provider_managed", UsagePool: "external_provider", RecommendedForFirstRun: true,
			DefaultZeroCostOnly: true, RequiresExplicitUsageOptIn: false,
			InstallSteps: []string{"Install/start OmniRoute", "Connect one or more providers", "Enable STRICT_ZERO_COST for the recommended first-run profile", "Create or select an OmniRoute endpoint credential if required", "Probe and register the gateway"},
		},
		{
			ID: PresetOpenAIChatGPTPlan, DisplayName: "ChatGPT plan (optional fallback)",
			Description: "Official Sign in with ChatGPT account-backed inference. Requests consume the user's included Work/Codex allowance and are never scheduler-eligible without explicit opt-in.",
			AccessMode:  "oauth_plan_usage", Transport: "openai-chatgpt-plan", DefaultEndpoint: "https://api.openai.com/v1", EndpointEditable: false,
			AllowedHostSuffixes: []string{"api.openai.com"}, AuthType: "oauth2-pkce", AuthMethods: []string{"oauth_pkce"}, OAuthMode: "browser_pkce", CredentialProvider: "openai-chatgpt-plan",
			CostHint: "included_subscription", UsagePool: "chatgpt_work_codex", RecommendedForFirstRun: false,
			DefaultZeroCostOnly: false, RequiresExplicitUsageOptIn: true,
			InstallSteps: []string{"Continue with ChatGPT", "Authorize ChatGPT plan usage", "Store OAuth credentials in the local credential store", "Discover account-visible models", "Register selected models as protected fallback deployments"},
		},
		openAICompatible(PresetOpenAI, "OpenAI API", "https://api.openai.com/v1", "openai", "api.openai.com"),
		{
			ID: PresetAnthropic, DisplayName: "Anthropic Claude API",
			Description: "Direct Anthropic Messages API with response normalization into OnePane's inference envelope.",
			AccessMode:  "direct_api", Transport: "anthropic", DefaultEndpoint: "https://api.anthropic.com", EndpointEditable: false,
			AllowedHostSuffixes: []string{"api.anthropic.com"}, AuthType: "api_key", AuthMethods: []string{"api_key","oauth_external"}, OAuthMode: "claude_subscription_external", AuthHeader: "x-api-key", CredentialProvider: "anthropic", ProbePath: "/v1/models",
			Automated: true, CostHint: "paid_or_provider_managed", UsagePool: "direct_provider", RequiresExplicitUsageOptIn: true,
			InstallSteps: []string{"Create/store an Anthropic API key", "Select a Claude model", "Probe the Models API", "Enable an explicit spend budget before autonomous use"},
		},
		openAICompatible(PresetGemini, "Google Gemini API", "https://generativelanguage.googleapis.com/v1beta/openai", "google-gemini", "generativelanguage.googleapis.com"),
		{
			ID: PresetVertexAI, DisplayName: "Google Vertex AI",
			Description: "Google Cloud Vertex AI OpenAI-compatible endpoint using a Google Cloud access token.",
			AccessMode:  "cloud_identity", Transport: "openai-compatible", EndpointTemplate: "https://aiplatform.googleapis.com/v1/projects/{project}/locations/{location}/endpoints/openapi", EndpointEditable: true,
			AllowedHostSuffixes: []string{"aiplatform.googleapis.com"}, AuthType: "bearer", AuthMethods: []string{"oauth2","adc","service_account"}, OAuthMode: "google_cloud_identity", AuthHeader: "Authorization", AuthPrefix: "Bearer", CredentialProvider: "google-vertex-ai",
			Automated: true, CostHint: "paid_or_provider_managed", UsagePool: "direct_provider", RequiresExplicitUsageOptIn: true,
			InstallSteps: []string{"Obtain a short-lived Google Cloud access token or brokered identity", "Enter project/location endpoint", "Select a Vertex model", "Enable an explicit spend budget"},
			Notes:        []string{"OnePane stores only the supplied token reference; automatic ADC/service-account refresh is a later credential-broker extension."},
		},
		{
			ID: PresetAzureOpenAI, DisplayName: "Azure OpenAI",
			Description: "Azure OpenAI v1-compatible inference endpoint.",
			AccessMode:  "direct_api", Transport: "openai-compatible", EndpointTemplate: "https://{resource}.openai.azure.com/openai/v1", EndpointEditable: true,
			AllowedHostSuffixes: []string{".openai.azure.com"}, AuthType: "api_key", AuthHeader: "api-key", AuthRaw: true, CredentialProvider: "azure-openai",
			ProbePath: "/models", Automated: true, CostHint: "paid_or_provider_managed", UsagePool: "direct_provider", RequiresExplicitUsageOptIn: true,
			InstallSteps: []string{"Store an Azure OpenAI API key", "Enter the resource endpoint", "Select a deployment/model", "Enable an explicit spend budget"},
		},
		{
			ID: PresetBedrock, DisplayName: "Amazon Bedrock",
			Description: "Amazon Bedrock OpenAI-compatible runtime using a Bedrock API key.",
			AccessMode:  "cloud_identity", Transport: "openai-compatible", EndpointTemplate: "https://bedrock-runtime.{region}.amazonaws.com/openai/v1", EndpointEditable: true,
			AllowedHostSuffixes: []string{".amazonaws.com", ".api.aws"}, AuthType: "bearer", AuthMethods: []string{"api_key","aws_credentials"}, AuthHeader: "Authorization", AuthPrefix: "Bearer", CredentialProvider: "amazon-bedrock",
			Automated: true, CostHint: "paid_or_provider_managed", UsagePool: "direct_provider", RequiresExplicitUsageOptIn: true,
			InstallSteps: []string{"Generate/store a short-term Bedrock API key", "Choose a regional Bedrock OpenAI endpoint", "Select an inference profile/model", "Enable an explicit spend budget"},
			Notes:        []string{"The recommended bedrock-runtime endpoint does not expose OpenAI GET /models; model selection is explicit."},
		},
	}

	ps = append(ps,
		Preset{
			ID: PresetNousPortal, DisplayName: "Nous Portal (OAuth)",
			Description: "Nous Research subscription gateway with one OAuth login and a broad frontier-model catalogue.",
			AccessMode: "oauth_subscription", Transport: "openai-compatible", DefaultEndpoint: "https://inference-api.nousresearch.com/v1", EndpointEditable: false,
			AllowedHostSuffixes: []string{"inference-api.nousresearch.com"}, AuthType: "oauth_external", AuthMethods: []string{"oauth_device_code","oauth_pkce"}, OAuthMode: "nous_portal", CredentialProvider: "nous-portal",
			ProbePath: "/models", Automated: true, CostHint: "included_subscription", UsagePool: "nous_subscription", RequiresExplicitUsageOptIn: true,
			InstallSteps: []string{"Continue with Nous Research", "Authorize the Portal subscription", "Discover account-visible models", "Register selected models as protected provider routes"},
		},
		Preset{
			ID: PresetQwenOAuth, DisplayName: "Qwen Portal (OAuth)",
			Description: "Consumer Qwen Portal access using browser OAuth rather than a DashScope API key.",
			AccessMode: "oauth_subscription", Transport: "openai-compatible", DefaultEndpoint: "https://portal.qwen.ai/v1", EndpointEditable: false,
			AllowedHostSuffixes: []string{"portal.qwen.ai"}, AuthType: "oauth_external", AuthMethods: []string{"oauth_pkce"}, OAuthMode: "qwen_portal", CredentialProvider: "qwen-oauth",
			ProbePath: "/models", Automated: true, CostHint: "included_subscription", UsagePool: "qwen_portal", RequiresExplicitUsageOptIn: true,
		},
		Preset{
			ID: PresetMiniMaxOAuth, DisplayName: "MiniMax Portal (OAuth)",
			Description: "MiniMax subscription access using the portal OAuth flow.",
			AccessMode: "oauth_subscription", Transport: "anthropic", DefaultEndpoint: "https://api.minimax.io/anthropic", EndpointEditable: false,
			AllowedHostSuffixes: []string{"api.minimax.io"}, AuthType: "oauth_external", AuthMethods: []string{"oauth_device_code","oauth_pkce"}, OAuthMode: "minimax_portal", CredentialProvider: "minimax-oauth",
			Automated: true, CostHint: "included_subscription", UsagePool: "minimax_portal", RequiresExplicitUsageOptIn: true,
		},
		Preset{
			ID: PresetXAIOAuth, DisplayName: "xAI Grok (OAuth)",
			Description: "SuperGrok / X Premium+ OAuth route, separate from xAI API-key billing.",
			AccessMode: "oauth_subscription", Transport: "openai-compatible", DefaultEndpoint: "https://api.x.ai/v1", EndpointEditable: false,
			AllowedHostSuffixes: []string{"api.x.ai"}, AuthType: "oauth_external", AuthMethods: []string{"oauth_device_code"}, OAuthMode: "xai_device_code", CredentialProvider: "xai-oauth",
			ProbePath: "/models", Automated: true, CostHint: "included_subscription", UsagePool: "xai_subscription", RequiresExplicitUsageOptIn: true,
		},
	)

	for _, p := range []Preset{
		openAICompatible(PresetXAI, "xAI", "https://api.x.ai/v1", "xai", "api.x.ai"),
		openAICompatible(PresetMistral, "Mistral AI", "https://api.mistral.ai/v1", "mistral", "api.mistral.ai"),
		openAICompatible(PresetGroq, "Groq", "https://api.groq.com/openai/v1", "groq", "api.groq.com"),
		openAICompatible(PresetDeepSeek, "DeepSeek", "https://api.deepseek.com", "deepseek", "api.deepseek.com"),
		openAICompatible(PresetOpenRouter, "OpenRouter", "https://openrouter.ai/api/v1", "openrouter", "openrouter.ai"),
		openAICompatible(PresetTogether, "Together AI", "https://api.together.xyz/v1", "together", "api.together.xyz"),
		openAICompatible(PresetFireworks, "Fireworks AI", "https://api.fireworks.ai/inference/v1", "fireworks", "api.fireworks.ai"),
		openAICompatible(PresetCerebras, "Cerebras", "https://api.cerebras.ai/v1", "cerebras", "api.cerebras.ai"),
		openAICompatible(PresetCohere, "Cohere", "https://api.cohere.ai/compatibility/v1", "cohere", "api.cohere.ai"),
		openAICompatible(PresetPerplexity, "Perplexity", "https://api.perplexity.ai", "perplexity", "api.perplexity.ai"),
		openAICompatible(PresetNVIDIA, "NVIDIA NIM", "https://integrate.api.nvidia.com/v1", "nvidia-nim", "integrate.api.nvidia.com"),
		openAICompatible(PresetSambaNova, "SambaNova Cloud", "https://api.sambanova.ai/v1", "sambanova", "api.sambanova.ai"),
		openAICompatible(PresetHuggingFace, "Hugging Face Inference", "https://router.huggingface.co/v1", "huggingface", "router.huggingface.co"),
		openAICompatible(PresetKimi, "Moonshot / Kimi", "https://api.moonshot.ai/v1", "moonshot-kimi", "api.moonshot.ai"),
		openAICompatible(PresetMiniMax, "MiniMax", "https://api.minimax.io/v1", "minimax", "api.minimax.io"),
	} {
		ps = append(ps, p)
	}

	cloudflare := openAICompatible(PresetCloudflare, "Cloudflare Workers AI", "", "cloudflare-workers-ai", "api.cloudflare.com")
	cloudflare.EndpointTemplate = "https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/v1"
	cloudflare.EndpointEditable = true
	ps = append(ps, cloudflare)
	alibaba := openAICompatible(PresetAlibaba, "Alibaba Model Studio", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "alibaba-model-studio", ".aliyuncs.com")
	alibaba.EndpointEditable = true
	ps = append(ps, alibaba)
	nebius := openAICompatible(PresetNebius, "Nebius AI", "", "nebius", ".nebius.com")
	nebius.EndpointEditable = true
	nebius.Description = "Nebius OpenAI-compatible inference endpoint; enter the endpoint issued for the selected Nebius service/region."
	ps = append(ps, nebius)
	custom := openAICompatible(PresetCustomOpenAI, "Custom OpenAI-compatible endpoint", "", "custom-openai-compatible")
	custom.EndpointEditable = true
	custom.AllowedHostSuffixes = nil
	custom.Description = "Explicit custom OpenAI-compatible endpoint. Because the destination is operator-supplied, use a credential created specifically for this connector."
	ps = append(ps, custom)
	return ps
}

func PresetByID(id PresetID) (Preset, bool) {
	for _, p := range Builtins() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

func FirstRunPresets() []Preset {
	all := Builtins()
	out := make([]Preset, 0, len(all))
	for _, p := range all {
		if p.RecommendedForFirstRun {
			out = append(out, p)
		}
	}
	return out
}

var (
	ErrInvalidOnboarding         = errors.New("invalid provider onboarding request")
	ErrProbeFailed               = errors.New("provider probe failed")
	ErrStrictZeroCostNotVerified = errors.New("strict zero-cost mode was requested but could not be verified")
	ErrCredentialScope           = errors.New("credential does not belong to the selected provider")
	ErrEndpointNotAllowed        = errors.New("provider endpoint is outside the preset's allowed hosts")
)
