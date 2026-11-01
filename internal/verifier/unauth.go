package verifier

import "gosumdochi/internal/model"

const UnauthAccessRuleID = "unauth-access-v1"

// VerifyUnauthAccess confirms when a request sent with no session at all
// (Record.Account == model.AccountNone, never a Planner-proposed candidate)
// returns the same owner and marker an authenticated read already showed.
// Record and NewRecord are shared with doc-cross-read-v1. Baseline and
// comparison are correlated by marker, not by URL: the public mirror this
// rule targets lives at a different path than the authenticated original on
// purpose, so the two requests never share a URL.
func VerifyUnauthAccess(records []Record) model.Verification {
	v := model.Verification{RuleID: UnauthAccessRuleID, Outcome: model.OutcomeInconclusive}

	var baselines []Record
	for _, r := range records {
		if r.Account != model.AccountNone && r.Status == 200 && r.Usable && r.Marker != "" {
			baselines = append(baselines, r)
		}
	}

	anonTried := false
	for _, r := range records {
		if r.Account != model.AccountNone {
			continue
		}
		anonTried = true
		switch {
		case r.Status == 200 && r.Usable && r.Marker != "":
			for _, base := range baselines {
				if base.Marker != r.Marker {
					continue
				}
				v.Outcome = model.OutcomeConfirmed
				v.BaselineIDs = []string{base.EvidenceID}
				v.ComparisonIDs = []string{r.EvidenceID}
				v.Reasons = []string{"a request with no session returned the same document (same marker) an authenticated read showed"}
				return v
			}
		case len(baselines) > 0 && (r.Status == 401 || r.Status == 403):
			v.Outcome = model.OutcomeRejected
			v.BaselineIDs = []string{baselines[0].EvidenceID}
			v.ComparisonIDs = []string{r.EvidenceID}
			v.Reasons = []string{"the unauthenticated request was refused"}
			return v
		}
	}
	if !anonTried {
		v.Reasons = []string{"no unauthenticated request attempted yet"}
	} else {
		v.Reasons = []string{"unauthenticated response neither confirms nor rejects"}
	}
	return v
}
