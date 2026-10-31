// M5: report generation and regeneration (AC-12), with fake Planner/Judge so
// this runs deterministically with no network and no real model.
package integration

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gosumdochi/internal/judge"
	"gosumdochi/internal/labapp"
	"gosumdochi/internal/planner"
	"gosumdochi/internal/report"
	"gosumdochi/internal/state"
)

// AC-01 + AC-12: a confirmed run's report is traceable and regenerates
// identically from the stored state alone, with no agent, no store writer,
// no network involved.
func TestReportRegenerationMatchesStoredRun(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	live := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if len(live.Findings) == 0 {
		t.Fatal("setup: expected a confirmed finding to report on")
	}

	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	snap1, err := state.ReadSnapshot(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	r1 := report.Build(snap1, now)
	if err := report.WriteFiles(e.dir, r1); err != nil {
		t.Fatal(err)
	}

	// Regenerate again from disk only, as `gosumdochi report -run <dir>`
	// would on a second invocation. No Store, no Policy, no Tool involved.
	snap2, err := state.ReadSnapshot(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	r2 := report.Build(snap2, now)
	j1, _ := json.Marshal(r1)
	j2, _ := json.Marshal(r2)
	if string(j1) != string(j2) {
		t.Fatal("regenerated report differs from the first")
	}

	for _, name := range []string{"report.json", "report.md"} {
		b, err := os.ReadFile(e.dir + "/" + name)
		if err != nil || len(b) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
	md, _ := os.ReadFile(e.dir + "/report.md")
	f := live.Findings[0]
	for _, id := range append([]string{f.ID, f.VerificationID}, f.EvidenceIDs...) {
		if !strings.Contains(string(md), id) {
			t.Errorf("report.md does not trace to %s", id)
		}
		if _, err := e.store.GetEvidence(context.Background(), id); err != nil && strings.HasPrefix(id, "E-") {
			t.Errorf("referenced evidence %s missing on disk: %v", id, err)
		}
	}
}

// AC-02 + AC-09: a fixed-mode run's report must carry no finding and say
// why the hypothesis did not confirm.
func TestReportOnFixedModeHasNoFinding(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeFixed})
	e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	snap, err := state.ReadSnapshot(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	r := report.Build(snap, time.Now())
	if len(r.Findings) != 0 {
		t.Fatalf("fixed mode must not produce a finding: %+v", r.Findings)
	}
	if len(r.Limitations) == 0 {
		t.Fatal("expected limitations explaining why")
	}
}
