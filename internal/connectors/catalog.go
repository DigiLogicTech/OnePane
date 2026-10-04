package connectors

import "strings"

type Preset struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"display_name"`
	Category         string            `json:"category"`
	BaseURL          string            `json:"base_url"`
	AuthHeader       string            `json:"auth_header"`
	AuthPrefix       string            `json:"auth_prefix,omitempty"`
	CredentialKind   string            `json:"credential_kind"`
	AllowedPrefixes  []string          `json:"allowed_path_prefixes"`
	StaticHeaders    map[string]string `json:"static_headers,omitempty"`
	ReadMethods      []string          `json:"read_methods"`
	ReadPostPrefixes []string          `json:"read_post_prefixes,omitempty"`
	MutationMethods  []string          `json:"mutation_methods"`
	SupportsSend     bool              `json:"supports_send"`
	SendPathMarkers  []string          `json:"send_path_markers,omitempty"`
	LogicalSuccess   string            `json:"logical_success,omitempty"`
	ReadToolID       string            `json:"read_tool_id"`
	MutateToolID     string            `json:"mutate_tool_id"`
	SendToolID       string            `json:"send_tool_id,omitempty"`
	Description      string            `json:"description"`
}

func preset(id, name, category, base string) Preset {
	return Preset{
		ID: id, DisplayName: name, Category: category, BaseURL: base,
		AuthHeader: "Authorization", AuthPrefix: "Bearer", CredentialKind: "access-token",
		AllowedPrefixes: []string{"/"}, ReadMethods: []string{"GET", "HEAD"}, MutationMethods: []string{"POST", "PUT", "PATCH", "DELETE"},
		ReadToolID: "plugin." + id + ".read", MutateToolID: "plugin." + id + ".mutate",
		Description: "Vault-backed API connector executed only through OnePane ToolGateway policy, capability and verification boundaries.",
	}
}

func Builtins() []Preset {
	gmail := preset("gmail", "Gmail", "email", "https://gmail.googleapis.com")
	gmail.AllowedPrefixes = []string{"/gmail/v1/"}
	gmail.SupportsSend, gmail.SendToolID = true, "plugin.gmail.send"
	gmail.SendPathMarkers = []string{"/messages/send", "/drafts/send"}

	gcal := preset("google-calendar", "Google Calendar", "calendar", "https://www.googleapis.com")
	gcal.AllowedPrefixes = []string{"/calendar/v3/"}
	gdrive := preset("google-drive", "Google Drive", "files", "https://www.googleapis.com")
	gdrive.AllowedPrefixes = []string{"/drive/"}

	outlook := preset("outlook-mail", "Outlook / Microsoft 365 Mail", "email", "https://graph.microsoft.com")
	outlook.AllowedPrefixes = []string{"/v1.0/", "/beta/"}
	outlook.SupportsSend, outlook.SendToolID = true, "plugin.outlook-mail.send"
	outlook.SendPathMarkers = []string{"/sendMail", "/send", "/reply", "/replyAll", "/forward"}
	outcal := preset("outlook-calendar", "Outlook / Microsoft 365 Calendar", "calendar", "https://graph.microsoft.com")
	outcal.AllowedPrefixes = []string{"/v1.0/", "/beta/"}
	onedrive := preset("onedrive", "OneDrive", "files", "https://graph.microsoft.com")
	onedrive.AllowedPrefixes = []string{"/v1.0/", "/beta/"}
	sharepoint := preset("sharepoint", "SharePoint", "files", "https://graph.microsoft.com")
	sharepoint.AllowedPrefixes = []string{"/v1.0/", "/beta/"}
	teams := preset("teams", "Microsoft Teams", "messaging", "https://graph.microsoft.com")
	teams.AllowedPrefixes = []string{"/v1.0/", "/beta/"}
	teams.SupportsSend, teams.SendToolID = true, "plugin.teams.send"
	teams.SendPathMarkers = []string{"/messages"}

	github := preset("github", "GitHub", "developer", "https://api.github.com")
	github.StaticHeaders = map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
	gitlab := preset("gitlab", "GitLab.com", "developer", "https://gitlab.com")
	gitlab.AllowedPrefixes = []string{"/api/v4/"}

	slack := preset("slack", "Slack", "messaging", "https://slack.com")
	slack.AllowedPrefixes = []string{"/api/"}
	slack.SupportsSend, slack.SendToolID = true, "plugin.slack.send"
	slack.SendPathMarkers = []string{"/api/chat.", "/api/files.upload"}
	slack.LogicalSuccess = "slack_ok"

	discord := preset("discord", "Discord", "messaging", "https://discord.com")
	discord.AllowedPrefixes = []string{"/api/"}
	discord.AuthPrefix = "Bot"
	discord.SupportsSend, discord.SendToolID = true, "plugin.discord.send"
	discord.SendPathMarkers = []string{"/messages"}

	notion := preset("notion", "Notion", "knowledge", "https://api.notion.com")
	notion.AllowedPrefixes = []string{"/v1/"}
	notion.ReadMethods = []string{"GET", "HEAD", "POST"}
	notion.ReadPostPrefixes = []string{"/v1/search"}
	notion.StaticHeaders = map[string]string{"Notion-Version": "2022-06-28"}

	linear := preset("linear", "Linear", "project_management", "https://api.linear.app")
	linear.AllowedPrefixes = []string{"/graphql"}
	linear.ReadMethods = []string{"POST"}
	linear.ReadPostPrefixes = []string{"/graphql"}
	linear.MutationMethods = []string{"POST"}

	dropbox := preset("dropbox", "Dropbox", "files", "https://api.dropboxapi.com")
	dropbox.AllowedPrefixes = []string{"/2/"}
	dropbox.ReadMethods = []string{"POST"}
	dropbox.ReadPostPrefixes = []string{"/2/files/list_folder", "/2/files/get_metadata", "/2/files/search_v2", "/2/files/get_temporary_link", "/2/sharing/list_", "/2/users/get_current_account"}
	dropbox.MutationMethods = []string{"POST"}

	asana := preset("asana", "Asana", "project_management", "https://app.asana.com")
	asana.AllowedPrefixes = []string{"/api/1.0/"}
	jira := preset("jira", "Jira Cloud", "project_management", "https://api.atlassian.com")
	jira.AllowedPrefixes = []string{"/ex/jira/"}
	confluence := preset("confluence", "Confluence Cloud", "knowledge", "https://api.atlassian.com")
	confluence.AllowedPrefixes = []string{"/ex/confluence/"}
	hubspot := preset("hubspot", "HubSpot", "crm", "https://api.hubapi.com")
	todoist := preset("todoist", "Todoist", "productivity", "https://api.todoist.com")
	todoist.AllowedPrefixes = []string{"/api/", "/rest/"}

	return []Preset{gmail, gcal, gdrive, outlook, outcal, onedrive, sharepoint, teams, github, gitlab, slack, discord, notion, linear, dropbox, asana, jira, confluence, hubspot, todoist}
}

func ByID(id string) (Preset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range Builtins() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
