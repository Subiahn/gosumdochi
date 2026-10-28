package judge

import (
	"context"

	"gosumdochi/internal/model"
)

// FirstCandidate always executes the first candidate; with none it ends the
// exploration. Test double for the agent loop.
type FirstCandidate struct{}

func (FirstCandidate) Judge(_ context.Context, in Input) (model.Decision, error) {
	if len(in.Candidates) == 0 {
		return model.Decision{Decision: model.DecisionInconclusive, ReasonCode: model.ReasonExplorationExhausted, EvidenceIDs: []string{}}, nil
	}
	return model.Decision{
		Decision: model.DecisionExecute, SelectedActionID: in.Candidates[0].ID,
		ReasonCode: model.ReasonNewObservationNeeded, EvidenceIDs: []string{},
	}, nil
}
