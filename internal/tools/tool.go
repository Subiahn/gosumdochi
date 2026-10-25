// Package tools defines the executable tool contract.
package tools

import (
	"context"

	"gosumdochi/internal/model"
)

// Error codes carried in Result.ErrorCode.
const (
	ErrDeniedAddress   = "denied_address"
	ErrBudgetExhausted = "budget_exhausted"
	ErrLoginFailed     = "login_failed"
	ErrTimeout         = "timeout"
	ErrCanceled        = "canceled"
	ErrNetwork         = "network"
)

type Result struct {
	Status        model.ResultStatus
	HTTPStatus    int
	Headers       map[string][]string
	Body          string // limited, text only
	Truncated     bool   // Body was cut at the size limit
	ContentLength int64  // declared Content-Length of the response, -1 if unknown
	DurationMS    int64
	ErrorCode     string
}

// Tool executes an action that Policy already approved. Implementations must
// honor ctx cancellation and must not be reachable from llm.
type Tool interface {
	Name() string
	Execute(ctx context.Context, a model.Action) (Result, error)
}
