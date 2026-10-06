package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type SecretBroker interface {
	Resolve(context.Context, string) (string, error)
	ValidateGatewayCredentialRef(context.Context, string, string) error
}

type sendAdapter struct {
	db      *sql.DB
	secrets SecretBroker
	client  *http.Client
}

func NewSendAdapter(db *sql.DB, secrets SecretBroker) tool.Adapter {
	return &sendAdapter{db: db, secrets: secrets, client: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (a *sendAdapter) ID() string      { return "builtin.gateway.send" }
func (a *sendAdapter) Version() string { return "1" }

type outbound struct {
	TargetID   string `json:"target_id"`
	DeliveryID string `json:"delivery_id"`
	Text       string `json:"text"`
	Title      string `json:"title,omitempty"`
}
type connectionRecord struct {
	ID, WorkspaceID, PresetID, Mode string
	CredentialRef                   *string
	Config                          json.RawMessage
}
type targetRecord struct {
	ID, WorkspaceID, Address string
	ThreadRef                *string
	Config                   json.RawMessage
}

func (a *sendAdapter) Invoke(ctx context.Context, req tool.AdapterRequest) (tool.AdapterResult, error) {
	var in outbound
	if json.Unmarshal(req.Input, &in) != nil || strings.TrimSpace(in.TargetID) == "" || strings.TrimSpace(in.Text) == "" {
		return tool.AdapterResult{}, tool.KnownFailure(errors.New("target_id and text are required"))
	}
	c, t, err := a.load(ctx, in.TargetID)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	if c.WorkspaceID != req.WorkspaceID || t.WorkspaceID != req.WorkspaceID {
		return tool.AdapterResult{}, tool.KnownFailure(errors.New("gateway target workspace mismatch"))
	}
	p, ok := ByID(c.PresetID)
	if !ok {
		return tool.AdapterResult{}, tool.KnownFailure(errors.New("unknown gateway preset"))
	}
	var secret string
	if c.CredentialRef != nil && strings.TrimSpace(*c.CredentialRef) != "" {
		if a.secrets == nil {
			return tool.AdapterResult{}, tool.KnownFailure(errors.New("secret broker unavailable"))
		}
		if err := a.secrets.ValidateGatewayCredentialRef(ctx, *c.CredentialRef, p.ID); err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
		secret, err = a.secrets.Resolve(ctx, *c.CredentialRef)
		if err != nil {
			return tool.AdapterResult{}, tool.KnownFailure(err)
		}
	}
	var receipt map[string]any
	switch c.Mode {
	case "relay":
		receipt, err = a.sendRelay(ctx, p, c, t, in, secret)
	case "smtp":
		receipt, err = a.sendSMTP(p, c, t, in, secret)
	case "native_http":
		receipt, err = a.sendNative(ctx, p, c, t, in, secret)
	default:
		err = fmt.Errorf("unsupported gateway delivery mode %q", c.Mode)
	}
	if err != nil {
		return tool.AdapterResult{}, err
	}
	out, _ := json.Marshal(receipt)
	return tool.AdapterResult{Summary: "gateway accepted notification for " + p.DisplayName, Result: out}, nil
}

func (a *sendAdapter) load(ctx context.Context, targetID string) (connectionRecord, targetRecord, error) {
	var c connectionRecord
	var t targetRecord
	var cred sql.NullString
	var cc, tc string
	var thread sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT c.id,c.workspace_id,c.preset_id,c.delivery_mode,c.credential_ref,c.config_json,t.id,t.workspace_id,t.address,t.thread_ref,t.config_json FROM gateway_targets t JOIN gateway_connections c ON c.id=t.connection_id WHERE t.id=? AND t.status='active' AND c.status='active'`, targetID).Scan(&c.ID, &c.WorkspaceID, &c.PresetID, &c.Mode, &cred, &cc, &t.ID, &t.WorkspaceID, &t.Address, &thread, &tc)
	if err != nil {
		return c, t, err
	}
	if cred.Valid {
		c.CredentialRef = &cred.String
	}
	if thread.Valid {
		t.ThreadRef = &thread.String
	}
	c.Config = json.RawMessage(cc)
	t.Config = json.RawMessage(tc)
	return c, t, nil
}
func config(raw json.RawMessage) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}
func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (a *sendAdapter) sendRelay(ctx context.Context, p Preset, c connectionRecord, t targetRecord, in outbound, secret string) (map[string]any, error) {
	m := config(c.Config)
	base := str(m, "relay_url")
	if base == "" {
		return nil, tool.KnownFailure(errors.New("relay_url is required"))
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return nil, tool.KnownFailure(errors.New("relay_url must be http(s)"))
	}
	if u.Scheme == "http" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return nil, tool.KnownFailure(errors.New("plaintext relay_url is allowed only on loopback"))
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/messages/send"
	body, _ := json.Marshal(map[string]any{"platform": p.ID, "target": t.Address, "thread_ref": t.ThreadRef, "text": in.Text, "title": in.Title, "delivery_id": in.DeliveryID})
	h, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, tool.KnownFailure(err)
	}
	h.Header.Set("Content-Type", "application/json")
	if secret != "" {
		h.Header.Set("Authorization", "Bearer "+secret)
	}
	return a.do(h, p.ID)
}

func (a *sendAdapter) sendNative(ctx context.Context, p Preset, c connectionRecord, t targetRecord, in outbound, secret string) (map[string]any, error) {
	cm := config(c.Config)
	base, baseErr := nativeBase(p, str(cm, "base_url"))
	if baseErr != nil {
		return nil, tool.KnownFailure(baseErr)
	}
	method := http.MethodPost
	headers := map[string]string{"Content-Type": "application/json"}
	var endpoint string
	var payload any
	switch p.ID {
	case "telegram":
		endpoint = base + "/bot" + url.PathEscape(secret) + "/sendMessage"
		payload = map[string]any{"chat_id": t.Address, "text": in.Text}
		if t.ThreadRef != nil {
			if n, e := strconv.ParseInt(*t.ThreadRef, 10, 64); e == nil {
				payload.(map[string]any)["message_thread_id"] = n
			}
		}
	case "discord":
		endpoint = base + "/api/v10/channels/" + url.PathEscape(t.Address) + "/messages"
		headers["Authorization"] = "Bot " + secret
		payload = map[string]any{"content": in.Text}
		if t.ThreadRef != nil {
			payload.(map[string]any)["message_reference"] = map[string]any{"message_id": *t.ThreadRef}
		}
	case "slack":
		endpoint = base + "/api/chat.postMessage"
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"channel": t.Address, "text": in.Text}
		if t.ThreadRef != nil {
			payload.(map[string]any)["thread_ts"] = *t.ThreadRef
		}
	case "whatsapp-cloud":
		phone := str(cm, "phone_number_id")
		if phone == "" {
			return nil, tool.KnownFailure(errors.New("phone_number_id required"))
		}
		endpoint = base + "/v23.0/" + url.PathEscape(phone) + "/messages"
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"messaging_product": "whatsapp", "to": t.Address, "type": "text", "text": map[string]any{"body": in.Text}}
	case "line":
		endpoint = base + "/v2/bot/message/push"
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"to": t.Address, "messages": []any{map[string]any{"type": "text", "text": in.Text}}}
	case "microsoft-teams":
		endpoint = base + "/v1.0/" + strings.TrimLeft(t.Address, "/") + "/messages"
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"body": map[string]any{"contentType": "text", "content": in.Text}}
	case "mattermost":
		if base == "" {
			return nil, tool.KnownFailure(errors.New("base_url required"))
		}
		endpoint = strings.TrimRight(base, "/") + "/api/v4/posts"
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"channel_id": t.Address, "message": in.Text}
		if t.ThreadRef != nil {
			payload.(map[string]any)["root_id"] = *t.ThreadRef
		}
	case "matrix":
		if base == "" {
			return nil, tool.KnownFailure(errors.New("base_url required"))
		}
		txn := url.PathEscape(in.DeliveryID)
		endpoint = strings.TrimRight(base, "/") + "/_matrix/client/v3/rooms/" + url.PathEscape(t.Address) + "/send/m.room.message/" + txn
		method = http.MethodPut
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"msgtype": "m.text", "body": in.Text}
	case "home-assistant":
		if base == "" {
			return nil, tool.KnownFailure(errors.New("base_url required"))
		}
		svc := str(cm, "notify_service")
		if svc == "" {
			svc = "notify/notify"
		}
		endpoint = strings.TrimRight(base, "/") + "/api/services/" + strings.TrimLeft(svc, "/")
		headers["Authorization"] = "Bearer " + secret
		payload = map[string]any{"message": in.Text, "title": in.Title, "target": t.Address}
	case "ntfy":
		if base == "" {
			base = "https://ntfy.sh"
		}
		endpoint = strings.TrimRight(base, "/") + "/" + url.PathEscape(t.Address)
		if secret != "" {
			headers["Authorization"] = "Bearer " + secret
		}
		payload = map[string]any{"topic": t.Address, "message": in.Text, "title": in.Title}
	case "sms":
		sid := str(cm, "account_sid")
		from := str(cm, "from")
		if sid == "" || from == "" {
			return nil, tool.KnownFailure(errors.New("account_sid and from required"))
		}
		endpoint = base + "/2010-04-01/Accounts/" + url.PathEscape(sid) + "/Messages.json"
		form := url.Values{"To": {t.Address}, "From": {from}, "Body": {in.Text}}.Encode()
		h, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(form))
		if err != nil {
			return nil, tool.KnownFailure(err)
		}
		h.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(sid+":"+secret)))
		return a.do(h, p.ID)
	case "google-chat", "dingtalk", "feishu", "wecom", "webhook":
		return a.sendWebhook(ctx, p, t, in, secret)
	default:
		return nil, tool.KnownFailure(fmt.Errorf("native delivery not implemented for %s; use relay mode", p.ID))
	}
	b, _ := json.Marshal(payload)
	h, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, tool.KnownFailure(err)
	}
	for k, v := range headers {
		h.Header.Set(k, v)
	}
	return a.do(h, p.ID)
}

func nativeBase(p Preset, override string) (string, error) {
	base := strings.TrimSpace(override)
	if base == "" {
		base = p.DefaultBaseURL
	}
	if base == "" && p.ID != "google-chat" && p.ID != "dingtalk" && p.ID != "feishu" && p.ID != "wecom" && p.ID != "webhook" {
		return "", errors.New("base_url is required")
	}
	if base == "" {
		return "", nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("gateway base_url must be http(s)")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "http" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", errors.New("plaintext gateway base_url is allowed only on loopback")
	}
	if !p.SelfHosted && len(p.CanonicalHosts) > 0 {
		ok := false
		for _, allowed := range p.CanonicalHosts {
			if strings.EqualFold(host, allowed) {
				ok = true
				break
			}
		}
		if !ok {
			return "", fmt.Errorf("gateway %s credential destination %s is not canonical", p.ID, host)
		}
	}
	return strings.TrimRight(base, "/"), nil
}

func (a *sendAdapter) sendWebhook(ctx context.Context, p Preset, t targetRecord, in outbound, secret string) (map[string]any, error) {
	u, err := url.Parse(secret)
	if err != nil || u.Scheme != "https" {
		return nil, tool.KnownFailure(errors.New("webhook credential must contain an https URL"))
	}
	host := strings.ToLower(u.Hostname())
	allowed := p.ID == "webhook" || (p.ID == "google-chat" && strings.HasSuffix(host, "googleapis.com")) || (p.ID == "dingtalk" && strings.HasSuffix(host, "dingtalk.com")) || (p.ID == "feishu" && (strings.HasSuffix(host, "feishu.cn") || strings.HasSuffix(host, "larksuite.com"))) || (p.ID == "wecom" && strings.HasSuffix(host, "weixin.qq.com"))
	if !allowed {
		return nil, tool.KnownFailure(errors.New("webhook host does not match gateway preset"))
	}
	var payload any = map[string]any{"text": in.Text}
	if p.ID == "dingtalk" {
		payload = map[string]any{"msgtype": "text", "text": map[string]any{"content": in.Text}}
	}
	if p.ID == "feishu" {
		payload = map[string]any{"msg_type": "text", "content": map[string]any{"text": in.Text}}
	}
	if p.ID == "wecom" {
		payload = map[string]any{"msgtype": "text", "text": map[string]any{"content": in.Text}}
	}
	b, _ := json.Marshal(payload)
	h, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(b))
	if err != nil {
		return nil, tool.KnownFailure(err)
	}
	h.Header.Set("Content-Type", "application/json")
	return a.do(h, p.ID)
}

func (a *sendAdapter) sendSMTP(p Preset, c connectionRecord, t targetRecord, in outbound, secret string) (map[string]any, error) {
	m := config(c.Config)
	host := str(m, "host")
	port := str(m, "port")
	user := str(m, "username")
	from := str(m, "from")
	if host == "" || port == "" || from == "" {
		return nil, tool.KnownFailure(errors.New("SMTP host, port and from are required"))
	}
	addr := host + ":" + port
	auth := smtp.PlainAuth("", user, secret, host)
	subj := in.Title
	if subj == "" {
		subj = "OnePane notification"
	}
	msg := []byte("To: " + t.Address + "\r\nFrom: " + from + "\r\nSubject: " + subj + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + in.Text + "\r\n")
	if err := smtp.SendMail(addr, auth, from, []string{t.Address}, msg); err != nil {
		return nil, err
	}
	return map[string]any{"gateway": p.ID, "accepted": true, "target": t.Address}, nil
}

func (a *sendAdapter) do(h *http.Request, gatewayID string) (map[string]any, error) {
	r, err := a.client.Do(h)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway %s returned HTTP %d", gatewayID, r.StatusCode)
	}
	if gatewayID == "slack" {
		var x struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &x) == nil && !x.OK {
			return nil, tool.KnownFailure(fmt.Errorf("slack API error: %s", x.Error))
		}
	}
	var body any
	if len(bytes.TrimSpace(data)) > 0 {
		if json.Unmarshal(data, &body) != nil {
			body = string(data)
		}
	} else {
		body = map[string]any{}
	}
	return map[string]any{"gateway": gatewayID, "accepted": true, "http_status": r.StatusCode, "body": body}, nil
}
