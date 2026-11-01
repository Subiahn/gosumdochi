package verifier

import (
	"fmt"
	"strings"

	"gosumdochi/internal/model"
)

const PassiveExposureRuleID = "passive-exposure-v1"

// passiveSignals are deterministic string matches against the already
// masked SanitizedResponse (headers + body). No new request, no budget.
var passiveSignals = []string{"X-Debug-Backend", "/Users/", "stack trace", ".env"}

// VerifyPassiveExposure rescans Evidence already collected for other
// purposes. It never makes a request itself.
func VerifyPassiveExposure(evs []model.Evidence) model.Verification {
	v := model.Verification{RuleID: PassiveExposureRuleID, Outcome: model.OutcomeRejected}
	seen := map[string]bool{}
	for _, e := range evs {
		for _, sig := range passiveSignals {
			if !strings.Contains(e.SanitizedResponse, sig) {
				continue
			}
			v.Outcome = model.OutcomeConfirmed
			if !seen[e.ID] {
				v.ComparisonIDs = append(v.ComparisonIDs, e.ID)
				seen[e.ID] = true
			}
			v.Reasons = append(v.Reasons, fmt.Sprintf("evidence %s contains signal %q", e.ID, sig))
		}
	}
	if v.Outcome == model.OutcomeRejected {
		v.Reasons = []string{fmt.Sprintf("no known exposure signal found in %d pieces of evidence", len(evs))}
	}
	return v
}
