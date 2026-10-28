package planner

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"gosumdochi/internal/model"
)

// LabHeuristic is a deterministic test double that knows the shape of the
// practice app (a document list that exposes per-document URLs). It reads
// only observations, like a real planner, and exists so the agent loop can be
// verified without a model. It is not a model substitute for evaluation.
type LabHeuristic struct{}

var accounts = []model.AccountRef{model.AccountA, model.AccountB}

func (LabHeuristic) Plan(_ context.Context, in Input) (Output, error) {
	if len(in.Hypotheses) == 0 {
		return Output{}, nil
	}
	h := in.Hypotheses[0].ID
	done := map[string]int{} // acct|url -> completed reads
	lists := map[model.AccountRef]model.Observation{}
	var listURL string
	var listSrc model.Observation
	for _, o := range in.Observations {
		if o.ResultStatus == model.ResultCompleted {
			done[string(o.AccountRef)+"|"+o.URL]++
		}
		for _, r := range o.ExposedRefs {
			if u, err := url.Parse(r); err == nil && strings.HasSuffix(u.Path, "/docs") && listURL == "" {
				listURL, listSrc = r, o
			}
		}
	}
	if listURL == "" {
		return Output{}, nil
	}
	for _, o := range in.Observations {
		if o.URL == listURL && o.ResultStatus == model.ResultCompleted && len(o.ExposedRefs) > 0 {
			lists[o.AccountRef] = o
		}
	}

	var steps []model.Action
	add := func(acct model.AccountRef, target string, src model.Observation) {
		steps = append(steps, model.Action{
			Tool: model.ToolHTTPRequest, HypothesisID: h,
			TypedArgs:        model.HTTPArgs{Method: "GET", URL: target, AccountRef: acct, SourceObservationID: src.ID},
			RationaleSummary: fmt.Sprintf("read %s as %s", target, acct),
		})
	}
	for _, a := range accounts { // 1. each account lists its documents
		if _, ok := lists[a]; !ok {
			add(a, listURL, listSrc)
		}
	}
	if len(steps) == 0 {
		first := func(a model.AccountRef) string { return lists[a].ExposedRefs[0] }
		for _, a := range []model.AccountRef{model.AccountB, model.AccountA} { // 2. own documents: baseline, control
			if done[string(a)+"|"+first(a)] == 0 {
				add(a, first(a), lists[a])
			}
		}
		// 3. cross read, then one independent repeat
		if bDoc := first(model.AccountB); done[string(model.AccountA)+"|"+bDoc] < 2 {
			add(model.AccountA, bDoc, lists[model.AccountB])
		}
	}
	if len(steps) > 3 {
		steps = steps[:3]
	}
	for i := range steps {
		steps[i].ID = fmt.Sprintf("P%d-%d", len(in.Observations), i+1)
	}
	return Output{Candidates: steps}, nil
}
