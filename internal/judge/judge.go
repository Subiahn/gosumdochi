// Package judge selects among validated candidates using fixed enums.
package judge

import (
	"context"

	"gosumdochi/internal/model"
)

type Input struct {
	Candidates    []model.Action
	Hypotheses    []model.Hypothesis
	Observations  []model.Observation
	Verifications []model.Verification
	Remaining     model.Budgets
	Feedback      string // validation error code of the previous attempt, on recovery
}

// Judge returns a Decision that must pass model.ValidateDecision. It cannot
// change scope, build requests, or mark a hypothesis confirmed.
type Judge interface {
	Judge(ctx context.Context, in Input) (model.Decision, error)
}
