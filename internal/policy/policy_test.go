package policy

import (
	"net/netip"
	"testing"

	"gosumdochi/internal/config"
	"gosumdochi/internal/model"
)

func testPolicy(t *testing.T) *Policy {
	t.Helper()
	p, err := New(config.Config{
		TargetOrigin:        "http://labapp:8080",
		AllowedPathPrefixes: []string{"/api/", "/login", "/me"},
		AllowedLabCIDRs:     []string{"172.28.0.0/16"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckURL(t *testing.T) {
	p := testPolicy(t)
	cases := []struct {
		url  string
		code string // "" = allowed
	}{
		{"http://labapp:8080/api/docs/d-1", ""},
		{"http://LABAPP:8080/api/me", ""},
		{"http://labapp:8080/login", ""},
		{"http://labapp:8080/me/x", ""},
		{"http://evil.example/api/docs", CodeOutOfOrigin},
		{"http://labapp:9999/api/docs", CodeOutOfOrigin},
		{"https://labapp:8080/api/docs", CodeOutOfOrigin},
		{"http://host.docker.internal:11434/api/chat", CodeOutOfOrigin},
		{"http://labapp:8080@evil.example/api/", CodeInvalidURL},
		{"http://user:pw@labapp:8080/api/me", CodeInvalidURL},
		{"http://labapp:8080/api/me#x", CodeInvalidURL},
		{"http://labapp:8080/admin", CodePathNotAllowed},
		{"http://labapp:8080/loginx", CodePathNotAllowed},
		{"http://labapp:8080/media", CodePathNotAllowed},
		{"http://labapp:8080/api/../admin", CodePathNotAllowed},
		{"http://labapp:8080/api/%2e%2e/admin", CodePathNotAllowed},
		{"http://labapp:8080/api%2fdocs", CodePathNotAllowed},
		{"http://labapp:8080/api/a//b", CodePathNotAllowed},
	}
	for _, c := range cases {
		err := p.CheckURL(c.url)
		if got := DeniedCode(err); got != c.code {
			t.Errorf("%s: code %q (err %v), want %q", c.url, got, err, c.code)
		}
	}
}

func TestCheckAction(t *testing.T) {
	p := testPolicy(t)
	a := model.Action{Tool: model.ToolHTTPRequest, TypedArgs: model.HTTPArgs{Method: "GET", URL: "http://labapp:8080/api/me"}}
	if err := p.CheckAction(a); err != nil {
		t.Fatal(err)
	}
	a.TypedArgs.Headers = map[string]string{"accept": "application/json"}
	if err := p.CheckAction(a); err != nil {
		t.Fatalf("Accept should be allowed: %v", err)
	}
	for _, h := range []string{"Cookie", "Authorization", "Host", "X-Forwarded-For"} {
		a.TypedArgs.Headers = map[string]string{h: "x"}
		if DeniedCode(p.CheckAction(a)) != CodeHeaderNotAllowed {
			t.Errorf("header %s not denied", h)
		}
	}
	a.TypedArgs.Headers = nil
	a.TypedArgs.Method = "POST"
	if DeniedCode(p.CheckAction(a)) != CodeMethodNotAllowed {
		t.Error("POST not denied")
	}
}

func TestCheckIP(t *testing.T) {
	p := testPolicy(t)
	for ip, ok := range map[string]bool{
		"172.28.0.5": true, "::ffff:172.28.0.5": true,
		"8.8.8.8": false, "127.0.0.1": false, "169.254.169.254": false,
		"0.0.0.0": false, "224.0.0.1": false, "172.29.0.1": false,
	} {
		err := p.CheckIP(netip.MustParseAddr(ip))
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, want allowed=%v", ip, err, ok)
		}
	}
}

func TestBudget(t *testing.T) {
	b := NewBudget(2)
	for i := 0; i < 2; i++ {
		if err := b.Spend(); err != nil {
			t.Fatal(err)
		}
	}
	if DeniedCode(b.Spend()) != CodeBudgetExhausted || b.Used() != 2 {
		t.Fatalf("third spend must be denied without counting; used=%d", b.Used())
	}
}
