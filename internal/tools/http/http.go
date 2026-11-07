// Package http is the http_request tool. All scope, address and budget checks
// happen here or in policy; the model never reaches the network directly.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	nethttp "net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"gosumdochi/internal/config"
	"gosumdochi/internal/evidence"
	"gosumdochi/internal/model"
	"gosumdochi/internal/policy"
	"gosumdochi/internal/session"
	"gosumdochi/internal/tools"
)

// Resolver is satisfied by *net.Resolver; tests inject fakes.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type Tool struct {
	cfg      config.Config
	policy   *policy.Policy
	budget   *policy.Budget
	sessions *session.Manager
	masker   *evidence.Masker
	client   map[model.AccountRef]*nethttp.Client
	anon     *nethttp.Client // no jar, no login: unauth-access-v1 only
	target   *url.URL
	maxBody  int
}

type Options struct {
	Resolver Resolver // default: net.DefaultResolver
}

func New(cfg config.Config, p *policy.Policy, b *policy.Budget, sm *session.Manager, m *evidence.Masker, opt Options) (*Tool, error) {
	target, err := url.Parse(cfg.TargetOrigin)
	if err != nil {
		return nil, err
	}
	res := opt.Resolver
	if res == nil {
		res = net.DefaultResolver
	}
	timeout := time.Duration(cfg.Budgets.HTTPTimeoutSec) * time.Second
	// One transport for both accounts: it holds no cookies. Per-account state
	// lives only in each client's jar.
	tr := &nethttp.Transport{
		Proxy:                  nil, // never honor HTTP(S)_PROXY
		DialContext:            safeDial(p, res, timeout),
		DisableKeepAlives:      true, // every request re-resolves and re-checks
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 << 10,
	}
	t := &Tool{cfg: cfg, policy: p, budget: b, sessions: sm, masker: m, target: target,
		client: map[model.AccountRef]*nethttp.Client{}, maxBody: cfg.Budgets.MaxBodyBytes}
	for _, ref := range []model.AccountRef{model.AccountA, model.AccountB} {
		s, ok := sm.Get(ref)
		if !ok {
			return nil, errors.New("missing session for " + string(ref))
		}
		t.client[ref] = &nethttp.Client{
			Transport: tr, Jar: s.Jar, Timeout: timeout,
			// Redirects are returned to the caller as plain 3xx responses.
			CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error { return nethttp.ErrUseLastResponse },
		}
	}
	t.anon = &nethttp.Client{
		Transport: tr, Jar: nil, Timeout: timeout, // no cookies, ever
		CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error { return nethttp.ErrUseLastResponse },
	}
	return t, nil
}

func (t *Tool) Name() string { return model.ToolHTTPRequest }

