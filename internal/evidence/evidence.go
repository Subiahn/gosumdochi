// Package evidence masks secrets and builds hashed evidence records.
// Masking is best effort: known secrets and common sensitive fields only.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gosumdochi/internal/model"
)

const Redacted = "[REDACTED]"

var (
	jsonSecretField = regexp.MustCompile(`(?i)("(?:password|passwd|token|secret|session|api_key)"\s*:\s*)"[^"]*"`)
	cookieLike      = regexp.MustCompile(`(?i)\b(lab_session|session|token|sid)=([^;\s&"]+)`)
)

// sensitiveHeaders never reach storage or the model, whatever their value.
var sensitiveHeaders = map[string]bool{"authorization": true, "cookie": true, "set-cookie": true, "proxy-authorization": true}

// Masker replaces known secrets. Session values are registered as they are
// learned, so later responses that echo them are masked too.
type Masker struct {
	mu      sync.Mutex
	secrets []string
}

func NewMasker(known ...string) *Masker {
	m := &Masker{}
	m.Add(known...)
	return m
}

// Add registers secret values. Empty strings are ignored.
func (m *Masker) Add(vals ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range vals {
		if v != "" {
			m.secrets = append(m.secrets, v)
		}
	}
	// Longest first so a secret that contains another is replaced whole.
	sort.Slice(m.secrets, func(i, j int) bool { return len(m.secrets[i]) > len(m.secrets[j]) })
}

func (m *Masker) Mask(s string) string {
	m.mu.Lock()
	secrets := append([]string(nil), m.secrets...)
	m.mu.Unlock()
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec, Redacted)
	}
	s = jsonSecretField.ReplaceAllString(s, `${1}"`+Redacted+`"`)
	return cookieLike.ReplaceAllString(s, "${1}="+Redacted)
}

// Headers renders headers sorted, dropping the values of sensitive ones.
func (m *Masker) Headers(h map[string][]string) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		val := Redacted
		if !sensitiveHeaders[strings.ToLower(k)] {
			val = m.Mask(strings.Join(h[k], ", "))
		}
		fmt.Fprintf(&b, "%s: %s\n", k, val)
	}
	return b.String()
}

// Input is the raw material for one piece of evidence.
type Input struct {
	ID, ActionID  string
	Account       model.AccountRef
	Request       string // already free of auth material
	Status        int
	Headers       map[string][]string
	Body          string
	Truncated     bool
	ContentLength int64 // -1 if unknown
	RunID         string
	Now           time.Time
}

// Build masks and hashes. The hash covers only the stored (masked) body.
func Build(m *Masker, in Input) model.Evidence {
	body := m.Mask(in.Body)
	sum := sha256.Sum256([]byte(body))
	e := model.Evidence{
		Meta:              model.Meta{SchemaVersion: model.SchemaVersion, RunID: in.RunID, Timestamp: in.Now.UTC()},
		ID:                in.ID,
		ActionID:          in.ActionID,
		AccountRef:        in.Account,
		SanitizedRequest:  m.Mask(in.Request),
		SanitizedResponse: fmt.Sprintf("HTTP %d\n%s\n%s", in.Status, m.Headers(in.Headers), body),
		CapturedBodyHash:  hex.EncodeToString(sum[:]),
		Truncated:         in.Truncated,
	}
	if in.ContentLength >= 0 {
		n := in.ContentLength
		e.ContentLength = &n
	}
	return e
}
