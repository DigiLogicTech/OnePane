package gateway

import "strings"

type Capabilities struct {
	Inbound   bool `json:"inbound"`
	Outbound  bool `json:"outbound"`
	Threads   bool `json:"threads"`
	Images    bool `json:"images"`
	Files     bool `json:"files"`
	Reactions bool `json:"reactions"`
	Typing    bool `json:"typing"`
	Streaming bool `json:"streaming"`
	Voice     bool `json:"voice"`
}

type Preset struct {
	ID               string       `json:"id"`
	DisplayName      string       `json:"display_name"`
	DefaultMode      string       `json:"default_mode"`
	CredentialKind   string       `json:"credential_kind,omitempty"`
	DefaultBaseURL   string       `json:"default_base_url,omitempty"`
	CanonicalHosts   []string     `json:"canonical_hosts,omitempty"`
	SelfHosted       bool         `json:"self_hosted,omitempty"`
	RelayRecommended bool         `json:"relay_recommended,omitempty"`
	Capabilities     Capabilities `json:"capabilities"`
	Description      string       `json:"description"`
}

func cap(in, out, threads, images, files, reactions, typing, streaming, voice bool) Capabilities {
	return Capabilities{Inbound: in, Outbound: out, Threads: threads, Images: images, Files: files, Reactions: reactions, Typing: typing, Streaming: streaming, Voice: voice}
}

func p(id, name, mode, cred, base string, hosts []string, c Capabilities) Preset {
	return Preset{ID: id, DisplayName: name, DefaultMode: mode, CredentialKind: cred, DefaultBaseURL: base, CanonicalHosts: hosts, Capabilities: c,
		Description: "OnePane messaging gateway preset. Outbound delivery is durable and policy-governed; inbound capability metadata is reserved for conversational ingress."}
}

