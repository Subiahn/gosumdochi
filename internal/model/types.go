package model

import "time"

// Meta is carried by every persisted record.
type Meta struct {
	SchemaVersion int       `json:"schema_version"`
	RunID         string    `json:"run_id"`
	Timestamp     time.Time `json:"timestamp"` // UTC
}

type Budgets struct {
	MaxCycles       int `json:"max_cycles"`
	MaxHTTPRequests int `json:"max_http_requests"`
	MaxModelCalls   int `json:"max_model_calls"`
	MaxCandidates   int `json:"max_candidates"`
	HTTPTimeoutSec  int `json:"http_timeout_sec"`
	LLMTimeoutSec   int `json:"llm_timeout_sec"`
	TotalTimeoutSec int `json:"total_timeout_sec"`
	MaxBodyBytes    int `json:"max_body_bytes"`
}

// DefaultBudgets are the SDD v0.1 initial values.
func DefaultBudgets() Budgets {
	return Budgets{
		MaxCycles: 15, MaxHTTPRequests: 30, MaxModelCalls: 32, MaxCandidates: 3,
		HTTPTimeoutSec: 10, LLMTimeoutSec: 120, TotalTimeoutSec: 900, MaxBodyBytes: 64 * 1024,
	}
}

type Run struct {
	Meta
	ID           string         `json:"id"`
	Goal         string         `json:"goal"`
	ConfigDigest string         `json:"config_digest"`
	ModelID      string         `json:"model_id"`
	ModelOptions map[string]any `json:"model_options,omitempty"`
	Status       RunStatus      `json:"status"`
	StopReason   StopReason     `json:"stop_reason,omitempty"`
	Budgets      Budgets        `json:"budgets"`
}

// HTTPArgs are the typed arguments of the http_request tool. The model never
// supplies login paths, secrets or auth headers.
type HTTPArgs struct {
	Method              string            `json:"method"`
	URL                 string            `json:"url"`
	AccountRef          AccountRef        `json:"account_ref"`
	Headers             map[string]string `json:"headers,omitempty"`
	SourceObservationID string            `json:"source_observation_id"`
}

const ToolHTTPRequest = "http_request"

type Action struct {
	Meta
	ID               string   `json:"id"`
	Tool             string   `json:"tool"`
	TypedArgs        HTTPArgs `json:"typed_args"`
	HypothesisID     string   `json:"hypothesis_id"`
	RationaleSummary string   `json:"rationale_summary"`
}

type Observation struct {
	Meta
	ID           string       `json:"id"`
	ActionID     string       `json:"action_id"`
	ResultStatus ResultStatus `json:"result_status"`
	EvidenceIDs  []string     `json:"evidence_ids"`
	Summary      string       `json:"summary"`
	// ExposedRefs are URLs/document IDs extracted deterministically from the
	// response. Later actions may only target refs exposed by an observation.
	ExposedRefs []string `json:"exposed_refs,omitempty"`
}

type Hypothesis struct {
	Meta
	ID                       string           `json:"id"`
	Claim                    string           `json:"claim"`
	SupportingEvidenceIDs    []string         `json:"supporting_evidence_ids"`
	ContradictingEvidenceIDs []string         `json:"contradicting_evidence_ids"`
	Status                   HypothesisStatus `json:"status"`
}

type Decision struct {
	Meta
	ID               string       `json:"id,omitempty"`
	SelectedActionID string       `json:"selected_action_id,omitempty"`
	Decision         DecisionKind `json:"decision"`
	ReasonCode       ReasonCode   `json:"reason_code"`
	EvidenceIDs      []string     `json:"evidence_ids"`
}

type Evidence struct {
	Meta
	ID                string     `json:"id"`
	ActionID          string     `json:"action_id"`
	AccountRef        AccountRef `json:"account_ref"`
	SanitizedRequest  string     `json:"sanitized_request"`
	SanitizedResponse string     `json:"sanitized_response"`
	// CapturedBodyHash is the SHA-256 of the stored (masked, truncated) body
	// bytes only; it is not a hash of the original response.
	CapturedBodyHash string `json:"captured_body_hash"`
	Truncated        bool   `json:"truncated"`
	ContentLength    *int64 `json:"original_content_length,omitempty"`
}

type VerificationOutcome string

const (
	OutcomeConfirmed    VerificationOutcome = "confirmed"
	OutcomeRejected     VerificationOutcome = "rejected"
	OutcomeInconclusive VerificationOutcome = "inconclusive"
)

var VerificationOutcomes = []VerificationOutcome{OutcomeConfirmed, OutcomeRejected, OutcomeInconclusive}

type Verification struct {
	Meta
	ID            string              `json:"id"`
	HypothesisID  string              `json:"hypothesis_id"`
	BaselineIDs   []string            `json:"baseline_ids"`
	ComparisonIDs []string            `json:"comparison_ids"`
	Outcome       VerificationOutcome `json:"outcome"`
	RuleID        string              `json:"rule_id"`
}

type Finding struct {
	Meta
	ID             string   `json:"id"`
	HypothesisID   string   `json:"hypothesis_id"`
	Title          string   `json:"title"`
	Impact         string   `json:"impact"`
	VerificationID string   `json:"verification_id"`
	EvidenceIDs    []string `json:"evidence_ids"`
	Limitations    []string `json:"limitations"`
}
