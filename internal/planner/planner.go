// Package planner turns observations into hypotheses and action candidates.
package planner

import (
	"context"

	"gosumdochi/internal/model"
)

type Input struct {
	Goal         string
	Observations []model.Observation // already summarized
	Hypotheses   []model.Hypothesis
	// Verifications are the Verifier's own, code-computed findings so far
	// (outcome and Reasons). The Verifier already names exactly what
	// evidence is missing (e.g. "no control: reader's own document
	// response"); handing that to the Planner beats re-deriving the same
	// gap from free-text prose rules alone.
	Verifications []model.Verification
	Remaining     model.Budgets
	// Feedback is the validation error code of the previous attempt in this
	// cycle, set only on a recovery call.
	Feedback string
}

type HypothesisProposal struct {
	ID    string `json:"id"`
	Claim string `json:"claim"`
}

type Output struct {
	Candidates []model.Action       `json:"candidates"`
	Proposals  []HypothesisProposal `json:"hypothesis_proposals"`
}

// Planner proposes; it never executes tools. Output must be validated with
// model.ValidateCandidates before use.
type Planner interface {
	Plan(ctx context.Context, in Input) (Output, error)
}
