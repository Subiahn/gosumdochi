// Package config loads and validates run configuration. Secrets are only
// ever referenced here (env:NAME or file:PATH), never embedded.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"gosumdochi/internal/model"
)

type Account struct {
	UsernameRef  string `json:"username_ref"`
	PasswordRef  string `json:"password_ref"`
	LoginPath    string `json:"login_path"`
	IdentityPath string `json:"identity_path"`
}

type LLM struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
}

type Config struct {
	TargetOrigin        string                       `json:"target_origin"`
	AllowedPathPrefixes []string                     `json:"allowed_path_prefixes"`
	AllowedLabCIDRs     []string                     `json:"allowed_lab_cidrs"`
	Accounts            map[model.AccountRef]Account `json:"accounts"`
	LLM                 LLM                          `json:"llm"`
	Budgets             *model.Budgets               `json:"budgets,omitempty"`
	OutputDir           string                       `json:"output_dir"`
}

// Parse decodes strictly, applies defaults and validates.
func Parse(data []byte) (Config, error) {
	c, err := model.DecodeStrict[Config](data)
	if err != nil {
		return c, err
	}
	if c.Budgets == nil {
		b := model.DefaultBudgets()
		c.Budgets = &b
	}
	if c.OutputDir == "" {
		c.OutputDir = "runs"
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	u, err := url.Parse(c.TargetOrigin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("target_origin: must be http(s)://host[:port]")
	}
	if u.User != nil {
		return fmt.Errorf("target_origin: userinfo not allowed")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("target_origin: must not contain path, query or fragment")
	}
	if len(c.AllowedPathPrefixes) == 0 {
		return fmt.Errorf("allowed_path_prefixes: at least one required")
	}
	for _, p := range c.AllowedPathPrefixes {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("allowed_path_prefixes: %q must start with /", p)
		}
	}
	if len(c.AllowedLabCIDRs) == 0 {
		return fmt.Errorf("allowed_lab_cidrs: at least one required")
	}
	for _, cidr := range c.AllowedLabCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("allowed_lab_cidrs: %q: %w", cidr, err)
		}
	}
	if len(c.Accounts) != 2 {
		return fmt.Errorf("accounts: exactly A and B required")
	}
	refs := map[string]model.AccountRef{}
	for _, name := range []model.AccountRef{model.AccountA, model.AccountB} {
		a, ok := c.Accounts[name]
		if !ok {
			return fmt.Errorf("accounts: %s missing", name)
		}
		for field, ref := range map[string]string{"username_ref": a.UsernameRef, "password_ref": a.PasswordRef} {
			if err := checkSecretRef(ref); err != nil {
				return fmt.Errorf("accounts.%s.%s: %w", name, field, err)
			}
		}
		if !strings.HasPrefix(a.LoginPath, "/") || !strings.HasPrefix(a.IdentityPath, "/") {
			return fmt.Errorf("accounts.%s: login_path and identity_path must start with /", name)
		}
		if other, dup := refs[a.UsernameRef]; dup {
			return fmt.Errorf("accounts: %s and %s share username_ref", other, name)
		}
		refs[a.UsernameRef] = name
	}
	lu, err := url.Parse(c.LLM.BaseURL)
	if c.LLM.Provider == "" || c.LLM.Model == "" || err != nil || lu.Hostname() == "" {
		return fmt.Errorf("llm: provider, model and base_url required")
	}
	if lu.Host == u.Host {
		return fmt.Errorf("llm.base_url must not be the target origin")
	}
	b := c.Budgets
	if b == nil {
		return fmt.Errorf("budgets: not set")
	}
	for name, v := range map[string]int{
		"max_cycles": b.MaxCycles, "max_http_requests": b.MaxHTTPRequests, "max_model_calls": b.MaxModelCalls,
		"max_candidates": b.MaxCandidates, "http_timeout_sec": b.HTTPTimeoutSec, "llm_timeout_sec": b.LLMTimeoutSec,
		"total_timeout_sec": b.TotalTimeoutSec, "max_body_bytes": b.MaxBodyBytes,
	} {
		if v <= 0 {
			return fmt.Errorf("budgets.%s: must be > 0", name)
		}
	}
	return nil
}

func checkSecretRef(ref string) error {
	kind, name, ok := strings.Cut(ref, ":")
	if !ok || name == "" || (kind != "env" && kind != "file") {
		return fmt.Errorf("must be env:NAME or file:PATH, not a literal secret")
	}
	return nil
}

// Digest hashes the non-secret configuration (secrets are refs only) for run.json.
func (c Config) Digest() string {
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
