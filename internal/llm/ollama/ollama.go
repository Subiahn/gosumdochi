// Package ollama is the Ollama adapter for llm.Client. It talks only to the
// configured inference server and shares no client or transport with the
// target HTTP tool.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"strings"
	"time"

	"gosumdochi/internal/llm"
)

const maxResponseBytes = 1 << 20

var (
	ErrTruncated = errors.New("ollama: output cut at the token limit")
	ErrEmpty     = errors.New("ollama: empty output")
	ErrNoModel   = errors.New("ollama: model not installed")
)

// DefaultOptions make runs repeatable: greedy-ish sampling and a fixed seed.
// They are recorded in run metadata by the caller.
func DefaultOptions() map[string]any {
	return map[string]any{"temperature": 0, "seed": 42, "num_ctx": 8192, "num_predict": 1024}
}

type Client struct {
	baseURL string
	model   string
	http    *nethttp.Client
	options map[string]any
}

// New returns a client for baseURL (e.g. http://localhost:11434). timeout
// bounds one call; ctx cancellation also applies.
func New(baseURL, model string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("ollama: invalid base_url")
	}
	if model == "" {
		return nil, fmt.Errorf("ollama: model required")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), model: model, options: DefaultOptions(),
		http: &nethttp.Client{
			Timeout: timeout,
			// No proxy, no redirects: this client reaches exactly one server.
			Transport:     &nethttp.Transport{Proxy: nil, DisableKeepAlives: true},
			CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error { return nethttp.ErrUseLastResponse },
		},
	}, nil
}

func (c *Client) Options() map[string]any {
	out := make(map[string]any, len(c.options))
	for k, v := range c.options {
		out[k] = v
	}
	return out
}

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []llm.Message   `json:"messages"`
	Stream   bool            `json:"stream"`
	Think    bool            `json:"think"`
	Format   json.RawMessage `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

// Generate performs one non-streaming chat call. A schema, when given, is
// passed as Ollama's structured-output format; callers must still validate.
func (c *Client) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	opts := c.Options()
	for k, v := range req.Options {
		opts[k] = v
	}
	body, err := json.Marshal(chatRequest{
		Model: c.model, Messages: req.Messages, Stream: false, Think: false, Format: req.Schema, Options: opts,
	})
	if err != nil {
		return llm.Response{}, err
	}
	raw, status, err := c.do(ctx, nethttp.MethodPost, "/api/chat", body)
	if err != nil {
		return llm.Response{}, err
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return llm.Response{}, fmt.Errorf("ollama: undecodable response (HTTP %d)", status)
	}
	if status != nethttp.StatusOK || cr.Error != "" {
		return llm.Response{}, fmt.Errorf("ollama: HTTP %d: %s", status, truncate(cr.Error, 200))
	}
	if cr.DoneReason == "length" {
		return llm.Response{}, ErrTruncated
	}
	if strings.TrimSpace(cr.Message.Content) == "" {
		return llm.Response{}, ErrEmpty
	}
	return llm.Response{
		Content: cr.Message.Content, ModelID: cr.Model,
		PromptTokens: cr.PromptEvalCount, OutputTokens: cr.EvalCount,
	}, nil
}

// ResolveModel returns "name@digest" for the installed model, to record the
// exact model in run metadata (FR-12). It fails if the model is not installed.
func (c *Client) ResolveModel(ctx context.Context) (string, error) {
	raw, status, err := c.do(ctx, nethttp.MethodGet, "/api/tags", nil)
	if err != nil {
		return "", err
	}
	if status != nethttp.StatusOK {
		return "", fmt.Errorf("ollama: /api/tags HTTP %d", status)
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &tags); err != nil {
		return "", fmt.Errorf("ollama: undecodable /api/tags")
	}
	want := c.model
	if !strings.Contains(want, ":") {
		want += ":latest"
	}
	for _, m := range tags.Models {
		if m.Name == want || m.Model == want {
			return m.Name + "@" + m.Digest, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNoModel, c.model)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	req, err := nethttp.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("ollama: read: %w", err)
	}
	return raw, resp.StatusCode, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var _ llm.Client = (*Client)(nil)
