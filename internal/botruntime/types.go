package botruntime

import (
	"encoding/json"
	"errors"
)

var (
	ErrInvalid        = errors.New("invalid bot runtime command")
	ErrHostedSurface  = errors.New("bot is available only on its provider-hosted surface")
	ErrUnknownOutcome = errors.New("bot turn outcome is unknown")
)

type Connection struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	PresetID      string          `json:"preset_id"`
	DisplayName   string          `json:"display_name"`
	SourceKind    string          `json:"source_kind"`
	AccessMode    string          `json:"access_mode"`
	SourceRef     *string         `json:"source_ref,omitempty"`
	BaseURL       *string         `json:"base_url,omitempty"`
	CredentialRef *string         `json:"credential_ref,omitempty"`
	Config        json.RawMessage `json:"config"`
	Status        string          `json:"status"`
	CreatedBy     string          `json:"created_by"`
	Revision      int64           `json:"revision"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     int64           `json:"updated_at"`
}

type Bot struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	ConnectionID string          `json:"connection_id"`
	RemoteBotID  string          `json:"remote_bot_id"`
	DisplayName  string          `json:"display_name"`
	Description  string          `json:"description"`
	AvatarURL    *string         `json:"avatar_url,omitempty"`
	LaunchURL    *string         `json:"launch_url,omitempty"`
	Capabilities json.RawMessage `json:"capabilities"`
	Metadata     json.RawMessage `json:"metadata"`
	Status       string          `json:"status"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
}

type Session struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	BotID           string          `json:"bot_id"`
	RemoteSessionID *string         `json:"remote_session_id,omitempty"`
	Title           string          `json:"title"`
	SessionKind     string          `json:"session_kind"`
	Status          string          `json:"status"`
	CreatedBy       string          `json:"created_by"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       int64           `json:"created_at"`
	UpdatedAt       int64           `json:"updated_at"`
	LastMessageAt   *int64          `json:"last_message_at,omitempty"`
}

type Message struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	SessionID       string          `json:"session_id"`
	Role            string          `json:"role"`
	Content         json.RawMessage `json:"content"`
	RemoteMessageID *string         `json:"remote_message_id,omitempty"`
	DeliveryStatus  string          `json:"delivery_status"`
	CreatedAt       int64           `json:"created_at"`
}

type Turn struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	SessionID         string          `json:"session_id"`
	UserMessageID     string          `json:"user_message_id"`
	ResponseMessageID *string         `json:"response_message_id,omitempty"`
	Status            string          `json:"status"`
	IdempotencyKey    string          `json:"idempotency_key"`
	RemoteRunID       *string         `json:"remote_run_id,omitempty"`
	Receipt           json.RawMessage `json:"receipt,omitempty"`
	ErrorText         *string         `json:"error_text,omitempty"`
	CreatedAt         int64           `json:"created_at"`
	UpdatedAt         int64           `json:"updated_at"`
	CompletedAt       *int64          `json:"completed_at,omitempty"`
}

type CreateConnectionCommand struct {
	WorkspaceID   string
	PresetID      string
	DisplayName   string
	BaseURL       string
	CredentialRef *string
	Config        json.RawMessage
	CreatedBy     string
}

type CreateBotCommand struct {
	WorkspaceID  string
	ConnectionID string
	RemoteBotID  string
	DisplayName  string
	Description  string
	AvatarURL    *string
	LaunchURL    *string
	Capabilities json.RawMessage
	Metadata     json.RawMessage
	CreatedBy    string
}

type CreateSessionCommand struct {
	BotID     string
	Title     string
	Canonical bool
	CreatedBy string
}

type SendMessageCommand struct {
	SessionID      string
	Text           string
	IdempotencyKey string
	CreatedBy      string
}

type SendResult struct {
	Turn             Turn     `json:"turn"`
	UserMessage      Message  `json:"user_message"`
	AssistantMessage *Message `json:"assistant_message,omitempty"`
	LaunchURL        *string  `json:"launch_url,omitempty"`
}
