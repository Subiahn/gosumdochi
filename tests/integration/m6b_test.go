// M6b: reflected-input-v1 (ADR-0002), driven by the real agent loop with a
// fake Planner/Judge, no model.
package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gosumdochi/internal/agent"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/labapp"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
)

// reflectedPlanner finds the search form's discovered "q" parameter (exposed
// by the bootstrap home-page read) and tries markers in order, one per
// cycle, until H-REFLECTED is confirmed or the markers run out.
func reflectedPlanner() planner.Planner {
	tried := map[model.MarkerID]bool{}
	markers := []model.MarkerID{model.MarkerXSSScript, model.MarkerSQLiQuote}
	step := 0
	return plannerFunc(func(in planner.Input) (planner.Output, error) {
		var searchURL, srcID string
		for _, o := range in.Observations {
			for _, p := range o.DiscoveredParams {
				if p != "q" {
					continue
				}
				for _, ref := range o.ExposedRefs {
					if strings.Contains(ref, "/api/search") {
						searchURL, srcID = ref, o.ID
					}
				}
			}
		}
		if searchURL == "" {
			return planner.Output{}, nil
		}
		for _, m := range markers {
			if tried[m] {
				continue
			}
			tried[m] = true
			step++
			return planner.Output{Candidates: []model.Action{{
				ID: fmt.Sprintf("REF-%d", step), Tool: model.ToolHTTPRequest, HypothesisID: agent.HypReflected,
				TypedArgs: model.HTTPArgs{
					Method: "GET", URL: searchURL, AccountRef: model.AccountA, SourceObservationID: srcID,
					ParamName: "q", MarkerID: m,
				},
			}}}, nil
		}
		return planner.Output{}, nil
	})
}

func TestReflectedInputConfirmedInVulnerableMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	s := e.runAgent(context.Background(), reflectedPlanner(), judge.FirstCandidate{})
	if hypothesis(s, agent.HypReflected).Status != model.HypConfirmed {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypReflected))
	}
	f, ok := finding(s, agent.HypReflected)
	if !ok || f.Severity != model.SeverityHigh {
		t.Fatalf("finding %+v ok=%v", f, ok)
	}
	for _, id := range f.EvidenceIDs {
		if _, err := e.store.GetEvidence(context.Background(), id); err != nil {
			t.Errorf("evidence %s: %v", id, err)
		}
	}
}

func TestReflectedInputRejectedInFixedMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeFixed})
	s := e.runAgent(context.Background(), reflectedPlanner(), judge.FirstCandidate{})
	if hypothesis(s, agent.HypReflected).Status != model.HypRejected {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.HypReflected))
	}
	if _, ok := finding(s, agent.HypReflected); ok {
		t.Fatal("must not produce a finding when rejected")
	}
}

// The benign marker alone must never confirm H-REFLECTED (it has no
// metacharacters -- see internal/verifier/reflected.go).
func TestReflectedInputBenignMarkerAloneNeverConfirms(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	p := plannerFunc(func(in planner.Input) (planner.Output, error) {
		var searchURL, srcID string
		for _, o := range in.Observations {
			for _, p := range o.DiscoveredParams {
				if p != "q" {
					continue
				}
				for _, ref := range o.ExposedRefs {
					if strings.Contains(ref, "/api/search") {
						searchURL, srcID = ref, o.ID
					}
				}
			}
		}
		if searchURL == "" {
			return planner.Output{}, nil
		}
		return planner.Output{Candidates: []model.Action{{
			ID: "REF-1", Tool: model.ToolHTTPRequest, HypothesisID: agent.HypReflected,
			TypedArgs: model.HTTPArgs{
				Method: "GET", URL: searchURL, AccountRef: model.AccountA, SourceObservationID: srcID,
				ParamName: "q", MarkerID: model.MarkerBenign,
			},
		}}}, nil
	})
	s := e.runAgent(context.Background(), p, judge.FirstCandidate{})
	if hypothesis(s, agent.HypReflected).Status == model.HypConfirmed {
		t.Fatalf("benign marker alone must not confirm: %+v", hypothesis(s, agent.HypReflected))
	}
}

// An action naming an undiscovered param_name must be rejected before it
// ever reaches the network (model.ValidateAction, ADR-0002 §2).
func TestReflectedInputUndiscoveredParamRejected(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	p := plannerFunc(func(in planner.Input) (planner.Output, error) {
		for _, o := range in.Observations {
			if o.URL == e.url("/") {
				return planner.Output{Candidates: []model.Action{{
					ID: "REF-1", Tool: model.ToolHTTPRequest, HypothesisID: agent.HypReflected,
					TypedArgs: model.HTTPArgs{
						Method: "GET", URL: e.url("/api/search"), AccountRef: model.AccountA, SourceObservationID: o.ID,
						ParamName: "not_a_real_param", MarkerID: model.MarkerXSSScript,
					},
				}}}, nil
			}
		}
		return planner.Output{}, nil
	})
	s := e.runAgent(context.Background(), p, judge.FirstCandidate{})
	for _, a := range s.Actions {
		if a.ID == "REF-1" {
			t.Fatal("an action with an undiscovered param_name must never reach s.Actions (rejected candidate)")
		}
	}
}
