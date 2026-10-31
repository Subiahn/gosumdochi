// Package report turns a stored Snapshot into JSON and Markdown. It reads
// only state.Snapshot: no network, no model, no evaluator answer key. The
// same Snapshot always yields the same Report (AC-12), aside from the
// generation timestamp.
package report

import (
	"fmt"
	"sort"
	"time"

	"gosumdochi/internal/model"
	"gosumdochi/internal/state"
)

const SchemaVersion = model.SchemaVersion

// standingLimitations hold regardless of outcome (SDD §10, §12): they are
// properties of the method, not of any one run.
var standingLimitations = []string{
	"Verification rule doc-cross-read-v1 is specific to this synthetic practice scenario; it is not a general authorization checker.",
	"A finding covers only the documents actually read during this run. It does not imply the rest of the application is safe.",
	"Status codes or identical response bodies alone never confirm a finding; confirmation requires matching owner and marker evidence.",
}

type Report struct {
	SchemaVersion int                  `json:"schema_version"`
	GeneratedAt   time.Time            `json:"generated_at"`
	Run           model.Run            `json:"run"`
	Hypotheses    []model.Hypothesis   `json:"hypotheses"`
	Verifications []model.Verification `json:"verifications"`
	Findings      []model.Finding      `json:"findings"`
	// RuleSummary counts verification outcomes per rule_id, across every
	// verification recorded for the run (v0.1 has one rule).
	RuleSummary []RuleCount `json:"rule_summary"`
	Limitations []string    `json:"limitations"`
}

type RuleCount struct {
	RuleID       string `json:"rule_id"`
	Confirmed    int    `json:"confirmed"`
	Rejected     int    `json:"rejected"`
	Inconclusive int    `json:"inconclusive"`
}

// Build derives a Report from a run's stored Snapshot. now is injected for
// deterministic tests; callers pass time.Now.
func Build(snap state.Snapshot, now time.Time) Report {
	r := Report{
		SchemaVersion: SchemaVersion, GeneratedAt: now.UTC(), Run: snap.Run,
		Hypotheses: snap.Hypotheses, Verifications: snap.Verifications, Findings: snap.Findings,
	}
	r.RuleSummary = summarizeRules(snap.Verifications)
	r.Limitations = append(limitationsFor(snap), standingLimitations...)
	return r
}

func summarizeRules(vs []model.Verification) []RuleCount {
	counts := map[string]*RuleCount{}
	var order []string
	for _, v := range vs {
		c, ok := counts[v.RuleID]
		if !ok {
			c = &RuleCount{RuleID: v.RuleID}
			counts[v.RuleID] = c
			order = append(order, v.RuleID)
		}
		switch v.Outcome {
		case model.OutcomeConfirmed:
			c.Confirmed++
		case model.OutcomeRejected:
			c.Rejected++
		case model.OutcomeInconclusive:
			c.Inconclusive++
		}
	}
	sort.Strings(order)
	out := make([]RuleCount, len(order))
	for i, id := range order {
		out[i] = *counts[id]
	}
	return out
}

// limitationsFor explains gaps specific to this run: an incomplete status,
// or a hypothesis left inconclusive and why.
func limitationsFor(snap state.Snapshot) []string {
	var out []string
	if snap.Run.Status != model.RunCompleted {
		note := snap.Run.StopNote
		if note == "" {
			note = string(snap.Run.StopReason)
		}
		out = append(out, fmt.Sprintf("Run ended as %q (stop_reason=%s): %s. Findings below reflect only what was confirmed before that point.",
			snap.Run.Status, snap.Run.StopReason, note))
	}
	for _, h := range snap.Hypotheses {
		if h.Status != model.HypInconclusive {
			continue
		}
		reason := lastReason(snap.Verifications, h.ID)
		if reason == "" {
			reason = "not enough evidence was collected to confirm or reject it"
		}
		out = append(out, fmt.Sprintf("Hypothesis %s (%q) is inconclusive: %s.", h.ID, h.Claim, reason))
	}
	return out
}

func lastReason(vs []model.Verification, hypID string) string {
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].HypothesisID == hypID && len(vs[i].Reasons) > 0 {
			return vs[i].Reasons[len(vs[i].Reasons)-1]
		}
	}
	return ""
}
