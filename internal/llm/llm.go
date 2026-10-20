// Package llm defines the provider-neutral inference contract. Adapters
// (e.g. Ollama) implement Client; callers never learn provider details.
package llm

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem Role = "system"
	RoleUser   Role = "user"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Messages []Message
	// Schema constrains the output when the provider supports structured output.
	// Callers must still validate the result.
	Schema  json.RawMessage
	Options map[string]any
}

type Response struct {
	Content      string
	ModelID      string // resolved model identifier, recorded in run metadata
	PromptTokens int
	OutputTokens int
}

type Client interface {
	Generate(ctx context.Context, req Request) (Response, error)
}
