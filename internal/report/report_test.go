package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gosumdochi/internal/model"
	"gosumdochi/internal/state"
)

var fixedNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func confirmedSnapshot() state.Snapshot {
	return state.Snapshot{
		Run:        model.Run{ID: "run-1", Goal: "check cross-account access", ModelID: "qwen3:8b@abc", Status: model.RunCompleted, StopReason: model.StopGoalVerified, Cycles: 4, ModelCalls: 6, HTTPRequests: 11},
		Hypotheses: []model.Hypothesis{{ID: "H-1", Claim: "A can read B's doc", Status: model.HypConfirmed}},
		Verifications: []model.Verification{
			{ID: "V-0001", HypothesisID: "H-1", Outcome: model.OutcomeInconclusive, RuleID: "doc-cross-read-v1", Reasons: []string{"no cross-account read attempted yet"}},
			{ID: "V-0002", HypothesisID: "H-1", Outcome: model.OutcomeConfirmed, RuleID: "doc-cross-read-v1", BaselineIDs: []string{"E-1"}, ComparisonIDs: []string{"E-2", "E-3"}, Reasons: []string{"A read B's document"}},
		},
		Findings: []model.Finding{{ID: "F-0001", HypothesisID: "H-1", Title: "cross-account read", Impact: "A read B's document", VerificationID: "V-0002", EvidenceIDs: []string{"E-1", "E-2", "E-3"}, Limitations: []string{"only one document tested"}}},
	}
}

func TestBuildConfirmed(t *testing.T) {
	r := Build(confirmedSnapshot(), fixedNow)
	if len(r.Findings) != 1 || len(r.RuleSummary) != 1 {
		t.Fatalf("%+v", r)
	}
	rs := r.RuleSummary[0]
	if rs.RuleID != "doc-cross-read-v1" || rs.Confirmed != 1 || rs.Inconclusive != 1 || rs.Rejected != 0 {
		t.Fatalf("%+v", rs)
	}
	for _, l := range r.Limitations {
		if strings.Contains(l, "inconclusive") && strings.Contains(l, "H-1") {
			t.Fatalf("H-1 is confirmed; must not be reported as inconclusive: %s", l)
		}
	}
	// Standing limitations are always present, confirmed or not.
	if !containsSub(r.Limitations, "doc-cross-read-v1 is specific") {
		t.Fatalf("missing standing limitation: %v", r.Limitations)
	}
}

func TestBuildInconclusive(t *testing.T) {
	snap := state.Snapshot{
		Run:        model.Run{ID: "run-2", Status: model.RunCompleted, StopReason: model.StopExplorationExhaust},
		Hypotheses: []model.Hypothesis{{ID: "H-1", Claim: "A can read B's doc", Status: model.HypInconclusive}},
		Verifications: []model.Verification{
			{ID: "V-0001", HypothesisID: "H-1", Outcome: model.OutcomeInconclusive, RuleID: "doc-cross-read-v1", Reasons: []string{"status alone is not enough"}},
		},
	}
	r := Build(snap, fixedNow)
	if len(r.Findings) != 0 {
		t.Fatalf("no finding should exist: %+v", r.Findings)
	}
	if !containsSub(r.Limitations, "H-1") || !containsSub(r.Limitations, "status alone is not enough") {
		t.Fatalf("missing per-hypothesis limitation: %v", r.Limitations)
	}
}

func TestBuildIncompleteRunNotedInLimitations(t *testing.T) {
	snap := state.Snapshot{Run: model.Run{ID: "run-3", Status: model.RunIncomplete, StopReason: model.StopBudgetExhausted, StopNote: "http request budget"}}
	r := Build(snap, fixedNow)
	if !containsSub(r.Limitations, "budget_exhausted") || !containsSub(r.Limitations, "http request budget") {
		t.Fatalf("%v", r.Limitations)
	}
}

func containsSub(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// AC-12: regenerating from the same stored snapshot is deterministic.
func TestBuildIsDeterministic(t *testing.T) {
	snap := confirmedSnapshot()
	a, b := Build(snap, fixedNow), Build(snap, fixedNow)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("Build is not deterministic for identical input")
	}
}

func TestMarkdownTracesToIDs(t *testing.T) {
	md := Markdown(Build(confirmedSnapshot(), fixedNow))
	for _, want := range []string{"F-0001", "V-0002", "E-1", "E-2", "E-3", "doc-cross-read-v1", "H-1", "run-1"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestMarkdownNoFindingsSaysSo(t *testing.T) {
	md := Markdown(Build(state.Snapshot{Run: model.Run{ID: "run-4", Status: model.RunCompleted}}, fixedNow))
	if !strings.Contains(md, "None confirmed") {
		t.Fatalf("%s", md)
	}
}

// A field value containing "|" or a newline must not break the markdown tables.
func TestMarkdownEscapesTableCells(t *testing.T) {
	snap := state.Snapshot{
		Run:        model.Run{ID: "run-5", Goal: "a | b\nsecond line", Status: model.RunCompleted},
		Hypotheses: []model.Hypothesis{{ID: "H-1", Claim: "x | y", Status: model.HypInconclusive}},
	}
	md := Markdown(Build(snap, fixedNow))
	// Table cells must be escaped (checked below); prose in the Limitations
	// bullet list is not a table and is fine to carry "|" verbatim.
	if strings.Contains(md, "a | b\nsecond") {
		t.Fatal("unescaped cell content broke the table")
	}
	if !strings.Contains(md, `a \| b second line`) || !strings.Contains(md, `| H-1 | inconclusive | x \| y |`) {
		t.Fatalf("expected escaped cell content:\n%s", md)
	}
}
