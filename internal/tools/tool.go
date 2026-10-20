// Package tools defines the executable tool contract.
package tools

import (
	"context"

	"gosumdochi/internal/model"
)

type Result struct {
	Status     model.ResultStatus
	HTTPStatus int
	Headers    map[string][]string
	Body       string // limited, text only
	DurationMS int64
	ErrorCode  string
}

// Tool executes an action that Policy already approved. Implementations must
// honor ctx cancellation and must not be reachable from llm.
type Tool interface {
	Name() string
	Execute(ctx context.Context, a model.Action) (Result, error)
}
