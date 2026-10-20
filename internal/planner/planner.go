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
	Remaining    model.Budgets
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
