package verifier

import (
	"fmt"
	"strings"

	"gosumdochi/internal/model"
)

const RuleIDReflected = "reflected-input-v1"

// sqliErrorPatterns are fixed, deterministic signals that a probe surfaced a
// database error (ADR-0002 §7). String matching only, never model judgment.
var sqliErrorPatterns = []string{
	"sql syntax", "sqlite error", "sqlite3.operationalerror",
	"unrecognized token", "near \"'\"", "syntax error",
}

// ReflectedRecord is one reflected-input-v1 probe: the marker the agent asked
// Policy to inject, and the response body it produced.
type ReflectedRecord struct {
	EvidenceID string
	MarkerID   model.MarkerID
	// Value is the literal payload Policy resolved MarkerID to (ADR-0002);
	// the verifier never sees a marker_id string the model invented, only
	// what Policy actually sent.
	Value string
	Body  string
}

// NewReflectedRecord builds a record from stored evidence. value is the
// marker's resolved literal payload (e.g. policy.MarkerValue(markerID)).
func NewReflectedRecord(ev model.Evidence, markerID model.MarkerID, value string) ReflectedRecord {
	_, body, _ := strings.Cut(ev.SanitizedResponse, "\n\n")
	return ReflectedRecord{EvidenceID: ev.ID, MarkerID: markerID, Value: value, Body: body}
}

// VerifyReflectedInput evaluates the reflected-input-v1 hypothesis.
//
// confirmed: an "xss-*" marker's literal payload appears verbatim,
// unescaped, in the response body (reflection survived whatever encoding
// the app applies); or any probe's response body matches a fixed database
// error pattern (sqli signal). The benign marker is never itself a
// confirming signal -- it has no metacharacters, so finding it proves
// nothing about escaping.
func VerifyReflectedInput(records []ReflectedRecord) model.Verification {
	v := model.Verification{RuleID: RuleIDReflected, Outcome: model.OutcomeInconclusive}
	if len(records) == 0 {
		v.Reasons = []string{"no reflected-input probe attempted yet"}
		return v
	}
	for _, r := range records {
		id := string(r.MarkerID)
		if strings.HasPrefix(id, "xss-") && r.Value != "" && strings.Contains(r.Body, r.Value) {
			v.Outcome = model.OutcomeConfirmed
			v.ComparisonIDs = []string{r.EvidenceID}
			v.Reasons = []string{"marker " + id + " was reflected unescaped in the response"}
			return v
		}
		lower := strings.ToLower(r.Body)
		for _, pat := range sqliErrorPatterns {
			if strings.Contains(lower, pat) {
				v.Outcome = model.OutcomeConfirmed
				v.ComparisonIDs = []string{r.EvidenceID}
				v.Reasons = []string{"response surfaced a database error pattern after marker " + id}
				return v
			}
		}
	}
	v.Outcome = model.OutcomeRejected
	v.Reasons = []string{fmt.Sprintf("no marker reflected unescaped and no database error pattern across %d probes", len(records))}
	return v
}
