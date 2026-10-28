package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gosumdochi/internal/llm"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := New(s.URL, "m:1", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGenerateSendsContractAndParses(t *testing.T) {
	var got map[string]any
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"model":"m:1","message":{"role":"assistant","content":"{\"ok\":true}"},"done":true,"done_reason":"stop","prompt_eval_count":11,"eval_count":5}`))
	})
	resp, err := c.Generate(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleSystem, Content: "s"}, {Role: llm.RoleUser, Content: "u"}},
		Schema:   json.RawMessage(`{"type":"object"}`),
		Options:  map[string]any{"temperature": 0.3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"ok":true}` || resp.ModelID != "m:1" || resp.PromptTokens != 11 || resp.OutputTokens != 5 {
		t.Fatalf("%+v", resp)
	}
	if got["stream"] != false || got["think"] != false || got["model"] != "m:1" {
		t.Errorf("request %v", got)
	}
	if f, _ := got["format"].(map[string]any); f["type"] != "object" {
		t.Errorf("format %v", got["format"])
	}
	opts := got["options"].(map[string]any)
	if opts["temperature"] != 0.3 || opts["seed"] != float64(42) {
		t.Errorf("per-call options must override defaults and keep the seed: %v", opts)
	}
	if c.Options()["temperature"] != float64(0) && c.Options()["temperature"] != 0 {
		t.Error("client defaults mutated by a call")
	}
}

func TestGenerateFailures(t *testing.T) {
	cases := map[string]struct {
		h    http.HandlerFunc
		want error
		sub  string
	}{
		"truncated": {func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"message":{"content":"{\"a\":"},"done":true,"done_reason":"length"}`))
		}, ErrTruncated, ""},
		"empty": {func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"message":{"content":"  "},"done":true}`))
		}, ErrEmpty, ""},
		"http error": {func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"model 'm:1' not found"}`))
		}, nil, "not found"},
		"garbage": {func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("<html>")) }, nil, "undecodable"},
		"redirect": {func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://evil.example/", http.StatusFound)
		}, nil, "undecodable"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := serve(t, c.h).Generate(context.Background(), llm.Request{})
			if err == nil {
				t.Fatal("expected error")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("err %v", err)
			}
			if c.sub != "" && !strings.Contains(err.Error(), c.sub) {
				t.Errorf("err %v", err)
			}
		})
	}
}

func TestGenerateTimeoutAndCancel(t *testing.T) {
	slow := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	s := httptest.NewServer(http.HandlerFunc(slow))
	defer s.Close()
	c, _ := New(s.URL, "m", 300*time.Millisecond)
	start := time.Now()
	if _, err := c.Generate(context.Background(), llm.Request{}); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("timeout not enforced: %v after %v", err, time.Since(start))
	}
	c, _ = New(s.URL, "m", 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Generate(ctx, llm.Request{}); err == nil {
		t.Fatal("ctx deadline not honored")
	}
}

func TestResolveModel(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"name":"other:2","model":"other:2","digest":"aaa"},{"name":"m:1","model":"m:1","digest":"abc123"}]}`))
	})
	id, err := c.ResolveModel(context.Background())
	if err != nil || id != "m:1@abc123" {
		t.Fatalf("%q %v", id, err)
	}
	c.model = "absent"
	if _, err := c.ResolveModel(context.Background()); !errors.Is(err, ErrNoModel) {
		t.Fatalf("%v", err)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	for _, u := range []string{"", "ftp://x", "localhost:11434", "http://"} {
		if _, err := New(u, "m", time.Second); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if _, err := New("http://x", "", time.Second); err == nil {
		t.Error("empty model accepted")
	}
}

// Live smoke test against a real Ollama: OLLAMA_LIVE_MODEL=qwen3:8b go test -run Live
func TestLive(t *testing.T) {
	model := os.Getenv("OLLAMA_LIVE_MODEL")
	if model == "" {
		t.Skip("set OLLAMA_LIVE_MODEL to run against a real Ollama")
	}
	base := os.Getenv("OLLAMA_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	c, err := New(base, model, 120*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.ResolveModel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s", id)
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["decision","reason_code"],
		"properties":{"decision":{"enum":["execute","stop","inconclusive"]},"reason_code":{"enum":["new_observation_needed","insufficient_evidence"]}}}`)
	begin := time.Now()
	resp, err := c.Generate(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "Reply with JSON only."},
			{Role: llm.RoleUser, Content: "There is one candidate action. Choose execute with reason new_observation_needed."},
		},
		Schema: schema,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v: %s (prompt %d, output %d tokens)", time.Since(begin), resp.Content, resp.PromptTokens, resp.OutputTokens)
	var out struct{ Decision, ReasonCode string }
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if out.Decision == "" {
		t.Fatalf("schema not enforced: %s", resp.Content)
	}
}
