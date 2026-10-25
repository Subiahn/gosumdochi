// Package policy enforces target scope and budgets. It is the only gate
// between a validated action and the network; the model cannot change it.
package policy

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"

	"gosumdochi/internal/config"
	"gosumdochi/internal/model"
)

// Denial codes.
const (
	CodeInvalidURL        = "invalid_url"
	CodeOutOfOrigin       = "out_of_origin"
	CodePathNotAllowed    = "path_not_allowed"
	CodeMethodNotAllowed  = "method_not_allowed"
	CodeHeaderNotAllowed  = "header_not_allowed"
	CodeAddressNotAllowed = "address_not_allowed"
	CodeBudgetExhausted   = "budget_exhausted"
)

// DeniedError is returned for every policy refusal. Nothing was sent.
type DeniedError struct {
	Code string
	Msg  string
}

func (e *DeniedError) Error() string { return fmt.Sprintf("denied: %s: %s", e.Code, e.Msg) }

func denied(code, format string, a ...any) error {
	return &DeniedError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// DeniedCode returns the denial code of err, or "" if err is not a denial.
func DeniedCode(err error) string {
	var d *DeniedError
	if errors.As(err, &d) {
		return d.Code
	}
	return ""
}

// allowedHeaders are the only headers a model-built request may carry.
// Auth headers and cookies come from the session, never from the model.
var allowedHeaders = map[string]bool{"Accept": true}

type Policy struct {
	scheme, host string // host includes the port when explicit
	prefixes     []string
	cidrs        []netip.Prefix
}

func New(c config.Config) (*Policy, error) {
	u, err := url.Parse(c.TargetOrigin)
	if err != nil {
		return nil, err
	}
	p := &Policy{scheme: u.Scheme, host: normalizeHost(u.Scheme, u.Host), prefixes: c.AllowedPathPrefixes}
	for _, s := range c.AllowedLabCIDRs {
		pf, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		p.cidrs = append(p.cidrs, pf.Masked())
	}
	return p, nil
}

func normalizeHost(scheme, host string) string {
	host = strings.ToLower(host)
	switch {
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		return strings.TrimSuffix(host, ":80")
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		return strings.TrimSuffix(host, ":443")
	}
	return host
}

// CheckAction validates the request an action would send.
func (p *Policy) CheckAction(a model.Action) error {
	if a.Tool != model.ToolHTTPRequest {
		return denied(CodeMethodNotAllowed, "unknown tool %q", a.Tool)
	}
	if a.TypedArgs.Method != "GET" {
		return denied(CodeMethodNotAllowed, "method %q", a.TypedArgs.Method)
	}
	for k := range a.TypedArgs.Headers {
		if !allowedHeaders[textprotoCanonical(k)] {
			return denied(CodeHeaderNotAllowed, "header %q", k)
		}
	}
	return p.CheckURL(a.TypedArgs.URL)
}

// CheckURL validates origin and path scope. It is also applied to the
// configured login path before the session POSTs to it.
func (p *Policy) CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return denied(CodeInvalidURL, "unparseable")
	}
	if u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return denied(CodeInvalidURL, "userinfo, fragment or opaque form")
	}
	if u.Scheme != p.scheme || normalizeHost(u.Scheme, u.Host) != p.host {
		return denied(CodeOutOfOrigin, "origin %s://%s", u.Scheme, u.Host)
	}
	if err := checkPath(u); err != nil {
		return err
	}
	for _, pre := range p.prefixes {
		if hasPathPrefix(u.Path, pre) {
			return nil
		}
	}
	return denied(CodePathNotAllowed, "path %q", u.Path)
}

func checkPath(u *url.URL) error {
	raw := strings.ToLower(u.EscapedPath())
	if strings.ContainsAny(u.Path, "\\\x00") || strings.Contains(raw, "%2e") ||
		strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") || strings.Contains(raw, "%00") {
		return denied(CodePathNotAllowed, "encoded or ambiguous path separators")
	}
	if u.Path == "" || u.Path[0] != '/' || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") && u.Path != "/" {
		return denied(CodePathNotAllowed, "non-canonical path %q", u.Path)
	}
	return nil
}

// hasPathPrefix: a prefix ending in "/" matches by string prefix; otherwise
// the path must equal it or continue at a segment boundary.
func hasPathPrefix(p, prefix string) bool {
	if strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(p, prefix)
	}
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// CheckIP is called by the dialer with every address a name resolved to.
func (p *Policy) CheckIP(ip netip.Addr) error {
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsMulticast() {
		return denied(CodeAddressNotAllowed, "address class not allowed")
	}
	for _, c := range p.cidrs {
		if c.Contains(ip) {
			return nil
		}
	}
	return denied(CodeAddressNotAllowed, "%s outside allowed lab CIDRs", ip)
}

func textprotoCanonical(k string) string {
	if k == "" {
		return k
	}
	parts := strings.Split(strings.ToLower(k), "-")
	for i, s := range parts {
		if s != "" {
			parts[i] = strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return strings.Join(parts, "-")
}

// Budget counts HTTP requests (logins and retries included). Safe for
// concurrent use even though the agent runs one tool at a time.
type Budget struct {
	mu   sync.Mutex
	max  int
	used int
}

func NewBudget(maxHTTPRequests int) *Budget { return &Budget{max: maxHTTPRequests} }

// Spend reserves one request, or denies when the budget is exhausted.
func (b *Budget) Spend() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.max {
		return denied(CodeBudgetExhausted, "http request budget %d used", b.max)
	}
	b.used++
	return nil
}

func (b *Budget) Used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
