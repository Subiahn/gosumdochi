package model

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
)

// DecodeStrict decodes a single JSON value, rejecting unknown fields and
// trailing data. Model output must go through this before any use.
func DecodeStrict[T any](data []byte) (T, error) {
	var v T
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, &ValidationError{Code: CodeDecode, Field: "$", Msg: err.Error()}
	}
	if _, err := dec.Token(); err != io.EOF {
		return v, &ValidationError{Code: CodeDecode, Field: "$", Msg: "trailing data after JSON value"}
	}
	return v, nil
}

// Refs resolves identifiers that a model-produced record may refer to.
type Refs struct {
	Candidates   map[string]Action      // candidate actions of the current cycle
	Evidence     map[string]struct{}    // known evidence IDs
	Observations map[string]Observation // known observations
	Hypotheses   map[string]struct{}    // known hypothesis IDs
}

func (r Refs) evidenceExists(id string) bool { _, ok := r.Evidence[id]; return ok }

func (r Refs) checkEvidence(field string, ids []string) error {
	for _, id := range ids {
		if !r.evidenceExists(id) {
			return &ValidationError{Code: CodeUnknownRef, Field: field, Msg: "unknown evidence id " + id}
		}
	}
	return nil
}

// ValidateDecision checks a Judge output against the fixed enums and the
// candidates/evidence that actually exist. It does not decide whether a stop
// is justified; the agent checks that against Verifier state.
func ValidateDecision(d Decision, refs Refs) error {
	if !oneOf(d.Decision, DecisionKinds) {
		return enumErr("decision", d.Decision)
	}
	if !oneOf(d.ReasonCode, ReasonCodes) {
		return enumErr("reason_code", d.ReasonCode)
	}
	switch d.Decision {
	case DecisionExecute:
		if d.SelectedActionID == "" {
			return missing("selected_action_id")
		}
		if _, ok := refs.Candidates[d.SelectedActionID]; !ok {
			return &ValidationError{Code: CodeUnknownRef, Field: "selected_action_id", Msg: "not a current candidate: " + d.SelectedActionID}
		}
	default:
		if d.SelectedActionID != "" {
			return invalid("selected_action_id", "must be empty unless decision is execute")
		}
	}
	return refs.checkEvidence("evidence_ids", d.EvidenceIDs)
}

// ValidateAction checks a Planner candidate. The tool must be known, the
// method GET (data requests), and the URL must be a ref exposed by the
// referenced observation, so model-invented URLs/IDs never reach Policy.
func ValidateAction(a Action, refs Refs) error {
	if a.ID == "" {
		return missing("id")
	}
	if a.Tool != ToolHTTPRequest {
		return enumErr("tool", a.Tool)
	}
	args := a.TypedArgs
	if args.Method != "GET" {
		return invalid("typed_args.method", "only GET is allowed for target data requests")
	}
	if args.AccountRef != AccountA && args.AccountRef != AccountB {
		return enumErr("typed_args.account_ref", args.AccountRef)
	}
	if args.URL == "" {
		return missing("typed_args.url")
	}
	if _, err := url.Parse(args.URL); err != nil {
		return invalid("typed_args.url", err.Error())
	}
	if a.HypothesisID == "" {
		return missing("hypothesis_id")
	}
	if _, ok := refs.Hypotheses[a.HypothesisID]; !ok {
		return &ValidationError{Code: CodeUnknownRef, Field: "hypothesis_id", Msg: "unknown hypothesis " + a.HypothesisID}
	}
	if args.SourceObservationID == "" {
		return missing("typed_args.source_observation_id")
	}
	obs, ok := refs.Observations[args.SourceObservationID]
	if !ok {
		return &ValidationError{Code: CodeUnknownRef, Field: "typed_args.source_observation_id", Msg: "unknown observation " + args.SourceObservationID}
	}
	for _, ref := range obs.ExposedRefs {
		if ref == args.URL {
			return nil
		}
	}
	return &ValidationError{Code: CodeUntrustedRef, Field: "typed_args.url", Msg: "url not exposed by observation " + obs.ID}
}

// ValidateCandidates enforces the per-cycle candidate cap and per-item validity.
func ValidateCandidates(cands []Action, max int, refs Refs) error {
	if len(cands) > max {
		return &ValidationError{Code: CodeTooMany, Field: "candidates", Msg: "exceeds per-cycle limit"}
	}
	seen := map[string]bool{}
	for i, c := range cands {
		if seen[c.ID] {
			return invalid("candidates", "duplicate id "+c.ID)
		}
		seen[c.ID] = true
		if err := ValidateAction(c, refs); err != nil {
			if ve, ok := err.(*ValidationError); ok {
				return &ValidationError{Code: ve.Code, Field: "candidates[" + itoa(i) + "]." + ve.Field, Msg: ve.Msg}
			}
			return err
		}
	}
	return nil
}

// ValidateFinding enforces FR-09: a finding references an existing
// confirmed verification of the same hypothesis and existing evidence.
func ValidateFinding(f Finding, verifications map[string]Verification, refs Refs) error {
	v, ok := verifications[f.VerificationID]
	if !ok {
		return &ValidationError{Code: CodeUnknownRef, Field: "verification_id", Msg: "unknown verification " + f.VerificationID}
	}
	if v.HypothesisID != f.HypothesisID {
		return &ValidationError{Code: CodeInvalidFinding, Field: "hypothesis_id", Msg: "does not match verification"}
	}
	if v.Outcome != OutcomeConfirmed {
		return &ValidationError{Code: CodeInvalidFinding, Field: "verification_id", Msg: "verification outcome is not confirmed"}
	}
	if len(f.EvidenceIDs) == 0 {
		return missing("evidence_ids")
	}
	return refs.checkEvidence("evidence_ids", f.EvidenceIDs)
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
