package state

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"

	"gosumdochi/internal/model"
)

func TestFileStore(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir, "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	e := model.Evidence{ID: "E-0001", ActionID: "a1", AccountRef: model.AccountA, CapturedBodyHash: "h"}
	if err := s.PutEvidence(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.PutEvidence(ctx, e); err == nil {
		t.Error("evidence overwrite must fail")
	}
	got, err := s.GetEvidence(ctx, "E-0001")
	if err != nil || got.CapturedBodyHash != "h" {
		t.Fatalf("%v %+v", err, got)
	}
	for _, bad := range []string{"../state", "a/b", "", "E 1"} {
		if _, err := s.GetEvidence(ctx, bad); err == nil {
			t.Errorf("id %q accepted", bad)
		}
		if err := s.PutEvidence(ctx, model.Evidence{ID: bad}); err == nil {
			t.Errorf("put id %q accepted", bad)
		}
	}

	for _, k := range []string{"one", "two", "three"} {
		if err := s.AppendEvent(ctx, k, map[string]string{"k": k}); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := os.Open(filepath.Join(dir, "events.jsonl"))
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	if n != 3 {
		t.Errorf("events %d", n)
	}

	snap := Snapshot{Run: model.Run{ID: "r1", Status: model.RunRunning}}
	if err := s.SaveState(ctx, snap); err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadState(ctx)
	if err != nil || back.Run.ID != "r1" {
		t.Fatalf("%v %+v", err, back)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file left behind")
	}
}
