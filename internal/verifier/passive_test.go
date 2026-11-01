package verifier

import (
	"testing"

	"gosumdochi/internal/model"
)

func TestVerifyPassiveExposureConfirmed(t *testing.T) {
	evs := []model.Evidence{
		{ID: "E-1", SanitizedResponse: "HTTP 200\nX-Debug-Backend: labapp-internal\n\n{}"},
		{ID: "E-2", SanitizedResponse: "HTTP 200\n\n{\"ok\":true}"},
	}
	v := VerifyPassiveExposure(evs)
	if v.Outcome != model.OutcomeConfirmed || len(v.ComparisonIDs) != 1 || v.ComparisonIDs[0] != "E-1" {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyPassiveExposureRejectedWhenClean(t *testing.T) {
	evs := []model.Evidence{{ID: "E-1", SanitizedResponse: "HTTP 200\n\n{\"ok\":true}"}}
	v := VerifyPassiveExposure(evs)
	if v.Outcome != model.OutcomeRejected {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyPassiveExposureDedupesSameEvidence(t *testing.T) {
	evs := []model.Evidence{{ID: "E-1", SanitizedResponse: "stack trace near /Users/me/app and .env leaked"}}
	v := VerifyPassiveExposure(evs)
	if v.Outcome != model.OutcomeConfirmed || len(v.ComparisonIDs) != 1 {
		t.Fatalf("expected one deduped evidence id: %+v", v)
	}
	if len(v.Reasons) < 2 {
		t.Fatalf("expected a reason per matched signal: %+v", v.Reasons)
	}
}

func TestVerifyPassiveExposureEmptyEvidence(t *testing.T) {
	v := VerifyPassiveExposure(nil)
	if v.Outcome != model.OutcomeRejected {
		t.Fatalf("%+v", v)
	}
}
