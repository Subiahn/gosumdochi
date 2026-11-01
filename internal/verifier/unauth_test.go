package verifier

import (
	"fmt"
	"testing"

	"gosumdochi/internal/model"
)

func docEvidence(id string, status int, ownerID, marker string) model.Evidence {
	body := fmt.Sprintf(`{"id":"d-1","owner_id":%q,"body":"text %s"}`, ownerID, marker)
	if status != 200 {
		body = `{"error":"x"}`
	}
	return model.Evidence{ID: id, SanitizedResponse: fmt.Sprintf("HTTP %d\n\n%s", status, body)}
}

func TestVerifyUnauthAccessConfirmed(t *testing.T) {
	recs := []Record{
		NewRecord(docEvidence("E-1", 200, "u-a", "LAB-MARK-abc123"), "http://x/d-1", "/d-1"),
		NewRecord(docEvidence("E-2", 200, "u-a", "LAB-MARK-abc123"), "http://x/d-1", "/d-1"),
	}
	recs[0].Account, recs[1].Account = "A", model.AccountNone
	v := VerifyUnauthAccess(recs)
	if v.Outcome != model.OutcomeConfirmed || v.BaselineIDs[0] != "E-1" || v.ComparisonIDs[0] != "E-2" {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyUnauthAccessRejected(t *testing.T) {
	recs := []Record{
		NewRecord(docEvidence("E-1", 200, "u-a", "LAB-MARK-abc123"), "http://x/d-1", "/d-1"),
		NewRecord(docEvidence("E-2", 401, "", ""), "http://x/d-1", "/d-1"),
	}
	recs[0].Account, recs[1].Account = "A", model.AccountNone
	v := VerifyUnauthAccess(recs)
	if v.Outcome != model.OutcomeRejected {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyUnauthAccessInconclusiveWithoutAttempt(t *testing.T) {
	recs := []Record{NewRecord(docEvidence("E-1", 200, "u-a", "LAB-MARK-abc123"), "http://x/d-1", "/d-1")}
	recs[0].Account = "A"
	v := VerifyUnauthAccess(recs)
	if v.Outcome != model.OutcomeInconclusive {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyUnauthAccessMismatchedMarkerNotEnough(t *testing.T) {
	// Anonymous got 200 but a different marker (e.g. a different, public doc):
	// must not confirm on status alone.
	recs := []Record{
		NewRecord(docEvidence("E-1", 200, "u-a", "LAB-MARK-abc123"), "http://x/d-1", "/d-1"),
		NewRecord(docEvidence("E-2", 200, "u-a", "LAB-MARK-zzz999"), "http://x/d-1", "/d-1"),
	}
	recs[0].Account, recs[1].Account = "A", model.AccountNone
	v := VerifyUnauthAccess(recs)
	if v.Outcome != model.OutcomeInconclusive {
		t.Fatalf("%+v", v)
	}
}
