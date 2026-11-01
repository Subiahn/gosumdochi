// M6a: the three new independent Verifier rules (SDD §14.1), driven by the
// real agent loop with fake Planner/Judge, no model.
package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gosumdochi/internal/agent"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/labapp"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
	"gosumdochi/internal/report"
	"gosumdochi/internal/state"
)

// findExposedAdminURL returns the admin endpoint URL and the observation
// that exposed it, once the planner has seen the home page.
func findExposedAdminURL(in planner.Input) (string, model.Observation) {
	for _, o := range in.Observations {
		for _, ref := range o.ExposedRefs {
			if strings.Contains(ref, "/api/admin/users") {
				return ref, o
			}
		}
	}
	return "", model.Observation{}
}

// adminPlanner reads /api/admin/users as A first, then as B, independent of
// the doc-cross-read scenario (it never proposes anything for H-1).
func adminPlanner() planner.Planner {
	step := 0
	return plannerFunc(func(in planner.Input) (planner.Output, error) {
		url, src := findExposedAdminURL(in)
		if url == "" {
			return planner.Output{}, nil
		}
		acct, done := model.AccountA, map[model.AccountRef]bool{}
		for _, o := range in.Observations {
			if o.URL == url && o.ResultStatus == model.ResultCompleted {
				done[o.AccountRef] = true
			}
		}
		if done[model.AccountA] {
			acct = model.AccountB
		}
		if done[model.AccountA] && done[model.AccountB] {
			return planner.Output{}, nil // both sides seen; nothing further to propose
		}
		step++
		return planner.Output{Candidates: []model.Action{{
			ID: fmt.Sprintf("ADM-%d", step), Tool: model.ToolHTTPRequest, HypothesisID: agent.HypPrivesc,
			TypedArgs: model.HTTPArgs{Method: "GET", URL: url, AccountRef: acct, SourceObservationID: src.ID},
		}}}, nil
	})
}

func TestVerticalPrivescConfirmedInVulnerableMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	s := e.runAgent(context.Background(), adminPlanner(), judge.FirstCandidate{})
	if hypothesis(s, agent.HypPrivesc).Status != model.HypConfirmed {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypPrivesc))
	}
	f, ok := finding(s, agent.HypPrivesc)
	if !ok || f.Severity != model.SeverityCritical {
		t.Fatalf("finding %+v ok=%v", f, ok)
	}
	for _, id := range f.EvidenceIDs {
		if _, err := e.store.GetEvidence(context.Background(), id); err != nil {
			t.Errorf("evidence %s: %v", id, err)
		}
	}
}

func TestVerticalPrivescRejectedInFixedMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeFixed})
	s := e.runAgent(context.Background(), adminPlanner(), judge.FirstCandidate{})
	if hypothesis(s, agent.HypPrivesc).Status != model.HypRejected {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypPrivesc))
	}
	if _, ok := finding(s, agent.HypPrivesc); ok {
		t.Fatal("must not produce a finding when rejected")
	}
}

// Reusing LabHeuristic (the normal doc-cross-read flow) is enough for
// unauth-access-v1: it already reads an owned document via getDoc, whose
// response exposes a public mirror URL (SDD §14.3); the agent's own
// code-driven step then repeats that URL with no session.
func TestUnauthAccessConfirmedInVulnerableMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if hypothesis(s, agent.HypUnauth).Status != model.HypConfirmed {
		t.Fatalf("hypothesis %+v, verifications %+v", hypothesis(s, agent.HypUnauth), s.Verifications)
	}
	f, ok := finding(s, agent.HypUnauth)
	if !ok || f.Severity != model.SeverityHigh {
		t.Fatalf("finding %+v ok=%v", f, ok)
	}
	// The anonymous follow-up must be on record as its own Action with no
	// account, never inferred.
	found := false
	for _, a := range s.Actions {
		if a.TypedArgs.AccountRef == model.AccountNone {
			found = true
		}
	}
	if !found {
		t.Fatal("no code-driven anonymous action recorded")
	}
}

func TestUnauthAccessRejectedInFixedMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeFixed})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if hypothesis(s, agent.HypUnauth).Status != model.HypRejected {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypUnauth))
	}
}

// passive-exposure-v1 needs no dedicated scenario planner: the debug header
// is present on every response in vulnerable mode from the very first
// bootstrap request, so it confirms immediately regardless of what else runs.
func TestPassiveExposureConfirmedInVulnerableMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if hypothesis(s, agent.HypPassive).Status != model.HypConfirmed {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypPassive))
	}
	f, ok := finding(s, agent.HypPassive)
	if !ok || f.Severity != model.SeverityInfo {
		t.Fatalf("finding %+v ok=%v", f, ok)
	}
}

func TestPassiveExposureRejectedInFixedMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeFixed})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if hypothesis(s, agent.HypPassive).Status != model.HypRejected {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypPassive))
	}
}

// Sanity: the debug header toggle itself, end to end over real HTTP (not
// just the labapp unit test), through the executor's masking path.
func TestDebugHeaderReachesEvidenceUnmasked(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	obs := e.get(context.Background(), "A", e.url("/"))
	ev := e.evidence(obs)
	if !strings.Contains(ev.SanitizedResponse, "X-Debug-Backend") {
		t.Fatalf("%s", ev.SanitizedResponse)
	}
}

// Multi-rule report: report.Build must aggregate all four rule_ids seen in a
// single run, each independently, with no network and from stored state
// alone (AC-12 extended to M6a).
func TestReportAggregatesM6aRules(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	live := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	snap, err := state.ReadSnapshot(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	r := report.Build(snap, time.Now())
	byRule := map[string]report.RuleCount{}
	for _, c := range r.RuleSummary {
		byRule[c.RuleID] = c
	}
	if byRule["doc-cross-read-v1"].Confirmed != 1 {
		t.Errorf("doc-cross-read-v1: %+v", byRule["doc-cross-read-v1"])
	}
	// Unlike doc-cross-read-v1 (which stops the run on its first confirm),
	// passive-exposure-v1 keeps re-confirming with growing evidence each
	// cycle until the run ends for some other reason: at least one, not
	// necessarily exactly one.
	if byRule["passive-exposure-v1"].Confirmed < 1 {
		t.Errorf("passive-exposure-v1: %+v", byRule["passive-exposure-v1"])
	}
	// H-PRIVESC was never exercised by LabHeuristic; it ends inconclusive.
	if byRule["vertical-privesc-v1"].Inconclusive == 0 {
		t.Errorf("vertical-privesc-v1: %+v", byRule["vertical-privesc-v1"])
	}
	if len(live.Findings) < 2 {
		t.Fatalf("expected at least doc-cross-read and passive-exposure findings: %+v", live.Findings)
	}
}
