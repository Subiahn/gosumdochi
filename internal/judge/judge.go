// Package judge selects among validated candidates using fixed enums.
package judge

import (
	"context"

	"gosumdochi/internal/model"
)

type Input struct {
	Candidates   []model.Action
	Hypotheses   []model.Hypothesis
	Observations []model.Observation
	Remaining    model.Budgets
}

// Judge returns a Decision that must pass model.ValidateDecision. It cannot
// change scope, build requests, or mark a hypothesis confirmed.
type Judge interface {
	Judge(ctx context.Context, in Input) (model.Decision, error)
}
