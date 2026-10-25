package extract

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

var base, _ = url.Parse("http://labapp:8080/api/docs")

func TestJSONWhitelistAndRefs(t *testing.T) {
	body := `{"documents":[{"id":"d-1","title":"A note","url":"/api/docs/d-1","body":"LAB-MARK-secret","token":"abc"}],"password":"pw"}`
	r, err := Extract("application/json; charset=utf-8", []byte(body), base, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"LAB-MARK", "abc", "pw", "body", "token"} {
		if strings.Contains(r.Summary, bad) {
			t.Errorf("summary leaks %q: %s", bad, r.Summary)
		}
	}
	want := `{"documents":[{"id":"d-1","title":"A note","url":"/api/docs/d-1"}]}`
	if r.Summary != want {
		t.Errorf("summary %s", r.Summary)
	}
	if len(r.Refs) != 1 || r.Refs[0] != "http://labapp:8080/api/docs/d-1" {
		t.Errorf("refs %v", r.Refs)
	}
}

func TestJSONDeterministic(t *testing.T) {
	body := []byte(`{"title":"t","id":"i","owner_name":"o","user_id":"u"}`)
	first, _ := Extract("application/json", body, base, 0)
	for i := 0; i < 20; i++ {
		r, _ := Extract("application/json", body, base, 0)
		if r.Summary != first.Summary {
			t.Fatal("non-deterministic summary")
		}
	}
}

func TestSummaryCap(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"documents":[`)
	for i := 0; i < 20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"d-` + strings.Repeat("가", 40) + `","title":"` + strings.Repeat("나", 100) + `"}`)
	}
	sb.WriteString(`]}`)
	r, err := Extract("application/json", []byte(sb.String()), base, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Summary) > DefaultMaxSummary || !utf8.ValidString(r.Summary) {
		t.Fatalf("len=%d valid=%v", len(r.Summary), utf8.ValidString(r.Summary))
	}
}

func TestHTML(t *testing.T) {
	body := `<!doctype html><title>Lab &amp; Notes</title><script>alert(1)</script><a href="/api/docs">docs</a>
<a href="mailto:x@y">m</a><form action="/login" method="post"><input name=p></form>`
	r, err := Extract("text/html; charset=utf-8", []byte(body), base, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"title: Lab & Notes", "form: POST /login", "link: http://labapp:8080/api/docs"} {
		if !strings.Contains(r.Summary, want) {
			t.Errorf("missing %q in %q", want, r.Summary)
		}
	}
	if strings.Contains(r.Summary, "alert") || strings.Contains(r.Summary, "mailto") {
		t.Errorf("summary %q", r.Summary)
	}
}

func TestFailures(t *testing.T) {
	if _, err := Extract("application/json", []byte(`{"id":`), base, 0); err == nil {
		t.Error("truncated JSON must fail")
	}
	if _, err := Extract("application/octet-stream", []byte("x"), base, 0); err != ErrUnsupported {
		t.Errorf("err %v", err)
	}
	if _, err := Extract("", []byte("x"), base, 0); err != ErrUnsupported {
		t.Errorf("err %v", err)
	}
}
