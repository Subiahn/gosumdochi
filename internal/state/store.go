// Package state defines run-state persistence. v0.1 backs it with files;
// SQLite can replace it behind this interface.
package state

import (
	"context"

	"gosumdochi/internal/model"
)

// Store keeps facts, hypotheses and decisions separate: model guesses are
// never stored as facts. A Store has a single writer.
type Store interface {
	AppendEvent(ctx context.Context, kind string, payload any) error
	PutEvidence(ctx context.Context, e model.Evidence) error
	GetEvidence(ctx context.Context, id string) (model.Evidence, error)
	SaveState(ctx context.Context, s Snapshot) error
	LoadState(ctx context.Context) (Snapshot, error)
}

type Snapshot struct {
	model.Meta
	Run           model.Run            `json:"run"`
	Observations  []model.Observation  `json:"observations"`
	Hypotheses    []model.Hypothesis   `json:"hypotheses"`
	Decisions     []model.Decision     `json:"decisions"`
	Verifications []model.Verification `json:"verifications"`
	Findings      []model.Finding      `json:"findings"`
}
