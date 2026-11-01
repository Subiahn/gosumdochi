package verifier

import (
	"fmt"
	"testing"

	"gosumdochi/internal/model"
)

func adminEvidence(id string, status int, hasUsers bool) model.Evidence {
	body := `{"ok":true}`
	if hasUsers {
		body = `{"users":[{"user_id":"u-a","role":"admin"},{"user_id":"u-b","role":""}]}`
	}
	return model.Evidence{ID: id, SanitizedResponse: fmt.Sprintf("HTTP %d\nContent-Type: application/json\n\n%s", status, body)}
}

func TestVerifyVerticalPrivescConfirmed(t *testing.T) {
	recs := []AdminRecord{
		NewAdminRecord(adminEvidence("E-1", 200, true), adminUsersPath),
		NewAdminRecord(adminEvidence("E-2", 200, true), adminUsersPath),
	}
	recs[0].Account, recs[1].Account = "A", "B"
	v := VerifyVerticalPrivesc(recs, "A", "B")
	if v.Outcome != model.OutcomeConfirmed || v.BaselineIDs[0] != "E-1" || v.ComparisonIDs[0] != "E-2" {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyVerticalPrivescRejected(t *testing.T) {
	recs := []AdminRecord{
		NewAdminRecord(adminEvidence("E-1", 200, true), adminUsersPath),
		NewAdminRecord(adminEvidence("E-2", 403, false), adminUsersPath),
	}
	recs[0].Account, recs[1].Account = "A", "B"
	v := VerifyVerticalPrivesc(recs, "A", "B")
	if v.Outcome != model.OutcomeRejected {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyVerticalPrivescInconclusiveWithoutBaseline(t *testing.T) {
	recs := []AdminRecord{NewAdminRecord(adminEvidence("E-2", 200, true), adminUsersPath)}
	recs[0].Account = "B"
	v := VerifyVerticalPrivesc(recs, "A", "B")
	if v.Outcome != model.OutcomeInconclusive {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyVerticalPrivescStatusAloneNotEnough(t *testing.T) {
	// B got 200 but body has no user list: must not confirm on status alone.
	recs := []AdminRecord{
		NewAdminRecord(adminEvidence("E-1", 200, true), adminUsersPath),
		NewAdminRecord(adminEvidence("E-2", 200, false), adminUsersPath),
	}
	recs[0].Account, recs[1].Account = "A", "B"
	v := VerifyVerticalPrivesc(recs, "A", "B")
	if v.Outcome != model.OutcomeInconclusive {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyVerticalPrivescIgnoresOtherPaths(t *testing.T) {
	recs := []AdminRecord{NewAdminRecord(adminEvidence("E-1", 200, true), "/api/docs")}
	recs[0].Account = "A"
	v := VerifyVerticalPrivesc(recs, "A", "B")
	if v.Outcome != model.OutcomeInconclusive {
		t.Fatalf("%+v", v)
	}
}
