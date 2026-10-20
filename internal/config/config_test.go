package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "config", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse(t *testing.T) {
	c, err := Parse(read(t, "valid.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Budgets.MaxCycles != 15 || c.OutputDir != "runs" {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if len(c.Digest()) != 64 {
		t.Fatal("bad digest")
	}
}

func TestParseRejects(t *testing.T) {
	for file, want := range map[string]string{
		"literal_secret.json": "literal secret",
		"same_account.json":   "share username_ref",
		"llm_is_target.json":  "target origin",
		"bad_origin.json":     "userinfo",
	} {
		_, err := Parse(read(t, file))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want contains %q", file, err, want)
		}
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "configs", "lab.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(b); err != nil {
		t.Fatal(err)
	}
}
