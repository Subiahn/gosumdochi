// Package extract turns a response body into the short, deterministic
// observation summary the model may see. The raw body never leaves evidence.
package extract

import (
	"encoding/json"
	"errors"
	"html"
	"mime"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultMaxSummary = 1024
	maxValueRunes     = 120
	maxListItems      = 20
)

var ErrUnsupported = errors.New("unsupported content type")

// jsonAllow is the whitelist of JSON fields that may appear in a summary.
// Free-text fields such as "body" are deliberately absent.
var jsonAllow = map[string]bool{
	"id": true, "title": true, "url": true, "user_id": true, "display_name": true,
	"owner_id": true, "owner_name": true, "error": true, "ok": true, "documents": true,
}

type Result struct {
	Summary string
	Refs    []string // absolute URLs exposed by the response, sorted, unique
}

// Extract summarizes body. base is the request URL, used to resolve
// relative links. A non-nil error means the observation is inconclusive-only.
func Extract(contentType string, body []byte, base *url.URL, maxSummary int) (Result, error) {
	if maxSummary <= 0 {
		maxSummary = DefaultMaxSummary
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Result{}, ErrUnsupported
	}
	var r Result
	switch {
	case mt == "application/json" || strings.HasSuffix(mt, "+json"):
		r, err = fromJSON(body, base)
	case mt == "text/html":
		r = fromHTML(body, base)
	case mt == "text/plain":
		r = Result{Summary: clean(string(body))}
	default:
		return Result{}, ErrUnsupported
	}
	if err != nil {
		return Result{}, err
	}
	r.Summary = capBytes(r.Summary, maxSummary)
	return r, nil
}

func fromJSON(body []byte, base *url.URL) (Result, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return Result{}, err
	}
	refs := map[string]bool{}
	out := filter(v, base, refs)
	b, _ := json.Marshal(out) // map keys are sorted: output is deterministic
	return Result{Summary: string(b), Refs: sortedKeys(refs)}, nil
}

func filter(v any, base *url.URL, refs map[string]bool) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if !jsonAllow[k] {
				continue
			}
			if s, ok := val.(string); ok && k == "url" {
				if abs := resolve(base, s); abs != "" {
					refs[abs] = true
				}
			}
			out[k] = filter(val, base, refs)
		}
		return out
	case []any:
		if len(x) > maxListItems {
			x = x[:maxListItems]
		}
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, filter(e, base, refs))
		}
		return out
	case string:
		return clean(x)
	case bool, float64, nil:
		return x
	}
	return nil
}

var (
	reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reLink  = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*["']([^"']*)["']`)
	reForm  = regexp.MustCompile(`(?is)<form\s[^>]*>`)
	reAttr  = regexp.MustCompile(`(?is)\b(action|method)\s*=\s*["']([^"']*)["']`)
)

func fromHTML(body []byte, base *url.URL) Result {
	s := string(body)
	refs := map[string]bool{}
	var parts []string
	if m := reTitle.FindStringSubmatch(s); m != nil {
		parts = append(parts, "title: "+clean(html.UnescapeString(m[1])))
	}
	for _, m := range reLink.FindAllStringSubmatch(s, maxListItems) {
		if abs := resolve(base, html.UnescapeString(m[1])); abs != "" {
			refs[abs] = true
		}
	}
	for _, f := range reForm.FindAllString(s, 5) {
		attrs := map[string]string{}
		for _, m := range reAttr.FindAllStringSubmatch(f, -1) {
			attrs[strings.ToLower(m[1])] = clean(m[2])
		}
		parts = append(parts, "form: "+strings.ToUpper(attrs["method"])+" "+attrs["action"])
	}
	for _, l := range sortedKeys(refs) {
		parts = append(parts, "link: "+l)
	}
	return Result{Summary: strings.Join(parts, "\n"), Refs: sortedKeys(refs)}
}

// resolve returns an absolute http(s) URL, or "" for anything else.
func resolve(base *url.URL, raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(u)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}

// clean drops control characters and bounds one value's length.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxValueRunes {
		s = string(r[:maxValueRunes]) + "…"
	}
	return s
}

// capBytes cuts to at most n bytes on a rune boundary.
func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
