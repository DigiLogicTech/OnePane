package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/DigiLogicTech/OnePane/internal/botruntime"
	"github.com/DigiLogicTech/OnePane/internal/vault"
)

func (s *Server) botPresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, botruntime.BuiltinPresets())
}

func (s *Server) createBotCredential(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "vault unavailable")
		return
	}
	var in struct {
		WorkspaceID  string               `json:"workspace_id"`
		BotPresetID  string               `json:"bot_preset_id"`
		Kind         vault.CredentialKind `json:"kind"`
		Value        string               `json:"value"`
		DisplayLabel string               `json:"display_label"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "vault.write") {
		return
	}
	ws := in.WorkspaceID
	out, err := s.vault.CreateBotCredential(r.Context(), vault.BotCredentialCommand{WorkspaceID: &ws, BotPresetID: in.BotPresetID, Kind: in.Kind, Value: []byte(in.Value), DisplayLabel: in.DisplayLabel, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listBotConnections(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.bots == nil {
		writeError(w, 503, "bot runtime unavailable")
		return
	}
	ws := r.URL.Query().Get("workspace_id")
	if !s.authorize(w, r, i, ws, "bot.read") {
		return
	}
	out, err := s.bots.ListConnections(r.Context(), ws)
	respondDomain(w, out, err, 200)
}
func (s *Server) createBotConnection(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.bots == nil {
		writeError(w, 503, "bot runtime unavailable")
		return
	}
	var in struct {
		WorkspaceID   string          `json:"workspace_id"`
		PresetID      string          `json:"preset_id"`
		DisplayName   string          `json:"display_name"`
		BaseURL       string          `json:"base_url"`
		CredentialRef *string         `json:"credential_ref"`
		Config        json.RawMessage `json:"config"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "bot.write") {
		return
	}
	out, err := s.bots.CreateConnection(r.Context(), botruntime.CreateConnectionCommand{WorkspaceID: in.WorkspaceID, PresetID: in.PresetID, DisplayName: in.DisplayName, BaseURL: in.BaseURL, CredentialRef: in.CredentialRef, Config: in.Config, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.bots == nil {
		writeError(w, 503, "bot runtime unavailable")
		return
	}
	ws := r.URL.Query().Get("workspace_id")
	if !s.authorize(w, r, i, ws, "bot.read") {
		return
	}
	out, err := s.bots.ListBots(r.Context(), ws)
	respondDomain(w, out, err, 200)
}
func (s *Server) createBot(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.bots == nil {
		writeError(w, 503, "bot runtime unavailable")
		return
	}
	var in struct {
		WorkspaceID  string          `json:"workspace_id"`
		ConnectionID string          `json:"connection_id"`
		RemoteBotID  string          `json:"remote_bot_id"`
		DisplayName  string          `json:"display_name"`
		Description  string          `json:"description"`
		AvatarURL    *string         `json:"avatar_url"`
		LaunchURL    *string         `json:"launch_url"`
		Capabilities json.RawMessage `json:"capabilities"`
		Metadata     json.RawMessage `json:"metadata"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "bot.write") {
		return
	}
	out, err := s.bots.CreateBot(r.Context(), botruntime.CreateBotCommand{WorkspaceID: in.WorkspaceID, ConnectionID: in.ConnectionID, RemoteBotID: in.RemoteBotID, DisplayName: in.DisplayName, Description: in.Description, AvatarURL: in.AvatarURL, LaunchURL: in.LaunchURL, Capabilities: in.Capabilities, Metadata: in.Metadata, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) botFor(w http.ResponseWriter, r *http.Request, cap string) (Identity, botruntime.Bot, bool) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return Identity{}, botruntime.Bot{}, false
	}
	if s.bots == nil {
		writeError(w, 503, "bot runtime unavailable")
		return Identity{}, botruntime.Bot{}, false
	}
	b, err := s.bots.Bot(r.Context(), r.PathValue("botID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return Identity{}, botruntime.Bot{}, false
	}
	if !s.authorize(w, r, i, b.WorkspaceID, cap) {
		return Identity{}, botruntime.Bot{}, false
	}
	return i, b, true
}
func (s *Server) listBotSessions(w http.ResponseWriter, r *http.Request) {
	_, b, ok := s.botFor(w, r, "bot.read")
	if !ok {
		return
	}
	out, err := s.bots.ListSessions(r.Context(), b.ID)
	respondDomain(w, out, err, 200)
}
func (s *Server) createBotSession(w http.ResponseWriter, r *http.Request) {
	i, b, ok := s.botFor(w, r, "bot.chat")
	if !ok {
		return
	}
	var in struct {
		Title     string `json:"title"`
		Canonical bool   `json:"canonical"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.bots.CreateSession(r.Context(), botruntime.CreateSessionCommand{BotID: b.ID, Title: in.Title, Canonical: in.Canonical, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) botSessionFor(w http.ResponseWriter, r *http.Request, cap string) (Identity, botruntime.Session, bool) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return Identity{}, botruntime.Session{}, false
	}
	if s.bots == nil {
		writeError(w, 503, "bot runtime unavailable")
		return Identity{}, botruntime.Session{}, false
	}
	ss, err := s.bots.Session(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return Identity{}, botruntime.Session{}, false
	}
	if !s.authorize(w, r, i, ss.WorkspaceID, cap) {
		return Identity{}, botruntime.Session{}, false
	}
	return i, ss, true
}
func (s *Server) getBotSession(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.botSessionFor(w, r, "bot.read")
	if ok {
		writeJSON(w, 200, ss)
	}
}
func (s *Server) listBotMessages(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.botSessionFor(w, r, "bot.read")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.bots.ListMessages(r.Context(), ss.ID, limit)
	respondDomain(w, out, err, 200)
}
func (s *Server) sendBotMessage(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.botSessionFor(w, r, "bot.chat")
	if !ok {
		return
	}
	var in struct {
		Text           string `json:"text"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.bots.SendMessage(r.Context(), botruntime.SendMessageCommand{SessionID: ss.ID, Text: in.Text, IdempotencyKey: in.IdempotencyKey, CreatedBy: i.PrincipalID})
	if errors.Is(err, botruntime.ErrHostedSurface) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "launch_url": out.LaunchURL})
		return
	}
	if errors.Is(err, botruntime.ErrUnknownOutcome) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "turn": out.Turn})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
