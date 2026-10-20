// Package model defines the shared data contracts. It depends on no other
// internal package.
package model

import "fmt"

// SchemaVersion is stamped on every persisted record.
const SchemaVersion = 1

type HypothesisStatus string

const (
	HypProposed     HypothesisStatus = "proposed"
	HypTesting      HypothesisStatus = "testing"
	HypConfirmed    HypothesisStatus = "confirmed"
	HypRejected     HypothesisStatus = "rejected"
	HypInconclusive HypothesisStatus = "inconclusive"
)

type ResultStatus string

const (
	ResultCompleted ResultStatus = "completed"
	ResultFailed    ResultStatus = "failed"
	ResultDenied    ResultStatus = "denied"
	ResultCanceled  ResultStatus = "canceled"
)

type RunStatus string

const (
	RunPending    RunStatus = "pending"
	RunRunning    RunStatus = "running"
	RunCompleted  RunStatus = "completed"
	RunIncomplete RunStatus = "incomplete"
	RunCanceled   RunStatus = "canceled"
	RunFailed     RunStatus = "failed"
)

type StopReason string

const (
	StopGoalVerified        StopReason = "goal_verified"
	StopExplorationExhaust  StopReason = "exploration_exhausted"
	StopBudgetExhausted     StopReason = "budget_exhausted"
	StopRepeatedAction      StopReason = "repeated_action"
	StopModelError          StopReason = "model_error"
	StopToolError           StopReason = "tool_error"
	StopUserCanceled        StopReason = "user_canceled"
	StopInfrastructureError StopReason = "infrastructure_error"
)

type DecisionKind string

const (
	DecisionExecute      DecisionKind = "execute"
	DecisionStop         DecisionKind = "stop"
	DecisionInconclusive DecisionKind = "inconclusive"
)

// ReasonCode is versioned with the Judge prompt/contract (schema_version 1).
type ReasonCode string

const (
	ReasonMissingControlComparison ReasonCode = "missing_control_comparison"
	ReasonNeedsReproduction        ReasonCode = "needs_reproduction"
	ReasonNeedsIdentityCheck       ReasonCode = "needs_identity_check"
	ReasonNewObservationNeeded     ReasonCode = "new_observation_needed"
	ReasonHypothesisVerified       ReasonCode = "hypothesis_verified"
	ReasonExplorationExhausted     ReasonCode = "exploration_exhausted"
	ReasonInsufficientEvidence     ReasonCode = "insufficient_evidence"
	ReasonBudgetLow                ReasonCode = "budget_low"
)

// Enumerations used for validation and schema consistency tests.
var (
	HypothesisStatuses = []HypothesisStatus{HypProposed, HypTesting, HypConfirmed, HypRejected, HypInconclusive}
	ResultStatuses     = []ResultStatus{ResultCompleted, ResultFailed, ResultDenied, ResultCanceled}
	RunStatuses        = []RunStatus{RunPending, RunRunning, RunCompleted, RunIncomplete, RunCanceled, RunFailed}
	StopReasons        = []StopReason{StopGoalVerified, StopExplorationExhaust, StopBudgetExhausted, StopRepeatedAction, StopModelError, StopToolError, StopUserCanceled, StopInfrastructureError}
	DecisionKinds      = []DecisionKind{DecisionExecute, DecisionStop, DecisionInconclusive}
	ReasonCodes        = []ReasonCode{ReasonMissingControlComparison, ReasonNeedsReproduction, ReasonNeedsIdentityCheck, ReasonNewObservationNeeded, ReasonHypothesisVerified, ReasonExplorationExhausted, ReasonInsufficientEvidence, ReasonBudgetLow}
)

func oneOf[T ~string](v T, set []T) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// AccountRef is the only identity handle exposed to the model.
type AccountRef string

const (
	AccountA AccountRef = "A"
	AccountB AccountRef = "B"
)

// CanTransition reports whether a hypothesis may move between statuses.
// Only the Verifier path may move a hypothesis to confirmed; this function
// only checks structural legality.
func CanTransition(from, to HypothesisStatus) bool {
	switch from {
	case HypProposed:
		return to == HypTesting || to == HypRejected || to == HypInconclusive
	case HypTesting:
		return to == HypConfirmed || to == HypRejected || to == HypInconclusive
	case HypInconclusive:
		return to == HypTesting
	}
	return false
}

func enumErr(field string, v any) error {
	return &ValidationError{Code: CodeUnknownEnum, Field: field, Msg: fmt.Sprintf("unknown value %q", v)}
}