func Builtins() []Preset {
	telegram := p("telegram", "Telegram", "native_http", "bot-token", "https://api.telegram.org", []string{"api.telegram.org"}, cap(true, true, true, true, true, false, true, true, true))
	discord := p("discord", "Discord", "native_http", "bot-token", "https://discord.com", []string{"discord.com"}, cap(true, true, true, true, true, true, true, true, true))
	slack := p("slack", "Slack", "native_http", "bot-token", "https://slack.com", []string{"slack.com"}, cap(true, true, true, true, true, true, true, true, true))
	gchat := p("google-chat", "Google Chat", "native_http", "webhook-url", "", nil, cap(true, true, true, true, true, false, true, false, false))
	wa := p("whatsapp", "WhatsApp (bridge)", "relay", "relay-token", "", nil, cap(true, true, false, true, true, false, true, true, false))
	wa.RelayRecommended = true
	wacloud := p("whatsapp-cloud", "WhatsApp Cloud API", "native_http", "access-token", "https://graph.facebook.com", []string{"graph.facebook.com"}, cap(true, true, false, true, true, false, true, true, true))
	signal := p("signal", "Signal", "relay", "relay-token", "", nil, cap(true, true, false, true, true, false, true, true, false))
	signal.RelayRecommended = true
	sms := p("sms", "SMS / Twilio", "native_http", "auth-token", "https://api.twilio.com", []string{"api.twilio.com"}, cap(true, true, false, false, false, false, false, false, false))
	email := p("email", "Email (SMTP)", "smtp", "smtp-password", "", nil, cap(true, true, true, true, true, false, false, false, false))
	email.SelfHosted = true
	ha := p("home-assistant", "Home Assistant", "native_http", "access-token", "", nil, cap(true, true, false, false, false, false, false, false, false))
	ha.SelfHosted = true
	mattermost := p("mattermost", "Mattermost", "native_http", "access-token", "", nil, cap(true, true, true, true, true, false, true, true, true))
	mattermost.SelfHosted = true
	matrix := p("matrix", "Matrix", "native_http", "access-token", "", nil, cap(true, true, true, true, true, true, true, true, true))
	matrix.SelfHosted = true
	dingtalk := p("dingtalk", "DingTalk", "native_http", "webhook-url", "", nil, cap(true, true, false, true, true, true, false, true, false))
	feishu := p("feishu", "Feishu / Lark", "native_http", "webhook-url", "", nil, cap(true, true, true, true, true, true, true, true, true))
	wecom := p("wecom", "WeCom", "native_http", "webhook-url", "", nil, cap(true, true, false, true, true, false, false, true, true))
	wecomcb := p("wecom-callback", "WeCom Callback", "relay", "relay-token", "", nil, cap(true, true, false, false, false, false, false, false, false))
	wecomcb.RelayRecommended = true
	weixin := p("weixin", "Weixin / WeChat", "relay", "relay-token", "", nil, cap(true, true, false, true, true, false, true, true, true))
	weixin.RelayRecommended = true
	blue := p("bluebubbles", "BlueBubbles / iMessage", "relay", "relay-token", "", nil, cap(true, true, false, true, true, true, true, true, false))
	blue.RelayRecommended = true
	photon := p("photon", "Photon / iMessage", "relay", "relay-token", "", nil, cap(true, true, false, true, true, true, true, true, true))
	photon.RelayRecommended = true
	qq := p("qqbot", "QQ Bot", "relay", "relay-token", "", nil, cap(true, true, false, true, true, false, true, false, true))
	qq.RelayRecommended = true
	yuanbao := p("yuanbao", "Yuanbao", "relay", "relay-token", "", nil, cap(true, true, false, true, true, false, true, true, true))
	yuanbao.RelayRecommended = true
	teams := p("microsoft-teams", "Microsoft Teams", "native_http", "access-token", "https://graph.microsoft.com", []string{"graph.microsoft.com"}, cap(true, true, true, true, false, false, true, false, false))
	teamsMeet := p("microsoft-teams-meetings", "Microsoft Teams Meetings", "relay", "relay-token", "", nil, cap(true, true, true, true, true, false, true, false, true))
	teamsMeet.RelayRecommended = true
	graphWebhook := p("microsoft-graph-webhook", "Microsoft Graph Webhook", "relay", "relay-token", "", nil, cap(true, true, false, false, false, false, false, false, false))
	graphWebhook.RelayRecommended = true
	line := p("line", "LINE", "native_http", "access-token", "https://api.line.me", []string{"api.line.me"}, cap(true, true, false, true, true, false, true, false, false))
	ntfy := p("ntfy", "ntfy", "native_http", "access-token", "https://ntfy.sh", []string{"ntfy.sh"}, cap(true, true, false, false, false, false, false, false, false))
	ntfy.SelfHosted = true
	simplex := p("simplex", "SimpleX", "relay", "relay-token", "", nil, cap(true, true, false, true, true, false, true, false, true))
	simplex.RelayRecommended = true
	openwebui := p("open-webui", "Open WebUI", "relay", "relay-token", "", nil, cap(true, true, true, true, true, false, true, true, false))
	openwebui.RelayRecommended = true
	webhook := p("webhook", "Generic Webhook", "native_http", "webhook-url", "", nil, cap(true, true, true, true, true, false, false, false, false))
	webhook.SelfHosted = true
	raft := p("raft", "Raft", "relay", "relay-token", "", nil, cap(true, true, false, false, false, false, false, false, false))
	raft.RelayRecommended = true
	irc := p("irc", "IRC", "relay", "relay-token", "", nil, cap(true, true, false, false, false, false, false, false, false))
	irc.RelayRecommended = true
	buzz := p("buzz", "Buzz", "relay", "relay-token", "", nil, cap(true, true, true, true, false, false, false, false, false))
	buzz.RelayRecommended = true
	return []Preset{telegram, discord, slack, gchat, wa, wacloud, signal, sms, email, ha, mattermost, matrix, dingtalk, feishu, wecom, wecomcb, weixin, blue, photon, qq, yuanbao, teams, teamsMeet, graphWebhook, line, ntfy, simplex, openwebui, webhook, raft, irc, buzz}
}

func ByID(id string) (Preset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, x := range Builtins() {
		if x.ID == id {
			return x, true
		}
	}
	return Preset{}, false
}
