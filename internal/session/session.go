// Package session isolates per-account authentication state. Each account
// has its own cookie jar; nothing is shared between A and B.
package session

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"

	"gosumdochi/internal/config"
	"gosumdochi/internal/model"
)

var (
	ErrLoginFailed     = errors.New("login failed")
	ErrReloginSpent    = errors.New("relogin limit reached")
	ErrAccountUnusable = errors.New("account session unusable")
)

// MaxRelogins is the per-account limit for expiry-driven logins (SDD §8).
const MaxRelogins = 1

// ResolveRef reads a secret from env:NAME or file:PATH.
func ResolveRef(ref string) (string, error) {
	kind, name, _ := strings.Cut(ref, ":")
	switch kind {
	case "env":
		v := os.Getenv(name)
		if v == "" {
			return "", fmt.Errorf("env %s is empty", name)
		}
		return v, nil
	case "file":
		b, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return "", fmt.Errorf("unsupported secret ref")
}

// Session is one account's login state.
type Session struct {
	Account   model.AccountRef
	Username  string
	Password  string
	LoginPath string
	Jar       http.CookieJar
	mu        sync.Mutex
	loggedIn  bool
	relogins  int
	unusable  bool
}

// Manager holds the two sessions.
type Manager struct {
	sessions map[model.AccountRef]*Session
}

func NewManager(c config.Config, resolve func(string) (string, error)) (*Manager, error) {
	if resolve == nil {
		resolve = ResolveRef
	}
	m := &Manager{sessions: map[model.AccountRef]*Session{}}
	for ref, acct := range c.Accounts {
		user, err := resolve(acct.UsernameRef)
		if err != nil {
			return nil, fmt.Errorf("account %s username: %w", ref, err)
		}
		pass, err := resolve(acct.PasswordRef)
		if err != nil {
			return nil, fmt.Errorf("account %s password: %w", ref, err)
		}
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, err
		}
		m.sessions[ref] = &Session{Account: ref, Username: user, Password: pass, LoginPath: acct.LoginPath, Jar: jar}
	}
	return m, nil
}

func (m *Manager) Get(ref model.AccountRef) (*Session, bool) {
	s, ok := m.sessions[ref]
	return s, ok
}

// Secrets lists values that must be masked everywhere.
func (m *Manager) Secrets() []string {
	var out []string
	for _, s := range m.sessions {
		out = append(out, s.Username, s.Password)
	}
	return out
}

// CookieValues lists current cookie values for target, to register as secrets.
func (s *Session) CookieValues(target *url.URL) []string {
	var out []string
	for _, c := range s.Jar.Cookies(target) {
		out = append(out, c.Value)
	}
	return out
}

func (s *Session) LoggedIn() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.loggedIn }
func (s *Session) Unusable() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.unusable }
func (s *Session) Relogins() int  { s.mu.Lock(); defer s.mu.Unlock(); return s.relogins }

func (s *Session) MarkLoggedIn() { s.mu.Lock(); s.loggedIn = true; s.mu.Unlock() }

// MarkFailed makes every later request for this account fail without network
// traffic, so no comparison can be built on a broken session (AC-03).
func (s *Session) MarkFailed() { s.mu.Lock(); s.unusable = true; s.loggedIn = false; s.mu.Unlock() }

// BeginRelogin consumes the account's single relogin allowance.
func (s *Session) BeginRelogin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.relogins >= MaxRelogins {
		return ErrReloginSpent
	}
	s.relogins++
	s.loggedIn = false
	return nil
}