// safeDial resolves, checks every address against the lab CIDRs, and dials
// the checked address itself, so the name is never resolved a second time.
func safeDial(p *policy.Policy, res Resolver, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	d := &net.Dialer{Timeout: timeout}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var ips []netip.Addr
		if ip, err := netip.ParseAddr(host); err == nil {
			ips = []netip.Addr{ip}
		} else if ips, err = res.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no addresses")
		}
		for _, ip := range ips { // all, not only the first: one bad answer denies the name
			if err := p.CheckIP(ip); err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, ip := range ips {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return c, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// Execute runs an approved GET for the action's account. A login happens
// lazily on first use; one expiry-driven relogin is allowed per account.
func (t *Tool) Execute(ctx context.Context, a model.Action) (tools.Result, error) {
	start := time.Now()
	res := t.execute(ctx, a)
	res.DurationMS = time.Since(start).Milliseconds()
	return res, nil
}

func (t *Tool) execute(ctx context.Context, a model.Action) tools.Result {
	ref := a.TypedArgs.AccountRef
	if ref == model.AccountNone {
		res, code := t.getAnonymous(ctx, a.TypedArgs.URL)
		if code != "" {
			return failed(code)
		}
		return res
	}
	s, ok := t.sessions.Get(ref)
	if !ok {
		return failed(tools.ErrLoginFailed)
	}
	if s.Unusable() {
		return failed(tools.ErrLoginFailed)
	}
	if !s.LoggedIn() {
		if code := t.login(ctx, s); code != "" {
			return failed(code)
		}
	}
	res, code := t.get(ctx, t.client[s.Account], a)
	if code != "" {
		return failed(code)
	}
	if res.HTTPStatus != nethttp.StatusUnauthorized {
		return res
	}
	// Session expired: relogin once, then retry once. Both count as requests.
	if err := s.BeginRelogin(); err != nil {
		s.MarkFailed()
		return failed(tools.ErrLoginFailed)
	}
	if code := t.login(ctx, s); code != "" {
		return failed(code)
	}
	res, code = t.get(ctx, t.client[s.Account], a)
	if code != "" {
		return failed(code)
	}
	return res
}

// getAnonymous is unauth-access-v1's only entry point: a GET with no cookies
// and no login attempt, over the same policy-protected transport.
func (t *Tool) getAnonymous(ctx context.Context, rawURL string) (tools.Result, string) {
	return t.get(ctx, t.anon, model.Action{TypedArgs: model.HTTPArgs{Method: "GET", URL: rawURL, AccountRef: model.AccountNone}})
}

func failed(code string) tools.Result {
	st := model.ResultFailed
	switch code {
	case tools.ErrDeniedAddress, tools.ErrBudgetExhausted:
		st = model.ResultDenied
	case tools.ErrCanceled:
		st = model.ResultCanceled
	}
	return tools.Result{Status: st, ErrorCode: code, ContentLength: -1}
}

// login POSTs the configured credentials. Returns an error code or "".
func (t *Tool) login(ctx context.Context, s *session.Session) string {
	loginURL := t.target.ResolveReference(&url.URL{Path: s.LoginPath}).String()
	if err := t.policy.CheckURL(loginURL); err != nil {
		s.MarkFailed()
		return tools.ErrLoginFailed
	}
	body, _ := json.Marshal(map[string]string{"username": s.Username, "password": s.Password})
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, loginURL, bytes.NewReader(body))
	if err != nil {
		s.MarkFailed()
		return tools.ErrLoginFailed
	}
	req.Header.Set("Content-Type", "application/json")
	resp, code := t.send(t.client[s.Account], req)
	if code != "" {
		switch code {
		case tools.ErrCanceled, tools.ErrBudgetExhausted, tools.ErrDeniedAddress:
			return code // not the account's fault; the session stays usable
		}
		s.MarkFailed()
		return tools.ErrLoginFailed
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		s.MarkFailed()
		return tools.ErrLoginFailed
	}
	t.masker.Add(s.CookieValues(t.target)...)
	s.MarkLoggedIn()
	return ""
}

func (t *Tool) get(ctx context.Context, c *nethttp.Client, a model.Action) (tools.Result, string) {
	reqURL := a.TypedArgs.URL
	if a.TypedArgs.MarkerID != "" {
		// Policy has already confirmed marker_id is known and param_name is
		// present (CheckAction); resolving it here, not earlier, keeps the
		// literal payload out of every step before the wire request.
		val, ok := policy.MarkerValue(a.TypedArgs.MarkerID)
		if !ok {
			return tools.Result{}, tools.ErrNetwork
		}
		u, err := url.Parse(reqURL)
		if err != nil {
			return tools.Result{}, tools.ErrNetwork
		}
		q := u.Query()
		q.Set(a.TypedArgs.ParamName, val)
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, reqURL, nil)
	if err != nil {
		return tools.Result{}, tools.ErrNetwork
	}
	for k, v := range a.TypedArgs.Headers { // policy allows Accept only
		req.Header.Set(k, v)
	}
	// Reduce cache influence on comparison requests (SDD §7).
	req.Header.Set("Cache-Control", "no-cache")
	resp, code := t.send(c, req)
	if code != "" {
		return tools.Result{}, code
	}
	defer resp.Body.Close()
	// Read one byte past the limit to learn whether the body was cut.
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(t.maxBody)+1))
	if err != nil {
		return tools.Result{}, classify(ctx, err)
	}
	truncated := len(b) > t.maxBody
	if truncated {
		b = b[:t.maxBody]
	}
	return tools.Result{
		Status: model.ResultCompleted, HTTPStatus: resp.StatusCode, Headers: resp.Header,
		Body: string(b), Truncated: truncated, ContentLength: contentLength(resp),
	}, ""
}

func contentLength(resp *nethttp.Response) int64 {
	if resp.ContentLength >= 0 {
		return resp.ContentLength
	}
	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
		return n
	}
	return -1
}

// send spends budget immediately before the request leaves.
func (t *Tool) send(c *nethttp.Client, req *nethttp.Request) (*nethttp.Response, string) {
	if err := req.Context().Err(); err != nil {
		return nil, tools.ErrCanceled
	}
	if err := t.budget.Spend(); err != nil {
		return nil, tools.ErrBudgetExhausted
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, classify(req.Context(), err)
	}
	return resp, ""
}

func classify(ctx context.Context, err error) string {
	var d *policy.DeniedError
	var ne net.Error
	switch {
	case errors.As(err, &d):
		return tools.ErrDeniedAddress
	case errors.Is(ctx.Err(), context.Canceled):
		return tools.ErrCanceled
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return tools.ErrTimeout
	}
	return tools.ErrNetwork
}

var _ tools.Tool = (*Tool)(nil)
